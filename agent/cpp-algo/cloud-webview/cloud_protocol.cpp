#include "cloud_protocol.h"

#include <memory>

namespace cloud
{

namespace
{
using Handle = std::unique_ptr<void, decltype(&CloseHandle)>;
}

std::wstring PipeName(DWORD process_id)
{
    return L"\\\\.\\pipe\\MaaEnd.CloudWeb." + std::to_wstring(process_id);
}

bool Transfer(HANDLE pipe, void* data, DWORD size, bool write, std::stop_token stop)
{
    const Handle event(CreateEventW(nullptr, TRUE, FALSE, nullptr), &CloseHandle);
    if (!event) {
        return false;
    }
    auto* cursor = static_cast<char*>(data);
    const ULONGLONG deadline = GetTickCount64() + kRequestTimeoutMs;
    while (size && !stop.stop_requested()) {
        OVERLAPPED operation {};
        operation.hEvent = event.get();
        ResetEvent(event.get());
        DWORD transferred = 0;
        const BOOL ok =
            write ? WriteFile(pipe, cursor, size, &transferred, &operation) : ReadFile(pipe, cursor, size, &transferred, &operation);
        if (!ok && GetLastError() != ERROR_IO_PENDING) {
            return false;
        }
        if (!ok) {
            while (WaitForSingleObject(event.get(), 100) == WAIT_TIMEOUT) {
                if (stop.stop_requested() || GetTickCount64() >= deadline) {
                    CancelIoEx(pipe, &operation);
                    GetOverlappedResult(pipe, &operation, &transferred, TRUE);
                    return false;
                }
            }
            if (!GetOverlappedResult(pipe, &operation, &transferred, FALSE)) {
                return false;
            }
        }
        if (!transferred) {
            return false;
        }
        cursor += transferred;
        size -= transferred;
    }
    return size == 0;
}

std::optional<json::value> ReadMessage(HANDLE pipe, std::stop_token stop)
{
    uint32_t size = 0;
    if (!Transfer(pipe, &size, sizeof(size), false, stop) || !size || size > kMaxMessageSize) {
        return std::nullopt;
    }
    std::string body(size, '\0');
    if (!Transfer(pipe, body.data(), size, false, stop)) {
        return std::nullopt;
    }
    return json::parse(body);
}

bool WriteMessage(HANDLE pipe, const json::value& message, std::stop_token stop)
{
    std::string body = message.to_string();
    if (body.empty() || body.size() > kMaxMessageSize) {
        return false;
    }
    auto size = static_cast<uint32_t>(body.size());
    return Transfer(pipe, &size, sizeof(size), true, stop) && Transfer(pipe, body.data(), size, true, stop);
}

std::optional<json::value> Request(DWORD process_id, const json::value& message)
{
    const auto name = PipeName(process_id);
    const ULONGLONG deadline = GetTickCount64() + kRequestTimeoutMs;
    Handle pipe(INVALID_HANDLE_VALUE, &CloseHandle);
    while (true) {
        pipe.reset(CreateFileW(name.c_str(), GENERIC_READ | GENERIC_WRITE, 0, nullptr, OPEN_EXISTING, FILE_FLAG_OVERLAPPED, nullptr));
        if (pipe.get() != INVALID_HANDLE_VALUE) {
            break;
        }
        // Waiting does not reserve the instance. Re-open only on contention,
        // before sending anything; a dispatched request must never be replayed.
        const auto error = GetLastError();
        const auto now = GetTickCount64();
        if (error != ERROR_PIPE_BUSY || now >= deadline || !WaitNamedPipeW(name.c_str(), static_cast<DWORD>(deadline - now))) {
            return std::nullopt;
        }
    }
    if (!WriteMessage(pipe.get(), message)) {
        return std::nullopt;
    }
    const auto response = ReadMessage(pipe.get());
    char acknowledged = 1;
    if (!Transfer(pipe.get(), &acknowledged, 1, true)) {
        return std::nullopt;
    }
    return response;
}

} // namespace cloud
