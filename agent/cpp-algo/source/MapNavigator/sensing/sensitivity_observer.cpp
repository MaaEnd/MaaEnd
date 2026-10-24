#include "sensitivity_observer.h"

#include <atomic>
#include <mutex>
#include <optional>

#include <MaaUtils/Logger.h>

#include "../../Common/notice.h"
#include "sensitivity_detector.h"

namespace mapnavigator
{

namespace sensitivity
{

namespace
{

std::mutex g_mutex;
Detector g_detector;
std::atomic<double> g_turn_units_scale { 1.0 };

void PublishVerdict(MaaContext* context, const Verdict& verdict)
{
    const int ratio_percent = verdict.ratio_percent;
    const int sample_count = verdict.sample_count;
    const double scale = 1.0 / verdict.ratio;
    g_turn_units_scale.store(scale);
    LogInfo << "Turn sensitivity corrected." << VAR(ratio_percent) << VAR(sample_count) << VAR(scale);

    common::notice::Publish(context, common::notice::Text("navigation.sensitivity_corrected", { ratio_percent, sample_count }));
}

void LogEstimate(const std::optional<Estimate>& estimate)
{
    if (!estimate) {
        return;
    }
    const double ratio = estimate->ratio;
    const double low = estimate->low;
    const double high = estimate->high;
    const int sample_count = estimate->sample_count;
    LogInfo << "Turn sensitivity estimate." << VAR(ratio) << VAR(low) << VAR(high) << VAR(sample_count);
}

} // namespace

void BeginRun()
{
    const double scale = g_turn_units_scale.load();
    if (scale != 1.0) {
        LogInfo << "Turn sensitivity scale in effect." << VAR(scale);
    }
    const std::lock_guard<std::mutex> guard(g_mutex);
    g_detector.BeginRun();
}

void NoteTurnIssued(double delta_deg)
{
    const std::lock_guard<std::mutex> guard(g_mutex);
    g_detector.NoteIssued(delta_deg);
}

double TurnUnitsScale()
{
    return g_turn_units_scale.load();
}

void RecordTick(
    MaaContext* context,
    uint64_t tick_seq,
    std::optional<double> camera_heading_deg,
    double issued_delta_deg,
    bool degraded_fix)
{
    std::optional<Verdict> verdict;
    std::optional<Estimate> estimate;
    {
        const std::lock_guard<std::mutex> guard(g_mutex);
        verdict = g_detector.RecordTick(tick_seq, camera_heading_deg, issued_delta_deg, degraded_fix);
        estimate = g_detector.TakeLastEstimate();
    }

    LogEstimate(estimate);
    if (verdict) {
        PublishVerdict(context, *verdict);
    }
}

void EndRun(MaaContext* context)
{
    std::optional<Verdict> verdict;
    std::optional<Estimate> estimate;
    {
        const std::lock_guard<std::mutex> guard(g_mutex);
        verdict = g_detector.EndRun();
        estimate = g_detector.TakeLastEstimate();
    }

    LogEstimate(estimate);
    if (verdict) {
        PublishVerdict(context, *verdict);
    }
}

} // namespace sensitivity

} // namespace mapnavigator
