#include <algorithm>
#include <filesystem>
#include <thread>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>
#include <MaaUtils/Platform.h>
#ifdef _WIN32
#include <MaaUtils/SafeWindows.hpp>
#endif
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
    int x = 0;
    int y = 0;
    double rot = 0.0;
    double locConf = 0.0;
    int latencyMs = 0;

    MEO_JSONIZATION(status, message, MEO_OPT mapName, MEO_OPT x, MEO_OPT y, MEO_OPT rot, MEO_OPT locConf, MEO_OPT latencyMs)
};

struct MapLocateAssertLocationParam
{
    std::string zone_id;
    std::vector<double> target;
    double loc_threshold = MinMatchScore;
    double yolo_threshold = 0.70;
    bool force_global_search = false;

    MEO_JSONIZATION(MEO_OPT zone_id, MEO_OPT target, MEO_OPT loc_threshold, MEO_OPT yolo_threshold, MEO_OPT force_global_search)
};

struct MapLocateAssertLocationOutput
{
    int status = 0;
    bool matched = false;
    bool inTarget = false;
    std::string message;
    std::string zoneId;
    int x = 0;
    int y = 0;
    double rot = 0.0;
    double locConf = 0.0;
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
        MEO_OPT latencyMs,
        MEO_OPT target)
};

fs::path getExeDir()
{
#ifdef _WIN32
    wchar_t buf[4096] = { 0 };
    GetModuleFileNameW(nullptr, buf, 4096);
    return fs::path(buf).parent_path();
#else
    return fs::read_symlink("/proc/self/exe").parent_path();
#endif
}

class ScopedImageBuffer
{
public:
    ScopedImageBuffer()
        : buffer_(MaaImageBufferCreate())
    {
    }

    ~ScopedImageBuffer() { MaaImageBufferDestroy(buffer_); }

    ScopedImageBuffer(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer& operator=(const ScopedImageBuffer&) = delete;

    MaaImageBuffer* Get() const { return buffer_; }

private:
    MaaImageBuffer* buffer_;
};

template <typename T>
T ParseCustomRecognitionParam(const char* custom_recognition_param)
{
    if (custom_recognition_param && std::strlen(custom_recognition_param) > 0) {
        return json::parse(custom_recognition_param).value_or(json::object {}).as<T>();
    }
    return T {};
}

template <typename T>
void WriteJsonDetail(MaaStringBuffer* out_detail, const T& payload)
{
    if (out_detail == nullptr) {
        return;
    }

    const std::string json_text = json::value(payload).dumps();
    MaaStringBufferSet(out_detail, json_text.c_str());
}

LocateOutput BuildLocateOutput(const LocateResult& result)
{
    LocateOutput output;
    output.status = static_cast<int>(result.status);
    output.message = result.debugMessage;
    if (!result.position.has_value()) {
        return output;
    }

    const auto& pos = result.position.value();
    output.mapName = pos.zoneId;
    output.x = static_cast<int>(pos.x);
    output.y = static_cast<int>(pos.y);
    output.rot = pos.angle;
    output.locConf = pos.score;
    output.latencyMs = static_cast<int>(pos.latencyMs);
    return output;
}

MapLocateAssertLocationOutput BuildAssertLocationOutput(
    const LocateResult& result,
    const MapLocateAssertLocationParam& param,
    bool matched)
{
    MapLocateAssertLocationOutput output;
    output.status = static_cast<int>(result.status);
    output.matched = matched;
    output.inTarget = matched;
    output.message = result.debugMessage;
    output.zoneId = param.zone_id;
    output.target = param.target;
    if (!result.position.has_value()) {
        return output;
    }

    const auto& pos = result.position.value();
    output.x = static_cast<int>(pos.x);
    output.y = static_cast<int>(pos.y);
    output.rot = pos.angle;
    output.locConf = pos.score;
    output.latencyMs = static_cast<int>(pos.latencyMs);
    return output;
}

MaaRect MakePointBox(const MapPosition& position)
{
    return {
        static_cast<int>(position.x),
        static_cast<int>(position.y),
        1,
        1,
    };
}

LocateOptions BuildAssertLocateOptions(const MapLocateAssertLocationParam& param)
{
    LocateOptions options;
    options.loc_threshold = param.loc_threshold;
    options.yolo_threshold = param.yolo_threshold;
    options.force_global_search = param.force_global_search;
    options.expected_zone_id = param.zone_id;
    return options;
}

bool TryBuildAssertRect(const MapLocateAssertLocationParam& param, MaaRect* out_rect)
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

bool IsPositionInsideRect(const MapPosition& position, const MaaRect& rect)
{
    const double left = static_cast<double>(rect.x);
    const double top = static_cast<double>(rect.y);
    const double right = left + static_cast<double>(rect.width);
    const double bottom = top + static_cast<double>(rect.height);
    return position.x >= left && position.x < right && position.y >= top && position.y < bottom;
}

bool TryLocateOnMinimap(
    MaaContext* context,
    const MaaImageBuffer* image,
    const LocateOptions& options,
    LocateResult* out_result)
{
    if (out_result == nullptr) {
        return false;
    }

    const MaaImageBuffer* actual_image = image;
    ScopedImageBuffer captured_image;
    if (MaaImageBufferIsEmpty(actual_image)) {
        auto controller = MaaTaskerGetController(MaaContextGetTasker(context));
        const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller);
        MaaControllerWait(controller, screencap_id);
        if (!MaaControllerCachedImage(controller, captured_image.Get()) || MaaImageBufferIsEmpty(captured_image.Get())) {
            LogError << "MapLocateRecognition: Image buffer is empty";
            return false;
        }
        actual_image = captured_image.Get();
    }

    if (MaaImageBufferIsEmpty(actual_image)) {
        LogError << "MapLocateRecognition: Image buffer is empty";
        return false;
    }

    auto locator = getOrInitLocator();
    if (!locator) {
        LogError << "MapLocateAction: Locator init failed";
        return false;
    }

    cv::Mat image_mat = to_mat(actual_image);
    cv::Rect roi(MinimapROIOriginX, MinimapROIOriginY, MinimapROIWidth, MinimapROIHeight);
    roi = roi & cv::Rect(0, 0, image_mat.cols, image_mat.rows);
    if (roi.empty()) {
        LogError << "MapLocateRecognition: ROI empty";
        return false;
    }

    *out_result = locator->locate(image_mat(roi), options);
    return true;
}

} // namespace

std::shared_ptr<MapLocator> getOrInitLocator()
{
    static std::shared_ptr<MapLocator> locator = []() {
        fs::path exeDir = getExeDir();
        fs::path mapRoot = exeDir / ".." / "resource" / "image" / "MapLocator";
        fs::path yoloModel = exeDir / ".." / "resource" / "model" / "map" / "cls.onnx";

        std::string mapRootStr = MAA_NS::path_to_utf8_string(fs::absolute(mapRoot));
        std::string yoloModelStr = fs::exists(yoloModel) ? MAA_NS::path_to_utf8_string(fs::absolute(yoloModel)) : "";

        LogInfo << "Auto-init: mapRoot=" << mapRootStr;
        LogInfo << "Auto-init: yoloModel=" << (yoloModelStr.empty() ? "(not found)" : yoloModelStr);

        MapLocatorConfig cfg;
        cfg.mapResourceDir = mapRootStr;
        cfg.yoloModelPath = yoloModelStr;
        const unsigned hardwareThreads = std::thread::hardware_concurrency();
        cfg.yoloThreads = (hardwareThreads >= 8) ? 4 : ((hardwareThreads >= 4) ? 2 : 1);

        auto loc = std::make_shared<MapLocator>();
        bool ok = loc->initialize(cfg);
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
    const LocateOptions options = ParseCustomRecognitionParam<LocateOptions>(custom_recognition_param);

    LocateResult result;
    if (!TryLocateOnMinimap(context, image, options, &result)) {
        return MAA_FALSE;
    }

    WriteJsonDetail(out_detail, BuildLocateOutput(result));

    if (result.status == LocateStatus::Success && result.position.has_value()) {
        if (out_box != nullptr) {
            *out_box = MakePointBox(result.position.value());
        }
        LogInfo << "OK " << VAR(result.position->zoneId) << VAR(result.position->x) << VAR(result.position->y)
                << VAR(result.position->angle) << VAR(result.position->score) << VAR(result.position->latencyMs);
        return MAA_TRUE;
    }
    if (result.status == LocateStatus::ScreenBlocked) {
        LogWarn << "Screen Blocked";
        return MAA_FALSE;
    }

    LogWarn << "failed: " << result.debugMessage;
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
    const auto param = ParseCustomRecognitionParam<MapLocateAssertLocationParam>(custom_recognition_param);

    MaaRect target_rect {};
    if (param.zone_id.empty() || !TryBuildAssertRect(param, &target_rect)) {
        LogError << "MapLocateAssertLocation: invalid param" << VAR(param.zone_id) << VAR(param.target.size());
        return MAA_FALSE;
    }

    LocateResult result;
    if (!TryLocateOnMinimap(context, image, BuildAssertLocateOptions(param), &result)) {
        return MAA_FALSE;
    }

    const bool matched = result.status == LocateStatus::Success && result.position.has_value()
                         && result.position->zoneId == param.zone_id && IsPositionInsideRect(result.position.value(), target_rect);

    WriteJsonDetail(out_detail, BuildAssertLocationOutput(result, param, matched));

    if (!matched) {
        if (result.position.has_value()) {
            LogInfo << "MapLocateAssertLocation miss" << VAR(param.zone_id) << VAR(result.position->zoneId)
                    << VAR(result.position->x) << VAR(result.position->y) << VAR(target_rect.x) << VAR(target_rect.y)
                    << VAR(target_rect.width) << VAR(target_rect.height);
        }
        else {
            LogInfo << "MapLocateAssertLocation miss" << VAR(param.zone_id) << VAR(result.debugMessage);
        }
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
