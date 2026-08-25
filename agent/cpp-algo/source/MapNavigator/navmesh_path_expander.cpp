#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <condition_variable>
#include <cstddef>
#include <deque>
#include <exception>
#include <filesystem>
#include <future>
#include <iterator>
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
#include "../Navmesh/RecastNavRoute.h"
#include "../utils.h"
#include "navi_config.h"
#include "navi_controller.h"
#include "navi_math.h"
#include "navmesh_path_expander.h"
#include "zipline_leg_planner.h"

namespace mapnavigator
{

namespace
{

struct CachedNavmesh
{
    navmesh::BaseNavPack pack;
    navmesh::BaseNavPlanner planner;
    mutable navmesh::recast::RecastNavEngine engine;

    explicit CachedNavmesh(navmesh::BaseNavPack nav_pack)
        : pack(std::move(nav_pack))
        , planner(pack)
        , engine(pack, planner)
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

constexpr std::array<BaseNavZoneAlias, 6> kBaseNavZoneAliases { {
    { "map01base", { "map01", "ValleyIV" } },
    { "map02base", { "map02", "Wuling" } },
    { "base01", { "base01", "OMVBase" } },
    { "dung01", { "dung01", "Dung" } },
    { "indie_dg005", { "indie_dg005", "IndieDg005" } },
    { "indie_dg007", { "indie_dg007", "IndieDg007" } },
} };

constexpr std::array<double, 3> kDetourRadii { 3.0, 5.0, 7.0 };
constexpr std::array<double, 8> kDetourHeadingOffsets { 30.0, -30.0, 50.0, -50.0, 70.0, -70.0, 90.0, -90.0 };
constexpr double kDetourBlockedForwardDistance = 6.0;
constexpr size_t kDetourBlockedTriangleCount = 4;
// 点封堵对起/终点的净空(px),须大于 navmesh::recast::kBlockedPointRadius,否则把端点自己封死
constexpr double kDetourBlockedPointStandoff = 2.0;
constexpr double kDetourBacktrackPenalty = 8.0;
constexpr double kDetourSnapPenalty = 3.0;

// Blind-target fallback: navmesh omits water, so a target a human can reach is reported unreachable.
// Route as close as the mesh allows, then walk the residual gap blind toward the exact target.
constexpr double kBlindTargetFallbackSnapRadius = 12.0;
constexpr double kBlindTargetProbeStep = 2.0;
// 探针扫描的墙钟上限。能命中的探针在头几个就返回(近处目标单次规划几十毫秒),耗满预算即目标与
// 起点不连通,余下探针是同一个失败。远距离目标单次规划本身就要吃掉扩窗预算,这里只容一个来回。
constexpr int64_t kBlindTargetProbeBudgetMs = 8000;

bool IsNavmeshWaypoint(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::NAVMESH && waypoint.HasPosition();
}

bool IsGlobalRouteTarget(const Waypoint& waypoint)
{
    return !waypoint.IsZoneDeclaration() && waypoint.HasPosition();
}

bool ContainsNavmeshWaypoint(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), IsNavmeshWaypoint);
}

bool ContainsGlobalRouteTarget(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), IsGlobalRouteTarget);
}

bool HasExplicitCoordinateFrame(const Waypoint& waypoint)
{
    return !waypoint.target_tier.empty() && (waypoint.HasPosition() || waypoint.heading_uses_target);
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
    return { };
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
    std::error_code ec;
    const std::filesystem::path exe_dir = get_exe_dir();
    const std::filesystem::path navmesh_dir = exe_dir / ".." / "resource" / "model" / "map" / "navmesh";

    if (!configured_path.empty()) {
        const std::filesystem::path configured(configured_path);
        if (configured.is_absolute()) {
            return configured;
        }
        // A relative override resolves exe-anchored first (production layout), then falls back to the
        // CWD-walk for dev. If it exists nowhere, we intentionally return the exe-anchored path rather
        // than the bare relative one so a not-found diagnostic names the deployed location instead of a
        // CWD-relative path that only resolves in dev.
        const std::filesystem::path anchored = exe_dir / ".." / configured;
        if (std::filesystem::exists(anchored, ec) && !ec) {
            return anchored;
        }
        if (auto found = FindExistingFromParents(configured); found.has_value()) {
            return *found;
        }
        return anchored;
    }

    for (const char* file_name : { "base.nav.gz", "base.nav" }) {
        const std::filesystem::path candidate = navmesh_dir / file_name;
        if (std::filesystem::exists(candidate, ec) && !ec) {
            return candidate;
        }
    }

    for (const char* relative_path : { kDefaultCompressedNavmeshRelativePath, kDefaultNavmeshRelativePath }) {
        if (auto found = FindExistingFromParents(relative_path); found.has_value()) {
            return *found;
        }
    }

    return navmesh_dir / "base.nav.gz";
}

std::string BuildNavmeshCacheKey(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    return std::filesystem::absolute(navmesh_path).lexically_normal().string() + "#" + navmesh_zone;
}

std::shared_ptr<CachedNavmesh> LoadNavmeshPack(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    const auto load_result = navmesh::LoadBaseNavPack(navmesh_path, navmesh_zone);
    if (!load_result.ok()) {
        LogError << "Failed to load navmesh pack." << VAR(navmesh_path) << VAR(navmesh_zone) << VAR(load_result.message);
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
    const navmesh::BaseNavPack& pack,
    const std::string& locator_zone,
    const std::string& navmesh_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    const std::vector<uint32_t>& blocked_triangles = { },
    const std::vector<navmesh::WorldPoint>& blocked_points = { },
    float goal_floor_y = navmesh::kBaseNavFloorYNone,
    std::optional<double> goal_deck_y = std::nullopt,
    std::optional<double> start_floor_y = std::nullopt)
{
    navmesh::BaseNavRouteRequest request;
    request.zone_name = navmesh_zone;
    request.start = start;
    request.goal = goal;
    request.blocked_triangles = blocked_triangles;
    request.blocked_points = blocked_points;
    // 终点声明决定停在哪张面; 起点站在哪张面由搜索自己按起点高度定
    if (goal_deck_y) {
        request.goal_deck_y = static_cast<float>(*goal_deck_y);
    }
    // Per-endpoint floor: the start snaps onto the live locator tier's floor; the goal snaps onto its own
    // declared frame's floor when the caller supplies one (cross-tier targets), otherwise the same start
    // floor (legacy single-floor behavior). A geometry / base / unknown zone yields the sentinel ->
    // floor-blind, byte-identical to the pre-floor behavior. Mirrors the python tool's floor_y_for(tier).
    //
    // 起点高度只在调用方确实知道角色站在哪一层时才覆盖 —— 目前唯一的来源是滑索下索点，
    // 它的高度是导入数据里带来的逐点真值。不传就还是按 zone 的主层走，逐位不变。
    const float zone_floor_y = pack.floorYForZoneName(locator_zone);
    request.start_floor_y = start_floor_y ? static_cast<float>(*start_floor_y) : zone_floor_y;
    // 终点兜底取 zone 主层而不是跟着起点走：起点被覆盖时那是「角色站在哪」，与终点该落在哪无关。
    // 起点没被覆盖时两者本就同值。
    request.goal_floor_y = goal_floor_y > navmesh::kBaseNavFloorYValidMin ? goal_floor_y : zone_floor_y;
    return request;
}

struct ProjectedTarget
{
    navmesh::WorldPoint point;
    float floor_y = navmesh::kBaseNavFloorYNone;
    std::optional<double> deck_y;
};

// Resolve a NAVMESH waypoint's target into the base-pixel routing frame. When the node declares a
// target_tier (the tier the developer authored the coordinate in), project it through that tier's OWN baked
// affine — the mirror of NormalizeLivePositionToBase on the start — and carry that tier's floor for the goal
// snap so the two endpoints land in the same base frame and each snaps onto its own floor. No target_tier
// (legacy) -> the target is already base-pixel: identity projection, floor-blind, byte-for-byte unchanged.
// An unknown tier (typo / missing zone) is logged and treated as base-pixel rather than silently mis-routing.
ProjectedTarget ResolveProjectedTarget(const navmesh::BaseNavPack& pack, const Waypoint& waypoint)
{
    const navmesh::WorldPoint raw { .x = waypoint.x, .y = waypoint.y };
    if (waypoint.target_tier.empty()) {
        return { raw, navmesh::kBaseNavFloorYNone, waypoint.target_deck_y };
    }
    const auto projection = pack.projectToBase(waypoint.target_tier, waypoint.x, waypoint.y);
    if (!projection) {
        LogWarn << "NAVMESH target_tier unknown; treating target as base-frame." << VAR(waypoint.target_tier) << VAR(waypoint.x)
                << VAR(waypoint.y);
        return { raw, navmesh::kBaseNavFloorYNone, waypoint.target_deck_y };
    }
    return { navmesh::WorldPoint { .x = projection->x, .y = projection->y },
             pack.floorYForZoneName(waypoint.target_tier),
             waypoint.target_deck_y };
}

std::optional<Waypoint> ProjectRegularWaypointToBase(const navmesh::BaseNavPack& pack, const Waypoint& waypoint)
{
    if (!HasExplicitCoordinateFrame(waypoint)) {
        return waypoint;
    }

    const auto projection = pack.projectToBase(waypoint.target_tier, waypoint.x, waypoint.y);
    if (!projection) {
        LogError << "MapNavigator waypoint target_tier is unknown." << VAR(waypoint.target_tier) << VAR(waypoint.x) << VAR(waypoint.y);
        return std::nullopt;
    }

    Waypoint projected = waypoint;
    projected.x = projection->x;
    projected.y = projection->y;
    projected.target_tier.clear();
    return projected;
}

navmesh::BaseNavRouteResult PlanCorridorRoute(
    const CachedNavmesh& navmesh,
    const navmesh::BaseNavRouteRequest& request,
    const std::function<bool()>& should_stop = { })
{
    navmesh::BaseNavRouteResult result;
    const navmesh::BaseNavZone* zone = navmesh.pack.findZoneByName(request.zone_name);
    if (zone == nullptr) {
        result.status = navmesh::BaseNavRouteStatus::ZoneNotFound;
        return result;
    }
    const float start_floor = request.start_floor_y > navmesh::kBaseNavFloorYValidMin ? request.start_floor_y : request.floor_y;
    const float goal_floor = request.goal_floor_y > navmesh::kBaseNavFloorYValidMin ? request.goal_floor_y : request.floor_y;
    // 绕障候选探测(带封堵)会成批试错,失败与告警都不上日志,由绕障侧汇总
    const bool detour_probe = !request.blocked_triangles.empty() || !request.blocked_points.empty();
    auto plan = navmesh.engine.plan(
        request.zone_name,
        request.start,
        request.goal,
        start_floor,
        goal_floor,
        request.goal_deck_y,
        request.blocked_triangles,
        request.blocked_points,
        should_stop);
    if (!plan.ok || plan.points.size() < 2) {
        if (!detour_probe) {
            LogWarn << "RECAST plan failed." << VAR(request.zone_name) << VAR(plan.error);
        }
        return result;
    }
    if (!detour_probe) {
        for (const std::string& warning : plan.warnings) {
            LogWarn << "RECAST plan warning." << VAR(request.zone_name) << VAR(warning);
        }
    }
    result.status = navmesh::BaseNavRouteStatus::Success;
    result.path.zone_id = zone->zone_id;
    result.path.zone_name = request.zone_name;
    result.path.points = std::move(plan.points);
    result.path.clearance = std::move(plan.clearance);
    result.path.waypoints = std::move(plan.waypoints);
    result.cost = plan.length;
    return result;
}

std::optional<navmesh::BaseNavRouteResult> PlanNavmeshRouteImpl(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    const std::vector<uint32_t>& blocked_triangles,
    const std::vector<navmesh::WorldPoint>& blocked_points = { },
    std::optional<double> goal_deck_y = std::nullopt,
    std::optional<double> start_floor_y = std::nullopt)
{
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    if (navmesh_zone.empty()) {
        LogWarn << "Failed to infer NAVMESH base zone." << VAR(locator_zone) << VAR(param.map_name);
        return std::nullopt;
    }

    const std::filesystem::path navmesh_path = ResolveNavmeshFile(param.navmesh_file);
    const auto load_started_at = std::chrono::steady_clock::now();
    const auto navmesh = LoadCachedNavmesh(navmesh_path, navmesh_zone);
    const int64_t navmesh_load_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - load_started_at).count();
    if (!navmesh) {
        return std::nullopt;
    }

    const bool detour_probe = !blocked_triangles.empty() || !blocked_points.empty();
    const auto request = BuildRouteRequest(
        navmesh->pack,
        locator_zone,
        navmesh_zone,
        start,
        goal,
        blocked_triangles,
        blocked_points,
        navmesh::kBaseNavFloorYNone,
        goal_deck_y,
        start_floor_y);
    const auto plan_started_at = std::chrono::steady_clock::now();
    const auto route_result = PlanCorridorRoute(*navmesh, request);
    const int64_t plan_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - plan_started_at).count();
    if (!route_result.ok()) {
        if (!detour_probe) {
            LogWarn << "Failed to plan NAVMESH route." << VAR(navmesh_zone) << VAR(locator_zone) << VAR(start.x) << VAR(start.y)
                    << VAR(goal.x) << VAR(goal.y) << VAR(navmesh::ToString(route_result.status)) << VAR(plan_ms);
        }
        return std::nullopt;
    }

    if (!detour_probe) {
        LogInfo << "NAVMESH route planned." << VAR(navmesh_zone) << VAR(locator_zone) << VAR(route_result.cost)
                << VAR(route_result.path.points.size()) << VAR(plan_ms) << VAR(navmesh_load_ms);
    }
    return route_result;
}

void LogGeneratedNavmeshPath(
    const NavmeshExpansionState& state,
    const navmesh::BaseNavRouteRequest& request,
    const navmesh::BaseNavRouteResult& route_result)
{
    const size_t path_point_count = route_result.path.points.size();
    const std::vector<double> navmesh_start_point { request.start.x, request.start.y };
    const std::vector<double> navmesh_goal_point { request.goal.x, request.goal.y };
    const auto navmesh_path_points = PathPointsForLog(route_result.path);
    const auto navmesh_segment_breaks = route_result.path.segment_breaks;
    LogInfo << "NAVMESH generated path." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(route_result.cost)
            << VAR(path_point_count) << VAR(navmesh_start_point) << VAR(navmesh_goal_point) << VAR(navmesh_segment_breaks)
            << VAR(navmesh_path_points);
}

// Probe from the target back toward the agent; the first reachable probe is the closest connected mesh
// point. Route there, then walk the residual gap to the exact target as a single non-strict (blind) RUN.
bool AppendBlindTargetFallback(
    const NaviParam& param,
    const CachedNavmesh& navmesh,
    const navmesh::WorldPoint& base_target,
    float goal_floor_y,
    const std::function<bool()>& should_stop,
    NavmeshExpansionState& state,
    std::vector<Waypoint>& out_path)
{
    const navmesh::WorldPoint target = base_target;
    const navmesh::WorldPoint start = state.route_start;
    const double total = std::hypot(target.x - start.x, target.y - start.y);
    if (total < 1e-6) {
        return false;
    }
    const navmesh::BaseNavZone* zone = navmesh.pack.findZoneByName(state.navmesh_zone);
    if (zone == nullptr) {
        return false;
    }
    const double snap_radius = std::max(param.navmesh_snap_radius, kBlindTargetFallbackSnapRadius);
    const float snap_floor =
        goal_floor_y > navmesh::kBaseNavFloorYValidMin ? goal_floor_y : navmesh.pack.floorYForZoneName(state.current_zone);

    // A probe at distance `offset` from the target snaps to within snap_radius, so its residual gap is at
    // least (offset - snap_radius). Past (kBlindTargetMaxExtension + snap_radius) every probe would fail the
    // gap check below, so cap the scan there.
    const double probe_limit = std::min(total, kBlindTargetMaxExtension + snap_radius);

    navmesh::BaseNavRouteResult approach;
    bool found = false;
    double blind_gap = 0.0;
    const auto probe_started_at = std::chrono::steady_clock::now();
    for (double offset = 0.0; offset <= probe_limit + 1e-6; offset += kBlindTargetProbeStep) {
        if (should_stop()) {
            return false;
        }
        const int64_t probe_elapsed_ms =
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - probe_started_at).count();
        if (probe_elapsed_ms > kBlindTargetProbeBudgetMs) {
            LogWarn << "NAVMESH blind-target fallback gave up: probe budget exhausted." << VAR(state.navmesh_zone)
                    << VAR(state.current_zone) << VAR(target.x) << VAR(target.y) << VAR(offset) << VAR(probe_limit)
                    << VAR(probe_elapsed_ms);
            return false;
        }
        const double t = std::min(offset / total, 1.0);
        const navmesh::WorldPoint probe {
            .x = target.x + (start.x - target.x) * t,
            .y = target.y + (start.y - target.y) * t,
        };
        const auto entry = navmesh.planner.snap(zone->zone_id, probe, snap_radius, snap_floor);
        if (!entry) {
            continue;
        }
        const navmesh::BaseNavRouteRequest request =
            BuildRouteRequest(navmesh.pack, state.current_zone, state.navmesh_zone, start, entry->point, { }, { }, goal_floor_y);
        const auto route = PlanCorridorRoute(navmesh, request, should_stop);
        if (!route.ok() || route.path.points.empty()) {
            continue;
        }
        const navmesh::WorldPoint reached = route.path.points.back();
        blind_gap = std::hypot(target.x - reached.x, target.y - reached.y);
        approach = route;
        found = true;
        break;
    }

    if (!found) {
        return false;
    }
    if (blind_gap > kBlindTargetMaxExtension) {
        LogWarn << "NAVMESH blind-target fallback rejected: residual gap too large." << VAR(state.navmesh_zone) << VAR(state.current_zone)
                << VAR(target.x) << VAR(target.y) << VAR(blind_gap) << VAR(kBlindTargetMaxExtension);
        return false;
    }

    if (!AppendGeneratedNavmeshWaypoints(approach.path, out_path, true, false, &navmesh.planner, approach.path.zone_id)) {
        return false;
    }
    if (blind_gap > kWaypointArrivalSlack) {
        out_path.emplace_back(target.x, target.y, ActionType::RUN);
        out_path.back().strict_arrival = false;
    }
    state.route_start = target;
    LogInfo << "NAVMESH blind-target fallback applied." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(target.x)
            << VAR(target.y) << VAR(blind_gap) << VAR(approach.path.points.back().x) << VAR(approach.path.points.back().y);
    return true;
}

bool AppendStartRecovery(
    const NaviParam& param,
    const CachedNavmesh& navmesh,
    const navmesh::BaseNavRouteRequest& request,
    NavmeshExpansionState& state,
    std::vector<Waypoint>& out_path)
{
    const navmesh::BaseNavZone* zone = navmesh.pack.findZoneByName(state.navmesh_zone);
    if (zone == nullptr) {
        return false;
    }
    const double radius = std::max(param.navmesh_snap_radius, kStartRecoveryMaxBlindWalk);
    const auto entry = navmesh.planner.snap(zone->zone_id, request.start, radius, request.start_floor_y);
    if (!entry) {
        LogWarn << "NAVMESH start recovery rejected: no mesh point within the blind-walk budget." << VAR(state.navmesh_zone)
                << VAR(state.current_zone) << VAR(request.start.x) << VAR(request.start.y) << VAR(radius);
        return false;
    }

    if (entry->distance <= kWaypointArrivalSlack) {
        return false;
    }

    out_path.emplace_back(entry->point.x, entry->point.y, ActionType::RUN);
    out_path.back().strict_arrival = false;
    state.route_start = entry->point;
    LogInfo << "NAVMESH start off mesh; walking onto the nearest mesh point first." << VAR(state.navmesh_zone) << VAR(state.current_zone)
            << VAR(request.start.x) << VAR(request.start.y) << VAR(entry->point.x) << VAR(entry->point.y) << VAR(entry->distance);
    return true;
}

// walking 非空时，滑索只在严格更省时顶替走路；为空时，滑索尝试桥接纯步行不连通的
// 起终两侧可走面。这里不改目的地也不改到达声明：换的只是走法，终点面仍钉在末点上。
bool TryAppendZiplineLeg(
    const NaviParam& param,
    const CachedNavmesh& navmesh,
    const ProjectedTarget& target,
    const navmesh::BaseNavRouteResult* walking,
    const std::function<bool()>& should_stop,
    NavmeshExpansionState& state,
    std::vector<Waypoint>& out_path)
{
    const navmesh::WorldPath* walking_path = walking == nullptr ? nullptr : &walking->path;
    auto route = PlanZiplineRoute(
        param,
        state.current_zone,
        state.navmesh_zone,
        state.route_start,
        target.point,
        walking_path,
        target.deck_y,
        should_stop);
    if (!route || route->approach.points.empty() || route->departure.points.empty() || route->towers.size() < 2) {
        return false;
    }

    const size_t insert_index = out_path.size();
    // 上索点自己补, 不让通用追加代劳: 起点已经站在索下时它一个点都不会产出, 而这个点必须存在
    AppendGeneratedNavmeshWaypoints(route->approach, out_path, false, false, &navmesh.planner, route->approach.zone_id);
    const navmesh::WorldPoint mount = route->approach.points.back();
    // 一跳一个航点。中途落在下一根架子上, 人就站在下一跳的上索点上, 所以跳与跳之间不插走路点
    for (size_t hop = 0; hop + 1 < route->towers.size(); ++hop) {
        const zipline::ZiplineNode& from = route->towers[hop];
        const zipline::ZiplineNode& to = route->towers[hop + 1];
        // 头一跳的上索点取走路那一段的末点, 让两段严丝合缝地接上
        out_path.emplace_back(hop == 0 ? mount.x : from.x, hop == 0 ? mount.y : from.y, ActionType::ZIPLINE);
        out_path.back().strict_arrival = true;
        out_path.back().target_deck_y = from.height;
        // 备用站位只挂在链首: 后面那些跳是从索上落下来的, 不再按上索提示
        if (hop == 0 && route->mount_restand) {
            out_path.back().mount_restand = ZiplineRestand { .x = route->mount_restand->x, .y = route->mount_restand->y };
        }
        // 仰角只能用世界坐标算: 平面 x/y 是按地图比例缩放过的, 跟高度不同尺, 混着算出来的角
        // 没有意义
        const double span_x = to.world_x - from.world_x;
        const double span_z = to.world_z - from.world_z;
        const double rise = to.world_y - from.world_y;
        out_path.back().zipline_target = ZiplineTarget {
            .x = to.x,
            .y = to.y,
            .height = to.height,
            .elevation_deg = std::atan2(rise, std::hypot(span_x, span_z)) * 180.0 / kPi,
        };
    }

    const size_t departure_index = out_path.size();
    const navmesh::WorldPoint landing = route->departure.points.back();
    AppendGeneratedNavmeshWaypoints(route->departure, out_path, true, false, &navmesh.planner, route->departure.zone_id);
    if (out_path.size() == departure_index) {
        // 下索点就是终点: 滑行本身已经把人送到了, 补一个到达点让这一腿仍以目标点收尾
        out_path.emplace_back(landing.x, landing.y, ActionType::RUN);
        out_path.back().strict_arrival = true;
    }
    if (target.deck_y) {
        out_path.back().target_deck_y = target.deck_y;
    }

    state.route_start = landing;
    const bool walking_baseline_available = walking != nullptr;
    LogInfo << "Expanded NAVMESH waypoint via zipline." << VAR(state.navmesh_zone) << VAR(state.current_zone)
            << VAR(walking_baseline_available) << VAR(route->cost) << VAR(insert_index) << VAR(out_path.size() - insert_index)
            << VAR(route->towers.size()) << VAR(mount.x) << VAR(mount.y) << VAR(route->towers.back().x) << VAR(route->towers.back().y);
    return true;
}

bool AppendNavmeshWaypoint(
    const NaviParam& param,
    const CachedNavmesh& navmesh,
    const Waypoint& waypoint,
    const std::function<bool()>& should_stop,
    NavmeshExpansionState& state,
    std::vector<Waypoint>& out_path,
    bool failure_is_terminal = true)
{
    const auto expand_started_at = std::chrono::steady_clock::now();
    const ProjectedTarget target = ResolveProjectedTarget(navmesh.pack, waypoint);
    navmesh::BaseNavRouteRequest request = BuildRouteRequest(
        navmesh.pack,
        state.current_zone,
        state.navmesh_zone,
        state.route_start,
        target.point,
        { },
        { },
        target.floor_y,
        target.deck_y);
    const auto plan_started_at = std::chrono::steady_clock::now();
    auto route_result = PlanCorridorRoute(navmesh, request, should_stop);
    bool start_recovered = false;
    if (!route_result.ok() && AppendStartRecovery(param, navmesh, request, state, out_path)) {
        request.start = state.route_start;
        route_result = PlanCorridorRoute(navmesh, request, should_stop);
        start_recovered = true;
    }
    const int64_t plan_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - plan_started_at).count();
    if (!route_result.ok()) {
        // 纯步行不连通不代表整腿不可达：连续滑索可能分别接上起终两侧的可走面。它是完整
        // 规划结果，优先级高于二维盲走和作者提示回退，也能保住整条连续链而不被提示点切碎。
        if (TryAppendZiplineLeg(param, navmesh, target, nullptr, should_stop, state, out_path)) {
            return true;
        }
        if (should_stop()) {
            return false;
        }
        // 盲走兜底是二维的, 走到目标正上方也算到; 有声明时让它兜就等于把错层吞回去
        if (target.deck_y) {
            if (failure_is_terminal) {
                LogError << "Failed to plan NAVMESH waypoint on the declared deck." << VAR(state.navmesh_zone) << VAR(state.current_zone)
                         << VAR(target.point.x) << VAR(target.point.y) << VAR(*target.deck_y)
                         << VAR(navmesh::ToString(route_result.status));
            }
            else {
                LogDebug << "Global NAVMESH target is unavailable on the declared deck." << VAR(state.navmesh_zone)
                         << VAR(state.current_zone) << VAR(target.point.x) << VAR(target.point.y) << VAR(*target.deck_y)
                         << VAR(navmesh::ToString(route_result.status));
            }
            return false;
        }
        // 全局规划只有完整抵达终点才能跳过作者提示；盲走只是逐点执行时的局部兜底，
        // 不能把“走到可走面边缘再直冲终点”冒充成完整路线。
        if (!failure_is_terminal) {
            LogDebug << "Global NAVMESH target not directly reachable; preserving authored hints." << VAR(state.navmesh_zone)
                     << VAR(state.current_zone) << VAR(target.point.x) << VAR(target.point.y)
                     << VAR(navmesh::ToString(route_result.status));
            return false;
        }
        LogWarn << "NAVMESH waypoint not directly reachable; attempting blind-target fallback." << VAR(state.navmesh_zone)
                << VAR(state.current_zone) << VAR(target.point.x) << VAR(target.point.y) << VAR(navmesh::ToString(route_result.status));
        if (AppendBlindTargetFallback(param, navmesh, target.point, target.floor_y, should_stop, state, out_path)) {
            return true;
        }
        if (should_stop()) {
            return false;
        }
        LogError << "Failed to plan NAVMESH waypoint." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(target.point.x)
                 << VAR(target.point.y) << VAR(navmesh::ToString(route_result.status));
        return false;
    }

    LogGeneratedNavmeshPath(state, request, route_result);
    if (TryAppendZiplineLeg(param, navmesh, target, &route_result, should_stop, state, out_path)) {
        return true;
    }
    const size_t insert_index = out_path.size();
    if (!AppendGeneratedNavmeshWaypoints(route_result.path, out_path, true, false, &navmesh.planner, route_result.path.zone_id)) {
        LogError << "NAVMESH planning returned an empty path." << VAR(state.navmesh_zone);
        return false;
    }

    // 重规划的锚点就是这里生成的点, 声明落在点上才活得过重规划
    // 中间点是搜索生成的, 作者只对这一腿的末点声明了面
    // 起终收敛成一个点时这一腿一个点都不追加, 此时末点属于上一腿, 不能盖
    if (target.deck_y && out_path.size() > insert_index) {
        out_path.back().target_deck_y = target.deck_y;
    }
    state.route_start = route_result.path.points.back();
    const size_t path_point_count = route_result.path.points.size();
    const size_t appended_waypoints = out_path.size() - insert_index;
    const size_t route_waypoints = out_path.size();
    const int64_t expand_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - expand_started_at).count();
    LogInfo << "Expanded NAVMESH waypoint." << VAR(state.navmesh_zone) << VAR(state.current_zone) << VAR(path_point_count)
            << VAR(insert_index) << VAR(appended_waypoints) << VAR(route_waypoints) << VAR(start_recovered) << VAR(plan_ms)
            << VAR(expand_ms);
    return true;
}

bool AppendAuthoredWaypoint(
    const NaviParam& param,
    const CachedNavmesh& navmesh,
    const Waypoint& waypoint,
    const std::function<bool()>& should_stop,
    NavmeshExpansionState& state,
    std::vector<Waypoint>& out_path)
{
    if (IsNavmeshWaypoint(waypoint)) {
        return AppendNavmeshWaypoint(param, navmesh, waypoint, should_stop, state, out_path);
    }
    const auto projected_waypoint =
        param.normalize_position_via_navmesh ? ProjectRegularWaypointToBase(navmesh.pack, waypoint) : std::optional<Waypoint>(waypoint);
    if (!projected_waypoint) {
        return false;
    }
    out_path.push_back(*projected_waypoint);
    UpdateStateFromRegularWaypoint(*projected_waypoint, state);
    return true;
}

// Every point is an optional route/action hint until `required: true`, a ZONE declaration, or the path tail
// closes the interval. Try one direct start-to-end plan first so walking and zipline compete over the whole
// interval. If that is unavailable, replay every authored point with its original action; semantic actions
// therefore remain real fallbacks rather than route-wide obligations.
bool AppendGlobalRouteGroup(
    const NaviParam& param,
    const CachedNavmesh& navmesh,
    const std::vector<Waypoint>& path,
    size_t begin,
    size_t end,
    bool endpoint_is_hard,
    const std::function<bool()>& should_stop,
    NavmeshExpansionState& state,
    std::vector<Waypoint>& out_path)
{
    if (begin >= end) {
        return true;
    }

    const Waypoint& authored_endpoint = path[end - 1];
    size_t target_index = end;
    while (target_index > begin && !path[target_index - 1].HasPosition()) {
        --target_index;
    }
    if (target_index == begin) {
        if (endpoint_is_hard) {
            return AppendAuthoredWaypoint(param, navmesh, authored_endpoint, should_stop, state, out_path);
        }
        LogInfo << "Skipped optional route actions without a position target." << VAR(begin) << VAR(end);
        return true;
    }

    const Waypoint& authored_target = path[target_index - 1];
    const auto projected_endpoint = IsNavmeshWaypoint(authored_target)     ? std::optional<Waypoint>(authored_target)
                                    : param.normalize_position_via_navmesh ? ProjectRegularWaypointToBase(navmesh.pack, authored_target)
                                                                           : std::optional<Waypoint>(authored_target);
    if (!projected_endpoint) {
        return false;
    }

    Waypoint global_target(projected_endpoint->x, projected_endpoint->y, ActionType::NAVMESH);
    global_target.strict_arrival = true;
    global_target.zone_id = projected_endpoint->zone_id;
    global_target.target_tier = projected_endpoint->target_tier;
    global_target.target_deck_y = projected_endpoint->target_deck_y;

    const NavmeshExpansionState original_state = state;
    const size_t original_path_size = out_path.size();
    if (AppendNavmeshWaypoint(param, navmesh, global_target, should_stop, state, out_path, false)) {
        if (endpoint_is_hard && !authored_endpoint.HasPosition()) {
            if (!AppendAuthoredWaypoint(param, navmesh, authored_endpoint, should_stop, state, out_path)) {
                return false;
            }
        }
        else if (endpoint_is_hard && authored_endpoint.action != ActionType::NAVMESH && authored_endpoint.action != ActionType::RUN) {
            out_path.push_back(*projected_endpoint);
            UpdateStateFromRegularWaypoint(*projected_endpoint, state);
        }
        const size_t skipped_hints = end - begin - (endpoint_is_hard ? 1 : 0);
        LogInfo << "Expanded authored route globally." << VAR(skipped_hints) << VAR(authored_target.x) << VAR(authored_target.y)
                << VAR(endpoint_is_hard);
        return true;
    }
    if (should_stop()) {
        return false;
    }

    state = original_state;
    out_path.resize(original_path_size);
    LogInfo << "Global authored route unavailable; replaying authored hints." << VAR(begin) << VAR(end) << VAR(authored_target.x)
            << VAR(authored_target.y) << VAR(endpoint_is_hard);
    for (size_t index = begin; index < end; ++index) {
        if (!AppendAuthoredWaypoint(param, navmesh, path[index], should_stop, state, out_path)) {
            return false;
        }
    }
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
        const bool needs_regular_projection = param.normalize_position_via_navmesh && HasExplicitCoordinateFrame(waypoint);
        if (IsNavmeshWaypoint(waypoint) || needs_regular_projection) {
            std::string navmesh_zone = InferBaseNavZone(current_zone, param.map_name);
            if (navmesh_zone.empty() && !waypoint.target_tier.empty()) {
                navmesh_zone = InferBaseNavZone(waypoint.target_tier, param.map_name);
            }
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

std::filesystem::path ResolveNavmeshFilePath(const std::string& configured_path)
{
    return ResolveNavmeshFile(configured_path);
}

void NormalizeLivePositionToBase(const NaviParam& param, NaviPosition& pos)
{
    if (pos.zone_id.empty()) {
        return;
    }
    const std::string navmesh_zone = InferBaseNavZone(pos.zone_id, param.map_name);
    if (navmesh_zone.empty()) {
        return;
    }
    const std::filesystem::path navmesh_path = ResolveNavmeshFile(param.navmesh_file);
    const auto navmesh = LoadCachedNavmesh(navmesh_path, navmesh_zone);
    if (!navmesh) {
        return;
    }
    const auto projection = navmesh->pack.projectToBase(pos.zone_id, pos.x, pos.y);
    if (projection && projection->was_tier) {
        // Tier-template-pixel fix -> navmesh base-pixel via the navmesh's OWN baked affine. zone_id is
        // kept as the locator naming: zone validation matches against it and InferBaseNavZone already maps
        // it to the parent geometry zone for routing. Geometry / base-matched / unknown zones never reach
        // this branch (they project to identity), so this is zero-regression by construction.
        pos.x = projection->x;
        pos.y = projection->y;
    }
}

std::string InitialExpectedZone(const NaviParam& param)
{
    if (param.path.empty()) {
        return { };
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

bool ExpandNavmeshWaypoints(
    const NaviParam& param,
    const NaviPosition& initial_pos,
    const std::function<bool()>& should_stop,
    std::vector<Waypoint>& out_path)
{
    const bool contains_navmesh = ContainsNavmeshWaypoint(param.path);
    const bool needs_global_planning = ContainsGlobalRouteTarget(param.path);
    const bool needs_regular_projection =
        param.normalize_position_via_navmesh && std::any_of(param.path.begin(), param.path.end(), [](const Waypoint& waypoint) {
            return HasExplicitCoordinateFrame(waypoint) && (waypoint.HasPosition() || waypoint.route_required);
        });
    if (!needs_global_planning && !needs_regular_projection) {
        out_path.clear();
        std::copy_if(param.path.begin(), param.path.end(), std::back_inserter(out_path), [](const Waypoint& waypoint) {
            return waypoint.IsZoneDeclaration() || waypoint.route_required;
        });
        return true;
    }

    auto state = MakeExpansionState(param, initial_pos);
    if (!state) {
        if (!contains_navmesh && !needs_regular_projection) {
            out_path = param.path;
            LogInfo << "Global route planning unavailable; keeping the authored path.";
            return true;
        }
        return false;
    }

    const std::filesystem::path navmesh_path = ResolveNavmeshFile(param.navmesh_file);
    const auto expand_started_at = std::chrono::steady_clock::now();
    const auto navmesh = LoadCachedNavmesh(navmesh_path, state->navmesh_zone);
    const int64_t navmesh_load_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - expand_started_at).count();
    if (!navmesh) {
        if (!contains_navmesh && !needs_regular_projection) {
            out_path = param.path;
            LogInfo << "Global route navmesh unavailable; keeping the authored path." << VAR(state->navmesh_zone);
            return true;
        }
        return false;
    }

    out_path.clear();
    for (size_t index = 0; index < param.path.size();) {
        if (should_stop()) {
            return false;
        }
        const Waypoint& waypoint = param.path[index];
        if (waypoint.IsZoneDeclaration()) {
            state->current_zone = waypoint.zone_id;
            out_path.push_back(waypoint);
            out_path.back().authored_group_begin = index;
            ++index;
            continue;
        }
        size_t group_end = index;
        while (group_end < param.path.size() && !param.path[group_end].IsZoneDeclaration()) {
            const bool closes_group = param.path[group_end].route_required;
            ++group_end;
            if (closes_group) {
                break;
            }
        }
        const bool endpoint_is_hard = group_end > index && param.path[group_end - 1].route_required;
        const size_t group_output_begin = out_path.size();
        if (!AppendGlobalRouteGroup(param, *navmesh, param.path, index, group_end, endpoint_is_hard, should_stop, *state, out_path)) {
            return false;
        }
        // 记下这段产物折回作者路线时该从哪一组接; 滑索恢复按它切出剩余作者路线重新展开
        for (size_t output = group_output_begin; output < out_path.size(); ++output) {
            out_path[output].authored_group_begin = index;
        }
        index = group_end;
    }
    const int64_t expand_total_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - expand_started_at).count();
    const size_t authored_waypoints = param.path.size();
    const size_t expanded_waypoints = out_path.size();
    LogInfo << "NAVMESH route expansion finished." << VAR(state->navmesh_zone) << VAR(authored_waypoints) << VAR(expanded_waypoints)
            << VAR(navmesh_load_ms) << VAR(expand_total_ms);
    return true;
}

std::optional<navmesh::BaseNavRouteResult> PlanNavmeshRoute(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& start,
    const navmesh::WorldPoint& goal,
    std::optional<double> goal_deck_y,
    std::optional<double> start_floor_y)
{
    return PlanNavmeshRouteImpl(param, locator_zone, start, goal, { }, { }, goal_deck_y, start_floor_y);
}

float NavmeshFloorYForZone(const NaviParam& param, const std::string& locator_zone)
{
    if (locator_zone.empty()) {
        return navmesh::kBaseNavFloorYNone;
    }
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    if (navmesh_zone.empty()) {
        return navmesh::kBaseNavFloorYNone;
    }
    const auto navmesh = LoadCachedNavmesh(ResolveNavmeshFile(param.navmesh_file), navmesh_zone);
    if (!navmesh) {
        return navmesh::kBaseNavFloorYNone;
    }
    return navmesh->pack.floorYForZoneName(locator_zone);
}

bool NavmeshZonesShareGeometry(const NaviParam& param, const std::string& zone_a, const std::string& zone_b)
{
    if (zone_a.empty() || zone_b.empty()) {
        return false;
    }
    if (zone_a == zone_b) {
        return true;
    }
    const std::string navmesh_zone = InferBaseNavZone(zone_a, param.map_name);
    if (navmesh_zone.empty()) {
        return false;
    }
    const auto navmesh = LoadCachedNavmesh(ResolveNavmeshFile(param.navmesh_file), navmesh_zone);
    if (!navmesh) {
        return false;
    }
    const auto geometry_id = [&navmesh](const std::string& name) -> int {
        const navmesh::BaseNavZone* zone = navmesh->pack.findZoneByName(name);
        return zone == nullptr ? -1 : static_cast<int>(navmesh->pack.geometryZoneId(zone->zone_id));
    };
    const int geom_a = geometry_id(zone_a);
    return geom_a >= 0 && geom_a == geometry_id(zone_b);
}

std::optional<NavmeshSnap> NavmeshSnapAt(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::WorldPoint& point,
    double radius,
    std::optional<double> floor_y)
{
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    if (navmesh_zone.empty()) {
        return std::nullopt;
    }
    const auto navmesh = LoadCachedNavmesh(ResolveNavmeshFile(param.navmesh_file), navmesh_zone);
    if (!navmesh) {
        return std::nullopt;
    }
    const auto proj = navmesh->pack.projectToBase(navmesh_zone, point.x, point.y);
    if (!proj || proj->geometry_zone == nullptr) {
        return std::nullopt;
    }
    const auto entry = navmesh->planner.snap(
        proj->geometry_zone->zone_id,
        { .x = proj->x, .y = proj->y },
        radius,
        floor_y ? static_cast<float>(*floor_y) : navmesh::kBaseNavFloorYNone);
    if (!entry) {
        return std::nullopt;
    }
    return NavmeshSnap { .distance = entry->distance, .height = navmesh->planner.triangleHeight(entry->triangle) };
}

double NavmeshOffMeshFraction(
    const NaviParam& param,
    const std::string& locator_zone,
    const std::vector<navmesh::WorldPoint>& polyline,
    double step)
{
    if (polyline.size() < 2) {
        return 0.0;
    }
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    if (navmesh_zone.empty()) {
        return 0.0;
    }
    const std::filesystem::path navmesh_path = ResolveNavmeshFile(param.navmesh_file);
    const auto navmesh = LoadCachedNavmesh(navmesh_path, navmesh_zone);
    if (!navmesh) {
        return 0.0;
    }

    size_t total = 0;
    size_t off = 0;
    // projectToBase maps a tier query onto its geometry zone (identity for a base zone), keeping the
    // frame aligned with the routed mesh; an unresolvable sample is skipped (treated as on-mesh).
    const auto sample = [&](const navmesh::WorldPoint& p) {
        const auto proj = navmesh->pack.projectToBase(navmesh_zone, p.x, p.y);
        if (!proj || proj->geometry_zone == nullptr) {
            return;
        }
        ++total;
        if (!navmesh->planner.pointOnMesh(proj->geometry_zone->zone_id, { .x = proj->x, .y = proj->y })) {
            ++off;
        }
    };

    // Sample both endpoints of every segment so a mid-segment water dip between on-mesh vertices counts.
    ForEachResampledPoint(polyline, step, sample);
    if (total == 0) {
        return 0.0;
    }
    return static_cast<double>(off) / static_cast<double>(total);
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
    const auto direct_route = PlanNavmeshRouteImpl(param, position.zone_id, start, goal, { });
    if (!direct_route) {
        return std::nullopt;
    }

    const std::string navmesh_zone = InferBaseNavZone(position.zone_id, param.map_name);
    const auto navmesh = LoadCachedNavmesh(ResolveNavmeshFile(param.navmesh_file), navmesh_zone);
    if (!navmesh) {
        return std::nullopt;
    }
    const uint16_t zone_id = direct_route->path.zone_id;
    const float floor_y = navmesh->pack.floorYForZoneName(position.zone_id);
    const auto snapTriangle = [&](const navmesh::WorldPoint& point) -> std::optional<uint32_t> {
        const auto hit = navmesh->planner.snap(zone_id, point, navmesh::recast::kSnapRadius, floor_y);
        if (!hit) {
            return std::nullopt;
        }
        return hit->triangle;
    };

    // 封堵正前方一小段直连走廊踩到的三角形,但绝不封起点/终点自己的三角形:短腿上固定预算会
    // 摸到终点三角形,封了它绕障就永远到不了锚点。
    const auto start_triangle = snapTriangle(start);
    const auto goal_triangle = snapTriangle(goal);
    std::vector<uint32_t> blocked;
    std::vector<navmesh::WorldPoint> samples;
    {
        const auto& points = direct_route->path.points;
        const double forward_limit = kDetourBlockedForwardDistance * 2.0;
        double walked = 0.0;
        double next = navmesh::recast::kCS;
        for (size_t i = 1; i < points.size() && walked < forward_limit; ++i) {
            const double seg = std::hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y);
            while (next <= walked + seg + 1e-9 && next <= forward_limit + 1e-9) {
                const double t = seg > 1e-12 ? (next - walked) / seg : 0.0;
                const navmesh::WorldPoint sample { .x = points[i - 1].x + (points[i].x - points[i - 1].x) * t,
                                                   .y = points[i - 1].y + (points[i].y - points[i - 1].y) * t };
                next += navmesh::recast::kCS;
                samples.push_back(sample);
                const auto triangle = snapTriangle(sample);
                if (!triangle || triangle == start_triangle || triangle == goal_triangle) {
                    continue;
                }
                if (blocked.size() < kDetourBlockedTriangleCount && std::find(blocked.begin(), blocked.end(), *triangle) == blocked.end()) {
                    blocked.push_back(*triangle);
                }
            }
            walked += seg;
        }
    }

    // 开阔地大三角形能盖住整段前程,起/终点排除后封堵集会成空集;此时退化为点封堵,
    // 在起/终点净空之外沿直连采样点盖小半径,障碍粒度从三角形缩到格
    std::vector<navmesh::WorldPoint> blocked_points;
    if (blocked.empty()) {
        for (const navmesh::WorldPoint& sample : samples) {
            const double d_start = std::hypot(sample.x - start.x, sample.y - start.y);
            const double d_goal = std::hypot(sample.x - goal.x, sample.y - goal.y);
            if (d_start <= kDetourBlockedPointStandoff || d_goal <= kDetourBlockedPointStandoff) {
                continue;
            }
            blocked_points.push_back(sample);
        }
    }

    std::optional<navmesh::BaseNavRouteResult> best;
    double best_score = std::numeric_limits<double>::infinity();
    navmesh::WorldPoint best_detour;
    navmesh::WorldPoint best_detour_vertex { };
    const navmesh::WorldPoint forward_probe = OffsetPoint(position, route_heading, kDetourBlockedForwardDistance);
    for (double radius : kDetourRadii) {
        for (double heading_offset : kDetourHeadingOffsets) {
            const navmesh::WorldPoint candidate = OffsetPoint(position, route_heading + heading_offset, radius);
            if (std::hypot(candidate.x - forward_probe.x, candidate.y - forward_probe.y) <= radius * 0.35) {
                continue;
            }

            // 候选先吸上网格:绕行点必须钉在网格上,吸附距离计入评分
            const auto snapped = navmesh->planner.snap(zone_id, candidate, navmesh::recast::kSnapRadius, floor_y);
            if (!snapped) {
                continue;
            }
            const auto route_to_detour = PlanNavmeshRouteImpl(param, position.zone_id, start, snapped->point, blocked, blocked_points);
            if (!route_to_detour) {
                continue;
            }
            const auto route_to_goal = PlanNavmeshRouteImpl(param, position.zone_id, snapped->point, goal, blocked, blocked_points);
            if (!route_to_goal) {
                continue;
            }

            const double backtrack_penalty = std::max(0.0, std::abs(heading_offset) - 120.0) / 60.0 * kDetourBacktrackPenalty;
            const double score = route_to_detour->cost + route_to_goal->cost + backtrack_penalty + snapped->distance * kDetourSnapPenalty;
            if (score < best_score) {
                best = *route_to_detour;
                best->cost += route_to_goal->cost;
                best_score = score;
                best_detour = candidate;
                best_detour_vertex = snapped->point;
            }
        }
    }

    if (!best) {
        LogWarn << "NAVMESH detour failed to find a reachable bypass." << VAR(position.x) << VAR(position.y) << VAR(position.zone_id)
                << VAR(anchor.x) << VAR(anchor.y) << VAR(blocked.size()) << VAR(blocked_points.size());
        return std::nullopt;
    }

    if (out_detour_vertex != nullptr) {
        *out_detour_vertex = best_detour_vertex;
    }
    LogInfo << "NAVMESH detour selected." << VAR(best_detour.x) << VAR(best_detour.y) << VAR(best_detour_vertex.x)
            << VAR(best_detour_vertex.y) << VAR(best_score) << VAR(best->cost) << VAR(best->path.points.size());
    return best;
}

std::optional<navmesh::WorldPoint>
    PlanUnstickTarget(const NaviParam& param, const NaviPosition& position, double stuck_heading, int attempt_index, double* out_distance)
{
    const std::string navmesh_zone = InferBaseNavZone(position.zone_id, param.map_name);
    if (navmesh_zone.empty()) {
        return std::nullopt;
    }
    const std::filesystem::path navmesh_path = ResolveNavmeshFile(param.navmesh_file);
    const auto navmesh = LoadCachedNavmesh(navmesh_path, navmesh_zone);
    if (!navmesh) {
        return std::nullopt;
    }
    const auto on_mesh = [&](const navmesh::WorldPoint& p) {
        const auto proj = navmesh->pack.projectToBase(navmesh_zone, p.x, p.y);
        if (!proj || proj->geometry_zone == nullptr) {
            return true;
        }
        return navmesh->planner.pointOnMesh(proj->geometry_zone->zone_id, { .x = proj->x, .y = proj->y });
    };

    static constexpr double kFan[] = { 180.0, -135.0, 135.0, -90.0, 90.0 };
    constexpr int kFanCount = static_cast<int>(sizeof(kFan) / sizeof(kFan[0]));
    const int rot = ((attempt_index % kFanCount) + kFanCount) % kFanCount;

    for (int i = 0; i < kFanCount; ++i) {
        const double bearing = NaviMath::NormalizeHeading(stuck_heading + kFan[(i + rot) % kFanCount]);
        double run = 0.0;
        double longest = 0.0;
        double solid_start = -1.0; // distance where the trailing contiguous on-mesh stretch begins
        for (double d = kUnstickSampleStepM; d <= kUnstickMaxDistanceM + 1e-9; d += kUnstickSampleStepM) {
            if (on_mesh(OffsetPoint(position, bearing, d))) {
                if (solid_start < 0.0) {
                    solid_start = d;
                }
                run = 0.0;
            }
            else {
                solid_start = -1.0;
                run += kUnstickSampleStepM;
                longest = std::max(longest, run);
            }
        }
        if (solid_start >= 0.0 && longest <= kUnstickMaxRockCrossingM) {
            const double dist = std::clamp(solid_start + kUnstickMeshMarginM, kUnstickMinDistanceM, kUnstickMaxDistanceM);
            if (out_distance != nullptr) {
                *out_distance = dist;
            }
            return OffsetPoint(position, bearing, dist);
        }
    }
    return std::nullopt;
}

bool AppendGeneratedNavmeshWaypoints(
    const navmesh::WorldPath& world_path,
    std::vector<Waypoint>& out_path,
    bool include_goal,
    bool emit_interior_corners,
    const navmesh::BaseNavPlanner* drivability_planner,
    uint16_t drivable_zone_id,
    bool strict_segment_breaks)
{
    if (world_path.points.empty()) {
        return false;
    }

    const std::unordered_set<size_t> segment_breaks(world_path.segment_breaks.begin(), world_path.segment_breaks.end());
    const size_t total = world_path.points.size();
    const size_t loop_end = include_goal ? total : (total > 0 ? total - 1 : 0);
    const auto clearance_at = [&](size_t index) {
        return index < world_path.clearance.size() ? world_path.clearance[index] : 0.0;
    };

    if (emit_interior_corners) {
        for (size_t index = 1; index < loop_end; ++index) {
            const navmesh::WorldPoint& point = world_path.points[index];
            out_path.emplace_back(point.x, point.y, ActionType::RUN);
            out_path.back().strict_arrival = false;
            out_path.back().corridor_clearance = clearance_at(index);
        }
        if (include_goal && total >= 2) {
            const navmesh::WorldPoint& goal = world_path.points[total - 1];
            out_path.emplace_back(goal.x, goal.y, ActionType::RUN);
            out_path.back().strict_arrival = true;
        }
        return true;
    }

    size_t prev = 0; // the driven line starts at points[0] — the route origin / character's current position
    // Greedy string pull: from the last point committed, reach as far along the raw corridor as one straight
    // leg stays drivable, commit the vertex it stopped at, then start again from there. A real bend stops the
    // reach and survives; grid staircase and out-and-back spikes collapse into a single leg. What this
    // replaced asked the same question once across the entire leg — which no route of any length passes — so
    // every raw vertex was kept instead, down to 0.25px stair steps, and the follower had to steer them.
    const auto emit_corner = [&](size_t index) {
        out_path.emplace_back(world_path.points[index].x, world_path.points[index].y, ActionType::RUN);
        out_path.back().strict_arrival = false;
        out_path.back().corridor_clearance = clearance_at(index);
    };
    const auto restore_corners_to = [&](size_t anchor) {
        // 规划器自己拉直过的路线直接照抄下标。它那边看得到窗口挡线格图和起点所在面的高度,
        // 这里只有网格面一张判据,叠层处会顺着楼下那层把捷径判成可走。跨度对不上时仍走下面这条。
        if (!world_path.waypoints.empty() && prev == 0 && anchor + 1 == total) {
            for (size_t index = 0; index + 1 < world_path.waypoints.size(); ++index) {
                emit_corner(world_path.waypoints[index]);
            }
            return;
        }
        if (drivability_planner == nullptr || anchor <= prev + 1) {
            return;
        }
        // How many raw vertices one pull may swallow. Purely a cost bound — each extra vertex re-tests the
        // whole leg from the cursor, so an unbounded scan is quadratic on a route with hundreds of points.
        // Binding it only leaves a vertex in place, which is the direction that is safe to be wrong in.
        constexpr size_t kMaxPullSpan = 64;
        size_t cursor = prev;
        while (cursor + 1 < anchor) {
            size_t reach = cursor;
            const size_t reach_limit = std::min(anchor, cursor + kMaxPullSpan);
            double swallowed_clearance = std::numeric_limits<double>::infinity();
            while (reach < reach_limit) {
                // A shortcut may not be tighter than the narrowest corridor point it swallows: that width is
                // what the route already judged this passage needs, and nothing out here knows better. An
                // unknown width reads as zero, which is the bare centre-line test this has always used.
                const double required = std::isfinite(swallowed_clearance) ? swallowed_clearance : 0.0;
                if (!drivability_planner
                         ->isRouteSegmentDrivable(drivable_zone_id, world_path.points[cursor], world_path.points[reach + 1], required)) {
                    break;
                }
                swallowed_clearance = std::min(swallowed_clearance, clearance_at(reach + 1));
                ++reach;
            }
            // Even the corridor's own next edge can fail the test. Keeping that vertex is still the best
            // answer available, and it is what guarantees this loop advances.
            if (reach == cursor) {
                ++reach;
            }
            if (reach >= anchor) {
                return;
            }
            emit_corner(reach);
            cursor = reach;
        }
    };
    const auto flush_leg_to = [&](size_t anchor, bool strict_arrival) {
        restore_corners_to(anchor);
        out_path.emplace_back(world_path.points[anchor].x, world_path.points[anchor].y, ActionType::RUN);
        out_path.back().strict_arrival = strict_arrival;
        out_path.back().corridor_clearance = clearance_at(anchor);
        prev = anchor;
    };

    for (size_t index = 1; index < loop_end; ++index) {
        if (!segment_breaks.contains(index)) {
            continue;
        }
        const size_t emit_idx = index - 1;
        if (emit_idx == 0) {
            continue;
        }
        flush_leg_to(emit_idx, strict_segment_breaks);
    }

    if (total >= 2) {
        if (include_goal) {
            flush_leg_to(total - 1, true);
        }
        else {
            restore_corners_to(total - 1);
        }
    }

    return true;
}

bool AppendGeneratedNavmeshWaypoints(
    const NaviParam& param,
    const std::string& locator_zone,
    const navmesh::BaseNavRouteResult& route,
    std::vector<Waypoint>& out_path,
    bool include_goal,
    bool emit_interior_corners,
    bool strict_segment_breaks)
{
    const std::string navmesh_zone = InferBaseNavZone(locator_zone, param.map_name);
    const std::shared_ptr<CachedNavmesh> navmesh =
        navmesh_zone.empty() ? nullptr : LoadCachedNavmesh(ResolveNavmeshFile(param.navmesh_file), navmesh_zone);
    if (!navmesh) {
        LogWarn << "Generated navmesh waypoints emitted without a drivability check." << VAR(locator_zone) << VAR(navmesh_zone);
        return AppendGeneratedNavmeshWaypoints(
            route.path,
            out_path,
            include_goal,
            emit_interior_corners,
            nullptr,
            0,
            strict_segment_breaks);
    }
    return AppendGeneratedNavmeshWaypoints(
        route.path,
        out_path,
        include_goal,
        emit_interior_corners,
        &navmesh->planner,
        route.path.zone_id,
        strict_segment_breaks);
}

} // namespace mapnavigator
