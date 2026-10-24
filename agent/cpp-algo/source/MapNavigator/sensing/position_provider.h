#pragma once

#include <functional>
#include <memory>
#include <string>

#include <MaaFramework/MaaAPI.h>

#include "../../MapLocator/MapLocator.h"
#include "../navi_domain_types.h"

namespace mapnavigator
{

class PositionProvider
{
public:
    PositionProvider(MaaController* controller, std::shared_ptr<maplocator::MapLocator> locator, HeadingSource heading_source);

    // 位置与所配朝向都可用才返回 true
    // camera_heading_prior 以北为 0 度, 只能传上次识别到且此后没发过转向的镜头朝向
    bool Capture(
        NaviPosition* out_pos,
        bool force_global_search,
        const std::string& expected_zone_id,
        const std::vector<maplocator::SearchHint>& search_hints = {},
        std::optional<double> camera_heading_prior = std::nullopt);
    // 停车测量用, 取未平滑的定位和过了置信度检查的镜头朝向
    bool captureForStrictGoal(NaviPosition* out_pos, const std::string& expected_zone_id);
    bool WaitForFix(
        NaviPosition* out_pos,
        const std::string& expected_zone_id,
        int max_retries,
        int retry_interval_ms,
        const std::function<bool()>& should_stop);
    void ResetTracking();
    bool LastCaptureWasBlackScreen() const;

    // 每次成功取位后调用一次, 把坐标换算到统一坐标系
    void SetPositionNormalizer(std::function<void(NaviPosition&)> normalizer);

    // 定位前对每帧整图回调, 定位失败也会调, 跑在导航线程上须很轻
    void SetFrameObserver(std::function<void(const cv::Mat&)> observer);

private:
    bool captureImpl(
        NaviPosition* out_pos,
        bool force_global_search,
        const std::string& expected_zone_id,
        const std::vector<maplocator::SearchHint>& search_hints,
        HeadingSource heading_source,
        bool precise_position,
        std::optional<double> camera_heading_prior = std::nullopt);

    MaaController* controller_;
    std::shared_ptr<maplocator::MapLocator> locator_;
    const HeadingSource heading_source_;
    std::function<void(NaviPosition&)> position_normalizer_;
    std::function<void(const cv::Mat&)> frame_observer_;
    bool uses_adb_minimap_roi_ = false;
    bool last_capture_was_black_screen_ = false;
};

} // namespace mapnavigator
