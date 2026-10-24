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

// 判定成立时就地给客户端发提示
void RecordTick(MaaContext* context, uint64_t tick_seq, int64_t tick_gap_ms);

} // namespace latency

} // namespace mapnavigator
