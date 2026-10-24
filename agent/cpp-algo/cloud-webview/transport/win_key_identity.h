#pragma once

#include <cstdint>
#include <string>
#include <utility>

// PI PC keycodes use Windows virtual-key values. Keep the CDP translation free
// of platform headers so it can be verified without a WebView or game session.
inline std::pair<std::string, std::string> cloud_key_identity(int32_t code)
{
    if (code >= 'A' && code <= 'Z') {
        return { std::string(1, static_cast<char>(code - 'A' + 'a')), "Key" + std::string(1, static_cast<char>(code)) };
    }
    if (code >= '0' && code <= '9') {
        const auto digit = std::string(1, static_cast<char>(code));
        return { digit, "Digit" + digit };
    }
    if (code >= 112 && code <= 135) {
        const auto function_key = "F" + std::to_string(code - 112 + 1);
        return { function_key, function_key };
    }
    switch (code) {
    case 8:
        return { "Backspace", "Backspace" };
    case 27:
        return { "Escape", "Escape" };
    case 13:
        return { "Enter", "Enter" };
    case 32:
        return { " ", "Space" };
    case 9:
        return { "Tab", "Tab" };
    case 16:
    case 160:
        return { "Shift", "ShiftLeft" };
    case 161:
        return { "Shift", "ShiftRight" };
    case 17:
    case 162:
        return { "Control", "ControlLeft" };
    case 163:
        return { "Control", "ControlRight" };
    case 18:
    case 164:
        return { "Alt", "AltLeft" };
    case 165:
        return { "Alt", "AltRight" };
    case 91:
        return { "Meta", "MetaLeft" };
    case 92:
        return { "Meta", "MetaRight" };
    case 38:
        return { "ArrowUp", "ArrowUp" };
    case 40:
        return { "ArrowDown", "ArrowDown" };
    case 37:
        return { "ArrowLeft", "ArrowLeft" };
    case 39:
        return { "ArrowRight", "ArrowRight" };
    case 19:
        return { "Pause", "Pause" };
    case 20:
        return { "CapsLock", "CapsLock" };
    case 33:
        return { "PageUp", "PageUp" };
    case 34:
        return { "PageDown", "PageDown" };
    case 35:
        return { "End", "End" };
    case 36:
        return { "Home", "Home" };
    case 45:
        return { "Insert", "Insert" };
    case 46:
        return { "Delete", "Delete" };
    case 144:
        return { "NumLock", "NumLock" };
    case 145:
        return { "ScrollLock", "ScrollLock" };
    case 186:
        return { ";", "Semicolon" };
    case 187:
        return { "=", "Equal" };
    case 188:
        return { ",", "Comma" };
    case 189:
        return { "-", "Minus" };
    case 190:
        return { ".", "Period" };
    case 191:
        return { "/", "Slash" };
    case 192:
        return { "`", "Backquote" };
    case 219:
        return { "[", "BracketLeft" };
    case 220:
        return { "\\", "Backslash" };
    case 221:
        return { "]", "BracketRight" };
    case 222:
        return { "'", "Quote" };
    default:
        return {};
    }
}
