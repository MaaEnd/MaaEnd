#pragma once

#include <cstddef>
#include <cstdint>
#include <functional>
#include <optional>
#include <string>
#include <vector>

#include "../../Navmesh/BaseNavPlanner.h"
#include "../navi_domain_types.h"
#include "navmesh_diagnostics.h"

namespace mapnavigator
{

struct NaviParam;

// 按线程记录, ExpandNavmeshWaypoints 开头清空, 返回 false 后立即读取
struct NavmeshExpansionFailure
{
    std::string code;
    std::string message;
    std::optional<size_t> authored_index;
    std::string zone_id;
    std::optional<navmesh::WorldPoint> segment_start;
    std::optional<navmesh::WorldPoint> segment_goal;
    std::optional<navmesh::WorldPoint> gap_start;
    std::optional<navmesh::WorldPoint> gap_goal;
    std::optional<double> gap_distance;
    std::optional<navmesh::WorldPoint> target;
    std::string target_tier;
    std::optional<double> target_deck_y;
    std::string route_status;
    std::string route_error;
};

// 无引导时最多盲走多远, navmesh 不含水面, 终点要外延才够得着岸边目标
inline constexpr double kStartRecoveryMaxBlindWalk = 32.0;
inline constexpr double kBlindTargetMaxExtension = 30.0;

std::string InitialExpectedZone(const NaviParam& param);
void PreloadNavmeshWaypoints(const NaviParam& param);
// should_stop 在路点之间轮询, 状态机还没建好, 规划期间只有这里能响应停止
bool ExpandNavmeshWaypoints(
    const NaviParam& param,
    const NaviPosition& initial_pos,
    const std::function<bool()>& should_stop,
    std::vector<Waypoint>& out_path,
    std::vector<NavmeshRouteDiagnostic>* out_diagnostics = nullptr,
    const std::vector<VirtualNoGoDisc>* no_go = nullptr,
    std::optional<double> start_deck_y = std::nullopt);
NavmeshExpansionFailure CurrentNavmeshExpansionFailure();

struct RouteEndpoints
{
    navmesh::WorldPoint start;
    std::optional<double> start_floor_y;
    std::optional<double> start_deck_y;
    navmesh::WorldPoint goal;
    float goal_floor_y = navmesh::kBaseNavFloorYNone;
    std::optional<double> goal_deck_y;
};

// 给了 out_start_recovery_entry 时, 起点锚不上网格就改从盲走范围内最近的网格点规划, 该点由此传回
std::optional<navmesh::BaseNavRouteResult> PlanNavmeshRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const RouteEndpoints& endpoints,
    NavmeshRouteDiagnostic* out_diagnostic = nullptr,
    const std::vector<VirtualNoGoDisc>* no_go = nullptr,
    std::optional<navmesh::WorldPoint>* out_start_recovery_entry = nullptr);

struct GeneratedWaypointOptions
{
    bool include_goal = true;
    bool emit_interior_corners = false;
    bool strict_segment_breaks = true;
};

bool AppendGeneratedNavmeshWaypoints(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::BaseNavRouteResult& route,
    std::vector<Waypoint>& out_path,
    const GeneratedWaypointOptions& options);

} // namespace mapnavigator
