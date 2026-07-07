#pragma once

#include <filesystem>
#include <system_error>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>
#include <MaaUtils/Platform.h>

#ifndef _WIN32
#include <unistd.h>
#endif

inline cv::Mat to_mat(const MaaImageBuffer* buffer)
{
    return cv::Mat(MaaImageBufferHeight(buffer), MaaImageBufferWidth(buffer), MaaImageBufferType(buffer), MaaImageBufferGetRawData(buffer));
}

// RAII owners for the C-API Maa*Buffer handles. The framework hands these out via *Create and requires a
// matching *Destroy; these wrap that lifetime so callers never leak on an early return. Shared here because
// every screencap/recognition caller needs the exact same boilerplate (previously copy-pasted into ~5 TUs).
class ScopedImageBuffer
{
public:
    ScopedImageBuffer()
        : buffer_(MaaImageBufferCreate())
    {
    }

    ~ScopedImageBuffer()
    {
        if (buffer_ != nullptr) {
            MaaImageBufferDestroy(buffer_);
        }
    }

    ScopedImageBuffer(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer& operator=(const ScopedImageBuffer&) = delete;

    MaaImageBuffer* Get() const { return buffer_; }

private:
    MaaImageBuffer* buffer_ = nullptr;
};

class ScopedStringBuffer
{
public:
    ScopedStringBuffer()
        : buffer_(MaaStringBufferCreate())
    {
    }

    ~ScopedStringBuffer()
    {
        if (buffer_ != nullptr) {
            MaaStringBufferDestroy(buffer_);
        }
    }

    ScopedStringBuffer(const ScopedStringBuffer&) = delete;
    ScopedStringBuffer& operator=(const ScopedStringBuffer&) = delete;

    MaaStringBuffer* Get() const { return buffer_; }

private:
    MaaStringBuffer* buffer_ = nullptr;
};

// Directory containing the running executable. Resolve bundled resources against this rather than
// the process current-working-directory: the CWD differs between dev and production, but resources
// always ship at a fixed location relative to the binary. This is the single anchor used by every
// resource lookup (MapLocator models, navmesh pack, ...).
inline std::filesystem::path get_exe_dir()
{
#ifdef _WIN32
    const auto process_path = MAA_NS::get_process_path(GetCurrentProcessId());
#else
    const auto process_path = MAA_NS::get_process_path(::getpid());
#endif
    if (process_path && !process_path->empty()) {
        return process_path->parent_path();
    }

    std::error_code ec;
    const std::filesystem::path cwd = std::filesystem::current_path(ec);
    if (!ec && !cwd.empty()) {
        return cwd;
    }
    return {};
}

#ifdef _WIN32

#include <MaaUtils/SafeWindows.hpp>

#include <string>

inline bool setup_dll_directory()
{
    constexpr int kMaxPath = 4096;
    wchar_t exe_path[kMaxPath] = { 0 };
    if (!GetModuleFileNameW(nullptr, exe_path, kMaxPath)) {
        return false;
    }

    // Find the last backslash to get the directory of the executable
    wchar_t* last_sep = wcsrchr(exe_path, L'\\');
    if (!last_sep) {
        return false;
    }
    *last_sep = L'\0';

    // Construct the path: <exe_dir>\..\maafw
    std::wstring maafw_dir = std::wstring(exe_path) + L"\\..\\maafw";

    return SetDllDirectoryW(maafw_dir.c_str()) != 0;
}

#endif
