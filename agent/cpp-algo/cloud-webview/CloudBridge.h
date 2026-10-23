#pragma once

#include <atomic>
#include <memory>
#include <thread>

#include "CloudWindow.h"
#include "cloud_protocol.h"

class CloudBridge
{
public:
    explicit CloudBridge(CloudWindow& window);
    ~CloudBridge();

private:
    void serve(std::stop_token stop);
    json::value dispatch(const json::value& request);

    CloudWindow& window_;
    HANDLE pipe_ = INVALID_HANDLE_VALUE;
    std::atomic<bool> close_requested_ { false };
    std::shared_ptr<std::atomic<bool>> frame_pending_ = std::make_shared<std::atomic<bool>>(false);
    HANDLE owner_process_ = nullptr;
    DWORD owner_pid_ = 0;
    std::string owner_token_;
    std::jthread worker_;
};
