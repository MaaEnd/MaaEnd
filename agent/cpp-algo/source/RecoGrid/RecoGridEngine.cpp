#include "RecoGridEngine.h"

#include "RecoGridScanCells.h"
#include "RecoGridTransition.h"

#include <MaaUtils/ImageIo.h>

#include <algorithm>
#include <cctype>
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
        result.cellHashes,
        result.cellFeatures);
}

GridScanResult MakeFailure(std::string message)
{
    GridScanResult result;
    result.message = std::move(message);
    return result;
}

void UsePreviousSession(GridScanResult& result, const SessionState& session)
{
    result.incrementalUsed = true;
    result.sessionCols = session.cols;
    result.cells = ToSortedCells(session.cells);
    FinalizeCounts(result);
    result.rows = result.sessionRows;
    result.cols = result.sessionCols;
    result.totalCells = result.sessionTotalCells;
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
        GridScanOptions effectiveOptions = options;
        auto sessionIt = sessions_.find(sessionId);
        const bool hasSession = sessionIt != sessions_.end();
        if (options.incremental && hasSession) {
            effectiveOptions.recognition.detect.lockedRowHeight = sessionIt->second.lockedRowHeight;
            effectiveOptions.recognition.detect.lockedColWidth = sessionIt->second.lockedColWidth;
        }

        GridRecognitionResult recognition = RecognizeGrid(image, effectiveOptions.recognition);
        result.rows = static_cast<int>(recognition.grid.rows.size());
        result.cols = static_cast<int>(recognition.grid.cols.size());
        result.totalCells = result.rows * result.cols;
        result.detectedRows = result.rows;
        result.detectedCols = result.cols;
        result.detectedTotalCells = result.totalCells;
        if (result.totalCells <= 0) {
            result.message = recognition.message.empty() ? "Grid detected no cells" : recognition.message;
            if (options.incremental && hasSession) {
                result.success = true;
                UsePreviousSession(result, sessionIt->second);
                return result;
            }
            sessions_.erase(sessionId);
            return result;
        }
        if (options.incremental && hasSession && sessionIt->second.cols > 0 && result.cols != sessionIt->second.cols) {
            result.success = true;
            result.message = "Grid shape rejected; kept previous scan session";
            UsePreviousSession(result, sessionIt->second);
            return result;
        }

        const GridHashSnapshot currentSnapshot = ToHashSnapshot(recognition);
        const cv::Size imageSize = image.size();
        const GridClassifyOptions classifyOptions = ToClassifyOptions(effectiveOptions.recognition);
        GridDeltaResult delta;
        if (options.incremental && hasSession && sessionIt->second.cols == result.cols) {
            delta = ComputeGridDelta(
                sessionIt->second.snapshot,
                currentSnapshot,
                { options.matchDistanceThreshold, options.minMatchRatio });
            AdjustLeadingPartialRowsForDelta(
                delta,
                recognition,
                effectiveOptions,
                imageSize,
                sessionIt->second.viewportStartRow,
                &sessionIt->second.cells);
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

        if (options.incremental && hasSession && sessionIt->second.cols == result.cols) {
            HandleBeamTransition(
                sessionIt->second,
                result,
                recognition,
                templates_,
                effectiveOptions,
                classifyOptions,
                currentSnapshot,
                delta,
                imageSize);
            return result;
        }

        SessionState session;
        session.snapshot = currentSnapshot;
        session.viewportStartRow = 0;
        session.cols = result.cols;
        session.lockedRowHeight = ModalSegmentLength(recognition.grid.rows);
        session.lockedColWidth = ModalSegmentLength(recognition.grid.cols);
        session.pending.reset();
        std::vector<GridScanCell> currentCells = MakeUnknownCells(
            0,
            result.rows,
            result.cols,
            recognition.grid.roi,
            recognition.grid.cells,
            effectiveOptions,
            effectiveOptions.recognition,
            imageSize,
            effectiveOptions.unknownTemplateId);
        result.totalCells = static_cast<int>(currentCells.size());

        const std::vector<std::size_t> occupiedIndices = CellIndices(currentCells);
        GridClassificationResult classification = ClassifyGridCells(
            recognition,
            templates_,
            effectiveOptions.recognition,
            classifyOptions,
            imageSize,
            occupiedIndices);

        ApplyClassifications(currentCells, classification, result.cols, 0, effectiveOptions.unknownTemplateId);
        result.newCellIndices = occupiedIndices;
        result.dispatchableCells = currentCells;
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
    catch (const std::exception& e) {
        return MakeFailure(e.what());
    }
}

} // namespace recogrid
