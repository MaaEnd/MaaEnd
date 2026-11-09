#include "sensitivity_detector.h"

#include <algorithm>
#include <cmath>

namespace mapnavigator
{

namespace sensitivity
{

namespace
{

std::optional<Estimate> estimate_turn_ratio(const Detector& detector)
{
    const size_t count = detector.samples.size();
    if (count < static_cast<size_t>(detector.config.min_samples)) {
        return std::nullopt;
    }
    std::vector<double> sorted = detector.samples;
    std::sort(sorted.begin(), sorted.end());
    const double center = static_cast<double>(count) / 2.0;
    const double half_width = detector.config.sigma_margin * std::sqrt(static_cast<double>(count)) / 2.0;
    const size_t low_rank = static_cast<size_t>(std::max(0.0, std::floor(center - half_width)));
    const size_t high_rank = std::min(count - 1, static_cast<size_t>(std::ceil(center + half_width)) - 1);

    Estimate estimate;
    estimate.ratio = sorted[count / 2];
    estimate.low = sorted[low_rank];
    estimate.high = sorted[high_rank];
    estimate.sample_count = static_cast<int>(count);
    return estimate;
}

bool verdict_due(const Detector& detector, const Estimate& estimate)
{
    const bool too_fast = estimate.low > detector.config.overshoot_ratio && estimate.ratio <= detector.config.max_ratio;
    const bool too_slow = estimate.high < detector.config.undershoot_ratio && estimate.ratio >= detector.config.min_ratio;
    return !detector.fired && (too_fast || too_slow);
}

Verdict fire_verdict(Detector* detector, const Estimate& estimate)
{
    Verdict verdict;
    verdict.ratio = estimate.ratio;
    verdict.ratio_percent = static_cast<int>(std::lround(estimate.ratio * 100.0));
    verdict.sample_count = estimate.sample_count;
    detector->fired = true;
    // 校正后重新估计, 便于从日志看出倍率是否回到 1
    detector->samples.clear();
    detector->next_log_at = static_cast<size_t>(detector->config.min_samples);
    return verdict;
}

} // namespace

double NormalizeDeg(double deg)
{
    double out = std::fmod(deg + 180.0, 360.0);
    if (out <= 0.0) {
        out += 360.0;
    }
    return out - 180.0;
}

void BeginDetectorRun(Detector* detector)
{
    detector->has_prev_tick = false;
    detector->pending.reset();
    detector->issued_since_record = 0.0;
}

void NoteDetectorIssued(Detector* detector, double delta_deg)
{
    detector->issued_since_record += delta_deg;
}

std::optional<Estimate> TakeLastEstimate(Detector* detector)
{
    std::optional<Estimate> out;
    out.swap(detector->last_estimate);
    return out;
}

std::optional<Verdict> RecordDetectorTick(
    Detector* detector,
    uint64_t tick_seq,
    std::optional<double> camera_heading_deg,
    double issued_delta_deg,
    bool degraded_fix)
{
    const double issued_total = detector->issued_since_record;
    detector->issued_since_record = 0.0;
    const bool foreign_issued = std::abs(issued_total - issued_delta_deg) > detector->config.issued_match_tol_deg;
    const bool consecutive = detector->has_prev_tick && tick_seq == detector->prev_tick_seq + 1;
    detector->prev_tick_seq = tick_seq;
    detector->has_prev_tick = true;

    if (degraded_fix || !camera_heading_deg || foreign_issued) {
        detector->pending.reset();
        return std::nullopt;
    }
    const double heading = *camera_heading_deg;

    // 漏掉的拍里可能发过转向
    if (detector->pending && !consecutive) {
        detector->pending.reset();
    }
    bool sample_added = false;
    if (detector->pending) {
        if (issued_delta_deg != 0.0) {
            detector->pending.reset();
        }
        else if (++detector->pending->quiet_ticks >= detector->config.quiet_ticks) {
            detector->samples.push_back(NormalizeDeg(heading - detector->pending->start_heading_deg) / detector->pending->command_deg);
            detector->pending.reset();
            sample_added = true;
        }
    }
    const double magnitude = std::abs(issued_delta_deg);
    if (!detector->pending && magnitude >= detector->config.min_command_deg && magnitude <= detector->config.max_command_deg) {
        detector->pending = PendingTurn { .start_heading_deg = heading, .command_deg = issued_delta_deg };
    }

    if (!sample_added) {
        return std::nullopt;
    }
    const bool log_due = detector->samples.size() >= detector->next_log_at;
    if (log_due) {
        detector->next_log_at *= 2;
    }
    const std::optional<Estimate> estimate = estimate_turn_ratio(*detector);
    if (!estimate) {
        return std::nullopt;
    }
    const bool decided = verdict_due(*detector, *estimate);
    if (log_due || decided) {
        detector->last_estimate = *estimate;
    }
    if (!decided) {
        return std::nullopt;
    }
    return fire_verdict(detector, *estimate);
}

std::optional<Verdict> EndDetectorRun(Detector* detector)
{
    detector->pending.reset();
    const std::optional<Estimate> estimate = estimate_turn_ratio(*detector);
    if (!estimate) {
        return std::nullopt;
    }
    const bool decided = verdict_due(*detector, *estimate);
    detector->last_estimate = *estimate;
    if (!decided) {
        return std::nullopt;
    }
    return fire_verdict(detector, *estimate);
}

} // namespace sensitivity

} // namespace mapnavigator
