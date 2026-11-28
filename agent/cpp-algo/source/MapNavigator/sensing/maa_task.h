#pragma once

#include <optional>
#include <string>
#include <vector>

#include <MaaFramework/MaaDef.h>
#include <meojson/json.hpp>

namespace mapnavigator
{

// 调用失败与未命中分开, 节点不存在或框架报错须判失败
struct NodeSighting
{
    bool hit = false;
    MaaRect box {};
};

bool RunRecognitionNode(
    MaaContext* context,
    const std::string& node,
    const std::string& pipeline_override,
    const MaaImageBuffer* image,
    NodeSighting* out_sighting);
// 派发失败或读不到任务详情时返回 std::nullopt
std::optional<std::vector<std::string>>
    RunTaskForCompletedNodes(MaaContext* context, const char* entry, const std::string& pipeline_override);
// 读不到子任务结果时返回 std::nullopt
std::optional<bool> RunTaskAndCheckNode(MaaContext* context, const char* entry, const char* node, const std::string& pipeline_override);
// 截图失败时直接返回 false, 读缓存会拿到旧帧
bool CaptureFreshFrame(MaaController* controller, MaaImageBuffer* buffer);
// 只覆盖 expected, roi 与阈值留给 pipeline
json::object ExpectedTextOverride(const std::vector<std::string>& texts);

} // namespace mapnavigator
