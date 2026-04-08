#include <algorithm>
#include <chrono>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../action_wrapper.h"
#include "../navi_config.h"
#include "backend_turn_actuator.h"

namespace mapnavigator
{

namespace
{

std::chrono::milliseconds ResolveTouchReviewWindow(std::chrono::milliseconds elapsed)
{
    return elapsed + std::chrono::milliseconds(std::max(0, kTurnLifecycleConfig.adb_input_landing_buffer_ms));
}

} // namespace

BackendTurnActuator::BackendTurnActuator(ActionWrapper& action_wrapper, std::function<bool()> should_stop)
    : action_wrapper_(action_wrapper)
    , should_stop_(std::move(should_stop))
{
}

TurnActuationResult BackendTurnActuator::TurnByUnits(int units, int duration_millis)
{
    (void)duration_millis;

    if (units == 0) {
        return {};
    }

    if (should_stop_ && should_stop_()) {
        return {};
    }

    const auto started_at = std::chrono::steady_clock::now();
    if (!action_wrapper_.SendViewDeltaSync(units, 0)) {
        LogWarn << "BackendTurnActuator: failed to apply one-shot turn delta." << VAR(units);
        return {};
    }

    TurnActuationResult result;
    result.units_sent = units;
    result.requires_completion_tracking = action_wrapper_.uses_touch_backend();
    const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - started_at);
    result.expected_elapsed = result.requires_completion_tracking ? ResolveTouchReviewWindow(elapsed) : elapsed;

    LogInfo << "BackendTurnActuator completed one-shot turn." << VAR(units) << VAR(result.units_sent)
            << VAR(result.requires_completion_tracking) << VAR(result.expected_elapsed);
    return result;
}

} // namespace mapnavigator
