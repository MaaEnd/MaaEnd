#include "GameRegion.h"

#include <algorithm>
#include <cctype>
#include <filesystem>
#include <memory>
#include <optional>
#include <string_view>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#ifdef _WIN32
#include <MaaUtils/SafeWindows.hpp>

#include <Psapi.h>
#endif

namespace gamesetting
{

namespace
{

constexpr const char* kEndfieldProcessName = "Endfield.exe";
constexpr const char* kSdkDllCN = "hgsdk.dll";
constexpr const char* kSdkDllCNPCGame = "PCGameSDK.dll"; // Bilibili 服，仍归国服
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

void AppendUniqueDir(std::vector<std::filesystem::path>& dirs, const std::filesystem::path& dir)
{
    if (std::find(dirs.begin(), dirs.end(), dir) != dirs.end()) {
        return;
    }
    dirs.push_back(dir);
}

// Windows 使用 QUERY_LIMITED_INFORMATION，避免 MaaUtils list_processes 所需的 VM_READ 被拒。
std::vector<std::filesystem::path> CollectEndfieldInstallDirs()
{
    std::vector<std::filesystem::path> dirs;

#ifdef _WIN32
    constexpr size_t kMaxProcesses = 16 * 1024;
    constexpr DWORD kMaxPathChars = 32768;

    auto all_pids = std::make_unique<DWORD[]>(kMaxProcesses);
    DWORD bytes_needed = 0;
    if (!EnumProcesses(all_pids.get(), static_cast<DWORD>(sizeof(DWORD) * kMaxProcesses), &bytes_needed)) {
        const auto error = GetLastError();
        LogError << "GameRegion: EnumProcesses failed" << VAR(error);
        return dirs;
    }

    const DWORD count = bytes_needed / sizeof(DWORD);
    auto path_buff = std::make_unique<WCHAR[]>(kMaxPathChars);

    for (DWORD i = 0; i < count; ++i) {
        const DWORD pid = all_pids[i];
        if (pid == 0) {
            continue;
        }

        HANDLE process = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
        if (!process) {
            continue;
        }

        DWORD size = kMaxPathChars;
        const BOOL ok = QueryFullProcessImageNameW(process, 0, path_buff.get(), &size);
        CloseHandle(process);
        if (!ok) {
            continue;
        }

        const std::filesystem::path exe_path(path_buff.get());
        const auto name = MAA_NS::from_osstring(exe_path.filename().native());
        if (!EqualsIgnoreCase(name, kEndfieldProcessName)) {
            continue;
        }

        AppendUniqueDir(dirs, exe_path.parent_path().lexically_normal());
    }
#else
    const auto processes = MAA_NS::list_processes();
    for (const auto& info : processes) {
        if (!EqualsIgnoreCase(info.name, kEndfieldProcessName)) {
            continue;
        }
        const auto path_opt = MAA_NS::get_process_path(info.pid);
        if (!path_opt || path_opt->empty()) {
            continue;
        }
        AppendUniqueDir(dirs, path_opt->parent_path().lexically_normal());
    }
#endif

    return dirs;
}

Region DetectGameRegionUncached()
{
    const auto dirs = CollectEndfieldInstallDirs();

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
    const bool has_hg = std::filesystem::exists(dir / kSdkDllCN, ec);
    if (ec) {
        LogError << "GameRegion: failed to stat hgsdk.dll" << VAR(dir) << VAR(ec.message());
        return Region::Unknown;
    }
    ec.clear();
    const bool has_pcgame = std::filesystem::exists(dir / kSdkDllCNPCGame, ec);
    if (ec) {
        LogError << "GameRegion: failed to stat PCGameSDK.dll" << VAR(dir) << VAR(ec.message());
        return Region::Unknown;
    }
    const bool has_cn = has_hg || has_pcgame;
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
        LogError << "GameRegion: both CN SDK (hgsdk.dll/PCGameSDK.dll) and gfsdk.dll exist" << VAR(dir);
        return Region::Unknown;
    }

    LogError << "GameRegion: neither CN SDK (hgsdk.dll/PCGameSDK.dll) nor gfsdk.dll found" << VAR(dir);
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
