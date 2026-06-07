#include "RecoGridEngine.h"

#include <MaaUtils/ImageIo.h>

#include <MaaUtils/NoWarningCV.hpp>

#include <algorithm>
#include <cctype>
#include <cmath>
#include <stdexcept>
#include <unordered_set>
#include <utility>

namespace fs = std::filesystem;

namespace recogrid
{
namespace
{

using SessionCells = std::map<std::pair<int, int>, GridScanCell>;

std::string LowercaseExtension(fs::path path)
{
    std::string extension = path.extension().string();
    std::transform(extension.begin(), extension.end(), extension.begin(), [](unsigned char ch) {
        return static_cast<char>(std::tolower(ch));
    });
    return extension;
}

bool IsSupportedTemplateFile(const fs::path& path)
{
    if (!fs::is_regular_file(path)) {
        return false;
    }

    const std::string extension = LowercaseExtension(path);
    return extension == ".png" || extension == ".jpg" || extension == ".jpeg" || extension == ".webp" || extension == ".bmp";
}

cv::Rect ClampRect(const cv::Rect& rect, const cv::Size& bounds)
{
    return rect & cv::Rect(0, 0, bounds.width, bounds.height);
}

cv::Rect OffsetRect(const cv::Rect& rect, const cv::Point& offset)
{
    if (rect.empty()) {
        return {};
    }
    return { rect.x + offset.x, rect.y + offset.y, rect.width, rect.height };
}

cv::Rect ScaleRect(const cv::Rect& rect, cv::Size fromSize, cv::Size toSize)
{
    if (rect.empty() || fromSize.width <= 0 || fromSize.height <= 0 || toSize.width <= 0 || toSize.height <= 0) {
        return {};
    }

    const double scaleX = static_cast<double>(toSize.width) / static_cast<double>(fromSize.width);
    const double scaleY = static_cast<double>(toSize.height) / static_cast<double>(fromSize.height);
    const int x = static_cast<int>(std::lround(static_cast<double>(rect.x) * scaleX));
    const int y = static_cast<int>(std::lround(static_cast<double>(rect.y) * scaleY));
    const int right = static_cast<int>(std::lround(static_cast<double>(rect.x + rect.width) * scaleX));
    const int bottom = static_cast<int>(std::lround(static_cast<double>(rect.y + rect.height) * scaleY));
    return ClampRect({ x, y, std::max(1, right - x), std::max(1, bottom - y) }, toSize);
}

cv::Rect RoiToScreen(const cv::Rect& rect, const GridDetectOptions& options, cv::Size imageSize)
{
    return ScaleRect(OffsetRect(rect, { options.roi.x, options.roi.y }), options.normalizedSize, imageSize);
}

GridClassifyOptions ToClassifyOptions(const GridRecognitionOptions& options)
{
    GridClassifyOptions classify;
    classify.maxPhashDistance = std::max(0, options.maxPhashDistance);
    classify.minScore = std::clamp(options.minScore, 0.0, 1.0);
    classify.hueWeight = std::clamp(options.hueWeight, 0.0, 1.0);
    classify.maxRankedCandidates = std::max(0, options.maxRankedCandidates);
    return classify;
}

GridHashSnapshot ToHashSnapshot(const GridRecognitionResult& result)
{
    return MakeGridHashSnapshot(
        static_cast<int>(result.grid.rows.size()),
        static_cast<int>(result.grid.cols.size()),
        result.cellHashes);
}

std::size_t CellIndex(int row, int col, int cols)
{
    return static_cast<std::size_t>(row * cols + col);
}

std::vector<std::size_t> NewCellIndicesForOffset(const GridHashSnapshot& current, int rowOffset)
{
    std::vector<std::size_t> indices;
    if (rowOffset <= 0 || current.rows <= 0 || current.cols <= 0) {
        return indices;
    }

    const int newRows = std::min(rowOffset, current.rows);
    const int startRow = std::max(0, current.rows - newRows);
    indices.reserve(static_cast<std::size_t>(newRows * current.cols));
    for (int row = startRow; row < current.rows; ++row) {
        for (int col = 0; col < current.cols; ++col) {
            const std::size_t index = CellIndex(row, col, current.cols);
            if (index < current.hashes.size()) {
                indices.push_back(index);
            }
        }
    }
    return indices;
}

int CountLeadingPartialRows(const GridResult& grid)
{
    int count = 0;
    for (const Segment& row : grid.rows) {
        if (grid.minRowHeight <= 0 || SegmentLength(row) >= grid.minRowHeight) {
            break;
        }
        ++count;
    }
    return count;
}

cv::Mat ToGray(const cv::Mat& image)
{
    if (image.channels() == 1) {
        return image;
    }

    cv::Mat gray;
    if (image.channels() == 4) {
        cv::cvtColor(image, gray, cv::COLOR_BGRA2GRAY);
    }
    else if (image.channels() == 3) {
        cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
    }
    else {
        throw std::invalid_argument("Unsupported image channel count for grid cell occupancy");
    }
    return gray;
}

bool IsOccupiedCell(const cv::Mat& roi, const cv::Rect& rect, const GridScanOptions& options)
{
    const cv::Rect clipped = ClampRect(rect, roi.size());
    if (clipped.empty()) {
        return false;
    }

    const cv::Mat gray = ToGray(roi(clipped));
    const cv::Mat keepMask = BuildIgnoreMask(gray.size(), options.recognition.mask);
    const int keptPixels = keepMask.empty() ? gray.rows * gray.cols : cv::countNonZero(keepMask);
    if (keptPixels <= 0) {
        return false;
    }

    cv::Mat bright;
    cv::threshold(gray, bright, std::clamp(options.occupiedBrightThreshold, 0, 255), 255, cv::THRESH_BINARY);
    if (!keepMask.empty()) {
        cv::bitwise_and(bright, keepMask, bright);
    }

    const double mean = keepMask.empty() ? cv::mean(gray)[0] : cv::mean(gray, keepMask)[0];
    const double brightRatio = static_cast<double>(cv::countNonZero(bright)) / static_cast<double>(keptPixels);
    return mean >= options.minOccupiedMean && brightRatio >= options.minOccupiedBrightRatio;
}

std::vector<std::size_t> CellIndices(const std::vector<GridScanCell>& cells)
{
    std::vector<std::size_t> indices;
    indices.reserve(cells.size());
    for (const GridScanCell& cell : cells) {
        indices.push_back(cell.cellIndex);
    }
    return indices;
}

std::vector<std::size_t> ClassifyIndicesForIncrementalCells(
    const SessionCells& previousCells,
    const std::vector<GridScanCell>& currentCells,
    const std::vector<std::size_t>& deltaNewIndices)
{
    std::unordered_set<std::size_t> selected(deltaNewIndices.begin(), deltaNewIndices.end());
    for (const GridScanCell& cell : currentCells) {
        const auto key = std::make_pair(cell.row, cell.col);
        if (previousCells.find(key) == previousCells.end()) {
            selected.insert(cell.cellIndex);
        }
    }

    std::vector<std::size_t> indices;
    indices.reserve(currentCells.size());
    for (const GridScanCell& cell : currentCells) {
        if (selected.contains(cell.cellIndex)) {
            indices.push_back(cell.cellIndex);
        }
    }
    return indices;
}

std::vector<GridScanCell> MakeUnknownCells(
    int startRow,
    int rows,
    int cols,
    const cv::Mat& gridRoi,
    const std::vector<cv::Rect>& gridCells,
    const GridScanOptions& scanOptions,
    const GridRecognitionOptions& options,
    cv::Size imageSize,
    const std::string& unknownTemplateId)
{
    std::vector<GridScanCell> cells;
    if (rows <= 0 || cols <= 0) {
        return cells;
    }

    cells.reserve(static_cast<std::size_t>(rows * cols));
    for (int row = 0; row < rows; ++row) {
        for (int col = 0; col < cols; ++col) {
            const std::size_t index = CellIndex(row, col, cols);
            if (index >= gridCells.size() || !IsOccupiedCell(gridRoi, gridCells[index], scanOptions)) {
                continue;
            }

            GridScanCell cell;
            cell.row = startRow + row;
            cell.col = col;
            cell.cellIndex = index;
            cell.templateId = unknownTemplateId;
            cell.visible = true;
            cell.screenCell = RoiToScreen(gridCells[index], options.detect, imageSize);
            cells.push_back(std::move(cell));
        }
    }
    return cells;
}

void ApplyClassifications(
    std::vector<GridScanCell>& cells,
    const GridClassificationResult& classification,
    int cols,
    int startRow,
    const std::string& unknownTemplateId)
{
    std::unordered_map<std::size_t, GridScanCell*> cellsByIndex;
    cellsByIndex.reserve(cells.size());
    for (GridScanCell& cell : cells) {
        cellsByIndex.emplace(cell.cellIndex, &cell);
    }

    for (const GridCellClassification& source : classification.cells) {
        const auto iter = cellsByIndex.find(source.cellIndex);
        if (iter == cellsByIndex.end()) {
            continue;
        }

        GridScanCell& target = *iter->second;
        const int localRow = cols > 0 ? static_cast<int>(source.cellIndex / static_cast<std::size_t>(cols)) : 0;
        const int col = cols > 0 ? static_cast<int>(source.cellIndex % static_cast<std::size_t>(cols)) : 0;
        target.row = startRow + localRow;
        target.col = col;
        target.cellIndex = source.cellIndex;
        target.screenCell = source.screenCell;
        target.visible = true;
        target.matched = source.matched;
        target.templateId = source.matched ? source.templateId : unknownTemplateId;
        target.score = source.score;
        target.templateScore = source.templateScore;
        target.hueScore = source.hueScore;
        target.phashDistance = source.phashDistance;
    }
}

GridScanResult MakeFailure(std::string message)
{
    GridScanResult result;
    result.message = std::move(message);
    return result;
}

void FinalizeCounts(GridScanResult& result)
{
    result.sessionTotalCells = static_cast<int>(result.cells.size());
    result.sessionRows = 0;
    result.knownCells = 0;
    result.unknownCells = 0;
    for (const GridScanCell& cell : result.cells) {
        result.sessionRows = std::max(result.sessionRows, cell.row + 1);
        if (cell.matched) {
            ++result.knownCells;
        }
        else {
            ++result.unknownCells;
        }
    }
}

std::vector<GridScanCell> ToSortedCells(const SessionCells& cells)
{
    std::vector<GridScanCell> output;
    output.reserve(cells.size());
    for (const auto& [_, cell] : cells) {
        output.push_back(cell);
    }
    return output;
}

void HideSessionCells(SessionCells& cells)
{
    for (auto& [_, cell] : cells) {
        cell.visible = false;
        cell.screenCell = {};
    }
}

bool ShouldReplaceCell(const GridScanCell& current, const GridScanCell& candidate)
{
    if (candidate.matched && !current.matched) {
        return true;
    }
    if (!candidate.matched) {
        return false;
    }
    if (candidate.score != current.score) {
        return candidate.score > current.score;
    }
    if (candidate.templateScore != current.templateScore) {
        return candidate.templateScore > current.templateScore;
    }
    if (candidate.phashDistance != current.phashDistance) {
        return candidate.phashDistance < current.phashDistance;
    }
    return candidate.templateId < current.templateId;
}

int UpsertSessionCell(SessionCells& cells, const GridScanCell& visibleCell)
{
    const auto key = std::make_pair(visibleCell.row, visibleCell.col);
    auto iter = cells.find(key);
    if (iter == cells.end()) {
        cells.emplace(key, visibleCell);
        return 1;
    }

    GridScanCell& target = iter->second;
    if (ShouldReplaceCell(target, visibleCell)) {
        target = visibleCell;
        return 0;
    }

    target.cellIndex = visibleCell.cellIndex;
    target.screenCell = visibleCell.screenCell;
    target.visible = true;
    return 0;
}

int UpsertSessionCells(SessionCells& cells, const std::vector<GridScanCell>& visibleCells)
{
    int inserted = 0;
    for (const GridScanCell& cell : visibleCells) {
        inserted += UpsertSessionCell(cells, cell);
    }
    return inserted;
}

int MaxSessionRow(const SessionCells& cells)
{
    int maxRow = -1;
    for (const auto& [_, cell] : cells) {
        maxRow = std::max(maxRow, cell.row);
    }
    return maxRow;
}

int MaxVisibleRow(const std::vector<GridScanCell>& cells)
{
    int maxRow = -1;
    for (const GridScanCell& cell : cells) {
        maxRow = std::max(maxRow, cell.row);
    }
    return maxRow;
}

std::unordered_set<int> VisibleColsInRow(const std::vector<GridScanCell>& cells, int row)
{
    std::unordered_set<int> cols;
    for (const GridScanCell& cell : cells) {
        if (cell.row == row) {
            cols.insert(cell.col);
        }
    }
    return cols;
}

bool HasTrailingPartialRow(const std::vector<GridScanCell>& cells, int cols)
{
    if (cols <= 0) {
        return false;
    }

    const int lastRow = MaxVisibleRow(cells);
    if (lastRow < 0) {
        return false;
    }

    const std::unordered_set<int> visibleCols = VisibleColsInRow(cells, lastRow);
    return !visibleCols.empty() && static_cast<int>(visibleCols.size()) < cols;
}

bool HasNewVisibleSessionKey(const SessionCells& sessionCells, const std::vector<GridScanCell>& visibleCells)
{
    return std::any_of(visibleCells.begin(), visibleCells.end(), [&](const GridScanCell& cell) {
        return sessionCells.find(std::make_pair(cell.row, cell.col)) == sessionCells.end();
    });
}

void PruneSessionTrailingPartialRow(SessionCells& sessionCells, const std::vector<GridScanCell>& visibleCells, int cols)
{
    if (!HasTrailingPartialRow(visibleCells, cols)) {
        return;
    }

    const int lastRow = MaxVisibleRow(visibleCells);
    const std::unordered_set<int> visibleCols = VisibleColsInRow(visibleCells, lastRow);
    for (auto iter = sessionCells.begin(); iter != sessionCells.end();) {
        const GridScanCell& cell = iter->second;
        if (cell.row > lastRow || (cell.row == lastRow && !visibleCols.contains(cell.col))) {
            iter = sessionCells.erase(iter);
        }
        else {
            ++iter;
        }
    }
}

} // namespace

void RecoGridEngine::LoadTemplatesFromDirectory(const fs::path& directory, const TemplateLoadOptions& options)
{
    if (!fs::exists(directory)) {
        throw std::invalid_argument("RecoGrid template directory does not exist");
    }
    if (!fs::is_directory(directory)) {
        throw std::invalid_argument("RecoGrid template path is not a directory");
    }

    std::vector<fs::path> paths;
    if (options.recursive) {
        for (const auto& entry : fs::recursive_directory_iterator(directory)) {
            if (IsSupportedTemplateFile(entry.path())) {
                paths.push_back(entry.path());
            }
        }
    }
    else {
        for (const auto& entry : fs::directory_iterator(directory)) {
            if (IsSupportedTemplateFile(entry.path())) {
                paths.push_back(entry.path());
            }
        }
    }

    std::sort(paths.begin(), paths.end());
    if (paths.empty()) {
        throw std::invalid_argument("RecoGrid template directory contains no supported images");
    }

    std::unordered_set<std::string> ids;
    std::vector<GridClassifyTemplate> templates;
    templates.reserve(paths.size());
    for (const fs::path& path : paths) {
        const std::string id = path.stem().string();
        if (id.empty()) {
            throw std::invalid_argument("RecoGrid template id cannot be empty");
        }
        if (!ids.insert(id).second) {
            throw std::invalid_argument("RecoGrid template id is duplicated: " + id);
        }

        cv::Mat image = MAA_NS::imread(path, cv::IMREAD_UNCHANGED);
        if (image.empty()) {
            throw std::invalid_argument("RecoGrid template image cannot be loaded: " + path.string());
        }
        templates.push_back({ id, std::move(image) });
    }

    SetTemplates(std::move(templates));
}

void RecoGridEngine::SetTemplates(std::vector<GridClassifyTemplate> templates)
{
    std::unordered_set<std::string> ids;
    for (const GridClassifyTemplate& entry : templates) {
        if (entry.id.empty()) {
            throw std::invalid_argument("RecoGrid template id cannot be empty");
        }
        if (entry.image.empty()) {
            throw std::invalid_argument("RecoGrid template image cannot be empty: " + entry.id);
        }
        if (!ids.insert(entry.id).second) {
            throw std::invalid_argument("RecoGrid template id is duplicated: " + entry.id);
        }
    }
    templates_ = std::move(templates);
    ClearSessions();
}

void RecoGridEngine::ResetSession(const std::string& sessionId)
{
    sessions_.erase(sessionId);
}

void RecoGridEngine::ClearSessions()
{
    sessions_.clear();
}

const std::vector<GridClassifyTemplate>& RecoGridEngine::Templates() const noexcept
{
    return templates_;
}

GridScanResult RecoGridEngine::Scan(const std::string& sessionId, const cv::Mat& image, const GridScanOptions& options)
{
    if (image.empty()) {
        return MakeFailure("Image is empty");
    }
    if (templates_.empty()) {
        return MakeFailure("No templates loaded");
    }

    try {
        GridScanResult result;
        GridRecognitionResult recognition = RecognizeGrid(image, options.recognition);
        result.rows = static_cast<int>(recognition.grid.rows.size());
        result.cols = static_cast<int>(recognition.grid.cols.size());
        result.totalCells = result.rows * result.cols;
        if (result.totalCells <= 0) {
            result.message = recognition.message.empty() ? "Grid detected no cells" : recognition.message;
            sessions_.erase(sessionId);
            return result;
        }

        const GridHashSnapshot currentSnapshot = ToHashSnapshot(recognition);
        const cv::Size imageSize = image.size();
        const GridClassifyOptions classifyOptions = ToClassifyOptions(options.recognition);
        auto sessionIt = sessions_.find(sessionId);
        const bool hasSession = sessionIt != sessions_.end();

        bool useIncremental = false;
        GridDeltaResult delta;
        if (options.incremental && hasSession && sessionIt->second.cols == result.cols) {
            delta = ComputeGridDelta(
                sessionIt->second.snapshot,
                currentSnapshot,
                { options.matchDistanceThreshold, options.minMatchRatio });
            const int leadingPartialRows = CountLeadingPartialRows(recognition.grid);
            if (delta.reliable && delta.hasProgress && delta.rowOffset > 0 && leadingPartialRows > 0) {
                delta.rowOffset += leadingPartialRows;
                delta.newCellIndices = NewCellIndicesForOffset(currentSnapshot, delta.rowOffset);
                delta.hasProgress = !delta.newCellIndices.empty();
            }
            useIncremental = delta.reliable && delta.hasProgress && delta.rowOffset > 0;
        }

        result.deltaReliable = delta.reliable;
        result.rowOffset = delta.rowOffset;
        result.matchedCells = delta.matchedCells;
        result.comparedCells = delta.comparedCells;
        result.totalDistance = delta.totalDistance;
        result.averageDistance = delta.averageDistance;
        result.deltaScore = delta.score;
        result.matchRatio = delta.matchRatio;
        result.newCellIndices = delta.newCellIndices;
        result.hasProgress = delta.hasProgress;

        auto keepSessionResult = [&](const SessionState& session, bool reachedEnd, std::string message) {
            result.success = true;
            result.message = std::move(message);
            result.reachedEnd = reachedEnd;
            result.sessionCols = session.cols;
            result.cells = ToSortedCells(session.cells);
            FinalizeCounts(result);
        };

        if (options.incremental && hasSession && sessionIt->second.cols == result.cols && delta.reliable && !delta.hasProgress) {
            SessionState stableSession = sessionIt->second;
            std::vector<GridScanCell> currentCells = MakeUnknownCells(
                stableSession.viewportStartRow,
                result.rows,
                result.cols,
                recognition.grid.roi,
                recognition.grid.cells,
                options,
                options.recognition,
                imageSize,
                options.unknownTemplateId);
            if (delta.rowOffset == 0 && HasTrailingPartialRow(currentCells, result.cols)) {
                const int rowOverrun = MaxVisibleRow(currentCells) - MaxSessionRow(stableSession.cells);
                if (rowOverrun > 0) {
                    stableSession.viewportStartRow = std::max(0, stableSession.viewportStartRow - rowOverrun);
                    currentCells = MakeUnknownCells(
                        stableSession.viewportStartRow,
                        result.rows,
                        result.cols,
                        recognition.grid.roi,
                        recognition.grid.cells,
                        options,
                        options.recognition,
                        imageSize,
                        options.unknownTemplateId);
                }
                if (!HasNewVisibleSessionKey(stableSession.cells, currentCells)) {
                    PruneSessionTrailingPartialRow(stableSession.cells, currentCells, result.cols);
                }
            }
            result.totalCells = static_cast<int>(currentCells.size());

            const bool hasNewVisibleKey = HasNewVisibleSessionKey(stableSession.cells, currentCells);
            const bool reachedEnd =
                delta.rowOffset == 0 && !hasNewVisibleKey &&
                (delta.matchRatio >= std::clamp(options.endMinMatchRatio, 0.0, 1.0) ||
                 HasTrailingPartialRow(currentCells, result.cols));
            if (reachedEnd) {
                sessionIt->second = stableSession;
            }
            keepSessionResult(
                stableSession,
                reachedEnd,
                reachedEnd ? "Grid scan reached end" : "Grid delta has no progress below end threshold; kept previous scan session");
            return result;
        }

        if (options.incremental && hasSession && sessionIt->second.cols == result.cols && !delta.reliable) {
            keepSessionResult(sessionIt->second, false, "Grid delta is unreliable; kept previous scan session");
            return result;
        }

        if (!useIncremental) {
            SessionState session;
            session.snapshot = currentSnapshot;
            session.viewportStartRow = 0;
            session.cols = result.cols;
            std::vector<GridScanCell> currentCells = MakeUnknownCells(
                0,
                result.rows,
                result.cols,
                recognition.grid.roi,
                recognition.grid.cells,
                options,
                options.recognition,
                imageSize,
                options.unknownTemplateId);
            result.totalCells = static_cast<int>(currentCells.size());

            const std::vector<std::size_t> occupiedIndices = CellIndices(currentCells);
            GridClassificationResult classification = ClassifyGridCells(
                recognition,
                templates_,
                options.recognition,
                classifyOptions,
                imageSize,
                occupiedIndices);

            ApplyClassifications(currentCells, classification, result.cols, 0, options.unknownTemplateId);
            result.newCellIndices = occupiedIndices;
            UpsertSessionCells(session.cells, currentCells);
            result.success = true;
            result.message = recognition.message.empty() ? "Grid scanned" : recognition.message;
            result.hasProgress = true;
            result.sessionCols = result.cols;
            result.cells = ToSortedCells(session.cells);
            FinalizeCounts(result);
            sessions_[sessionId] = std::move(session);
            return result;
        }

        SessionState& previous = sessionIt->second;
        SessionState next = previous;
        next.snapshot = currentSnapshot;
        next.viewportStartRow = previous.viewportStartRow + delta.rowOffset;
        next.cols = result.cols;
        HideSessionCells(next.cells);

        std::vector<GridScanCell> currentCells = MakeUnknownCells(
            next.viewportStartRow,
            result.rows,
            result.cols,
            recognition.grid.roi,
            recognition.grid.cells,
            options,
            options.recognition,
            imageSize,
            options.unknownTemplateId);
        result.totalCells = static_cast<int>(currentCells.size());

        const std::vector<std::size_t> occupiedNewIndices =
            ClassifyIndicesForIncrementalCells(previous.cells, currentCells, delta.newCellIndices);
        GridClassificationResult classification = ClassifyGridCells(
            recognition,
            templates_,
            options.recognition,
            classifyOptions,
            imageSize,
            occupiedNewIndices);

        ApplyClassifications(currentCells, classification, result.cols, next.viewportStartRow, options.unknownTemplateId);
        result.newCellIndices = occupiedNewIndices;
        UpsertSessionCells(next.cells, currentCells);

        result.success = true;
        result.message = recognition.message.empty() ? "Grid incrementally scanned" : recognition.message;
        result.incrementalUsed = true;
        result.hasProgress = true;
        result.sessionCols = result.cols;
        result.cells = ToSortedCells(next.cells);
        FinalizeCounts(result);
        previous = std::move(next);
        return result;
    }
    catch (const std::exception& e) {
        return MakeFailure(e.what());
    }
}

} // namespace recogrid
