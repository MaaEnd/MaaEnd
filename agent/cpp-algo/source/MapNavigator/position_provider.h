#pragma once

#include <functional>
#include <memory>
#include <string>

#include <MaaFramework/MaaAPI.h>

#include "../MapLocator/MapLocator.h"
#include "navi_domain_types.h"

namespace mapnavigator
{

class PositionProvider
{
public:
    PositionProvider(MaaController* controller, std::shared_ptr<maplocator::MapLocator> locator, HeadingSource heading_source);

    // Succeeds only when both position and the configured heading are usable.
    // search_hints: 调用方知道人大概在哪时（滑索落点等）交给定位器多搜几个小窗，见 SearchHint。
    // camera_heading_prior: 预期镜头方位角（北为 0 度）。它是观测而非指令，只允许传上一次成功识别到的
    // 镜头方位角，且调用方必须确认自此没有任何转向指令——判据见 NavigationStateMachine 的朝向纪元。
    bool Capture(
        NaviPosition* out_pos,
        bool force_global_search,
        const std::string& expected_zone_id,
        const std::vector<maplocator::SearchHint>& search_hints = {},
        std::optional<double> camera_heading_prior = std::nullopt);
    bool WaitForFix(
        NaviPosition* out_pos,
        const std::string& expected_zone_id,
        int max_retries,
        int retry_interval_ms,
        const std::function<bool()>& should_stop);
    void ResetTracking();
    bool LastCaptureWasBlackScreen() const;

    // Optional post-locate hook: maps every successful fix onto a common coordinate frame at the single
    // capture chokepoint (so every consumer — WaitForFix, the state machine, semantic nodes — sees the
    // same frame). Defaults to absent, i.e. the raw locator output is passed through unchanged.
    void SetPositionNormalizer(std::function<void(NaviPosition&)> normalizer);

    // Optional per-frame observer: invoked with the full captured BGR/BGRA frame at the single capture
    // chokepoint, BEFORE minimap extraction or locate, so it fires on every frame even when localization
    // fails. Used to feed the async collectible scanner without a second screencap. The callback runs on
    // the nav thread and must be cheap (it just copies a ROI out to a worker).
    void SetFrameObserver(std::function<void(const cv::Mat&)> observer);

private:
    MaaController* controller_;
    std::shared_ptr<maplocator::MapLocator> locator_;
    const HeadingSource heading_source_;
    std::function<void(NaviPosition&)> position_normalizer_;
    std::function<void(const cv::Mat&)> frame_observer_;
    bool uses_adb_minimap_roi_ = false;
    bool last_capture_was_black_screen_ = false;
};

} // namespace mapnavigator
