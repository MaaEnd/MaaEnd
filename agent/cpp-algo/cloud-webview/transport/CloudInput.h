#pragma once

#include <iostream>
#include <map>
#include <mutex>
#include <set>
#include <utility>

#include "cloud_transport.h"

// MaaFramework's PC contact values are 0/1/2 (left/right/middle), and -1
// denotes hover. State belongs to this controller, never to desktop focus.
class CloudInput
{
public:
    explicit CloudInput(CloudTransport& transport)
        : transport_(transport)
    {
    }

    ~CloudInput()
    {
        try {
            if (!releaseAll()) {
                std::cerr << "Could not release all cloud controller input during cleanup.\n";
            }
        }
        catch (...) {
            std::cerr << "Cloud controller input cleanup failed.\n";
        }
    }

    bool touchDown(int32_t contact, int32_t x, int32_t y)
    {
        std::lock_guard lock(mutex_);
        if (contact == -1) {
            return true;
        }
        if (!valid_contact(contact)) {
            return false;
        }
        // Retain attempted presses until a release succeeds: a transport
        // failure can mean the browser applied an event but lost its reply.
        for (auto& [held_contact, position] : contacts_) {
            position = { x, y };
        }
        contacts_[contact] = { x, y };
        return transport_.mouse({ CloudMouseAction::Press, x, y, buttons(), 0, 0, contact, modifiers() });
    }

    bool touchMove(int32_t contact, int32_t x, int32_t y)
    {
        std::lock_guard lock(mutex_);
        if (contact != -1 && !valid_contact(contact)) {
            return false;
        }
        // Win32 mouse moves use contact -1 even while dragging. All buttons
        // share one pointer; releases must use its latest position, not the
        // coordinates at which each button was pressed.
        for (auto& [held_contact, position] : contacts_) {
            position = { x, y };
        }
        return transport_.mouse({ CloudMouseAction::Move, x, y, buttons(), 0, 0, -1, modifiers() });
    }

    bool touchUp(int32_t contact)
    {
        std::lock_guard lock(mutex_);
        if (contact == -1) {
            return true;
        }
        if (!valid_contact(contact)) {
            return false;
        }
        return release_contact(contact);
    }

    bool key(int32_t keycode, bool down)
    {
        std::lock_guard lock(mutex_);
        if (!down) {
            return release_key(keycode);
        }
        const bool repeated = !keys_.insert(keycode).second;
        return transport_.key(keycode, true, modifiers(), repeated);
    }

    bool relativeMove(int32_t dx, int32_t dy)
    {
        std::lock_guard lock(mutex_);
        return transport_.relative_move(dx, dy, buttons(), modifiers());
    }

    bool scroll(int32_t dx, int32_t dy)
    {
        std::lock_guard lock(mutex_);
        return transport_.mouse({ CloudMouseAction::Wheel, 640, 360, buttons(), dx, dy, -1, modifiers() });
    }

    bool releaseAll()
    {
        std::lock_guard lock(mutex_);
        const auto keys = keys_;
        const auto contacts = contacts_;
        bool ok = true;
        for (const auto keycode : keys) {
            ok = release_key(keycode) && ok;
        }
        for (const auto& [contact, position] : contacts) {
            ok = release_contact(contact) && ok;
        }
        return ok;
    }

private:
    static bool valid_contact(int32_t contact) { return contact >= 0 && contact <= 2; }

    int32_t buttons() const
    {
        int32_t mask = 0;
        for (const auto& [contact, position] : contacts_) {
            mask |= 1 << contact;
        }
        return mask;
    }

    int32_t modifiers() const
    {
        // CDP modifier bits; virtual-key codes follow the PC Pipeline contract.
        const auto held = [&](int generic, int left, int right) {
            return keys_.contains(generic) || keys_.contains(left) || keys_.contains(right);
        };
        return (held(18, 164, 165) ? 1 : 0) | (held(17, 162, 163) ? 2 : 0) | ((keys_.contains(91) || keys_.contains(92)) ? 4 : 0)
               | (held(16, 160, 161) ? 8 : 0);
    }

    bool release_key(int32_t keycode)
    {
        const bool held = keys_.erase(keycode) != 0;
        if (transport_.key(keycode, false, modifiers(), false)) {
            return true;
        }
        if (held) {
            keys_.insert(keycode);
        }
        return false;
    }

    bool release_contact(int32_t contact)
    {
        const auto it = contacts_.find(contact);
        if (it == contacts_.end()) {
            return true;
        }
        const auto [x, y] = it->second;
        if (!transport_.mouse({ CloudMouseAction::Release, x, y, buttons() & ~(1 << contact), 0, 0, contact, modifiers() })) {
            return false;
        }
        contacts_.erase(it);
        return true;
    }

    CloudTransport& transport_;
    std::mutex mutex_;
    std::map<int32_t, std::pair<int32_t, int32_t>> contacts_;
    std::set<int32_t> keys_;
};
