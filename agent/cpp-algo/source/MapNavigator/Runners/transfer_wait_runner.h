#pragma once

#include <chrono>
#include <cstdint>
#include <functional>

#include "../navigation_runtime_state.h"

namespace mapnavigator
{

class MotionController;
class PositionProvider;
struct NavigationSession;
struct NaviPosition;

class TransferWaitRunner
{
public:
    TransferWaitRunner(
        PositionProvider* position_provider,
        NavigationSession* session,
        MotionController* motion_controller,
        NaviPosition* position,
        NavigationRuntimeState* runtime_state,
        std::function<bool()> should_stop);

    bool Tick();

private:
    bool HasTimedOut(const std::chrono::steady_clock::time_point& now) const;
    bool FailTransferWait(const char* reason, const char* log_message, int64_t waited_ms) const;
    void ClearTransferWaitState();
    void SleepFor(int millis) const;

    PositionProvider* position_provider_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    NaviPosition* position_;
    NavigationRuntimeState* runtime_state_;
    std::function<bool()> should_stop_;
};

} // namespace mapnavigator
