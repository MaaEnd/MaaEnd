#include <algorithm>
#include <cmath>
#include <limits>
#include <optional>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
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

double tracking_position_quantum()
{
    return std::max(kMeasurementDefaultPositionQuantum, 0.25);
}

constexpr double kStartupMotionConfirmDistance = 0.8;
// 投影值按段长归一化, 0 在段起点, 1 在段终点
constexpr double kLatchMinProjection = 0.40;
constexpr double kProjectionGrowingMin = 0.55;
constexpr double kProjectionGrowingSlack = 0.05;
constexpr double kAdvanceMinProjection = 0.90;
constexpr double kEnteredNextSegmentProjection = 1.05;
constexpr double kPassedTurnBackYawDeg = 110.0;

bool is_same_zone_segment(const Waypoint& lhs, const Waypoint& rhs)
{
    return lhs.zone_id.empty() || rhs.zone_id.empty() || lhs.zone_id == rhs.zone_id;
}

size_t find_next_position_node(const std::vector<Waypoint>& path, size_t waypoint_idx)
{
    for (size_t index = waypoint_idx + 1; index < path.size(); ++index) {
        if (path[index].hasPosition()) {
            return index;
        }
    }
    return std::numeric_limits<size_t>::max();
}

// 下一个有坐标的点与 from_idx 同在一段连续 RUN 上时返回它的下标
std::optional<size_t> next_serial_segment_end(const std::vector<Waypoint>& path, size_t from_idx)
{
    const size_t to_idx = find_next_position_node(path, from_idx);
    if (to_idx == std::numeric_limits<size_t>::max()) {
        return std::nullopt;
    }
    const Waypoint& from = path[from_idx];
    const Waypoint& to = path[to_idx];
    if (!from.isContinuousRun() || !to.isContinuousRun() || !is_same_zone_segment(from, to)) {
        return std::nullopt;
    }
    return to_idx;
}

std::optional<SegmentProjection>
    project_onto_serial_route_segment(const std::vector<Waypoint>& path, size_t from_idx, const NaviPosition& position)
{
    const std::optional<size_t> to_idx = next_serial_segment_end(path, from_idx);
    if (!to_idx) {
        return std::nullopt;
    }

    const Waypoint& from = path[from_idx];
    const Waypoint& to = path[*to_idx];
    const double segment_x = to.x - from.x;
    const double segment_y = to.y - from.y;
    const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
    if (segment_len_sq <= std::numeric_limits<double>::epsilon()) {
        return std::nullopt;
    }

    SegmentProjection projection;
    projection.from_idx = from_idx;
    projection.to_idx = *to_idx;
    projection.segment_length = std::sqrt(segment_len_sq);
    projection.raw_projection = ((position.x - from.x) * segment_x + (position.y - from.y) * segment_y) / segment_len_sq;
    projection.clamped_projection = std::clamp(projection.raw_projection, 0.0, 1.0);
    const double projected_x = from.x + projection.clamped_projection * segment_x;
    const double projected_y = from.y + projection.clamped_projection * segment_y;
    projection.cross_track_distance = std::hypot(position.x - projected_x, position.y - projected_y);
    projection.current_distance = std::hypot(from.x - position.x, from.y - position.y);
    projection.next_distance = std::hypot(to.x - position.x, to.y - position.y);
    projection.turn_back_yaw =
        std::abs(NaviMath::normalizeAngle(NaviMath::calcTargetRotation(position.x, position.y, from.x, from.y) - position.angle));
    return projection;
}

void advance_passed_run_waypoints(
    NavigationSession* session,
    RouteTrackerState* state,
    bool startup_motion_confirmed,
    const NaviPosition& position)
{
    if (session == nullptr || state == nullptr) {
        return;
    }
    if (!startup_motion_confirmed) {
        LogDebug << "Passed advance blocked before startup movement confirmed."
                 << VAR_AS(session->current_node_idx(), session->currentNodeIdx()) << VAR(position.x) << VAR(position.y)
                 << VAR(position.zone_id);
        return;
    }

    const double position_quantum = tracking_position_quantum();
    while (session->hasCurrentWaypoint()) {
        const size_t current_idx = session->currentNodeIdx();
        const std::optional<SegmentProjection> segment = project_onto_serial_route_segment(session->currentPath(), current_idx, position);
        if (!segment.has_value()) {
            state->resetTracking();
            break;
        }

        const bool same_segment = state->last_segment_from_idx == segment->from_idx && state->last_segment_to_idx == segment->to_idx;
        if (!same_segment) {
            state->resetTracking();
        }
        state->last_segment_from_idx = segment->from_idx;
        state->last_segment_to_idx = segment->to_idx;

        if (segment->cross_track_distance <= kSerialRouteDeviationFailThreshold) {
            state->best_projection_on_segment = std::max(state->best_projection_on_segment, segment->clamped_projection);
        }

        const Waypoint& next_waypoint = session->currentPathAt(segment->to_idx);
        const double next_arrival_band = next_waypoint.arrivalBand(position_quantum);
        const bool near_next_waypoint = segment->next_distance <= next_arrival_band;
        const bool entered_next_segment = segment->raw_projection >= kEnteredNextSegmentProjection;
        const bool projection_growing = state->best_projection_on_segment >= kProjectionGrowingMin
                                        && segment->clamped_projection + kProjectionGrowingSlack >= state->best_projection_on_segment;
        const bool route_fact_passed =
            segment->next_distance + position_quantum < segment->current_distance || segment->turn_back_yaw >= kPassedTurnBackYawDeg;
        const bool hard_pass_evidence = route_fact_passed || entered_next_segment || near_next_waypoint;
        const bool should_latch =
            !state->passed_waypoint_latched && segment->raw_projection >= kLatchMinProjection
            && (((segment->cross_track_distance <= kSerialRouteDeviationThreshold) && hard_pass_evidence)
                || ((segment->cross_track_distance <= kSerialRouteDeviationFailThreshold) && projection_growing && hard_pass_evidence));
        if (should_latch) {
            state->passed_waypoint_idx = current_idx;
            state->passed_waypoint_latched = true;
        }

        const bool latched_for_current = state->passed_waypoint_latched && state->passed_waypoint_idx == current_idx;
        if (!latched_for_current) {
            if (segment->cross_track_distance > kSerialRouteDeviationFailThreshold) {
                state->resetTracking();
            }
            break;
        }

        if (segment->clamped_projection < kAdvanceMinProjection && !near_next_waypoint && !entered_next_segment) {
            break;
        }

        state->resetTracking();
        session->advanceToNextWaypoint(ActionType::RUN, "passed_waypoint_advance");
    }
}

} // namespace

RouteMeasurement RouteTracker::update(NavigationSession* session, RouteTrackerState* state, const NaviPosition& position)
{
    RouteMeasurement tracking;
    if (session == nullptr || state == nullptr || !session->hasCurrentWaypoint()) {
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
    advance_passed_run_waypoints(session, state, tracking.startup_motion_confirmed, position);
    if (!session->hasCurrentWaypoint()) {
        return tracking;
    }

    const Waypoint& waypoint = session->currentWaypoint();
    if (!waypoint.hasPosition()) {
        return tracking;
    }

    tracking.valid = true;
    tracking.arrival_band = waypoint.arrivalBand(tracking_position_quantum());
    tracking.waypoint_distance = std::hypot(waypoint.x - position.x, waypoint.y - position.y);
    tracking.progress_distance = tracking.waypoint_distance;
    tracking.waypoint_heading = NaviMath::calcTargetRotation(position.x, position.y, waypoint.x, waypoint.y);
    tracking.route_heading = tracking.waypoint_heading;
    if (!waypoint.isContinuousRun()) {
        return tracking;
    }

    const std::vector<Waypoint>& path = session->currentPath();
    const std::optional<SegmentProjection> segment = project_onto_serial_route_segment(path, session->currentNodeIdx(), position);
    if (!segment.has_value()) {
        return tracking;
    }

    tracking.cross_track = segment->cross_track_distance;
    tracking.progress_distance = std::min(tracking.progress_distance, segment->next_distance);
    tracking.on_route = segment->cross_track_distance <= kSerialRouteDeviationFailThreshold;

    const bool same_runtime_segment = state->last_segment_from_idx == segment->from_idx && state->last_segment_to_idx == segment->to_idx;
    const double projection_memory = same_runtime_segment ? state->best_projection_on_segment : 0.0;
    const double projection_anchor = std::clamp(std::max(segment->clamped_projection, projection_memory), 0.0, 1.0);
    tracking.projection_anchor = projection_anchor;

    double remaining_distance = (1.0 - projection_anchor) * segment->segment_length;
    size_t cursor = segment->to_idx;
    while (cursor < path.size()) {
        const std::optional<size_t> next_idx = next_serial_segment_end(path, cursor);
        if (!next_idx) {
            break;
        }
        remaining_distance += std::hypot(path[*next_idx].x - path[cursor].x, path[*next_idx].y - path[cursor].y);
        cursor = *next_idx;
    }
    tracking.along_track_remaining = remaining_distance;

    LogDebug << "RouteTracker update." << VAR_AS(session->current_node_idx(), session->currentNodeIdx()) << VAR(position.x)
             << VAR(position.y) << VAR(waypoint.x) << VAR(waypoint.y) << VAR(segment->from_idx) << VAR(segment->to_idx)
             << VAR(segment->raw_projection) << VAR(projection_anchor) << VAR(segment->cross_track_distance)
             << VAR(tracking.waypoint_distance) << VAR(tracking.route_heading) << VAR(tracking.along_track_remaining);
    return tracking;
}

} // namespace mapnavigator
