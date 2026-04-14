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
    double projected_x = 0.0;
    double projected_y = 0.0;
    double cross_track_distance = std::numeric_limits<double>::infinity();
    double signed_cross_track = 0.0;
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
    const double rel_x = position.x - from.x;
    const double rel_y = position.y - from.y;
    projection.raw_projection = ((position.x - from.x) * segment_x + (position.y - from.y) * segment_y) / segment_len_sq;
    projection.clamped_projection = std::clamp(projection.raw_projection, 0.0, 1.0);
    projection.projected_x = from.x + projection.clamped_projection * segment_x;
    projection.projected_y = from.y + projection.clamped_projection * segment_y;
    projection.signed_cross_track = ((rel_x * segment_y) - (rel_y * segment_x)) / projection.segment_length;
    projection.cross_track_distance = std::hypot(position.x - projection.projected_x, position.y - projection.projected_y);
    projection.current_distance = std::hypot(from.x - position.x, from.y - position.y);
    projection.next_distance = std::hypot(to.x - position.x, to.y - position.y);
    projection.turn_back_yaw =
        std::abs(NaviMath::NormalizeAngle(NaviMath::CalcTargetRotation(position.x, position.y, from.x, from.y) - position.angle));
    return projection;
}

size_t FindMovementLookaheadNode(const std::vector<Waypoint>& path, size_t waypoint_idx)
{
    if (waypoint_idx >= path.size() || !IsContinuousRunWaypoint(path[waypoint_idx])) {
        return std::numeric_limits<size_t>::max();
    }

    size_t lookahead_idx = std::numeric_limits<size_t>::max();
    const Waypoint* previous_waypoint = &path[waypoint_idx];
    std::optional<double> previous_leg_yaw;
    for (size_t index = waypoint_idx + 1; index < path.size(); ++index) {
        const Waypoint& candidate = path[index];
        if (!candidate.HasPosition()) {
            continue;
        }
        if (!IsContinuousRunWaypoint(candidate) || !IsSameZoneSegment(*previous_waypoint, candidate)) {
            break;
        }

        const double leg_yaw = NaviMath::CalcTargetRotation(previous_waypoint->x, previous_waypoint->y, candidate.x, candidate.y);
        if (previous_leg_yaw.has_value() && std::abs(NaviMath::NormalizeAngle(leg_yaw - *previous_leg_yaw)) > 25.0) {
            break;
        }

        lookahead_idx = index;
        previous_leg_yaw = leg_yaw;
        previous_waypoint = &candidate;
    }

    return lookahead_idx;
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

RouteTrackingState
    RouteTracker::Update(NavigationSession* session, RouteTrackerState* state, const NaviPosition& position, double heading_degrees)
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

    tracking.seg_from = segment->from_idx;
    tracking.seg_to = segment->to_idx;
    tracking.projection = segment->raw_projection;
    tracking.cross_track = segment->cross_track_distance;
    tracking.signed_cross_track = segment->signed_cross_track;
    tracking.current_distance = segment->current_distance;
    tracking.next_distance = segment->next_distance;
    tracking.turn_back_yaw = segment->turn_back_yaw;
    tracking.progress_distance = std::min(tracking.progress_distance, segment->next_distance);
    tracking.on_route = segment->cross_track_distance <= kSerialRouteDeviationFailThreshold;
    tracking.passed_current = segment->turn_back_yaw >= 110.0 || segment->next_distance + PositionQuantum() < segment->current_distance;

    const bool same_runtime_segment = state->last_segment_from_idx == segment->from_idx && state->last_segment_to_idx == segment->to_idx;
    const double projection_memory = same_runtime_segment ? state->best_projection_on_segment : 0.0;
    const double projection_anchor = std::clamp(std::max(segment->clamped_projection, projection_memory), 0.0, 1.0);
    tracking.projection_anchor = projection_anchor;

    const Waypoint& next_waypoint = session->CurrentPathAt(segment->to_idx);
    const double virtual_t = std::clamp(projection_anchor + (kSerialRoutePathFollowLookahead / segment->segment_length), 0.0, 1.0);
    const double target_x = waypoint.x + virtual_t * (next_waypoint.x - waypoint.x);
    const double target_y = waypoint.y + virtual_t * (next_waypoint.y - waypoint.y);
    double route_heading = NaviMath::CalcTargetRotation(position.x, position.y, target_x, target_y);
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

        const double leg_distance = std::hypot(to.x - from.x, to.y - from.y);
        remaining_distance += leg_distance;
        LogDebug << "RouteTracker remaining run leg." << VAR(cursor) << VAR(next_idx) << VAR(from.x) << VAR(from.y) << VAR(to.x)
                 << VAR(to.y) << VAR(leg_distance) << VAR(remaining_distance);
        cursor = next_idx;
    }
    tracking.along_track_remaining = remaining_distance;

    const size_t lookahead_index = FindMovementLookaheadNode(session->current_path(), session->current_node_idx());
    if (lookahead_index != std::numeric_limits<size_t>::max() && lookahead_index > segment->to_idx) {
        const Waypoint& lookahead_waypoint = session->CurrentPathAt(lookahead_index);
        const double preview_window = std::max(tracking.arrival_band + kGuidanceBlendLookahead, kGuidancePreviewWindow);
        if (tracking.progress_distance <= preview_window) {
            const double lookahead_heading =
                NaviMath::CalcTargetRotation(position.x, position.y, lookahead_waypoint.x, lookahead_waypoint.y);
            const double weight = 1.0 - std::clamp(tracking.progress_distance / preview_window, 0.0, 1.0);
            route_heading = NaviMath::BlendAngle(route_heading, lookahead_heading, weight);
        }
    }

    tracking.route_heading = route_heading;
    const double heading_error = std::abs(NaviMath::NormalizeAngle(route_heading - heading_degrees));
    LogDebug << "RouteTracker update." << VAR(session->current_node_idx()) << VAR(position.x) << VAR(position.y) << VAR(position.zone_id)
             << VAR(waypoint.x) << VAR(waypoint.y) << VAR(waypoint.zone_id) << VAR(segment->from_idx) << VAR(segment->to_idx)
             << VAR(segment->segment_length) << VAR(segment->raw_projection) << VAR(segment->clamped_projection) << VAR(projection_memory)
             << VAR(projection_anchor) << VAR(tracking.startup_motion_confirmed) << VAR(segment->projected_x) << VAR(segment->projected_y)
             << VAR(segment->cross_track_distance) << VAR(segment->signed_cross_track) << VAR(segment->current_distance)
             << VAR(segment->next_distance) << VAR(tracking.waypoint_distance) << VAR(tracking.progress_distance) << VAR(lookahead_index)
             << VAR(virtual_t) << VAR(target_x) << VAR(target_y) << VAR(route_heading) << VAR(heading_error)
             << VAR(tracking.along_track_remaining);
    return tracking;
}

} // namespace mapnavigator
