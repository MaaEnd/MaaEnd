#pragma once

#ifdef _WIN32

#include <Windows.h>

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
