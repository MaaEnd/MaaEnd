#include "sensitivity_observer.h"

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

void publish_verdict(SensitivityRun* run, MaaContext* context, const Verdict& verdict)
{
    const int ratio_percent = verdict.ratio_percent;
    const int sample_count = verdict.sample_count;
    const double scale = 1.0 / verdict.ratio;
    run->turn_units_scale.store(scale);
    LogInfo << "Turn sensitivity corrected." << VAR(ratio_percent) << VAR(sample_count) << VAR(scale);

    common::notice::Publish(context, common::notice::Text("navigation.sensitivity_corrected", { ratio_percent, sample_count }));
}

void log_estimate(const std::optional<Estimate>& estimate)
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

void BeginRun(SensitivityRun* run)
{
    const double scale = run->turn_units_scale.load();
    if (scale != 1.0) {
        LogInfo << "Turn sensitivity scale in effect." << VAR(scale);
    }
    const std::lock_guard<std::mutex> guard(run->mutex);
    BeginDetectorRun(&run->detector);
}

void NoteTurnIssued(SensitivityRun* run, double delta_deg)
{
    const std::lock_guard<std::mutex> guard(run->mutex);
    NoteDetectorIssued(&run->detector, delta_deg);
}

double TurnUnitsScale(const SensitivityRun& run)
{
    return run.turn_units_scale.load();
}

void RecordTick(
    SensitivityRun* run,
    MaaContext* context,
    uint64_t tick_seq,
    std::optional<double> camera_heading_deg,
    double issued_delta_deg,
    bool degraded_fix)
{
    std::optional<Verdict> verdict;
    std::optional<Estimate> estimate;
    {
        const std::lock_guard<std::mutex> guard(run->mutex);
        verdict = RecordDetectorTick(&run->detector, tick_seq, camera_heading_deg, issued_delta_deg, degraded_fix);
        estimate = TakeLastEstimate(&run->detector);
    }

    log_estimate(estimate);
    if (verdict) {
        publish_verdict(run, context, *verdict);
    }
}

void EndRun(SensitivityRun* run, MaaContext* context)
{
    std::optional<Verdict> verdict;
    std::optional<Estimate> estimate;
    {
        const std::lock_guard<std::mutex> guard(run->mutex);
        verdict = EndDetectorRun(&run->detector);
        estimate = TakeLastEstimate(&run->detector);
    }

    log_estimate(estimate);
    if (verdict) {
        publish_verdict(run, context, *verdict);
    }
}

} // namespace sensitivity

} // namespace mapnavigator
