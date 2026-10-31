#pragma once

#include <chrono>
#include <cstdint>
#include <optional>
#include <vector>

#include "zipline_types.h"

namespace mapnavigator
{

// hint_nodes 是这一帧定位优先搜索的位置
class IZiplineObserver
{
public:
    virtual ~IZiplineObserver() = default;
    virtual ZiplineObservation observe(const std::vector<ZiplineNodeRef>& hint_nodes, bool force_global_search) = 0;
    // 只在上索与下索确认期间调, 每次都要跑识别, 开销比一帧定位高
    virtual MountVerdict checkMounted() = 0;
    virtual void resetTracking() = 0;
};

// 每个动作同步发出、发完即回, 到没到位靠后续观测
class IZiplineActuator
{
public:
    virtual ~IZiplineActuator() = default;
    virtual bool resetPitchToMaximum() = 0;
    // 只发一个后端批次, 返回实际发出的度数, 发不出去返回空
    virtual std::optional<double> turnYaw(double delta_deg) = 0;
    virtual bool turnPitch(double delta_deg) = 0;
    // 识别不到交互提示时不发按键, 返回是否识别到
    virtual bool pressMount() = 0;
    virtual void fireLaunch() = 0;
    virtual void dismount() = 0;
    virtual void wait(int32_t ms) = 0;
};

// reached 返回命中的节点, 圈外且滑行过时按定位坐标现造一根未登记架子
LandingClass ClassifyLanding(
    const std::optional<NaviPosition>& fix,
    const ZiplineNodeRef& origin,
    const ZiplineNodeRef& target,
    const std::vector<ZiplineNodeRef>& known,
    bool riding_entered,
    ZiplineNodeRef* reached);

class YawAimLoop
{
public:
    using Clock = std::chrono::steady_clock;

    static int32_t turnBudgetMs(double residual);

    // 保留转向响应比
    void restart();
    // 转向生效且朝向读稳后才返回朝向, 同时结束在途转向
    std::optional<double> poll(const ZiplineObservation& obs, Clock::time_point now);
    std::optional<double> issueTurn(IZiplineActuator& actuator, double heading, double residual, Clock::time_point now);

    double responseRatio() const { return yaw_response_ratio_.value_or(1.0); }

    bool turnPending() const { return turn_pending_; }

    int stableHits() const { return stable_heading_hits_; }

private:
    std::optional<double> settledHeading(const ZiplineObservation& obs);
    void noteTurnResponse(double heading);

    std::optional<double> prev_heading_;
    int stable_heading_hits_ = 0;
    bool turn_pending_ = false;
    Clock::time_point turn_sent_at_ {};
    double turn_ref_heading_ = 0.0;
    double turn_cmd_deg_ = 0.0;
    // 实际转角与指令之比, 不小于 1
    std::optional<double> yaw_response_ratio_;
};

struct LandingWatch
{
    std::optional<NaviPosition> launch_fix;
    int miss_streak = 0;
    int settle_hits = 0;
    bool riding_entered = false;
    std::optional<std::chrono::steady_clock::time_point> minimap_back_at;
    std::optional<std::chrono::steady_clock::time_point> unknown_deadline;

    void arm(const std::optional<NaviPosition>& fix)
    {
        *this = {};
        launch_fix = fix;
    }
};

struct DismountWatch
{
    int presses = 0;
    std::optional<NaviPosition> stable_pos;
    int stable_hits = 0;

    void arm()
    {
        *this = {};
        presses = 1;
    }
};

class ZiplineRideMachine
{
public:
    // 人不在架子上时调用方要先按上索键再调
    void begin(const ZiplineHopPlan& plan);
    StageResult tick(IZiplineObserver& observer, IZiplineActuator& actuator);
    void dismount(IZiplineActuator& actuator);
    void markMountUnreachable(const ZiplineHopPlan& plan);

    bool onTower() const;
    std::optional<ZiplineNodeRef> towerUnderfoot() const;

    ZiplineStage stage() const { return stage_; }

    const HopLedger& ledger() const { return ledger_; }

    // 清掉这一跳, 账本留着
    void reset();
    void resetNavigation();

private:
    using Clock = std::chrono::steady_clock;

    void enterStage(ZiplineStage stage, Clock::time_point now);
    int64_t stageElapsedMs(Clock::time_point now) const;
    void commitRecord(HopOutcome outcome, Clock::time_point now);
    void commitFailedReturn(HopOutcome outcome, Clock::time_point now);
    std::vector<ZiplineNodeRef> knownNodes() const;
    std::vector<double> neighborOffsetsDeg() const;
    double firstShotAimBiasDeg() const;
    double escalatedAimBiasDeg(double wrong_bearing_deg) const;

    StageResult startExitAim(const HopCompleted& done, Clock::time_point now);
    double residualToExitAim() const;
    StageResult leaveTowerAfterAim(IZiplineActuator& actuator, Clock::time_point now);

    StageResult tickMounting(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator);
    StageResult remount(IZiplineActuator& actuator, const char* reason, Clock::time_point now);
    StageResult tickOnTower(IZiplineActuator& actuator, Clock::time_point now);
    StageResult tickAiming(const ZiplineObservation& obs, IZiplineActuator& actuator);
    StageResult tickFired(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator);
    StageResult tickRiding(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator);
    StageResult tickLanded(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator);
    StageResult tickExitAiming(const ZiplineObservation& obs, IZiplineActuator& actuator);
    StageResult classify(IZiplineObserver& observer, IZiplineActuator& actuator, Clock::time_point now);
    void stampLaunch(LandingClass landing, const ZiplineNodeRef& reached);
    StageResult onAtTarget(IZiplineActuator& actuator, Clock::time_point now);
    StageResult onWrongTower(LandingClass landing, const ZiplineNodeRef& reached, Clock::time_point now);
    StageResult onBackAtOrigin(Clock::time_point now);
    StageResult onUnknown(IZiplineObserver& observer, IZiplineActuator& actuator, Clock::time_point now);
    StageResult tickDismounting(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator);
    StageResult failAim(IZiplineActuator& actuator, const char* reason, Clock::time_point now);
    StageResult startDismount(IZiplineActuator& actuator, StageResult exit, Clock::time_point now);
    StageResult parkForReplan(const ZiplineNodeRef& tower, Clock::time_point now);
    StageResult parkAndHandoff(const ZiplineNodeRef& tower, StageResult exit, Clock::time_point now);
    StageResult handoff(Clock::time_point now);

    ZiplineStage stage_ = ZiplineStage::Idle;
    Clock::time_point stage_entered_at_ {};
    ZiplineHopPlan plan_;
    ZiplineHopRecord record_;
    bool hop_open_ = false;
    HopLedger ledger_;

    // 回程时 origin_ 是滑到的那根架子, target_ 是上索架
    ZiplineNodeRef origin_;
    ZiplineNodeRef target_;
    double seed_elevation_deg_ = 0.0;
    // 相对落点方位的瞄准偏置, 一跳内跨发射保留
    double aim_bias_deg_ = 0.0;
    bool pitch_lowered_ = false;
    int pitch_tier_ = 0;
    bool returning_ = false;
    int hop_retry_count_ = 0;
    int mount_presses_ = 0;
    int on_ground_hits_ = 0;
    std::vector<ZiplineNodeRef> discovered_towers_;
    // 交回导航后人还站着的架子
    std::optional<ZiplineNodeRef> parked_on_;

    std::optional<NaviPosition> last_fix_;
    LandingWatch landing_;

    YawAimLoop aim_;
    std::optional<int32_t> aim_budget_ms_;

    ZiplineMountSpot exit_aim_;
    int32_t exit_aim_budget_ms_ = 0;

    DismountWatch dismount_;
    StageResult pending_exit_;
};

} // namespace mapnavigator
