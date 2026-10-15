#pragma once

#include <chrono>
#include <cstddef>
#include <cstdint>
#include <functional>
#include <limits>
#include <optional>
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

// Position fixes that actually moved, newest last. Travel rate is a property of the agent rather than of
// any one plan, so this survives replans; only fixes past the localization quantum are kept so a held fix
// cannot dilute the estimate, and the tick counter spans the held ones so the rate stays per tick.
struct NavRunSpeedSample
{
    std::chrono::steady_clock::time_point at {};
    uint64_t tick_seq = 0;
    double x = 0.0;
    double y = 0.0;
};

struct NavRunPlan
{
    bool valid = false;
    std::string zone_id;
    size_t anchor_index = std::numeric_limits<size_t>::max();
    NaviPosition anchor_pos {};
    navmesh::WorldPath path;
    std::vector<double> corridor_arc_prefix;
    // `path` is the authored RUN line itself (corridor would only detour around water). Decided once
    // per anchor span, reused across its soft replans, reset when the anchor changes.
    bool literal = false;
    size_t cursor = 0;
    std::chrono::steady_clock::time_point planned_at {};
    std::chrono::steady_clock::time_point last_soft_replan_at {};
    int soft_replan_attempts = 0;
};

// A bend sharp enough to hold the aim at: corridor distance from the projection to its vertex, the turn accumulated
// from the current leg, and how far before the vertex the aim may start leading into it.
struct SharpCorner
{
    double distance = 0.0;
    double turn_deg = 0.0;
    double lead_distance = 0.0;
    navmesh::WorldPoint point {};
};

struct NavRunTickResult
{
    bool has_corridor_heading = false;
    double corridor_heading = 0.0;
    navmesh::WorldPoint lookahead_point {};
    // Foot of the perpendicular on the corridor. With lookahead_point it gives the corridor direction,
    // which is what turns the unsigned cross_track below into a side.
    navmesh::WorldPoint projection_point {};
    double cross_track = std::numeric_limits<double>::infinity();
    double remaining_to_anchor = std::numeric_limits<double>::infinity();
    // Straight-line agent->anchor distance, available whenever a RUN anchor exists — including the ticks
    // where the corridor is not. Finite for the whole life of an anchor, so a no-progress clock fed with
    // it never mixes two yardsticks; remaining_to_anchor is corridor arc length and must not be mixed in.
    double straight_to_anchor = std::numeric_limits<double>::infinity();
    double upcoming_turn_deg = 0.0;
    // The first such bend within kCornerBrakeScanM, if any.
    std::optional<SharpCorner> corner;
    // Corridor distance the agent covers per tick, once enough fixes have been seen to estimate it.
    std::optional<double> step_per_tick;
    NavRunReplanReason replanned_with = NavRunReplanReason::None;
    // Upcoming continuous-RUN session waypoints the corridor has carried the agent past this
    // tick. The state machine consumes these so the serial waypoint index keeps pace with
    // corridor progress even when the agent is far off the original waypoint line.
    size_t passed_run_waypoints = 0;
    // Time this tick spent in synchronous planning, with the forward key released. The caller moves its
    // no-progress clocks past it.
    std::chrono::steady_clock::duration planning {};
};

class NavRunController
{
public:
    // `halt` is called right before a synchronous plan, to release the forward key while the tick blocks.
    NavRunTickResult tick(
        NavigationSession* session,
        NavigationRuntimeState* runtime,
        const NaviPosition& position,
        const RouteTrackingState& route,
        const NaviParam& param,
        size_t anchor_index,
        const Waypoint& anchor,
        std::chrono::steady_clock::time_point now,
        const std::function<void()>& halt);

    void invalidate();

    const NavRunPlan& plan() const { return plan_; }

private:
    bool buildPlan(
        const NaviParam& param,
        const NavigationSession& session,
        const NavigationRuntimeState& runtime,
        const NaviPosition& position,
        size_t anchor_index,
        const Waypoint& anchor,
        NavRunReplanReason reason,
        std::chrono::steady_clock::time_point now,
        const std::function<void()>& halt,
        std::chrono::steady_clock::duration& planning);

    NavRunReplanReason detectReplanTrigger(const RouteTrackingState& route, std::chrono::steady_clock::time_point now) const;

    double chooseLookaheadDistance(const RouteTrackingState& route, std::optional<double> step) const;
    double chooseTurnCommitDistance(double lookahead_distance, std::optional<double> step) const;

    void recordSpeedSample(const NaviPosition& position, std::chrono::steady_clock::time_point now);

    std::optional<double> estimateStepPerTick() const;

    NavRunPlan plan_;
    std::vector<NavRunSpeedSample> speed_samples_;
    uint64_t tick_seq_ = 0;
    std::chrono::steady_clock::time_point last_progress_seen_ {};
    double last_remaining_to_anchor_ = std::numeric_limits<double>::infinity();
    size_t failed_build_anchor_ = std::numeric_limits<size_t>::max();
    std::chrono::steady_clock::time_point failed_build_at_ {};
};

} // namespace mapnavigator
