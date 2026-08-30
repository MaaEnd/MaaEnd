#pragma once

#include <cstddef>
#include <optional>

#include "semantic_nodes.h"

namespace mapnavigator
{

namespace semantic_nodes
{

enum class ZiplineAbandonMode
{
    Dismount,
    ReplanWhileMounted,
};

// 链上的一跳：不在架子上就先认提示站上去，再把镜头对准落点，按左键起滑。
// 起滑之后这一跳就交给 WaitZipline 相位了。
Result StartZiplineHop(
    const Context& ctx,
    const Waypoint& waypoint,
    double actual_distance,
    const std::optional<size_t>& arrived_absolute_node_idx);

// 滑行中的每一拍。只判「进没进落点圈」；落在中继架子上就接着瞄下一根，落在链尾才下索。
Result TickZiplineRide(const Context& ctx);

// 滑索走不成时的退路：丢掉这条链剩下的每一跳。通常立即下索；确认仍站在架子上的起滑耗尽
// 可以先保留上索状态，等状态机重规划后再决定。恢复接不回去就明确失败，不沿用预期落点的旧路线。
Result AbandonZipline(
    const Context& ctx,
    const char* reason,
    const char* detail,
    ZiplineAbandonMode mode = ZiplineAbandonMode::Dismount);

// 重规划确认下一段不能从当前架子直接起滑后，才真正下索并清除俯仰状态。
void DismountZipline(const Context& ctx);

} // namespace semantic_nodes

} // namespace mapnavigator
