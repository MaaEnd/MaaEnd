#pragma once

#include <cstddef>
#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

// 链上的一跳：不在架子上就先认提示站上去，再把镜头对准落点，按左键起滑。
// 起滑之后这一跳就交给 WaitZipline 相位了。
Result StartZiplineHop(
    const Context& ctx,
    const Waypoint& waypoint,
    double actual_distance,
    const std::optional<size_t>& arrived_absolute_node_idx);

// 滑行中的每一拍。只判「进没进落点圈」；落在中继架子上就接着瞄下一根，落在链尾才下索。
Result TickZiplineRide(const Context& ctx);

// 滑索走不成时的退路：还站在架子上就先下来，再丢掉这条链剩下的每一跳，重新规划一条走路的过去。
// 滑索省下的那点路换不来一次导航失败——所以这里不终止导航，reason 只进日志，说明为什么改成了徒步。
Result AbandonZipline(const Context& ctx, const char* reason, const char* detail);

} // namespace semantic_nodes

} // namespace mapnavigator
