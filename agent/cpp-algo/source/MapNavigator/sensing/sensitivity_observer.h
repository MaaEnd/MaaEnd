#pragma once

#include <atomic>
#include <cstdint>
#include <mutex>
#include <optional>

#include <MaaFramework/MaaDef.h>

#include "sensitivity_detector.h"

namespace mapnavigator
{

namespace sensitivity
{

struct SensitivityRun
{
    std::mutex mutex;
    Detector detector;
    std::atomic<double> turn_units_scale { 1.0 };
};

void BeginRun(SensitivityRun* run);

// 所有路径发出的偏航度数都要报到这里
void NoteTurnIssued(SensitivityRun* run, double delta_deg);

// 每个走到转向决策的拍调用一次, 判定成立时就地校正并发提示
void RecordTick(
    SensitivityRun* run,
    MaaContext* context,
    uint64_t tick_seq,
    std::optional<double> camera_heading_deg,
    double issued_delta_deg,
    bool degraded_fix);

void EndRun(SensitivityRun* run, MaaContext* context);

// 偏航与俯仰共用的输入量系数, 判定前恒为 1, 之后跨导航生效
double TurnUnitsScale(const SensitivityRun& run);

} // namespace sensitivity

} // namespace mapnavigator
