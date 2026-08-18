#pragma once

#include <cstdint>

#include <MaaFramework/MaaDef.h>

#include "latency_detector.h"

namespace mapnavigator
{

namespace latency
{

void BeginRun();
void RecordStage(Stage stage, int64_t elapsed_ms);

// 证据站得住时就地把提示发给客户端，除此之外只记录。
void RecordTick(MaaContext* context, uint64_t tick_seq, int64_t tick_gap_ms);

} // namespace latency

} // namespace mapnavigator
