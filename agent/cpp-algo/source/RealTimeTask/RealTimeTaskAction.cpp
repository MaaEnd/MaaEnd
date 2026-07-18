#include "RealTimeTaskAction.h"

#include <algorithm>
#include <cstring>
#include <string>
#include <type_traits>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "Common/WebView2.h"

namespace realtimetask
{

namespace
{

constexpr const char* kHolderNodeName = "__RealTimeTaskAction_Holder";

struct AttachConfig
{
    bool skland_map_enable = false;
    std::string skland_map_url = "https://game.skland.com/map/endfield";
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
        ReadField(attach, "skland_map_url", cfg.skland_map_url);
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
            skmap_webview = std::make_shared<WebView2>();
            skmap_webview->SetContextMenuEnabled(false);
            skmap_webview->SetExcludeFromCapture(true);
            skmap_webview->SetShowInTaskbar(false);
            skmap_webview->SetTopMost(true);
            skmap_webview->SetTouchEmulation(true);
            skmap_webview->SetOpacity(static_cast<double>(attach.skland_map_opacity) / 100.0);
            skmap_webview->SetURL(attach.skland_map_url);
            skmap_webview->SetSize(640, 360);
            if (!skmap_webview->Open()) {
                LogError << "RealTimeTaskAction: skmap_webview open failed";
                break;
            }
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
                << VAR(attach.skland_map_url) << VAR(attach.skland_map_opacity) << VAR(attach.video_browser_enable)
                << VAR(attach.video_browser_opacity) << VAR(attach.video_browser_url);

        MaaTasker* tasker = MaaContextGetTasker(context);
        if (!tasker) {
            LogError << "RealTimeTaskAction: no tasker bound to context";
            break;
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
    if (skmap_webview) {
        skmap_webview->Close();
    }
    if (video_browser_webview) {
        video_browser_webview->Close();
    }
    return ret;
}

} // namespace realtimetask
