#include <algorithm>
#include <limits>
#include <optional>
#include <string>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../planning/navmesh_path_expander.h"
#include "internal.h"

namespace mapnavigator
{

bool ZonesDiffer(const std::string& a, const std::string& b)
{
    return !a.empty() && !b.empty() && a != b;
}

bool IsZoneCompatible(const Waypoint& waypoint, const std::string& current_zone_id)
{
    return waypoint.hasPosition() && !ZonesDiffer(waypoint.zone_id, current_zone_id);
}

bool IsRequiredSemanticAnchor(const Waypoint& waypoint)
{
    if (!waypoint.hasPosition()) {
        return waypoint.isHeadingOnly() || waypoint.isZoneDeclaration();
    }
    return !waypoint.isContinuousRun();
}

std::optional<DynamicAnchor> ResolveCurrentAnchorFrom(NavigationSession* session, const NaviPosition& position, size_t start_index)
{
    std::optional<DynamicAnchor> fallback;
    const size_t path_size = session->currentPath().size();
    for (size_t index = std::min(start_index, path_size); index < path_size; ++index) {
        const Waypoint& waypoint = session->currentPathAt(index);
        const std::optional<size_t> canonical_index = session->canonicalIndexAtCurrentPath(index);
        if (!canonical_index) {
            continue;
        }
        if (waypoint.isZoneDeclaration()) {
            if (ZonesDiffer(waypoint.zone_id, position.zone_id)) {
                return fallback;
            }
            continue;
        }
        if (!waypoint.hasPosition()) {
            if (IsRequiredSemanticAnchor(waypoint)) {
                return fallback;
            }
            continue;
        }
        if (!IsZoneCompatible(waypoint, position.zone_id)) {
            continue;
        }

        fallback = { *canonical_index, waypoint };
        if (IsRequiredSemanticAnchor(waypoint)) {
            return fallback;
        }
    }
    return fallback;
}

std::optional<DynamicAnchor> ResolveCurrentAnchor(NavigationSession* session, const NaviPosition& position)
{
    return ResolveCurrentAnchorFrom(session, position, session->currentNodeIdx());
}

std::optional<DynamicAnchor> ResolveReachableNavmeshAnchor(
    const NaviParam& param,
    NavigationSession* session,
    const NaviPosition& position,
    size_t start_index,
    const char* reason)
{
    const size_t path_size = session->currentPath().size();
    const std::optional<double> walked_floor_y = session->walkedFloorY(position);
    std::optional<DynamicAnchor> anchor;
    double anchor_cost = std::numeric_limits<double>::infinity();
    int plan_attempts = 0;

    for (size_t index = std::min(start_index, path_size); index < path_size; ++index) {
        const Waypoint& waypoint = session->currentPathAt(index);
        const std::optional<size_t> canonical_index = session->canonicalIndexAtCurrentPath(index);
        if (!canonical_index) {
            continue;
        }
        if (waypoint.isZoneDeclaration()) {
            if (ZonesDiffer(waypoint.zone_id, position.zone_id)) {
                break;
            }
            continue;
        }
        if (!waypoint.hasPosition()) {
            if (IsRequiredSemanticAnchor(waypoint)) {
                break;
            }
            continue;
        }
        if (!IsZoneCompatible(waypoint, position.zone_id)) {
            continue;
        }

        const navmesh::WorldPoint start { .x = position.x, .y = position.y };
        const navmesh::WorldPoint goal { .x = waypoint.x, .y = waypoint.y };
        ++plan_attempts;
        const auto route = PlanNavmeshRoute(
            param,
            position.zone_id,
            {
                .start = start,
                .start_floor_y = walked_floor_y,
                .start_deck_y = session->landedTowerDeckY(position),
                .goal = goal,
                .goal_deck_y = waypoint.target_deck_y,
            });
        if (route) {
            anchor_cost = route->cost;
            anchor = { *canonical_index, waypoint };
            break;
        }
        if (IsRequiredSemanticAnchor(waypoint)) {
            break;
        }
        // 每次不可达都要跑满一次秒级 A*
        if (plan_attempts >= kReachableAnchorPlanAttemptsMax) {
            LogWarn << "Reachable navmesh anchor scan exhausted its plan budget." << VAR(reason) << VAR(plan_attempts) << VAR(index)
                    << VAR(path_size);
            break;
        }
    }

    if (anchor) {
        LogInfo << "Reachable navmesh anchor selected." << VAR(reason) << VAR(anchor->first) << VAR(anchor_cost) << VAR(plan_attempts)
                << VAR(start_index);
    }
    return anchor;
}

} // namespace mapnavigator
