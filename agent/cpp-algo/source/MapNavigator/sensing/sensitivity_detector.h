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
    // 实测转角与指令之比的两条判决线
    double overshoot_ratio = 1.20;
    double undershoot_ratio = 1.0 / 1.20;
    // 中位数置信区间的秩取 n/2 ± sigma_margin·√n/2
    double sigma_margin = 3.0;
    // 中位数超出此范围视为观测异常, 不做校正
    double max_ratio = 2.5;
    double min_ratio = 0.4;
    // 孤立转向的指令量范围, 上限乘 max_ratio 须小于 180 度, 否则朝向差会绕反
    double min_command_deg = 3.0;
    double max_command_deg = 60.0;
    // 发令后连续这么多拍不再发转向, 末拍的朝向差才算这条指令的转量
    int quiet_ticks = 3;
    // 攒够这么多条样本后每来一条判一次
    int min_samples = 12;
    // 出口记账与本拍上报指令相差超过此值, 说明别的路径发过转向, 本拍作废
    double issued_match_tol_deg = 0.5;
};

struct Verdict
{
    double ratio = 1.0;
    int ratio_percent = 0;
    int sample_count = 0;
};

// ratio 为中位数, low/high 为置信区间
struct Estimate
{
    double ratio = 0.0;
    double low = 0.0;
    double high = 0.0;
    int sample_count = 0;
};

// 只用孤立转向的样本估倍率, 样本跨导航在进程内累积
class Detector
{
public:
    explicit Detector(Config config = {});

    // 每次导航开始调用, 未完成的样本作废, 已攒样本保留
    void beginRun();
    // 所有路径发出的偏航度数都要报
    void noteIssued(double delta_deg);
    // camera_heading_deg 是本拍决策前读到的镜头朝向, 没读到传空
    std::optional<Verdict>
        recordTick(uint64_t tick_seq, std::optional<double> camera_heading_deg, double issued_delta_deg, bool degraded_fix);
    std::optional<Verdict> endRun();

    std::optional<Estimate> takeLastEstimate();

private:
    struct PendingTurn
    {
        double start_heading_deg = 0.0;
        double command_deg = 0.0;
        int quiet_ticks = 0;
    };

    std::optional<Verdict> evaluate(bool log_estimate);

    Config config_;

    uint64_t prev_tick_seq_ = 0;
    bool has_prev_tick_ = false;
    double issued_since_record_ = 0.0;

    std::optional<PendingTurn> pending_;
    // 每条是一次孤立转向的实测转角与指令之比
    std::vector<double> samples_;
    // 样本数每翻一倍打一次估计值日志
    size_t next_log_at_ = 0;

    std::optional<Estimate> last_estimate_;
    bool fired_ = false;
};

// 归一到 (-180, 180]
double NormalizeDeg(double deg);

} // namespace sensitivity

} // namespace mapnavigator
