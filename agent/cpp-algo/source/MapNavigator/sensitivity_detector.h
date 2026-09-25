#pragma once

#include <array>
#include <cstdint>
#include <optional>

namespace mapnavigator
{

namespace sensitivity
{

// 一条指令的转量摊在后面十来拍上，逐拍相除量到的是滞后。拿这么多拍一起拟合才是灵敏度。
inline constexpr int kLagCount = 12;

using LagVector = std::array<double, kLagCount>;
using LagMatrix = std::array<LagVector, kLagCount>;

struct Config
{
    // 倍率的两条判决线，正常机的估计值落在 1.00 附近 ±0.05。
    double overshoot_ratio = 1.20;
    double undershoot_ratio = 1.0 / 1.20;
    // 估计值往 1.0 的方向退这么多倍标准误差还过线才判。
    double sigma_margin = 3.0;
    // 估计值超出这段范围就当观测本身出了问题，不改。
    double max_ratio = 2.5;
    double min_ratio = 0.4;
    // 攒够这么多拍样本才开始判，之后样本每翻一倍再判一次，线路结束也判一次。
    int min_samples = 500;
    // 单条线路攒到这么多样本，它自己的估计值才有资格否决偏慢的判决。
    int min_run_samples = 50;
    // 拍号跳一格且那拍没发过转向，两拍相隔在此之内才把两拍并成一行记账。
    int64_t bridge_max_gap_ms = 600;
    // 出口记的账和这拍报的指令差出这么多度，就是别的路径发过转向，滞后链作废。
    double issued_match_tol_deg = 0.5;
};

struct Verdict
{
    double ratio = 1.0;
    int ratio_percent = 0;
    int sample_count = 0;
};

// 当前的估计值和它的标准误差，留给日志。
struct Estimate
{
    double ratio = 0.0;
    double se = 0.0;
    int sample_count = 0;
    double cmd_deg = 0.0;
};

// 正规方程的累加量。断拍只清滞后链，不动这些：只有「哪拍对哪拍」要连续，统计量不要。
struct NormalSums
{
    LagMatrix xtx {};
    LagVector xty {};
    double yty = 0.0;
    int sample_count = 0;
    double cmd_deg = 0.0;

    void Add(const LagVector& row, double heading_delta, double issued_delta_deg);
    void Reset();
};

// 比对发出的转向指令和实测的朝向变化，算出实际转到了指令的百分之多少。
// 整个进程一直攒着：灵敏度是个设置，不会这条线路对下条线路错。
class Detector
{
public:
    explicit Detector(Config config = {});

    // 每次导航开始调一次：断开滞后链，攒下的方程全部保留。
    void BeginRun();
    // 每一份从输入出口发出去的偏航度数，不论哪条路径发的。
    void NoteIssued(double delta_deg);
    std::optional<Verdict> RecordTick(uint64_t tick_seq, int64_t now_ms, double heading_deg, double issued_delta_deg, bool degraded_fix);
    // 线路结束时调一次。
    std::optional<Verdict> EndRun();

    bool fired() const { return fired_; }

    // 上一次算出的估计值，取走就清空。
    std::optional<Estimate> TakeLastEstimate();

private:
    void PushLag(double cmd_deg);
    void ResetAccumulators();
    std::optional<Estimate> Solve(const NormalSums& sums) const;
    bool RunReadsNormal() const;
    std::optional<Verdict> Evaluate();

    Config config_;

    uint64_t prev_tick_seq_ = 0;
    bool has_prev_tick_ = false;
    int64_t prev_tick_ms_ = 0;
    double prev_heading_deg_ = 0.0;
    bool has_prev_heading_ = false;
    double issued_since_record_ = 0.0;

    // 前 kLagCount 拍的指令，[0] 是上一拍。攒不满说明账刚断过，这拍不进方程。
    LagVector cmd_lags_ {};
    int chain_len_ = 0;

    // 全程的累加量，和当前这条线路单独的一份。
    // 卡墙时指令很大、朝向不动，一段就能把全程估计压到一半，所以偏慢要求没有任何一条线路单独读出正常值。
    NormalSums total_;
    NormalSums run_;
    bool normal_run_seen_ = false;
    int next_eval_at_ = 0;

    std::optional<Estimate> last_estimate_;
    bool fired_ = false;
};

// 归一到 (-180, 180]，两个朝向读数相减时用。
double NormalizeDeg(double deg);

} // namespace sensitivity

} // namespace mapnavigator
