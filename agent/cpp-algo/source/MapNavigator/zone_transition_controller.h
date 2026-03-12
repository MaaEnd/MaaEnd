#pragma once

#include <functional>
#include <string>

namespace mapnavigator
{

class LocalDriverLite;
class MotionController;
class PositionProvider;
struct NavigationSession;
struct NaviPosition;

class ZoneTransitionController
{
public:
    ZoneTransitionController(
        MotionController* motion_controller,
        PositionProvider* position_provider,
        NavigationSession* session,
        NaviPosition* position,
        LocalDriverLite* local_driver,
        std::function<bool()> should_stop);

    bool ConsumeZoneNodes(bool keep_moving_until_first_fix);
    bool ConsumeLandingPortalNode();

private:
    bool WaitForExpectedZone(const std::string& expected_zone_id, bool keep_moving_until_first_fix);

    MotionController* motion_controller_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    NaviPosition* position_;
    LocalDriverLite* local_driver_;
    std::function<bool()> should_stop_;
};

} // namespace mapnavigator
