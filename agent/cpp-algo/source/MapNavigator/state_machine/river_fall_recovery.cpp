#include <cmath>

#include <MaaUtils/Logger.h>

#include "../control/action_wrapper.h"
#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "river_fall_recovery.h"

namespace mapnavigator
{

RiverFallRecovery::RiverFallRecovery(
    RiverFallRecoveryState& state,
    ActionWrapper* action_wrapper,
    MotionController* motion_controller,
    const NaviPosition* position)
    : state_(state)
    , action_wrapper_(action_wrapper)
    , motion_controller_(motion_controller)
    , position_(position)
{
}

RiverFallRecovery::Step RiverFallRecovery::advance(double current_heading)
{
    RiverFallRecoveryState& rf = state_;
    const double rf_displacement = std::hypot(position_->x - rf.anchor_pos.x, position_->y - rf.anchor_pos.y);
    if (rf_displacement >= kRiverFallRecoveryClearDistance) {
        rf.reset();
        LogInfo << "River-fall recovery cleared; resuming navigation." << VAR(rf_displacement) << VAR(current_heading);
        return Step::Cleared;
    }
    motion_controller_->setForwardState(false);
    // 上岸瞬间的箭头读数不可信, 先站定再算转身
    if (!rf.settled) {
        rf.settled = true;
        SleepFor(kRiverFallRecoverySettleMs);
        LogInfo << "River-fall recovery settling before the about-face." << VAR(rf_displacement) << VAR(position_->x) << VAR(position_->y);
        return Step::Escaping;
    }
    if (!rf.turned) {
        rf.turned = true;
        rf.water_heading = current_heading;
        const int turn_units = static_cast<int>(std::lround(180.0 * action_wrapper_->defaultTurnUnitsPerDegree()));
        action_wrapper_->sendViewDeltaSync(turn_units, 0);
        SleepFor(kWaitAfterFirstTurnMs);
        LogInfo << "River-fall recovery about-face issued." << VAR(rf.water_heading) << VAR(turn_units) << VAR(rf_displacement);
    }
    // 角色朝向要迈一步才跟上镜头, 这一步沿相机方向离水
    action_wrapper_->pulseForwardSync(kRiverFallRecoveryPulseMs);
    motion_controller_->setForwardState(false);
    LogInfo << "River-fall recovery inland pulse." << VAR(current_heading) << VAR(rf_displacement) << VAR(position_->x)
            << VAR(position_->y);
    return Step::Escaping;
}

} // namespace mapnavigator
