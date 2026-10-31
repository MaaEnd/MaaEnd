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

#include "../../utils.h"

namespace mapnavigator
{

inline bool EqualsIgnoreCase(std::string_view lhs, std::string_view rhs)
{
    return lhs.size() == rhs.size() && std::ranges::equal(lhs, rhs, [](char l, char r) {
               return std::tolower(static_cast<unsigned char>(l)) == std::tolower(static_cast<unsigned char>(r));
           });
}

// native_android 不经 adb, 但画面是同一套移动端界面
inline bool IsAdbLikeControllerType(std::string_view controller_type)
{
    constexpr std::array<std::string_view, 4> kAdbLikeControllerTypes = { "adb", "playcover", "play_cover", "native_android" };
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

// 顺序与框架叠加 overlay 一致, 基础资源排最后, resource_cloud 无法按控制器类型区分故不参与
inline std::vector<std::filesystem::path> ResourceImageRoots(std::string_view controller_type)
{
    const std::filesystem::path install_dir = std::filesystem::absolute(get_install_dir());

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
