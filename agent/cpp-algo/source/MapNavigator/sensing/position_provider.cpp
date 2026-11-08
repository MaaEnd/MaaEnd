#include <chrono>
#include <cmath>

#include <MaaUtils/Logger.h>

#include "../../utils.h"
#include "../control/controller_info_utils.h"
#include "../control/controller_type_utils.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "../planning/navmesh_queries.h"
#include "frame_sinks.h"
#include "latency_observer.h"
#include "position_provider.h"

namespace mapnavigator
{

namespace
{

bool is_black_screen(const cv::Mat& image)
{
    if (image.empty()) {
        return false;
    }

    cv::Mat gray;
    switch (image.channels()) {
    case 4:
        cv::cvtColor(image, gray, cv::COLOR_BGRA2GRAY);
        break;
    case 3:
        cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
        break;
    default:
        gray = image;
        break;
    }

    cv::Scalar mean_luma;
    cv::Scalar stddev_luma;
    cv::meanStdDev(gray, mean_luma, stddev_luma);

    cv::Mat dark_mask;
    cv::threshold(gray, dark_mask, 24, 255, cv::THRESH_BINARY_INV);
    const double dark_ratio = static_cast<double>(cv::countNonZero(dark_mask)) / static_cast<double>(gray.total());

    return mean_luma[0] <= 12.0 && stddev_luma[0] <= 10.0 && dark_ratio >= 0.98;
}

void log_locate_result(const maplocator::LocateResult& locate_result)
{
    const int status = static_cast<int>(locate_result.status);
    if (locate_result.position) {
        const auto& position = *locate_result.position;
        const double cam_rot = locate_result.camRot ? locate_result.camRot->rot : -1.0;
        const double cam_rot_conf = locate_result.camRot ? locate_result.camRot->confidence : -1.0;
        LogInfo << "MapLocator" << VAR(status) << VAR(locate_result.debugMessage) << VAR(position.zoneId) << VAR(position.x)
                << VAR(position.y) << VAR(position.score) << VAR(position.sliceIndex) << VAR(position.angle) << VAR(cam_rot)
                << VAR(cam_rot_conf) << VAR(position.latencyMs);
    }
    else {
        LogInfo << "MapLocator" << VAR(status) << VAR(locate_result.debugMessage) << "position=null";
    }
}

std::optional<double> confident_camera_heading(const maplocator::LocateResult& locate_result)
{
    return locate_result.camRot && std::isfinite(locate_result.camRot->confidence)
                   && locate_result.camRot->confidence >= kNavigationCameraMinConfidence
               ? std::optional<double>(locate_result.camRot->rot)
               : std::nullopt;
}

bool wait_for_screencap(const PositionSensor& sensor)
{
    const MaaCtrlId screencap_id = MaaControllerPostScreencap(sensor.controller);
    if (screencap_id == MaaInvalidId) {
        LogWarn << "Position capture failed to dispatch screencap.";
        return false;
    }
    const MaaStatus capture_status = MaaControllerWait(sensor.controller, screencap_id);
    if (capture_status != MaaStatus_Succeeded) {
        LogWarn << "Position capture screencap did not succeed." << VAR(screencap_id) << VAR(capture_status);
        return false;
    }
    return true;
}

void fill_position(
    const PositionSensor& sensor,
    NaviPosition* out_pos,
    const maplocator::LocateResult& locate_result,
    double heading,
    std::optional<double> confident_camera,
    std::chrono::steady_clock::time_point captured_at)
{
    out_pos->x = locate_result.position->x;
    out_pos->y = locate_result.position->y;
    out_pos->angle = heading;
    out_pos->score = locate_result.position->score;
    out_pos->zone_id = locate_result.position->zoneId;
    out_pos->camera_angle = sensor.adb_like_controller
                                ? confident_camera
                                : (locate_result.camRot ? std::optional<double>(locate_result.camRot->rot) : std::nullopt);
    out_pos->valid = true;
    out_pos->timestamp = captured_at;
}

bool capture_with(PositionSensor* sensor, NaviPosition* out_pos, maplocator::LocateOptions options, HeadingSource heading_source)
{
    if (out_pos == nullptr) {
        return false;
    }

    out_pos->valid = false;
    sensor->last_capture_was_black_screen = false;
    sensor->last_capture_minimap_absent = false;
    sensor->last_locate_message = "screencap failed";
    const auto capture_started_at = std::chrono::steady_clock::now();

    if (!wait_for_screencap(*sensor)) {
        return false;
    }
    ScopedImageBuffer buffer;

    if (!MaaControllerCachedImage(sensor->controller, buffer.Get()) || MaaImageBufferIsEmpty(buffer.Get())) {
        return false;
    }
    const auto screencap_done_at = std::chrono::steady_clock::now();

    cv::Mat image = to_mat(buffer.Get());
    sensor->last_capture_was_black_screen = is_black_screen(image);
    if (options.precise_position && sensor->last_capture_was_black_screen) {
        sensor->last_locate_message = "black screen";
        return false;
    }
    if (sensor->frame_sinks.has_value()) {
        SubmitFrameToSinks(*sensor->frame_sinks, image);
    }
    cv::Mat minimap;
    if (!maplocator::TryExtractMinimap(image, sensor->adb_like_controller, &minimap)) {
        sensor->last_locate_message = "minimap not extracted";
        return false;
    }
    const auto image_prep_done_at = std::chrono::steady_clock::now();

    options.reject_occluded_frames = heading_source != HeadingSource::Camera;
    const auto locate_result = sensor->locator->locate(minimap, options);
    const auto locate_done_at = std::chrono::steady_clock::now();
    log_locate_result(locate_result);
    sensor->last_capture_minimap_absent = locate_result.minimap_absent;
    sensor->last_locate_message = locate_result.debugMessage;
    if (locate_result.status != maplocator::LocateStatus::Success || !locate_result.position) {
        return false;
    }

    if (options.precise_position
        && (!std::isfinite(locate_result.position->x) || !std::isfinite(locate_result.position->y)
            || !std::isfinite(locate_result.position->score) || locate_result.position->score < options.loc_threshold)) {
        sensor->last_locate_message = "precise position rejected";
        return false;
    }

    const std::optional<double> confident_camera = confident_camera_heading(locate_result);
    const std::optional<double> heading = heading_source == HeadingSource::Camera ? confident_camera : locate_result.rot;
    if (!heading || !std::isfinite(*heading) || *heading < 0.0 || *heading >= 360.0) {
        sensor->last_locate_message = "heading unavailable";
        return false;
    }

    // 全局搜索本就更慢, 不计入耗时统计
    if (!options.force_global_search) {
        latency::RecordStage(latency::Stage::Screencap, ElapsedMs(capture_started_at, screencap_done_at));
        latency::RecordStage(latency::Stage::ImagePrep, ElapsedMs(screencap_done_at, image_prep_done_at));
        latency::RecordStage(latency::Stage::Locate, ElapsedMs(image_prep_done_at, locate_done_at));
    }

    fill_position(*sensor, out_pos, locate_result, *heading, confident_camera, capture_started_at);

    // 所有取位都经过这里, 且 out_pos 刚由本次定位结果填好, 换算每次恰好一次
    if (sensor->normalize_param != nullptr) {
        NormalizeLivePositionToBase(*sensor->normalize_param, *out_pos);
    }
    return true;
}

maplocator::LocateOptions locate_options(
    const std::string& expected_zone_id,
    const std::vector<maplocator::SearchHint>& search_hints,
    std::optional<double> camera_heading_prior)
{
    maplocator::LocateOptions options;
    options.expected_zone_id = expected_zone_id;
    options.search_hints = search_hints;
    options.camera_heading_prior = camera_heading_prior;
    return options;
}

} // namespace

PositionSensor MakePositionSensor(MaaController* controller, std::shared_ptr<maplocator::MapLocator> locator, HeadingSource heading_source)
{
    PositionSensor sensor;
    sensor.controller = controller;
    sensor.locator = std::move(locator);
    sensor.heading_source = heading_source;
    sensor.adb_like_controller = IsAdbLikeControllerType(DetectControllerType(sensor.controller));
    return sensor;
}

bool CaptureTracked(
    PositionSensor* sensor,
    NaviPosition* out_pos,
    const std::string& expected_zone_id,
    const std::vector<maplocator::SearchHint>& search_hints,
    std::optional<double> camera_heading_prior)
{
    return capture_with(sensor, out_pos, locate_options(expected_zone_id, search_hints, camera_heading_prior), sensor->heading_source);
}

bool CaptureGlobal(
    PositionSensor* sensor,
    NaviPosition* out_pos,
    const std::string& expected_zone_id,
    const std::vector<maplocator::SearchHint>& search_hints,
    std::optional<double> camera_heading_prior)
{
    maplocator::LocateOptions options = locate_options(expected_zone_id, search_hints, camera_heading_prior);
    options.force_global_search = true;
    return capture_with(sensor, out_pos, std::move(options), sensor->heading_source);
}

bool CaptureForStrictGoal(PositionSensor* sensor, NaviPosition* out_pos, const std::string& expected_zone_id)
{
    maplocator::LocateOptions options;
    options.precise_position = true;
    options.expected_zone_id = expected_zone_id;
    return capture_with(sensor, out_pos, std::move(options), HeadingSource::Camera);
}

bool WaitForFix(
    PositionSensor* sensor,
    NaviPosition* out_pos,
    const std::string& expected_zone_id,
    int max_retries,
    int retry_interval_ms,
    const std::function<bool()>& should_stop)
{
    for (int retry = 0; retry < max_retries; ++retry) {
        if (should_stop()) {
            return false;
        }
        const bool captured =
            expected_zone_id.empty() ? CaptureTracked(sensor, out_pos, expected_zone_id) : CaptureGlobal(sensor, out_pos, expected_zone_id);
        if (captured) {
            return true;
        }
        SleepFor(retry_interval_ms);
    }
    return false;
}

void ResetSensorTracking(PositionSensor* sensor)
{
    sensor->locator->resetTrackingState();
    sensor->last_capture_was_black_screen = false;
    sensor->last_capture_minimap_absent = false;
}

} // namespace mapnavigator
