#pragma once

#include <cstdint>
#include <string>
#include <vector>

enum class CloudMouseAction
{
    Press,
    Release,
    Move,
    Wheel,
};

struct CloudMouseEvent
{
    CloudMouseAction action;
    // Logical 1280x720 page coordinates, independent of host window/DPI.
    int32_t x = 0;
    int32_t y = 0;
    // Left/right/middle are bits 0/1/2; contact -1 means no changed button.
    int32_t buttons = 0;
    int32_t delta_x = 0;
    int32_t delta_y = 0;
    int32_t contact = -1;
    // Alt/Control/Meta/Shift are bits 0/1/2/3, not native OS modifier flags.
    int32_t modifiers = 0;
};

struct CloudFrame
{
    int32_t width = 0;
    int32_t height = 0;
    // Website preparation can use a page image before decoded video exists.
    bool game_frame = true;
    // Packed, top-down BGR8: width * height * 3 bytes, without row padding.
    // The adapter owns capture synchronization; output must not alias a buffer
    // still being written by the host after capture returns.
    std::vector<uint8_t> pixels;
};

class CloudTransport
{
public:
    virtual ~CloudTransport() = default;
    virtual bool connect() = 0;
    virtual bool capture(CloudFrame& output) = 0;
    virtual bool mouse(const CloudMouseEvent& event) = 0;
    // Keycodes follow the existing PC Pipeline virtual-key contract. A platform
    // adapter maps its native key identities; never pass Linux evdev codes here.
    virtual bool key(int32_t keycode, bool down, int32_t modifiers, bool auto_repeat) = 0;
    // UTF-8 text, separate from physical key presses.
    virtual bool insert_text(const std::string& text) = 0;
    // Signed logical page deltas, not desktop pixels or a duration-based swipe.
    // A failed call may already have applied input; callers must not replay it.
    virtual bool relative_move(int32_t dx, int32_t dy, int32_t buttons, int32_t modifiers) = 0;
};
