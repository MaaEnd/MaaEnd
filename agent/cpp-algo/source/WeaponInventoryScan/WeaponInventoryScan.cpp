#include "WeaponInventoryScan.h"

#include "../RecoGrid/RecoGridEngine.h"
#include "../utils.h"

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>

#include <meojson/json.hpp>

#include <cstring>
#include <filesystem>
#include <regex>
#include <stdexcept>

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace weaponinventoryscan
{
namespace
{

constexpr const char* kRuntimeTemplateDir = "data/WeaponIcon/iconbig";
constexpr const char* kSourceTemplateDir = "assets/data/WeaponIcon/iconbig";
constexpr const char* kSessionId = "WeaponInventoryScan";

recogrid::RecoGridEngine g_engine;
bool g_loaded = false;
MaaTaskId g_lastTaskId = MaaInvalidId;
int g_inventoryTotalCells = 0;
bool g_inventoryTotalAttempted = false;

std::filesystem::path ResolveWeaponTemplateDir()
{
    for (const char* directory : { kRuntimeTemplateDir, kSourceTemplateDir }) {
        std::error_code ec;
        if (std::filesystem::exists(directory, ec) && std::filesystem::is_directory(directory, ec)) {
            return directory;
        }
    }
    throw std::runtime_error("Weapon icon template directory not found");
}

void ApplyWeaponInventoryMask(recogrid::GridScanOptions& options)
{
    options.recognition.mask.leftHeaderWidth = 20.0 / 96.0;
    options.recognition.mask.leftHeaderHeight = 20.0 / 96.0;
    options.recognition.mask.rightHeaderWidth = 30.0 / 96.0;
    options.recognition.mask.rightHeaderHeight = 30.0 / 96.0;
    options.recognition.mask.bottomHeight = 20.0 / 96.0;
}

void ApplyWeaponInventoryScanDefaults(recogrid::GridScanOptions& options)
{
    options.recognition.detect.roi = { 20, 70, 960, 600 };
    options.recognition.detect.normalizedSize = { 1280, 720 };
    options.recognition.detect.rowThresholdRatio = 0.2;
    options.recognition.detect.colThresholdRatio = 0.4;
    options.recognition.detect.minRawSegmentLength = 10;
    options.recognition.detect.minKeptSegmentRatio = 0.7;
    options.recognition.maxPhashDistance = 10;
    options.recognition.maxRankedCandidates = 0;
    options.recognition.minScore = 0.6;
    options.recognition.hueWeight = 0.4;
    ApplyWeaponInventoryMask(options);
    options.incremental = true;
    options.endMinMatchRatio = 0.95;
}

void EnsureLoaded()
{
    if (!g_loaded) {
        g_engine.LoadTemplatesFromDirectory(ResolveWeaponTemplateDir());
        g_loaded = true;
    }
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

int ReadIntegerOption(const char* raw, const char* key, int defaultValue)
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
    return object.at(key).as_integer();
}

bool ReadIncremental(const char* raw)
{
    return ReadBooleanOption(raw, "incremental", true);
}

recogrid::GridRecognitionRequest ParseWeaponRecognitionRequest(const char* raw, const recogrid::GridRecognitionOptions& defaults)
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
    g_engine.ResetSession(kSessionId);
    g_lastTaskId = taskId;
    g_inventoryTotalCells = 0;
    g_inventoryTotalAttempted = false;
    LogInfo << "WeaponInventoryScan reset session" << VAR(taskId);
}

int ParseInventoryTotalText(const std::string& text)
{
    static const std::regex pattern(R"((\d{1,6})\s*/\s*\d{1,6})");
    std::smatch match;
    if (!std::regex_search(text, match, pattern) || match.size() < 2) {
        return 0;
    }

    try {
        const int value = std::stoi(match[1].str());
        return value > 0 && value <= 10000 ? value : 0;
    }
    catch (const std::exception&) {
        return 0;
    }
}

int ParseInventoryTotalFromOcrItem(const json::value& value)
{
    if (!value.is_object()) {
        return 0;
    }

    const auto& object = value.as_object();
    if (!object.contains("text") || !object.at("text").is_string()) {
        return 0;
    }
    return ParseInventoryTotalText(object.at("text").as_string());
}

int ParseInventoryTotalFromOcrDetail(const char* raw)
{
    if (raw == nullptr || std::strlen(raw) == 0) {
        return 0;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        return 0;
    }

    const auto& object = parsed->as_object();
    if (object.contains("filtered") && object.at("filtered").is_array()) {
        for (const auto& item : object.at("filtered").as_array()) {
            const int value = ParseInventoryTotalFromOcrItem(item);
            if (value > 0) {
                return value;
            }
        }
    }
    if (object.contains("best")) {
        const int value = ParseInventoryTotalFromOcrItem(object.at("best"));
        if (value > 0) {
            return value;
        }
    }
    if (object.contains("all") && object.at("all").is_array()) {
        for (const auto& item : object.at("all").as_array()) {
            const int value = ParseInventoryTotalFromOcrItem(item);
            if (value > 0) {
                return value;
            }
        }
    }
    return 0;
}

int RecognizeInventoryTotalCells(MaaContext* context, const MaaImageBuffer* image)
{
    if (context == nullptr || image == nullptr || MaaImageBufferIsEmpty(image)) {
        return 0;
    }

    constexpr const char* ocrParam = R"({"roi":[0,0,155,85],"expected":["\\d+\\s*/\\s*\\d+"]})";
    const MaaRecoId recoId = MaaContextRunRecognitionDirect(context, "OCR", ocrParam, image);
    if (recoId == MaaInvalidId) {
        LogWarn << "WeaponInventoryScan inventory total OCR returned invalid id";
        return 0;
    }

    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        LogWarn << "WeaponInventoryScan inventory total OCR has no tasker";
        return 0;
    }

    MaaStringBuffer* nodeName = MaaStringBufferCreate();
    MaaStringBuffer* algorithm = MaaStringBufferCreate();
    MaaStringBuffer* detail = MaaStringBufferCreate();
    if (nodeName == nullptr || algorithm == nullptr || detail == nullptr) {
        if (nodeName != nullptr) {
            MaaStringBufferDestroy(nodeName);
        }
        if (algorithm != nullptr) {
            MaaStringBufferDestroy(algorithm);
        }
        if (detail != nullptr) {
            MaaStringBufferDestroy(detail);
        }
        LogWarn << "WeaponInventoryScan inventory total OCR buffer allocation failed";
        return 0;
    }

    MaaBool hit = MAA_FALSE;
    MaaRect box {};
    const bool ok = MaaTaskerGetRecognitionDetail(tasker, recoId, nodeName, algorithm, &hit, &box, detail, nullptr, nullptr);
    const int value = ok ? ParseInventoryTotalFromOcrDetail(MaaStringBufferGet(detail)) : 0;
    if (value > 0) {
        LogInfo << "WeaponInventoryScan inventory total OCR" << VAR(value) << VAR(hit);
    }
    else {
        LogWarn << "WeaponInventoryScan inventory total OCR parse failed" << VAR(ok) << VAR(hit)
                << VAR(MaaStringBufferGet(detail));
    }

    MaaStringBufferDestroy(nodeName);
    MaaStringBufferDestroy(algorithm);
    MaaStringBufferDestroy(detail);
    return value;
}

int ResolveExpectedTotalCells(const char* raw, MaaContext* context, const MaaImageBuffer* image)
{
    int expected = ReadIntegerOption(raw, "expected_cells", 0);
    if (expected <= 0) {
        expected = ReadIntegerOption(raw, "expected_total_cells", 0);
    }
    if (expected > 0) {
        return expected;
    }

    if (g_inventoryTotalCells > 0) {
        return g_inventoryTotalCells;
    }
    if (g_inventoryTotalAttempted) {
        return 0;
    }

    g_inventoryTotalAttempted = true;
    g_inventoryTotalCells = RecognizeInventoryTotalCells(context, image);
    return g_inventoryTotalCells;
}

void WriteSummaryDetail(MaaStringBuffer* outDetail, const recogrid::GridScanResult& result)
{
    if (outDetail == nullptr) {
        return;
    }

    json::object detail;
    detail["success"] = result.success;
    detail["page_grid"] = result.totalCells;
    detail["cumulative_grid"] = result.sessionTotalCells;
    detail["expected_grid"] = result.expectedTotalCells;
    detail["normalized_cell_delta"] = result.normalizedCellDelta;
    detail["unknown"] = result.unknownCells;
    detail["rows"] = result.sessionRows;
    detail["cols"] = result.sessionCols;
    detail["page_rows"] = result.rows;
    detail["page_cols"] = result.cols;
    detail["new_cells"] = static_cast<int>(result.newCellIndices.size());
    detail["row_offset"] = result.rowOffset;
    detail["delta_reliable"] = result.deltaReliable;
    detail["has_progress"] = result.hasProgress;
    detail["reached_end"] = result.reachedEnd;
    detail["matched_cells"] = result.matchedCells;
    detail["compared_cells"] = result.comparedCells;
    detail["match_ratio"] = result.matchRatio;
    if (!result.success) {
        detail["message"] = result.message;
    }

    const std::string text = json::value(std::move(detail)).dumps();
    MaaStringBufferSet(outDetail, text.c_str());
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

} // namespace

MaaBool MAA_CALL WeaponInventoryScanRecognitionRun(
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
        ApplyWeaponInventoryScanDefaults(options);

        recogrid::GridRecognitionRequest request =
            ParseWeaponRecognitionRequest(custom_recognition_param, options.recognition);
        if ((custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0) && roi != nullptr &&
            roi->width > 0 && roi->height > 0) {
            request = recogrid::ApplyRoiOverride(request, { roi->x, roi->y, roi->width, roi->height });
        }

        options.recognition = request.options;
        options.incremental = ReadIncremental(custom_recognition_param);
        options.endMinMatchRatio =
            ReadDoubleOption(custom_recognition_param, "end_min_match_ratio", options.endMinMatchRatio);
        ApplyWeaponInventoryMask(options);
        options.expectedTotalCells = ResolveExpectedTotalCells(custom_recognition_param, context, image);

        const recogrid::GridScanResult result = g_engine.Scan(kSessionId, to_mat(image), options);
        if (result.success) {
            const int cumulativeGrid = result.sessionTotalCells;
            const int unknown = result.unknownCells;
            const int rows = result.sessionRows;
            const int cols = result.sessionCols;
            const int pageGrid = result.totalCells;
            const int newCells = static_cast<int>(result.newCellIndices.size());
            const int expectedGrid = result.expectedTotalCells;
            const int normalizedDelta = result.normalizedCellDelta;
            LogInfo << "WeaponInventoryScan cumulative grid" << VAR(cumulativeGrid) << VAR(unknown) << VAR(rows)
                    << VAR(cols) << VAR(pageGrid) << VAR(newCells) << VAR(expectedGrid) << VAR(normalizedDelta);
            LogInfo << "WeaponInventoryScan scan delta" << VAR(result.deltaReliable) << VAR(result.hasProgress)
                    << VAR(result.reachedEnd) << VAR(result.rowOffset) << VAR(result.matchedCells)
                    << VAR(result.comparedCells) << VAR(result.matchRatio) << VAR(result.averageDistance)
                    << VAR(result.deltaScore);
            const char* nextNode = result.reachedEnd ? "WeaponInventoryScanFinish" : "WeaponInventoryScanSwipeNext";
            LogInfo << "WeaponInventoryScan override next" << VAR(nextNode);
            if (!OverrideNext(context, node_name, nextNode)) {
                LogWarn << "WeaponInventoryScan override next failed" << VAR(result.reachedEnd);
            }
        }
        WriteSummaryDetail(out_detail, result);

        if (out_box != nullptr) {
            for (const recogrid::GridScanCell& cell : result.cells) {
                if (cell.visible) {
                    *out_box = { cell.screenCell.x, cell.screenCell.y, cell.screenCell.width, cell.screenCell.height };
                    break;
                }
            }
        }
        return result.success ? MAA_TRUE : MAA_FALSE;
    }
    catch (const std::exception& e) {
        WriteError(out_detail, e.what());
        LogError << "WeaponInventoryScanRecognition failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

} // namespace weaponinventoryscan
