#pragma once

#include <array>
#include <algorithm>
#include <cctype>
#include <ranges>
#include <string_view>

namespace mapnavigator
{

inline bool EqualsIgnoreCase(std::string_view lhs, std::string_view rhs)
{
    return lhs.size() == rhs.size()
           && std::ranges::equal(lhs, rhs, [](char l, char r) {
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

inline bool IsWlrootsControllerType(std::string_view controller_type)
{
    return EqualsIgnoreCase(controller_type, "wlroots") || EqualsIgnoreCase(controller_type, "wl_roots");
}

} // namespace mapnavigator
