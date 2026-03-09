#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <string_view>
#include <thread>
#include <utility>

#include <meojson/json.hpp>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>

#include "input_backend.h"
#include "navi_config.h"

namespace mapnavigator
{

namespace
{

void SleepIfNeeded(int delay_millis)
{
    if (delay_millis > 0) {
        std::this_thread::sleep_for(std::chrono::milliseconds(delay_millis));
    }
}

std::string DetectControllerType(MaaController* ctrl)
{
    if (ctrl == nullptr) {
        return {};
    }

    MaaStringBuffer* buffer = MaaStringBufferCreate();
    if (buffer == nullptr) {
        return {};
    }

    std::string controller_type;
    if (MaaControllerGetInfo(ctrl, buffer) && !MaaStringBufferIsEmpty(buffer)) {
        const char* raw = MaaStringBufferGet(buffer);
        if (raw != nullptr && raw[0] != '\0') {
            const auto info = json::parse(raw).value_or(json::object {});
            if (info.contains("type") && info.at("type").is_string()) {
                controller_type = info.at("type").as_string();
            }
        }
    }

    MaaStringBufferDestroy(buffer);
    return controller_type;
}

bool RequiresTouchBackend(std::string_view controller_type)
{
    return controller_type == "adb";
}

class DesktopInputBackend final : public IInputBackend
{
public:
    DesktopInputBackend(MaaController* ctrl, std::string controller_type)
        : ctrl_(ctrl)
        , controller_type_(std::move(controller_type))
    {
    }

    MaaController* GetCtrl() const override { return ctrl_; }

    const std::string& controller_type() const override { return controller_type_; }

    bool uses_touch_backend() const override { return false; }

    bool is_supported() const override { return true; }

    const std::string& unsupported_reason() const override { return unsupported_reason_; }

    double default_turn_units_per_degree() const override { return kDefaultPixelsPerDegree; }

    void KeyDownSync(int key_code, int delay_millis) override
    {
        auto id = MaaControllerPostKeyDown(ctrl_, key_code);
        MaaControllerWait(ctrl_, id);
        SleepIfNeeded(delay_millis);
    }

    void KeyUpSync(int key_code, int delay_millis) override
    {
        auto id = MaaControllerPostKeyUp(ctrl_, key_code);
        MaaControllerWait(ctrl_, id);
        SleepIfNeeded(delay_millis);
    }

    void ClickKeySync(int key_code, int hold_millis) override
    {
        KeyDownSync(key_code, 0);
        SleepIfNeeded(hold_millis);
        KeyUpSync(key_code, 0);
    }

    void TriggerSprintSync() override
    {
        MouseRightDownSync(0);
        SleepIfNeeded(kActionSprintPressMs);
        MouseRightUpSync(0);
    }

    void ResetForwardWalkSync(int release_millis) override
    {
        KeyUpSync(kKeyW, 0);
        SleepIfNeeded(release_millis);
        KeyDownSync(kKeyW, 0);
    }

    void ClickMouseLeftSync() override
    {
        EnsureHoverAnchorSync();
        auto id = MaaControllerPostClick(ctrl_, hover_x_, hover_y_);
        MaaControllerWait(ctrl_, id);
    }

    void MouseRightDownSync(int delay_millis) override
    {
        EnsureHoverAnchorSync();
        auto id = MaaControllerPostTouchDown(ctrl_, kPrimaryTouchContactId, hover_x_, hover_y_, kDefaultTouchPressure);
        MaaControllerWait(ctrl_, id);
        SleepIfNeeded(delay_millis);
    }

    void MouseRightUpSync(int delay_millis) override
    {
        auto id = MaaControllerPostTouchUp(ctrl_, kPrimaryTouchContactId);
        MaaControllerWait(ctrl_, id);
        SleepIfNeeded(delay_millis);
    }

    void SendRelativeMoveSync(int dx, int dy) override
    {
        if (dx == 0 && dy == 0) {
            return;
        }

        LogInfo << "SendRelativeMoveNative" << VAR(dx) << VAR(dy);
        auto id = MaaControllerPostRelativeMove(ctrl_, dx, dy);
        MaaControllerWait(ctrl_, id);
    }

private:
    void EnsureHoverAnchorSync()
    {
        if (hover_inited_) {
            return;
        }

        hover_inited_ = true;
        hover_x_ = kWorkCx;
        hover_y_ = kWorkCy;

        auto id = MaaControllerPostTouchMove(ctrl_, kHoverTouchContactId, hover_x_, hover_y_, kDefaultTouchPressure);
        MaaControllerWait(ctrl_, id);
    }

    MaaController* ctrl_ = nullptr;
    std::string controller_type_;
    std::string unsupported_reason_;
    int hover_x_ = kWorkCx;
    int hover_y_ = kWorkCy;
    bool hover_inited_ = false;
};

// Placeholder backend reserved for future ADB/touch input support.
// It preserves the backend selection structure while the actual
// touch injection path is not enabled in the current version.
class TouchBackendPlaceholder final : public IInputBackend
{
public:
    TouchBackendPlaceholder(MaaController* ctrl, std::string controller_type, std::string placeholder_reason, bool uses_touch_backend)
        : ctrl_(ctrl)
        , controller_type_(std::move(controller_type))
        , placeholder_reason_(std::move(placeholder_reason))
        , uses_touch_backend_(uses_touch_backend)
    {
    }

    MaaController* GetCtrl() const override { return ctrl_; }

    const std::string& controller_type() const override { return controller_type_; }

    bool uses_touch_backend() const override { return uses_touch_backend_; }

    bool is_supported() const override { return false; }

    const std::string& unsupported_reason() const override { return placeholder_reason_; }

    double default_turn_units_per_degree() const override { return kDefaultPixelsPerDegree; }

    void KeyDownSync(int key_code, int delay_millis) override { ReservedCall("KeyDownSync", key_code, delay_millis); }

    void KeyUpSync(int key_code, int delay_millis) override { ReservedCall("KeyUpSync", key_code, delay_millis); }

    void ClickKeySync(int key_code, int hold_millis) override { ReservedCall("ClickKeySync", key_code, hold_millis); }

    void TriggerSprintSync() override { ReservedCall("TriggerSprintSync", 0, 0); }

    void ResetForwardWalkSync(int release_millis) override { ReservedCall("ResetForwardWalkSync", release_millis, 0); }

    void ClickMouseLeftSync() override { ReservedCall("ClickMouseLeftSync", 0, 0); }

    void MouseRightDownSync(int delay_millis) override { ReservedCall("MouseRightDownSync", delay_millis, 0); }

    void MouseRightUpSync(int delay_millis) override { ReservedCall("MouseRightUpSync", delay_millis, 0); }

    void SendRelativeMoveSync(int dx, int dy) override { ReservedCall("SendRelativeMoveSync", dx, dy); }

private:
    void ReservedCall(const char* operation, int value_a, int value_b)
    {
        LogWarn << "ADB controller is recognized, but touch backend support is not enabled in the current version." << VAR(operation)
                << VAR(value_a) << VAR(value_b) << VAR(controller_type_) << VAR(placeholder_reason_);
    }

    MaaController* ctrl_ = nullptr;
    std::string controller_type_;
    std::string placeholder_reason_;
    bool uses_touch_backend_ = false;
};

} // namespace

std::unique_ptr<IInputBackend> CreateInputBackend(MaaContext* context, MaaController* ctrl)
{
    (void)context;

    std::string controller_type = DetectControllerType(ctrl);
    if (controller_type.empty()) {
        controller_type = "unknown";
    }

    if (RequiresTouchBackend(controller_type)) {
        // Reserved for future ADB/touch backend integration.
        // Keeping this branch here makes controller-to-backend dispatch stable
        // and avoids reshaping the selection flow when touch support is added.
        // Current behavior is an explicit "unsupported" result.
        const std::string placeholder_reason =
            "ADB navigation requires recognition-backed control mapping for joystick and action buttons; blind touch coordinates are "
            "disabled.";
        LogWarn << "MapNavigator input backend reserved (not enabled)." << VAR(controller_type) << VAR(placeholder_reason);
        return std::make_unique<TouchBackendPlaceholder>(ctrl, std::move(controller_type), placeholder_reason, true);
    }

    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=desktop";
    return std::make_unique<DesktopInputBackend>(ctrl, std::move(controller_type));
}

} // namespace mapnavigator
