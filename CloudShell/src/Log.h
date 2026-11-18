#pragma once

#include <filesystem>
#include <format>
#include <string>
#include <string_view>

namespace cloudshell::log
{

void Init(const std::filesystem::path& file);
void Write(std::string_view level, std::string_view message);

template <typename... Args>
void Info(std::format_string<Args...> fmt, Args&&... args)
{
    Write("INF", std::format(fmt, std::forward<Args>(args)...));
}

template <typename... Args>
void Warn(std::format_string<Args...> fmt, Args&&... args)
{
    Write("WRN", std::format(fmt, std::forward<Args>(args)...));
}

template <typename... Args>
void Error(std::format_string<Args...> fmt, Args&&... args)
{
    Write("ERR", std::format(fmt, std::forward<Args>(args)...));
}

} // namespace cloudshell::log
