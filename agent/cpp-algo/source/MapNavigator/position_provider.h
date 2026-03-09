#pragma once

#include <functional>
#include <memory>
#include <string>

#include "MaaFramework/MaaAPI.h"

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

private:
    MaaController* controller_;
    std::shared_ptr<maplocator::MapLocator> locator_;
    bool last_capture_was_held_ = false;
    bool last_capture_was_black_screen_ = false;
    int held_fix_streak_ = 0;
};

} // namespace mapnavigator
