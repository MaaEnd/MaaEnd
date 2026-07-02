#pragma once

#include <chrono>
#include <cstddef>
#include <limits>
#include <string>

#include "navi_domain_types.h"

namespace mapnavigator
{

struct RouteTrackerState
{
    size_t passed_waypoint_idx = std::numeric_limits<size_t>::max();
    bool passed_waypoint_latched = false;
    double best_projection_on_segment = 0.0;
    size_t last_segment_from_idx = std::numeric_limits<size_t>::max();
    size_t last_segment_to_idx = std::numeric_limits<size_t>::max();
    NaviPosition startup_anchor_pos {};
    bool startup_anchor_initialized = false;
    bool startup_motion_confirmed = false;

    void ResetTracking()
    {
        passed_waypoint_idx = std::numeric_limits<size_t>::max();
        passed_waypoint_latched = false;
        best_projection_on_segment = 0.0;
        last_segment_from_idx = std::numeric_limits<size_t>::max();
        last_segment_to_idx = std::numeric_limits<size_t>::max();
    }

    void Reset()
    {
        ResetTracking();
        startup_anchor_pos = {};
        startup_anchor_initialized = false;
        startup_motion_confirmed = false;
    }
};

struct FlowState
{
    std::chrono::steady_clock::time_point navigate_started_at {};
    std::chrono::steady_clock::time_point last_auto_sprint_time {};
};

struct SemanticState
{
    std::chrono::steady_clock::time_point transfer_wait_started {};
    NaviPosition transfer_anchor_pos {};
    int transfer_stable_hits = 0;
    bool portal_transit_active = false;
    bool portal_transit_keep_moving_until_fix = false;
    bool portal_transit_needs_reacquire = false;
    std::chrono::steady_clock::time_point portal_transit_started {};
    std::string held_zone_candidate;
    int held_zone_hits = 0;

    void ResetTransient()
    {
        transfer_wait_started = {};
        transfer_anchor_pos = {};
        transfer_stable_hits = 0;
        portal_transit_active = false;
        portal_transit_keep_moving_until_fix = false;
        portal_transit_needs_reacquire = false;
        portal_transit_started = {};
        held_zone_candidate.clear();
        held_zone_hits = 0;
    }
};

struct DynamicRecoveryState
{
    NaviPosition anchor_pos {};
    std::chrono::steady_clock::time_point started_at {};
    std::chrono::steady_clock::time_point last_replan_at {};
    size_t anchor_index = std::numeric_limits<size_t>::max();
    int jump_attempt_count = 0;
    int detour_attempt_count = 0;
    bool active = false;

    void Reset()
    {
        anchor_pos = {};
        started_at = {};
        last_replan_at = {};
        anchor_index = std::numeric_limits<size_t>::max();
        jump_attempt_count = 0;
        detour_attempt_count = 0;
        active = false;
    }
};

struct LocalizationLossState
{
    std::chrono::steady_clock::time_point started_at {};
    std::chrono::steady_clock::time_point last_unstick_at {};
    bool saw_black_screen = false;

    void Reset()
    {
        started_at = {};
        last_unstick_at = {};
        saw_black_screen = false;
    }
};

// River-fall recovery latch: a black-screen loss = fell in water + force-teleport to shore facing the water.
// Armed on both re-acquire paths, consumed in TickNavigate. See navigator-river-fall-teleport-gap.
struct RiverFallRecoveryState
{
    NaviPosition anchor_pos {};
    // Post-fall facing (minimap arrow = toward water); recovery turns to water_heading + 180 to face inland.
    double water_heading = 0.0;
    bool pending = false;

    void Reset()
    {
        anchor_pos = {};
        water_heading = 0.0;
        pending = false;
    }
};

// Physical lateral-bypass escalation. Deliberately persists across recovery.Reset() so consecutive bypasses
// at the same stuck spot grow the step and alternate sides; cleared only on genuine progress (waypoint
// advance / new navigation) or once the agent has moved away from `origin`.
struct LateralBypassState
{
    NaviPosition origin {};
    int count = 0;
    bool active = false;

    void Reset()
    {
        origin = {};
        count = 0;
        active = false;
    }
};

// Previous-tick heading, used to estimate the agent's own turn rate for the steering damping term. Only the
// physical heading is tracked here; the rate is gated at the call site on the elapsed gap and on plausibility,
// so a stale entry after a recovery / relocation pause simply yields a zero rate that tick rather than a spike.
struct SteeringRateState
{
    double prev_heading_deg = 0.0;
    bool has_prev = false;
    std::chrono::steady_clock::time_point at {};

    void Reset()
    {
        prev_heading_deg = 0.0;
        has_prev = false;
        at = {};
    }
};

// Off-route wedge watchdog clock. Fed straight-line distance to the current waypoint and only run while the agent
// is off the route corridor, so it grows only during a genuine no-progress wedge that the corridor-fed stall
// clocks miss. Drives a replan, then a fail-fast.
struct OffRouteWedgeState
{
    std::chrono::steady_clock::time_point since {};
    std::chrono::steady_clock::time_point last_replan_at {};
    double best_distance = std::numeric_limits<double>::max();
    bool active = false;

    void Reset()
    {
        since = {};
        last_replan_at = {};
        best_distance = std::numeric_limits<double>::max();
        active = false;
    }
};

// Cross-tier escape. The agent fell onto a wrong FLOORED tier (one the route never planned for); we plan ONE
// navmesh corridor from that tier fix back to a reachable authored waypoint and follow it, tolerating the
// open-air shaft's tier<->base oscillation as a live guard rather than re-planning on every flip. Everything is
// gated on `active`: when false the navigator and the real-loss handling are byte-for-byte unchanged.
// `anchor_zone` is the tier we fell into; it defines the same-geometry span we tolerate flipping within. `goal_*`
// is the base-pixel rejoin waypoint (arrival exits the mode). A continuously-stuck escape is bounded at the call
// site by the hard-progress stall clock (no field needed here); a recover<->re-lose thrash is bounded by the
// top-level re-acquire streak below.
struct CrossTierEscapeState
{
    bool active = false;
    std::string anchor_zone;
    double goal_x = 0.0;
    double goal_y = 0.0;

    void Reset()
    {
        active = false;
        anchor_zone.clear();
        goal_x = 0.0;
        goal_y = 0.0;
    }
};

struct NavigationRuntimeState
{
    RouteTrackerState route;
    FlowState flow;
    SemanticState semantic;
    DynamicRecoveryState recovery;
    LocalizationLossState localization_loss;
    RiverFallRecoveryState river_fall;
    LateralBypassState bypass;
    SteeringRateState steering_rate;
    OffRouteWedgeState offroute;
    CrossTierEscapeState cross_tier_escape;
    // Consecutive global re-acquires (the navigation_state_machine "recovered via global re-acquire" path) since
    // the last genuine waypoint advance. Top-level on purpose: the loss/escape/overlay Resets that fire all through
    // a wrong-tier thrash storm never clear it — only real forward progress does — so it is the one storm-proof
    // fast-fail signal. Reset in OnWaypointAdvance / BeginNavigation only.
    int global_reacquire_streak = 0;
    bool dynamic_replan_requested = false;
    bool nav_run_dirty = true;

    void ResetNavigationAssistState()
    {
        route.ResetTracking();
        recovery.Reset();
        steering_rate.Reset();
        offroute.Reset();
        dynamic_replan_requested = false;
        nav_run_dirty = true;
    }

    void BeginNavigation(const std::chrono::steady_clock::time_point& now)
    {
        route.Reset();
        semantic.ResetTransient();
        recovery.Reset();
        localization_loss.Reset();
        river_fall.Reset();
        bypass.Reset();
        steering_rate.Reset();
        offroute.Reset();
        cross_tier_escape.Reset();
        global_reacquire_streak = 0;
        dynamic_replan_requested = false;
        nav_run_dirty = true;
        flow.navigate_started_at = now;
        flow.last_auto_sprint_time = {};
    }

    void OnWaypointAdvance()
    {
        route.ResetTracking();
        recovery.Reset();
        river_fall.Reset();
        bypass.Reset();
        offroute.Reset();
        global_reacquire_streak = 0;
        dynamic_replan_requested = false;
        nav_run_dirty = true;
        flow.last_auto_sprint_time = {};
    }
};

} // namespace mapnavigator
