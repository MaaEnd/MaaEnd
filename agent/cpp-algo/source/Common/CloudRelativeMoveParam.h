#pragma once

#include <cmath>
#include <cstdint>
#include <limits>
#include <optional>
#include <utility>

#include <meojson/json.hpp>

namespace cloud
{
inline std::optional<int32_t> ParseMoveInt32(const json::value& value)
{
    if (!value.is_number()) {
        return std::nullopt;
    }
    const auto number = value.as_double();
    if (!std::isfinite(number) || std::trunc(number) != number || number < std::numeric_limits<int32_t>::min()
        || number > std::numeric_limits<int32_t>::max()) {
        return std::nullopt;
    }
    return static_cast<int32_t>(number);
}

// Match the Schema's two exclusive forms, including rejection of unknown keys.
inline std::optional<std::pair<int32_t, int32_t>>
    ParseRelativeMove(const json::value& params, std::optional<std::pair<int32_t, int32_t>> target = std::nullopt)
{
    if (!params.is_object()) {
        return std::nullopt;
    }
    if (const auto begin = params.find_value("begin")) {
        if (params.as_object().size() != 1 || !begin->is_array() || begin->as_array().size() != 2 || !target) {
            return std::nullopt;
        }
        const auto x = ParseMoveInt32(begin->at(0));
        const auto y = ParseMoveInt32(begin->at(1));
        if (!x || !y) {
            return std::nullopt;
        }
        const auto dx = ParseMoveInt32(json::value(int64_t(target->first) - *x));
        const auto dy = ParseMoveInt32(json::value(int64_t(target->second) - *y));
        if (!dx || !dy) {
            return std::nullopt;
        }
        return std::pair { *dx, *dy };
    }
    const auto x = params.find_value("dx");
    const auto y = params.find_value("dy");
    if (params.as_object().size() != 2 || !x || !y) {
        return std::nullopt;
    }
    const auto dx = ParseMoveInt32(*x);
    const auto dy = ParseMoveInt32(*y);
    if (!dx || !dy) {
        return std::nullopt;
    }
    return std::pair { *dx, *dy };
}
} // namespace cloud
