#include "navmesh_cache.h"

#include <algorithm>
#include <array>
#include <condition_variable>
#include <deque>
#include <exception>
#include <future>
#include <mutex>
#include <optional>
#include <thread>
#include <unordered_map>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#include "../../Navmesh/BaseNavReader.h"
#include "../../utils.h"
#include "../navi_config.h"
#include "../navi_param.h"

namespace mapnavigator
{

namespace
{

struct BaseNavZoneAlias
{
    std::string_view zone_id;
    std::array<std::string_view, 2> prefixes;
};

constexpr std::array<BaseNavZoneAlias, 7> kBaseNavZoneAliases { {
    { "map01base", { "map01", "ValleyIV" } },
    { "map02base", { "map02", "Wuling" } },
    { "base01", { "base01", "OMVBase" } },
    { "dung01", { "dung01", "Dung" } },
    { "indie_dg005", { "indie_dg005", "IndieDg005" } },
    { "indie_dg007", { "indie_dg007", "IndieDg007" } },
    { "indie_dg016", { "indie_dg016", "IndieDg016" } },
} };

bool starts_with(std::string_view text, std::string_view prefix)
{
    return text.size() >= prefix.size() && text.substr(0, prefix.size()) == prefix;
}

std::optional<std::filesystem::path> find_existing_from_parents(const std::filesystem::path& relative_path)
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

std::string build_navmesh_cache_key(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    return MAA_NS::path_to_utf8_string(std::filesystem::absolute(navmesh_path).lexically_normal()) + "#" + navmesh_zone;
}

std::shared_ptr<CachedNavmesh> load_navmesh_pack(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
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
        : worker([this] { run(); })
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

    void run()
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

std::unordered_map<std::string, NavmeshFuture>& navmesh_future_cache()
{
    static std::unordered_map<std::string, NavmeshFuture> cache;
    return cache;
}

std::mutex& navmesh_future_cache_mutex()
{
    static std::mutex mutex;
    return mutex;
}

void remove_cached_navmesh_future_by_key(const std::string& cache_key)
{
    const std::lock_guard lock(navmesh_future_cache_mutex());
    navmesh_future_cache().erase(cache_key);
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
            remove_cached_navmesh_future_by_key(cache_key_);
        }
    }

    NavmeshCacheExceptionGuard(const NavmeshCacheExceptionGuard&) = delete;
    NavmeshCacheExceptionGuard& operator=(const NavmeshCacheExceptionGuard&) = delete;

    void dismiss() { active_ = false; }

private:
    std::string cache_key_;
    int uncaught_exceptions_ = 0;
    bool active_ = true;
};

NavmeshTaskQueue& get_navmesh_task_queue()
{
    static NavmeshTaskQueue queue;
    return queue;
}

NavmeshFuture enqueue_navmesh_load(std::filesystem::path navmesh_path, std::string navmesh_zone)
{
    NavmeshTask task([navmesh_path = std::move(navmesh_path), navmesh_zone = std::move(navmesh_zone)] {
        return load_navmesh_pack(navmesh_path, navmesh_zone);
    });
    NavmeshFuture future = task.get_future().share();
    auto& queue = get_navmesh_task_queue();
    {
        const std::lock_guard lock(queue.mutex);
        queue.tasks.push_back(std::move(task));
    }
    queue.cv.notify_one();
    return future;
}

NavmeshFuture
    get_cached_future_by_key(const std::string& cache_key, const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    const std::lock_guard lock(navmesh_future_cache_mutex());
    auto& cache = navmesh_future_cache();
    if (auto iter = cache.find(cache_key); iter != cache.end()) {
        return iter->second;
    }

    auto future = enqueue_navmesh_load(navmesh_path, navmesh_zone);
    cache.emplace(cache_key, future);
    return future;
}

} // namespace

std::filesystem::path ResolveNavmeshFilePath(const std::string& configured_path)
{
    std::error_code ec;
    const std::filesystem::path install_dir = get_install_dir();
    const std::filesystem::path navmesh_dir = install_dir / "resource" / "model" / "map" / "navmesh";

    if (!configured_path.empty()) {
        const std::filesystem::path configured = MAA_NS::path(configured_path);
        if (configured.is_absolute()) {
            return configured;
        }
        // 都找不到时返回 exe 相对路径, 报错才指向部署位置
        const std::filesystem::path anchored = install_dir / configured;
        if (std::filesystem::exists(anchored, ec) && !ec) {
            return anchored;
        }
        if (auto found = find_existing_from_parents(configured); found.has_value()) {
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
        if (auto found = find_existing_from_parents(relative_path); found.has_value()) {
            return *found;
        }
    }

    return navmesh_dir / "base.nav.gz";
}

std::filesystem::path NoGoTablePath()
{
    return get_install_dir() / kNoGoTableRelativePath;
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
                return starts_with(source, prefix);
            })) {
            return std::string(alias.zone_id);
        }
    }
    return {};
}

void PreloadNavmesh(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    (void)get_cached_future_by_key(build_navmesh_cache_key(navmesh_path, navmesh_zone), navmesh_path, navmesh_zone);
}

std::shared_ptr<CachedNavmesh> LoadCachedNavmesh(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone)
{
    const std::string cache_key = build_navmesh_cache_key(navmesh_path, navmesh_zone);
    NavmeshCacheExceptionGuard exception_guard(cache_key);
    auto navmesh = get_cached_future_by_key(cache_key, navmesh_path, navmesh_zone).get();
    if (!navmesh) {
        remove_cached_navmesh_future_by_key(cache_key);
        exception_guard.dismiss();
        return nullptr;
    }
    exception_guard.dismiss();
    return navmesh;
}

ZoneNavmesh LoadZoneNavmesh(const NaviParam& param, const std::string& locator_zone)
{
    ZoneNavmesh zone { .navmesh_zone = InferBaseNavZone(locator_zone, param.map_name) };
    if (!zone.navmesh_zone.empty()) {
        zone.mesh = LoadCachedNavmesh(ResolveNavmeshFilePath(param.navmesh_file), zone.navmesh_zone);
    }
    return zone;
}

} // namespace mapnavigator
