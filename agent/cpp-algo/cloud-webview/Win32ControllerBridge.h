#pragma once

#include <atomic>
#include <condition_variable>
#include <functional>
#include <mutex>
#include <thread>

#include <MaaUtils/SafeWindows.hpp>

#include "transport/CloudInput.h"

// An adapter for unmodified GUI/Framework Win32 controllers. The message loop
// must be independent of WebView's STA, which completes browser RPC callbacks.
class Win32ControllerBridge
{
public:
    explicit Win32ControllerBridge(CloudTransport& transport, std::function<void()> close_host = {});
    ~Win32ControllerBridge();
    Win32ControllerBridge(const Win32ControllerBridge&) = delete;
    Win32ControllerBridge& operator=(const Win32ControllerBridge&) = delete;

private:
    static LRESULT CALLBACK WindowProc(HWND hwnd, UINT message, WPARAM wparam, LPARAM lparam);
    LRESULT HandleMessage(HWND hwnd, UINT message, WPARAM wparam, LPARAM lparam);
    bool PrintFrame(HDC dc);
    bool RefreshFrame();
    void CaptureFrames(std::stop_token stop);
    void SetCapturing(bool enabled);

    CloudTransport& transport_;
    CloudInput input_;
    CloudFrame frame_;
    CloudFrame next_frame_;
    std::mutex frame_mutex_;
    std::mutex refresh_mutex_;
    std::atomic<bool> frame_message_pending_ = false;
    std::mutex capture_mutex_;
    std::condition_variable_any capture_cv_;
    bool capturing_ = false;
    bool capture_available_ = false;
    std::atomic<bool> game_frame_ = true;
    std::chrono::steady_clock::time_point preparation_deadline_ {};
    HANDLE owner_ = nullptr;
    HANDLE task_process_ = nullptr;
    intptr_t latest_task_id_ = 0;
    std::function<void()> close_host_;
    wchar_t high_surrogate_ = 0;
    std::atomic<HWND> hwnd_ = nullptr;
    std::jthread thread_;
    std::jthread capture_thread_;
};
