#include "maa_task.h"

#include <algorithm>
#include <optional>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../../utils.h"

namespace mapnavigator
{

bool RunRecognitionNode(
    MaaContext* context,
    const std::string& node,
    const std::string& pipeline_override,
    const MaaImageBuffer* image,
    NodeSighting* out_sighting)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        LogError << "Tasker is unavailable for recognition." << VAR(node);
        return false;
    }

    const MaaRecoId reco_id = MaaContextRunRecognition(context, node.c_str(), pipeline_override.c_str(), image);
    if (reco_id == MaaInvalidId) {
        LogError << "Recognition failed to dispatch; check the node name and its params." << VAR(node);
        return false;
    }

    MaaBool hit = 0;
    MaaRect box {};
    if (!MaaTaskerGetRecognitionDetail(tasker, reco_id, nullptr, nullptr, &hit, &box, nullptr, nullptr, nullptr)) {
        LogError << "Recognition detail is unavailable." << VAR(node) << VAR(reco_id);
        return false;
    }

    out_sighting->hit = hit != 0;
    out_sighting->box = box;
    return true;
}

std::optional<std::vector<std::string>>
    RunTaskForCompletedNodes(MaaContext* context, const char* entry, const std::string& pipeline_override)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        return std::nullopt;
    }
    const MaaTaskId task_id = MaaContextRunTask(context, entry, pipeline_override.c_str());
    if (task_id == MaaInvalidId) {
        LogWarn << "Subtask failed to dispatch." << VAR(entry);
        return std::nullopt;
    }

    ScopedStringBuffer entry_name;
    MaaSize node_count = 0;
    MaaStatus status = MaaStatus_Invalid;
    if (entry_name.Get() == nullptr || !MaaTaskerGetTaskDetail(tasker, task_id, entry_name.Get(), nullptr, &node_count, &status)) {
        return std::nullopt;
    }
    std::vector<std::string> completed_nodes;
    if (node_count == 0) {
        return completed_nodes;
    }
    std::vector<MaaNodeId> node_ids(node_count);
    if (!MaaTaskerGetTaskDetail(tasker, task_id, entry_name.Get(), node_ids.data(), &node_count, &status)) {
        return std::nullopt;
    }

    for (const MaaNodeId node_id : node_ids) {
        ScopedStringBuffer node_name;
        MaaRecoId reco_id = 0;
        MaaActId action_id = 0;
        MaaBool completed = 0;
        if (node_name.Get() == nullptr || !MaaTaskerGetNodeDetail(tasker, node_id, node_name.Get(), &reco_id, &action_id, &completed)) {
            return std::nullopt;
        }
        const char* raw = MaaStringBufferGet(node_name.Get());
        if (raw != nullptr && completed != 0) {
            completed_nodes.emplace_back(raw);
        }
    }
    return completed_nodes;
}

std::optional<bool> RunTaskAndCheckNode(MaaContext* context, const char* entry, const char* node, const std::string& pipeline_override)
{
    const std::optional<std::vector<std::string>> completed = RunTaskForCompletedNodes(context, entry, pipeline_override);
    if (!completed) {
        return std::nullopt;
    }
    return std::find(completed->begin(), completed->end(), node) != completed->end();
}

bool CaptureFreshFrame(MaaController* controller, MaaImageBuffer* buffer)
{
    const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller);
    if (screencap_id == MaaInvalidId) {
        LogWarn << "Screencap request was not posted.";
        return false;
    }
    if (MaaControllerWait(controller, screencap_id) != MaaStatus_Succeeded) {
        LogWarn << "Screencap did not succeed." << VAR(screencap_id);
        return false;
    }
    return MaaControllerCachedImage(controller, buffer) && !MaaImageBufferIsEmpty(buffer);
}

json::object ExpectedTextOverride(const std::vector<std::string>& texts)
{
    json::array expected;
    for (const std::string& text : texts) {
        expected.emplace_back(text);
    }

    json::object param;
    param["expected"] = std::move(expected);
    json::object recognition;
    recognition["param"] = std::move(param);
    json::object node;
    node["recognition"] = std::move(recognition);
    return node;
}

} // namespace mapnavigator
