#pragma once

#include <functional>
#include <string>

#include "../navigation_runtime_state.h"

namespace mapnavigator
{

class MotionController;
class PositionProvider;
struct NavigationSession;
struct NaviPosition;

class ZoneTransitionRunner
{
public:
    ZoneTransitionRunner(
        PositionProvider* position_provider,
        NavigationSession* session,
        MotionController* motion_controller,
        NaviPosition* position,
        NavigationRuntimeState* runtime_state,
        std::function<bool()> should_stop);

    bool Tick();
    bool TryFastPortalZoneTransition(const char* reason, bool require_nearby_portal = true);
    bool HandleImplicitZoneTransition(const std::string& expected_zone_id);

private:
    bool ConsumeZoneNodes(bool keep_moving_until_first_fix);
    bool ConsumeLandingPortalNode();
    bool WaitForExpectedZone(const std::string& expected_zone_id, bool keep_moving_until_first_fix);
    void SleepFor(int millis) const;

    PositionProvider* position_provider_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    NaviPosition* position_;
    NavigationRuntimeState* runtime_state_;
    std::function<bool()> should_stop_;
};

} // namespace mapnavigator
