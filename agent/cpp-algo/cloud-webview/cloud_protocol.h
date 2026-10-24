#pragma once

#include <cstdint>
#include <optional>
#include <stop_token>
#include <string>

#include <meojson/json.hpp>

#include <MaaUtils/SafeWindows.hpp>

namespace cloud
{

// Local, length-prefixed UTF-8 JSON. The process id binds a controller to one host
// lifetime; a restarted host must be discovered and connected again.
constexpr uint32_t kMaxMessageSize = 16 * 1024 * 1024;
constexpr DWORD kRequestTimeoutMs = 10000;

std::wstring PipeName(DWORD process_id);
bool Transfer(HANDLE pipe, void* data, DWORD size, bool write, std::stop_token stop = {});
std::optional<json::value> ReadMessage(HANDLE pipe, std::stop_token stop = {});
bool WriteMessage(HANDLE pipe, const json::value& message, std::stop_token stop = {});
std::optional<json::value> Request(DWORD process_id, const json::value& message);

} // namespace cloud
