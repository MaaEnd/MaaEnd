#pragma once

#include <chrono>
#include <cstddef>
#include <limits>
#include <string>

#include "navi_domain_types.h"

namespace mapnavigator
{

struct PoseEstimatorState
{
    double estimated_heading = 0.0;
    bool initialized = false;
};

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

struct ControllerState
{
    double filtered_heading_error = 0.0;

    void Reset() { filtered_heading_error = 0.0; }
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

struct RecoveryState
{
    bool armed = false;

    void Reset() { armed = false; }
};

struct NavigationRuntimeState
{
    PoseEstimatorState pose;
    RouteTrackerState route;
    ControllerState controller;
    FlowState flow;
    SemanticState semantic;
    RecoveryState recovery;

    void ResetRouteFollowState() { route.ResetTracking(); }

    void ResetControllerState() { controller.Reset(); }

    void ResetRecoveryState() { recovery.Reset(); }

    void ResetNavigationAssistState()
    {
        route.ResetTracking();
        controller.Reset();
        recovery.Reset();
    }

    void BeginNavigation(const std::chrono::steady_clock::time_point& now)
    {
        route.Reset();
        controller.Reset();
        recovery.Reset();
        semantic.ResetTransient();
        flow.navigate_started_at = now;
        flow.last_auto_sprint_time = {};
    }

    void OnWaypointAdvance()
    {
        route.ResetTracking();
        controller.Reset();
        recovery.Reset();
        flow.last_auto_sprint_time = {};
    }
};

} // namespace mapnavigator
