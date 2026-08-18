#include "latency_detector.h"

#include <algorithm>
#include <cmath>

namespace mapnavigator
{

namespace latency
{

namespace
{

// 慢拍要占过半才算这一窗慢；置信下界压过这条线，才谈得上「大多数拍都超时」。
constexpr double kSlowTickMajority = 0.5;

int64_t Median(const std::vector<int64_t>& samples)
{
    if (samples.empty()) {
        return 0;
    }
    std::vector<int64_t> sorted = samples;
    const std::size_t mid = sorted.size() / 2;
    std::nth_element(sorted.begin(), sorted.begin() + static_cast<std::ptrdiff_t>(mid), sorted.end());
    return sorted[mid];
}

} // namespace

double WilsonLowerBound(int64_t hits, int64_t total, double z)
{
    if (total <= 0) {
        return 0.0;
    }
    const double n = static_cast<double>(total);
    const double p = static_cast<double>(hits) / n;
    const double z2 = z * z;
    const double centre = p + z2 / (2.0 * n);
    const double margin = z * std::sqrt(p * (1.0 - p) / n + z2 / (4.0 * n * n));
    return std::max(0.0, (centre - margin) / (1.0 + z2 / n));
}

Detector::Detector(Config config)
    : config_(config)
{
}

void Detector::BeginRun()
{
    warmup_until_ = Clock::now() + std::chrono::milliseconds(config_.warmup_ms);
    has_prev_tick_ = false;
    // 攒到一半的窗不跨线路续，中间隔着换图、结算这些空档；已经判过的整窗结论要留着。
    has_window_ = false;
    window_samples_ = 0;
    window_slow_ = 0;
}

void Detector::Push(std::vector<int64_t>& history, int64_t value) const
{
    history.push_back(value);
    if (history.size() > config_.history_size) {
        history.erase(history.begin());
    }
}

void Detector::RecordStage(Stage stage, int64_t elapsed_ms)
{
    if (fired_ || elapsed_ms < 0) {
        return;
    }
    Push(stage_history_[static_cast<std::size_t>(stage)], elapsed_ms);
}

std::optional<Verdict> Detector::RecordTick(uint64_t tick_seq, int64_t tick_gap_ms)
{
    // 只收紧挨着上一拍的样本：中间跳过的拍是跳跃、攀爬、换层、丢定位，间隔里混着它们的耗时。
    const bool consecutive = has_prev_tick_ && tick_seq == prev_tick_seq_ + 1;
    prev_tick_seq_ = tick_seq;
    has_prev_tick_ = true;

    if (fired_ || !consecutive || tick_gap_ms <= 0) {
        return std::nullopt;
    }
    const auto now = Clock::now();
    if (now < warmup_until_) {
        return std::nullopt;
    }
    if (!has_window_) {
        window_started_at_ = now;
        has_window_ = true;
    }

    Push(tick_history_, tick_gap_ms);
    ++total_samples_;
    ++window_samples_;
    if (tick_gap_ms > config_.slow_tick_ms) {
        ++window_slow_;
    }

    const int64_t window_elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - window_started_at_).count();
    if (window_elapsed_ms < config_.window_ms || window_samples_ < config_.window_min_ticks) {
        return std::nullopt;
    }

    const bool window_slow = WilsonLowerBound(window_slow_, window_samples_, config_.confidence_z) > kSlowTickMajority;
    if (window_slow) {
        ++slow_windows_;
    }
    else {
        // 断了就从头数，采样也一起丢：提示里的中位数要正好是判定所依据的那段。
        slow_windows_ = 0;
        ClearHistory();
    }
    window_samples_ = 0;
    window_slow_ = 0;
    window_started_at_ = now;
    if (slow_windows_ < config_.required_windows) {
        return std::nullopt;
    }

    fired_ = true;
    return BuildVerdict();
}

void Detector::ClearHistory()
{
    tick_history_.clear();
    for (auto& history : stage_history_) {
        history.clear();
    }
    total_samples_ = 0;
}

Verdict Detector::BuildVerdict() const
{
    Verdict verdict;
    verdict.tick_median_ms = Median(tick_history_);
    for (std::size_t i = 0; i < kStageCount; ++i) {
        verdict.stage_median_ms[i] = Median(stage_history_[i]);
    }
    verdict.sample_count = total_samples_;
    if (!tick_history_.empty()) {
        const auto slow =
            std::count_if(tick_history_.begin(), tick_history_.end(), [this](int64_t ms) { return ms > config_.slow_tick_ms; });
        verdict.slow_percent = static_cast<int>(slow * 100 / static_cast<int64_t>(tick_history_.size()));
    }
    return verdict;
}

} // namespace latency

} // namespace mapnavigator
