#include "obstacle_device_recovery.h"

#include <filesystem>
#include <optional>
#include <vector>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../sensing/roi_template_scanner.h"
#include "controller_type_utils.h"

namespace mapnavigator
{

ObstacleDeviceRecovery MakeDeviceRecovery()
{
    return ObstacleDeviceRecovery {};
}

void StartDeviceRecovery(ObstacleDeviceRecovery* recovery, const cv::Rect& base_roi, std::string_view controller_type)
{
    if (recovery->scanner != nullptr) {
        return;
    }

    const std::vector<std::filesystem::path> roots = ResourceImageRoots(controller_type);
    cv::Mat button_template;
    if (const std::optional<std::filesystem::path> template_path = ResolveResourceImage(roots, kObstacleDeviceTemplateRelativePath)) {
        button_template = MAA_NS::imread(*template_path, cv::IMREAD_GRAYSCALE);
    }
    if (button_template.empty()) {
        LogWarn << "Blocking-device probe not started: interact button template not loaded." << VAR(kObstacleDeviceTemplateRelativePath)
                << VAR(DescribeRoots(roots));
        return;
    }

    recovery->scanner = std::make_unique<RoiTemplateScanner>(
        "device",
        base_roi,
        std::vector<ScanIcon> { ScanIcon { .templ = button_template, .threshold = kObstacleDeviceMatchThreshold } });
    LogInfo << "Blocking-device probe started." << VAR(base_roi.x) << VAR(base_roi.y) << VAR(base_roi.width) << VAR(base_roi.height);
}

void StopDeviceRecovery(ObstacleDeviceRecovery* recovery)
{
    recovery->scanner.reset();
    recovery->feeding = false;
}

void SubmitDeviceFrame(ObstacleDeviceRecovery* recovery, const cv::Mat& frame)
{
    if (recovery->scanner == nullptr || !recovery->feeding) {
        return;
    }
    recovery->scanner->submitFrame(frame);
}

void UpdateDeviceFeeding(ObstacleDeviceRecovery* recovery, int64_t stalled_ms, bool attempt_available)
{
    if (recovery->scanner == nullptr) {
        return;
    }
    const bool feed = stalled_ms > 0 && attempt_available;
    if (feed && !recovery->feeding) {
        recovery->scanner->consumeDetection(); // 重新开喂时丢掉停喂期间的命中
    }
    recovery->feeding = feed;
}

void ForgetDeviceObservation(ObstacleDeviceRecovery* recovery)
{
    if (recovery->scanner != nullptr) {
        recovery->scanner->consumeDetection();
    }
    recovery->feeding = false;
}

bool DeviceProbeRunning(const ObstacleDeviceRecovery& recovery)
{
    return recovery.scanner != nullptr;
}

bool ConsumeDeviceDetection(ObstacleDeviceRecovery* recovery)
{
    return recovery->scanner->consumeDetection();
}

} // namespace mapnavigator
