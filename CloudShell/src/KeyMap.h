#pragma once

#include <optional>
#include <string>

namespace cloudshell
{

struct KeyInfo
{
    std::string code;
    std::string key;
    std::string shifted_key;
    int location = 0; // KeyboardEvent.location：0 标准、1 左、2 右、3 小键盘
};

// extended 取自 lParam 的第 24 位，用来区分左右修饰键与小键盘回车
std::optional<KeyInfo> LookupKey(unsigned vk, bool extended);

} // namespace cloudshell
