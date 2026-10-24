#include "win_cloud_transport.h"

#include <memory>
#include <string_view>

#include <wincrypt.h>

#include <MaaUtils/NoWarningCV.hpp>

#include "cloud_protocol.h"
#include "win_key_identity.h"

namespace
{
using Handle = std::unique_ptr<void, decltype(&CloseHandle)>;

const char* button_name(int32_t contact)
{
    switch (contact) {
    case 0:
        return "left";
    case 1:
        return "right";
    case 2:
        return "middle";
    default:
        return "none";
    }
}

}

WinCloudTransport::WinCloudTransport(DWORD process_id, std::function<json::value(const json::value&)> local_input)
    : process_id_(process_id)
    , local_input_(std::move(local_input))
{
}

bool WinCloudTransport::send(const json::value& body)
{
    // A failed input may already have reached the game; never replay it over
    // the pipe. Remote transports keep the same request protocol as before.
    if (local_input_) {
        return local_input_(body).get("ok", false);
    }
    const auto result = cloud::Request(process_id_, body);
    return result && result->get("ok", false);
}

bool WinCloudTransport::connect()
{
    const auto status = cloud::Request(process_id_, json::object { { "op", "status" } });
    return status && status->get("ok", false);
}

bool WinCloudTransport::capturePage(CloudFrame& output)
{
    const auto response = cloud::Request(process_id_, json::object { { "op", "screencap" } });
    const auto value = response && response->get("ok", false) ? response->find<json::object>("value") : std::nullopt;
    const auto data = value ? value->find<std::string>("data") : std::nullopt;
    if (!data || data->empty() || data->size() > MAXDWORD) {
        return false;
    }
    DWORD size = 0;
    if (!CryptStringToBinaryA(data->data(), static_cast<DWORD>(data->size()), CRYPT_STRING_BASE64, nullptr, &size, nullptr, nullptr)) {
        return false;
    }
    std::vector<uint8_t> bytes(size);
    if (!CryptStringToBinaryA(data->data(), static_cast<DWORD>(data->size()), CRYPT_STRING_BASE64, bytes.data(), &size, nullptr, nullptr)) {
        return false;
    }
    bytes.resize(size);
    const auto image = cv::imdecode(bytes, cv::IMREAD_COLOR);
    if (image.cols != 1280 || image.rows != 720 || !image.isContinuous()) {
        return false;
    }
    output.pixels.assign(image.data, image.data + image.total() * image.elemSize());
    output.width = image.cols;
    output.height = image.rows;
    output.game_frame = false;
    return true;
}

bool WinCloudTransport::capture(CloudFrame& output)
{
    const auto response = cloud::Request(process_id_, json::object { { "op", "frame_shared" } });
    if (!response || !response->get("ok", false)) {
        // Before decoded video exists, Pipeline still needs a working GUI
        // controller to inspect DOM and dispatch startup clicks. Game captures
        // keep using raw shared frames; do not mask arbitrary transport errors.
        return response && response->get("error", std::string {}) == "video_unavailable" && capturePage(output);
    }
    const auto& frame = response->at("value");
    const auto mapping_handle = reinterpret_cast<HANDLE>(static_cast<uintptr_t>(frame.at("mapping_handle").as_integer()));
    const auto width = frame.get("width", 0);
    const auto height = frame.get("height", 0);
    const auto stride = frame.get("stride", 0);
    const auto byte_size = frame.get("byte_size", 0);
    const Handle mapping(mapping_handle, &CloseHandle);
    // Protocol v1 always exposes the fixed 720p viewport, independent of DPI.
    constexpr int kWidth = 1280;
    constexpr int kHeight = 720;
    constexpr int kStride = kWidth * 4;
    constexpr int kFrameBytes = kStride * kHeight;
    if (!mapping || mapping.get() == INVALID_HANDLE_VALUE || width != kWidth || height != kHeight || stride != kStride
        || byte_size != kFrameBytes || frame.get("protocol", 0) != 1 || frame.get("format", std::string {}) != "RGBA8") {
        return false;
    }
    const auto pixels = static_cast<const uint8_t*>(MapViewOfFile(mapping.get(), FILE_MAP_READ, 0, 0, byte_size));
    if (!pixels) {
        return false;
    }
    const auto unmap = [](const uint8_t* address) {
        UnmapViewOfFile(address);
    };
    const std::unique_ptr<const uint8_t, decltype(unmap)> view(pixels, unmap);
    output.pixels.resize(static_cast<size_t>(width) * height * 3);
    const cv::Mat rgba(height, width, CV_8UC4, const_cast<uint8_t*>(pixels), stride);
    cv::Mat bgr(height, width, CV_8UC3, output.pixels.data());
    cv::cvtColor(rgba, bgr, cv::COLOR_RGBA2BGR);
    output.width = width;
    output.height = height;
    output.game_frame = true;
    return true;
}

bool WinCloudTransport::mouse(const CloudMouseEvent& event)
{
    const char* type = "mouseMoved";
    if (event.action == CloudMouseAction::Press) {
        type = "mousePressed";
    }
    else if (event.action == CloudMouseAction::Release) {
        type = "mouseReleased";
    }
    else if (event.action == CloudMouseAction::Wheel) {
        type = "mouseWheel";
    }
    return send(json::object { { "op", "mouse" },
                               { "params",
                                 json::object { { "type", type },
                                                { "x", event.x },
                                                { "y", event.y },
                                                { "button",
                                                  (event.action == CloudMouseAction::Press || event.action == CloudMouseAction::Release)
                                                      ? button_name(event.contact)
                                                      : "none" },
                                                { "buttons", event.buttons },
                                                { "modifiers", event.modifiers },
                                                { "deltaX", event.delta_x },
                                                { "deltaY", event.delta_y },
                                                { "clickCount", 1 } } } });
}

bool WinCloudTransport::key(int32_t keycode, bool down, int32_t modifiers, bool auto_repeat)
{
    auto [key, code] = cloud_key_identity(keycode);
    if (key.empty()) {
        return false;
    }
    if (modifiers & 8) {
        if (keycode >= 'A' && keycode <= 'Z') {
            key.assign(1, static_cast<char>(keycode));
        }
        else if (keycode >= '0' && keycode <= '9') {
            key.assign(1, ")!@#$%^&*("[keycode - '0']);
        }
        else if (key.size() == 1) {
            const std::string_view plain = ";=,-./`[\\]'";
            const auto index = plain.find(key[0]);
            if (index != std::string_view::npos && !code.starts_with("Numpad")) {
                key.assign(1, ":+<_>?~{|}\""[index]);
            }
        }
    }
    return send(json::object { { "op", "key" },
                               { "params",
                                 json::object { { "type", down ? "keyDown" : "keyUp" },
                                                { "key", key },
                                                { "code", code },
                                                { "modifiers", modifiers },
                                                { "autoRepeat", auto_repeat },
                                                { "location",
                                                  code.starts_with("Numpad")                                              ? 3
                                                  : (keycode == 161 || keycode == 163 || keycode == 165 || keycode == 92) ? 2
                                                  : (keycode == 160 || keycode == 162 || keycode == 164 || keycode == 91) ? 1
                                                                                                                          : 0 },
                                                { "isKeypad", code.starts_with("Numpad") },
                                                { "windowsVirtualKeyCode", keycode },
                                                { "nativeVirtualKeyCode", keycode } } } });
}

bool WinCloudTransport::insert_text(const std::string& text)
{
    return send(json::object { { "op", "text" }, { "text", text } });
}

bool WinCloudTransport::relative_move(int32_t dx, int32_t dy, int32_t buttons, int32_t modifiers)
{
    return send(json::object { { "op", "relative" }, { "dx", dx }, { "dy", dy }, { "buttons", buttons }, { "modifiers", modifiers } });
}
