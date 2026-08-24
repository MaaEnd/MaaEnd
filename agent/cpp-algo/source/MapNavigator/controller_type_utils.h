#pragma once

#include <algorithm>
#include <array>
#include <cctype>
#include <filesystem>
#include <optional>
#include <ranges>
#include <string>
#include <string_view>
#include <system_error>
#include <vector>

#include "../utils.h"

namespace mapnavigator
{

inline bool EqualsIgnoreCase(std::string_view lhs, std::string_view rhs)
{
    return lhs.size() == rhs.size() && std::ranges::equal(lhs, rhs, [](char l, char r) {
               return std::tolower(static_cast<unsigned char>(l)) == std::tolower(static_cast<unsigned char>(r));
           });
}

inline bool IsAdbLikeControllerType(std::string_view controller_type)
{
    constexpr std::array<std::string_view, 3> kAdbLikeControllerTypes = { "adb", "playcover", "play_cover" };
    return std::ranges::any_of(kAdbLikeControllerTypes, [&](std::string_view candidate) {
        return EqualsIgnoreCase(controller_type, candidate);
    });
}

inline bool IsPlayCoverControllerType(std::string_view controller_type)
{
    return EqualsIgnoreCase(controller_type, "playcover") || EqualsIgnoreCase(controller_type, "play_cover");
}

inline bool IsLinuxControllerType(std::string_view controller_type)
{
    return EqualsIgnoreCase(controller_type, "linux");
}

// 框架先加载 ./resource, 再把控制器自己的 overlay 叠上去, 所以 overlay 里的同名图会胜出; 这里照同一个
// 顺序找, 基础资源永远排最后。云游戏与本地 ADB 在控制器类型上无法区分, 所以 resource_cloud_adb 不参与。
inline std::vector<std::filesystem::path> ResourceImageRoots(std::string_view controller_type)
{
    const std::filesystem::path install_dir = std::filesystem::absolute(get_exe_dir() / "..");

    std::vector<std::string> dirs;
    if (IsPlayCoverControllerType(controller_type)) {
        dirs.emplace_back("resource_playcover");
        dirs.emplace_back("resource_adb");
    }
    else if (IsAdbLikeControllerType(controller_type)) {
        dirs.emplace_back("resource_adb");
    }
    else if (IsLinuxControllerType(controller_type)) {
        dirs.emplace_back("resource_linux");
    }
    else if (EqualsIgnoreCase(controller_type, "macos")) {
        dirs.emplace_back("resource_macos");
    }
    dirs.emplace_back("resource");

    std::vector<std::filesystem::path> roots;
    roots.reserve(dirs.size());
    for (const std::string& dir : dirs) {
        roots.push_back(install_dir / dir / "image");
    }
    return roots;
}

// 相对路径按上面的顺序逐层找, 命中即返回。一层都没有是调用方该报的错, 这里不代它兜底。
inline std::optional<std::filesystem::path>
    ResolveResourceImage(const std::vector<std::filesystem::path>& roots, const std::filesystem::path& relative)
{
    for (const std::filesystem::path& root : roots) {
        std::filesystem::path candidate = root / relative;
        std::error_code ec;
        if (std::filesystem::exists(candidate, ec) && !ec) {
            return candidate;
        }
    }
    return std::nullopt;
}

// 找不到图时把整条层次写进日志, 好一眼看出是层次不对还是图真的缺
inline std::string DescribeRoots(const std::vector<std::filesystem::path>& roots)
{
    std::string text;
    for (const std::filesystem::path& root : roots) {
        if (!text.empty()) {
            text += " | ";
        }
        text += MAA_NS::path_to_utf8_string(root);
    }
    return text;
}

} // namespace mapnavigator
