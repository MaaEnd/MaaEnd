#pragma once

#include <chrono>

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
// 连续读取到 deadline；用于必须等待真实反馈的闭环动作。超时前没有稳定朝向时返回 false。
bool CaptureStableHeadingUntil(const Context& ctx, double* out_heading, std::chrono::steady_clock::time_point deadline);
// 转向指令发不出去时返回 false。发得出去不代表转到位，转到位与否由 VerifyAndCorrectHeading 复核。
bool CommitHeadingTurn(const Context& ctx, double heading_delta);
// 复核转向结果并按需补一次。返回实际朝向；读不到稳定朝向时返回 fallback_heading。
double VerifyAndCorrectHeading(const Context& ctx, double target_heading, double fallback_heading);
// 刹停、等读数不动了再重测，差得多就转向目标走一小步复测。返回是否已进到验收圈内；
// 返回 false 只表示没能收拢（走不动/次数或时间用尽），点位照旧按判定圈算到达。
bool SettleAtStrictGoal(const Context& ctx, const Waypoint& waypoint);

} // namespace semantic_nodes

} // namespace mapnavigator
