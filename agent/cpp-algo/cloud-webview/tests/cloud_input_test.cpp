#include <iostream>
#include <tuple>
#include <vector>

#include "CloudInput.h"
#include "win_key_identity.h"

class RecordingTransport final : public CloudTransport
{
public:
    bool connect() override { return true; }

    bool capture(CloudFrame&) override { return false; }

    bool insert_text(const std::string&) override { return false; }

    bool mouse(const CloudMouseEvent& event) override
    {
        mouse_events.push_back(event);
        return success;
    }

    bool key(int32_t code, bool down, int32_t modifiers, bool repeated) override
    {
        key_events.emplace_back(code, down, modifiers, repeated);
        return success;
    }

    bool relative_move(int32_t, int32_t, int32_t buttons, int32_t modifiers) override
    {
        relative_buttons = buttons;
        relative_modifiers = modifiers;
        return success;
    }

    bool success = true;
    int32_t relative_buttons = 0;
    int32_t relative_modifiers = 0;
    std::vector<CloudMouseEvent> mouse_events;
    std::vector<std::tuple<int32_t, bool, int32_t, bool>> key_events;
};

int main()
{
    int checks = 0;
    int failures = 0;
    const auto expect = [&](bool result, const char* scenario) {
        ++checks;
        if (!result) {
            std::cerr << "FAIL: " << scenario << '\n';
            ++failures;
        }
    };
    // Values accepted by the GUI must retain their physical CDP key codes.
    const std::vector<std::tuple<int32_t, std::string, std::string>> mapped_keys {
        { 19, "Pause", "Pause" },
        { 20, "CapsLock", "CapsLock" },
        { 33, "PageUp", "PageUp" },
        { 34, "PageDown", "PageDown" },
        { 35, "End", "End" },
        { 36, "Home", "Home" },
        { 45, "Insert", "Insert" },
        { 46, "Delete", "Delete" },
        { 144, "NumLock", "NumLock" },
        { 145, "ScrollLock", "ScrollLock" },
        { 186, ";", "Semicolon" },
        { 187, "=", "Equal" },
        { 188, ",", "Comma" },
        { 189, "-", "Minus" },
        { 190, ".", "Period" },
        { 191, "/", "Slash" },
        { 192, "`", "Backquote" },
        { 219, "[", "BracketLeft" },
        { 220, "\\", "Backslash" },
        { 221, "]", "BracketRight" },
        { 222, "'", "Quote" },
        { 27, "Escape", "Escape" },
        { 65, "a", "KeyA" },
        { 90, "z", "KeyZ" },
        { 48, "0", "Digit0" },
        { 57, "9", "Digit9" },
        { 112, "F1", "F1" },
        { 135, "F24", "F24" },
        { 161, "Shift", "ShiftRight" },
        { 163, "Control", "ControlRight" },
        { 165, "Alt", "AltRight" },
    };
    for (const auto& [value, key, code] : mapped_keys) {
        expect(cloud_key_identity(value) == std::pair { key, code }, code.c_str());
    }
    expect(cloud_key_identity(-1).first.empty() && cloud_key_identity(255).first.empty(), "unknown keys are rejected");
    RecordingTransport transport;
    CloudInput input(transport);
    expect(input.touchDown(-1, 100, 200) && input.touchUp(-1) && transport.mouse_events.empty(), "hover cannot press a button");
    expect(input.touchMove(-1, 100, 200) && transport.mouse_events.back().buttons == 0, "hover moves with no held buttons");
    expect(!input.touchDown(31, 0, 0) && !input.touchMove(-2, 0, 0) && !input.touchUp(3), "invalid contacts cannot be shifted");
    expect(
        input.touchDown(0, 100, 200) && input.touchDown(1, 300, 400) && transport.mouse_events.back().buttons == 3,
        "simultaneous left and right presses preserve both buttons");
    expect(
        input.touchMove(1, 310, 410) && transport.mouse_events.back().buttons == 3 && transport.mouse_events.back().contact == -1,
        "drag move retains buttons and uses no changed button");
    expect(
        input.touchUp(1) && transport.mouse_events.back().x == 310 && transport.mouse_events.back().y == 410
            && transport.mouse_events.back().buttons == 1,
        "release uses the last drag position without releasing other buttons");
    expect(
        input.touchMove(-1, 500, 600) && input.touchUp(0) && transport.mouse_events.back().x == 500
            && transport.mouse_events.back().y == 600 && transport.mouse_events.back().buttons == 0,
        "Win32 hover-contact moves update the held button release position");
    input.touchDown(0, 500, 600);
    expect(
        input.key(16, true) && input.key('W', true) && std::get<2>(transport.key_events.back()) == 8,
        "Shift modifier is included with W");
    expect(input.key('W', true) && std::get<3>(transport.key_events.back()), "held key reports auto-repeat");
    expect(
        input.relativeMove(20, -5) && transport.relative_buttons == 1 && transport.relative_modifiers == 8,
        "relative camera input retains held button and key state");
    expect(
        input.scroll(0, 100) && transport.mouse_events.back().buttons == 1 && transport.mouse_events.back().modifiers == 8,
        "scroll retains held buttons and modifiers");
    expect(
        input.releaseAll() && transport.mouse_events.back().buttons == 0 && std::get<1>(transport.key_events.back()) == false,
        "inactive releases every key and button");
    const auto idle_events = transport.mouse_events.size() + transport.key_events.size();
    expect(
        input.releaseAll() && transport.mouse_events.size() + transport.key_events.size() == idle_events,
        "inactive with no held state emits no input");

    transport.success = false;
    expect(!input.touchDown(2, 10, 20) && !input.key(27, true), "failed press is reported");
    expect(!input.releaseAll(), "failed releases remain pending");
    transport.success = true;
    expect(
        input.releaseAll() && transport.mouse_events.back().action == CloudMouseAction::Release && transport.mouse_events.back().x == 10
            && std::get<0>(transport.key_events.back()) == 27 && !std::get<1>(transport.key_events.back()),
        "uncertain presses can still be released after a transport failure");
    {
        CloudInput temporary(transport);
        temporary.touchDown(0, 12, 34);
        temporary.key('A', true);
    }
    expect(
        transport.mouse_events.back().action == CloudMouseAction::Release && !std::get<1>(transport.key_events.back()),
        "destruction releases held input before transport teardown");
    if (failures) {
        return 1;
    }
    std::cout << "Cloud input: " << checks << " checks passed.\n";
    return 0;
}
