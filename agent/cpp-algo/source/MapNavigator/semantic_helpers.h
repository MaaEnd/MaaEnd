#pragma once

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

void StopMotionAndCommitment(const Context& ctx);
void SelectPhaseForCurrentWaypoint(const Context& ctx, const char* reason);
// 只转镜头，不带前进脉冲。指令发不出去时返回 false。
bool TurnToHeadingOnce(const Context& ctx, double heading_delta);
// 连着读到两帧一致的朝向才算数。读不出来返回 false，此时 out_heading 不可用。
bool CaptureStableHeading(const Context& ctx, double* out_heading);
// 转向指令发不出去时返回 false。发得出去不代表转到位，转到位与否由 VerifyAndCorrectHeading 复核。
bool CommitHeadingTurn(const Context& ctx, double heading_delta);
// 复核转向结果并按需补一次。返回实际朝向；读不到稳定朝向时返回 fallback_heading。
double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading);

} // namespace semantic_nodes

} // namespace mapnavigator
