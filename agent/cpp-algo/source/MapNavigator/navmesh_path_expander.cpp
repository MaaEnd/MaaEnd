#include <algorithm>
#include <array>
#include <cmath>
#include <condition_variable>
#include <cstddef>
#include <deque>
#include <exception>
#include <filesystem>
#include <future>
#include <limits>
#include <memory>
#include <mutex>
#include <optional>
#include <string>
#include <string_view>
#include <thread>
#include <unordered_map>
#include <unordered_set>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../Navmesh/BaseNavReader.h"
#include "navi_config.h"
#include "navi_controller.h"
#include "navi_math.h"
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

constexpr std::array<double, 3> kDetourRadii { 3.0, 5.0, 7.0 };
constexpr std::array<double, 8> kDetourHeadingOffsets { 30.0, -30.0, 50.0, -50.0, 70.0, -70.0, 90.0, -90.0 };
constexpr double kDetourSnapRadius = 4.0;
constexpr double kDetourBlockedForwardDistance = 6.0;
constexpr size_t kDetourBlockedTriangleCount = 4;
constexpr double kDetourBacktrackPenalty = 8.0;
constexpr double kDetourSnapPenalty = 3.0;

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

std::string BuildNavmeshCacheKey(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    return std::filesystem::absolute(navmesh_path).lexically_normal().string() + "#" + navmesh_zone;
}

std::shared_ptr<CachedNavmesh> LoadNavmeshPack(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    const auto load_result = navmesh::LoadBaseNavPack(navmesh_path, navmesh_zone);
    if (!load_result.ok()) {
        LogError << "Failed to load navmesh .nav file." << VAR(navmesh_path) << VAR(navmesh_zone) << VAR(load_result.message);
        return nullptr;
    }
    return std::make_shared<CachedNavmesh>(std::move(*load_result.pack));
}

using NavmeshFuture = std::shared_future<std::shared_ptr<CachedNavmesh>>;
using NavmeshTask = std::packaged_task<std::shared_ptr<CachedNavmesh>()>;

struct NavmeshTaskQueue
{
    NavmeshTaskQueue()
        : worker([this] { Run(); })
    {
    }

    ~NavmeshTaskQueue()
    {
        {
            const std::lock_guard lock(mutex);
            stopping = true;
        }
        cv.notify_one();
        if (worker.joinable()) {
            worker.join();
        }
    }

    NavmeshTaskQueue(const NavmeshTaskQueue&) = delete;
    NavmeshTaskQueue& operator=(const NavmeshTaskQueue&) = delete;

    void Run()
    {
        while (true) {
            NavmeshTask task;
            {
                std::unique_lock lock(mutex);
                cv.wait(lock, [this] { return stopping || !tasks.empty(); });
                if (stopping && tasks.empty()) {
                    return;
                }
                task = std::move(tasks.front());
                tasks.pop_front();
            }
            task();
        }
    }

    std::mutex mutex;
    std::condition_variable cv;
    std::deque<NavmeshTask> tasks;
    std::thread worker;
    bool stopping = false;
};

std::unordered_map<std::string, NavmeshFuture>& NavmeshFutureCache()
{
    static std::unordered_map<std::string, NavmeshFuture> cache;
    return cache;
}

std::mutex& NavmeshFutureCacheMutex()
{
    static std::mutex mutex;
    return mutex;
}

void RemoveCachedNavmeshFutureByKey(const std::string& cache_key)
{
    const std::lock_guard lock(NavmeshFutureCacheMutex());
    NavmeshFutureCache().erase(cache_key);
}

class NavmeshCacheExceptionGuard
{
public:
    explicit NavmeshCacheExceptionGuard(std::string cache_key)
        : cache_key_(std::move(cache_key))
        , uncaught_exceptions_(std::uncaught_exceptions())
    {
    }

    ~NavmeshCacheExceptionGuard()
    {
        if (active_ && std::uncaught_exceptions() > uncaught_exceptions_) {
            RemoveCachedNavmeshFutureByKey(cache_key_);
        }
    }

    NavmeshCacheExceptionGuard(const NavmeshCacheExceptionGuard&) = delete;
    NavmeshCacheExceptionGuard& operator=(const NavmeshCacheExceptionGuard&) = delete;

    void Dismiss() { active_ = false; }

private:
    std::string cache_key_;
    int uncaught_exceptions_ = 0;
    bool active_ = true;
};

NavmeshTaskQueue& GetNavmeshTaskQueue()
{
    static NavmeshTaskQueue queue;
    return queue;
}

NavmeshFuture EnqueueNavmeshLoad(std::filesystem::path navmesh_path, std::string navmesh_zone)
{
    NavmeshTask task([navmesh_path = std::move(navmesh_path), navmesh_zone = std::move(navmesh_zone)] {
        return LoadNavmeshPack(navmesh_path, navmesh_zone);
    });
    NavmeshFuture future = task.get_future().share();
    auto& queue = GetNavmeshTaskQueue();
    {
        const std::lock_guard lock(queue.mutex);
        queue.tasks.push_back(std::move(task));
    }
    queue.cv.notify_one();
    return future;
}

NavmeshFuture GetCachedFutureByKey(const std::string& cache_key, const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    const std::lock_guard lock(NavmeshFutureCacheMutex());
    auto& cache = NavmeshFutureCache();
    if (auto iter = cache.find(cache_key); iter != cache.end()) {
        return iter->second;
    }

    auto future = EnqueueNavmeshLoad(navmesh_path, navmesh_zone);
    cache.emplace(cache_key, future);
    return future;
}

NavmeshFuture GetCachedNavmeshFuture(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    const std::string cache_key = BuildNavmeshCacheKey(navmesh_path, navmesh_zone);
    return GetCachedFutureByKey(cache_key, navmesh_path, navmesh_zone);
}

std::shared_ptr<CachedNavmesh> LoadCachedNavmesh(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    const std::string cache_key = BuildNavmeshCacheKey(navmesh_path, navmesh_zone);
    NavmeshCacheExceptionGuard exception_guard(cache_key);
    auto navmesh = GetCachedFutureByKey(cache_key, navmesh_path, navmesh_zone).get();
    if (!navmesh) {
        RemoveCachedNavmeshFutureByKey(cache_key);
        exception_guard.Dismiss();
        return nullptr;
    }
    exception_guard.Dismiss();
    return navmesh;
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

navmesh::BaseNavRouteRequest BuildRouteRequest(
    const NaviParam& param,
    const std::string& navmesh_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    const std::vector<uint32_t>& blocked_triangles = {})
{
    navmesh::BaseNavRouteRequest request;
    request.zone_name = navmesh_zone;
    request.start = start;
    request.goal = goal;
    request.snap_radius = param.navmesh_snap_radius;
    request.max_cost = param.navmesh_max_cost;
    request.blocked_triangles = blocked_triangles;
    return request;
}

navmesh::BaseNavRouteRequest BuildRouteRequest(const NaviParam& param, const NavmeshExpansionState& state, const Waypoint& waypoint)
{
    return BuildRouteRequest(param, state.navmesh_zone, state.route_start, { .x = waypoint.x, .y = waypoint.y });
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
    if (!AppendGeneratedNavmeshWaypoints(route_result.path, out_path, true)) {
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

std::optional<std::string> InferPreloadNavmeshZone(const NaviParam& param)
{
    std::string current_zone = param.map_name;
    for (const Waypoint& waypoint : param.path) {
        if (waypoint.IsZoneDeclaration()) {
            current_zone = waypoint.zone_id;
            continue;
        }
        if (IsNavmeshWaypoint(waypoint)) {
            std::string navmesh_zone = InferBaseNavZone(current_zone, param.map_name);
            if (navmesh_zone.empty()) {
                return std::nullopt;
            }
            return navmesh_zone;
        }
        if (!waypoint.zone_id.empty()) {
            current_zone = waypoint.zone_id;
        }
    }
    return std::nullopt;
}

navmesh::WorldPoint OffsetPoint(const NaviPosition& position, double heading, double distance)
{
    const double radians = NaviMath::NormalizeHeading(heading) * kPi / 180.0;
    return { .x = position.x + std::sin(radians) * distance, .y = position.y - std::cos(radians) * distance };
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

void PreloadNavmeshWaypoints(const NaviParam& param)
{
    const auto navmesh_zone = InferPreloadNavmeshZone(param);
    if (!navmesh_zone) {
        return;
    }

    const std::filesystem::path navmesh_path = ResolveNavmeshFile(param.navmesh_file);
    (void)GetCachedNavmeshFuture(navmesh_path, *navmesh_zone);
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

std::optional<navmesh::BaseNavRouteResult> PlanNavmeshRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    const std::vector<uint32_t>& blocked_triangles)
{
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    if (navmesh_zone.empty()) {
        LogError << "Failed to infer NAVMESH base zone." << VAR(locator_zone) << VAR(param.map_name);
        return std::nullopt;
    }

    const std::filesystem::path navmesh_path = ResolveNavmeshFile(param.navmesh_file);
    const auto navmesh = LoadCachedNavmesh(navmesh_path, navmesh_zone);
    if (!navmesh) {
        return std::nullopt;
    }

    const auto request = BuildRouteRequest(param, navmesh_zone, start, goal, blocked_triangles);
    const auto route_result = navmesh->planner.findPath(request);
    if (!route_result.ok()) {
        if (blocked_triangles.empty()) {
            LogError << "Failed to plan NAVMESH route." << VAR(navmesh_zone) << VAR(locator_zone) << VAR(start.x) << VAR(start.y)
                     << VAR(goal.x) << VAR(goal.y) << VAR(navmesh::ToString(route_result.status)) << VAR(blocked_triangles.size());
        }
        return std::nullopt;
    }

    if (blocked_triangles.empty()) {
        LogInfo << "NAVMESH route planned." << VAR(navmesh_zone) << VAR(locator_zone) << VAR(route_result.cost)
                << VAR(route_result.triangles.size()) << VAR(route_result.path.points.size()) << VAR(blocked_triangles.size());
    }
    return route_result;
}

std::optional<navmesh::BaseNavRouteResult> PlanNavmeshDetourRoute(
    const NaviParam& param,
    const NaviPosition& position,
    const Waypoint& anchor,
    double route_heading,
    navmesh::WorldPoint* out_detour_vertex)
{
    if (!anchor.HasPosition()) {
        return std::nullopt;
    }

    const navmesh::WorldPoint start { .x = position.x, .y = position.y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    const auto direct_route = PlanNavmeshRoute(param, position.zone_id, start, goal);
    if (!direct_route) {
        return std::nullopt;
    }

    std::vector<uint32_t> blocked;
    // Block the obstacle triangles just ahead of the agent, but never the goal triangle (the last one
    // in the route). At short range the fixed block budget can otherwise reach the destination triangle,
    // leaving the detour planner unable to route to the anchor — recovery then fails even when a clean
    // short bypass exists. For routes longer than the budget this leaves the blocked set unchanged.
    const size_t blockable = direct_route->triangles.size() > 1 ? direct_route->triangles.size() - 1 : 0;
    const size_t blocked_end = std::min(blockable, kDetourBlockedTriangleCount + 1);
    for (size_t index = 1; index < blocked_end; ++index) {
        blocked.push_back(direct_route->triangles[index]);
    }
    std::optional<navmesh::BaseNavRouteResult> best;
    double best_score = std::numeric_limits<double>::infinity();
    navmesh::WorldPoint best_detour;
    // The on-mesh point the start->candidate leg actually reaches (candidate snapped onto the mesh).
    // This is the bypass vertex the recovery overlay pins so the agent is steered to the side of the
    // obstacle rather than back into it.
    navmesh::WorldPoint best_detour_vertex {};
    for (double radius : kDetourRadii) {
        for (double heading_offset : kDetourHeadingOffsets) {
            const navmesh::WorldPoint candidate = OffsetPoint(position, route_heading + heading_offset, radius);
            const navmesh::WorldPoint forward_probe = OffsetPoint(position, route_heading, kDetourBlockedForwardDistance);
            if (std::hypot(candidate.x - forward_probe.x, candidate.y - forward_probe.y) <= radius * 0.35) {
                continue;
            }

            NaviParam detour_param = param;
            detour_param.navmesh_snap_radius = std::max(detour_param.navmesh_snap_radius, kDetourSnapRadius);
            const auto route_to_detour = PlanNavmeshRoute(detour_param, position.zone_id, start, candidate, blocked);
            const auto route_to_goal = PlanNavmeshRoute(detour_param, position.zone_id, candidate, goal, blocked);
            if (!route_to_detour || !route_to_goal) {
                continue;
            }

            const double snap_distance = std::hypot(route_to_detour->path.points.back().x - candidate.x,
                route_to_detour->path.points.back().y - candidate.y);
            const double backtrack_penalty = std::max(0.0, std::abs(heading_offset) - 120.0) / 60.0 * kDetourBacktrackPenalty;
            const double score = route_to_detour->cost + route_to_goal->cost + backtrack_penalty + snap_distance * kDetourSnapPenalty;
            if (score < best_score) {
                best = *route_to_detour;
                const size_t point_offset = best->path.points.size();
                best_detour_vertex = best->path.points.back();
                if (route_to_goal->path.points.size() > 1) {
                    best->path.points.insert(best->path.points.end(), route_to_goal->path.points.begin() + 1, route_to_goal->path.points.end());
                }
                for (size_t break_index : route_to_goal->path.segment_breaks) {
                    if (break_index != 0) {
                        best->path.segment_breaks.push_back(point_offset + break_index - 1);
                    }
                }
                if (route_to_goal->triangles.size() > 1) {
                    best->triangles.insert(best->triangles.end(), route_to_goal->triangles.begin() + 1, route_to_goal->triangles.end());
                }
                best->cost += route_to_goal->cost;
                best_score = score;
                best_detour = candidate;
            }
        }
    }

    if (!best) {
        LogError << "NAVMESH detour failed to find a reachable bypass." << VAR(position.x) << VAR(position.y) << VAR(position.zone_id)
                 << VAR(anchor.x) << VAR(anchor.y) << VAR(blocked.size());
        return std::nullopt;
    }

    if (out_detour_vertex != nullptr) {
        *out_detour_vertex = best_detour_vertex;
    }
    LogInfo << "NAVMESH detour selected." << VAR(best_detour.x) << VAR(best_detour.y) << VAR(best_detour_vertex.x)
            << VAR(best_detour_vertex.y) << VAR(best_score) << VAR(best->cost) << VAR(best->triangles.size())
            << VAR(best->path.points.size());
    return best;
}

bool AppendGeneratedNavmeshWaypoints(const navmesh::WorldPath& world_path, std::vector<Waypoint>& out_path, bool include_goal)
{
    if (world_path.points.empty()) {
        return false;
    }

    const std::unordered_set<size_t> segment_breaks(world_path.segment_breaks.begin(), world_path.segment_breaks.end());
    const size_t total = world_path.points.size();
    const size_t loop_end = include_goal ? total : (total > 0 ? total - 1 : 0);

    for (size_t index = 1; index < loop_end; ++index) {
        if (!segment_breaks.contains(index)) {
            continue;
        }
        const size_t emit_idx = index - 1;
        if (emit_idx == 0) {
            continue;
        }
        const navmesh::WorldPoint& point = world_path.points[emit_idx];
        out_path.emplace_back(point.x, point.y, ActionType::RUN);
        out_path.back().strict_arrival = true;
    }

    if (include_goal && total >= 2) {
        const navmesh::WorldPoint& goal = world_path.points[total - 1];
        out_path.emplace_back(goal.x, goal.y, ActionType::RUN);
        out_path.back().strict_arrival = true;
    }

    return true;
}

} // namespace mapnavigator
