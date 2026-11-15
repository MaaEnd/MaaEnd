#pragma once

#include <cstdint>
#include <mutex>

#include <MaaFramework/MaaDef.h>

#include "latency_detector.h"

namespace mapnavigator
{

namespace latency
{

struct LatencyRun
{
    std::mutex mutex;
    Detector detector;
};

void BeginRun(LatencyRun* run);
void RecordStage(LatencyRun* run, Stage stage, int64_t elapsed_ms);

// 判定成立时就地给客户端发提示
void RecordTick(LatencyRun* run, MaaContext* context, uint64_t tick_seq, int64_t tick_gap_ms);

} // namespace latency

} // namespace mapnavigator
