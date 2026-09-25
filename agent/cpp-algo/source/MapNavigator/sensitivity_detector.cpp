#include "sensitivity_detector.h"

#include <algorithm>
#include <cmath>
#include <utility>

namespace mapnavigator
{

namespace sensitivity
{

namespace
{

// 主元小于这个数就当矩阵奇异：正规方程的量纲是度的平方，正常样本的主元在 1e3 量级以上。
constexpr double kPivotEpsilon = 1e-9;
// 岭正则的强度，按矩阵迹取相对值。
constexpr double kRidgeScale = 1e-3;
// 一起解的右端项：系数向量，以及给标准误差用的全 1 向量。
constexpr int kRhsCount = 2;

using RhsSet = std::array<LagVector, kRhsCount>;

// 高斯-约当消元，两组右端项一起解。解不出来说明指令谱太窄，各阶滞后分不开。
bool SolveNormalEquations(LagMatrix matrix, RhsSet rhs, RhsSet& out)
{
    for (int col = 0; col < kLagCount; ++col) {
        int pivot = col;
        for (int row = col + 1; row < kLagCount; ++row) {
            if (std::abs(matrix[row][col]) > std::abs(matrix[pivot][col])) {
                pivot = row;
            }
        }
        if (std::abs(matrix[pivot][col]) < kPivotEpsilon) {
            return false;
        }
        if (pivot != col) {
            std::swap(matrix[col], matrix[pivot]);
            for (int r = 0; r < kRhsCount; ++r) {
                std::swap(rhs[r][col], rhs[r][pivot]);
            }
        }
        for (int row = 0; row < kLagCount; ++row) {
            if (row == col) {
                continue;
            }
            const double factor = matrix[row][col] / matrix[col][col];
            for (int k = col; k < kLagCount; ++k) {
                matrix[row][k] -= factor * matrix[col][k];
            }
            for (int r = 0; r < kRhsCount; ++r) {
                rhs[r][row] -= factor * rhs[r][col];
            }
        }
    }
    for (int r = 0; r < kRhsCount; ++r) {
        for (int i = 0; i < kLagCount; ++i) {
            out[r][i] = rhs[r][i] / matrix[i][i];
        }
    }
    return true;
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

Detector::Detector(Config config)
    : config_(config)
    , next_eval_at_(config.min_samples)
{
}

void Detector::BeginRun()
{
    has_prev_tick_ = false;
    has_prev_heading_ = false;
    chain_len_ = 0;
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

void Detector::PushLag(double cmd_deg)
{
    for (int i = kLagCount - 1; i > 0; --i) {
        cmd_lags_[i] = cmd_lags_[i - 1];
    }
    cmd_lags_[0] = cmd_deg;
    if (chain_len_ < kLagCount) {
        ++chain_len_;
    }
}

void NormalSums::Add(const LagVector& row, double heading_delta, double issued_delta_deg)
{
    for (int p = 0; p < kLagCount; ++p) {
        xty[p] += row[p] * heading_delta;
        for (int q = 0; q < kLagCount; ++q) {
            xtx[p][q] += row[p] * row[q];
        }
    }
    yty += heading_delta * heading_delta;
    cmd_deg += std::abs(issued_delta_deg);
    ++sample_count;
}

void NormalSums::Reset()
{
    *this = NormalSums {};
}

void Detector::ResetAccumulators()
{
    total_.Reset();
    run_.Reset();
    normal_run_seen_ = false;
    next_eval_at_ = config_.min_samples;
}

std::optional<Verdict>
    Detector::RecordTick(uint64_t tick_seq, int64_t now_ms, double heading_deg, double issued_delta_deg, bool degraded_fix)
{
    // 出口记的账比这拍报的多，就是别的路径发过转向，之前的滞后链对不上号了。
    const double issued_total = issued_since_record_;
    issued_since_record_ = 0.0;
    const bool foreign_issued = std::abs(issued_total - issued_delta_deg) > config_.issued_match_tol_deg;
    int64_t gap_ticks = -1;
    if (has_prev_tick_ && tick_seq > prev_tick_seq_) {
        gap_ticks = static_cast<int64_t>(tick_seq - prev_tick_seq_) - 1;
    }
    const int64_t gap_ms = now_ms - prev_tick_ms_;
    prev_tick_seq_ = tick_seq;
    prev_tick_ms_ = now_ms;
    has_prev_tick_ = true;

    if (degraded_fix) {
        chain_len_ = 0;
        has_prev_heading_ = false;
        return std::nullopt;
    }

    const double heading = NormalizeDeg(heading_deg);
    const bool linked = has_prev_heading_ && !foreign_issued;
    if (linked && gap_ticks == 0) {
        if (chain_len_ >= kLagCount) {
            const double heading_delta = NormalizeDeg(heading - prev_heading_deg_);
            total_.Add(cmd_lags_, heading_delta, issued_delta_deg);
            run_.Add(cmd_lags_, heading_delta, issued_delta_deg);
        }
    }
    else if (linked && gap_ticks == 1 && gap_ms <= config_.bridge_max_gap_ms) {
        // 跳过的那拍什么都没发，它和这拍的两行方程相加仍然成立：朝向差取两拍合计，指令按各自的滞后对齐。
        if (chain_len_ >= kLagCount) {
            LagVector row = cmd_lags_;
            for (int i = 1; i < kLagCount; ++i) {
                row[i] += cmd_lags_[i - 1];
            }
            const double heading_delta = NormalizeDeg(heading - prev_heading_deg_);
            total_.Add(row, heading_delta, issued_delta_deg);
            run_.Add(row, heading_delta, issued_delta_deg);
        }
        PushLag(0.0);
    }
    else {
        chain_len_ = 0;
    }
    PushLag(foreign_issued ? issued_total : issued_delta_deg);
    prev_heading_deg_ = heading;
    has_prev_heading_ = true;

    if (total_.sample_count < next_eval_at_) {
        return std::nullopt;
    }
    next_eval_at_ *= 2;
    return Evaluate();
}

std::optional<Verdict> Detector::EndRun()
{
    chain_len_ = 0;
    has_prev_heading_ = false;
    std::optional<Verdict> verdict = Evaluate();
    if (RunReadsNormal()) {
        normal_run_seen_ = true;
    }
    run_.Reset();
    return verdict;
}

std::optional<Estimate> Detector::Solve(const NormalSums& sums) const
{
    if (sums.sample_count <= kLagCount) {
        return std::nullopt;
    }
    double trace = 0.0;
    for (int i = 0; i < kLagCount; ++i) {
        trace += sums.xtx[i][i];
    }
    // 岭正则按矩阵自身尺度取：用绝对值会把大信号压得比小信号还狠。
    const double ridge = kRidgeScale * trace / static_cast<double>(kLagCount);
    LagMatrix matrix = sums.xtx;
    for (int i = 0; i < kLagCount; ++i) {
        matrix[i][i] += ridge;
    }
    RhsSet rhs {};
    rhs[0] = sums.xty;
    rhs[1].fill(1.0);
    RhsSet solved {};
    if (!SolveNormalEquations(matrix, rhs, solved)) {
        return std::nullopt;
    }

    // 各阶滞后的系数加起来才是完整倍率；残差平方和从累加量直接展开，不用回放样本。
    const LagVector& gains = solved[0];
    double ratio = 0.0;
    double ones_weight = 0.0;
    double fit_energy = 0.0;
    for (int p = 0; p < kLagCount; ++p) {
        ratio += gains[p];
        ones_weight += solved[1][p];
        double xtx_row = 0.0;
        for (int q = 0; q < kLagCount; ++q) {
            xtx_row += sums.xtx[p][q] * gains[q];
        }
        fit_energy += gains[p] * (xtx_row - 2.0 * sums.xty[p]);
    }
    const double residual_ss = std::max(0.0, sums.yty + fit_energy);
    const double variance = residual_ss / static_cast<double>(sums.sample_count - kLagCount);

    Estimate estimate;
    estimate.ratio = ratio;
    estimate.se = std::sqrt(std::max(0.0, variance * ones_weight));
    estimate.sample_count = sums.sample_count;
    estimate.cmd_deg = sums.cmd_deg;
    return estimate;
}

bool Detector::RunReadsNormal() const
{
    if (run_.sample_count < config_.min_run_samples) {
        return false;
    }
    const std::optional<Estimate> estimate = Solve(run_);
    return estimate && estimate->ratio > config_.undershoot_ratio;
}

std::optional<Verdict> Detector::Evaluate()
{
    if (total_.sample_count < config_.min_samples) {
        return std::nullopt;
    }
    const std::optional<Estimate> estimate = Solve(total_);
    if (!estimate) {
        return std::nullopt;
    }
    last_estimate_ = estimate;
    if (fired_) {
        return std::nullopt;
    }
    // 置信区间整个落在判决线外才算证据；估计值离谱说明观测本身出了问题，同样不改。
    const double margin = config_.sigma_margin * estimate->se;
    const bool too_fast = estimate->ratio - margin > config_.overshoot_ratio && estimate->ratio <= config_.max_ratio;
    const bool too_slow = estimate->ratio + margin < config_.undershoot_ratio && estimate->ratio >= config_.min_ratio && !normal_run_seen_
                          && !RunReadsNormal();
    if (!too_fast && !too_slow) {
        return std::nullopt;
    }

    Verdict verdict;
    verdict.ratio = estimate->ratio;
    verdict.ratio_percent = static_cast<int>(std::lround(estimate->ratio * 100.0));
    verdict.sample_count = total_.sample_count;
    fired_ = true;
    // 系数落地后从头再估一遍，日志里能看到校正后的倍率是不是回到 1。
    ResetAccumulators();
    return verdict;
}

} // namespace sensitivity

} // namespace mapnavigator
