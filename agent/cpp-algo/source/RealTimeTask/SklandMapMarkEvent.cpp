#include "SklandMapMarkEvent.h"

#include <cstring>
#include <mutex>
#include <string>

#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

namespace realtimetask
{

namespace
{

// 当前的森空岛地图悬浮窗。事件节点与 RealTimeTaskAction 在同一个任务线程上执行，
// 但仍用 mutex 保护，避免以后把循环挪到别的线程时读到半更新状态。
std::mutex g_skland_map_mutex;
std::weak_ptr<WebView2> g_skland_map_webview;

} // namespace

void SetSklandMapWebView(std::shared_ptr<WebView2> webview)
{
    std::lock_guard<std::mutex> lock(g_skland_map_mutex);
    g_skland_map_webview = std::move(webview);
}

std::shared_ptr<WebView2> GetSklandMapWebView()
{
    std::lock_guard<std::mutex> lock(g_skland_map_mutex);
    return g_skland_map_webview.lock();
}

MaaBool MAA_CALL SklandMapMarkEventRun(
    [[maybe_unused]] MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    if (!custom_action_param || std::strlen(custom_action_param) == 0) {
        LogError << "SklandMapMarkEvent: empty custom_action_param";
        return false;
    }

    auto parsed = json::parse(custom_action_param);
    if (!parsed || !parsed->is_object()) {
        LogError << "SklandMapMarkEvent: invalid JSON object" << VAR(custom_action_param);
        return false;
    }

    const auto type = parsed->find<std::string>("type");
    if (!type || type->empty()) {
        LogError << "SklandMapMarkEvent: 'type' missing or empty" << VAR(custom_action_param);
        return false;
    }

    auto webview = GetSklandMapWebView();
    if (!webview) {
        // 用户没有打开地图悬浮窗时属于正常情况，不刷 Error。
        LogWarn << "SklandMapMarkEvent: skland map webview not available, event dropped" << VAR(*type);
        return false;
    }

    // type 只来自本仓库的 Pipeline，仍是外部输入：用 JSON 转义后再拼进脚本。
    const std::string script =
        "window.__maaMapMark && window.__maaMapMark.onEvent({type:" + json::value(*type).dumps() + "})";
    webview->ExecuteScript(script, [](bool ok, const std::string& result) {
        if (!ok) {
            LogWarn << "SklandMapMarkEvent: ExecuteScript failed" << VAR(result);
        }
    });

    LogDebug << "SklandMapMarkEvent: event dispatched" << VAR(*type);
    return true;
}

} // namespace realtimetask
