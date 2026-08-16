#include "CrashDump.h"

#ifdef _WIN32

#include <atomic>
#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cwchar>
#include <exception>
#include <filesystem>
#include <string>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <MaaUtils/SafeWindows.hpp>

#include <dbghelp.h>

#else

// 非 Windows：空实现，无额外依赖。

#endif

namespace common
{

#ifdef _WIN32

namespace
{

constexpr int kMaxPathChars = 4096;
constexpr DWORD kDumpTimeoutMs = 60'000;
constexpr MINIDUMP_TYPE kDumpType = static_cast<MINIDUMP_TYPE>(
    MiniDumpWithIndirectlyReferencedMemory | MiniDumpWithThreadInfo | MiniDumpWithHandleData);

struct DumpRequest
{
    EXCEPTION_POINTERS* exception_pointers = nullptr;
    DWORD crashing_thread_id = 0;
};

wchar_t g_dump_dir[kMaxPathChars] = {};
wchar_t g_exe_name[kMaxPathChars] = {};
HANDLE g_request_ready = nullptr;
HANDLE g_request_done = nullptr;
DumpRequest g_request {};
std::atomic_flag g_dumping = ATOMIC_FLAG_INIT;

void CopyWide(wchar_t* dest, size_t dest_chars, const wchar_t* src)
{
    if (dest == nullptr || dest_chars == 0) {
        return;
    }
    if (src == nullptr) {
        dest[0] = L'\0';
        return;
    }
    wcsncpy_s(dest, dest_chars, src, _TRUNCATE);
}

bool InitDumpPaths()
{
    wchar_t exe_path[kMaxPathChars] = {};
    const DWORD n = GetModuleFileNameW(nullptr, exe_path, kMaxPathChars);
    if (n == 0 || n >= kMaxPathChars) {
        CopyWide(g_dump_dir, kMaxPathChars, L".\\debug");
        CopyWide(g_exe_name, kMaxPathChars, L"cpp-algo.exe");
        return false;
    }

    std::filesystem::path exe(exe_path);
    const auto filename = exe.filename().wstring();
    CopyWide(g_exe_name, kMaxPathChars, filename.empty() ? L"cpp-algo.exe" : filename.c_str());

    const auto debug_dir = exe.parent_path().parent_path() / L"debug";
    CopyWide(g_dump_dir, kMaxPathChars, debug_dir.c_str());
    return true;
}

bool BuildDumpFilePath(wchar_t* out_path, size_t out_chars)
{
    if (out_path == nullptr || out_chars == 0) {
        return false;
    }
    const int written = _snwprintf_s(
        out_path,
        out_chars,
        _TRUNCATE,
        L"%s\\%s.%lu.dmp",
        g_dump_dir,
        g_exe_name,
        GetCurrentProcessId());
    return written > 0;
}

bool WriteMinidumpOnWorker()
{
    wchar_t dump_path[kMaxPathChars] = {};
    if (!BuildDumpFilePath(dump_path, kMaxPathChars)) {
        return false;
    }

    HANDLE file = CreateFileW(dump_path, GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file == INVALID_HANDLE_VALUE) {
        return false;
    }

    MINIDUMP_EXCEPTION_INFORMATION info {};
    MINIDUMP_EXCEPTION_INFORMATION* info_ptr = nullptr;
    if (g_request.exception_pointers != nullptr) {
        info.ThreadId = g_request.crashing_thread_id;
        info.ExceptionPointers = g_request.exception_pointers;
        info.ClientPointers = FALSE;
        info_ptr = &info;
    }

    const BOOL ok = MiniDumpWriteDump(
        GetCurrentProcess(),
        GetCurrentProcessId(),
        file,
        kDumpType,
        info_ptr,
        nullptr,
        nullptr);

    CloseHandle(file);
    return ok == TRUE;
}

DWORD WINAPI DumpWorkerThread(LPVOID)
{
    while (true) {
        const DWORD wait_result = WaitForSingleObject(g_request_ready, INFINITE);
        if (wait_result != WAIT_OBJECT_0) {
            continue;
        }
        WriteMinidumpOnWorker();
        SetEvent(g_request_done);
    }
}

void RequestMinidump(EXCEPTION_POINTERS* exception_pointers)
{
    if (g_request_ready == nullptr || g_request_done == nullptr) {
        return;
    }
    if (g_dumping.test_and_set(std::memory_order_acq_rel)) {
        return;
    }

    g_request.exception_pointers = exception_pointers;
    g_request.crashing_thread_id = GetCurrentThreadId();
    ResetEvent(g_request_done);
    SetEvent(g_request_ready);
    WaitForSingleObject(g_request_done, kDumpTimeoutMs);
}

LONG WINAPI UnhandledExceptionHandler(EXCEPTION_POINTERS* exception_pointers)
{
    RequestMinidump(exception_pointers);
    return EXCEPTION_CONTINUE_SEARCH;
}

void TerminateHandler()
{
    RequestMinidump(nullptr);
    std::_Exit(3);
}

void AbortSignalHandler(int)
{
    RequestMinidump(nullptr);
    std::_Exit(3);
}

} // namespace

void InstallCrashDumpHandler()
{
    if (g_request_ready != nullptr) {
        return;
    }

    InitDumpPaths();

    std::error_code ec;
    std::filesystem::create_directories(std::filesystem::path(g_dump_dir), ec);
    if (ec) {
        const std::string debug_dir = MAA_NS::path_to_utf8_string(std::filesystem::path(g_dump_dir));
        LogWarn << "Failed to create crash dump directory." << VAR(debug_dir);
    }

    g_request_ready = CreateEventW(nullptr, FALSE, FALSE, nullptr);
    g_request_done = CreateEventW(nullptr, TRUE, FALSE, nullptr);
    if (g_request_ready == nullptr || g_request_done == nullptr) {
        LogWarn << "Failed to create crash dump events; dump handler disabled.";
        if (g_request_ready != nullptr) {
            CloseHandle(g_request_ready);
            g_request_ready = nullptr;
        }
        if (g_request_done != nullptr) {
            CloseHandle(g_request_done);
            g_request_done = nullptr;
        }
        return;
    }

    HANDLE worker = CreateThread(nullptr, 0, DumpWorkerThread, nullptr, 0, nullptr);
    if (worker == nullptr) {
        const DWORD last_error = GetLastError();
        LogWarn << "Failed to create crash dump thread; dump handler disabled." << VAR(last_error);
        CloseHandle(g_request_ready);
        CloseHandle(g_request_done);
        g_request_ready = nullptr;
        g_request_done = nullptr;
        return;
    }
    CloseHandle(worker);

    SetUnhandledExceptionFilter(UnhandledExceptionHandler);
    std::set_terminate(TerminateHandler);
    std::signal(SIGABRT, AbortSignalHandler);

    const std::string debug_dir = MAA_NS::path_to_utf8_string(std::filesystem::path(g_dump_dir));
    LogInfo << "Crash dump handler installed." << VAR(debug_dir);
}

#else

void InstallCrashDumpHandler()
{
}

#endif

} // namespace common
