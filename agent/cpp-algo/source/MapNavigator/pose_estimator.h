#pragma once

#include "navi_domain_types.h"
#include "navigation_runtime_state.h"

namespace mapnavigator
{

struct PoseEstimate
{
    NaviPosition filtered_position {};
    double estimated_heading = 0.0;
    bool degraded_fix = false;
};

class PoseEstimator
{
public:
    static PoseEstimate Update(const NaviPosition& raw_pose, bool held_fix, bool black_screen, PoseEstimatorState* state);

    static void NotifyAppliedSteering(PoseEstimatorState* state, double yaw_delta_deg);
};

} // namespace mapnavigator
