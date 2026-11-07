#include "Resources.h"

#include <windows.h>

namespace cloudshell
{

std::wstring LoadTextResource(int id)
{
    HRSRC info = FindResourceW(nullptr, MAKEINTRESOURCEW(id), RT_RCDATA);
    if (!info) {
        return {};
    }
    HGLOBAL handle = LoadResource(nullptr, info);
    const void* data = handle ? LockResource(handle) : nullptr;
    if (!data) {
        return {};
    }
    return Utf8ToWide(std::string_view(static_cast<const char*>(data), SizeofResource(nullptr, info)));
}

std::string WideToUtf8(std::wstring_view src)
{
    if (src.empty()) {
        return {};
    }
    const int needed = WideCharToMultiByte(CP_UTF8, 0, src.data(), static_cast<int>(src.size()), nullptr, 0, nullptr, nullptr);
    if (needed <= 0) {
        return {};
    }
    std::string out(static_cast<size_t>(needed), '\0');
    WideCharToMultiByte(CP_UTF8, 0, src.data(), static_cast<int>(src.size()), out.data(), needed, nullptr, nullptr);
    return out;
}

std::wstring Utf8ToWide(std::string_view src)
{
    if (src.empty()) {
        return {};
    }
    const int needed = MultiByteToWideChar(CP_UTF8, 0, src.data(), static_cast<int>(src.size()), nullptr, 0);
    if (needed <= 0) {
        return {};
    }
    std::wstring out(static_cast<size_t>(needed), L'\0');
    MultiByteToWideChar(CP_UTF8, 0, src.data(), static_cast<int>(src.size()), out.data(), needed);
    return out;
}

} // namespace cloudshell
