#include "ParentProcessWatcher.h"

#include <chrono>
#include <cstdlib>
#include <thread>

#include <MaaUtils/Logger.h>

#ifdef _WIN32

#include <MaaUtils/SafeWindows.hpp>

#include <tlhelp32.h>

#else

#include <cerrno>
#include <csignal>
#include <unistd.h>

#endif

namespace common
{

namespace
{

constexpr auto kPollInterval = std::chrono::seconds(1);

#ifdef _WIN32

// 通过 ToolHelp 快照在 PID 表中找父 PID。
// 不依赖 ntdll 私有 API（NtQueryInformationProcess），跨 Windows 版本更稳。
DWORD QueryParentProcessId(DWORD pid)
{
    HANDLE snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (snapshot == INVALID_HANDLE_VALUE) {
        return 0;
    }

    DWORD parent_pid = 0;
    PROCESSENTRY32W entry {};
    entry.dwSize = sizeof(entry);

    if (Process32FirstW(snapshot, &entry)) {
        do {
            if (entry.th32ProcessID == pid) {
                parent_pid = entry.th32ParentProcessID;
                break;
            }
        } while (Process32NextW(snapshot, &entry));
    }

    CloseHandle(snapshot);
    return parent_pid;
}

void RunWatcherLoop(HANDLE parent_handle, DWORD parent_pid)
{
    // 启动时一次性 OpenProcess(SYNCHRONIZE)，循环里只用 WaitForSingleObject(0) 探活，
    // 避免每秒重新查 PID 时遇到 PID 复用导致误判。
    while (true) {
        DWORD wait_result = WaitForSingleObject(parent_handle, 0);
        if (wait_result == WAIT_OBJECT_0) {
            LogWarn << "Parent process exited; shutting down cpp-algo." << VAR(parent_pid);
            CloseHandle(parent_handle);
            std::exit(0);
        }
        if (wait_result == WAIT_FAILED) {
            LogWarn << "WaitForSingleObject failed on parent handle." << VAR(parent_pid)
                    << VAR(GetLastError());
        }
        std::this_thread::sleep_for(kPollInterval);
    }
}

#else

void RunWatcherLoop(pid_t parent_pid)
{
    while (true) {
        if (kill(parent_pid, 0) != 0 && errno == ESRCH) {
            LogWarn << "Parent process exited; shutting down cpp-algo." << VAR(parent_pid);
            std::exit(0);
        }
        std::this_thread::sleep_for(kPollInterval);
    }
}

#endif

} // namespace

void StartParentProcessWatcher()
{
#ifdef _WIN32
    DWORD self_pid = GetCurrentProcessId();
    DWORD parent_pid = QueryParentProcessId(self_pid);
    if (parent_pid == 0) {
        LogWarn << "Failed to query parent pid; parent watcher disabled." << VAR(self_pid);
        return;
    }

    HANDLE parent_handle = OpenProcess(SYNCHRONIZE, FALSE, parent_pid);
    if (parent_handle == nullptr) {
        LogWarn << "Failed to open parent process; parent watcher disabled." << VAR(parent_pid)
                << VAR(GetLastError());
        return;
    }

    LogInfo << "Parent process watcher started." << VAR(parent_pid);
    std::thread(RunWatcherLoop, parent_handle, parent_pid).detach();
#else
    pid_t parent_pid = getppid();
    if (parent_pid <= 1) {
        LogWarn << "Invalid parent pid; parent watcher disabled." << VAR(parent_pid);
        return;
    }

    LogInfo << "Parent process watcher started." << VAR(parent_pid);
    std::thread(RunWatcherLoop, parent_pid).detach();
#endif
}

} // namespace common
