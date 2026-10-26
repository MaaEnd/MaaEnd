#include "Log.h"

#include <fstream>
#include <mutex>

#include <windows.h>

namespace cloudshell::log
{

namespace
{

std::mutex g_mutex;
std::ofstream g_file;

} // namespace

void Init(const std::filesystem::path& file)
{
    std::lock_guard lock(g_mutex);
    std::error_code ec;
    std::filesystem::create_directories(file.parent_path(), ec);
    g_file.open(file, std::ios::app);
}

void Write(std::string_view level, std::string_view message)
{
    // 不用 std::chrono::current_zone()：它依赖系统 ICU，旧版 Windows 上会抛异常
    SYSTEMTIME now {};
    GetLocalTime(&now);
    const std::string line = std::format(
        "[{:04}-{:02}-{:02} {:02}:{:02}:{:02}.{:03}][{}][Tx{}] {}\n",
        now.wYear,
        now.wMonth,
        now.wDay,
        now.wHour,
        now.wMinute,
        now.wSecond,
        now.wMilliseconds,
        level,
        GetCurrentThreadId(),
        message);

    std::lock_guard lock(g_mutex);
    if (g_file.is_open()) {
        g_file << line;
        g_file.flush();
    }
    OutputDebugStringA(line.c_str());
}

} // namespace cloudshell::log
