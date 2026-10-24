#pragma once

#include <atomic>
#include <memory>
#include <mutex>
#include <thread>

#include "CloudWindow.h"
#include "cloud_protocol.h"

class CloudBridge
{
public:
    explicit CloudBridge(CloudWindow& window);
    ~CloudBridge();

    // Called from the controller message thread, never the WebView STA.
    // Input must not queue behind a video capture on the external pipe.
    json::value input(const json::value& request);

private:
    void serve(std::stop_token stop);
    json::value dispatch(const json::value& request);

    CloudWindow& window_;
    HANDLE pipe_ = INVALID_HANDLE_VALUE;
    std::atomic<bool> close_requested_ { false };
    std::mutex input_mutex_;
    std::shared_ptr<std::atomic<bool>> frame_pending_ = std::make_shared<std::atomic<bool>>(false);
    std::jthread worker_;
};
