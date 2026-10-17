#pragma once

#include <chrono>
#include <functional>
#include <optional>
#include <string>
#include <vector>

#include <MaaFramework/MaaDef.h>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

void StopMotionAndCommitment(const Context& ctx);
void SelectPhaseForCurrentWaypoint(const Context& ctx, const char* reason);
// 到一个点之后的公共收尾：记账、推进、按下一个点选相位
Result CompleteArrival(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason);
// 只转镜头，不带前进脉冲。指令发不出去时返回 false。
bool TurnToHeadingOnce(const Context& ctx, double heading_delta, const std::function<bool()>& should_stop = {});
// 连着读到两帧一致的朝向才算数。读不出来返回 false，此时 out_heading 不可用。
bool CaptureStableHeading(const Context& ctx, double* out_heading);
// 连续读取到 deadline；用于必须等待真实反馈的闭环动作。超时前没有稳定朝向时返回 false。
bool CaptureStableHeadingUntil(const Context& ctx, double* out_heading, std::chrono::steady_clock::time_point deadline);
// 转向指令发不出去时返回 false。发得出去不代表转到位，转到位与否由 VerifyAndCorrectHeading 复核。
bool CommitHeadingTurn(const Context& ctx, double heading_delta);
// 复核转向结果并按需补一次。返回实际朝向；读不到稳定朝向时返回 fallback_heading。
double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading);
// 停车后按有效镜头观测对齐，仅前进脉冲，停稳后验收。所有退出释放输入。
// false 是非致命的未达标结果，调用方保留到点动作；取消时不得再执行动作。
bool SettleAtStrictGoal(const Context& ctx, const Waypoint& waypoint, const std::function<bool()>& should_stop);

// 调用成功与命中分开: 节点不存在或框架报错要当场判失败, 不能当成"没看见"
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
// 交回子任务里识别命中且动作做完的节点名; 派发失败或读不到任务详情返回 std::nullopt
std::optional<std::vector<std::string>>
    RunTaskForCompletedNodes(MaaContext* context, const char* entry, const std::string& pipeline_override);
// 截图发不出去或没等到结果就直接空手而归: 读缓存会拿到旧帧, 调用方会照着过期画面走
bool CaptureFreshFrame(MaaController* controller, MaaImageBuffer* buffer);

} // namespace semantic_nodes

} // namespace mapnavigator
