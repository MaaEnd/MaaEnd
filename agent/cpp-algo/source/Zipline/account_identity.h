#pragma once

#include <optional>
#include <string>
#include <string_view>

namespace zipline
{

// 游戏 UID 与网页 roleId 都是 8 到 12 位数字
bool IsValidRawUid(std::string_view uid);

// SHA-256(uid + salt) 的前 16 位小写十六进制, 盐与 go-service CaptureUid 共用, 仅 Windows 与 macOS 有实现
std::optional<std::string> HashUidForAccount(std::string_view uid);

} // namespace zipline
