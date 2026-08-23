#include "CrashHandler.h"

#include <atomic>
#include <csignal>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <exception>
#include <iterator>
#include <string>
#include <system_error>

#include <MaaUtils/Logger.h>

#ifdef _WIN32

#include <MaaUtils/SafeWindows.hpp>

#include <dbghelp.h>

#endif

namespace common
{

namespace
{

// 重入多半是转储过程自己又炸了，安静退出好过写出半截文件。
std::atomic_flag g_dumping;

// 只在转储写完之后调用：它会分配内存，而 bad_alloc 正是可能的死因之一。
void LogTerminating(const char* source)
{
    // 有无待决异常足以分辨 abort 和未接住的抛出，异常内容本身在转储里。
    const bool uncaught_exception = std::current_exception() != nullptr;
    LogError << "Process terminating." << VAR(source) << VAR(uncaught_exception);
}

#ifdef _WIN32

// 崩溃退出码，外部据此认出这是非正常退出。
constexpr int kCrashExitCode = 3;

// 转储目录绝对路径，以反斜杠结尾。崩溃时只往后拼文件名，不做任何分配。
wchar_t g_dump_dir[MAX_PATH] = {};

// 还得放得下 cpp-algo-YYYYMMDD-HHMMSS-PID.dmp，余量不够就不装。
constexpr size_t kDumpNameReserve = 48;

void WriteDump(EXCEPTION_POINTERS* exception_pointers)
{
    if (g_dump_dir[0] == L'\0') {
        return;
    }

    SYSTEMTIME now {};
    GetLocalTime(&now);

    // 用 _TRUNCATE 而不是 swprintf_s：后者拼不下时会撞进非法参数钩子，而那时转储标记已置位，
    // 结果是既无转储也无日志的静默退出。这样即便文件名格式改长到超出 kDumpNameReserve，也只是失败返回。
    wchar_t path[MAX_PATH] = {};
    const int written = _snwprintf_s(
        path,
        std::size(path),
        _TRUNCATE,
        L"%scpp-algo-%04u%02u%02u-%02u%02u%02u-%lu.dmp",
        g_dump_dir,
        static_cast<unsigned>(now.wYear),
        static_cast<unsigned>(now.wMonth),
        static_cast<unsigned>(now.wDay),
        static_cast<unsigned>(now.wHour),
        static_cast<unsigned>(now.wMinute),
        static_cast<unsigned>(now.wSecond),
        GetCurrentProcessId());
    if (written < 0) {
        return;
    }

    HANDLE file = CreateFileW(path, GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file == INVALID_HANDLE_VALUE) {
        return;
    }

    MINIDUMP_EXCEPTION_INFORMATION info {};
    info.ThreadId = GetCurrentThreadId();
    info.ExceptionPointers = exception_pointers;
    info.ClientPointers = FALSE;

    // 带上被间接引用的内存，栈上的对象才能在调试器里展开。
    const MINIDUMP_TYPE type = static_cast<MINIDUMP_TYPE>(MiniDumpWithIndirectlyReferencedMemory | MiniDumpScanMemory);
    const BOOL ok = MiniDumpWriteDump(
        GetCurrentProcess(),
        GetCurrentProcessId(),
        file,
        type,
        exception_pointers != nullptr ? &info : nullptr,
        nullptr,
        nullptr);
    const DWORD dump_error = ok ? 0 : GetLastError();

    CloseHandle(file);

    const std::wstring dump_path(path);
    if (ok) {
        LogError << "Crash dump written." << VAR(dump_path);
    }
    else {
        LogError << "Failed to write crash dump." << VAR(dump_path) << VAR(dump_error);
    }
}

LONG WINAPI OnUnhandledException(EXCEPTION_POINTERS* exception_pointers)
{
    if (!g_dumping.test_and_set()) {
        WriteDump(exception_pointers);
        const DWORD exception_code = exception_pointers != nullptr && exception_pointers->ExceptionRecord != nullptr
                                         ? exception_pointers->ExceptionRecord->ExceptionCode
                                         : 0;
        LogError << "Process terminating on unhandled structured exception." << VAR(exception_code);
    }
    return EXCEPTION_EXECUTE_HANDLER;
}

// abort() 走 __fastfail，不经过用户态异常分发，所以转储得在这里自己写。
// 写完直接 _exit：再往下走会重新进入 abort，二次终止只会丢掉刚写的现场。
[[noreturn]] void OnTerminate()
{
    if (!g_dumping.test_and_set()) {
        WriteDump(nullptr);
        LogTerminating("terminate");
    }
    _exit(kCrashExitCode);
}

[[noreturn]] void OnAbortSignal(int)
{
    if (!g_dumping.test_and_set()) {
        WriteDump(nullptr);
        LogError << "Process aborted.";
    }
    _exit(kCrashExitCode);
}

// CRT 判定参数非法时直接终止进程，上面两个钩子都看不到。
void OnInvalidParameter(const wchar_t*, const wchar_t*, const wchar_t*, unsigned int, uintptr_t)
{
    if (!g_dumping.test_and_set()) {
        WriteDump(nullptr);
        LogError << "Process terminating on invalid CRT parameter.";
    }
    _exit(kCrashExitCode);
}

#else

[[noreturn]] void OnTerminate()
{
    if (!g_dumping.test_and_set()) {
        LogTerminating("terminate");
    }
    std::abort();
}

#endif

} // namespace

void InstallCrashHandler(const std::filesystem::path& dump_dir)
{
    std::error_code ec;
    const std::filesystem::path absolute_dir = std::filesystem::absolute(dump_dir, ec);
    if (ec) {
        LogWarn << "Failed to resolve crash dump directory; crash handler disabled." << VAR(dump_dir) << VAR(ec.message());
        return;
    }

    std::filesystem::create_directories(absolute_dir, ec);
    if (ec) {
        LogWarn << "Failed to create crash dump directory; crash handler disabled." << VAR(absolute_dir) << VAR(ec.message());
        return;
    }

#ifdef _WIN32
    const std::wstring prefix = absolute_dir.wstring() + L'\\';
    if (prefix.size() + kDumpNameReserve >= std::size(g_dump_dir)) {
        LogWarn << "Crash dump directory path is too long; crash handler disabled." << VAR(absolute_dir);
        return;
    }
    prefix.copy(g_dump_dir, prefix.size());
    g_dump_dir[prefix.size()] = L'\0';

    SetUnhandledExceptionFilter(OnUnhandledException);
    std::set_terminate(OnTerminate);
    std::signal(SIGABRT, OnAbortSignal);
    _set_invalid_parameter_handler(OnInvalidParameter);
#else
    std::set_terminate(OnTerminate);
#endif

    LogInfo << "Crash handler installed." << VAR(absolute_dir);
}

} // namespace common
