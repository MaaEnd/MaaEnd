#pragma once

#include <functional>

#include <MaaUtils/SafeWindows.hpp>
#include <meojson/json.hpp>

#include "cloud_transport.h"

class WinCloudTransport final : public CloudTransport
{
public:
    explicit WinCloudTransport(DWORD process_id, std::function<json::value(const json::value&)> local_input = {});

    bool connect() override;
    bool capture(CloudFrame& output) override;
    bool mouse(const CloudMouseEvent& event) override;
    bool key(int32_t keycode, bool down, int32_t modifiers, bool auto_repeat) override;
    bool insert_text(const std::string& text) override;
    bool relative_move(int32_t dx, int32_t dy, int32_t buttons, int32_t modifiers) override;

private:
    bool capturePage(CloudFrame& output);
    bool send(const json::value& body);

    DWORD process_id_;
    std::function<json::value(const json::value&)> local_input_;
};
