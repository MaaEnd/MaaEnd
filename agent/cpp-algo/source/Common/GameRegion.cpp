#include "GameRegion.h"

#include <algorithm>
#include <cctype>
#include <filesystem>
#include <optional>
#include <string_view>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

namespace gamesetting
{

namespace
{

constexpr const char* kEndfieldProcessName = "Endfield.exe";
constexpr const char* kSdkDllCN = "hgsdk.dll";
constexpr const char* kSdkDllGlobal = "gfsdk.dll";

std::optional<Region> g_cached_region;

bool EqualsIgnoreCase(std::string_view a, std::string_view b)
{
    if (a.size() != b.size()) {
        return false;
    }
    for (size_t i = 0; i < a.size(); ++i) {
        const unsigned char ca = static_cast<unsigned char>(a[i]);
        const unsigned char cb = static_cast<unsigned char>(b[i]);
        if (std::tolower(ca) != std::tolower(cb)) {
            return false;
        }
    }
    return true;
}

Region DetectGameRegionUncached()
{
    const auto processes = MAA_NS::list_processes();
    std::vector<std::filesystem::path> dirs;
    for (const auto& info : processes) {
        if (!EqualsIgnoreCase(info.name, kEndfieldProcessName)) {
            continue;
        }
        const auto path_opt = MAA_NS::get_process_path(info.pid);
        if (!path_opt || path_opt->empty()) {
            continue;
        }
        const auto dir = path_opt->parent_path().lexically_normal();
        if (std::find(dirs.begin(), dirs.end(), dir) != dirs.end()) {
            continue;
        }
        dirs.push_back(dir);
    }

    if (dirs.empty()) {
        LogError << "GameRegion: Endfield.exe not running; cannot auto-detect region";
        return Region::Unknown;
    }
    if (dirs.size() > 1) {
        LogError << "GameRegion: multiple Endfield.exe directories found, cannot auto-detect region" << VAR(dirs.size());
        return Region::Unknown;
    }

    const auto& dir = dirs.front();
    std::error_code ec;
    const bool has_cn = std::filesystem::exists(dir / kSdkDllCN, ec);
    if (ec) {
        LogError << "GameRegion: failed to stat hgsdk.dll" << VAR(dir) << VAR(ec.message());
        return Region::Unknown;
    }
    ec.clear();
    const bool has_global = std::filesystem::exists(dir / kSdkDllGlobal, ec);
    if (ec) {
        LogError << "GameRegion: failed to stat gfsdk.dll" << VAR(dir) << VAR(ec.message());
        return Region::Unknown;
    }

    if (has_cn && !has_global) {
        return Region::CN;
    }
    if (has_global && !has_cn) {
        return Region::Global;
    }
    if (has_cn && has_global) {
        LogError << "GameRegion: both hgsdk.dll and gfsdk.dll exist" << VAR(dir);
        return Region::Unknown;
    }

    LogError << "GameRegion: neither hgsdk.dll nor gfsdk.dll found" << VAR(dir);
    return Region::Unknown;
}

} // namespace

Region DetectGameRegion()
{
    if (g_cached_region) {
        return *g_cached_region;
    }

    const Region region = DetectGameRegionUncached();
    if (region == Region::Unknown) {
        return Region::Unknown;
    }
    g_cached_region = region;
    return region;
}

} // namespace gamesetting
