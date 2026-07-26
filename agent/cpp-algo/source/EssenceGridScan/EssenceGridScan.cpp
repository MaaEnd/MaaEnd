#include "EssenceGridScan.h"

#include "../RecoGrid/RecoGridEngine.h"
#include "../utils.h"

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>

#include <meojson/json.hpp>

#include <algorithm>
#include <cmath>
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

constexpr const char* kDefaultEssenceGeneralTemplatePath = "resource/image/EssenceFilter/EssenceGeneral.png";
constexpr const char* kDefaultThumbDiscardTemplatePath = "resource/image/EssenceFilter/ThumbDiscard.png";
constexpr const char* kDefaultThumbLockTemplatePath = "resource/image/EssenceFilter/ThumbLock.png";
constexpr const char* kDefaultThumbLockPurpleTemplatePath = "resource/image/EssenceFilter/LockPurple.png";
constexpr const char* kSessionId = "EssenceGridScan";
constexpr const char* kClickNextNode = "EssenceGridClickPending";
constexpr const char* kSwipeNextNode = "EssenceGridSwipeNext";
constexpr const char* kFinishNode = "EssenceFilterFinish";

recogrid::RecoGridEngine g_engine;
MaaTaskId g_lastTaskId = MaaInvalidId;
std::set<std::pair<int, int>> g_issuedCellKeys;
std::set<std::pair<int, int>> g_seenCellKeys;
std::map<std::pair<int, int>, std::string> g_cellQualities;
std::map<std::pair<int, int>, std::string> g_cellThumbStates;
std::optional<recogrid::GridScanCell> g_pendingCell;
std::optional<recogrid::GridScanResult> g_lastScanResult;
std::vector<recogrid::GridScanCell> g_currentPageQueue;
std::size_t g_currentPageQueueIndex = 0;
bool g_scanRequired = true;
int g_maxSeenRow = -1;
cv::Mat g_thumbDiscardTemplate;
std::vector<cv::Mat> g_thumbLockTemplates;

struct EssenceScanDefaults
{
    cv::Rect roi;
    cv::Size normalizedSize;
    double rowThresholdRatio = 0.0;
    double colThresholdRatio = 0.0;
    int minRawSegmentLength = 0;
    double minKeptSegmentRatio = 0.0;
    int maxPhashDistance = 0;
    int maxRankedCandidates = 0;
    double minScore = 0.0;
    double hueWeight = 0.0;
    bool incremental = true;
    double endMinMatchRatio = 0.0;
};

struct EssenceTemplateConfig
{
    std::string essenceGeneralTemplatePath = kDefaultEssenceGeneralTemplatePath;
    std::string thumbDiscardTemplatePath = kDefaultThumbDiscardTemplatePath;
    std::vector<std::string> thumbLockTemplatePaths = {
        kDefaultThumbLockTemplatePath,
        kDefaultThumbLockPurpleTemplatePath,
    };

    bool operator==(const EssenceTemplateConfig&) const = default;
};

std::optional<EssenceTemplateConfig> g_loadedTemplateConfig;

const EssenceScanDefaults kEssenceScanDefaults {
    { 18, 72, 956, 570 }, { 1280, 720 }, 0.2, 0.4, 10, 0.9, 10, 0, 0.35, 0.4, true, 0.95,
};

struct HsvRange
{
    int hueMin = 0;
    int hueMax = 0;
    int saturationMin = 0;
    int valueMin = 0;
};

constexpr HsvRange kFlawlessGoldHsvRange { 16, 29, 71, 89 };
constexpr HsvRange kHighPurityPurpleHsvRange { 128, 158, 61, 71 };
constexpr int kQualitySampleHeightDivisor = 10;
constexpr int kMinQualityPixels = 80;
constexpr int kQualityDominanceRatio = 2;
constexpr int kThumbSearchWidthPercent = 20;
constexpr int kThumbSearchHeightPercent = 20;
constexpr double kThumbLockMatchThreshold = 0.7;
constexpr double kThumbDiscardMatchThreshold = 0.75;

std::filesystem::path ResolveEssenceImagePath(const std::string& configuredPath)
{
    if (configuredPath.empty()) {
        throw std::invalid_argument("Essence image path cannot be empty");
    }

    const std::filesystem::path configured(configuredPath);
    const std::filesystem::path executableDir = get_exe_dir();
    const std::vector<std::filesystem::path> candidates = {
        configured,
        std::filesystem::path("assets") / configured,
        std::filesystem::path("resource/image") / configured,
        std::filesystem::path("assets/resource/image") / configured,
        executableDir / configured,
        executableDir.parent_path() / configured,
        executableDir.parent_path() / "resource/image" / configured,
    };

    for (const std::filesystem::path& path : candidates) {
        std::error_code ec;
        if (std::filesystem::exists(path, ec) && std::filesystem::is_regular_file(path, ec)) {
            return path;
        }
    }
    throw std::runtime_error("Essence image not found: " + configuredPath);
}

void EnsureLoaded(const EssenceTemplateConfig& config)
{
    if (g_loadedTemplateConfig && *g_loadedTemplateConfig == config) {
        return;
    }

    const std::filesystem::path path = ResolveEssenceImagePath(config.essenceGeneralTemplatePath);
    cv::Mat image = MAA_NS::imread(path, cv::IMREAD_UNCHANGED);
    if (image.empty()) {
        throw std::runtime_error("Essence grid template image cannot be loaded: " + path.string());
    }

    cv::Mat thumbDiscardTemplate =
        MAA_NS::imread(ResolveEssenceImagePath(config.thumbDiscardTemplatePath), cv::IMREAD_UNCHANGED);
    std::vector<cv::Mat> thumbLockTemplates;
    thumbLockTemplates.reserve(config.thumbLockTemplatePaths.size());
    for (const std::string& templatePath : config.thumbLockTemplatePaths) {
        thumbLockTemplates.emplace_back(MAA_NS::imread(ResolveEssenceImagePath(templatePath), cv::IMREAD_UNCHANGED));
    }
    const bool lockTemplateMissing =
        thumbLockTemplates.empty()
        || std::any_of(thumbLockTemplates.begin(), thumbLockTemplates.end(), [](const cv::Mat& templ) { return templ.empty(); });
    if (thumbDiscardTemplate.empty() || lockTemplateMissing) {
        throw std::runtime_error("Essence thumb templates cannot be loaded");
    }

    g_engine.SetTemplates({ { "essence_general", std::move(image) } });
    g_thumbDiscardTemplate = std::move(thumbDiscardTemplate);
    g_thumbLockTemplates = std::move(thumbLockTemplates);
    g_loadedTemplateConfig = config;
    g_lastTaskId = MaaInvalidId;
    LogInfo << "EssenceGridScan templates loaded" << VAR(config.essenceGeneralTemplatePath)
            << VAR(config.thumbDiscardTemplatePath) << VAR(config.thumbLockTemplatePaths);
}

void ApplyEssenceScanDefaults(recogrid::GridScanOptions& options)
{
    options.recognition.detect.roi = kEssenceScanDefaults.roi;
    options.recognition.detect.normalizedSize = kEssenceScanDefaults.normalizedSize;
    options.recognition.detect.rowThresholdRatio = kEssenceScanDefaults.rowThresholdRatio;
    options.recognition.detect.colThresholdRatio = kEssenceScanDefaults.colThresholdRatio;
    options.recognition.detect.minRawSegmentLength = kEssenceScanDefaults.minRawSegmentLength;
    options.recognition.detect.minKeptSegmentRatio = kEssenceScanDefaults.minKeptSegmentRatio;
    options.recognition.maxPhashDistance = kEssenceScanDefaults.maxPhashDistance;
    options.recognition.maxRankedCandidates = kEssenceScanDefaults.maxRankedCandidates;
    options.recognition.minScore = kEssenceScanDefaults.minScore;
    options.recognition.hueWeight = kEssenceScanDefaults.hueWeight;
    options.incremental = kEssenceScanDefaults.incremental;
    options.endMinMatchRatio = kEssenceScanDefaults.endMinMatchRatio;
}

json::object ReadEssenceGridConfig(MaaContext* context, const char* nodeName, const char* fallbackRaw)
{
    json::object config;
    if (fallbackRaw != nullptr && std::strlen(fallbackRaw) > 0) {
        const std::string fallback(fallbackRaw);
        const std::size_t first = fallback.find_first_not_of(" \t\r\n");
        const std::size_t last = fallback.find_last_not_of(" \t\r\n");
        const bool hasValue = first != std::string::npos;
        const bool isNull = hasValue && fallback.substr(first, last - first + 1) == "null";
        if (hasValue && !isNull) {
            const auto parsed = json::parse(fallback);
            if (!parsed) {
                throw std::invalid_argument("custom_recognition_param must be valid JSON");
            }
            if (!parsed->is_object()) {
                throw std::invalid_argument("custom_recognition_param must be a JSON object");
            }
            config = parsed->as_object();
        }
    }

    if (context == nullptr || nodeName == nullptr) {
        return config;
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, nodeName, buffer.Get())) {
        LogWarn << "EssenceGridScan node data failed" << VAR(nodeName);
        return config;
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || std::strlen(raw) == 0) {
        LogWarn << "EssenceGridScan node data empty" << VAR(nodeName);
        return config;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogWarn << "EssenceGridScan node JSON invalid" << VAR(nodeName);
        return config;
    }

    const json::object& node = parsed->as_object();
    if (!node.contains("attach") || !node.at("attach").is_object()) {
        return config;
    }

    for (const auto& [key, value] : node.at("attach").as_object()) {
        config[key] = value;
    }
    return config;
}

bool ReadBooleanOption(const json::object& object, const char* key, bool defaultValue)
{
    if (!object.contains(key) || !object.at(key).is_boolean()) {
        return defaultValue;
    }
    return object.at(key).as_boolean();
}

double ReadDoubleOption(const json::object& object, const char* key, double defaultValue)
{
    if (!object.contains(key) || !object.at(key).is_number()) {
        return defaultValue;
    }
    return object.at(key).as_double();
}

std::string ReadStringOption(const json::object& object, const char* key, std::string defaultValue)
{
    if (!object.contains(key) || !object.at(key).is_string()) {
        return defaultValue;
    }
    return object.at(key).as_string();
}

std::vector<std::string>
    ReadStringArrayOption(const json::object& object, const char* key, std::vector<std::string> defaultValue)
{
    if (!object.contains(key) || !object.at(key).is_array()) {
        return defaultValue;
    }

    std::vector<std::string> values;
    for (const json::value& item : object.at(key).as_array()) {
        if (!item.is_string()) {
            return defaultValue;
        }
        values.push_back(item.as_string());
    }
    return values.empty() ? defaultValue : values;
}

recogrid::GridRecognitionRequest
    ParseEssenceRecognitionRequest(const json::object& config, const recogrid::GridRecognitionOptions& defaults)
{
    recogrid::GridRecognitionRequest request;
    request.options = defaults;
    request.classify.maxPhashDistance = defaults.maxPhashDistance;
    request.classify.minScore = defaults.minScore;
    request.classify.hueWeight = defaults.hueWeight;
    request.classify.maxRankedCandidates = defaults.maxRankedCandidates;

    if (!request.from_json(json::value(config))) {
        throw std::invalid_argument("Essence grid config cannot be converted to GridRecognitionRequest");
    }
    return request;
}

EssenceTemplateConfig
    ParseEssenceTemplateConfig(const json::object& config, const recogrid::GridRecognitionRequest& recognitionRequest)
{
    EssenceTemplateConfig output;
    if (!recognitionRequest.templatePath.empty()) {
        output.essenceGeneralTemplatePath = recognitionRequest.templatePath;
    }
    output.thumbDiscardTemplatePath =
        ReadStringOption(config, "thumb_discard_template_path", output.thumbDiscardTemplatePath);
    output.thumbLockTemplatePaths =
        ReadStringArrayOption(config, "thumb_lock_template_paths", std::move(output.thumbLockTemplatePaths));
    return output;
}

void ResetSessionForNewTask(MaaTaskId taskId)
{
    if (taskId == MaaInvalidId || taskId == g_lastTaskId) {
        return;
    }
    g_issuedCellKeys.clear();
    g_seenCellKeys.clear();
    g_cellQualities.clear();
    g_cellThumbStates.clear();
    g_pendingCell.reset();
    g_lastScanResult.reset();
    g_currentPageQueue.clear();
    g_currentPageQueueIndex = 0;
    g_scanRequired = true;
    g_engine.ResetSession(kSessionId);
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

std::string CellThumbState(const recogrid::GridScanCell& cell)
{
    const auto iter = g_cellThumbStates.find(CellKey(cell));
    if (iter == g_cellThumbStates.end()) {
        return "none";
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
    bool skipThumbLock = true;
    bool skipThumbDiscard = true;
};

bool IsInHsvRange(const cv::Vec3b& hsv, const HsvRange& range)
{
    return hsv[0] >= range.hueMin && hsv[0] <= range.hueMax && hsv[1] >= range.saturationMin && hsv[2] >= range.valueMin;
}

bool IsGoldPixel(const cv::Vec3b& hsv)
{
    return IsInHsvRange(hsv, kFlawlessGoldHsvRange);
}

bool IsPurplePixel(const cv::Vec3b& hsv)
{
    return IsInHsvRange(hsv, kHighPurityPurpleHsvRange);
}

QualityStats ClassifyCellQuality(const cv::Mat& image, const cv::Rect& screenCell)
{
    QualityStats stats;
    const cv::Rect imageBounds(0, 0, image.cols, image.rows);
    const cv::Rect cell = screenCell & imageBounds;
    if (cell.empty()) {
        return stats;
    }

    const int sampleHeight = std::max(1, cell.height / kQualitySampleHeightDivisor);
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

    if (stats.goldPixels >= kMinQualityPixels && stats.goldPixels >= stats.purplePixels * kQualityDominanceRatio) {
        stats.quality = "flawless_gold";
    }
    else if (stats.purplePixels >= kMinQualityPixels && stats.purplePixels >= stats.goldPixels * kQualityDominanceRatio) {
        stats.quality = "high_purity_purple";
    }
    return stats;
}

cv::Mat ToGrayForTemplate(const cv::Mat& image)
{
    if (image.empty()) {
        return {};
    }
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
    return gray;
}

struct ThumbMatchScore
{
    double score = 0.0;
    cv::Point location;
};

struct ThumbDetection
{
    std::string state = "none";
    cv::Rect cell;
    cv::Rect search;
    ThumbMatchScore lock;
    ThumbMatchScore discard;
};

ThumbMatchScore MatchTemplateScore(const cv::Mat& search, const cv::Mat& templ)
{
    if (search.empty() || templ.empty() || search.cols < templ.cols || search.rows < templ.rows) {
        return {};
    }

    const cv::Mat searchGray = ToGrayForTemplate(search);
    const cv::Mat templateGray = ToGrayForTemplate(templ);
    if (searchGray.empty() || templateGray.empty()) {
        return {};
    }

    cv::Mat result;
    cv::matchTemplate(searchGray, templateGray, result, cv::TM_CCOEFF_NORMED);

    ThumbMatchScore score;
    cv::minMaxLoc(result, nullptr, &score.score, nullptr, &score.location);
    if (!std::isfinite(score.score)) {
        score.score = 0.0;
        score.location = {};
    }
    return score;
}

ThumbDetection DetectCellThumbState(const cv::Mat& image, const cv::Rect& screenCell)
{
    ThumbDetection detection;
    const cv::Rect imageBounds(0, 0, image.cols, image.rows);
    const cv::Rect cell = screenCell & imageBounds;
    detection.cell = cell;
    if (cell.empty()) {
        return detection;
    }

    const int searchWidth = std::max(1, cell.width * kThumbSearchWidthPercent / 100);
    const int searchHeight = std::max(1, cell.height * kThumbSearchHeightPercent / 100);
    const cv::Rect searchRect(cell.x, cell.y + cell.height - searchHeight, searchWidth, searchHeight);
    detection.search = searchRect & imageBounds;
    const cv::Mat search = image(detection.search);

    for (std::size_t index = 0; index < g_thumbLockTemplates.size(); ++index) {
        const cv::Mat& lockTemplate = g_thumbLockTemplates[index];
        const ThumbMatchScore lockScore = MatchTemplateScore(search, lockTemplate);
        if (lockScore.score > detection.lock.score) {
            detection.lock = lockScore;
        }
    }
    detection.discard = MatchTemplateScore(search, g_thumbDiscardTemplate);
    if (detection.discard.score >= kThumbDiscardMatchThreshold) {
        detection.state = "discard";
        return detection;
    }
    if (detection.lock.score >= kThumbLockMatchThreshold) {
        detection.state = "lock";
        return detection;
    }
    return detection;
}

QualityFilter ParseQualityFilter(const json::object& object)
{
    QualityFilter filter;
    const bool hasFlawless = object.contains("flawless_essence") && object.at("flawless_essence").is_boolean();
    const bool hasPure = object.contains("pure_essence") && object.at("pure_essence").is_boolean();
    filter.hasExplicitSelection = hasFlawless || hasPure;
    if (hasFlawless) {
        filter.flawlessEssence = object.at("flawless_essence").as_boolean();
    }
    if (hasPure) {
        filter.pureEssence = object.at("pure_essence").as_boolean();
    }
    if (object.contains("skip_thumb_lock") && object.at("skip_thumb_lock").is_boolean()) {
        filter.skipThumbLock = object.at("skip_thumb_lock").as_boolean();
    }
    if (object.contains("skip_thumb_discard") && object.at("skip_thumb_discard").is_boolean()) {
        filter.skipThumbDiscard = object.at("skip_thumb_discard").as_boolean();
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

bool ShouldDispatchThumbState(const recogrid::GridScanCell& cell, const QualityFilter& filter)
{
    const std::string state = CellThumbState(cell);
    if (state == "lock") {
        return !filter.skipThumbLock;
    }
    if (state == "discard") {
        return !filter.skipThumbDiscard;
    }
    return true;
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
    detail["dispatchable_grid"] = static_cast<int>(result.dispatchableCells.size());
    detail["rows"] = result.sessionRows;
    detail["cols"] = result.sessionCols;
    detail["detected_rows"] = result.detectedRows;
    detail["detected_cols"] = result.detectedCols;
    detail["detected_grid"] = result.detectedTotalCells;
    detail["visible_candidates"] = visibleCandidates;
    detail["issued_cells"] = static_cast<int>(g_issuedCellKeys.size());
    detail["queue_remaining"] = remainingQueueCells;
    detail["scan_required"] = g_scanRequired;
    detail["filter_flawless_essence"] = filter.flawlessEssence;
    detail["filter_pure_essence"] = filter.pureEssence;
    detail["skip_thumb_lock"] = filter.skipThumbLock;
    detail["skip_thumb_discard"] = filter.skipThumbDiscard;
    detail["filter_explicit"] = filter.hasExplicitSelection;
    detail["selected_cell_index"] = selected ? static_cast<int>(selected->cellIndex) : -1;
    detail["selected_row"] = selected ? selected->row : -1;
    detail["selected_col"] = selected ? selected->col : -1;
    detail["selected_quality"] = selected ? CellQuality(*selected) : "unknown";
    detail["selected_thumb_state"] = selected ? CellThumbState(*selected) : "none";
    detail["selected_box"] = selected ? ToJsonRect(selected->screenCell) : json::object {};
    detail["reached_end"] = result.reachedEnd;
    detail["has_progress"] = result.hasProgress;
    detail["row_offset"] = result.rowOffset;
    detail["raw_alignment_offset"] = result.rawAlignmentOffset;
    detail["adjusted_alignment_offset"] = result.adjustedAlignmentOffset;
    detail["support_rows"] = result.supportRows;
    detail["alignment_status"] = result.alignmentStatus;
    detail["delta_reliable"] = result.deltaReliable;
    detail["matched_cells"] = result.matchedCells;
    detail["compared_cells"] = result.comparedCells;
    detail["average_distance"] = result.averageDistance;
    detail["delta_score"] = result.deltaScore;
    detail["match_ratio"] = result.matchRatio;
    detail["previous_viewport_start_row"] = result.previousViewportStartRow;
    detail["current_viewport_start_row"] = result.currentViewportStartRow;
    detail["fallback_used"] = result.fallbackUsed;
    detail["fallback_streak"] = result.fallbackStreak;
    detail["end_confirmations"] = result.endConfirmations;
    detail["unresolved_reason"] = result.unresolvedReason;

    json::object retainedQualityCounts;
    retainedQualityCounts["flawless_gold"] = 0;
    retainedQualityCounts["high_purity_purple"] = 0;
    retainedQualityCounts["unknown"] = 0;
    for (const recogrid::GridScanCell& cell : result.cells) {
        if (!cell.visible) {
            continue;
        }
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

    json::object thumbCounts;
    thumbCounts["lock"] = 0;
    thumbCounts["discard"] = 0;
    thumbCounts["none"] = 0;
    for (const recogrid::GridScanCell& cell : result.dispatchableCells) {
        const std::string state = CellThumbState(cell);
        if (thumbCounts.contains(state) && thumbCounts[state].is_number()) {
            thumbCounts[state] = thumbCounts[state].as_integer() + 1;
        }
        else {
            thumbCounts["none"] = thumbCounts["none"].as_integer() + 1;
        }
    }
    detail["thumb_counts"] = std::move(thumbCounts);

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

    const bool ok =
        MaaStringBufferSet(item, nextNode) && MaaStringListBufferAppend(list, item) && MaaContextOverrideNext(context, nodeName, list);
    MaaStringListBufferDestroy(list);
    MaaStringBufferDestroy(item);
    return ok;
}

void UpdateSeenCells(const std::vector<recogrid::GridScanCell>& cells)
{
    for (const recogrid::GridScanCell& cell : cells) {
        if (!cell.visible) {
            continue;
        }
        g_seenCellKeys.insert({ cell.row, cell.col });
        g_maxSeenRow = std::max(g_maxSeenRow, cell.row);
    }
}

void UpdateCellQualities(const cv::Mat& image, const std::vector<recogrid::GridScanCell>& cells)
{
    for (const recogrid::GridScanCell& cell : cells) {
        if (!cell.visible) {
            continue;
        }
        const QualityStats stats = ClassifyCellQuality(image, cell.screenCell);
        g_cellQualities[CellKey(cell)] = stats.quality;
    }
}

void UpdateCellThumbStates(const cv::Mat& image, const std::vector<recogrid::GridScanCell>& cells)
{
    for (const recogrid::GridScanCell& cell : cells) {
        if (!cell.visible) {
            continue;
        }
        const ThumbDetection detection = DetectCellThumbState(image, cell.screenCell);
        g_cellThumbStates[CellKey(cell)] = detection.state;
    }
}

recogrid::GridScanResult
    ScanWithRecoGridEngine([[maybe_unused]] MaaTaskId taskId, const cv::Mat& image, const recogrid::GridScanOptions& options)
{
    recogrid::GridScanResult result = g_engine.Scan(kSessionId, image, options);
    if (!result.success) {
        return result;
    }
    UpdateSeenCells(result.dispatchableCells);
    UpdateCellQualities(image, result.dispatchableCells);
    UpdateCellThumbStates(image, result.dispatchableCells);
    return result;
}

void RebuildCurrentPageQueue(const recogrid::GridScanResult& result, const QualityFilter& filter)
{
    g_currentPageQueue.clear();
    g_currentPageQueueIndex = 0;
    for (const recogrid::GridScanCell& cell : result.dispatchableCells) {
        if (!cell.visible) {
            continue;
        }
        if (g_issuedCellKeys.find({ cell.row, cell.col }) != g_issuedCellKeys.end()) {
            continue;
        }
        if (!ShouldDispatchQuality(cell, filter)) {
            continue;
        }
        if (!ShouldDispatchThumbState(cell, filter)) {
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
        recogrid::GridScanOptions options;
        ApplyEssenceScanDefaults(options);

        const json::object config = ReadEssenceGridConfig(context, node_name, custom_recognition_param);
        recogrid::GridRecognitionRequest request = ParseEssenceRecognitionRequest(config, options.recognition);
        if (!config.contains("roi") && roi != nullptr && roi->width > 0 && roi->height > 0) {
            request = recogrid::ApplyRoiOverride(request, { roi->x, roi->y, roi->width, roi->height });
        }

        EnsureLoaded(ParseEssenceTemplateConfig(config, request));
        ResetSessionForNewTask(task_id);

        options.recognition = request.options;
        options.incremental = ReadBooleanOption(config, "incremental", options.incremental);
        options.endMinMatchRatio = ReadDoubleOption(config, "end_min_match_ratio", options.endMinMatchRatio);
        const QualityFilter qualityFilter = ParseQualityFilter(config);

        std::optional<recogrid::GridScanCell> selected = SelectNextQueuedCell();
        recogrid::GridScanResult result = g_lastScanResult.value_or(recogrid::GridScanResult {});
        const char* nextNode = nullptr;
        cv::Mat scannedImage;

        if (!selected && !g_scanRequired) {
            g_pendingCell.reset();
            g_scanRequired = true;
            nextNode = result.reachedEnd ? kFinishNode : kSwipeNextNode;
        }
        else if (!selected) {
            scannedImage = to_mat(image);
            result = ScanWithRecoGridEngine(task_id, scannedImage, options);
            g_lastScanResult = result;
            if (!result.success) {
                g_scanRequired = true;
                g_pendingCell.reset();
                WriteAdvanceDetail(out_detail, result, std::nullopt, qualityFilter);
                LogWarn << "EssenceGridScan scan miss" << VAR(result.message) << VAR(result.alignmentStatus)
                        << VAR(result.rawAlignmentOffset) << VAR(result.adjustedAlignmentOffset) << VAR(result.supportRows)
                        << VAR(result.matchRatio) << VAR(result.fallbackStreak) << VAR(result.unresolvedReason);
                return MAA_FALSE;
            }

            g_scanRequired = false;
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

        LogInfo << "EssenceGridScan advance" << VAR(nextNode) << VAR(result.sessionTotalCells) << VAR(g_issuedCellKeys.size())
                << VAR(g_currentPageQueue.size()) << VAR(g_currentPageQueueIndex) << VAR(g_scanRequired) << VAR(result.reachedEnd)
                << VAR(result.hasProgress) << VAR(result.rowOffset) << VAR(result.rawAlignmentOffset)
                << VAR(result.adjustedAlignmentOffset) << VAR(result.supportRows) << VAR(result.alignmentStatus)
                << VAR(result.matchRatio) << VAR(result.fallbackUsed) << VAR(result.fallbackStreak);
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
            << VAR(g_pendingCell->screenCell.y) << VAR(g_pendingCell->screenCell.width) << VAR(g_pendingCell->screenCell.height);
    return MAA_TRUE;
}

} // namespace essencegridscan
