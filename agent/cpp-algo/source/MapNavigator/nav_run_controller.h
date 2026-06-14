#pragma once

#include <chrono>
#include <cstddef>
#include <cstdint>
#include <limits>
#include <string>
#include <vector>

#include "../Navmesh/BaseNavPlanner.h"
#include "navi_domain_types.h"

namespace mapnavigator
{

struct NaviParam;
struct NavigationSession;
struct NavigationRuntimeState;
struct RouteTrackingState;

enum class NavRunReplanReason
{
    None,
    AnchorChanged,
    ZoneChanged,
    OffCorridor,
    ProgressRegression,
};

struct NavRunPlan
{
    bool valid = false;
    std::string zone_id;
    size_t anchor_index = std::numeric_limits<size_t>::max();
    NaviPosition anchor_pos {};
    navmesh::WorldPath path;
    std::vector<double> corridor_arc_prefix;
    size_t cursor = 0;
    std::chrono::steady_clock::time_point planned_at {};
    std::chrono::steady_clock::time_point last_soft_replan_at {};
    int soft_replan_attempts = 0;
};

struct NavRunTickResult
{
    bool has_corridor_heading = false;
    double corridor_heading = 0.0;
    navmesh::WorldPoint lookahead_point {};
    double cross_track = std::numeric_limits<double>::infinity();
    double remaining_to_anchor = std::numeric_limits<double>::infinity();
    double upcoming_turn_deg = 0.0;
    NavRunReplanReason replanned_with = NavRunReplanReason::None;
    // Upcoming continuous-RUN session waypoints the corridor has carried the agent past this
    // tick. The state machine consumes these so the serial waypoint index keeps pace with
    // corridor progress even when the agent is far off the original waypoint line.
    size_t passed_run_waypoints = 0;
};

class NavRunController
{
public:
    NavRunTickResult tick(
        NavigationSession* session,
        NavigationRuntimeState* runtime,
        const NaviPosition& position,
        const RouteTrackingState& route,
        const NaviParam& param,
        size_t anchor_index,
        const Waypoint& anchor,
        bool sprint_active,
        std::chrono::steady_clock::time_point now);

    void invalidate();

    const NavRunPlan& plan() const { return plan_; }

private:
    bool buildPlan(
        const NaviParam& param,
        const NaviPosition& position,
        size_t anchor_index,
        const Waypoint& anchor,
        NavRunReplanReason reason,
        std::chrono::steady_clock::time_point now);

    NavRunReplanReason detectReplanTrigger(const RouteTrackingState& route, std::chrono::steady_clock::time_point now) const;

    double chooseLookaheadDistance(const RouteTrackingState& route, bool sprint_active, double upcoming_turn_deg) const;

    NavRunPlan plan_;
    std::chrono::steady_clock::time_point last_progress_seen_ {};
    double last_remaining_to_anchor_ = std::numeric_limits<double>::infinity();
};

} // namespace mapnavigator
