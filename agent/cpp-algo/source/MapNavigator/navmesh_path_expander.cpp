#include <algorithm>
#include <array>
#include <filesystem>
#include <memory>
#include <mutex>
#include <optional>
#include <string_view>
#include <unordered_map>
#include <unordered_set>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../Navmesh/BaseNavReader.h"
#include "navi_config.h"
#include "navi_controller.h"
#include "navmesh_path_expander.h"

namespace mapnavigator
{

namespace
{

struct CachedNavmesh
{
    navmesh::BaseNavPack pack;
    navmesh::BaseNavPlanner planner;

    explicit CachedNavmesh(navmesh::BaseNavPack nav_pack)
        : pack(std::move(nav_pack))
        , planner(pack)
    {
    }
};

struct NavmeshExpansionState
{
    navmesh::WorldPoint route_start;
    std::string current_zone;
    std::string navmesh_zone;
};

struct BaseNavZoneAlias
{
    std::string_view zone_id;
    std::array<std::string_view, 2> prefixes;
};

constexpr std::array<BaseNavZoneAlias, 4> kBaseNavZoneAliases {{
    { "map01base", { "map01", "ValleyIV" } },
    { "map02base", { "map02", "Wuling" } },
    { "base01", { "base01", "OMVBase" } },
    { "dung01", { "dung01", "Dung" } },
}};

bool IsNavmeshWaypoint(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::NAVMESH && waypoint.HasPosition();
}

bool ContainsNavmeshWaypoint(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), IsNavmeshWaypoint);
}

bool StartsWith(std::string_view text, std::string_view prefix)
{
    return text.size() >= prefix.size() && text.substr(0, prefix.size()) == prefix;
}

bool IsBaseNavZoneName(std::string_view zone_id)
{
    return std::any_of(kBaseNavZoneAliases.begin(), kBaseNavZoneAliases.end(), [zone_id](const BaseNavZoneAlias& alias) {
        return alias.zone_id == zone_id;
    });
}

std::string InferBaseNavZone(const std::string& locator_zone, const std::string& map_name)
{
    const std::string& source = !locator_zone.empty() ? locator_zone : map_name;
    if (IsBaseNavZoneName(source)) {
        return source;
    }
    for (const BaseNavZoneAlias& alias : kBaseNavZoneAliases) {
        if (std::any_of(alias.prefixes.begin(), alias.prefixes.end(), [&source](std::string_view prefix) {
                return StartsWith(source, prefix);
            })) {
            return std::string(alias.zone_id);
        }
    }
    return {};
}

std::optional<std::filesystem::path> FindExistingFromParents(const std::filesystem::path& relative_path)
{
    std::error_code ec;
    std::filesystem::path current = std::filesystem::current_path(ec);
    if (ec) {
        return std::nullopt;
    }

    while (!current.empty()) {
        const std::filesystem::path candidate = current / relative_path;
        if (std::filesystem::exists(candidate, ec) && !ec) {
            return candidate;
        }
        const std::filesystem::path parent = current.parent_path();
        if (parent == current) {
            break;
        }
        current = parent;
    }
    return std::nullopt;
}

std::filesystem::path ResolveNavmeshFile(const std::string& configured_path)
{
    if (!configured_path.empty()) {
        const std::filesystem::path configured(configured_path);
        if (configured.is_absolute()) {
            return configured;
        }
        if (auto found = FindExistingFromParents(configured); found.has_value()) {
            return *found;
        }
        return configured;
    }

    for (const char* relative_path : { kDefaultNavmeshRelativePath, kDefaultCompressedNavmeshRelativePath }) {
        if (auto found = FindExistingFromParents(relative_path); found.has_value()) {
            return *found;
        }
    }
    return std::filesystem::path(kDefaultNavmeshRelativePath);
}

std::shared_ptr<CachedNavmesh> LoadCachedNavmesh(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    static std::unordered_map<std::string, std::shared_ptr<CachedNavmesh>> cache;
    static std::mutex cache_mutex;

    const std::string cache_key = std::filesystem::absolute(navmesh_path).lexically_normal().string() + "#" + navmesh_zone;
    const std::lock_guard lock(cache_mutex);
    if (auto iter = cache.find(cache_key); iter != cache.end()) {
        return iter->second;
    }

    const auto load_result = navmesh::LoadBaseNavPack(navmesh_path, navmesh_zone);
    if (!load_result.ok()) {
        LogError << "Failed to load navmesh .nav file." << VAR(navmesh_path) << VAR(navmesh_zone) << VAR(load_result.message);
        return nullptr;
    }
    auto loaded = std::make_shared<CachedNavmesh>(std::move(*load_result.pack));
    cache.emplace(cache_key, loaded);
    return loaded;
}

std::vector<std::vector<double>> PathPointsForLog(const navmesh::WorldPath& path)
{
    std::vector<std::vector<double>> result;
    result.reserve(path.points.size());
    for (const navmesh::WorldPoint& point : path.points) {
        result.push_back({ point.x, point.y });
    }
    return result;
}

void UpdateStateFromRegularWaypoint(const Waypoint& waypoint, NavmeshExpansionState& state)
{
    if (waypoint.HasPosition()) {
        state.route_start = { .x = waypoint.x, .y = waypoint.y };
    }
    if (!waypoint.zone_id.empty()) {
        state.current_zone = waypoint.zone_id;
    }
}

bool AppendPlannedNavmeshWaypoints(const navmesh::WorldPath& world_path, std::vector<Waypoint>& out_path)
{
    if (world_path.points.empty()) {
        return false;
    }

    const std::unordered_set<size_t> segment_breaks(world_path.segment_breaks.begin(), world_path.segment_breaks.end());
    for (size_t index = 0; index < world_path.points.size(); ++index) {
        const navmesh::WorldPoint& point = world_path.points[index];
        if (segment_breaks.contains(index) && !out_path.empty()) {
            out_path.back().strict_arrival = true;
        }
        if (index == 0) {
            continue;
        }
        Waypoint waypoint(point.x, point.y, ActionType::RUN);
        out_path.push_back(std::move(waypoint));
    }

    if (!out_path.empty() && out_path.back().HasPosition()) {
        out_path.back().strict_arrival = true;
    }
    return true;
}

navmesh::BaseNavRouteRequest BuildRouteRequest(const NaviParam& param, const NavmeshExpansionState& state, const Waypoint& waypoint)
{
    navmesh::BaseNavRouteRequest request;
    request.zone_name = state.navmesh_zone;
    request.start = state.route_start;
    request.goal = { .x = waypoint.x, .y = waypoint.y };
    request.snap_radius = param.navmesh_snap_radius;
    request.max_cost = param.navmesh_max_cost;
    return request;
}

void LogGeneratedNavmeshPath(
    const NavmeshExpansionState& state,
    const navmesh::BaseNavRouteRequest& request,
    const navmesh::BaseNavRouteResult& route_result)
{
    const size_t triangle_count = route_result.triangles.size();
    const size_t path_point_count = route_result.path.points.size();
    const std::vector<double> navmesh_start_point { request.start.x, request.start.y };
    const std::vector<double> navmesh_goal_point { request.goal.x, request.goal.y };
    const auto navmesh_path_points = PathPointsForLog(route_result.path);
    const auto navmesh_segment_breaks = route_result.path.segment_breaks;
    LogInfo << "NAVMESH generated path." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(route_result.cost)
            << VAR(triangle_count) << VAR(path_point_count) << VAR(navmesh_start_point) << VAR(navmesh_goal_point)
            << VAR(navmesh_segment_breaks) << VAR(navmesh_path_points);
}

bool AppendNavmeshWaypoint(
    const NaviParam& param,
    const CachedNavmesh& navmesh,
    const Waypoint& waypoint,
    NavmeshExpansionState& state,
    std::vector<Waypoint>& out_path)
{
    const navmesh::BaseNavRouteRequest request = BuildRouteRequest(param, state, waypoint);
    const auto route_result = navmesh.planner.findPath(request);
    if (!route_result.ok()) {
        LogError << "Failed to plan NAVMESH waypoint." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(waypoint.x)
                 << VAR(waypoint.y) << VAR(navmesh::ToString(route_result.status));
        return false;
    }

    LogGeneratedNavmeshPath(state, request, route_result);
    if (!AppendPlannedNavmeshWaypoints(route_result.path, out_path)) {
        LogError << "NAVMESH planning returned an empty path." << VAR(state.navmesh_zone);
        return false;
    }

    state.route_start = route_result.path.points.back();
    const size_t triangle_count = route_result.triangles.size();
    const size_t path_point_count = route_result.path.points.size();
    LogInfo << "Expanded NAVMESH waypoint." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(triangle_count)
            << VAR(path_point_count);
    return true;
}

std::optional<NavmeshExpansionState> MakeExpansionState(const NaviParam& param, const NaviPosition& initial_pos)
{
    NavmeshExpansionState state;
    state.route_start = { .x = initial_pos.x, .y = initial_pos.y };
    state.current_zone = initial_pos.zone_id.empty() ? param.map_name : initial_pos.zone_id;
    state.navmesh_zone = InferBaseNavZone(state.current_zone, param.map_name);
    if (state.navmesh_zone.empty()) {
        LogError << "Failed to infer NAVMESH base zone." << VAR(state.current_zone) << VAR(param.map_name);
        return std::nullopt;
    }
    return state;
}

} // namespace

std::string InitialExpectedZone(const NaviParam& param)
{
    if (param.path.empty()) {
        return {};
    }
    const std::string expected_zone = param.path.front().zone_id.empty() ? param.map_name : param.path.front().zone_id;
    return IsBaseNavZoneName(expected_zone) ? std::string() : expected_zone;
}

bool ExpandNavmeshWaypoints(const NaviParam& param, const NaviPosition& initial_pos, std::vector<Waypoint>& out_path)
{
    if (!ContainsNavmeshWaypoint(param.path)) {
        out_path = param.path;
        return true;
    }

    auto state = MakeExpansionState(param, initial_pos);
    if (!state) {
        return false;
    }

    const std::filesystem::path navmesh_path = ResolveNavmeshFile(param.navmesh_file);
    const auto navmesh = LoadCachedNavmesh(navmesh_path, state->navmesh_zone);
    if (!navmesh) {
        return false;
    }

    out_path.clear();
    for (const Waypoint& waypoint : param.path) {
        if (waypoint.IsZoneDeclaration()) {
            state->current_zone = waypoint.zone_id;
            out_path.push_back(waypoint);
            continue;
        }
        if (!IsNavmeshWaypoint(waypoint)) {
            out_path.push_back(waypoint);
            UpdateStateFromRegularWaypoint(waypoint, *state);
            continue;
        }
        if (!AppendNavmeshWaypoint(param, *navmesh, waypoint, *state, out_path)) {
            return false;
        }
    }
    return true;
}

} // namespace mapnavigator
