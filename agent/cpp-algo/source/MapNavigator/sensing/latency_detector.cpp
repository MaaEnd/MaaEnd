#include "latency_detector.h"

#include <algorithm>
#include <cmath>

namespace mapnavigator
{

namespace latency
{

namespace
{

// 慢拍占比的置信下界超过此值才算这一窗慢
constexpr double kSlowTickMajority = 0.5;

void push(const Config& config, std::vector<int64_t>* history, int64_t value)
{
    history->push_back(value);
    if (history->size() > config.history_size) {
        history->erase(history->begin());
    }
}

void clear_history(Detector* detector)
{
    detector->tick_history.clear();
    for (auto& history : detector->stage_history) {
        history.clear();
    }
    detector->total_samples = 0;
}

Verdict build_verdict(const Detector& detector)
{
    Verdict verdict;
    verdict.tick_median_ms = Median(detector.tick_history);
    for (std::size_t i = 0; i < kStageCount; ++i) {
        verdict.stage_median_ms[i] = Median(detector.stage_history[i]);
    }
    verdict.sample_count = detector.total_samples;
    if (!detector.tick_history.empty()) {
        const int64_t slow_tick_ms = detector.config.slow_tick_ms;
        const auto slow = std::count_if(detector.tick_history.begin(), detector.tick_history.end(), [slow_tick_ms](int64_t ms) {
            return ms > slow_tick_ms;
        });
        verdict.slow_percent = static_cast<int>(slow * 100 / static_cast<int64_t>(detector.tick_history.size()));
    }
    return verdict;
}

} // namespace

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

void BeginDetectorRun(Detector* detector)
{
    detector->warmup_until = std::chrono::steady_clock::now() + std::chrono::milliseconds(detector->config.warmup_ms);
    detector->has_prev_tick = false;
    detector->has_window = false;
    detector->window_samples = 0;
    detector->window_slow = 0;
}

void RecordDetectorStage(Detector* detector, Stage stage, int64_t elapsed_ms)
{
    if (detector->fired || elapsed_ms < 0) {
        return;
    }
    push(detector->config, &detector->stage_history[static_cast<std::size_t>(stage)], elapsed_ms);
}

std::optional<Verdict> RecordDetectorTick(Detector* detector, uint64_t tick_seq, int64_t tick_gap_ms)
{
    // 只收紧挨上一拍的样本, 跳过的拍间隔里混着跳跃、攀爬等耗时
    const bool consecutive = detector->has_prev_tick && tick_seq == detector->prev_tick_seq + 1;
    detector->prev_tick_seq = tick_seq;
    detector->has_prev_tick = true;

    if (detector->fired || !consecutive || tick_gap_ms <= 0) {
        return std::nullopt;
    }
    const auto now = std::chrono::steady_clock::now();
    if (now < detector->warmup_until) {
        return std::nullopt;
    }
    if (!detector->has_window) {
        detector->window_started_at = now;
        detector->has_window = true;
    }

    push(detector->config, &detector->tick_history, tick_gap_ms);
    ++detector->total_samples;
    ++detector->window_samples;
    if (tick_gap_ms > detector->config.slow_tick_ms) {
        ++detector->window_slow;
    }

    const int64_t window_elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - detector->window_started_at).count();
    if (window_elapsed_ms < detector->config.window_ms || detector->window_samples < detector->config.window_min_ticks) {
        return std::nullopt;
    }

    const bool window_slow =
        WilsonLowerBound(detector->window_slow, detector->window_samples, detector->config.confidence_z) > kSlowTickMajority;
    if (window_slow) {
        ++detector->slow_windows;
    }
    else {
        // 采样一起清掉, 让报告的中位数只来自判定所依据的那几窗
        detector->slow_windows = 0;
        clear_history(detector);
    }
    detector->window_samples = 0;
    detector->window_slow = 0;
    detector->window_started_at = now;
    if (detector->slow_windows < detector->config.required_windows) {
        return std::nullopt;
    }

    detector->fired = true;
    return build_verdict(*detector);
}

} // namespace latency

} // namespace mapnavigator
