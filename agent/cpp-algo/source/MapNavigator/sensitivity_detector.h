#pragma once

#include <cstddef>
#include <cstdint>
#include <optional>
#include <vector>

namespace mapnavigator
{

namespace sensitivity
{

struct Config
{
    // 倍率的两条判决线，正常机的估计值落在 1.00 附近 ±0.1。
    double overshoot_ratio = 1.20;
    double undershoot_ratio = 1.0 / 1.20;
    // 中位数的置信区间：秩取 n/2 ± sigma_margin·√n/2，整个区间落在判决线外才判。
    double sigma_margin = 3.0;
    // 中位数超出这段范围就当观测本身出了问题，不改。
    double max_ratio = 2.5;
    double min_ratio = 0.4;
    // 孤立转向的指令量。太小时读数噪声占比大；上限保证倍率到 max_ratio 也转不过半圈，朝向差不会绕成反方向。
    double min_command_deg = 3.0;
    double max_command_deg = 60.0;
    // 发令后连着这么多拍什么都不发，末拍的镜头朝向差才算这条指令转出来的量。镜头第 3 拍转到位，再短读数偏低。
    int quiet_ticks = 3;
    // 攒够这么多条样本后每来一条判一次。样本这么少时区间两端就是最小、最大值，要全部样本都过线才判。
    int min_samples = 12;
    // 出口记的账和这拍报的指令差出这么多度，就是别的路径发过转向，这拍的读数作废。
    double issued_match_tol_deg = 0.5;
};

struct Verdict
{
    double ratio = 1.0;
    int ratio_percent = 0;
    int sample_count = 0;
};

// 当前的中位数和它的置信区间，留给日志。
struct Estimate
{
    double ratio = 0.0;
    double low = 0.0;
    double high = 0.0;
    int sample_count = 0;
};

// 只拿孤立转向估倍率：发一次转向，之后几拍什么都不发，看镜头最后停在指令的几倍。
// 连着转或卡住时镜头会被游戏带着回摆，那些拍的朝向变化不全是指令转出来的，不收。
// 整个进程一直攒着：灵敏度是个设置，不会这条线路对下条线路错。
class Detector
{
public:
    explicit Detector(Config config = {});

    // 每次导航开始调一次：没走完的那条样本作废，攒下的样本全部保留。
    void BeginRun();
    // 每一份从输入出口发出去的偏航度数，不论哪条路径发的。
    void NoteIssued(double delta_deg);
    // camera_heading_deg 是这拍决策前读到的镜头朝向，没读到传空。
    std::optional<Verdict>
        RecordTick(uint64_t tick_seq, std::optional<double> camera_heading_deg, double issued_delta_deg, bool degraded_fix);
    // 线路结束时调一次。
    std::optional<Verdict> EndRun();

    // 上一次算出的估计值，取走就清空。
    std::optional<Estimate> TakeLastEstimate();

private:
    struct PendingTurn
    {
        double start_heading_deg = 0.0;
        double command_deg = 0.0;
        int quiet_ticks = 0;
    };

    std::optional<Verdict> Evaluate(bool log_estimate);

    Config config_;

    uint64_t prev_tick_seq_ = 0;
    bool has_prev_tick_ = false;
    double issued_since_record_ = 0.0;

    // 发出去还在等结果的那条孤立转向。
    std::optional<PendingTurn> pending_;
    // 每条是一次孤立转向的实测转角占指令的比例。
    std::vector<double> samples_;
    // 估计值日志在样本数每翻一倍时打一次，判决和线路结束时另打。
    size_t next_log_at_ = 0;

    std::optional<Estimate> last_estimate_;
    bool fired_ = false;
};

// 归一到 (-180, 180]，两个朝向读数相减时用。
double NormalizeDeg(double deg);

} // namespace sensitivity

} // namespace mapnavigator
