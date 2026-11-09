#include "bootstrap.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <limits>
#include <optional>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../semantic/semantic_helpers.h"
#include "../sensing/sensitivity_observer.h"
#include "internal.h"
#include "replan.h"

namespace mapnavigator
{

namespace
{

struct BootstrapWaypointCandidate
{
    size_t index = std::numeric_limits<size_t>::max();
    double distance = std::numeric_limits<double>::infinity();
};

struct BootstrapContinueCandidate
{
    size_t continue_index = std::numeric_limits<size_t>::max();
    double route_distance = std::numeric_limits<double>::infinity();
    const char* reason = "";
};

std::optional<BootstrapContinueCandidate> find_projected_continue_candidate(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    std::optional<BootstrapContinueCandidate> best_candidate;
    for (size_t index = 0; index + 1 < path.size(); ++index) {
        const Waypoint& from = path[index];
        const Waypoint& to = path[index + 1];
        if (!IsZoneCompatible(from, position.zone_id) || !IsZoneCompatible(to, position.zone_id)) {
            continue;
        }
        if (ZonesDiffer(from.zone_id, to.zone_id)) {
            continue;
        }

        const double segment_x = to.x - from.x;
        const double segment_y = to.y - from.y;
        const double segment_len_sq = segment_x * segment_x + segment_y * segment_y;
        if (segment_len_sq <= std::numeric_limits<double>::epsilon()) {
            continue;
        }

        const double offset_x = position.x - from.x;
        const double offset_y = position.y - from.y;
        const double projection = (offset_x * segment_x + offset_y * segment_y) / segment_len_sq;
        if (projection < 0.0 || projection > 1.0) {
            continue;
        }

        const double projected_x = from.x + projection * segment_x;
        const double projected_y = from.y + projection * segment_y;
        const double route_distance = std::hypot(position.x - projected_x, position.y - projected_y);
        if (route_distance > kBootstrapOwnershipProjectionCorridor) {
            continue;
        }

        size_t continue_index = index + 1;
        const double distance_to_from = std::hypot(position.x - from.x, position.y - from.y);
        const double distance_to_to = std::hypot(position.x - to.x, position.y - to.y);
        if (projection <= kBootstrapOwnershipProjectionFrontThreshold) {
            continue_index = index;
        }
        else if (
            projection <= kBootstrapOwnershipProjectionMiddleThreshold
            && distance_to_from + kBootstrapOwnershipContinueBiasDistance < distance_to_to) {
            continue_index = index;
        }

        if (!best_candidate.has_value() || route_distance < best_candidate->route_distance) {
            best_candidate = BootstrapContinueCandidate {
                .continue_index = continue_index,
                .route_distance = route_distance,
                .reason = "projected_segment",
            };
        }
    }
    return best_candidate;
}

std::vector<BootstrapWaypointCandidate> collect_reachable_waypoints(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    std::vector<BootstrapWaypointCandidate> candidates;
    for (size_t index = 0; index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (!IsZoneCompatible(waypoint, position.zone_id)) {
            continue;
        }

        const double distance = std::hypot(position.x - waypoint.x, position.y - waypoint.y);
        if (distance > kBootstrapOwnershipMaxDistance) {
            continue;
        }

        candidates.push_back(BootstrapWaypointCandidate { .index = index, .distance = distance });
    }
    return candidates;
}

size_t rewind_to_earliest_nearby(const std::vector<Waypoint>& path, const NaviPosition& position, size_t index)
{
    const Waypoint& anchor = path[index];
    if (!anchor.has_position) {
        return index;
    }

    size_t rewound = index;
    while (rewound > 0) {
        const Waypoint& previous = path[rewound - 1];
        if (!previous.has_position || !IsZoneCompatible(previous, position.zone_id)) {
            break;
        }
        if (std::hypot(previous.x - anchor.x, previous.y - anchor.y) > kBootstrapOwnershipMaxDistance) {
            break;
        }
        --rewound;
    }

    const auto rewound_begin = path.begin() + static_cast<std::ptrdiff_t>(rewound);
    if (std::none_of(path.begin(), rewound_begin, [](const Waypoint& waypoint) { return waypoint.has_position; })) {
        return 0;
    }
    return rewound;
}

std::optional<BootstrapContinueCandidate>
    resolve_bootstrap_continue_candidate(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    const std::optional<BootstrapContinueCandidate> projected = find_projected_continue_candidate(path, position);
    if (projected.has_value()) {
        return projected;
    }

    const std::vector<BootstrapWaypointCandidate> reachable = collect_reachable_waypoints(path, position);
    if (reachable.empty()) {
        return std::nullopt;
    }

    // 取最早的一个, 环形路线首尾重合时认作起点
    const auto standing = std::find_if(reachable.begin(), reachable.end(), [](const BootstrapWaypointCandidate& candidate) {
        return candidate.distance <= kBootstrapOwnershipStandingDistance;
    });
    if (standing != reachable.end()) {
        return BootstrapContinueCandidate {
            .continue_index = standing->index,
            .route_distance = standing->distance,
            .reason = "standing_on_waypoint",
        };
    }

    const BootstrapWaypointCandidate* nearest = &reachable.front();
    for (const BootstrapWaypointCandidate& candidate : reachable) {
        if (candidate.distance < nearest->distance) {
            nearest = &candidate;
        }
    }

    std::optional<double> nearest_before;
    for (const BootstrapWaypointCandidate& candidate : reachable) {
        if (candidate.index >= nearest->index) {
            break;
        }
        nearest_before = nearest_before ? std::min(*nearest_before, candidate.distance) : candidate.distance;
    }

    if (!nearest_before.has_value() || nearest->distance + kBootstrapOwnershipDecisiveMargin < *nearest_before) {
        return BootstrapContinueCandidate {
            .continue_index = nearest->index,
            .route_distance = nearest->distance,
            .reason = "decisive_nearest",
        };
    }

    return BootstrapContinueCandidate {
        .continue_index = rewind_to_earliest_nearby(path, position, nearest->index),
        .route_distance = nearest->distance,
        .reason = "rewound_to_earliest",
    };
}

std::optional<DynamicAnchor> resolve_bootstrap_anchor(const NaviParam& param, const Session& session, const NaviPosition& position)
{
    size_t start_index = 0;
    const std::optional<BootstrapContinueCandidate> continue_candidate =
        resolve_bootstrap_continue_candidate(session.original_path, position);
    if (continue_candidate.has_value()) {
        start_index = continue_candidate->continue_index;
        LogInfo << "Bootstrap dynamic anchor scan adjusted." << VAR(continue_candidate->reason) << VAR(start_index)
                << VAR(continue_candidate->route_distance);
    }
    if (std::optional<DynamicAnchor> navmesh_anchor = ResolveReachableNavmeshAnchor(param, session, position, start_index, "bootstrap")) {
        return navmesh_anchor;
    }
    return ResolveCurrentAnchorFrom(param, session, position, start_index);
}

} // namespace

std::optional<size_t> ResolveRouteResumeIndex(const std::vector<Waypoint>& path, const NaviPosition& position)
{
    const std::optional<BootstrapContinueCandidate> candidate = resolve_bootstrap_continue_candidate(path, position);
    if (!candidate || candidate->continue_index == 0 || candidate->continue_index >= path.size()) {
        return std::nullopt;
    }
    LogInfo << "Route resume index resolved." << VAR(candidate->continue_index) << VAR(candidate->route_distance) << VAR(candidate->reason)
            << VAR(position.x) << VAR(position.y) << VAR(position.zone_id);
    return candidate->continue_index;
}

bool Bootstrap(Navigator* nav)
{
    BeginNavigation(&nav->runtime_state, std::chrono::steady_clock::now());
    sensitivity::BeginRun();

    std::optional<DynamicAnchor> anchor;
    if (nav->param->enable_bootstrap_navmesh) {
        anchor = resolve_bootstrap_anchor(*nav->param, *nav->session, *nav->position);
    }
    else {
        LogInfo << "Bootstrap navmesh planning disabled by route param." << VAR(nav->position->x) << VAR(nav->position->y)
                << VAR(nav->position->zone_id);
    }
    bool overlaid = false;
    if (anchor) {
        overlaid = TryApplyDynamicOverlayToAnchor(nav, "bootstrap_navmesh_overlay", anchor->first, anchor->second);
    }
    if (overlaid) {
        semantic_nodes::SelectPhaseOrFinishRoute(nav, "bootstrap_navmesh_overlay");
        return true;
    }

    const std::optional<BootstrapContinueCandidate> continue_candidate =
        resolve_bootstrap_continue_candidate(nav->session->original_path, *nav->position);
    if (continue_candidate && continue_candidate->continue_index > 0
        && continue_candidate->continue_index < nav->session->original_path.size()) {
        ApplyDynamicOverlay(nav->session, {}, continue_candidate->continue_index, *nav->position);
        RestartRouteTracking(&nav->runtime_state.route);
        MarkNavRunDirty(&nav->runtime_state);
        LogInfo << "Bootstrap serial route continue after navmesh overlay unavailable." << VAR(continue_candidate->continue_index)
                << VAR(continue_candidate->route_distance) << VAR(nav->position->x) << VAR(nav->position->y) << VAR(nav->position->zone_id);
        semantic_nodes::SelectPhaseOrFinishRoute(nav, "bootstrap_serial_continue");
        return true;
    }

    LogWarn << "Bootstrap ownership fallback to route head." << VAR(nav->position->x) << VAR(nav->position->y)
            << VAR(nav->position->zone_id);
    semantic_nodes::SelectPhaseOrFinishRoute(nav, "bootstrap_ready");
    return true;
}

} // namespace mapnavigator
