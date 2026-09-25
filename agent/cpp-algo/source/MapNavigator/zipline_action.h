#pragma once

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

// 链上的一跳：不在架子上就先认提示站上去，然后把这一跳交给阶段机瞄准起滑。
// 起滑之后这一跳就交给 WaitZipline 相位了。
Result StartZiplineHop(const Context& ctx, const Waypoint& waypoint, double actual_distance);

// 滑行中的每一拍。阶段机自己判落点、滑错回程、重试；这里只把它的出口事件接回导航。
Result TickZiplineRide(const Context& ctx);

// 这个站位上没出上索提示：改瞄计划里的下一个站位走过去再认一次。站位全试过还不出提示就记一笔
// 「这根架子上不去」并退索走路，让后续重规划不再挑它当上索点。
Result AdvanceMountSpot(const Context& ctx, const Waypoint& waypoint, const char* reason);

// 滑索走不成时的退路：还站在架子上就先下来，再丢掉这条链剩下的每一跳。状态机随后等待稳定
// 定位，从剩余路线中第一个实际可达的点重新接入；接不回去就明确失败，不沿用从预期落点生成的旧路线。
Result AbandonZipline(const Context& ctx, const char* reason, const char* detail);

// 人还站在架子上、新路线又用不上这根架子时下来。没站在上面就什么也不发
void LeaveZiplineTower(const Context& ctx);

// 当前航点就是从脚下这根架子起滑的一跳: 人已经站在上面, 不用走过去也不用再按上索
bool CurrentHopStartsUnderfoot(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
