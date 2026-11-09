#include <algorithm>
#include <limits>
#include <optional>
#include <string>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../planning/navmesh_path_expander.h"
#include "../planning/navmesh_queries.h"
#include "internal.h"

namespace mapnavigator
{

namespace
{

struct ZoneGeometryCheck
{
    std::string zone;
    bool differs = false;
};

bool differs_in_geometry(const NaviParam& param, ZoneGeometryCheck* last, const std::string& waypoint_zone, const std::string& current_zone)
{
    if (last->zone != waypoint_zone) {
        last->zone = waypoint_zone;
        last->differs = ZonesDifferInGeometry(param, waypoint_zone, current_zone);
    }
    return last->differs;
}

} // namespace

bool ZonesDiffer(const std::string& a, const std::string& b)
{
    return !a.empty() && !b.empty() && a != b;
}

bool ZonesDifferInGeometry(const NaviParam& param, const std::string& a, const std::string& b)
{
    return ZonesDiffer(a, b) && !NavmeshZonesShareGeometry(param, a, b);
}

bool IsZoneCompatible(const Waypoint& waypoint, const std::string& current_zone_id)
{
    return waypoint.has_position && !ZonesDiffer(waypoint.zone_id, current_zone_id);
}

bool IsRequiredSemanticAnchor(const Waypoint& waypoint)
{
    if (!waypoint.has_position) {
        return IsHeadingOnly(waypoint) || IsZoneDeclaration(waypoint);
    }
    return !IsContinuousRun(waypoint);
}

std::optional<DynamicAnchor>
    ResolveCurrentAnchorFrom(const NaviParam& param, Session* session, const NaviPosition& position, size_t start_index)
{
    std::optional<DynamicAnchor> fallback;
    ZoneGeometryCheck last_check;
    const size_t path_size = session->current_path.size();
    for (size_t index = std::min(start_index, path_size); index < path_size; ++index) {
        const Waypoint& waypoint = CurrentPathAt(*session, index);
        const std::optional<size_t> canonical_index = CanonicalIndexAtCurrentPath(*session, index);
        if (!canonical_index) {
            continue;
        }
        if (IsZoneDeclaration(waypoint)) {
            if (differs_in_geometry(param, &last_check, waypoint.zone_id, position.zone_id)) {
                return fallback;
            }
            continue;
        }
        if (!waypoint.has_position) {
            if (IsRequiredSemanticAnchor(waypoint)) {
                return fallback;
            }
            continue;
        }
        if (differs_in_geometry(param, &last_check, waypoint.zone_id, position.zone_id)) {
            continue;
        }

        fallback = { *canonical_index, waypoint };
        if (IsRequiredSemanticAnchor(waypoint)) {
            return fallback;
        }
    }
    return fallback;
}

std::optional<DynamicAnchor> ResolveCurrentAnchor(const NaviParam& param, Session* session, const NaviPosition& position)
{
    return ResolveCurrentAnchorFrom(param, session, position, session->current_node_idx);
}

std::optional<DynamicAnchor> ResolveReachableNavmeshAnchor(
    const NaviParam& param,
    Session* session,
    const NaviPosition& position,
    size_t start_index,
    const char* reason)
{
    const size_t path_size = session->current_path.size();
    const std::optional<double> walked_floor_y = WalkedFloorY(*session, position);
    std::optional<DynamicAnchor> anchor;
    double anchor_cost = std::numeric_limits<double>::infinity();
    int plan_attempts = 0;
    ZoneGeometryCheck last_check;

    for (size_t index = std::min(start_index, path_size); index < path_size; ++index) {
        const Waypoint& waypoint = CurrentPathAt(*session, index);
        const std::optional<size_t> canonical_index = CanonicalIndexAtCurrentPath(*session, index);
        if (!canonical_index) {
            continue;
        }
        if (IsZoneDeclaration(waypoint)) {
            if (differs_in_geometry(param, &last_check, waypoint.zone_id, position.zone_id)) {
                break;
            }
            continue;
        }
        if (!waypoint.has_position) {
            if (IsRequiredSemanticAnchor(waypoint)) {
                break;
            }
            continue;
        }
        if (differs_in_geometry(param, &last_check, waypoint.zone_id, position.zone_id)) {
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
                .start_deck_y = LandedTowerDeckY(*session, position),
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
