#pragma once

#include <memory>
#include <optional>
#include <string>

#include <MaaFramework/MaaAPI.h>

#include "../../MapLocator/MapLocator.h"
#include "../navi_domain_types.h"
#include "frame_sinks.h"

namespace mapnavigator
{

struct NaviParam;

namespace latency
{

struct LatencyRun;

} // namespace latency

struct PositionSensor
{
    MaaController* controller = nullptr;
    std::shared_ptr<maplocator::MapLocator> locator;
    HeadingSource heading_source = HeadingSource::Character;
    // 移动端界面, 小地图位置和镜头朝向的取法都与桌面端不同
    bool adb_like_controller = false;
    bool last_capture_was_black_screen = false;
    bool last_capture_minimap_absent = false;
    std::string last_locate_message;
};

struct CaptureParts
{
    PositionSensor* sensor = nullptr;
    // 为空时不喂帧
    const FrameSinks* frame_sinks = nullptr;
    latency::LatencyRun* latency = nullptr;
    // 非空时期望区域按整张地图接受, 每次成功取位后用它把坐标换算到统一坐标系
    const NaviParam* normalize_param = nullptr;
};

PositionSensor MakePositionSensor(MaaController* controller, std::shared_ptr<maplocator::MapLocator> locator, HeadingSource heading_source);

// 位置与所配朝向都可用才返回 true
// camera_heading_prior 以北为 0 度, 只能传上次识别到且此后没发过转向的镜头朝向
bool CaptureTracked(
    const CaptureParts& parts,
    NaviPosition* out_pos,
    const std::string& expected_zone_id,
    const std::vector<maplocator::SearchHint>& search_hints = {},
    std::optional<double> camera_heading_prior = std::nullopt);
bool CaptureGlobal(
    const CaptureParts& parts,
    NaviPosition* out_pos,
    const std::string& expected_zone_id,
    const std::vector<maplocator::SearchHint>& search_hints = {},
    std::optional<double> camera_heading_prior = std::nullopt);
// 停车测量用, 取未平滑的定位和过了置信度检查的镜头朝向
bool CaptureForStrictGoal(const CaptureParts& parts, NaviPosition* out_pos, const std::string& expected_zone_id);
bool WaitForFix(
    const CaptureParts& parts,
    NaviPosition* out_pos,
    const std::string& expected_zone_id,
    int max_retries,
    int retry_interval_ms,
    MaaContext* context);
void ResetSensorTracking(PositionSensor* sensor);

} // namespace mapnavigator
