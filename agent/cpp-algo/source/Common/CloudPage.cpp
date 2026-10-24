#include "CloudPage.h"

#include <cmath>
#include <optional>
#include <string>
#include <string_view>

#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../utils.h"
#ifdef _WIN32
#include "../../cloud-webview/cloud_protocol.h"
#include "../../cloud-webview/win32_bridge_protocol.h"
#endif

namespace cloud
{
namespace
{
#ifdef _WIN32
std::optional<json::value> request(MaaContext* context, const json::value& body)
{
    const auto tasker = context ? MaaContextGetTasker(context) : nullptr;
    const auto controller = tasker ? MaaTaskerGetController(tasker) : nullptr;
    ScopedStringBuffer info_buffer;
    if (!controller || !MaaControllerGetInfo(controller, info_buffer.Get())) {
        return std::nullopt;
    }
    const auto info = json::parse(MaaStringBufferGet(info_buffer.Get()));
    if (!info || !info->is_object() || info->get("type", std::string {}) != "win32") {
        return std::nullopt;
    }
    const auto hwnd = reinterpret_cast<HWND>(static_cast<uintptr_t>(info->get("hwnd", int64_t { 0 })));
    wchar_t class_name[128] {};
    DWORD process_id = 0;
    if (!GetClassNameW(hwnd, class_name, static_cast<int>(std::size(class_name))) || std::wstring_view(class_name) != win32bridge::kClass
        || !GetWindowThreadProcessId(hwnd, &process_id)) {
        return std::nullopt;
    }
    auto response = Request(process_id, body);
    if (!response || !response->is_object() || !response->get("ok", false)) {
        return std::nullopt;
    }
    return response;
}

std::optional<double> recognition_epoch(const json::value& result)
{
    const auto best = result.find<json::object>("best");
    const auto detail = best ? best->find<json::object>("detail") : std::nullopt;
    return detail ? detail->find<double>("document_epoch") : std::nullopt;
}

std::optional<double> previous_document(MaaContext* context, const std::string& node)
{
    const auto tasker = MaaContextGetTasker(context);
    MaaNodeId node_id = MaaInvalidId;
    MaaRecoId reco_id = MaaInvalidId;
    ScopedStringBuffer detail;
    if (!tasker || !MaaTaskerGetLatestNode(tasker, node.c_str(), &node_id)
        || !MaaTaskerGetNodeDetail(tasker, node_id, nullptr, &reco_id, nullptr, nullptr)
        || !MaaTaskerGetRecognitionDetail(tasker, reco_id, nullptr, nullptr, nullptr, nullptr, detail.Get(), nullptr, nullptr)) {
        return std::nullopt;
    }
    const auto parsed = json::parse(MaaStringBufferGet(detail.Get()));
    // Custom recognition detail is wrapped by Framework in "best".
    return parsed ? recognition_epoch(*parsed) : std::nullopt;
}
#endif
} // namespace

MaaBool MAA_CALL PageStateRun(
    MaaContext* context,
    MaaTaskId,
    const char*,
    const char*,
    const char* params,
    const MaaImageBuffer*,
    const MaaRect*,
    void*,
    MaaRect* box,
    MaaStringBuffer* detail)
{
#ifdef _WIN32
    try {
        const auto query = json::parse(params ? params : "{}");
        if (!query || !query->is_object()) {
            return false;
        }
        const auto response = request(context, json::object { { "op", "status" } });
        const auto state = response ? response->find<json::object>("value") : std::nullopt;
        if (!state) {
            return false;
        }
        if (const auto match = query->find<json::object>("match")) {
            for (const auto& [key, expected] : *match) {
                if (!state->contains(key) || state->at(key) != expected) {
                    return false;
                }
            }
        }
        if (const auto changed_from = query->find<std::string>("changed_from")) {
            const auto previous = previous_document(context, *changed_from);
            const auto current = state->find<double>("document_epoch");
            if (!previous || !current || *previous == *current) {
                return false;
            }
        }
        MaaRect target { 0, 0, 1, 1 };
        if (const auto name = query->find<std::string>("target")) {
            const auto point = state->find<json::object>(*name);
            const auto x = point ? point->find<double>("x") : std::nullopt;
            const auto y = point ? point->find<double>("y") : std::nullopt;
            if (!x || !y || !std::isfinite(*x) || !std::isfinite(*y) || *x < 0 || *x >= 1280 || *y < 0 || *y >= 720) {
                return false;
            }
            target = { static_cast<int32_t>(*x), static_cast<int32_t>(*y), 1, 1 };
        }
        if (box) {
            *box = target;
        }
        // Only public page-state metadata, never forms or account identifiers.
        if (detail) {
            MaaStringBufferSet(detail, json::value(*state).to_string().c_str());
        }
        return true;
    }
    catch (const std::exception& error) {
        LogError << "Cloud page state failed" << error.what();
    }
#else
    (void)context;
    (void)params;
    (void)box;
    (void)detail;
#endif
    return false;
}

MaaBool MAA_CALL
    PageReloadRun(MaaContext* context, MaaTaskId, const char*, const char*, const char* params, MaaRecoId reco_id, const MaaRect*, void*)
{
#ifdef _WIN32
    try {
        const auto parsed = json::parse(params ? params : "{}");
        if (!parsed || !parsed->is_object()) {
            return false;
        }
        const auto tasker = context ? MaaContextGetTasker(context) : nullptr;
        ScopedStringBuffer detail;
        if (!tasker
            || !MaaTaskerGetRecognitionDetail(tasker, reco_id, nullptr, nullptr, nullptr, nullptr, detail.Get(), nullptr, nullptr)) {
            return false;
        }
        const auto recognized = json::parse(MaaStringBufferGet(detail.Get()));
        const auto epoch = recognized ? recognition_epoch(*recognized) : std::nullopt;
        const auto response = request(context, json::object { { "op", "status" } });
        const auto current = response ? response->find<json::object>("value") : std::nullopt;
        if (!epoch || !current) {
            return false;
        }
        // Pipeline's failure-only backoff can overlap a successful navigation
        // or user login. Never reload that newer/healthy document.
        if (current->get("document_epoch", 0.0) != *epoch || !current->get("startup_resource_failed", false)) {
            return true;
        }
        return request(context, json::object { { "op", "reload" }, { "ignore_cache", parsed->get("ignore_cache", false) } }).has_value();
    }
    catch (const std::exception& error) {
        LogError << "Cloud page reload failed" << error.what();
    }
#else
    (void)context;
    (void)params;
    (void)reco_id;
#endif
    return false;
}
} // namespace cloud
