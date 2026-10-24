#include "sensitivity_detector.h"

#include <algorithm>
#include <cmath>

namespace mapnavigator
{

namespace sensitivity
{

double NormalizeDeg(double deg)
{
    double out = std::fmod(deg + 180.0, 360.0);
    if (out <= 0.0) {
        out += 360.0;
    }
    return out - 180.0;
}

Detector::Detector(Config config)
    : config_(config)
    , next_log_at_(static_cast<size_t>(config.min_samples))
{
}

void Detector::BeginRun()
{
    has_prev_tick_ = false;
    pending_.reset();
    issued_since_record_ = 0.0;
}

void Detector::NoteIssued(double delta_deg)
{
    issued_since_record_ += delta_deg;
}

std::optional<Estimate> Detector::TakeLastEstimate()
{
    std::optional<Estimate> out;
    out.swap(last_estimate_);
    return out;
}

std::optional<Verdict>
    Detector::RecordTick(uint64_t tick_seq, std::optional<double> camera_heading_deg, double issued_delta_deg, bool degraded_fix)
{
    const double issued_total = issued_since_record_;
    issued_since_record_ = 0.0;
    const bool foreign_issued = std::abs(issued_total - issued_delta_deg) > config_.issued_match_tol_deg;
    const bool consecutive = has_prev_tick_ && tick_seq == prev_tick_seq_ + 1;
    prev_tick_seq_ = tick_seq;
    has_prev_tick_ = true;

    if (degraded_fix || !camera_heading_deg || foreign_issued) {
        pending_.reset();
        return std::nullopt;
    }
    const double heading = *camera_heading_deg;

    // 漏掉的拍里可能发过转向
    if (pending_ && !consecutive) {
        pending_.reset();
    }
    bool sample_added = false;
    if (pending_) {
        if (issued_delta_deg != 0.0) {
            pending_.reset();
        }
        else if (++pending_->quiet_ticks >= config_.quiet_ticks) {
            samples_.push_back(NormalizeDeg(heading - pending_->start_heading_deg) / pending_->command_deg);
            pending_.reset();
            sample_added = true;
        }
    }
    const double magnitude = std::abs(issued_delta_deg);
    if (!pending_ && magnitude >= config_.min_command_deg && magnitude <= config_.max_command_deg) {
        pending_ = PendingTurn { .start_heading_deg = heading, .command_deg = issued_delta_deg };
    }

    if (!sample_added) {
        return std::nullopt;
    }
    const bool log_due = samples_.size() >= next_log_at_;
    if (log_due) {
        next_log_at_ *= 2;
    }
    return Evaluate(log_due);
}

std::optional<Verdict> Detector::EndRun()
{
    pending_.reset();
    return Evaluate(true);
}

std::optional<Verdict> Detector::Evaluate(bool log_estimate)
{
    const size_t count = samples_.size();
    if (count < static_cast<size_t>(config_.min_samples)) {
        return std::nullopt;
    }
    std::vector<double> sorted = samples_;
    std::sort(sorted.begin(), sorted.end());
    const double center = static_cast<double>(count) / 2.0;
    const double half_width = config_.sigma_margin * std::sqrt(static_cast<double>(count)) / 2.0;
    const size_t low_rank = static_cast<size_t>(std::max(0.0, std::floor(center - half_width)));
    const size_t high_rank = std::min(count - 1, static_cast<size_t>(std::ceil(center + half_width)) - 1);

    Estimate estimate;
    estimate.ratio = sorted[count / 2];
    estimate.low = sorted[low_rank];
    estimate.high = sorted[high_rank];
    estimate.sample_count = static_cast<int>(count);
    const bool too_fast = estimate.low > config_.overshoot_ratio && estimate.ratio <= config_.max_ratio;
    const bool too_slow = estimate.high < config_.undershoot_ratio && estimate.ratio >= config_.min_ratio;
    const bool decided = !fired_ && (too_fast || too_slow);
    if (log_estimate || decided) {
        last_estimate_ = estimate;
    }
    if (!decided) {
        return std::nullopt;
    }

    Verdict verdict;
    verdict.ratio = estimate.ratio;
    verdict.ratio_percent = static_cast<int>(std::lround(estimate.ratio * 100.0));
    verdict.sample_count = estimate.sample_count;
    fired_ = true;
    // 校正后重新估计, 便于从日志看出倍率是否回到 1
    samples_.clear();
    next_log_at_ = static_cast<size_t>(config_.min_samples);
    return verdict;
}

} // namespace sensitivity

} // namespace mapnavigator
