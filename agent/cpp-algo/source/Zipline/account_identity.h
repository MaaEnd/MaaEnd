#pragma once

#include <optional>
#include <string>
#include <string_view>

namespace zipline
{

// 游戏 UID 与网页 roleId 都是 8--12 位数字；只有通过同一校验后才允许生成账号标识。
bool IsValidRawUid(std::string_view uid);

// 与 go-service CaptureUid 共用 random_salt.txt，并计算 SHA-256(uid + salt) 的前 16 位小写十六进制。
// ZiplineImport 仅在 Windows 注册；非 Windows 构建保留接口但返回空。
std::optional<std::string> HashUidForAccount(std::string_view uid);

} // namespace zipline
