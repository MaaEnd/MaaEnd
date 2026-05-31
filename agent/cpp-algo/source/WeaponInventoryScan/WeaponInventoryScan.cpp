#include "WeaponInventoryScan.h"

#include "../RecoGrid/CellMask.h"
#include "../RecoGrid/GridDetector.h"
#include "../RecoGrid/GridMatcher.h"
#include "../RecoGrid/PHashFilter.h"
#include "../utils.h"

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

#include <opencv2/imgproc.hpp>

#include <meojson/json.hpp>

#include <algorithm>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <map>
#include <memory>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include <type_traits>
#include <vector>

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace fs = std::filesystem;

namespace weaponinventoryscan
{
namespace
{

constexpr const char* kDefaultOutputPath = "debug/record/WeaponInventoryScan.json";
constexpr int kDefaultSameCellDistance = 4;
constexpr double kDefaultUnchangedRatio = 0.9;
constexpr int kDefaultMaxPhashDistance = 10;
constexpr int kDefaultMaxReturnedMatches = 3;
constexpr double kDefaultMinScore = 0.80;
constexpr double kTemplateScoreWeight = 0.6;
constexpr double kHueScoreWeight = 0.4;

struct RectOutput
{
    int x = 0;
    int y = 0;
    int width = 0;
    int height = 0;

    MEO_JSONIZATION(x, y, width, height)
};

struct WeaponOutput
{
    std::string weaponId;
    int cellIndex = 0;
    double score = 0.0;
    double templateScore = 0.0;
    double hueScore = 0.0;
    int phashDistance = 0;
    std::vector<int> screenCell;

    MEO_JSONIZATION(
        MEO_KEY("weapon_id") weaponId,
        MEO_KEY("cell_index") cellIndex,
        score,
        MEO_KEY("template_score") templateScore,
        MEO_KEY("hue_score") hueScore,
        MEO_KEY("phash_distance") phashDistance,
        MEO_KEY("screen_cell") screenCell)
};

struct RecognitionDetail
{
    int schemaVersion = 1;
    int status = 0;
    std::string message = "ok";
    int rows = 0;
    int cols = 0;
    int cellCount = 0;
    std::vector<std::string> cellHashes;
    std::vector<WeaponOutput> weapons;

    MEO_JSONIZATION(
        MEO_KEY("schema_version") schemaVersion,
        status,
        message,
        rows,
        cols,
        MEO_KEY("cell_count") cellCount,
        MEO_KEY("cell_hashes") cellHashes,
        weapons)
};

struct ScanOutput
{
    int schemaVersion = 1;
    int weaponCount = 0;
    int weaponTypes = 0;
    std::map<std::string, int> weapons;

    MEO_JSONIZATION(
        MEO_KEY("schema_version") schemaVersion,
        MEO_KEY("weapon_count") weaponCount,
        MEO_KEY("weapon_types") weaponTypes,
        weapons)
};

struct RecognitionOptions
{
    recogrid::GridDetectOptions detect;
    recogrid::CellMaskRatios mask;
    int maxPhashDistance = kDefaultMaxPhashDistance;
    int maxReturnedMatches = kDefaultMaxReturnedMatches;
    double minScore = kDefaultMinScore;
    std::vector<std::string> templatePaths;
};

struct TemplateEntry
{
    std::string weaponId;
    fs::path path;
    cv::Mat image;
};

struct ScanState
{
    RecognitionOptions options;
    std::vector<TemplateEntry> templates;
    std::vector<recogrid::Hash> lastHashes;
    std::map<std::string, int> weaponCounts;
    int pageCount = 0;
    int totalCellsSeen = 0;
    int totalRecognizedSlots = 0;
    int totalCountedSlots = 0;
    int bottomSkippedSlots = 0;
    bool stop = false;
    std::string outputPath = kDefaultOutputPath;
    int sameCellDistance = kDefaultSameCellDistance;
    double unchangedRatio = kDefaultUnchangedRatio;
};

std::map<MaaTasker*, std::shared_ptr<ScanState>> g_states;
std::shared_ptr<ScanState> g_currentState;

template <typename T>
void ReadField(const json::object& object, const char* key, T& out)
{
    if (!object.contains(key)) {
        return;
    }

    const json::value& value = object.at(key);
    if constexpr (std::is_same_v<T, bool>) {
        if (value.is_boolean()) {
            out = value.as_boolean();
        }
    }
    else if constexpr (std::is_same_v<T, int>) {
        if (value.is_number()) {
            out = value.as_integer();
        }
    }
    else if constexpr (std::is_same_v<T, double>) {
        if (value.is_number()) {
            out = value.as_double();
        }
    }
    else if constexpr (std::is_same_v<T, std::string>) {
        if (value.is_string()) {
            out = value.as_string();
        }
    }
}

std::vector<int> ReadIntArray(const json::object& object, const char* key)
{
    if (!object.contains(key) || !object.at(key).is_array()) {
        return {};
    }

    std::vector<int> values;
    for (const auto& item : object.at(key).as_array()) {
        if (!item.is_number()) {
            return {};
        }
        values.push_back(item.as_integer());
    }
    return values;
}

std::vector<std::string> ReadStringArray(const json::object& object, const char* key)
{
    if (!object.contains(key) || !object.at(key).is_array()) {
        return {};
    }

    std::vector<std::string> values;
    for (const auto& item : object.at(key).as_array()) {
        if (!item.is_string()) {
            return {};
        }
        values.push_back(item.as_string());
    }
    return values;
}

bool ApplyRect(const std::vector<int>& values, cv::Rect& rect)
{
    if (values.size() != 4 || values[2] <= 0 || values[3] <= 0) {
        return false;
    }
    rect = { values[0], values[1], values[2], values[3] };
    return true;
}

bool ApplySize(const std::vector<int>& values, cv::Size& size)
{
    if (values.size() != 2 || values[0] <= 0 || values[1] <= 0) {
        return false;
    }
    size = { values[0], values[1] };
    return true;
}

void ReadMaskField(const json::object& object, const char* key, recogrid::CellMaskRatios& out)
{
    if (!object.contains(key) || !object.at(key).is_object()) {
        return;
    }

    const json::object& mask = object.at(key).as_object();
    ReadField(mask, "left_header_width", out.leftHeaderWidth);
    ReadField(mask, "left_header_height", out.leftHeaderHeight);
    ReadField(mask, "right_header_width", out.rightHeaderWidth);
    ReadField(mask, "right_header_height", out.rightHeaderHeight);
    ReadField(mask, "bottom_height", out.bottomHeight);
}

RecognitionOptions ParseRecognitionOptions(const char* raw)
{
    RecognitionOptions options;
    options.templatePaths = {
        "data/WeaponIcon/iconbig",
        "assets/data/WeaponIcon/iconbig",
    };

    if (raw == nullptr || std::strlen(raw) == 0) {
        return options;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        throw std::invalid_argument("custom param must be a JSON object");
    }

    const json::object& object = parsed->as_object();
    if (object.contains("options") && object.at("options").is_object()) {
        const json::object& nested = object.at("options").as_object();
        ApplyRect(ReadIntArray(nested, "roi"), options.detect.roi);
        ApplySize(ReadIntArray(nested, "normalized_size"), options.detect.normalizedSize);
        ReadField(nested, "row_threshold_ratio", options.detect.rowThresholdRatio);
        ReadField(nested, "col_threshold_ratio", options.detect.colThresholdRatio);
        ReadField(nested, "min_raw_segment_length", options.detect.minRawSegmentLength);
        ReadField(nested, "min_kept_segment_ratio", options.detect.minKeptSegmentRatio);
        ReadField(nested, "max_phash_distance", options.maxPhashDistance);
        ReadField(nested, "max_returned_matches", options.maxReturnedMatches);
        ReadField(nested, "min_score", options.minScore);
        ReadMaskField(nested, "mask_ratios", options.mask);
        ReadMaskField(nested, "mask", options.mask);
    }

    ApplyRect(ReadIntArray(object, "roi"), options.detect.roi);
    ApplySize(ReadIntArray(object, "normalized_size"), options.detect.normalizedSize);
    ReadField(object, "row_threshold_ratio", options.detect.rowThresholdRatio);
    ReadField(object, "col_threshold_ratio", options.detect.colThresholdRatio);
    ReadField(object, "min_raw_segment_length", options.detect.minRawSegmentLength);
    ReadField(object, "min_kept_segment_ratio", options.detect.minKeptSegmentRatio);
    ReadField(object, "max_phash_distance", options.maxPhashDistance);
    ReadField(object, "max_returned_matches", options.maxReturnedMatches);
    ReadField(object, "min_score", options.minScore);
    ReadMaskField(object, "mask_ratios", options.mask);
    ReadMaskField(object, "mask", options.mask);

    std::string singlePath;
    ReadField(object, "template_path", singlePath);
    std::vector<std::string> templatePaths = ReadStringArray(object, "template_paths");
    if (!singlePath.empty()) {
        templatePaths.insert(templatePaths.begin(), singlePath);
    }
    if (!templatePaths.empty()) {
        options.templatePaths = std::move(templatePaths);
    }

    options.detect.rowThresholdRatio = std::clamp(options.detect.rowThresholdRatio, 0.0, 1.0);
    options.detect.colThresholdRatio = std::clamp(options.detect.colThresholdRatio, 0.0, 1.0);
    options.detect.minRawSegmentLength = std::max(1, options.detect.minRawSegmentLength);
    options.detect.minKeptSegmentRatio = std::clamp(options.detect.minKeptSegmentRatio, 0.0, 1.0);
    options.maxPhashDistance = std::max(0, options.maxPhashDistance);
    options.maxReturnedMatches = std::max(1, options.maxReturnedMatches);
    options.minScore = std::clamp(options.minScore, 0.0, 1.0);
    return options;
}

fs::path ResolvePath(const std::string& configured)
{
    const fs::path path(configured);
    if (path.is_absolute() && fs::exists(path)) {
        return path;
    }

    std::error_code ec;
    const fs::path cwd = fs::current_path(ec);
    if (ec || cwd.empty()) {
        return path;
    }

    for (const fs::path& base : { cwd, cwd / "assets" }) {
        const fs::path candidate = base / path;
        if (fs::exists(candidate)) {
            return candidate;
        }
    }
    return path;
}

std::vector<TemplateEntry> LoadTemplateIndex(const RecognitionOptions& options)
{
    std::vector<TemplateEntry> templates;
    std::set<std::string> seen;

    for (const std::string& configured : options.templatePaths) {
        const fs::path dir = ResolvePath(configured);
        std::error_code ec;
        if (!fs::exists(dir, ec) || !fs::is_directory(dir, ec)) {
            continue;
        }

        for (const fs::directory_entry& entry : fs::directory_iterator(dir, ec)) {
            if (ec || !entry.is_regular_file()) {
                continue;
            }

            const fs::path path = entry.path();
            if (path.extension() != ".png") {
                continue;
            }

            const std::string id = path.stem().string();
            if (id.empty() || seen.contains(id)) {
                continue;
            }

            cv::Mat image = MAA_NS::imread(path, cv::IMREAD_UNCHANGED);
            if (image.empty()) {
                LogWarn << "WeaponInventoryScan: skip unreadable template" << VAR(path.string());
                continue;
            }

            seen.insert(id);
            templates.push_back({ id, path, std::move(image) });
        }
    }

    std::sort(templates.begin(), templates.end(), [](const TemplateEntry& lhs, const TemplateEntry& rhs) {
        return lhs.weaponId < rhs.weaponId;
    });
    return templates;
}

ScanState* GetState(MaaContext* context)
{
    if (context == nullptr) {
        return g_currentState.get();
    }
    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        return g_currentState.get();
    }
    auto it = g_states.find(tasker);
    if (it == g_states.end()) {
        return g_currentState.get();
    }
    return it->second.get();
}

void SetState(MaaTasker* tasker, std::shared_ptr<ScanState> state)
{
    g_currentState = std::move(state);
    if (tasker != nullptr && g_currentState != nullptr) {
        g_states[tasker] = g_currentState;
    }
}

std::shared_ptr<ScanState> TakeState(MaaContext* context)
{
    if (context != nullptr) {
        MaaTasker* tasker = MaaContextGetTasker(context);
        auto it = g_states.find(tasker);
        if (it != g_states.end()) {
            std::shared_ptr<ScanState> state = it->second;
            g_states.erase(it);
            if (g_currentState == state) {
                g_currentState.reset();
            }
            return state;
        }
    }

    std::shared_ptr<ScanState> state = g_currentState;
    if (state != nullptr) {
        for (auto it = g_states.begin(); it != g_states.end();) {
            if (it->second == state) {
                it = g_states.erase(it);
            }
            else {
                ++it;
            }
        }
        g_currentState.reset();
    }
    return state;
}

std::string HashToHex(recogrid::Hash hash)
{
    std::ostringstream stream;
    stream << std::hex << std::setw(16) << std::setfill('0') << static_cast<std::uint64_t>(hash);
    return stream.str();
}

std::vector<int> RectToArray(const cv::Rect& rect)
{
    return { rect.x, rect.y, rect.width, rect.height };
}

cv::Rect ScaleRect(const cv::Rect& rect, cv::Size fromSize, cv::Size toSize)
{
    if (rect.empty() || fromSize.width <= 0 || fromSize.height <= 0 || toSize.width <= 0 || toSize.height <= 0) {
        return {};
    }

    const double scaleX = static_cast<double>(toSize.width) / static_cast<double>(fromSize.width);
    const double scaleY = static_cast<double>(toSize.height) / static_cast<double>(fromSize.height);
    const int x = static_cast<int>(std::lround(rect.x * scaleX));
    const int y = static_cast<int>(std::lround(rect.y * scaleY));
    const int right = static_cast<int>(std::lround((rect.x + rect.width) * scaleX));
    const int bottom = static_cast<int>(std::lround((rect.y + rect.height) * scaleY));
    return { x, y, std::max(1, right - x), std::max(1, bottom - y) };
}

cv::Rect CellToScreen(const cv::Rect& cell, const recogrid::GridDetectOptions& options, cv::Size imageSize)
{
    return ScaleRect({ cell.x + options.roi.x, cell.y + options.roi.y, cell.width, cell.height }, options.normalizedSize, imageSize);
}

cv::Rect ClampRect(const cv::Rect& rect, const cv::Size& bounds)
{
    return rect & cv::Rect(0, 0, bounds.width, bounds.height);
}

cv::Mat ToBgr(const cv::Mat& image)
{
    if (image.channels() == 3) {
        return image;
    }
    cv::Mat bgr;
    if (image.channels() == 4) {
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
    }
    else if (image.channels() == 1) {
        cv::cvtColor(image, bgr, cv::COLOR_GRAY2BGR);
    }
    else {
        throw std::invalid_argument("Unsupported image channel count for hue score");
    }
    return bgr;
}

cv::Mat VisibleAlphaMask(const cv::Mat& image)
{
    if (image.channels() != 4) {
        return {};
    }

    std::vector<cv::Mat> channels;
    cv::split(image, channels);
    cv::Mat mask;
    cv::threshold(channels[3], mask, 10, 255, cv::THRESH_BINARY);
    return mask;
}

double HueHistogramScore(const cv::Mat& source, const cv::Mat& target)
{
    if (source.empty() || target.empty()) {
        return 0.0;
    }

    cv::Mat sourceBgr = ToBgr(source);
    cv::Mat targetBgr = ToBgr(target);
    if (sourceBgr.empty() || targetBgr.empty()) {
        return 0.0;
    }

    if (sourceBgr.size() != targetBgr.size()) {
        cv::resize(targetBgr, targetBgr, sourceBgr.size(), 0, 0, cv::INTER_AREA);
    }

    cv::Mat sourceHsv;
    cv::Mat targetHsv;
    cv::cvtColor(sourceBgr, sourceHsv, cv::COLOR_BGR2HSV);
    cv::cvtColor(targetBgr, targetHsv, cv::COLOR_BGR2HSV);

    cv::Mat sourceMask = VisibleAlphaMask(source);
    cv::Mat targetMask = VisibleAlphaMask(target);
    if (!sourceMask.empty() && sourceMask.size() != sourceHsv.size()) {
        cv::resize(sourceMask, sourceMask, sourceHsv.size(), 0, 0, cv::INTER_NEAREST);
    }
    if (!targetMask.empty() && targetMask.size() != targetHsv.size()) {
        cv::resize(targetMask, targetMask, targetHsv.size(), 0, 0, cv::INTER_NEAREST);
    }

    cv::Mat combinedMask;
    if (!sourceMask.empty() && !targetMask.empty()) {
        cv::bitwise_and(sourceMask, targetMask, combinedMask);
    }
    else if (!sourceMask.empty()) {
        combinedMask = sourceMask;
    }
    else if (!targetMask.empty()) {
        combinedMask = targetMask;
    }

    const int histSize[] = { 30 };
    const float hueRange[] = { 0.0F, 180.0F };
    const float* ranges[] = { hueRange };
    const int channels[] = { 0 };

    cv::Mat sourceHist;
    cv::Mat targetHist;
    cv::calcHist(&sourceHsv, 1, channels, combinedMask, sourceHist, 1, histSize, ranges);
    cv::calcHist(&targetHsv, 1, channels, combinedMask, targetHist, 1, histSize, ranges);

    if (cv::sum(sourceHist)[0] <= 0.0 || cv::sum(targetHist)[0] <= 0.0) {
        return 0.0;
    }

    cv::normalize(sourceHist, sourceHist, 1.0, 0.0, cv::NORM_L1);
    cv::normalize(targetHist, targetHist, 1.0, 0.0, cv::NORM_L1);
    return std::clamp(cv::compareHist(sourceHist, targetHist, cv::HISTCMP_CORREL), 0.0, 1.0);
}

RecognitionDetail MakeErrorDetail(int status, std::string message)
{
    RecognitionDetail detail;
    detail.status = status;
    detail.message = std::move(message);
    return detail;
}

template <typename T>
void WriteJsonDetail(MaaStringBuffer* outDetail, const T& payload)
{
    if (outDetail == nullptr) {
        return;
    }
    const std::string text = json::value(payload).dumps();
    MaaStringBufferSet(outDetail, text.c_str());
}

RecognitionDetail ExtractInnerRecognitionDetail(const std::string& raw)
{
    auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        throw std::invalid_argument("recognition detail is not a JSON object");
    }

    RecognitionDetail detail;
    if (detail.from_json(*parsed)) {
        return detail;
    }

    const json::object& object = parsed->as_object();
    if (object.contains("best") && object.at("best").is_object()) {
        const json::object& best = object.at("best").as_object();
        if (best.contains("detail")) {
            const json::value& wrapped = best.at("detail");
            if (wrapped.is_string()) {
                return ExtractInnerRecognitionDetail(wrapped.as_string());
            }
            if (wrapped.is_object() && detail.from_json(wrapped)) {
                return detail;
            }
        }
    }

    throw std::invalid_argument("recognition detail does not match WeaponInventoryScan schema");
}

std::string GetRecognitionDetailJson(MaaContext* context, MaaRecoId recoId)
{
    if (recoId == MaaInvalidId) {
        throw std::invalid_argument("invalid reco_id");
    }

    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        throw std::runtime_error("context has no tasker");
    }

    MaaStringBuffer* nodeName = MaaStringBufferCreate();
    MaaStringBuffer* algorithm = MaaStringBufferCreate();
    MaaStringBuffer* detailJson = MaaStringBufferCreate();
    MaaBool hit = MAA_FALSE;
    MaaRect box {};

    const MaaBool ok =
        MaaTaskerGetRecognitionDetail(tasker, recoId, nodeName, algorithm, &hit, &box, detailJson, nullptr, nullptr);

    std::string text;
    if (ok && hit) {
        const char* raw = MaaStringBufferGet(detailJson);
        if (raw != nullptr) {
            text = raw;
        }
    }

    MaaStringBufferDestroy(nodeName);
    MaaStringBufferDestroy(algorithm);
    MaaStringBufferDestroy(detailJson);

    if (!ok) {
        throw std::runtime_error("MaaTaskerGetRecognitionDetail failed");
    }
    if (!hit) {
        throw std::runtime_error("previous recognition did not hit");
    }
    if (text.empty()) {
        throw std::runtime_error("previous recognition detail is empty");
    }
    return text;
}

std::vector<recogrid::Hash> ParseHashes(const RecognitionDetail& detail)
{
    std::vector<recogrid::Hash> hashes;
    hashes.reserve(detail.cellHashes.size());
    for (const std::string& text : detail.cellHashes) {
        std::uint64_t value = 0;
        std::stringstream stream(text);
        stream >> std::hex >> value;
        hashes.push_back(static_cast<recogrid::Hash>(value));
    }
    return hashes;
}

struct SamePageStats
{
    int compared = 0;
    int unchanged = 0;
    double ratio = 0.0;
    bool same = false;
};

SamePageStats CompareSamePage(
    const std::vector<recogrid::Hash>& previous,
    const std::vector<recogrid::Hash>& current,
    int distance,
    double ratioThreshold)
{
    SamePageStats stats;
    const std::size_t count = std::min(previous.size(), current.size());
    if (count == 0) {
        return stats;
    }

    stats.compared = static_cast<int>(count);
    for (std::size_t i = 0; i < count; ++i) {
        if (recogrid::HammingDistance(previous[i], current[i]) <= distance) {
            ++stats.unchanged;
        }
    }
    stats.ratio = static_cast<double>(stats.unchanged) / static_cast<double>(count);
    stats.same = stats.ratio >= ratioThreshold;
    return stats;
}

void ApplyStateParams(ScanState& state, const char* raw)
{
    if (raw == nullptr || std::strlen(raw) == 0) {
        return;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        return;
    }

    const json::object& object = parsed->as_object();
    ReadField(object, "output_path", state.outputPath);
    ReadField(object, "same_cell_distance", state.sameCellDistance);
    ReadField(object, "unchanged_ratio", state.unchangedRatio);
    state.sameCellDistance = std::max(0, state.sameCellDistance);
    state.unchangedRatio = std::clamp(state.unchangedRatio, 0.0, 1.0);
}

void WriteOutputFile(const ScanState& state)
{
    ScanOutput output;
    output.weapons = state.weaponCounts;
    output.weaponTypes = static_cast<int>(output.weapons.size());
    for (const auto& [_, count] : output.weapons) {
        output.weaponCount += count;
    }

    const fs::path path = ResolvePath(state.outputPath);
    std::error_code ec;
    if (path.has_parent_path()) {
        fs::create_directories(path.parent_path(), ec);
    }
    if (ec) {
        throw std::runtime_error("failed to create output directory: " + ec.message());
    }

    std::ofstream file(path, std::ios::out | std::ios::trunc);
    if (!file) {
        throw std::runtime_error("failed to open output file: " + path.string());
    }
    file << json::value(output).dumps(4) << '\n';
}

} // namespace

MaaBool MAA_CALL WeaponInventoryScanRecognitionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    if (image == nullptr || MaaImageBufferIsEmpty(image)) {
        WriteJsonDetail(out_detail, MakeErrorDetail(-2, "Image buffer is empty"));
        LogError << "WeaponInventoryScanRecognition: Image buffer is empty";
        return MAA_FALSE;
    }

    try {
        ScanState* state = GetState(context);
        RecognitionOptions options = state == nullptr ? ParseRecognitionOptions(custom_recognition_param) : state->options;
        if ((custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0) && roi != nullptr &&
            roi->width > 0 && roi->height > 0) {
            options.detect.roi = cv::Rect(roi->x, roi->y, roi->width, roi->height);
        }

        std::vector<TemplateEntry> templates;
        const std::vector<TemplateEntry>* templatePtr = nullptr;
        if (state != nullptr && !state->templates.empty()) {
            templatePtr = &state->templates;
        }
        else {
            templates = LoadTemplateIndex(options);
            templatePtr = &templates;
        }
        if (templatePtr == nullptr || templatePtr->empty()) {
            WriteJsonDetail(out_detail, MakeErrorDetail(-3, "No weapon icon templates loaded"));
            LogError << "WeaponInventoryScanRecognition: no templates loaded";
            return MAA_FALSE;
        }

        const cv::Mat screenshot = to_mat(image);
        const recogrid::GridResult grid = recogrid::DetectGrid(screenshot, options.detect);
        if (grid.cells.empty()) {
            WriteJsonDetail(out_detail, MakeErrorDetail(1, "Grid detected no cells"));
            LogWarn << "WeaponInventoryScanRecognition: grid detected no cells";
            return MAA_FALSE;
        }

        RecognitionDetail detail;
        detail.rows = static_cast<int>(grid.rows.size());
        detail.cols = static_cast<int>(grid.cols.size());
        detail.cellCount = static_cast<int>(grid.cells.size());

        const std::vector<recogrid::Hash> cellHashes = recogrid::ComputeCellHashes(grid.roi, grid.cells, options.mask);
        detail.cellHashes.reserve(cellHashes.size());
        for (const recogrid::Hash hash : cellHashes) {
            detail.cellHashes.push_back(HashToHex(hash));
        }

        std::map<std::size_t, WeaponOutput> bestByCell;
        int templatesScanned = 0;
        int templatesWithCandidates = 0;
        int candidatesAfterPhash = 0;
        int templatesRanked = 0;
        int rejectedByMinScore = 0;
        for (const TemplateEntry& entry : *templatePtr) {
            ++templatesScanned;
            std::vector<recogrid::Candidate> candidates =
                recogrid::FilterCandidates(grid.roi, grid.cells, entry.image, options.maxPhashDistance, options.mask);
            if (candidates.empty()) {
                continue;
            }
            ++templatesWithCandidates;
            candidatesAfterPhash += static_cast<int>(candidates.size());

            if (static_cast<int>(candidates.size()) > options.maxReturnedMatches) {
                candidates.resize(static_cast<std::size_t>(options.maxReturnedMatches));
            }

            const std::vector<recogrid::TemplateMatchResult> ranked =
                recogrid::RankTemplateMatches(grid.roi, entry.image, candidates, options.mask);
            if (ranked.empty()) {
                continue;
            }
            ++templatesRanked;

            const recogrid::TemplateMatchResult& best = ranked.front();
            if (!std::isfinite(best.score) || best.score < options.minScore) {
                ++rejectedByMinScore;
                continue;
            }

            const cv::Rect sourceRect = ClampRect(best.match, grid.roi.size());
            double hueScore = 0.0;
            if (!sourceRect.empty()) {
                hueScore = HueHistogramScore(grid.roi(sourceRect), entry.image);
            }
            const double finalScore = std::clamp(kTemplateScoreWeight * best.score + kHueScoreWeight * hueScore, 0.0, 1.0);

            WeaponOutput output {
                entry.weaponId,
                static_cast<int>(best.cellIndex),
                finalScore,
                best.score,
                hueScore,
                best.phashDistance,
                RectToArray(CellToScreen(best.cell, options.detect, screenshot.size())),
            };

            auto it = bestByCell.find(best.cellIndex);
            if (it == bestByCell.end() || output.score > it->second.score ||
                (output.score == it->second.score && output.phashDistance < it->second.phashDistance)) {
                bestByCell[best.cellIndex] = std::move(output);
            }
        }

        for (auto& [_, weapon] : bestByCell) {
            detail.weapons.push_back(std::move(weapon));
        }

        WriteJsonDetail(out_detail, detail);
        if (out_box != nullptr) {
            const cv::Rect firstCell = grid.cells.empty() ? cv::Rect() : CellToScreen(grid.cells.front(), options.detect, screenshot.size());
            *out_box = { firstCell.x, firstCell.y, firstCell.width, firstCell.height };
        }

        LogInfo << "WeaponInventoryScanRecognition matched" << VAR(detail.rows) << VAR(detail.cols)
                << VAR(detail.cellCount) << VAR(detail.weapons.size()) << VAR(templatesScanned)
                << VAR(templatesWithCandidates) << VAR(candidatesAfterPhash) << VAR(templatesRanked)
                << VAR(rejectedByMinScore);
        return MAA_TRUE;
    }
    catch (const std::exception& e) {
        WriteJsonDetail(out_detail, MakeErrorDetail(-4, e.what()));
        LogError << "WeaponInventoryScanRecognition failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

MaaBool MAA_CALL WeaponInventoryScanInitActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    try {
        if (context == nullptr) {
            LogError << "WeaponInventoryScanInitAction: null context";
            return MAA_FALSE;
        }

        MaaTasker* tasker = MaaContextGetTasker(context);
        if (tasker == nullptr) {
            LogError << "WeaponInventoryScanInitAction: no tasker";
            return MAA_FALSE;
        }

        auto state = std::make_shared<ScanState>();
        state->options = ParseRecognitionOptions(custom_action_param);
        ApplyStateParams(*state, custom_action_param);
        state->templates = LoadTemplateIndex(state->options);
        if (state->templates.empty()) {
            LogError << "WeaponInventoryScanInitAction: no weapon templates loaded";
            return MAA_FALSE;
        }

        LogInfo << "WeaponInventoryScanInitAction initialized" << VAR(state->templates.size())
                << VAR(state->outputPath) << VAR(state->sameCellDistance) << VAR(state->unchangedRatio)
                << VAR(static_cast<const void*>(tasker));
        SetState(tasker, std::move(state));
        return MAA_TRUE;
    }
    catch (const std::exception& e) {
        LogError << "WeaponInventoryScanInitAction failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

MaaBool MAA_CALL WeaponInventoryScanRecordActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    try {
        ScanState* state = GetState(context);
        if (state == nullptr) {
            LogError << "WeaponInventoryScanRecordAction: missing scan state" << VAR(g_states.size())
                     << VAR(static_cast<bool>(g_currentState));
            return MAA_FALSE;
        }
        ApplyStateParams(*state, custom_action_param);

        const RecognitionDetail detail = ExtractInnerRecognitionDetail(GetRecognitionDetailJson(context, reco_id));
        const std::vector<recogrid::Hash> hashes = ParseHashes(detail);

        const SamePageStats samePage =
            CompareSamePage(state->lastHashes, hashes, state->sameCellDistance, state->unchangedRatio);
        const bool reachedEnd = !state->lastHashes.empty() && samePage.same;

        int added = 0;
        if (!reachedEnd) {
            for (const WeaponOutput& weapon : detail.weapons) {
                if (!weapon.weaponId.empty()) {
                    ++state->weaponCounts[weapon.weaponId];
                    ++added;
                }
            }
        }
        else {
            state->bottomSkippedSlots += static_cast<int>(detail.weapons.size());
        }

        ++state->pageCount;
        state->totalCellsSeen += detail.cellCount;
        state->totalRecognizedSlots += static_cast<int>(detail.weapons.size());
        state->totalCountedSlots += added;
        state->stop = reachedEnd;
        state->lastHashes = hashes;

        LogInfo << "WeaponInventoryScanRecordAction page" << VAR(state->pageCount)
                << VAR(detail.cellCount) << VAR(detail.weapons.size()) << VAR(added)
                << VAR(state->weaponCounts.size()) << VAR(reachedEnd) << VAR(samePage.compared)
                << VAR(samePage.unchanged) << VAR(samePage.ratio) << VAR(state->totalCellsSeen)
                << VAR(state->totalRecognizedSlots) << VAR(state->totalCountedSlots)
                << VAR(state->bottomSkippedSlots);
        return reachedEnd ? MAA_FALSE : MAA_TRUE;
    }
    catch (const std::exception& e) {
        LogError << "WeaponInventoryScanRecordAction failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

MaaBool MAA_CALL WeaponInventoryScanFinishActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    try {
        if (context == nullptr) {
            LogError << "WeaponInventoryScanFinishAction: null context";
            return MAA_FALSE;
        }

        std::shared_ptr<ScanState> state = TakeState(context);
        if (state == nullptr) {
            LogError << "WeaponInventoryScanFinishAction: missing scan state";
            return MAA_FALSE;
        }

        ApplyStateParams(*state, custom_action_param);
        WriteOutputFile(*state);
        LogInfo << "WeaponInventoryScanFinishAction finished" << VAR(state->pageCount)
                << VAR(state->weaponCounts.size()) << VAR(state->totalCellsSeen)
                << VAR(state->totalRecognizedSlots) << VAR(state->totalCountedSlots)
                << VAR(state->bottomSkippedSlots) << VAR(state->outputPath);
        return MAA_TRUE;
    }
    catch (const std::exception& e) {
        LogError << "WeaponInventoryScanFinishAction failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

} // namespace weaponinventoryscan
