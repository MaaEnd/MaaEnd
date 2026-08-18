#pragma once

#include <array>
#include <chrono>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <vector>

namespace mapnavigator
{

namespace sensitivity
{

// 一条指令的转量摊在后面几拍上，逐拍相除量到的是滞后。拿这么多拍一起拟合才是灵敏度。
inline constexpr int kLagCount = 6;

using LagVector = std::array<double, kLagCount>;
using LagMatrix = std::array<LagVector, kLagCount>;

struct Config
{
    // 按真实时间切窗，不按拍数：慢机每秒攒不到几拍，按拍数等于越慢越难判出来。
    int64_t window_ms = 12000;
    // 一窗至少发出这么多度才作数，转得太少的窗拟合出来是噪声。
    double window_min_cmd_deg = 150.0;
    // 只报转过头。转不到位有太多正常原因（指令被吞、贴墙转不动、掉帧），低倍率分不出是哪种。
    double overshoot_ratio = 1.20;
    // 攒够这么多窗才判，两倍灵敏度实机 37 秒就够。
    int required_windows = 3;
    // 线路走坏时改用这个窗数，跑得再短也给个结论。
    int failure_windows = 2;
    std::size_t history_size = 256;
};

struct Verdict
{
    int ratio_percent = 0;
    int window_count = 0;
};

// 一个窗的原始账目，留给日志。
struct WindowSample
{
    double ratio = 0.0;
    double cmd_deg = 0.0;
    int tick_count = 0;
};

// 比对发出的转向指令和实测的朝向变化，算出实际转到了指令的百分之多少。
// 跨线路一直攒着：灵敏度是个设置，不会这条线路对下条线路错。
class Detector
{
public:
    explicit Detector(Config config = {});

    // 每次导航开始调一次：断开滞后链，已经封好的窗全部保留。
    void BeginRun();
    std::optional<Verdict> RecordTick(uint64_t tick_seq, double heading_deg, double issued_delta_deg, bool degraded_fix);
    // 线路结束时调一次。走坏了的话按更低的窗数门槛再判一次。
    std::optional<Verdict> EndRun(bool route_failed);

    bool fired() const { return fired_; }

    // 上一次封好的窗，取走就清空。
    std::optional<WindowSample> TakeLastWindow();

private:
    using Clock = std::chrono::steady_clock;

    void ResetWindow();
    void CloseWindow();
    std::optional<Verdict> Evaluate(int required_windows);

    Config config_;
    std::vector<double> ratio_history_;

    uint64_t prev_tick_seq_ = 0;
    bool has_prev_tick_ = false;
    double prev_heading_deg_ = 0.0;
    bool has_prev_heading_ = false;

    // 最近 kLagCount 拍的指令，[0] 是当前拍。攒不满说明账刚断过，这拍不进方程。
    LagVector cmd_lags_ {};
    int chain_len_ = 0;

    // 正规方程的累加量。断拍只清滞后链，不动这两个：只有「哪拍对哪拍」要连续，统计量不要。
    LagMatrix xtx_ {};
    LagVector xty_ {};
    double window_cmd_deg_ = 0.0;
    int window_samples_ = 0;
    Clock::time_point window_started_at_ {};

    std::optional<WindowSample> last_window_;
    bool fired_ = false;
};

// 归一到 (-180, 180]，两个朝向读数相减时用。
double NormalizeDeg(double deg);

} // namespace sensitivity

} // namespace mapnavigator
