#pragma once

#include <cstdint>
#include <memory>

#include <opencv2/core.hpp>

#include "../navi_domain_types.h"

struct MaaContext;

namespace mapnavigator
{

class MotionController;
class PositionProvider;
class RoiTemplateScanner;
struct NavigationSession;
struct RouteTrackingState;

// 除 NotAttempted 外都已跑过子任务并用掉这次机会
enum class DeviceRemovalOutcome
{
    NotAttempted,
    NeedsFreshFix, // 已处理但定位不可用, 本拍须到此结束
    Escaped,
    StillPinned,
};

class ObstacleDeviceRecovery
{
public:
    ObstacleDeviceRecovery(
        MaaContext* maa_context,
        MotionController* motion_controller,
        PositionProvider* position_provider,
        NavigationSession* session,
        NaviPosition* position);
    ~ObstacleDeviceRecovery();

    ObstacleDeviceRecovery(const ObstacleDeviceRecovery&) = delete;
    ObstacleDeviceRecovery& operator=(const ObstacleDeviceRecovery&) = delete;

    // base_roi 取自 pipeline 节点, 交互按钮模板缺失时探测不启动
    void Start(const cv::Rect& base_roi);
    void Stop();

    // 只有已启动且正在喂帧时才生效
    void SubmitFrame(const cv::Mat& frame);

    void UpdateFeeding(int64_t stalled_ms, bool attempt_available);

    // 锚点变化时调用方须清掉旧的命中
    void ForgetObservation();

    DeviceRemovalOutcome TryRemove(const RouteTrackingState& route, const Waypoint& waypoint, bool attempt_available);

private:
    MaaContext* maa_context_;
    MotionController* motion_controller_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    NaviPosition* position_;

    std::unique_ptr<RoiTemplateScanner> scanner_;
    bool feeding_ = false;
};

} // namespace mapnavigator
