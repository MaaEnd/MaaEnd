#include "RealTimeTaskAction.h"

#include <cstring>
#include <string>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

namespace realtimetask
{

namespace
{

constexpr const char* kHolderNodeName = "__RealTimeTaskAction_Holder";

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
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    if (!context) {
        LogError << "RealTimeTaskAction: null context";
        return false;
    }

    std::vector<std::string> nodes;
    if (!ParseNodes(custom_action_param, nodes)) {
        return false;
    }

    const std::string pipeline_override = BuildPipelineOverride(nodes);
    LogInfo << "RealTimeTaskAction: start polling realtime nodes" << VAR(nodes.size());

    MaaTasker* tasker = MaaContextGetTasker(context);
    if (!tasker) {
        LogError << "RealTimeTaskAction: no tasker bound to context";
        return false;
    }

    while (!MaaTaskerStopping(tasker)) {
        const MaaTaskId child_id = MaaContextRunTask(context, kHolderNodeName, pipeline_override.c_str());
        if (child_id == MaaInvalidId) {
            LogWarn << "RealTimeTaskAction: RunTask returned invalid id, continue loop";
        }
    }

    LogInfo << "RealTimeTaskAction: tasker stopping signal received, exit loop";
    return true;
}

} // namespace realtimetask
