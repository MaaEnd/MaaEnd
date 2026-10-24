#pragma once

#include <array>
#include <chrono>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <vector>

namespace mapnavigator
{

namespace latency
{

enum class Stage
{
    Screencap,
    ImagePrep,
    Locate,
    Steer,
    Other,
};

inline constexpr std::size_t kStageCount = 5;

struct Config
{
    int64_t slow_tick_ms = 280;
    int64_t warmup_ms = 2000;
    int64_t window_ms = 5000;
    // 拍数不够就不封窗, 接着攒
    int64_t window_min_ticks = 8;
    // 须连续这么多窗判慢, 中间一窗正常即清零
    int required_windows = 3;
    // 单侧 99% 置信的 z 值
    double confidence_z = 2.326;
    std::size_t history_size = 512;
};

struct Verdict
{
    int64_t tick_median_ms = 0;
    std::array<int64_t, kStageCount> stage_median_ms {};
    int slow_percent = 0;
    int64_t sample_count = 0;
};

// 只按整拍耗时判定, 各阶段耗时只用于报告
class Detector
{
public:
    explicit Detector(Config config = {});

    // 每次导航开始调用, 重开预热和当前窗口, 已判过的整窗保留
    void BeginRun();
    void RecordStage(Stage stage, int64_t elapsed_ms);
    std::optional<Verdict> RecordTick(uint64_t tick_seq, int64_t tick_gap_ms);

    bool fired() const { return fired_; }

private:
    using Clock = std::chrono::steady_clock;

    void Push(std::vector<int64_t>& history, int64_t value) const;
    void ClearHistory();
    Verdict BuildVerdict() const;

    Config config_;
    std::vector<int64_t> tick_history_;
    std::array<std::vector<int64_t>, kStageCount> stage_history_;

    uint64_t prev_tick_seq_ = 0;
    bool has_prev_tick_ = false;
    Clock::time_point warmup_until_ {};
    Clock::time_point window_started_at_ {};
    bool has_window_ = false;
    int64_t window_samples_ = 0;
    int64_t window_slow_ = 0;
    int slow_windows_ = 0;
    int64_t total_samples_ = 0;
    bool fired_ = false;
};

double WilsonLowerBound(int64_t hits, int64_t total, double z);

// 偶数个取上中位, 空集返回 0
int64_t Median(const std::vector<int64_t>& samples);

} // namespace latency

} // namespace mapnavigator
