#pragma once

#include "Common/WebView2.h"

#include <condition_variable>
#include <mutex>

#include <meojson/json.hpp>

class CloudWindow : public WebView2
{
public:
    CloudWindow();
    ~CloudWindow() override;

    bool publish();
    void waitForClose();

    HWND nativeHandle() const noexcept { return GetHwnd(); }

    void captureVideoFrame(std::function<void(json::value)> on_done);

protected:
    void onUiThreadInit() override;
    void onUiThreadShutdown() override;
    void onBeforeNavigate(std::function<void(bool)> on_done) override;

    bool focusWebContentOnActivation() const override { return true; }

    void resizeToClientRect() override;
    std::optional<LRESULT> onMessage(UINT msg, WPARAM wparam, LPARAM lparam) override;

private:
    static LRESULT CALLBACK inputShieldProc(HWND hwnd, UINT msg, WPARAM wparam, LPARAM lparam);
    void shieldPhysicalInput();
    void configureViewport(std::function<void(bool)> on_done);
    double viewport_scale_ = 1.0;
    UINT shield_release_message_ = 0;
    bool manual_input_enabled_ = false;
    HWND input_shield_ = nullptr;
    std::mutex mutex_;
    std::condition_variable closed_cv_;
    bool closed_ = false;
    // Owned and released on the WebView STA. The controller duplicates only a
    // read handle, and copies pixels before acknowledging its frame request.
    Microsoft::WRL::ComPtr<ICoreWebView2SharedBuffer> video_buffer_;
};
