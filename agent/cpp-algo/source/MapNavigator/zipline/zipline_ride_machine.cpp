#include "zipline_ride_machine.h"

#include <algorithm>
#include <cmath>
#include <limits>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_time.h"

namespace mapnavigator
{

namespace
{

// 上升索的仰角正负不确定, 所以第二次取反, 第三次不动俯仰
double PitchTargetForAttempt(double elevation_deg, int attempt)
{
    if (std::abs(elevation_deg) < kZiplinePitchDeadbandDeg) {
        return 0.0;
    }
    const double aim = std::clamp(elevation_deg, -kZiplinePitchMaximumDepressionDeg, kZiplinePitchMaximumElevationDeg);
    if (attempt == 0) {
        return aim;
    }
    if (attempt == 1) {
        return -aim;
    }
    return 0.0;
}

double BearingDeg(const ZiplineNodeRef& from, const ZiplineNodeRef& to)
{
    return NaviMath::CalcTargetRotation(from.x, from.y, to.x, to.y);
}

// 现发现的架子没有世界坐标, 算不出仰角就平着按
double ElevationDeg(const ZiplineNodeRef& from, const ZiplineNodeRef& to)
{
    if (!from.has_world || !to.has_world) {
        return 0.0;
    }
    const double run = std::hypot(to.world_x - from.world_x, to.world_z - from.world_z);
    return std::atan2(to.world_y - from.world_y, run) * 180.0 / kPi;
}

double AwaySideCapDeg(const std::vector<double>& neighbor_offsets_deg, double away)
{
    double cap = std::numeric_limits<double>::infinity();
    for (const double offset : neighbor_offsets_deg) {
        if (offset * away > 0.0) {
            cap = std::min(cap, std::abs(offset) / 2.0);
        }
    }
    return cap;
}

double DistanceWu(const NaviPosition& a, const NaviPosition& b)
{
    return std::hypot(a.x - b.x, a.y - b.y);
}

const char* StageName(ZiplineStage stage)
{
    switch (stage) {
    case ZiplineStage::Idle:
        return "idle";
    case ZiplineStage::Mounting:
        return "mounting";
    case ZiplineStage::OnTower:
        return "on_tower";
    case ZiplineStage::Aiming:
        return "aiming";
    case ZiplineStage::Fired:
        return "fired";
    case ZiplineStage::Riding:
        return "riding";
    case ZiplineStage::Landed:
        return "landed";
    case ZiplineStage::Classified:
        return "classified";
    case ZiplineStage::ExitAiming:
        return "exit_aiming";
    case ZiplineStage::ReturnAiming:
        return "return_aiming";
    case ZiplineStage::Dismounting:
        return "dismounting";
    case ZiplineStage::Handoff:
        return "handoff";
    case ZiplineStage::Failed:
        return "failed";
    }
    return "?";
}

} // namespace

LandingClass ClassifyLanding(
    const std::optional<NaviPosition>& fix,
    const ZiplineNodeRef& origin,
    const ZiplineNodeRef& target,
    const std::vector<ZiplineNodeRef>& known,
    bool riding_entered,
    ZiplineNodeRef* reached)
{
    if (!fix) {
        return LandingClass::Unknown;
    }
    const ZiplineNodeRef* nearest = nullptr;
    double nearest_distance = kZiplineLandingBandWu;
    for (const ZiplineNodeRef& node : known) {
        const double distance = std::hypot(fix->x - node.x, fix->y - node.y);
        if (distance < nearest_distance) {
            nearest = &node;
            nearest_distance = distance;
        }
    }
    if (nearest != nullptr) {
        *reached = *nearest;
        if (nearest->SameTower(target)) {
            return LandingClass::AtTarget;
        }
        if (nearest->SameTower(origin)) {
            return LandingClass::AtOrigin;
        }
        return LandingClass::AtOther;
    }
    if (riding_entered) {
        *reached = ZiplineNodeRef { .x = fix->x, .y = fix->y };
        return LandingClass::AtStrayTower;
    }
    return LandingClass::Unknown;
}

void ZiplineRideMachine::Begin(const ZiplineHopPlan& plan)
{
    const auto now = Clock::now();
    const bool standing = OnTower();
    // 重新站过上索点再回来仍算同一跳, 记录接着写
    const bool resume = hop_open_ && plan_.mount.SameTower(plan.mount) && plan_.landing.SameTower(plan.landing);
    if (!resume) {
        if (hop_open_) {
            CommitRecord(HopOutcome::Dismounted, now);
        }
        Reset();
        plan_ = plan;
        record_ = {};
        record_.plan = plan;
        record_.began_at = now;
        hop_open_ = true;
        aim_bias_deg_ = FirstShotAimBiasDeg();
    }
    parked_on_.reset();
    origin_ = plan_.mount;
    target_ = plan_.landing;
    returning_ = false;
    pitch_tier_ = 0;
    launch_fix_.reset();
    last_fix_.reset();
    riding_entered_ = false;
    unknown_deadline_.reset();
    pending_exit_ = {};
    LogInfo << "zipline/begin" << VAR(resume) << VAR(plan_.mount.x) << VAR(plan_.mount.y) << VAR(plan_.landing.x) << VAR(plan_.landing.y)
            << VAR(plan_.planned_elevation_deg) << VAR(plan_.siblings.size()) << VAR(aim_bias_deg_) << VAR(plan_.chain_continues)
            << VAR(standing);
    EnterStage(standing ? ZiplineStage::OnTower : ZiplineStage::Mounting, now);
}

StageResult ZiplineRideMachine::Tick(IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = Clock::now();
    switch (stage_) {
    case ZiplineStage::OnTower:
        return TickOnTower(actuator, now);
    case ZiplineStage::Handoff:
    case ZiplineStage::Failed:
        return Handoff(now);
    case ZiplineStage::Idle:
    case ZiplineStage::Classified:
        return {};
    default:
        break;
    }

    // Fired 阶段跟踪器还停在上索点, 会把小地图隐藏帧跟出贴着上索点的假位置
    const ZiplineObservation obs = observer.Observe(KnownNodes(), stage_ == ZiplineStage::Fired);
    switch (stage_) {
    case ZiplineStage::Mounting:
        return TickMounting(obs, observer, actuator);
    case ZiplineStage::Aiming:
    case ZiplineStage::ReturnAiming:
        return TickAiming(obs, actuator);
    case ZiplineStage::Fired:
        return TickFired(obs, observer, actuator);
    case ZiplineStage::Riding:
        return TickRiding(obs, observer, actuator);
    case ZiplineStage::Landed:
        return TickLanded(obs, observer, actuator);
    case ZiplineStage::ExitAiming:
        return TickExitAiming(obs, actuator);
    case ZiplineStage::Dismounting:
        return TickDismounting(obs, observer, actuator);
    default:
        return {};
    }
}

void ZiplineRideMachine::Dismount(IZiplineActuator& actuator)
{
    if (OnTower()) {
        actuator.Dismount();
    }
    // 发射过却被叫下来只可能是重站后仍没发出去, 按滑不动记
    if (hop_open_) {
        CommitRecord(record_.launches.empty() ? HopOutcome::Dismounted : HopOutcome::NoLaunch, Clock::now());
    }
    Reset();
}

// Mounting 不算在架上, 它正在判定是否已上架
bool ZiplineRideMachine::OnTower() const
{
    return parked_on_.has_value() || stage_ == ZiplineStage::OnTower || stage_ == ZiplineStage::Aiming || stage_ == ZiplineStage::Fired
           || stage_ == ZiplineStage::ExitAiming || stage_ == ZiplineStage::ReturnAiming;
}

std::optional<ZiplineNodeRef> ZiplineRideMachine::TowerUnderfoot() const
{
    if (parked_on_) {
        return parked_on_;
    }
    if (OnTower()) {
        return origin_;
    }
    return std::nullopt;
}

void ZiplineRideMachine::Reset()
{
    stage_ = ZiplineStage::Idle;
    stage_entered_at_ = {};
    plan_ = {};
    record_ = {};
    hop_open_ = false;
    origin_ = {};
    target_ = {};
    seed_elevation_deg_ = 0.0;
    aim_bias_deg_ = 0.0;
    pitch_lowered_ = false;
    pitch_tier_ = 0;
    returning_ = false;
    hop_retry_count_ = 0;
    mount_presses_ = 0;
    on_ground_hits_ = 0;
    discovered_towers_.clear();
    parked_on_.reset();
    launch_fix_.reset();
    last_fix_.reset();
    miss_streak_ = 0;
    settle_hits_ = 0;
    riding_entered_ = false;
    minimap_back_at_.reset();
    unknown_deadline_.reset();
    ResetStableHeading();
    yaw_response_ratio_.reset();
    exit_aim_ = {};
    exit_aim_budget_ms_ = 0;
    dismount_presses_ = 0;
    dismount_stable_pos_.reset();
    dismount_stable_hits_ = 0;
    pending_exit_ = {};
}

void ZiplineRideMachine::ResetNavigation()
{
    Reset();
    ledger_.clear();
}

void ZiplineRideMachine::EnterStage(ZiplineStage stage, Clock::time_point now)
{
    stage_ = stage;
    stage_entered_at_ = now;
    LogInfo << "zipline/stage" << VAR(StageName(stage)) << VAR(returning_) << VAR(pitch_tier_) << VAR(hop_retry_count_);
}

int64_t ZiplineRideMachine::StageElapsedMs(Clock::time_point now) const
{
    return ElapsedMs(stage_entered_at_, now);
}

void ZiplineRideMachine::CommitRecord(HopOutcome outcome, Clock::time_point now)
{
    record_.outcome = outcome;
    record_.ended_at = now;
    ledger_.push_back(record_);
    hop_open_ = false;
    LogInfo << "zipline/hop_recorded" << VAR(static_cast<int>(outcome)) << VAR(record_.launches.size())
            << VAR(record_.wrong_rope_bearings_deg.size()) << VAR(ledger_.size());
}

// 规划器只按每条记录的两端封索, 回程索不单独记会被重规划再派回去
void ZiplineRideMachine::CommitFailedReturn(HopOutcome outcome, Clock::time_point now)
{
    ZiplineHopRecord record;
    record.plan.mount = origin_;
    record.plan.landing = plan_.mount;
    record.outcome = outcome;
    record.began_at = now;
    record.ended_at = now;
    ledger_.push_back(record);
    LogWarn << "zipline/return/failed" << VAR(static_cast<int>(outcome)) << VAR(origin_.x) << VAR(origin_.y) << VAR(plan_.mount.x)
            << VAR(plan_.mount.y) << VAR(ledger_.size());
}

// 起点额外按起滑时的实际定位放一份, 台面比节点大, 站偏了也要认出是原地
std::vector<ZiplineNodeRef> ZiplineRideMachine::KnownNodes() const
{
    std::vector<ZiplineNodeRef> known;
    if (launch_fix_) {
        ZiplineNodeRef here = origin_;
        here.x = launch_fix_->x;
        here.y = launch_fix_->y;
        known.push_back(here);
    }
    known.push_back(plan_.mount);
    known.push_back(plan_.landing);
    known.insert(known.end(), plan_.siblings.begin(), plan_.siblings.end());
    known.insert(known.end(), discovered_towers_.begin(), discovered_towers_.end());
    for (const ZiplineHopRecord& record : ledger_) {
        known.push_back(record.plan.mount);
        known.push_back(record.plan.landing);
    }
    return known;
}

std::vector<double> ZiplineRideMachine::NeighborOffsetsDeg() const
{
    std::vector<ZiplineNodeRef> nodes = plan_.siblings;
    nodes.insert(nodes.end(), discovered_towers_.begin(), discovered_towers_.end());
    for (const ZiplineLaunch& launch : record_.launches) {
        if (launch.reached) {
            nodes.push_back(*launch.reached);
        }
    }
    const double planned = BearingDeg(plan_.mount, plan_.landing);
    std::vector<double> offsets;
    for (const ZiplineNodeRef& node : nodes) {
        if (node.SameTower(plan_.mount) || node.SameTower(plan_.landing)) {
            continue;
        }
        offsets.push_back(NaviMath::NormalizeAngle(BearingDeg(plan_.mount, node) - planned));
    }
    return offsets;
}

double ZiplineRideMachine::FirstShotAimBiasDeg() const
{
    const std::vector<double> offsets = NeighborOffsetsDeg();
    const auto nearest = std::ranges::min_element(offsets, {}, [](double offset) { return std::abs(offset); });
    if (nearest == offsets.end() || *nearest == 0.0 || std::abs(*nearest) >= 2.0 * kZiplineAimToleranceDeg) {
        return 0.0;
    }
    const double away = *nearest > 0.0 ? -1.0 : 1.0;
    return away * std::min(kZiplineAimBiasStepDeg, AwaySideCapDeg(offsets, away));
}

double ZiplineRideMachine::EscalatedAimBiasDeg(double wrong_bearing_deg) const
{
    const double offset = NaviMath::NormalizeAngle(wrong_bearing_deg - BearingDeg(plan_.mount, plan_.landing));
    if (offset == 0.0) {
        return aim_bias_deg_;
    }
    const bool overshot = offset * aim_bias_deg_ > 0.0;
    const double away = overshot ? std::copysign(1.0, aim_bias_deg_) : std::copysign(1.0, -offset);
    const double magnitude = overshot ? std::abs(aim_bias_deg_) / 2.0 : std::abs(aim_bias_deg_) + kZiplineAimBiasStepDeg;
    return away * std::min(magnitude, AwaySideCapDeg(NeighborOffsetsDeg(), away));
}

// 被架子锁住时无法移动, 所以能移动即在地面, 零位移本身说明不了什么
StageResult ZiplineRideMachine::TickMounting(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    const int64_t elapsed_ms = StageElapsedMs(now);
    const bool walking = obs.fix && last_fix_ && DistanceWu(*obs.fix, *last_fix_) >= kZiplineMountMinMoveWu;
    if (obs.fix) {
        last_fix_ = obs.fix;
    }
    const MountVerdict verdict = walking ? MountVerdict::OnGround : observer.CheckMounted();
    // settle 内按钮可能还没收起, 地面读数不计入连续帧
    const bool settled = elapsed_ms >= kZiplineMountSettleMs;
    on_ground_hits_ = verdict == MountVerdict::OnGround && settled ? on_ground_hits_ + 1 : 0;

    // 架上操作引导只在站上架子后出现, 但逐帧时有时无, 所以读到一帧即认
    if (verdict == MountVerdict::OnTower) {
        LogInfo << "zipline/mount/confirmed" << VAR(elapsed_ms) << VAR(mount_presses_);
        EnterStage(ZiplineStage::OnTower, now);
        return {};
    }
    // 还在动时重按无效, 这个阶段外面没有看门狗, 窗口耗满必须自己交回导航
    if (walking) {
        if (elapsed_ms <= kZiplineMountWindowMs) {
            return {};
        }
        LogWarn << "zipline/mount/still_moving" << VAR(elapsed_ms) << VAR(mount_presses_);
        CommitRecord(HopOutcome::NotMounted, now);
        EnterStage(ZiplineStage::Idle, now);
        return NeedsReposition {};
    }
    if (on_ground_hits_ >= kZiplineMountOnGroundFixes) {
        return Remount(actuator, "zipline/mount/on_ground", now);
    }
    if (elapsed_ms <= kZiplineMountWindowMs) {
        return {};
    }
    // 判不清时按已上架但漏读提示处理, 先下索回到可判定的地面态再重规划
    if (verdict == MountVerdict::Unclear) {
        LogWarn << "zipline/mount/unreadable" << VAR(elapsed_ms) << VAR(mount_presses_);
        CommitRecord(HopOutcome::NotMounted, now);
        return StartDismount(actuator, ReplanRequested { .still_on_tower = false }, now);
    }
    return Remount(actuator, "zipline/mount/window_expired", now);
}

StageResult ZiplineRideMachine::Remount(IZiplineActuator& actuator, const char* reason, Clock::time_point now)
{
    const int64_t elapsed_ms = StageElapsedMs(now);
    if (mount_presses_ < kZiplineMountPressBudget && actuator.PressMount()) {
        ++mount_presses_;
        on_ground_hits_ = 0;
        LogWarn << "zipline/mount/repress" << VAR(reason) << VAR(elapsed_ms) << VAR(mount_presses_);
        EnterStage(ZiplineStage::Mounting, now);
        return {};
    }
    LogWarn << "zipline/mount/unmounted" << VAR(reason) << VAR(elapsed_ms) << VAR(mount_presses_) << VAR(plan_.mount_spots.size());
    CommitRecord(HopOutcome::NotMounted, now);
    EnterStage(ZiplineStage::Idle, now);
    return NeedsReposition {};
}

void ZiplineRideMachine::MarkMountUnreachable(const ZiplineHopPlan& plan)
{
    const Clock::time_point now = Clock::now();
    ZiplineHopRecord record;
    record.plan = plan;
    record.outcome = HopOutcome::Unboardable;
    record.began_at = now;
    record.ended_at = now;
    ledger_.push_back(record);
    LogWarn << "zipline/mount/unboardable" << VAR(plan.mount.x) << VAR(plan.mount.y) << VAR(plan.mount_spots.size()) << VAR(ledger_.size());
}

StageResult ZiplineRideMachine::TickOnTower(IZiplineActuator& actuator, Clock::time_point now)
{
    if (returning_) {
        target_ = plan_.mount;
        seed_elevation_deg_ = ElevationDeg(origin_, target_);
    }
    else {
        origin_ = plan_.mount;
        target_ = plan_.landing;
        seed_elevation_deg_ = plan_.planned_elevation_deg;
    }
    ResetStableHeading();
    yaw_response_ratio_.reset();
    aim_budget_ms_.reset();
    pitch_lowered_ = false;
    if (!actuator.ResetPitchToMaximum()) {
        return FailAim(actuator, "zipline/aim/pitch_reset_failed", now);
    }
    LogInfo << "zipline/on_tower" << VAR(returning_) << VAR(pitch_tier_) << VAR(seed_elevation_deg_) << VAR(aim_bias_deg_) << VAR(target_.x)
            << VAR(target_.y);
    EnterStage(returning_ ? ZiplineStage::ReturnAiming : ZiplineStage::Aiming, now);
    return {};
}

std::optional<double> ZiplineRideMachine::SettledHeading(const ZiplineObservation& obs)
{
    if (!obs.fix) {
        ResetStableHeading();
        return std::nullopt;
    }
    const double heading = obs.fix->angle;
    const bool agrees = prev_heading_ && std::abs(NaviMath::NormalizeAngle(heading - *prev_heading_)) <= kHeadingStableReadToleranceDeg;
    stable_heading_hits_ = agrees ? stable_heading_hits_ + 1 : 1;
    prev_heading_ = heading;
    if (stable_heading_hits_ < 2) {
        return std::nullopt;
    }
    return heading;
}

void ZiplineRideMachine::ResetStableHeading()
{
    prev_heading_.reset();
    stable_heading_hits_ = 0;
    turn_pending_ = false;
    turn_sent_at_ = {};
}

void ZiplineRideMachine::NoteTurnResponse(double heading)
{
    if (!turn_pending_ || std::abs(turn_cmd_deg_) < kZiplineAimResponseMinCmdDeg) {
        return;
    }
    const double achieved = NaviMath::NormalizeAngle(heading - turn_ref_heading_);
    if (achieved * turn_cmd_deg_ <= 0.0) {
        return;
    }
    yaw_response_ratio_ = std::clamp(achieved / turn_cmd_deg_, 1.0, kZiplineAimResponseRatioMax);
}

std::optional<double> ZiplineRideMachine::IssueYawTurn(IZiplineActuator& actuator, double heading, double residual, Clock::time_point now)
{
    const std::optional<double> issued = actuator.TurnYaw(residual / yaw_response_ratio_.value_or(1.0));
    if (!issued) {
        return std::nullopt;
    }
    turn_ref_heading_ = heading;
    turn_cmd_deg_ = *issued;
    turn_sent_at_ = now;
    turn_pending_ = true;
    stable_heading_hits_ = 0;
    prev_heading_.reset();
    return issued;
}

StageResult ZiplineRideMachine::TickAiming(const ZiplineObservation& obs, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    const int32_t budget_ms = aim_budget_ms_.value_or(kZiplineAimHeadingTimeoutMs);
    if (StageElapsedMs(now) > budget_ms) {
        LogWarn << "zipline/aim/timeout" << VAR(returning_) << VAR(turn_pending_) << VAR(stable_heading_hits_) << VAR(budget_ms);
        return FailAim(actuator, "zipline/aim/timeout", now);
    }
    if (turn_pending_ && ElapsedMs(turn_sent_at_, now) < kWaitAfterFirstTurnMs) {
        return {};
    }
    const std::optional<double> settled = SettledHeading(obs);
    if (!settled) {
        return {};
    }
    const double heading = *settled;
    NoteTurnResponse(heading);
    turn_pending_ = false;

    const double bias = returning_ ? 0.0 : aim_bias_deg_;
    const bool sweep_pending = bias != 0.0 && !pitch_lowered_;
    const double target_heading = NaviMath::CalcTargetRotation(obs.fix->x, obs.fix->y, target_.x, target_.y) + bias
                                  + (sweep_pending ? std::copysign(kZiplineAimSweepLeadDeg, bias) : 0.0);
    const double residual = NaviMath::NormalizeAngle(target_heading - heading);
    if (!aim_budget_ms_) {
        const int64_t elapsed_ms = StageElapsedMs(now);
        aim_budget_ms_ = std::max(kZiplineAimHeadingTimeoutMs, static_cast<int32_t>(elapsed_ms) + TurnBudgetMs(residual));
        LogInfo << "zipline/aim/budget" << VAR(residual) << VAR(elapsed_ms) << VAR(*aim_budget_ms_);
    }
    if (std::abs(residual) > kZiplineAimToleranceDeg) {
        const std::optional<double> issued = IssueYawTurn(actuator, heading, residual, now);
        if (!issued) {
            return FailAim(actuator, "zipline/aim/turn_rejected", now);
        }
        const double response_ratio = yaw_response_ratio_.value_or(1.0);
        LogInfo << "zipline/aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued) << VAR(response_ratio)
                << VAR(pitch_lowered_);
        return {};
    }

    // 镜头此刻在俯仰上限, 从那里开环调到目标角
    const double pitch_target = PitchTargetForAttempt(seed_elevation_deg_, pitch_tier_);
    if (!pitch_lowered_) {
        const double pitch_delta = pitch_target - kZiplinePitchMaximumElevationDeg;
        if (std::abs(pitch_delta) >= 1.0) {
            if (!actuator.TurnPitch(pitch_delta)) {
                return FailAim(actuator, "zipline/aim/pitch_rejected", now);
            }
            actuator.Wait(kWaitAfterFirstTurnMs);
        }
        pitch_lowered_ = true;
        if (sweep_pending) {
            LogInfo << "zipline/aim/sweep" << VAR(target_heading) << VAR(heading) << VAR(bias) << VAR(pitch_target);
            // 横扫是新一段闭环, 重新计时
            stage_entered_at_ = Clock::now();
            aim_budget_ms_.reset();
            ResetStableHeading();
            return {};
        }
    }
    actuator.FireLaunch();
    const auto fired_at = Clock::now();
    record_.launches.push_back(ZiplineLaunch {
        .fired_at = fired_at,
        .heading_deg = heading,
        .pitch_tier = pitch_tier_,
        .aim_bias_deg = bias,
    });
    launch_fix_ = obs.fix;
    last_fix_.reset();
    miss_streak_ = 0;
    settle_hits_ = 0;
    riding_entered_ = false;
    minimap_back_at_.reset();
    unknown_deadline_.reset();
    LogInfo << "zipline/fired" << VAR(returning_) << VAR(pitch_tier_) << VAR(heading) << VAR(target_heading) << VAR(bias)
            << VAR(pitch_target) << VAR(record_.launches.size());
    EnterStage(ZiplineStage::Fired, fired_at);
    return {};
}

// 滑行中小地图隐藏, 定位连断就是滑出去了; 空响时人还在上索架上, 小地图照常显示
StageResult ZiplineRideMachine::TickFired(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    const int64_t elapsed_ms = StageElapsedMs(now);
    if (elapsed_ms < kZiplineLaunchSettleMs) {
        return {};
    }
    if (obs.fix) {
        miss_streak_ = 0;
        const double moved = launch_fix_ ? DistanceWu(*obs.fix, *launch_fix_) : kZiplineMountMinMoveWu;
        if (moved >= kZiplineMountMinMoveWu) {
            riding_entered_ = true;
            last_fix_ = obs.fix;
            settle_hits_ = 0;
            LogInfo << "zipline/fired/moved" << VAR(moved) << VAR(elapsed_ms);
            EnterStage(ZiplineStage::Landed, now);
            return {};
        }
        if (elapsed_ms > kZiplineLaunchConfirmMs) {
            last_fix_ = obs.fix;
            LogWarn << "zipline/fired/no_launch" << VAR(moved) << VAR(elapsed_ms) << VAR(pitch_tier_) << VAR(aim_bias_deg_);
            return Classify(observer, actuator, now);
        }
        return {};
    }
    if (++miss_streak_ >= kZiplineRideLostFixes) {
        riding_entered_ = true;
        // 跟踪器留着起点旧位置会把真落点当远跳拒掉
        observer.ResetTracking();
        LogInfo << "zipline/fired/riding" << VAR(elapsed_ms);
        EnterStage(ZiplineStage::Riding, now);
        return {};
    }
    if (elapsed_ms > kZiplineRideTimeoutMs) {
        last_fix_.reset();
        return Classify(observer, actuator, now);
    }
    return {};
}

StageResult ZiplineRideMachine::TickRiding(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (obs.fix && !minimap_back_at_) {
        minimap_back_at_ = now;
        LogInfo << "zipline/riding/minimap_back" << VAR(obs.fix->x) << VAR(obs.fix->y) << VAR(StageElapsedMs(now));
    }
    if (minimap_back_at_ && ElapsedMs(*minimap_back_at_, now) < kZiplineLandingSettleMs) {
        // 定位器会把冷却期内的位置记成跟踪锚点
        observer.ResetTracking();
        return {};
    }
    if (obs.fix) {
        last_fix_ = obs.fix;
        settle_hits_ = 0;
        LogInfo << "zipline/riding/fix_back" << VAR(obs.fix->x) << VAR(obs.fix->y) << VAR(StageElapsedMs(now));
        EnterStage(ZiplineStage::Landed, now);
        return {};
    }
    if (StageElapsedMs(now) > kZiplineRideTimeoutMs) {
        LogWarn << "zipline/riding/timeout" << VAR(kZiplineRideTimeoutMs);
        last_fix_.reset();
        return Classify(observer, actuator, now);
    }
    return {};
}

StageResult ZiplineRideMachine::TickLanded(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (obs.fix) {
        settle_hits_ = last_fix_ && DistanceWu(*obs.fix, *last_fix_) < kZiplineSettleMoveWu ? settle_hits_ + 1 : 1;
        last_fix_ = obs.fix;
        if (settle_hits_ >= kZiplineSettleFixes) {
            return Classify(observer, actuator, now);
        }
    }
    else {
        settle_hits_ = 0;
    }
    if (StageElapsedMs(now) > kZiplineUnknownTimeoutMs) {
        LogWarn << "zipline/landed/unsettled" << VAR(settle_hits_) << VAR(last_fix_.has_value());
        last_fix_.reset();
        return Classify(observer, actuator, now);
    }
    return {};
}

// 下索会沿按键时的朝向带出一段动量, 所以先摆朝向; 这一步失败也照常下索
StageResult ZiplineRideMachine::TickExitAiming(const ZiplineObservation& obs, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (StageElapsedMs(now) > exit_aim_budget_ms_) {
        LogWarn << "zipline/exit_aim/timeout" << VAR(StageElapsedMs(now)) << VAR(exit_aim_budget_ms_) << VAR(turn_pending_)
                << VAR(stable_heading_hits_);
        return LeaveTowerAfterAim(actuator, now);
    }
    if (turn_pending_ && ElapsedMs(turn_sent_at_, now) < kWaitAfterFirstTurnMs) {
        return {};
    }
    const std::optional<double> settled = SettledHeading(obs);
    if (!settled) {
        return {};
    }
    const double heading = *settled;
    NoteTurnResponse(heading);
    turn_pending_ = false;

    const double target_heading = NaviMath::CalcTargetRotation(obs.fix->x, obs.fix->y, exit_aim_.x, exit_aim_.y);
    const double residual = NaviMath::NormalizeAngle(target_heading - heading);
    if (std::abs(residual) > kZiplineExitAimToleranceDeg) {
        const std::optional<double> issued = IssueYawTurn(actuator, heading, residual, now);
        if (!issued) {
            LogWarn << "zipline/exit_aim/turn_rejected" << VAR(target_heading) << VAR(heading) << VAR(residual);
            return LeaveTowerAfterAim(actuator, now);
        }
        const double response_ratio = yaw_response_ratio_.value_or(1.0);
        LogInfo << "zipline/exit_aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued) << VAR(response_ratio);
        return {};
    }
    LogInfo << "zipline/exit_aim/aligned" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(StageElapsedMs(now));
    return LeaveTowerAfterAim(actuator, now);
}

double ZiplineRideMachine::ResidualToExitAim() const
{
    if (!last_fix_) {
        return 0.0;
    }
    const double target_heading = NaviMath::CalcTargetRotation(last_fix_->x, last_fix_->y, exit_aim_.x, exit_aim_.y);
    return NaviMath::NormalizeAngle(target_heading - last_fix_->angle);
}

int32_t ZiplineRideMachine::TurnBudgetMs(double residual) const
{
    return kZiplineTurnBudgetBaseMs + static_cast<int32_t>(std::lround(std::abs(residual) * kZiplineTurnBudgetPerDegMs));
}

StageResult ZiplineRideMachine::StartExitAim(const HopCompleted& done, Clock::time_point now)
{
    ResetStableHeading();
    yaw_response_ratio_.reset();
    exit_aim_ = *plan_.exit_aim;
    exit_aim_budget_ms_ = TurnBudgetMs(ResidualToExitAim());
    pending_exit_ = done;
    EnterStage(ZiplineStage::ExitAiming, now);
    LogInfo << "zipline/exit_aim/begin" << VAR(exit_aim_.x) << VAR(exit_aim_.y) << VAR(exit_aim_budget_ms_)
            << VAR(last_fix_ ? last_fix_->angle : 0.0);
    return {};
}

StageResult ZiplineRideMachine::LeaveTowerAfterAim(IZiplineActuator& actuator, Clock::time_point now)
{
    StageResult exit = std::move(pending_exit_);
    return StartDismount(actuator, std::move(exit), now);
}

StageResult ZiplineRideMachine::Classify(IZiplineObserver& observer, IZiplineActuator& actuator, Clock::time_point now)
{
    EnterStage(ZiplineStage::Classified, now);
    ZiplineNodeRef reached;
    const LandingClass landing = ClassifyLanding(last_fix_, origin_, target_, KnownNodes(), riding_entered_, &reached);
    if (!record_.launches.empty()) {
        ZiplineLaunch& launch = record_.launches.back();
        launch.result = landing;
        launch.stopped_at = last_fix_;
        if (landing != LandingClass::Unknown) {
            launch.reached = reached;
        }
    }
    LogInfo << "zipline/classified" << VAR(static_cast<int>(landing)) << VAR(returning_) << VAR(riding_entered_) << VAR(pitch_tier_)
            << VAR(hop_retry_count_) << VAR(last_fix_ ? last_fix_->x : 0.0) << VAR(last_fix_ ? last_fix_->y : 0.0) << VAR(reached.x)
            << VAR(reached.y);

    switch (landing) {
    case LandingClass::AtTarget: {
        if (!returning_) {
            CommitRecord(HopOutcome::Completed, now);
            const HopCompleted done { .at = *last_fix_, .still_on_tower = plan_.chain_continues };
            if (plan_.chain_continues) {
                parked_on_ = plan_.landing;
                pending_exit_ = done;
                EnterStage(ZiplineStage::Handoff, now);
                return Handoff(now);
            }
            if (plan_.exit_aim) {
                return StartExitAim(done, now);
            }
            return StartDismount(actuator, done, now);
        }
        // 回程时 target_ 是上索架, 到了就是滑回来了
        const double wrong_bearing = BearingDeg(plan_.mount, origin_);
        record_.wrong_rope_bearings_deg.push_back(wrong_bearing);
        if (hop_retry_count_ < kZiplineHopRetryBudget) {
            ++hop_retry_count_;
            returning_ = false;
            pitch_tier_ = 0;
            aim_bias_deg_ = EscalatedAimBiasDeg(wrong_bearing);
            LogInfo << "zipline/aim/bias_escalated" << VAR(wrong_bearing) << VAR(aim_bias_deg_) << VAR(hop_retry_count_);
            EnterStage(ZiplineStage::OnTower, now);
            return {};
        }
        CommitRecord(HopOutcome::WrongRope, now);
        return ParkForReplan(plan_.mount, now);
    }
    case LandingClass::AtOther:
    case LandingClass::AtStrayTower: {
        if (!returning_) {
            if (landing == LandingClass::AtStrayTower) {
                discovered_towers_.push_back(reached);
            }
            origin_ = reached;
            returning_ = true;
            pitch_tier_ = 0;
            EnterStage(ZiplineStage::OnTower, now);
            return {};
        }
        CommitRecord(HopOutcome::WrongRope, now);
        CommitFailedReturn(HopOutcome::WrongRope, now);
        return ParkForReplan(reached, now);
    }
    case LandingClass::AtOrigin: {
        if (!returning_ && std::abs(aim_bias_deg_) / 2.0 >= kZiplineAimBiasMinDeg) {
            aim_bias_deg_ /= 2.0;
            LogInfo << "zipline/aim/bias_retracted" << VAR(aim_bias_deg_) << VAR(pitch_tier_);
            EnterStage(ZiplineStage::OnTower, now);
            return {};
        }
        if (pitch_tier_ + 1 < kZiplineLaunchAttempts) {
            ++pitch_tier_;
            EnterStage(ZiplineStage::OnTower, now);
            return {};
        }
        if (returning_) {
            CommitRecord(HopOutcome::WrongRope, now);
            CommitFailedReturn(HopOutcome::NoLaunch, now);
            return ParkForReplan(origin_, now);
        }
        // 留在架上等重规划, 新路线用不上这根架子时才下来
        CommitRecord(HopOutcome::NoLaunch, now);
        return ParkForReplan(plan_.mount, now);
    }
    case LandingClass::Unknown: {
        if (!unknown_deadline_) {
            unknown_deadline_ = now + std::chrono::milliseconds(kZiplineUnknownTimeoutMs);
            observer.ResetTracking();
        }
        if (now < *unknown_deadline_) {
            settle_hits_ = 0;
            EnterStage(ZiplineStage::Landed, now);
            return {};
        }
        CommitRecord(HopOutcome::Lost, now);
        return StartDismount(actuator, ChainAbandoned { "zipline/landing/lost" }, now);
    }
    case LandingClass::Pending:
        break;
    }
    return {};
}

// 下索键偶尔不生效, 人留在架上定位也稳, 所以还要读不到架上引导才算下来
StageResult ZiplineRideMachine::TickDismounting(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (obs.fix) {
        const bool same = dismount_stable_pos_ && DistanceWu(*obs.fix, *dismount_stable_pos_) <= kZiplineRecoveryStableRadiusWu;
        dismount_stable_hits_ = same ? dismount_stable_hits_ + 1 : 1;
        dismount_stable_pos_ = obs.fix;
        if (dismount_stable_hits_ >= kZiplineRecoveryStableFixes && observer.CheckMounted() != MountVerdict::OnTower) {
            EnterStage(ZiplineStage::Handoff, now);
            return Handoff(now);
        }
    }
    const int64_t elapsed_ms = StageElapsedMs(now);
    if (dismount_presses_ == 1 && elapsed_ms > 2 * kZiplineDismountTimeoutMs) {
        LogWarn << "zipline/dismount/repress" << VAR(elapsed_ms) << VAR(dismount_stable_hits_);
        actuator.Dismount();
        ++dismount_presses_;
        return {};
    }
    if (elapsed_ms > 4 * kZiplineDismountTimeoutMs) {
        LogWarn << "zipline/dismount/stuck" << VAR(elapsed_ms) << VAR(dismount_presses_) << VAR(dismount_stable_hits_);
        pending_exit_ = ChainAbandoned { "zipline/dismount/stuck" };
        EnterStage(ZiplineStage::Failed, now);
        return Handoff(now);
    }
    return {};
}

StageResult ZiplineRideMachine::FailAim(IZiplineActuator& actuator, const char* reason, Clock::time_point now)
{
    LogWarn << "zipline/aim/failed" << VAR(reason) << VAR(returning_) << VAR(pitch_tier_);
    CommitRecord(HopOutcome::AimFailed, now);
    return StartDismount(actuator, ChainAbandoned { reason }, now);
}

StageResult ZiplineRideMachine::StartDismount(IZiplineActuator& actuator, StageResult exit, Clock::time_point now)
{
    pending_exit_ = std::move(exit);
    actuator.Dismount();
    dismount_presses_ = 1;
    dismount_stable_pos_.reset();
    dismount_stable_hits_ = 0;
    EnterStage(ZiplineStage::Dismounting, now);
    return {};
}

StageResult ZiplineRideMachine::ParkForReplan(const ZiplineNodeRef& tower, Clock::time_point now)
{
    parked_on_ = tower;
    pending_exit_ = ReplanRequested { .still_on_tower = true, .on_tower = tower };
    EnterStage(ZiplineStage::Handoff, now);
    return Handoff(now);
}

StageResult ZiplineRideMachine::Handoff(Clock::time_point now)
{
    LogInfo << "zipline/handoff" << VAR(StageName(stage_)) << VAR(pending_exit_.index()) << VAR(parked_on_.has_value())
            << VAR(StageElapsedMs(now));
    StageResult exit = std::move(pending_exit_);
    const std::optional<ZiplineNodeRef> parked = parked_on_;
    Reset();
    parked_on_ = parked;
    return exit;
}

} // namespace mapnavigator
