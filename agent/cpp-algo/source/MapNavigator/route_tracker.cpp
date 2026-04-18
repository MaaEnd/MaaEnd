#include <algorithm>
#include <cmath>
#include <limits>
#include <optional>

#include <MaaUtils/Logger.h>

#include "navi_config.h"
#include "navi_math.h"
#include "route_tracker.h"

namespace mapnavigator
{

namespace
{

struct SegmentProjection
{
    size_t from_idx = std::numeric_limits<size_t>::max();
    size_t to_idx = std::numeric_limits<size_t>::max();
    double raw_projection = 0.0;
    double clamped_projection = 0.0;
    double segment_length = 0.0;
    double cross_track_distance = std::numeric_limits<double>::infinity();
    double current_distance = std::numeric_limits<double>::infinity();
    double next_distance = std::numeric_limits<double>::infinity();
    double turn_back_yaw = 0.0;
};

double PositionQuantum()
{
    return std::max(kMeasurementDefaultPositionQuantum, 0.25);
}

constexpr double kStartupMotionConfirmDistance = 0.8;

bool IsSameZoneSegment(const Waypoint& lhs, const Waypoint& rhs)
{
    return lhs.zone_id.empty() || rhs.zone_id.empty() || lhs.zone_id == rhs.zone_id;
}

bool IsContinuousRunWaypoint(const Waypoint& waypoint)
{
    return waypoint.HasPosition() && waypoint.action == ActionType::RUN && !waypoint.RequiresStrictArrival();
}

size_t FindNextPositionNode(const std::vector<Waypoint>& path, size_t waypoint_idx)
{
    for (size_t index = waypoint_idx + 1; index < path.size(); ++index) {
        if (path[index].HasPosition()) {
            return index;
        }
    }
    return std::numeric_limits<size_t>::max();
}

std::optional<SegmentProjection>
    ProjectOntoSerialRouteSegment(const std::vector<Waypoint>& path, size_t from_idx, const NaviPosition& position)
{
    const size_t to_idx = FindNextPositionNode(path, from_idx);
    if (to_idx == std::numeric_limits<size_t>::max()) {
        return std::nullopt;
    }

    const Waypoint& from = path[from_idx];
    const Waypoint& to = path[to_idx];
    if (!IsContinuousRunWaypoint(from) || !IsContinuousRunWaypoint(to) || !IsSameZoneSegment(from, to)) {
        return std::nullopt;
    }

    const double segment_x = to.x - from.x;
    const double segment_y = to.y - from.y;
    const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
    if (segment_len_sq <= std::numeric_limits<double>::epsilon()) {
        return std::nullopt;
    }

    SegmentProjection projection;
    projection.from_idx = from_idx;
    projection.to_idx = to_idx;
    projection.segment_length = std::sqrt(segment_len_sq);
    projection.raw_projection = ((position.x - from.x) * segment_x + (position.y - from.y) * segment_y) / segment_len_sq;
    projection.clamped_projection = std::clamp(projection.raw_projection, 0.0, 1.0);
    const double projected_x = from.x + projection.clamped_projection * segment_x;
    const double projected_y = from.y + projection.clamped_projection * segment_y;
    projection.cross_track_distance = std::hypot(position.x - projected_x, position.y - projected_y);
    projection.current_distance = std::hypot(from.x - position.x, from.y - position.y);
    projection.next_distance = std::hypot(to.x - position.x, to.y - position.y);
    projection.turn_back_yaw =
        std::abs(NaviMath::NormalizeAngle(NaviMath::CalcTargetRotation(position.x, position.y, from.x, from.y) - position.angle));
    return projection;
}

bool TryAdvancePassedRunWaypoints(
    NavigationSession* session,
    RouteTrackerState* state,
    bool startup_motion_confirmed,
    const NaviPosition& position)
{
    if (session == nullptr || state == nullptr) {
        return false;
    }
    if (!startup_motion_confirmed) {
        LogDebug << "Passed advance blocked before startup movement confirmed." << VAR(session->current_node_idx()) << VAR(position.x)
                 << VAR(position.y) << VAR(position.zone_id);
        return false;
    }

    const double position_quantum = PositionQuantum();
    bool advanced = false;
    while (session->HasCurrentWaypoint()) {
        const size_t current_idx = session->current_node_idx();
        const std::optional<SegmentProjection> segment = ProjectOntoSerialRouteSegment(session->current_path(), current_idx, position);
        if (!segment.has_value()) {
            state->ResetTracking();
            break;
        }

        const bool same_segment = state->last_segment_from_idx == segment->from_idx && state->last_segment_to_idx == segment->to_idx;
        if (!same_segment) {
            state->ResetTracking();
        }
        state->last_segment_from_idx = segment->from_idx;
        state->last_segment_to_idx = segment->to_idx;

        if (segment->cross_track_distance <= kSerialRouteDeviationFailThreshold) {
            state->best_projection_on_segment = std::max(state->best_projection_on_segment, segment->clamped_projection);
        }

        const Waypoint& next_waypoint = session->CurrentPathAt(segment->to_idx);
        const double next_arrival_band = next_waypoint.GetLookahead() + kWaypointArrivalSlack + position_quantum;
        const bool projection_growing =
            state->best_projection_on_segment >= 0.55 && segment->clamped_projection + 0.05 >= state->best_projection_on_segment;
        const bool route_fact_passed =
            segment->next_distance + position_quantum < segment->current_distance || segment->turn_back_yaw >= 110.0;
        const bool hard_pass_evidence = route_fact_passed || segment->raw_projection >= 1.05 || segment->next_distance <= next_arrival_band;
        const bool should_latch =
            !state->passed_waypoint_latched && segment->raw_projection >= 0.40
            && (((segment->cross_track_distance <= kSerialRouteDeviationThreshold) && hard_pass_evidence)
                || ((segment->cross_track_distance <= kSerialRouteDeviationFailThreshold) && projection_growing && hard_pass_evidence));
        if (should_latch) {
            state->passed_waypoint_idx = current_idx;
            state->passed_waypoint_latched = true;
        }

        const bool latched_for_current = state->passed_waypoint_latched && state->passed_waypoint_idx == current_idx;
        if (!latched_for_current) {
            if (segment->cross_track_distance > kSerialRouteDeviationFailThreshold) {
                state->ResetTracking();
            }
            break;
        }

        const bool entered_next_segment = segment->raw_projection >= 1.05;
        if (segment->clamped_projection < 0.90 && segment->next_distance > next_arrival_band && !entered_next_segment) {
            break;
        }

        state->ResetTracking();
        session->AdvanceToNextWaypoint(ActionType::RUN, "passed_waypoint_advance");
        advanced = true;
    }
    return advanced;
}

} // namespace

RouteTrackingState RouteTracker::Update(NavigationSession* session, RouteTrackerState* state, const NaviPosition& position)
{
    RouteTrackingState tracking;
    if (session == nullptr || state == nullptr || !session->HasCurrentWaypoint()) {
        return tracking;
    }

    if (!state->startup_anchor_initialized) {
        state->startup_anchor_pos = position;
        state->startup_anchor_initialized = true;
    }

    if (!state->startup_motion_confirmed) {
        const bool same_zone =
            state->startup_anchor_pos.zone_id.empty() || position.zone_id.empty() || state->startup_anchor_pos.zone_id == position.zone_id;
        const double startup_displacement =
            same_zone ? std::hypot(position.x - state->startup_anchor_pos.x, position.y - state->startup_anchor_pos.y)
                      : std::numeric_limits<double>::infinity();
        if (!same_zone || startup_displacement >= kStartupMotionConfirmDistance) {
            state->startup_motion_confirmed = true;
        }
        LogDebug << "Startup motion gate." << VAR(state->startup_anchor_initialized) << VAR(state->startup_motion_confirmed)
                 << VAR(state->startup_anchor_pos.x) << VAR(state->startup_anchor_pos.y) << VAR(state->startup_anchor_pos.zone_id)
                 << VAR(position.x) << VAR(position.y) << VAR(position.zone_id) << VAR(startup_displacement);
    }

    tracking.startup_motion_confirmed = state->startup_motion_confirmed;
    TryAdvancePassedRunWaypoints(session, state, tracking.startup_motion_confirmed, position);
    if (!session->HasCurrentWaypoint()) {
        return tracking;
    }

    const Waypoint& waypoint = session->CurrentWaypoint();
    if (!waypoint.HasPosition()) {
        return tracking;
    }

    tracking.valid = true;
    tracking.arrival_band = waypoint.RequiresStrictArrival() ? waypoint.GetLookahead() + PositionQuantum()
                                                             : waypoint.GetLookahead() + kWaypointArrivalSlack + PositionQuantum();
    tracking.waypoint_distance = std::hypot(waypoint.x - position.x, waypoint.y - position.y);
    tracking.progress_distance = tracking.waypoint_distance;
    tracking.waypoint_heading = NaviMath::CalcTargetRotation(position.x, position.y, waypoint.x, waypoint.y);

    if (!IsContinuousRunWaypoint(waypoint)) {
        tracking.route_heading = tracking.waypoint_heading;
        tracking.on_route = false;
        return tracking;
    }

    const std::optional<SegmentProjection> segment =
        ProjectOntoSerialRouteSegment(session->current_path(), session->current_node_idx(), position);
    if (!segment.has_value()) {
        tracking.route_heading = tracking.waypoint_heading;
        tracking.on_route = false;
        return tracking;
    }

    tracking.cross_track = segment->cross_track_distance;
    tracking.progress_distance = std::min(tracking.progress_distance, segment->next_distance);
    tracking.on_route = segment->cross_track_distance <= kSerialRouteDeviationFailThreshold;

    const bool same_runtime_segment = state->last_segment_from_idx == segment->from_idx && state->last_segment_to_idx == segment->to_idx;
    const double projection_memory = same_runtime_segment ? state->best_projection_on_segment : 0.0;
    const double projection_anchor = std::clamp(std::max(segment->clamped_projection, projection_memory), 0.0, 1.0);
    tracking.projection_anchor = projection_anchor;

    tracking.route_heading = tracking.waypoint_heading;

    double remaining_distance = (1.0 - projection_anchor) * segment->segment_length;
    size_t cursor = segment->to_idx;
    while (cursor < session->current_path().size()) {
        const size_t next_idx = FindNextPositionNode(session->current_path(), cursor);
        if (next_idx == std::numeric_limits<size_t>::max()) {
            break;
        }

        const Waypoint& from = session->CurrentPathAt(cursor);
        const Waypoint& to = session->CurrentPathAt(next_idx);
        if (!IsContinuousRunWaypoint(from) || !IsContinuousRunWaypoint(to) || !IsSameZoneSegment(from, to)) {
            break;
        }

        remaining_distance += std::hypot(to.x - from.x, to.y - from.y);
        cursor = next_idx;
    }
    tracking.along_track_remaining = remaining_distance;

    LogDebug << "RouteTracker update." << VAR(session->current_node_idx()) << VAR(position.x) << VAR(position.y)
             << VAR(waypoint.x) << VAR(waypoint.y) << VAR(segment->from_idx) << VAR(segment->to_idx)
             << VAR(segment->raw_projection) << VAR(projection_anchor) << VAR(segment->cross_track_distance)
             << VAR(tracking.waypoint_distance) << VAR(tracking.route_heading) << VAR(tracking.along_track_remaining);
    return tracking;
}

} // namespace mapnavigator
