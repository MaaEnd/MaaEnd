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
    const BOOL ok = MiniDumpWriteDump(GetCurrentProcess(), GetCurrentProcessId(), file, type, &info, nullptr, nullptr);
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

// 照 Crashpad 的做法：没有现成的异常现场时手工抓下当前现场，伪造一条异常记录交给转储。
// 不传现场时 dbghelp 自己取的当前线程上下文不可靠，调试器展不开栈。
// 抓到的现场指向本函数的栈帧，所以不能内联，且要在本函数里写完转储。
__declspec(noinline) void WriteDumpWithCurrentContext(DWORD exception_code)
{
    CONTEXT context {};
    RtlCaptureContext(&context);

    EXCEPTION_RECORD record {};
    record.ExceptionCode = exception_code;
    record.ExceptionFlags = EXCEPTION_NONCONTINUABLE;
#if defined(_M_ARM64)
    record.ExceptionAddress = reinterpret_cast<PVOID>(context.Pc);
#else
    record.ExceptionAddress = reinterpret_cast<PVOID>(context.Rip);
#endif

    EXCEPTION_POINTERS exception_pointers { &record, &context };
    WriteDump(&exception_pointers);
}

// 不用 set_terminate：它只对安装它的线程生效。任何线程未接住的异常、noexcept 违约都会走默认 terminate
// 再 abort 到这里，此时抛出点的栈帧还没回卷，抓下的现场能一路展开到 throw。
// 与 Crashpad 不同，只在有待决异常时写转储：没有待决异常的 abort 多是收尾时的直接 terminate，没有排查价值。
// 直接 _exit：信号处理函数返回后 CRT 会再次 abort。
[[noreturn]] void OnAbortSignal(int)
{
    if (!g_dumping.test_and_set()) {
        const bool uncaught_exception = std::current_exception() != nullptr;
        if (uncaught_exception) {
            WriteDumpWithCurrentContext(STATUS_FATAL_APP_EXIT);
        }
        LogError << "Process aborted." << VAR(uncaught_exception);
    }
    _exit(kCrashExitCode);
}

// CRT 判定参数非法时直接终止进程，上面两个钩子都看不到。
void OnInvalidParameter(const wchar_t*, const wchar_t*, const wchar_t*, unsigned int, uintptr_t)
{
    if (!g_dumping.test_and_set()) {
        WriteDumpWithCurrentContext(STATUS_INVALID_CRUNTIME_PARAMETER);
        LogError << "Process terminating on invalid CRT parameter.";
    }
    _exit(kCrashExitCode);
}

#else

[[noreturn]] void OnTerminate()
{
    if (!g_dumping.test_and_set()) {
        // 有无待决异常足以分辨 abort 和未接住的抛出。
        const bool uncaught_exception = std::current_exception() != nullptr;
        LogError << "Process terminating." << VAR(uncaught_exception);
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
    std::signal(SIGABRT, OnAbortSignal);
    _set_invalid_parameter_handler(OnInvalidParameter);
#else
    std::set_terminate(OnTerminate);
#endif

    LogInfo << "Crash handler installed." << VAR(absolute_dir);
}

} // namespace common
