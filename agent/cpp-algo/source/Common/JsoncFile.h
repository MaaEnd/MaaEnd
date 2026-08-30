#pragma once

#include <filesystem>
#include <optional>

#include <meojson/json.hpp>

namespace common
{

inline std::optional<json::value> OpenJsoncFile(const std::filesystem::path& path)
{
    // 仓库内 JSONC 允许注释，同时兼容可能带 UTF-8 BOM 的文件。
    return json::open(path, true, true);
}

} // namespace common
