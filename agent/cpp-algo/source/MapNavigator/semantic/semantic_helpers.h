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
Result CompleteArrival(const Context& ctx, const Waypoint& waypoint, const std::optional<size_t>& node_idx, const char* reason);
// 只转镜头不前进, 指令发不出去时返回 false
bool TurnToHeadingOnce(const Context& ctx, double heading_delta, const std::function<bool()>& should_stop = {});
// 连续两帧朝向一致才算数, 返回 false 时 out_heading 不可用
bool CaptureStableHeading(const Context& ctx, double* out_heading);
bool CaptureStableHeadingUntil(const Context& ctx, double* out_heading, std::chrono::steady_clock::time_point deadline);
// 返回 true 只代表指令已发出, 是否转到位由 VerifyAndCorrectHeading 复核
bool CommitHeadingTurn(const Context& ctx, double heading_delta);
// 返回实际朝向, 读不到稳定朝向时返回 fallback_heading
double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading);
int PlanPulseHoldMs(double residual, int max_hold_ms);
// 返回 false 是非致命的未达标, 调用方照常做到点动作, 取消时不得再做
bool SettleAtStrictGoal(const Context& ctx, const Waypoint& waypoint, const std::function<bool()>& should_stop);

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
// 截图失败时直接返回 false, 读缓存会拿到旧帧
bool CaptureFreshFrame(MaaController* controller, MaaImageBuffer* buffer);

} // namespace semantic_nodes

} // namespace mapnavigator
