#pragma once

#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

struct HeadingTurnSteps
{
    int step_count = 0;
    double step_deg = 0.0;
    int step_interval_ms = 0;
};

// 1 度以内不用转, 返回 std::nullopt
std::optional<HeadingTurnSteps> PlanHeadingTurn(const InputBackend& input_backend, double heading_delta);
// 不是最后一步时发完等一个间隔
bool SendHeadingTurnStep(InputBackend* input_backend, const HeadingTurnSteps& turn, int step);
// 只转镜头不前进, 指令发不出去时返回 false
bool TurnToHeadingOnce(const Context& ctx, double heading_delta);
// 读不到镜头朝向时什么都不做
void AlignCameraAndRecapture(const Context& ctx);
// 连续两帧朝向一致才算数, 返回 false 时 out_heading 不可用
bool CaptureStableHeading(const Context& ctx, double* out_heading);
// 返回 true 只代表指令已发出, 是否转到位由 VerifyAndCorrectHeading 复核
bool TurnAndPulseForward(const Context& ctx, double heading_delta);
// 返回实际朝向, 读不到稳定朝向时返回 fallback_heading
double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading);
Result ConsumeHeadingNodes(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
