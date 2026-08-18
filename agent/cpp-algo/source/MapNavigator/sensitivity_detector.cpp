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

// 主元小于这个数就当矩阵奇异：正规方程的量纲是度的平方，正常窗的主元在 1e3 量级。
constexpr double kPivotEpsilon = 1e-9;
// 岭正则的强度，按矩阵迹取相对值。
constexpr double kRidgeScale = 1e-3;
// 一窗至少要这么多拍才解方程，未知数只有 kLagCount 个，样本得比它富余一倍。
constexpr int kMinWindowSamples = kLagCount * 2;
// 过线的窗至少要这么多个，只有一个离群窗不算证据。
constexpr std::size_t kMinOvershootWindows = 2;

double Median(std::vector<double> values)
{
    if (values.empty()) {
        return 0.0;
    }
    const std::size_t mid = values.size() / 2;
    std::nth_element(values.begin(), values.begin() + mid, values.end());
    const double upper = values[mid];
    if (values.size() % 2 != 0) {
        return upper;
    }
    return 0.5 * (upper + *std::max_element(values.begin(), values.begin() + mid));
}

// 高斯-约当消元解正规方程。解不出来说明这窗的指令谱太窄，各阶滞后分不开，放弃这窗。
bool SolveNormalEquations(LagMatrix& matrix, LagVector& rhs, LagVector& out)
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
            for (int k = col; k < kLagCount; ++k) {
                std::swap(matrix[col][k], matrix[pivot][k]);
            }
            std::swap(rhs[col], rhs[pivot]);
        }
        for (int row = 0; row < kLagCount; ++row) {
            if (row == col) {
                continue;
            }
            const double factor = matrix[row][col] / matrix[col][col];
            for (int k = col; k < kLagCount; ++k) {
                matrix[row][k] -= factor * matrix[col][k];
            }
            rhs[row] -= factor * rhs[col];
        }
    }
    for (int i = 0; i < kLagCount; ++i) {
        out[i] = rhs[i] / matrix[i][i];
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
{
    ratio_history_.reserve(config_.history_size);
}

void Detector::BeginRun()
{
    has_prev_tick_ = false;
    has_prev_heading_ = false;
    chain_len_ = 0;
}

void Detector::ResetWindow()
{
    xty_.fill(0.0);
    for (LagVector& row : xtx_) {
        row.fill(0.0);
    }
    window_cmd_deg_ = 0.0;
    window_samples_ = 0;
    window_started_at_ = {};
}

std::optional<WindowSample> Detector::TakeLastWindow()
{
    std::optional<WindowSample> out;
    out.swap(last_window_);
    return out;
}

void Detector::CloseWindow()
{
    if (window_samples_ > kMinWindowSamples && window_cmd_deg_ >= config_.window_min_cmd_deg) {
        double trace = 0.0;
        for (int i = 0; i < kLagCount; ++i) {
            trace += xtx_[i][i];
        }
        // 岭正则按矩阵自身尺度取：用绝对值会把大信号压得比小信号还狠。
        const double ridge = kRidgeScale * trace / static_cast<double>(kLagCount);
        LagMatrix matrix = xtx_;
        LagVector rhs = xty_;
        for (int i = 0; i < kLagCount; ++i) {
            matrix[i][i] += ridge;
        }

        LagVector gains {};
        if (SolveNormalEquations(matrix, rhs, gains)) {
            // 各阶滞后的系数加起来才是完整倍率。
            double ratio = 0.0;
            for (const double gain : gains) {
                ratio += gain;
            }
            if (ratio_history_.size() >= config_.history_size) {
                ratio_history_.erase(ratio_history_.begin());
            }
            ratio_history_.push_back(ratio);
            last_window_ = WindowSample { ratio, window_cmd_deg_, window_samples_ };
        }
    }
    ResetWindow();
}

std::optional<Verdict> Detector::RecordTick(uint64_t tick_seq, double heading_deg, double issued_delta_deg, bool degraded_fix)
{
    const bool tick_broken = !has_prev_tick_ || tick_seq != prev_tick_seq_ + 1;
    prev_tick_seq_ = tick_seq;
    has_prev_tick_ = true;

    // 拍号断了或定位不可信就断开滞后链，接下来 kLagCount 拍不记账；窗里攒下的照样留着。
    if (tick_broken || degraded_fix) {
        chain_len_ = 0;
        if (!degraded_fix) {
            prev_heading_deg_ = NormalizeDeg(heading_deg);
            has_prev_heading_ = true;
        }
        return std::nullopt;
    }

    const double heading = NormalizeDeg(heading_deg);
    const double heading_delta = has_prev_heading_ ? NormalizeDeg(heading - prev_heading_deg_) : 0.0;
    prev_heading_deg_ = heading;
    has_prev_heading_ = true;

    const Clock::time_point now = Clock::now();
    if (window_started_at_.time_since_epoch().count() == 0) {
        window_started_at_ = now;
    }

    for (int i = kLagCount - 1; i > 0; --i) {
        cmd_lags_[i] = cmd_lags_[i - 1];
    }
    cmd_lags_[0] = issued_delta_deg;
    if (chain_len_ < kLagCount) {
        ++chain_len_;
    }

    if (chain_len_ >= kLagCount) {
        for (int p = 0; p < kLagCount; ++p) {
            xty_[p] += cmd_lags_[p] * heading_delta;
            for (int q = 0; q < kLagCount; ++q) {
                xtx_[p][q] += cmd_lags_[p] * cmd_lags_[q];
            }
        }
        window_cmd_deg_ += std::abs(issued_delta_deg);
        ++window_samples_;
    }

    if (std::chrono::duration_cast<std::chrono::milliseconds>(now - window_started_at_).count() < config_.window_ms) {
        return std::nullopt;
    }
    CloseWindow();
    return Evaluate(config_.required_windows);
}

std::optional<Verdict> Detector::EndRun(bool route_failed)
{
    // 没到时间的窗也封掉：线路很可能正好死在这个窗里。转量不够的窗照样会被能量门丢掉。
    CloseWindow();
    chain_len_ = 0;
    if (!route_failed) {
        return std::nullopt;
    }
    return Evaluate(config_.failure_windows);
}

std::optional<Verdict> Detector::Evaluate(int required_windows)
{
    if (fired_ || static_cast<int>(ratio_history_.size()) < required_windows) {
        return std::nullopt;
    }

    const std::size_t over = static_cast<std::size_t>(
        std::count_if(ratio_history_.begin(), ratio_history_.end(), [this](double ratio) { return ratio > config_.overshoot_ratio; }));
    // 过线的窗要占多数且够数：只有两个窗时中位数退化成取平均，一个离群窗能把正常窗抬过线。
    if (over * 2 <= ratio_history_.size() || over < kMinOvershootWindows) {
        return std::nullopt;
    }

    Verdict verdict;
    // 过线的占多数就蕴含中位数也过线，展示值用中位数不会和判定矛盾。
    verdict.ratio_percent = static_cast<int>(std::lround(Median(ratio_history_) * 100.0));
    verdict.window_count = static_cast<int>(ratio_history_.size());
    fired_ = true;
    return verdict;
}

} // namespace sensitivity

} // namespace mapnavigator
