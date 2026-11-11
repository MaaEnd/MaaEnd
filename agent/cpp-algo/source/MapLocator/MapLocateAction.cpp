#include <algorithm>
#include <chrono>
#include <cmath>
#include <filesystem>
#include <string_view>
#include <thread>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>
#include <MaaUtils/Platform.h>
#ifdef _WIN32
#include <MaaUtils/SafeWindows.hpp>
#else
#include <unistd.h>
#endif

#include "../Common/paths.h"
#include "../MapNavigator/navi_param.h"
#include "../MapNavigator/planning/navmesh_queries.h"
#include "../utils.h"

#include "MapLocateAction.h"
#include "MapLocator.h"

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace fs = std::filesystem;

namespace maplocator
{

namespace
{

struct LocateOutput
{
    int status = 0;
    std::string message;
    std::string mapName;
    double x = 0.0;
    double y = 0.0;
    std::optional<double> rot;
    double locConf = 0.0;
    std::optional<double> camRot;
    std::optional<double> camRotConf;
    int latencyMs = 0;

    MEO_JSONIZATION(
        status,
        message,
        MEO_OPT mapName,
        MEO_OPT x,
        MEO_OPT y,
        MEO_OPT rot,
        MEO_OPT locConf,
        MEO_OPT camRot,
        MEO_OPT camRotConf,
        MEO_OPT latencyMs)
};

struct MapLocateAssertLocationParam
{
    std::string zone_id;
    std::vector<double> target;

    MEO_JSONIZATION(MEO_OPT zone_id, MEO_OPT target)
};

struct MapLocateAssertLocationOutput
{
    int status = 0;
    bool matched = false;
    bool inTarget = false;
    std::string message;
    std::string zoneId;
    double x = 0.0;
    double y = 0.0;
    std::optional<double> rot;
    double locConf = 0.0;
    std::optional<double> camRot;
    std::optional<double> camRotConf;
    int latencyMs = 0;
    std::vector<double> target;

    MEO_JSONIZATION(
        status,
        matched,
        inTarget,
        message,
        MEO_OPT zoneId,
        MEO_OPT x,
        MEO_OPT y,
        MEO_OPT rot,
        MEO_OPT locConf,
        MEO_OPT camRot,
        MEO_OPT camRotConf,
        MEO_OPT latencyMs,
        MEO_OPT target)
};

// 参数由 pipeline 提供，字段类型不由本模块保证。解析失败退回默认值，与不传参数同路。
template <typename T>
T parse_custom_recognition_param(const char* custom_recognition_param)
{
    if (custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0) {
        return T {};
    }

    const auto parsed = json::parse(custom_recognition_param);
    T value {};
    if (!parsed || !value.from_json(*parsed)) {
        // from_json 中途失败会留下写了一半的字段，整个丢掉重来。
        LogWarn << "Invalid param, using defaults" << VAR(custom_recognition_param);
        return T {};
    }
    return value;
}

template <typename T>
void write_json_detail(MaaStringBuffer* out_detail, const T& payload)
{
    if (out_detail == nullptr) {
        return;
    }

    json::value detail = payload;
    // MEO_OPT 只影响反序列化；输出时显式省略不可用朝向，不能用零或 null 代替。
    if (!payload.rot.has_value()) {
        detail.as_object().erase("rot");
    }
    if (!payload.camRot.has_value()) {
        detail.as_object().erase("camRot");
        detail.as_object().erase("camRotConf");
    }
    const std::string json_text = detail.dumps();
    MaaStringBufferSet(out_detail, json_text.c_str());
}

LocateOutput build_locate_output(const LocateResult& result)
{
    LocateOutput output;
    output.status = static_cast<int>(result.status);
    output.message = result.debug_message;
    output.rot = result.rot;
    if (!result.position.has_value()) {
        return output;
    }

    const auto& pos = result.position.value();
    output.mapName = pos.zone_id;
    output.x = pos.x;
    output.y = pos.y;
    output.locConf = pos.score;
    output.latencyMs = static_cast<int>(pos.latency_ms);
    if (result.cam_rot.has_value()) {
        output.camRot = result.cam_rot->rot;
        output.camRotConf = result.cam_rot->confidence;
    }
    return output;
}

MapLocateAssertLocationOutput
    build_assert_location_output(const LocateResult& result, const MapLocateAssertLocationParam& param, bool matched)
{
    MapLocateAssertLocationOutput output;
    output.status = static_cast<int>(result.status);
    output.matched = matched;
    output.inTarget = matched;
    output.message = result.debug_message;
    output.zoneId = param.zone_id;
    output.target = param.target;
    output.rot = result.rot;
    if (!result.position.has_value()) {
        return output;
    }

    const auto& pos = result.position.value();
    output.x = pos.x;
    output.y = pos.y;
    output.locConf = pos.score;
    output.latencyMs = static_cast<int>(pos.latency_ms);
    if (result.cam_rot.has_value()) {
        output.camRot = result.cam_rot->rot;
        output.camRotConf = result.cam_rot->confidence;
    }
    return output;
}

MaaRect make_point_box(const MapPosition& position)
{
    return {
        static_cast<int>(std::lround(position.x)),
        static_cast<int>(std::lround(position.y)),
        1,
        1,
    };
}

bool try_build_assert_rect(const MapLocateAssertLocationParam& param, MaaRect* out_rect)
{
    if (out_rect == nullptr || param.target.size() != 4) {
        return false;
    }

    const double width = param.target[2];
    const double height = param.target[3];
    if (width <= 0.0 || height <= 0.0) {
        return false;
    }

    out_rect->x = static_cast<int>(std::lround(param.target[0]));
    out_rect->y = static_cast<int>(std::lround(param.target[1]));
    out_rect->width = std::max(1, static_cast<int>(std::lround(width)));
    out_rect->height = std::max(1, static_cast<int>(std::lround(height)));
    return true;
}

bool is_position_inside_rect(const MapPosition& position, const MaaRect& rect)
{
    const double left = static_cast<double>(rect.x);
    const double top = static_cast<double>(rect.y);
    const double right = left + static_cast<double>(rect.width);
    const double bottom = top + static_cast<double>(rect.height);
    return position.x >= left && position.x < right && position.y >= top && position.y < bottom;
}

// 定位落在同一地图的别的层上时, 位置和框都换到底图再比
bool is_position_inside_target(
    const MapPosition& position,
    const std::string& target_zone,
    const MaaRect& rect,
    const mapnavigator::NaviParam& navi_param)
{
    if (position.zone_id == target_zone) {
        return is_position_inside_rect(position, rect);
    }
    const auto at = mapnavigator::ProjectToMapBase(navi_param, position.zone_id, position.x, position.y);
    const auto top_left = mapnavigator::ProjectToMapBase(navi_param, target_zone, rect.x, rect.y);
    const auto bottom_right = mapnavigator::ProjectToMapBase(navi_param, target_zone, rect.x + rect.width, rect.y + rect.height);
    if (!at || !top_left || !bottom_right) {
        return false;
    }
    return at->x >= top_left->x && at->x < bottom_right->x && at->y >= top_left->y && at->y < bottom_right->y;
}

// 传送落地后区域横幅会压住小地图，定位在横幅散掉之前拿不到可用结果，预算要覆盖这段等待。
constexpr int kAssertLocateMaxFrames = 60;
constexpr auto kAssertLocatePollDelay = std::chrono::milliseconds(250);

std::string detect_controller_type(MaaContext* context)
{
    if (context == nullptr) {
        return {};
    }

    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        return {};
    }

    MaaController* controller = MaaTaskerGetController(tasker);
    if (controller == nullptr) {
        return {};
    }

    MaaStringBuffer* buffer = MaaStringBufferCreate();
    if (buffer == nullptr) {
        return {};
    }

    std::string controller_type;
    if (MaaControllerGetInfo(controller, buffer) && !MaaStringBufferIsEmpty(buffer)) {
        const char* raw = MaaStringBufferGet(buffer);
        if (raw != nullptr && raw[0] != '\0') {
            const auto info = json::parse(raw).value_or(json::object {});
            if (info.contains("type") && info.at("type").is_string()) {
                controller_type = info.at("type").as_string();
            }
        }
    }

    MaaStringBufferDestroy(buffer);
    return controller_type;
}

bool uses_adb_minimap_roi(std::string_view controller_type)
{
    auto equals_ignore_case = [controller_type](std::string_view candidate) {
        return controller_type.size() == candidate.size() && std::ranges::equal(controller_type, candidate, [](char lhs, char rhs) {
                   return std::tolower(static_cast<unsigned char>(lhs)) == std::tolower(static_cast<unsigned char>(rhs));
               });
    };

    return equals_ignore_case("adb") || equals_ignore_case("playcover") || equals_ignore_case("play_cover")
           || equals_ignore_case("native_android");
}

bool try_locate_on_minimap(MaaContext* context, const MaaImageBuffer* image, const LocateOptions& options, LocateResult* out_result)
{
    if (out_result == nullptr) {
        return false;
    }

    const MaaImageBuffer* actual_image = image;
    ScopedImageBuffer captured_image;
    if (actual_image == nullptr || MaaImageBufferIsEmpty(actual_image)) {
        auto controller = MaaTaskerGetController(MaaContextGetTasker(context));
        const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller);
        MaaControllerWait(controller, screencap_id);
        if (!MaaControllerCachedImage(controller, captured_image.Get()) || MaaImageBufferIsEmpty(captured_image.Get())) {
            LogError << "MapLocateRecognition: Image buffer is empty";
            return false;
        }
        actual_image = captured_image.Get();
    }

    if (actual_image == nullptr || MaaImageBufferIsEmpty(actual_image)) {
        LogError << "MapLocateRecognition: Image buffer is empty";
        return false;
    }

    auto locator = getOrInitLocator();
    if (!locator) {
        LogError << "MapLocateAction: Locator init failed";
        return false;
    }

    cv::Mat minimap;
    const cv::Mat image_mat = to_mat(actual_image);
    if (!TryExtractMinimap(image_mat, uses_adb_minimap_roi(detect_controller_type(context)), &minimap)) {
        LogError << "MapLocateRecognition: ROI empty";
        return false;
    }

    *out_result = Locate(locator.get(), minimap, options);
    return true;
}

} // namespace

std::shared_ptr<MapLocator> getOrInitLocator()
{
    static std::shared_ptr<MapLocator> locator = []() {
        fs::path mapRoot = common::InstallPath("resource/image/MapLocator");
        fs::path yoloModel = common::InstallPath("resource/model/map/cls.onnx");
        fs::path cameraOrientationDir = common::InstallPath("resource/model/map/cameraorientation");
        fs::path cameraOrientationRefModel = cameraOrientationDir / "polar_with_ref.onnx";

        std::string mapRootStr = MAA_NS::path_to_utf8_string(fs::absolute(mapRoot));
        std::string yoloModelStr = fs::exists(yoloModel) ? MAA_NS::path_to_utf8_string(fs::absolute(yoloModel)) : "";
        std::string cameraOrientationRefModelStr =
            fs::exists(cameraOrientationRefModel) ? MAA_NS::path_to_utf8_string(fs::absolute(cameraOrientationRefModel)) : "";

        LogInfo << "Auto-init: mapRoot=" << mapRootStr;
        LogInfo << "Auto-init: yoloModel=" << (yoloModelStr.empty() ? "(not found)" : yoloModelStr);
        LogInfo << "Auto-init: cameraOrientationRefModel="
                << (cameraOrientationRefModelStr.empty() ? "(not found)" : cameraOrientationRefModelStr);

        MapLocatorConfig cfg;
        cfg.map_resource_dir = mapRootStr;
        cfg.yolo_model_path = yoloModelStr;
        cfg.camera_orientation_ref_model_path = cameraOrientationRefModelStr;
        const unsigned hardwareThreads = std::thread::hardware_concurrency();
        cfg.yolo_threads = (hardwareThreads >= 8) ? 4 : ((hardwareThreads >= 4) ? 2 : 1);

        auto loc = std::make_shared<MapLocator>();
        bool ok = InitializeLocator(loc.get(), cfg);
        if (!ok) {
            LogError << "Initialize failed!";
        }

        return loc;
    }();

    return locator;
}

MaaBool MAA_CALL MapLocateRecognitionRun(
    [[maybe_unused]] MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi_param,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    const LocateOptions options = parse_custom_recognition_param<LocateOptions>(custom_recognition_param);

    LocateResult result;
    if (!try_locate_on_minimap(context, image, options, &result)) {
        return MAA_FALSE;
    }

    write_json_detail(out_detail, build_locate_output(result));

    if (result.status == LocateStatus::Success && result.position.has_value()) {
        if (out_box != nullptr) {
            *out_box = make_point_box(result.position.value());
        }
        LogInfo << "OK " << VAR(result.position->zone_id) << VAR(result.position->x) << VAR(result.position->y)
                << VAR(result.position->angle) << VAR(result.position->score) << VAR(result.position->latency_ms);
        return MAA_TRUE;
    }
    if (result.status == LocateStatus::ScreenBlocked) {
        LogWarn << "Screen Blocked";
        return MAA_FALSE;
    }

    LogWarn << "failed: " << result.debug_message;
    return MAA_FALSE;
}

MaaBool MAA_CALL MapLocateAssertLocationRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi_param,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    const auto param = parse_custom_recognition_param<MapLocateAssertLocationParam>(custom_recognition_param);

    MaaRect target_rect {};
    if (param.zone_id.empty() || !try_build_assert_rect(param, &target_rect)) {
        LogError << "MapLocateAssertLocation: invalid param" << VAR(param.zone_id) << VAR(param.target.size());
        return MAA_FALSE;
    }

    auto locator = getOrInitLocator();
    if (!locator) {
        LogError << "MapLocateAssertLocation: Locator init failed";
        return MAA_FALSE;
    }
    ResetTrackingState(locator.get());

    const mapnavigator::NaviParam navi_param;
    LocateOptions options;
    options.expected_zone_id = param.zone_id;
    options.accepted_zone_ids = mapnavigator::ZonesOnSameMap(navi_param, param.zone_id);
    options.force_global_search = true;

    LocateResult result;
    bool matched = false;

    for (int frame = 0; frame < kAssertLocateMaxFrames; ++frame) {
        const MaaImageBuffer* frame_image = frame == 0 ? image : nullptr;
        if (!try_locate_on_minimap(context, frame_image, options, &result)) {
            return MAA_FALSE;
        }

        const bool located = result.status == LocateStatus::Success && result.position.has_value();
        if (located && is_position_inside_target(result.position.value(), param.zone_id, target_rect, navi_param)) {
            matched = true;
            break;
        }

        LogInfo << "MapLocateAssertLocation waiting" << VAR(frame) << VAR(param.zone_id) << VAR(located) << VAR(result.debug_message);
        if (frame + 1 < kAssertLocateMaxFrames) {
            std::this_thread::sleep_for(kAssertLocatePollDelay);
        }
    }

    write_json_detail(out_detail, build_assert_location_output(result, param, matched));

    if (!matched) {
        LogInfo << "MapLocateAssertLocation miss" << VAR(param.zone_id) << VAR(target_rect.x) << VAR(target_rect.y)
                << VAR(target_rect.width) << VAR(target_rect.height) << VAR(result.debug_message);
        return MAA_FALSE;
    }

    if (out_box != nullptr) {
        *out_box = target_rect;
    }

    LogInfo << "MapLocateAssertLocation matched" << VAR(param.zone_id) << VAR(result.position->x) << VAR(result.position->y)
            << VAR(target_rect.x) << VAR(target_rect.y) << VAR(target_rect.width) << VAR(target_rect.height);
    return MAA_TRUE;
}

} // namespace maplocator
