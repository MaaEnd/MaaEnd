#include "GameRegion.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <cstdint>
#include <filesystem>
#include <iterator>
#include <memory>
#include <optional>
#include <set>
#include <sstream>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#ifdef _WIN32
#include <MaaUtils/SafeWindows.hpp>

#include <Psapi.h>
#endif

#include "../MapNavigator/controller_info_utils.h"
#include "../MapNavigator/controller_type_utils.h"
#include "../utils.h"
#ifdef __APPLE__
#include "MacAppHost.h"
#endif

namespace gamesetting
{

namespace
{

constexpr const char* kEndfieldProcessName = "Endfield.exe";
constexpr const char* kSdkDllCN = "hgsdk.dll";
constexpr const char* kSdkDllCNPCGame = "PCGameSDK.dll"; // Bilibili 服，仍归国服
constexpr const char* kSdkDllGlobal = "gfsdk.dll";

constexpr const char* kAdbListProcessesCommand = "ps -A -o NAME";
constexpr int64_t kAdbShellTimeoutMs = 20000;
constexpr std::array<std::pair<std::string_view, Region>, 4> kEndfieldAppIds = { {
    { "com.hypergryph.endfield", Region::CN },
    { "com.hypergryph.endfield.bilibili", Region::CN },
    { "com.gryphline.endfield.gp", Region::Global },
    { "com.gryphline.endfield.ios", Region::Global },
} };

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

Region RegionOfRunningApps(const std::vector<std::string>& app_ids, const std::string& controller_type)
{
    std::set<Region> regions;
    for (const auto& [app_id, region] : kEndfieldAppIds) {
        if (std::find(app_ids.begin(), app_ids.end(), app_id) != app_ids.end()) {
            regions.insert(region);
        }
    }

    switch (regions.size()) {
    case 1:
        return *regions.begin();
    case 0:
        LogError << "GameRegion: Endfield not running; cannot auto-detect region" << VAR(controller_type);
        return Region::Unknown;
    default:
        LogError << "GameRegion: both CN and Global Endfield are running, cannot auto-detect region" << VAR(controller_type);
        return Region::Unknown;
    }
}

std::vector<std::string> ListAdbProcessNames(MaaController* controller)
{
    const MaaCtrlId shell_id = MaaControllerPostShell(controller, kAdbListProcessesCommand, kAdbShellTimeoutMs);
    if (MaaControllerWait(controller, shell_id) != MaaStatus_Succeeded) {
        LogError << "GameRegion: adb shell failed" << VAR(kAdbListProcessesCommand);
        return {};
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaControllerGetShellOutput(controller, buffer.Get())) {
        LogError << "GameRegion: adb shell output unavailable" << VAR(kAdbListProcessesCommand);
        return {};
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    std::istringstream output(raw != nullptr ? raw : "");
    return std::vector<std::string>(std::istream_iterator<std::string>(output), std::istream_iterator<std::string>());
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

Region DetectGameRegion(MaaController* controller)
{
    const std::string controller_type = mapnavigator::DetectControllerType(controller);
#ifdef __APPLE__
    if (mapnavigator::IsPlayCoverControllerType(controller_type)) {
        return RegionOfRunningApps(common::macapp::RunningApplicationIds(), controller_type);
    }
#endif
    if (mapnavigator::IsAdbLikeControllerType(controller_type)) {
        return RegionOfRunningApps(ListAdbProcessNames(controller), controller_type);
    }
    return DetectGameRegion();
}

} // namespace gamesetting
