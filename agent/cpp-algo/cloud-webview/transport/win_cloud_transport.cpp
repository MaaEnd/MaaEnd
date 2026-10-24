#include "win_cloud_transport.h"

#include <memory>

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

bool WinCloudTransport::capture(CloudFrame& output)
{
    const auto response = cloud::Request(process_id_, json::object { { "op", "frame_shared" } });
    if (!response || !response->get("ok", false)) {
        return false;
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
    const auto [key, code] = cloud_key_identity(keycode);
    if (key.empty()) {
        return false;
    }
    return send(json::object { { "op", "key" },
                               { "params",
                                 json::object { { "type", down ? "keyDown" : "keyUp" },
                                                { "key", key },
                                                { "code", code },
                                                { "modifiers", modifiers },
                                                { "autoRepeat", auto_repeat },
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
