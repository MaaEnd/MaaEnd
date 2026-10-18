#include "RealTimeTaskAction.h"

#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <iterator>
#include <memory>
#include <string>
#include <thread>
#include <type_traits>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "Common/GameRegion.h"
#include "Common/WebView2.h"
#include "Common/output_paths.h"
#include "SklandMapMarkEvent.h"

namespace realtimetask
{

namespace
{

constexpr const char* kHolderNodeName = "__RealTimeTaskAction_Holder";
constexpr const char* kMapUrlCN = "https://game.skland.com/map/endfield";
constexpr const char* kMapUrlGlobal = "https://game.skport.com/map/endfield";

struct AttachConfig
{
    bool skland_map_enable = false;
    int skland_map_opacity = 100;
    bool video_browser_enable = false;
    int video_browser_opacity = 100;
    std::string video_browser_url = "https://www.bilibili.com";
};

template <typename T>
void ReadField(const json::object& attach, const char* key, T& out)
{
    if (!attach.contains(key)) {
        return;
    }
    const auto& v = attach.at(key);
    if constexpr (std::is_same_v<T, bool>) {
        if (!v.is_boolean()) {
            LogWarn << "RealTimeTaskAction: attach field expected boolean" << VAR(key);
            return;
        }
        out = v.as_boolean();
    }
    else if constexpr (std::is_same_v<T, std::string>) {
        if (!v.is_string()) {
            LogWarn << "RealTimeTaskAction: attach field expected string" << VAR(key);
            return;
        }
        out = v.as_string();
    }
    else if constexpr (std::is_same_v<T, int>) {
        if (!v.is_number()) {
            LogWarn << "RealTimeTaskAction: attach field expected number" << VAR(key);
            return;
        }
        out = std::clamp(v.as_integer(), 0, 100);
    }
    else {
        static_assert(sizeof(T) == 0, "unsupported attach field type");
    }
}

AttachConfig ReadAttach(MaaContext* context, const char* node_name)
{
    AttachConfig cfg;

    if (!context || !node_name) {
        return cfg;
    }

    MaaStringBuffer* buffer = MaaStringBufferCreate();
    if (!buffer) {
        LogWarn << "RealTimeTaskAction: MaaStringBufferCreate failed, use defaults";
        return cfg;
    }

    do {
        if (!MaaContextGetNodeData(context, node_name, buffer)) {
            LogWarn << "RealTimeTaskAction: MaaContextGetNodeData failed" << VAR(node_name);
            break;
        }

        const char* raw = MaaStringBufferGet(buffer);
        if (!raw || std::strlen(raw) == 0) {
            LogWarn << "RealTimeTaskAction: empty node data" << VAR(node_name);
            break;
        }

        auto parsed = json::parse(raw);
        if (!parsed || !parsed->is_object()) {
            LogWarn << "RealTimeTaskAction: invalid node JSON" << VAR(node_name);
            break;
        }

        const auto& obj = parsed->as_object();
        if (!obj.contains("attach") || !obj.at("attach").is_object()) {
            // 没有 attach 字段是合法的：外部直接进入本节点也允许，全用默认值。
            break;
        }

        const auto& attach = obj.at("attach").as_object();
        ReadField(attach, "skland_map_enable", cfg.skland_map_enable);
        ReadField(attach, "skland_map_opacity", cfg.skland_map_opacity);
        ReadField(attach, "video_browser_enable", cfg.video_browser_enable);
        ReadField(attach, "video_browser_opacity", cfg.video_browser_opacity);
        ReadField(attach, "video_browser_url", cfg.video_browser_url);
    } while (0);

    MaaStringBufferDestroy(buffer);
    return cfg;
}

bool ParseNodes(const char* custom_action_param, std::vector<std::string>& out_nodes)
{
    if (!custom_action_param || std::strlen(custom_action_param) == 0) {
        LogError << "RealTimeTaskAction: empty custom_action_param";
        return false;
    }

    auto parsed = json::parse(custom_action_param);
    if (!parsed || !parsed->is_object()) {
        LogError << "RealTimeTaskAction: invalid JSON object" << VAR(custom_action_param);
        return false;
    }

    if (!parsed->contains("nodes") || !parsed->at("nodes").is_array()) {
        LogError << "RealTimeTaskAction: 'nodes' missing or not an array" << VAR(custom_action_param);
        return false;
    }

    const auto& nodes_arr = parsed->at("nodes").as_array();
    if (nodes_arr.empty()) {
        LogError << "RealTimeTaskAction: 'nodes' must be non-empty";
        return false;
    }

    out_nodes.reserve(nodes_arr.size());
    for (const auto& v : nodes_arr) {
        if (!v.is_string()) {
            LogError << "RealTimeTaskAction: every entry in 'nodes' must be a string";
            return false;
        }
        out_nodes.push_back(v.as_string());
    }
    return true;
}

std::string BuildPipelineOverride(const std::vector<std::string>& nodes)
{
    json::array next_arr;
    for (const auto& n : nodes) {
        next_arr.emplace_back(n);
    }

    json::object holder;
    holder["next"] = std::move(next_arr);

    json::object root;
    root[kHolderNodeName] = std::move(holder);

    return json::value(std::move(root)).dumps();
}

// LoadAutoMarkScript 读取地图自动标记助手脚本，作为 WebView2 的初始化脚本注入。
// 脚本随资源一起分发，因此在安装目录下的 assets/resource 与打包后的 resource 两处查找。
// 找不到时返回空串：此时地图照常打开，只是不会自动标记。
std::string LoadAutoMarkScript()
{
    namespace fs = std::filesystem;

    const fs::path candidates[] = {
        common::OutputPath(fs::path("assets/resource/MapAutoMark/auto-mark.js")),
        common::OutputPath(fs::path("resource/MapAutoMark/auto-mark.js")),
    };

    for (const auto& path : candidates) {
        std::error_code ec;
        if (!fs::exists(path, ec) || ec) {
            continue;
        }
        std::ifstream in(path, std::ios::binary);
        if (!in) {
            continue;
        }
        std::string content { std::istreambuf_iterator<char>(in), std::istreambuf_iterator<char>() };
        if (!content.empty()) {
            LogInfo << "RealTimeTaskAction: loaded auto-mark script" << VAR(path.string()) << VAR(content.size());
            return content;
        }
    }

    LogWarn << "RealTimeTaskAction: auto-mark script not found, auto marking disabled";
    return {};
}

// WaitForSklandMapReady 轮询地图页面里的自动标记助手，直到它报告就绪或超时。
// 识别循环每轮截图都会把游戏窗口拉到前台；页面还在加载（例如首次登录）时就开始循环，
// 用户会连登录框都点不到。所以先等页面把地图实例和登录态准备好。
void WaitForSklandMapReady(WebView2* webview, MaaTasker* tasker, std::chrono::milliseconds timeout)
{
    auto ready = std::make_shared<std::atomic<bool>>(false);
    const auto deadline = std::chrono::steady_clock::now() + timeout;

    while (!ready->load() && !MaaTaskerStopping(tasker)) {
        if (std::chrono::steady_clock::now() >= deadline) {
            LogWarn << "RealTimeTaskAction: skland map not ready before timeout, start loop anyway";
            break;
        }
        webview->ExecuteScript(
            "!!(window.__maaMapMark && window.__maaMapMark.ready && window.__maaMapMark.ready())",
            [ready](bool ok, std::string result) {
                if (ok && result == "true") {
                    ready->store(true);
                }
            });
        std::this_thread::sleep_for(std::chrono::milliseconds(1000));
    }

    LogInfo << "RealTimeTaskAction: skland map ready wait finished" << VAR(ready->load());
}

} // namespace

MaaBool MAA_CALL RealTimeTaskActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    bool ret = false;
    std::shared_ptr<WebView2> skmap_webview;
    std::shared_ptr<WebView2> video_browser_webview;
    do {
        if (!context) {
            LogError << "RealTimeTaskAction: null context";
            break;
        }

        std::vector<std::string> nodes;
        if (!ParseNodes(custom_action_param, nodes)) {
            break;
        }

        const AttachConfig attach = ReadAttach(context, node_name);
        if (attach.skland_map_enable) {
            std::string map_url;
            switch (gamesetting::DetectGameRegion()) {
            case gamesetting::Region::CN:
                map_url = kMapUrlCN;
                break;
            case gamesetting::Region::Global:
                map_url = kMapUrlGlobal;
                break;
            case gamesetting::Region::Unknown:
                LogError << "RealTimeTaskAction: failed to resolve skland map URL from game region";
                break;
            }
            if (map_url.empty()) {
                break;
            }
            skmap_webview = std::make_shared<WebView2>();
            skmap_webview->SetContextMenuEnabled(false);
            skmap_webview->SetExcludeFromCapture(true);
            skmap_webview->SetShowInTaskbar(false);
            skmap_webview->SetTopMost(true);
            skmap_webview->SetTouchEmulation(true);
            skmap_webview->SetOpacity(static_cast<double>(attach.skland_map_opacity) / 100.0);
            skmap_webview->SetURL(map_url);
            skmap_webview->SetSize(640, 360);
            // document-start 注入自动标记助手；脚本拿不到时地图仍可正常打开。
            skmap_webview->SetInitScript(LoadAutoMarkScript());
            if (!skmap_webview->Open()) {
                LogError << "RealTimeTaskAction: skmap_webview open failed";
                break;
            }
            SetSklandMapWebView(skmap_webview);
            LogInfo << "RealTimeTaskAction: skland map opened" << VAR(map_url) << VAR(attach.skland_map_opacity);
        }
        if (attach.video_browser_enable) {
            video_browser_webview = std::make_shared<WebView2>();
            video_browser_webview->SetContextMenuEnabled(false);
            video_browser_webview->SetExcludeFromCapture(true);
            video_browser_webview->SetShowInTaskbar(false);
            video_browser_webview->SetTopMost(true);
            video_browser_webview->SetOpacity(static_cast<double>(attach.video_browser_opacity) / 100.0);
            video_browser_webview->SetURL(attach.video_browser_url);
            if (!video_browser_webview->Open()) {
                LogError << "RealTimeTaskAction: video_browser_webview open failed";
                break;
            }
        }
        const std::string pipeline_override = BuildPipelineOverride(nodes);
        LogInfo << "RealTimeTaskAction: start polling realtime nodes" << VAR(nodes.size()) << VAR(attach.skland_map_enable)
                << VAR(attach.skland_map_opacity) << VAR(attach.video_browser_enable) << VAR(attach.video_browser_opacity)
                << VAR(attach.video_browser_url);

        MaaTasker* tasker = MaaContextGetTasker(context);
        if (!tasker) {
            LogError << "RealTimeTaskAction: no tasker bound to context";
            break;
        }

        // 只有开了地图悬浮窗才需要等页面就绪；没有悬浮窗时保持原有行为。
        if (attach.skland_map_enable && skmap_webview) {
            LogInfo << "RealTimeTaskAction: waiting for skland map to become ready";
            WaitForSklandMapReady(skmap_webview.get(), tasker, std::chrono::minutes(5));
        }

        while (!MaaTaskerStopping(tasker)) {
            const MaaTaskId child_id = MaaContextRunTask(context, kHolderNodeName, pipeline_override.c_str());
            if (child_id == MaaInvalidId) {
                LogWarn << "RealTimeTaskAction: RunTask returned invalid id, continue loop";
            }
        }

        LogInfo << "RealTimeTaskAction: tasker stopping signal received, exit loop";
        ret = true;
    } while (0);
    // 事件节点只在循环期间有效，先注销再关窗口，避免悬空引用。
    SetSklandMapWebView(nullptr);
    if (skmap_webview) {
        skmap_webview->Close();
    }
    if (video_browser_webview) {
        video_browser_webview->Close();
    }
    return ret;
}

} // namespace realtimetask
