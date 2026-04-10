#pragma once

#include <functional>

namespace mapnavigator
{

class ActionWrapper;
class MotionController;
struct NavigationSession;
struct NaviPosition;

class HeadingAlignRunner
{
public:
    HeadingAlignRunner(
        ActionWrapper* action_wrapper,
        NavigationSession* session,
        MotionController* motion_controller,
        NaviPosition* position,
        std::function<bool()> should_stop);

    bool Tick();

private:
    bool ConsumeHeadingNodes(bool sync_with_sensor_yaw);
    void SleepFor(int millis) const;

    ActionWrapper* action_wrapper_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    NaviPosition* position_;
    std::function<bool()> should_stop_;
};

} // namespace mapnavigator
