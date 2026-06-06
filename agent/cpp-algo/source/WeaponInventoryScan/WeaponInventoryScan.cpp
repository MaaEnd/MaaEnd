#include "WeaponInventoryScan.h"

#include "../RecoGrid/RecoGridEngine.h"
#include "../utils.h"

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>

#include <meojson/json.hpp>

#include <cstring>
#include <filesystem>
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

void EnsureLoaded()
{
    if (!g_loaded) {
        for (const char* directory : { kRuntimeTemplateDir, kSourceTemplateDir }) {
            std::error_code ec;
            if (std::filesystem::exists(directory, ec) && std::filesystem::is_directory(directory, ec)) {
                g_engine.LoadTemplatesFromDirectory(directory);
                g_loaded = true;
                return;
            }
        }
        throw std::runtime_error("Weapon icon template directory not found");
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

bool ReadIncremental(const char* raw)
{
    return ReadBooleanOption(raw, "incremental", true);
}

bool ReadDebugDetail(const char* raw)
{
    return ReadBooleanOption(raw, "debug_detail", false);
}

void ApplyWeaponMask(recogrid::GridScanOptions& options)
{
    options.recognition.mask.leftHeaderWidth = 20.0 / 96.0;
    options.recognition.mask.leftHeaderHeight = 20.0 / 96.0;
    options.recognition.mask.rightHeaderWidth = 30.0 / 96.0;
    options.recognition.mask.rightHeaderHeight = 30.0 / 96.0;
    options.recognition.mask.bottomHeight = 20.0 / 96.0;
}

void ResetSessionForNewTask(MaaTaskId taskId)
{
    if (taskId == MaaInvalidId || taskId == g_lastTaskId) {
        return;
    }
    g_engine.ResetSession(kSessionId);
    g_lastTaskId = taskId;
    LogInfo << "WeaponInventoryScan reset session" << VAR(taskId);
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

void WriteDebugDetail(MaaStringBuffer* outDetail, const recogrid::GridScanResult& result)
{
    if (outDetail == nullptr) {
        return;
    }

    json::array cells;
    for (const recogrid::GridScanCell& cell : result.cells) {
        json::object item;
        item["row"] = cell.row;
        item["col"] = cell.col;
        item["template_id"] = cell.templateId;
        item["matched"] = cell.matched;
        cells.emplace_back(std::move(item));
    }

    json::object detail;
    detail["success"] = result.success;
    detail["message"] = result.message;
    detail["rows"] = result.rows;
    detail["cols"] = result.cols;
    detail["total_cells"] = result.totalCells;
    detail["cumulative_rows"] = result.sessionRows;
    detail["cumulative_cols"] = result.sessionCols;
    detail["cumulative_cells"] = result.sessionTotalCells;
    detail["known_cells"] = result.knownCells;
    detail["unknown_cells"] = result.unknownCells;
    detail["incremental_used"] = result.incrementalUsed;
    detail["row_offset"] = result.rowOffset;
    detail["delta_reliable"] = result.deltaReliable;
    detail["has_progress"] = result.hasProgress;
    detail["reached_end"] = result.reachedEnd;
    detail["matched_cells"] = result.matchedCells;
    detail["compared_cells"] = result.comparedCells;
    detail["total_distance"] = result.totalDistance;
    detail["average_distance"] = result.averageDistance;
    detail["delta_score"] = result.deltaScore;
    detail["match_ratio"] = result.matchRatio;
    detail["new_cells"] = static_cast<int>(result.newCellIndices.size());
    detail["cells"] = std::move(cells);

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

        recogrid::GridRecognitionRequest request = recogrid::ParseGridRecognitionRequest(custom_recognition_param);
        if ((custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0) && roi != nullptr &&
            roi->width > 0 && roi->height > 0) {
            request = recogrid::ApplyRoiOverride(request, { roi->x, roi->y, roi->width, roi->height });
        }

        recogrid::GridScanOptions options;
        options.recognition = request.options;
        options.incremental = ReadIncremental(custom_recognition_param);
        ApplyWeaponMask(options);

        const bool debugDetail = ReadDebugDetail(custom_recognition_param);
        const recogrid::GridScanResult result = g_engine.Scan(kSessionId, to_mat(image), options);
        if (result.success) {
            const int cumulativeGrid = result.sessionTotalCells;
            const int unknown = result.unknownCells;
            const int rows = result.sessionRows;
            const int cols = result.sessionCols;
            const int pageGrid = result.totalCells;
            const int newCells = static_cast<int>(result.newCellIndices.size());
            LogInfo << "WeaponInventoryScan cumulative grid" << VAR(cumulativeGrid) << VAR(unknown) << VAR(rows)
                    << VAR(cols) << VAR(pageGrid) << VAR(newCells);
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
        if (debugDetail) {
            WriteDebugDetail(out_detail, result);
        }
        else {
            WriteSummaryDetail(out_detail, result);
        }

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
