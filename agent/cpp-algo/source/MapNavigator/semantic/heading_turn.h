#pragma once

#include <functional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

// 只转镜头不前进, 指令发不出去时返回 false
bool TurnToHeadingOnce(const Context& ctx, double heading_delta, const std::function<bool()>& should_stop = {});
// 读不到镜头朝向时什么都不做
void AlignCameraToHeadingOnce(const Context& ctx);
// 连续两帧朝向一致才算数, 返回 false 时 out_heading 不可用
bool CaptureStableHeading(const Context& ctx, double* out_heading);
// 返回 true 只代表指令已发出, 是否转到位由 VerifyAndCorrectHeading 复核
bool CommitHeadingTurn(const Context& ctx, double heading_delta);
// 返回实际朝向, 读不到稳定朝向时返回 fallback_heading
double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading);
Result ConsumeHeadingNodes(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
