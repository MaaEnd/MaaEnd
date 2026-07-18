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
    PositionProvider(MaaController* controller, std::shared_ptr<maplocator::MapLocator> locator);

    bool Capture(NaviPosition* out_pos, bool force_global_search, const std::string& expected_zone_id);
    bool WaitForFix(
        NaviPosition* out_pos,
        const std::string& expected_zone_id,
        int max_retries,
        int retry_interval_ms,
        const std::function<bool()>& should_stop);
    void ResetTracking();
    bool LastCaptureWasHeld() const;
    bool LastCaptureWasBlackScreen() const;
    int HeldFixStreak() const;

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
    std::function<void(NaviPosition&)> position_normalizer_;
    std::function<void(const cv::Mat&)> frame_observer_;
    bool uses_adb_minimap_roi_ = false;
    bool last_capture_was_held_ = false;
    bool last_capture_was_black_screen_ = false;
    int held_fix_streak_ = 0;
};

} // namespace mapnavigator
