#pragma once

#include <memory>

#include <MaaUtils/NoWarningCV.hpp>

namespace mapnavigator
{

struct AsyncPromptAction;
struct ObstacleDeviceRecovery;
class RoiTemplateScanner;

// 定位前把每帧整图交给这些扫描器, 定位失败也交, 跑在导航线程上须很轻
struct FrameSinks
{
    AsyncPromptAction* collect = nullptr;
    AsyncPromptAction* interact = nullptr;
    ObstacleDeviceRecovery* device = nullptr;
    // 指向持有者的成员, 上索扫描器可能在挂上之后才建
    const std::unique_ptr<RoiTemplateScanner>* zipline_mount = nullptr;
};

void SubmitFrameToSinks(const FrameSinks& sinks, const cv::Mat& frame);

} // namespace mapnavigator
