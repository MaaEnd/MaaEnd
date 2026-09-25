#pragma once

#include <cstdint>

#include <MaaFramework/MaaDef.h>

namespace mapnavigator
{

namespace sensitivity
{

void BeginRun();

// 每份从输入出口发出去的偏航度数都报到这里，估计器靠它核对每拍到底发了多少。
void NoteTurnIssued(double delta_deg);

// 每个走到转向决策的拍调一次。证据站得住时就地校正并发提示，除此之外只记录。
void RecordTick(MaaContext* context, uint64_t tick_seq, int64_t now_ms, double heading_deg, double issued_delta_deg, bool degraded_fix);

// 每次导航结束调一次。
void EndRun(MaaContext* context);

// 偏航度→输入单位要乘的系数，判决落地前恒为 1，之后跨线路一直生效。
double TurnUnitsScale();

} // namespace sensitivity

} // namespace mapnavigator
