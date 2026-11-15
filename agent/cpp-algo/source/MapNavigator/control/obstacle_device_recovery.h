#pragma once

#include <cstdint>
#include <memory>
#include <string_view>

#include <opencv2/core.hpp>

namespace mapnavigator
{

class RoiTemplateScanner;

struct ObstacleDeviceRecovery
{
    std::unique_ptr<RoiTemplateScanner> scanner;
    bool feeding = false;
};

ObstacleDeviceRecovery MakeDeviceRecovery();

// base_roi 取自 pipeline 节点, 交互按钮模板缺失时探测不启动
void StartDeviceRecovery(ObstacleDeviceRecovery* recovery, const cv::Rect& base_roi, std::string_view controller_type);
void StopDeviceRecovery(ObstacleDeviceRecovery* recovery);

// 只有已启动且正在喂帧时才生效
void SubmitDeviceFrame(ObstacleDeviceRecovery* recovery, const cv::Mat& frame);

void UpdateDeviceFeeding(ObstacleDeviceRecovery* recovery, int64_t stalled_ms, bool attempt_available);

// 锚点变化时调用方须清掉旧的命中
void ForgetDeviceObservation(ObstacleDeviceRecovery* recovery);

bool DeviceProbeRunning(const ObstacleDeviceRecovery& recovery);
bool ConsumeDeviceDetection(ObstacleDeviceRecovery* recovery);

} // namespace mapnavigator
