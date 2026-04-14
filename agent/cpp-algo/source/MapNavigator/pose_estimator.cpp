#include <algorithm>
#include <cmath>

#include "navi_math.h"
#include "pose_estimator.h"

namespace mapnavigator
{

PoseEstimate PoseEstimator::Update(const NaviPosition& raw_pose, bool held_fix, bool black_screen, PoseEstimatorState* state)
{
    PoseEstimate estimate;
    estimate.filtered_position = raw_pose;
    estimate.degraded_fix = held_fix || black_screen || !raw_pose.valid;

    const double raw_heading = NaviMath::NormalizeAngle(raw_pose.angle);
    if (state == nullptr) {
        estimate.estimated_heading = raw_heading;
        estimate.filtered_position.angle = estimate.estimated_heading;
        return estimate;
    }

    if (!state->initialized) {
        state->estimated_heading = raw_heading;
        state->initialized = true;
    }

    const double heading_mismatch = std::abs(NaviMath::NormalizeAngle(raw_heading - state->estimated_heading));
    if (!estimate.degraded_fix) {
        const double correction_alpha = heading_mismatch <= 10.0 ? 0.22 : (heading_mismatch <= 20.0 ? 0.10 : 0.0);
        if (correction_alpha > 0.0) {
            state->estimated_heading = NaviMath::BlendAngle(state->estimated_heading, raw_heading, correction_alpha);
        }
    }

    estimate.estimated_heading = state->estimated_heading;
    estimate.filtered_position.angle = estimate.estimated_heading;
    return estimate;
}

void PoseEstimator::NotifyAppliedSteering(PoseEstimatorState* state, double yaw_delta_deg)
{
    if (state == nullptr) {
        return;
    }

    state->estimated_heading = NaviMath::NormalizeAngle(state->estimated_heading + yaw_delta_deg);
    state->initialized = true;
}

} // namespace mapnavigator
