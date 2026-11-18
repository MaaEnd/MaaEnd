#include "KeyMap.h"

#include <format>
#include <string_view>

#include <windows.h>

namespace cloudshell
{

namespace
{

KeyInfo same(std::string code, std::string key, int location = 0)
{
    std::string shifted = key;
    return KeyInfo { .code = std::move(code), .key = std::move(key), .shifted_key = std::move(shifted), .location = location };
}

KeyInfo shifted(std::string code, std::string key, std::string shifted_key)
{
    return KeyInfo { .code = std::move(code), .key = std::move(key), .shifted_key = std::move(shifted_key), .location = 0 };
}

} // namespace

std::optional<KeyInfo> LookupKey(unsigned vk, bool extended)
{
    if (vk >= 'A' && vk <= 'Z') {
        const char lower = static_cast<char>('a' + (vk - 'A'));
        const char upper = static_cast<char>(vk);
        return shifted(std::format("Key{}", upper), std::string(1, lower), std::string(1, upper));
    }

    if (vk >= '0' && vk <= '9') {
        constexpr std::string_view kDigitShifted = ")!@#$%^&*(";
        const char digit = static_cast<char>(vk);
        return shifted(std::format("Digit{}", digit), std::string(1, digit), std::string(1, kDigitShifted[vk - '0']));
    }

    if (vk >= VK_F1 && vk <= VK_F24) {
        const std::string name = std::format("F{}", vk - VK_F1 + 1);
        return same(name, name);
    }

    if (vk >= VK_NUMPAD0 && vk <= VK_NUMPAD9) {
        const char digit = static_cast<char>('0' + (vk - VK_NUMPAD0));
        return same(std::format("Numpad{}", digit), std::string(1, digit), 3);
    }

    switch (vk) {
    case VK_ESCAPE:
        return same("Escape", "Escape");
    case VK_RETURN:
        return extended ? same("NumpadEnter", "Enter", 3) : same("Enter", "Enter");
    case VK_TAB:
        return same("Tab", "Tab");
    case VK_SPACE:
        return same("Space", " ");
    case VK_BACK:
        return same("Backspace", "Backspace");
    case VK_DELETE:
        return same("Delete", "Delete");
    case VK_INSERT:
        return same("Insert", "Insert");
    case VK_HOME:
        return same("Home", "Home");
    case VK_END:
        return same("End", "End");
    case VK_PRIOR:
        return same("PageUp", "PageUp");
    case VK_NEXT:
        return same("PageDown", "PageDown");
    case VK_LEFT:
        return same("ArrowLeft", "ArrowLeft");
    case VK_RIGHT:
        return same("ArrowRight", "ArrowRight");
    case VK_UP:
        return same("ArrowUp", "ArrowUp");
    case VK_DOWN:
        return same("ArrowDown", "ArrowDown");
    case VK_CAPITAL:
        return same("CapsLock", "CapsLock");

    // 泛指的修饰键按扩展位区分左右；MaaFramework 投递的键码不带扩展位，一律落到左键
    case VK_SHIFT:
    case VK_LSHIFT:
        return same("ShiftLeft", "Shift", 1);
    case VK_RSHIFT:
        return same("ShiftRight", "Shift", 2);
    case VK_CONTROL:
        return extended ? same("ControlRight", "Control", 2) : same("ControlLeft", "Control", 1);
    case VK_LCONTROL:
        return same("ControlLeft", "Control", 1);
    case VK_RCONTROL:
        return same("ControlRight", "Control", 2);
    case VK_MENU:
        return extended ? same("AltRight", "Alt", 2) : same("AltLeft", "Alt", 1);
    case VK_LMENU:
        return same("AltLeft", "Alt", 1);
    case VK_RMENU:
        return same("AltRight", "Alt", 2);
    case VK_LWIN:
        return same("MetaLeft", "Meta", 1);
    case VK_RWIN:
        return same("MetaRight", "Meta", 2);

    case VK_MULTIPLY:
        return same("NumpadMultiply", "*", 3);
    case VK_ADD:
        return same("NumpadAdd", "+", 3);
    case VK_SUBTRACT:
        return same("NumpadSubtract", "-", 3);
    case VK_DECIMAL:
        return same("NumpadDecimal", ".", 3);
    case VK_DIVIDE:
        return same("NumpadDivide", "/", 3);

    case VK_OEM_1:
        return shifted("Semicolon", ";", ":");
    case VK_OEM_PLUS:
        return shifted("Equal", "=", "+");
    case VK_OEM_COMMA:
        return shifted("Comma", ",", "<");
    case VK_OEM_MINUS:
        return shifted("Minus", "-", "_");
    case VK_OEM_PERIOD:
        return shifted("Period", ".", ">");
    case VK_OEM_2:
        return shifted("Slash", "/", "?");
    case VK_OEM_3:
        return shifted("Backquote", "`", "~");
    case VK_OEM_4:
        return shifted("BracketLeft", "[", "{");
    case VK_OEM_5:
        return shifted("Backslash", "\\", "|");
    case VK_OEM_6:
        return shifted("BracketRight", "]", "}");
    case VK_OEM_7:
        return shifted("Quote", "'", "\"");

    default:
        return std::nullopt;
    }
}

} // namespace cloudshell
