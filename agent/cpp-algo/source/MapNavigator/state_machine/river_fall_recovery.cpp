#include <cmath>

#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "river_fall_recovery.h"

namespace mapnavigator
{

RiverFallStep AdvanceRiverFallRecovery(
    RiverFallRecoveryState* state,
    ActionWrapper* action_wrapper,
    MotionController* motion_controller,
    const NaviPosition& position,
    double current_heading)
{
    const double rf_displacement = std::hypot(position.x - state->anchor_pos.x, position.y - state->anchor_pos.y);
    if (rf_displacement >= kRiverFallRecoveryClearDistance) {
        *state = {};
        LogInfo << "River-fall recovery cleared; resuming navigation." << VAR(rf_displacement) << VAR(current_heading);
        return RiverFallStep::Cleared;
    }
    motion_controller->setForwardState(false);
    // 上岸瞬间的箭头读数不可信, 先站定再算转身
    if (!state->settled) {
        state->settled = true;
        SleepFor(kRiverFallRecoverySettleMs);
        LogInfo << "River-fall recovery settling before the about-face." << VAR(rf_displacement) << VAR(position.x) << VAR(position.y);
        return RiverFallStep::Escaping;
    }
    if (!state->turned) {
        state->turned = true;
        state->water_heading = current_heading;
        const int turn_units = static_cast<int>(std::lround(180.0 * action_wrapper->defaultTurnUnitsPerDegree()));
        action_wrapper->sendViewDeltaSync(turn_units, 0);
        SleepFor(kWaitAfterFirstTurnMs);
        LogInfo << "River-fall recovery about-face issued." << VAR(state->water_heading) << VAR(turn_units) << VAR(rf_displacement);
    }
    // 角色朝向要迈一步才跟上镜头, 这一步沿相机方向离水
    action_wrapper->pulseForwardSync(kRiverFallRecoveryPulseMs);
    motion_controller->setForwardState(false);
    LogInfo << "River-fall recovery inland pulse." << VAR(current_heading) << VAR(rf_displacement) << VAR(position.x) << VAR(position.y);
    return RiverFallStep::Escaping;
}

} // namespace mapnavigator
