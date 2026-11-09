#pragma once

#include <cstdint>
#include <memory>
#include <string_view>

#include <opencv2/core.hpp>

#include "../navi_domain_types.h"

struct MaaContext;

namespace mapnavigator
{

struct Motion;
struct PositionSensor;
class RoiTemplateScanner;
struct Session;
struct RouteMeasurement;

// 除 NotAttempted 外都已跑过子任务并用掉这次机会
enum class DeviceRemovalOutcome
{
    NotAttempted,
    NeedsFreshFix, // 已处理但定位不可用, 本拍须到此结束
    Escaped,
    StillPinned,
    UnknownUi,
};

struct ObstacleDeviceRecovery
{
    MaaContext* maa_context;
    Motion* motion_controller;
    PositionSensor* position_provider;
    Session* session;
    NaviPosition* position;

    std::unique_ptr<RoiTemplateScanner> scanner;
    bool feeding = false;
};

ObstacleDeviceRecovery MakeDeviceRecovery(
    MaaContext* maa_context,
    Motion* motion_controller,
    PositionSensor* position_provider,
    Session* session,
    NaviPosition* position);

// base_roi 取自 pipeline 节点, 交互按钮模板缺失时探测不启动
void StartDeviceRecovery(ObstacleDeviceRecovery* recovery, const cv::Rect& base_roi, std::string_view controller_type);
void StopDeviceRecovery(ObstacleDeviceRecovery* recovery);

// 只有已启动且正在喂帧时才生效
void SubmitDeviceFrame(ObstacleDeviceRecovery* recovery, const cv::Mat& frame);

void UpdateDeviceFeeding(ObstacleDeviceRecovery* recovery, int64_t stalled_ms, bool attempt_available);

// 锚点变化时调用方须清掉旧的命中
void ForgetDeviceObservation(ObstacleDeviceRecovery* recovery);

DeviceRemovalOutcome TryRemoveDevice(ObstacleDeviceRecovery* recovery, const RouteMeasurement& route, const Waypoint& waypoint);

} // namespace mapnavigator
