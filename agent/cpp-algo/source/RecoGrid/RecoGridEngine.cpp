#include "RecoGridEngine.h"

#include <MaaUtils/ImageIo.h>

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

std::vector<std::size_t> AllCellIndices(int rows, int cols)
{
    std::vector<std::size_t> indices;
    if (rows <= 0 || cols <= 0) {
        return indices;
    }

    indices.reserve(static_cast<std::size_t>(rows * cols));
    for (int row = 0; row < rows; ++row) {
        for (int col = 0; col < cols; ++col) {
            indices.push_back(CellIndex(row, col, cols));
        }
    }
    return indices;
}

std::vector<GridScanCell> MakeUnknownCells(
    int startRow,
    int rows,
    int cols,
    const std::vector<cv::Rect>& gridCells,
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
            GridScanCell cell;
            cell.row = startRow + row;
            cell.col = col;
            cell.cellIndex = index;
            cell.templateId = unknownTemplateId;
            cell.visible = true;
            if (index < gridCells.size()) {
                cell.screenCell = RoiToScreen(gridCells[index], options.detect, imageSize);
            }
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
    for (const GridCellClassification& source : classification.cells) {
        if (source.cellIndex >= cells.size()) {
            continue;
        }

        GridScanCell& target = cells[source.cellIndex];
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
    result.knownCells = 0;
    result.unknownCells = 0;
    for (const GridScanCell& cell : result.cells) {
        if (cell.matched) {
            ++result.knownCells;
        }
        else {
            ++result.unknownCells;
        }
    }
}

std::vector<GridScanCell> MakeSessionCells(int rows, int cols, const std::string& unknownTemplateId)
{
    std::vector<GridScanCell> cells;
    if (rows <= 0 || cols <= 0) {
        return cells;
    }

    cells.reserve(static_cast<std::size_t>(rows * cols));
    for (int row = 0; row < rows; ++row) {
        for (int col = 0; col < cols; ++col) {
            GridScanCell cell;
            cell.row = row;
            cell.col = col;
            cell.cellIndex = CellIndex(row, col, cols);
            cell.templateId = unknownTemplateId;
            cells.push_back(std::move(cell));
        }
    }
    return cells;
}

void MergeVisibleCell(GridScanCell& target, const GridScanCell& visibleCell, const std::string& unknownTemplateId)
{
    if (visibleCell.matched) {
        target = visibleCell;
        return;
    }

    target.row = visibleCell.row;
    target.col = visibleCell.col;
    target.cellIndex = visibleCell.cellIndex;
    target.screenCell = visibleCell.screenCell;
    target.visible = true;
    if (target.templateId.empty()) {
        target.templateId = unknownTemplateId;
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

        if (options.incremental && hasSession && sessionIt->second.cols == result.cols && delta.reliable && !delta.hasProgress) {
            result.success = true;
            result.message = "Grid scan reached end";
            result.reachedEnd = true;
            result.sessionRows = static_cast<int>(sessionIt->second.cells.size() /
                                                  static_cast<std::size_t>(std::max(1, sessionIt->second.cols)));
            result.sessionCols = sessionIt->second.cols;
            result.cells = sessionIt->second.cells;
            FinalizeCounts(result);
            return result;
        }

        if (!useIncremental) {
            GridClassificationResult classification = ClassifyGridCells(
                recognition,
                templates_,
                options.recognition,
                classifyOptions,
                imageSize,
                AllCellIndices(result.rows, result.cols));

            SessionState session;
            session.snapshot = currentSnapshot;
            session.viewportStartRow = 0;
            session.cols = result.cols;
            session.cells = MakeUnknownCells(
                0,
                result.rows,
                result.cols,
                recognition.grid.cells,
                options.recognition,
                imageSize,
                options.unknownTemplateId);
            ApplyClassifications(session.cells, classification, result.cols, 0, options.unknownTemplateId);
            result.success = true;
            result.message = recognition.message.empty() ? "Grid scanned" : recognition.message;
            result.hasProgress = true;
            result.sessionRows = result.rows;
            result.sessionCols = result.cols;
            result.cells = session.cells;
            FinalizeCounts(result);
            sessions_[sessionId] = std::move(session);
            return result;
        }

        GridClassificationResult classification = ClassifyGridCells(
            recognition,
            templates_,
            options.recognition,
            classifyOptions,
            imageSize,
            delta.newCellIndices);

        SessionState& previous = sessionIt->second;
        SessionState next;
        next.snapshot = currentSnapshot;
        next.viewportStartRow = previous.viewportStartRow + delta.rowOffset;
        next.cols = result.cols;

        const int newSessionRows = std::max(
            static_cast<int>(previous.cells.size() / static_cast<std::size_t>(std::max(1, previous.cols))) + delta.rowOffset,
            result.rows);
        next.cells = MakeSessionCells(newSessionRows, result.cols, options.unknownTemplateId);

        for (const GridScanCell& oldCell : previous.cells) {
            if (oldCell.col < 0 || oldCell.col >= result.cols || oldCell.row < 0) {
                continue;
            }
            const std::size_t targetIndex = CellIndex(oldCell.row, oldCell.col, result.cols);
            if (targetIndex >= next.cells.size()) {
                continue;
            }
            GridScanCell copied = oldCell;
            copied.cellIndex = targetIndex;
            copied.visible = false;
            copied.screenCell = {};
            copied.row = oldCell.row;
            copied.col = oldCell.col;
            next.cells[targetIndex] = std::move(copied);
        }

        std::vector<GridScanCell> currentCells = MakeUnknownCells(
            next.viewportStartRow,
            result.rows,
            result.cols,
            recognition.grid.cells,
            options.recognition,
            imageSize,
            options.unknownTemplateId);
        ApplyClassifications(currentCells, classification, result.cols, next.viewportStartRow, options.unknownTemplateId);

        for (const GridScanCell& currentCell : currentCells) {
            const std::size_t targetIndex = CellIndex(currentCell.row, currentCell.col, result.cols);
            if (targetIndex < next.cells.size()) {
                MergeVisibleCell(next.cells[targetIndex], currentCell, options.unknownTemplateId);
            }
        }

        result.success = true;
        result.message = recognition.message.empty() ? "Grid incrementally scanned" : recognition.message;
        result.incrementalUsed = true;
        result.hasProgress = true;
        result.sessionRows = newSessionRows;
        result.sessionCols = result.cols;
        result.cells = next.cells;
        FinalizeCounts(result);
        previous = std::move(next);
        return result;
    }
    catch (const std::exception& e) {
        return MakeFailure(e.what());
    }
}

} // namespace recogrid
