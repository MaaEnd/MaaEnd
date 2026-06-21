#include "EssenceGridScan.h"

#include "../RecoGrid/GridRecognizer.h"
#include "../RecoGrid/RecoGridScanCells.h"
#include "../utils.h"

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>

#include <meojson/json.hpp>

#include <algorithm>
#include <cstring>
#include <filesystem>
#include <map>
#include <optional>
#include <set>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace essencegridscan
{
namespace
{

constexpr const char* kRuntimeTemplatePath = "resource/image/EssenceFilter/EssenceGeneral.png";
constexpr const char* kSourceTemplatePath = "assets/resource/image/EssenceFilter/EssenceGeneral.png";
constexpr const char* kClickNextNode = "EssenceGridClickPending";
constexpr const char* kSwipeNextNode = "EssenceGridSwipeNext";
constexpr const char* kFinishNode = "EssenceFilterFinish";

std::vector<recogrid::GridClassifyTemplate> g_templates;
bool g_loaded = false;
MaaTaskId g_lastTaskId = MaaInvalidId;
std::set<std::pair<int, int>> g_issuedCellKeys;
std::set<std::pair<int, int>> g_seenCellKeys;
std::map<std::pair<int, int>, std::string> g_cellQualities;
std::optional<recogrid::GridScanCell> g_pendingCell;
std::optional<recogrid::GridScanResult> g_lastScanResult;
std::vector<recogrid::GridScanCell> g_currentPageQueue;
std::size_t g_currentPageQueueIndex = 0;
bool g_scanRequired = true;
bool g_hasViewportSnapshot = false;
recogrid::GridHashSnapshot g_lastViewportSnapshot;
int g_viewportStartRow = 0;
int g_maxSeenRow = -1;

std::filesystem::path ResolveEssenceTemplatePath()
{
    for (const char* path : { kRuntimeTemplatePath, kSourceTemplatePath }) {
        std::error_code ec;
        if (std::filesystem::exists(path, ec) && std::filesystem::is_regular_file(path, ec)) {
            return path;
        }
    }
    throw std::runtime_error("Essence grid template image not found");
}

void EnsureLoaded()
{
    if (g_loaded) {
        return;
    }

    const std::filesystem::path path = ResolveEssenceTemplatePath();
    cv::Mat image = MAA_NS::imread(path, cv::IMREAD_UNCHANGED);
    if (image.empty()) {
        throw std::runtime_error("Essence grid template image cannot be loaded: " + path.string());
    }
    g_templates = { { "essence_general", std::move(image) } };
    g_loaded = true;
}

void ApplyEssenceScanDefaults(recogrid::GridScanOptions& options)
{
    options.recognition.detect.roi = { 18, 72, 956, 570 };
    options.recognition.detect.normalizedSize = { 1280, 720 };
    options.recognition.detect.rowThresholdRatio = 0.2;
    options.recognition.detect.colThresholdRatio = 0.4;
    options.recognition.detect.minRawSegmentLength = 10;
    options.recognition.detect.minKeptSegmentRatio = 0.9;
    options.recognition.maxPhashDistance = 10;
    options.recognition.maxRankedCandidates = 0;
    options.recognition.minScore = 0.35;
    options.recognition.hueWeight = 0.4;
    options.incremental = true;
    options.endMinMatchRatio = 0.95;
}

bool ReadBooleanOption(const char* raw, const char* key, bool defaultValue)
{
    if (raw == nullptr || std::strlen(raw) == 0 || key == nullptr || std::strlen(key) == 0) {
        return defaultValue;
    }
    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        return defaultValue;
    }
    const auto& object = parsed->as_object();
    if (!object.contains(key) || !object.at(key).is_boolean()) {
        return defaultValue;
    }
    return object.at(key).as_boolean();
}

double ReadDoubleOption(const char* raw, const char* key, double defaultValue)
{
    if (raw == nullptr || std::strlen(raw) == 0 || key == nullptr || std::strlen(key) == 0) {
        return defaultValue;
    }
    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        return defaultValue;
    }
    const auto& object = parsed->as_object();
    if (!object.contains(key) || !object.at(key).is_number()) {
        return defaultValue;
    }
    return object.at(key).as_double();
}

recogrid::GridRecognitionRequest ParseEssenceRecognitionRequest(
    const char* raw,
    const recogrid::GridRecognitionOptions& defaults)
{
    recogrid::GridRecognitionRequest request;
    request.options = defaults;
    request.classify.maxPhashDistance = defaults.maxPhashDistance;
    request.classify.minScore = defaults.minScore;
    request.classify.hueWeight = defaults.hueWeight;
    request.classify.maxRankedCandidates = defaults.maxRankedCandidates;

    if (raw == nullptr || std::strlen(raw) == 0) {
        return request;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        throw std::invalid_argument("custom_recognition_param must be a JSON object");
    }
    if (!request.from_json(*parsed)) {
        throw std::invalid_argument("custom_recognition_param cannot be converted to GridRecognitionRequest");
    }
    return request;
}

void ResetSessionForNewTask(MaaTaskId taskId)
{
    if (taskId == MaaInvalidId || taskId == g_lastTaskId) {
        return;
    }
    g_issuedCellKeys.clear();
    g_seenCellKeys.clear();
    g_cellQualities.clear();
    g_pendingCell.reset();
    g_lastScanResult.reset();
    g_currentPageQueue.clear();
    g_currentPageQueueIndex = 0;
    g_scanRequired = true;
    g_hasViewportSnapshot = false;
    g_lastViewportSnapshot = {};
    g_viewportStartRow = 0;
    g_maxSeenRow = -1;
    g_lastTaskId = taskId;
    LogInfo << "EssenceGridScan reset session" << VAR(taskId);
}

json::object ToJsonRect(const cv::Rect& rect)
{
    json::object output;
    output["x"] = rect.x;
    output["y"] = rect.y;
    output["width"] = rect.width;
    output["height"] = rect.height;
    return output;
}

std::pair<int, int> CellKey(const recogrid::GridScanCell& cell)
{
    return { cell.row, cell.col };
}

std::string CellQuality(const recogrid::GridScanCell& cell)
{
    const auto iter = g_cellQualities.find(CellKey(cell));
    if (iter == g_cellQualities.end()) {
        return "unknown";
    }
    return iter->second;
}

struct QualityStats
{
    std::string quality = "unknown";
    int sampledPixels = 0;
    int goldPixels = 0;
    int purplePixels = 0;
};

struct QualityFilter
{
    bool hasExplicitSelection = false;
    bool flawlessEssence = true;
    bool pureEssence = true;
};

bool IsGoldPixel(const cv::Vec3b& hsv)
{
    return hsv[0] >= 16 && hsv[0] <= 29 && hsv[1] >= 71 && hsv[2] >= 89;
}

bool IsPurplePixel(const cv::Vec3b& hsv)
{
    return hsv[0] >= 128 && hsv[0] <= 158 && hsv[1] >= 61 && hsv[2] >= 71;
}

QualityStats ClassifyCellQuality(const cv::Mat& image, const cv::Rect& screenCell)
{
    QualityStats stats;
    const cv::Rect imageBounds(0, 0, image.cols, image.rows);
    const cv::Rect cell = screenCell & imageBounds;
    if (cell.empty()) {
        return stats;
    }

    const int sampleHeight = std::max(1, cell.height / 10);
    const cv::Rect sampleRect(cell.x, cell.y + cell.height - sampleHeight, cell.width, sampleHeight);
    cv::Mat sample = image(sampleRect);
    cv::Mat bgr;
    if (sample.channels() == 4) {
        cv::cvtColor(sample, bgr, cv::COLOR_BGRA2BGR);
    }
    else if (sample.channels() == 3) {
        bgr = sample;
    }
    else if (sample.channels() == 1) {
        cv::cvtColor(sample, bgr, cv::COLOR_GRAY2BGR);
    }
    else {
        return stats;
    }

    cv::Mat hsv;
    cv::cvtColor(bgr, hsv, cv::COLOR_BGR2HSV);
    stats.sampledPixels = hsv.rows * hsv.cols;
    for (int row = 0; row < hsv.rows; ++row) {
        for (int col = 0; col < hsv.cols; ++col) {
            const cv::Vec3b pixel = hsv.at<cv::Vec3b>(row, col);
            if (IsGoldPixel(pixel)) {
                stats.goldPixels++;
            }
            if (IsPurplePixel(pixel)) {
                stats.purplePixels++;
            }
        }
    }

    constexpr int kMinQualityPixels = 80;
    if (stats.goldPixels >= kMinQualityPixels && stats.goldPixels >= stats.purplePixels * 2) {
        stats.quality = "flawless_gold";
    }
    else if (stats.purplePixels >= kMinQualityPixels && stats.purplePixels >= stats.goldPixels * 2) {
        stats.quality = "high_purity_purple";
    }
    return stats;
}

QualityFilter ParseQualityFilter(const char* raw)
{
    QualityFilter filter;
    if (raw == nullptr || std::strlen(raw) == 0) {
        return filter;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        return filter;
    }

    const auto& object = parsed->as_object();
    const bool hasFlawless = object.contains("flawless_essence") && object.at("flawless_essence").is_boolean();
    const bool hasPure = object.contains("pure_essence") && object.at("pure_essence").is_boolean();
    filter.hasExplicitSelection = hasFlawless || hasPure;
    if (hasFlawless) {
        filter.flawlessEssence = object.at("flawless_essence").as_boolean();
    }
    if (hasPure) {
        filter.pureEssence = object.at("pure_essence").as_boolean();
    }
    return filter;
}

bool ShouldDispatchQuality(const recogrid::GridScanCell& cell, const QualityFilter& filter)
{
    if (!filter.hasExplicitSelection) {
        return true;
    }

    const std::string quality = CellQuality(cell);
    if (quality == "flawless_gold") {
        return filter.flawlessEssence;
    }
    if (quality == "high_purity_purple") {
        return filter.pureEssence;
    }
    return false;
}

void WriteError(MaaStringBuffer* outDetail, const char* message)
{
    if (outDetail == nullptr) {
        return;
    }

    json::object detail;
    detail["success"] = false;
    detail["message"] = message == nullptr ? "" : message;
    const std::string text = json::value(std::move(detail)).dumps();
    MaaStringBufferSet(outDetail, text.c_str());
}

void WriteAdvanceDetail(
    MaaStringBuffer* outDetail,
    const recogrid::GridScanResult& result,
    const std::optional<recogrid::GridScanCell>& selected,
    const QualityFilter& filter)
{
    if (outDetail == nullptr) {
        return;
    }

    const int remainingQueueCells = static_cast<int>(g_currentPageQueue.size() - g_currentPageQueueIndex);
    const int visibleCandidates = remainingQueueCells + (selected ? 1 : 0);

    json::object detail;
    detail["success"] = result.success;
    detail["message"] = result.message;
    detail["page_grid"] = result.totalCells;
    detail["cumulative_grid"] = result.sessionTotalCells;
    detail["rows"] = result.sessionRows;
    detail["cols"] = result.sessionCols;
    detail["visible_candidates"] = visibleCandidates;
    detail["issued_cells"] = static_cast<int>(g_issuedCellKeys.size());
    detail["queue_remaining"] = remainingQueueCells;
    detail["scan_required"] = g_scanRequired;
    detail["filter_flawless_essence"] = filter.flawlessEssence;
    detail["filter_pure_essence"] = filter.pureEssence;
    detail["filter_explicit"] = filter.hasExplicitSelection;
    detail["selected_cell_index"] = selected ? static_cast<int>(selected->cellIndex) : -1;
    detail["selected_row"] = selected ? selected->row : -1;
    detail["selected_col"] = selected ? selected->col : -1;
    detail["selected_quality"] = selected ? CellQuality(*selected) : "unknown";
    detail["selected_box"] = selected ? ToJsonRect(selected->screenCell) : json::object {};
    detail["reached_end"] = result.reachedEnd;
    detail["has_progress"] = result.hasProgress;
    detail["row_offset"] = result.rowOffset;
    detail["match_ratio"] = result.matchRatio;
    detail["pending_stored"] = result.pendingStored;
    detail["pending_resolved"] = result.pendingResolved;

    json::object retainedQualityCounts;
    retainedQualityCounts["flawless_gold"] = 0;
    retainedQualityCounts["high_purity_purple"] = 0;
    retainedQualityCounts["unknown"] = 0;
    for (const recogrid::GridScanCell& cell : result.cells) {
        const std::string quality = CellQuality(cell);
        if (retainedQualityCounts.contains(quality) && retainedQualityCounts[quality].is_number()) {
            retainedQualityCounts[quality] = retainedQualityCounts[quality].as_integer() + 1;
        }
        else {
            retainedQualityCounts["unknown"] = retainedQualityCounts["unknown"].as_integer() + 1;
        }
    }
    detail["retained_quality_counts"] = std::move(retainedQualityCounts);

    json::object qualityCounts;
    qualityCounts["flawless_gold"] = 0;
    qualityCounts["high_purity_purple"] = 0;
    qualityCounts["unknown"] = 0;
    for (const recogrid::GridScanCell& cell : g_currentPageQueue) {
        const std::string quality = CellQuality(cell);
        if (qualityCounts.contains(quality) && qualityCounts[quality].is_number()) {
            qualityCounts[quality] = qualityCounts[quality].as_integer() + 1;
        }
        else {
            qualityCounts["unknown"] = qualityCounts["unknown"].as_integer() + 1;
        }
    }
    detail["quality_counts"] = std::move(qualityCounts);

    const std::string text = json::value(std::move(detail)).dumps();
    MaaStringBufferSet(outDetail, text.c_str());
}

void WritePendingDetail(MaaStringBuffer* outDetail, bool success, const char* message)
{
    if (outDetail == nullptr) {
        return;
    }

    json::object detail;
    detail["success"] = success;
    detail["message"] = message == nullptr ? "" : message;
    detail["selected_cell_index"] = g_pendingCell ? static_cast<int>(g_pendingCell->cellIndex) : -1;
    detail["selected_quality"] = g_pendingCell ? CellQuality(*g_pendingCell) : "unknown";
    detail["selected_box"] = g_pendingCell ? ToJsonRect(g_pendingCell->screenCell) : json::object {};
    const std::string text = json::value(std::move(detail)).dumps();
    MaaStringBufferSet(outDetail, text.c_str());
}

bool OverrideNext(MaaContext* context, const char* nodeName, const char* nextNode)
{
    if (context == nullptr || nodeName == nullptr || nextNode == nullptr) {
        return false;
    }

    MaaStringBuffer* item = MaaStringBufferCreate();
    MaaStringListBuffer* list = MaaStringListBufferCreate();
    if (item == nullptr || list == nullptr) {
        if (item != nullptr) {
            MaaStringBufferDestroy(item);
        }
        if (list != nullptr) {
            MaaStringListBufferDestroy(list);
        }
        return false;
    }

    const bool ok = MaaStringBufferSet(item, nextNode) && MaaStringListBufferAppend(list, item) &&
                    MaaContextOverrideNext(context, nodeName, list);
    MaaStringListBufferDestroy(list);
    MaaStringBufferDestroy(item);
    return ok;
}

void ApplyDeltaToResult(recogrid::GridScanResult& result, const recogrid::GridDeltaResult& delta)
{
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
}

void UpdateSeenCells(const std::vector<recogrid::GridScanCell>& cells)
{
    for (const recogrid::GridScanCell& cell : cells) {
        g_seenCellKeys.insert({ cell.row, cell.col });
        g_maxSeenRow = std::max(g_maxSeenRow, cell.row);
    }
}

void UpdateCellQualities(const cv::Mat& image, const std::vector<recogrid::GridScanCell>& cells)
{
    int goldCells = 0;
    int purpleCells = 0;
    int unknownCells = 0;
    for (const recogrid::GridScanCell& cell : cells) {
        const QualityStats stats = ClassifyCellQuality(image, cell.screenCell);
        g_cellQualities[CellKey(cell)] = stats.quality;
        if (stats.quality == "flawless_gold") {
            goldCells++;
        }
        else if (stats.quality == "high_purity_purple") {
            purpleCells++;
        }
        else {
            unknownCells++;
        }
        LogInfo << "EssenceGridScan quality" << VAR(cell.row) << VAR(cell.col) << VAR(stats.quality)
                << VAR(stats.goldPixels) << VAR(stats.purplePixels) << VAR(stats.sampledPixels);
    }
    LogInfo << "EssenceGridScan quality summary" << VAR(goldCells) << VAR(purpleCells) << VAR(unknownCells);
}

void FinalizeEssenceCounts(recogrid::GridScanResult& result)
{
    recogrid::FinalizeCounts(result);
    result.sessionTotalCells = static_cast<int>(g_seenCellKeys.size());
    result.sessionRows = g_maxSeenRow + 1;
    if (result.sessionCols <= 0) {
        result.sessionCols = result.cols;
    }
}

recogrid::GridScanResult ScanCurrentViewport(
    const cv::Mat& image,
    const recogrid::GridScanOptions& options,
    const recogrid::GridClassifyOptions& classifyOptions)
{
    recogrid::GridScanResult result;
    const recogrid::GridRecognitionResult recognition = recogrid::RecognizeGrid(image, options.recognition);
    result.rows = static_cast<int>(recognition.grid.rows.size());
    result.cols = static_cast<int>(recognition.grid.cols.size());
    result.totalCells = result.rows * result.cols;
    result.sessionCols = result.cols;

    if (result.totalCells <= 0) {
        result.message = recognition.message.empty() ? "Grid detected no cells" : recognition.message;
        return result;
    }

    const recogrid::GridHashSnapshot currentSnapshot = recogrid::MakeGridHashSnapshot(
        result.rows,
        result.cols,
        std::vector<recogrid::Hash>(recognition.cellHashes));
    int currentViewportStartRow = g_viewportStartRow;

    if (options.incremental && g_hasViewportSnapshot && g_lastViewportSnapshot.cols == result.cols) {
        recogrid::GridDeltaResult delta = recogrid::ComputeGridDelta(
            g_lastViewportSnapshot,
            currentSnapshot,
            { options.matchDistanceThreshold, options.minMatchRatio });
        recogrid::AdjustLeadingPartialRowsForDelta(
            delta,
            recognition,
            options,
            image.size(),
            g_viewportStartRow,
            nullptr);
        ApplyDeltaToResult(result, delta);

        const double endRatio = std::clamp(options.endMinMatchRatio, 0.0, 1.0);
        if (delta.rowOffset == 0 && delta.matchRatio >= endRatio) {
            result.success = true;
            result.reachedEnd = true;
            result.message = "Grid reached end";
            FinalizeEssenceCounts(result);
            return result;
        }
        if (!delta.reliable || delta.rowOffset <= 0) {
            result.message = "Grid delta is not reliable";
            return result;
        }

        g_viewportStartRow += delta.rowOffset;
        currentViewportStartRow = g_viewportStartRow;
        g_lastViewportSnapshot = currentSnapshot;
    }
    else {
        g_viewportStartRow = 0;
        currentViewportStartRow = 0;
        g_lastViewportSnapshot = currentSnapshot;
        g_hasViewportSnapshot = true;
        result.hasProgress = true;
        result.newCellIndices = recogrid::NewCellIndicesForOffset(currentSnapshot, result.rows);
    }

    std::vector<recogrid::GridScanCell> cells = recogrid::MakeUnknownCells(
        currentViewportStartRow,
        result.rows,
        result.cols,
        recognition.grid.roi,
        recognition.grid.cells,
        options,
        options.recognition,
        image.size(),
        options.unknownTemplateId);
    result.totalCells = static_cast<int>(cells.size());

    const std::vector<std::size_t> occupiedIndices = recogrid::CellIndices(cells);
    const recogrid::GridClassificationResult classification = recogrid::ClassifyGridCells(
        recognition,
        g_templates,
        options.recognition,
        classifyOptions,
        image.size(),
        occupiedIndices);
    recogrid::ApplyClassifications(cells, classification, result.cols, currentViewportStartRow, options.unknownTemplateId);

    std::sort(cells.begin(), cells.end(), [](const recogrid::GridScanCell& lhs, const recogrid::GridScanCell& rhs) {
        if (lhs.row != rhs.row) {
            return lhs.row < rhs.row;
        }
        return lhs.col < rhs.col;
    });

    UpdateSeenCells(cells);
    UpdateCellQualities(image, cells);
    result.success = true;
    result.message = recognition.message.empty() ? "Grid scanned" : recognition.message;
    result.cells = std::move(cells);
    FinalizeEssenceCounts(result);
    return result;
}

void RebuildCurrentPageQueue(const recogrid::GridScanResult& result, const QualityFilter& filter)
{
    g_currentPageQueue.clear();
    g_currentPageQueueIndex = 0;
    for (const recogrid::GridScanCell& cell : result.cells) {
        if (!cell.visible || g_issuedCellKeys.find({ cell.row, cell.col }) != g_issuedCellKeys.end() ||
            !ShouldDispatchQuality(cell, filter)) {
            continue;
        }
        g_currentPageQueue.push_back(cell);
    }
}

std::optional<recogrid::GridScanCell> SelectNextQueuedCell()
{
    while (g_currentPageQueueIndex < g_currentPageQueue.size()) {
        recogrid::GridScanCell cell = g_currentPageQueue[g_currentPageQueueIndex++];
        if (!cell.visible || g_issuedCellKeys.find({ cell.row, cell.col }) != g_issuedCellKeys.end()) {
            continue;
        }
        return cell;
    }
    return std::nullopt;
}

} // namespace

MaaBool MAA_CALL EssenceGridAdvanceRecognitionRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    if (image == nullptr || MaaImageBufferIsEmpty(image)) {
        WriteError(out_detail, "Image buffer is empty");
        return MAA_FALSE;
    }

    try {
        EnsureLoaded();
        ResetSessionForNewTask(task_id);

        recogrid::GridScanOptions options;
        ApplyEssenceScanDefaults(options);

        recogrid::GridRecognitionRequest request =
            ParseEssenceRecognitionRequest(custom_recognition_param, options.recognition);
        if ((custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0) && roi != nullptr &&
            roi->width > 0 && roi->height > 0) {
            request = recogrid::ApplyRoiOverride(request, { roi->x, roi->y, roi->width, roi->height });
        }

        options.recognition = request.options;
        options.incremental = ReadBooleanOption(custom_recognition_param, "incremental", options.incremental);
        options.endMinMatchRatio =
            ReadDoubleOption(custom_recognition_param, "end_min_match_ratio", options.endMinMatchRatio);
        const QualityFilter qualityFilter = ParseQualityFilter(custom_recognition_param);

        std::optional<recogrid::GridScanCell> selected = SelectNextQueuedCell();
        recogrid::GridScanResult result = g_lastScanResult.value_or(recogrid::GridScanResult {});
        const char* nextNode = nullptr;

        if (!selected && !g_scanRequired) {
            g_pendingCell.reset();
            g_scanRequired = true;
            nextNode = kSwipeNextNode;
        }
        else if (!selected) {
            result = ScanCurrentViewport(to_mat(image), options, request.classify);
            g_lastScanResult = result;
            g_scanRequired = false;
            if (!result.success) {
                g_pendingCell.reset();
                WriteAdvanceDetail(out_detail, result, std::nullopt, qualityFilter);
                LogWarn << "EssenceGridScan scan miss" << VAR(result.message);
                return MAA_FALSE;
            }

            RebuildCurrentPageQueue(result, qualityFilter);
            selected = SelectNextQueuedCell();
            if (!selected) {
                g_pendingCell.reset();
                g_scanRequired = true;
                nextNode = result.reachedEnd ? kFinishNode : kSwipeNextNode;
            }
        }

        if (selected) {
            g_pendingCell = selected;
            g_issuedCellKeys.insert({ selected->row, selected->col });
            nextNode = kClickNextNode;
            if (out_box != nullptr) {
                *out_box = {
                    selected->screenCell.x,
                    selected->screenCell.y,
                    selected->screenCell.width,
                    selected->screenCell.height,
                };
            }
        }

        LogInfo << "EssenceGridScan advance" << VAR(nextNode) << VAR(result.sessionTotalCells)
                << VAR(g_issuedCellKeys.size()) << VAR(g_currentPageQueue.size()) << VAR(g_currentPageQueueIndex)
                << VAR(g_scanRequired) << VAR(result.reachedEnd) << VAR(result.hasProgress) << VAR(result.rowOffset)
                << VAR(result.matchRatio);
        if (!OverrideNext(context, node_name, nextNode)) {
            LogWarn << "EssenceGridScan override next failed" << VAR(nextNode);
        }
        WriteAdvanceDetail(out_detail, result, selected, qualityFilter);
        return MAA_TRUE;
    }
    catch (const std::exception& e) {
        g_pendingCell.reset();
        WriteError(out_detail, e.what());
        LogError << "EssenceGridAdvanceRecognition failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

MaaBool MAA_CALL EssenceGridPendingRecognitionRun(
    [[maybe_unused]] MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    [[maybe_unused]] const char* custom_recognition_param,
    [[maybe_unused]] const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    if (!g_pendingCell) {
        WritePendingDetail(out_detail, false, "No pending Essence grid cell");
        LogWarn << "EssenceGridPendingRecognition missing pending cell";
        return MAA_FALSE;
    }

    if (out_box != nullptr) {
        const cv::Rect& box = g_pendingCell->screenCell;
        *out_box = { box.x, box.y, box.width, box.height };
    }
    WritePendingDetail(out_detail, true, "Pending Essence grid cell");
    LogInfo << "EssenceGridScan pending" << VAR(g_pendingCell->cellIndex) << VAR(g_pendingCell->screenCell.x)
            << VAR(g_pendingCell->screenCell.y) << VAR(g_pendingCell->screenCell.width)
            << VAR(g_pendingCell->screenCell.height);
    return MAA_TRUE;
}

} // namespace essencegridscan
