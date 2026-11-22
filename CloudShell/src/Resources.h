#pragma once

#include <string>
#include <string_view>

namespace cloudshell
{

// 资源不存在时返回空串
std::wstring LoadTextResource(int id);

std::string WideToUtf8(std::wstring_view src);
std::wstring Utf8ToWide(std::string_view src);

} // namespace cloudshell
