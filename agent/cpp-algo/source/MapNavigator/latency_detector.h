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
    // 健康节拍约 110ms，到 350ms 就走不动了；判定要求过半超线，实际触发点比这个数高约 100ms。
    int64_t slow_tick_ms = 280;
    // 预热和窗口按真实时间切，不按拍数：慢机每秒攒不到几拍，按拍数等于越慢越难判出来。
    int64_t warmup_ms = 2000;
    int64_t window_ms = 5000;
    // 一窗至少要这么多拍才作数，不够就接着攒不封窗，免得稀疏的窗被几拍带偏。
    int64_t window_min_ticks = 8;
    // 连着这么多窗判慢才报，也就是要一直慢满 15 秒；中间有一窗正常就清零重来。
    int required_windows = 3;
    // 单侧 99% 置信；配合下界过半的判据，等价于「有把握认定过半的拍都超时」。
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

// 只对整拍耗时下结论，各阶段耗时仅用于说明时间花在哪，不参与判定。
class Detector
{
public:
    explicit Detector(Config config = {});

    // 每次导航开始调一次：重开预热和当前窗口，已经判过的整窗结论保留。
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

// 二项比例的 Wilson 置信下界，样本少时会自动给出保守的结果。
double WilsonLowerBound(int64_t hits, int64_t total, double z);

} // namespace latency

} // namespace mapnavigator
