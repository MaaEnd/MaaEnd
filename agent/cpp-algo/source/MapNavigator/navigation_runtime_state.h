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

struct NavigationRuntimeState
{
    RouteTrackerState route;
    FlowState flow;
    SemanticState semantic;
    DynamicRecoveryState recovery;
    LocalizationLossState localization_loss;
    RiverFallRecoveryState river_fall;
    SteeringRateState steering_rate;
    OffRouteWedgeState offroute;
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
        steering_rate.Reset();
        offroute.Reset();
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
        offroute.Reset();
        dynamic_replan_requested = false;
        nav_run_dirty = true;
        flow.last_auto_sprint_time = {};
    }
};

} // namespace mapnavigator
