#include "zipline_ride_machine.h"

#include <algorithm>
#include <cmath>
#include <iterator>
#include <limits>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"

namespace mapnavigator
{

namespace
{

// 上升索的仰角正负不确定, 所以第二次取反, 第三次不动俯仰
double pitch_target_for_attempt(double elevation_deg, int attempt)
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

double bearing_deg(const ZiplineNodeRef& from, const ZiplineNodeRef& to)
{
    return NaviMath::calcTargetRotation(from.x, from.y, to.x, to.y);
}

// 现发现的架子没有世界坐标, 算不出仰角就平着按
double elevation_deg(const ZiplineNodeRef& from, const ZiplineNodeRef& to)
{
    if (!from.has_world || !to.has_world) {
        return 0.0;
    }
    const double run = std::hypot(to.world_x - from.world_x, to.world_z - from.world_z);
    return std::atan2(to.world_y - from.world_y, run) * 180.0 / kPi;
}

double away_side_cap_deg(const std::vector<double>& neighbor_offsets_deg, double away)
{
    double cap = std::numeric_limits<double>::infinity();
    for (const double offset : neighbor_offsets_deg) {
        if (offset * away > 0.0) {
            cap = std::min(cap, std::abs(offset) / 2.0);
        }
    }
    return cap;
}

double distance_wu(const NaviPosition& a, const NaviPosition& b)
{
    return std::hypot(a.x - b.x, a.y - b.y);
}

std::optional<ZiplineNodeRef> previous_wrong_tower(const std::vector<ZiplineLaunch>& launches)
{
    if (launches.size() < 2) {
        return std::nullopt;
    }
    for (auto it = std::next(launches.rbegin()); it != launches.rend(); ++it) {
        if (it->result == LandingClass::AtOther || it->result == LandingClass::AtStrayTower) {
            return it->reached;
        }
    }
    return std::nullopt;
}

const char* stage_name(ZiplineStage stage)
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
        if (nearest->sameTower(target)) {
            return LandingClass::AtTarget;
        }
        if (nearest->sameTower(origin)) {
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

void ZiplineRideMachine::begin(const ZiplineHopPlan& plan)
{
    const auto now = Clock::now();
    const bool standing = onTower();
    // 重新站过上索点再回来仍算同一跳, 记录接着写
    const bool resume = hop_open_ && plan_.mount.sameTower(plan.mount) && plan_.landing.sameTower(plan.landing);
    if (!resume) {
        if (hop_open_) {
            commitRecord(HopOutcome::Dismounted, now);
        }
        reset();
        plan_ = plan;
        record_ = {};
        record_.plan = plan;
        record_.began_at = now;
        hop_open_ = true;
        aim_bias_deg_ = firstShotAimBiasDeg();
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
    enterStage(standing ? ZiplineStage::OnTower : ZiplineStage::Mounting, now);
}

StageResult ZiplineRideMachine::tick(IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = Clock::now();
    switch (stage_) {
    case ZiplineStage::OnTower:
        return tickOnTower(actuator, now);
    case ZiplineStage::Handoff:
    case ZiplineStage::Failed:
        return handoff(now);
    case ZiplineStage::Idle:
    case ZiplineStage::Classified:
        return {};
    default:
        break;
    }

    // Fired 阶段跟踪器还停在上索点, 会把小地图隐藏帧跟出贴着上索点的假位置
    const ZiplineObservation obs = observer.observe(knownNodes(), stage_ == ZiplineStage::Fired);
    switch (stage_) {
    case ZiplineStage::Mounting:
        return tickMounting(obs, observer, actuator);
    case ZiplineStage::Aiming:
    case ZiplineStage::ReturnAiming:
        return tickAiming(obs, actuator);
    case ZiplineStage::Fired:
        return tickFired(obs, observer, actuator);
    case ZiplineStage::Riding:
        return tickRiding(obs, observer, actuator);
    case ZiplineStage::Landed:
        return tickLanded(obs, observer, actuator);
    case ZiplineStage::ExitAiming:
        return tickExitAiming(obs, actuator);
    case ZiplineStage::Dismounting:
        return tickDismounting(obs, observer, actuator);
    default:
        return {};
    }
}

void ZiplineRideMachine::dismount(IZiplineActuator& actuator)
{
    if (onTower()) {
        actuator.dismount();
    }
    // 发射过却被叫下来只可能是重站后仍没发出去, 按滑不动记
    if (hop_open_) {
        commitRecord(record_.launches.empty() ? HopOutcome::Dismounted : HopOutcome::NoLaunch, Clock::now());
    }
    reset();
}

// Mounting 不算在架上, 它正在判定是否已上架
bool ZiplineRideMachine::onTower() const
{
    return parked_on_.has_value() || stage_ == ZiplineStage::OnTower || stage_ == ZiplineStage::Aiming || stage_ == ZiplineStage::Fired
           || stage_ == ZiplineStage::ExitAiming || stage_ == ZiplineStage::ReturnAiming;
}

std::optional<ZiplineNodeRef> ZiplineRideMachine::towerUnderfoot() const
{
    if (parked_on_) {
        return parked_on_;
    }
    if (onTower()) {
        return origin_;
    }
    return std::nullopt;
}

void ZiplineRideMachine::reset()
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
    resetStableHeading();
    yaw_response_ratio_.reset();
    exit_aim_ = {};
    exit_aim_budget_ms_ = 0;
    dismount_presses_ = 0;
    dismount_stable_pos_.reset();
    dismount_stable_hits_ = 0;
    pending_exit_ = {};
}

void ZiplineRideMachine::resetNavigation()
{
    reset();
    ledger_.clear();
}

void ZiplineRideMachine::enterStage(ZiplineStage stage, Clock::time_point now)
{
    stage_ = stage;
    stage_entered_at_ = now;
    LogInfo << "zipline/stage" << VAR_AS(StageName(stage), stage_name(stage)) << VAR(returning_) << VAR(pitch_tier_)
            << VAR(hop_retry_count_);
}

int64_t ZiplineRideMachine::stageElapsedMs(Clock::time_point now) const
{
    return ElapsedMs(stage_entered_at_, now);
}

void ZiplineRideMachine::commitRecord(HopOutcome outcome, Clock::time_point now)
{
    record_.outcome = outcome;
    record_.ended_at = now;
    ledger_.push_back(record_);
    hop_open_ = false;
    LogInfo << "zipline/hop_recorded" << VAR(static_cast<int>(outcome)) << VAR(record_.launches.size())
            << VAR(record_.wrong_rope_bearings_deg.size()) << VAR(ledger_.size());
}

// 规划器只按每条记录的两端封索, 回程索不单独记会被重规划再派回去
void ZiplineRideMachine::commitFailedReturn(HopOutcome outcome, Clock::time_point now)
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
std::vector<ZiplineNodeRef> ZiplineRideMachine::knownNodes() const
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

std::vector<double> ZiplineRideMachine::neighborOffsetsDeg() const
{
    std::vector<ZiplineNodeRef> nodes = plan_.siblings;
    nodes.insert(nodes.end(), discovered_towers_.begin(), discovered_towers_.end());
    for (const ZiplineLaunch& launch : record_.launches) {
        if (launch.reached) {
            nodes.push_back(*launch.reached);
        }
    }
    const double planned = bearing_deg(plan_.mount, plan_.landing);
    std::vector<double> offsets;
    for (const ZiplineNodeRef& node : nodes) {
        if (node.sameTower(plan_.mount) || node.sameTower(plan_.landing)) {
            continue;
        }
        offsets.push_back(NaviMath::normalizeAngle(bearing_deg(plan_.mount, node) - planned));
    }
    return offsets;
}

double ZiplineRideMachine::firstShotAimBiasDeg() const
{
    const std::vector<double> offsets = neighborOffsetsDeg();
    const auto nearest = std::ranges::min_element(offsets, {}, [](double offset) { return std::abs(offset); });
    if (nearest == offsets.end() || *nearest == 0.0 || std::abs(*nearest) >= 2.0 * kZiplineAimToleranceDeg) {
        return 0.0;
    }
    const double away = *nearest > 0.0 ? -1.0 : 1.0;
    return away * std::min(kZiplineAimBiasStepDeg, away_side_cap_deg(offsets, away));
}

double ZiplineRideMachine::escalatedAimBiasDeg(double wrong_bearing_deg) const
{
    const double offset = NaviMath::normalizeAngle(wrong_bearing_deg - bearing_deg(plan_.mount, plan_.landing));
    if (offset == 0.0) {
        return aim_bias_deg_;
    }
    const bool overshot = offset * aim_bias_deg_ > 0.0;
    const double away = overshot ? std::copysign(1.0, aim_bias_deg_) : std::copysign(1.0, -offset);
    const double magnitude = overshot ? std::abs(aim_bias_deg_) / 2.0 : std::abs(aim_bias_deg_) + kZiplineAimBiasStepDeg;
    return away * std::min(magnitude, away_side_cap_deg(neighborOffsetsDeg(), away));
}

// 被架子锁住时无法移动, 所以能移动即在地面, 零位移本身说明不了什么
StageResult ZiplineRideMachine::tickMounting(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    const int64_t elapsed_ms = stageElapsedMs(now);
    const bool walking = obs.fix && last_fix_ && distance_wu(*obs.fix, *last_fix_) >= kZiplineMountMinMoveWu;
    if (obs.fix) {
        last_fix_ = obs.fix;
    }
    const MountVerdict verdict = walking ? MountVerdict::OnGround : observer.checkMounted();
    // settle 内按钮可能还没收起, 地面读数不计入连续帧
    const bool settled = elapsed_ms >= kZiplineMountSettleMs;
    on_ground_hits_ = verdict == MountVerdict::OnGround && settled ? on_ground_hits_ + 1 : 0;

    // 架上操作引导只在站上架子后出现, 但逐帧时有时无, 所以读到一帧即认
    if (verdict == MountVerdict::OnTower) {
        LogInfo << "zipline/mount/confirmed" << VAR(elapsed_ms) << VAR(mount_presses_);
        enterStage(ZiplineStage::OnTower, now);
        return {};
    }
    // 还在动时重按无效, 这个阶段外面没有看门狗, 窗口耗满必须自己交回导航
    if (walking) {
        if (elapsed_ms <= kZiplineMountWindowMs) {
            return {};
        }
        LogWarn << "zipline/mount/still_moving" << VAR(elapsed_ms) << VAR(mount_presses_);
        commitRecord(HopOutcome::NotMounted, now);
        enterStage(ZiplineStage::Idle, now);
        return NeedsReposition {};
    }
    if (on_ground_hits_ >= kZiplineMountOnGroundFixes) {
        return remount(actuator, "zipline/mount/on_ground", now);
    }
    if (elapsed_ms <= kZiplineMountWindowMs) {
        return {};
    }
    // 判不清时按已上架但漏读提示处理, 先下索回到可判定的地面态再重规划
    if (verdict == MountVerdict::Unclear) {
        LogWarn << "zipline/mount/unreadable" << VAR(elapsed_ms) << VAR(mount_presses_);
        commitRecord(HopOutcome::NotMounted, now);
        return startDismount(actuator, ReplanRequested { .still_on_tower = false }, now);
    }
    return remount(actuator, "zipline/mount/window_expired", now);
}

StageResult ZiplineRideMachine::remount(IZiplineActuator& actuator, const char* reason, Clock::time_point now)
{
    const int64_t elapsed_ms = stageElapsedMs(now);
    if (mount_presses_ < kZiplineMountPressBudget && actuator.pressMount()) {
        ++mount_presses_;
        on_ground_hits_ = 0;
        LogWarn << "zipline/mount/repress" << VAR(reason) << VAR(elapsed_ms) << VAR(mount_presses_);
        enterStage(ZiplineStage::Mounting, now);
        return {};
    }
    LogWarn << "zipline/mount/unmounted" << VAR(reason) << VAR(elapsed_ms) << VAR(mount_presses_) << VAR(plan_.mount_spots.size());
    commitRecord(HopOutcome::NotMounted, now);
    enterStage(ZiplineStage::Idle, now);
    return NeedsReposition {};
}

void ZiplineRideMachine::markMountUnreachable(const ZiplineHopPlan& plan)
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

StageResult ZiplineRideMachine::tickOnTower(IZiplineActuator& actuator, Clock::time_point now)
{
    if (returning_) {
        target_ = plan_.mount;
        seed_elevation_deg_ = elevation_deg(origin_, target_);
    }
    else {
        origin_ = plan_.mount;
        target_ = plan_.landing;
        seed_elevation_deg_ = plan_.planned_elevation_deg;
    }
    resetStableHeading();
    yaw_response_ratio_.reset();
    aim_budget_ms_.reset();
    pitch_lowered_ = false;
    if (!actuator.resetPitchToMaximum()) {
        return failAim(actuator, "zipline/aim/pitch_reset_failed", now);
    }
    LogInfo << "zipline/on_tower" << VAR(returning_) << VAR(pitch_tier_) << VAR(seed_elevation_deg_) << VAR(aim_bias_deg_) << VAR(target_.x)
            << VAR(target_.y);
    enterStage(returning_ ? ZiplineStage::ReturnAiming : ZiplineStage::Aiming, now);
    return {};
}

std::optional<double> ZiplineRideMachine::settledHeading(const ZiplineObservation& obs)
{
    if (!obs.fix) {
        resetStableHeading();
        return std::nullopt;
    }
    const double heading = obs.fix->angle;
    const bool agrees = prev_heading_ && std::abs(NaviMath::normalizeAngle(heading - *prev_heading_)) <= kHeadingStableReadToleranceDeg;
    stable_heading_hits_ = agrees ? stable_heading_hits_ + 1 : 1;
    prev_heading_ = heading;
    if (stable_heading_hits_ < 2) {
        return std::nullopt;
    }
    return heading;
}

void ZiplineRideMachine::resetStableHeading()
{
    prev_heading_.reset();
    stable_heading_hits_ = 0;
    turn_pending_ = false;
    turn_sent_at_ = {};
}

void ZiplineRideMachine::noteTurnResponse(double heading)
{
    if (!turn_pending_ || std::abs(turn_cmd_deg_) < kZiplineAimResponseMinCmdDeg) {
        return;
    }
    const double achieved = NaviMath::normalizeAngle(heading - turn_ref_heading_);
    if (achieved * turn_cmd_deg_ <= 0.0) {
        return;
    }
    yaw_response_ratio_ = std::clamp(achieved / turn_cmd_deg_, 1.0, kZiplineAimResponseRatioMax);
}

std::optional<double> ZiplineRideMachine::issueYawTurn(IZiplineActuator& actuator, double heading, double residual, Clock::time_point now)
{
    const std::optional<double> issued = actuator.turnYaw(residual / yaw_response_ratio_.value_or(1.0));
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

StageResult ZiplineRideMachine::tickAiming(const ZiplineObservation& obs, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    const int32_t budget_ms = aim_budget_ms_.value_or(kZiplineAimHeadingTimeoutMs);
    if (stageElapsedMs(now) > budget_ms) {
        LogWarn << "zipline/aim/timeout" << VAR(returning_) << VAR(turn_pending_) << VAR(stable_heading_hits_) << VAR(budget_ms);
        return failAim(actuator, "zipline/aim/timeout", now);
    }
    if (turn_pending_ && ElapsedMs(turn_sent_at_, now) < kWaitAfterFirstTurnMs) {
        return {};
    }
    const std::optional<double> settled = settledHeading(obs);
    if (!settled) {
        return {};
    }
    const double heading = *settled;
    noteTurnResponse(heading);
    turn_pending_ = false;

    const double bias = returning_ ? 0.0 : aim_bias_deg_;
    const bool sweep_pending = bias != 0.0 && !pitch_lowered_;
    const double target_heading = NaviMath::calcTargetRotation(obs.fix->x, obs.fix->y, target_.x, target_.y) + bias
                                  + (sweep_pending ? std::copysign(kZiplineAimSweepLeadDeg, bias) : 0.0);
    const double residual = NaviMath::normalizeAngle(target_heading - heading);
    if (!aim_budget_ms_) {
        const int64_t elapsed_ms = stageElapsedMs(now);
        aim_budget_ms_ = std::max(kZiplineAimHeadingTimeoutMs, static_cast<int32_t>(elapsed_ms) + turnBudgetMs(residual));
        LogInfo << "zipline/aim/budget" << VAR(residual) << VAR(elapsed_ms) << VAR(*aim_budget_ms_);
    }
    if (std::abs(residual) > kZiplineAimToleranceDeg) {
        const std::optional<double> issued = issueYawTurn(actuator, heading, residual, now);
        if (!issued) {
            return failAim(actuator, "zipline/aim/turn_rejected", now);
        }
        const double response_ratio = yaw_response_ratio_.value_or(1.0);
        LogInfo << "zipline/aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued) << VAR(response_ratio)
                << VAR(pitch_lowered_);
        return {};
    }

    // 镜头此刻在俯仰上限, 从那里开环调到目标角
    const double pitch_target = pitch_target_for_attempt(seed_elevation_deg_, pitch_tier_);
    if (!pitch_lowered_) {
        const double pitch_delta = pitch_target - kZiplinePitchMaximumElevationDeg;
        if (std::abs(pitch_delta) >= 1.0) {
            if (!actuator.turnPitch(pitch_delta)) {
                return failAim(actuator, "zipline/aim/pitch_rejected", now);
            }
            actuator.wait(kWaitAfterFirstTurnMs);
        }
        pitch_lowered_ = true;
        if (sweep_pending) {
            LogInfo << "zipline/aim/sweep" << VAR(target_heading) << VAR(heading) << VAR(bias) << VAR(pitch_target);
            // 横扫是新一段闭环, 重新计时
            stage_entered_at_ = Clock::now();
            aim_budget_ms_.reset();
            resetStableHeading();
            return {};
        }
    }
    actuator.fireLaunch();
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
    enterStage(ZiplineStage::Fired, fired_at);
    return {};
}

// 滑行中小地图隐藏, 定位连断就是滑出去了; 空响时人还在上索架上, 小地图照常显示
StageResult ZiplineRideMachine::tickFired(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    const int64_t elapsed_ms = stageElapsedMs(now);
    if (elapsed_ms < kZiplineLaunchSettleMs) {
        return {};
    }
    if (obs.fix) {
        miss_streak_ = 0;
        const double moved = launch_fix_ ? distance_wu(*obs.fix, *launch_fix_) : kZiplineMountMinMoveWu;
        if (moved >= kZiplineMountMinMoveWu) {
            riding_entered_ = true;
            last_fix_ = obs.fix;
            settle_hits_ = 0;
            LogInfo << "zipline/fired/moved" << VAR(moved) << VAR(elapsed_ms);
            enterStage(ZiplineStage::Landed, now);
            return {};
        }
        if (elapsed_ms > kZiplineLaunchConfirmMs) {
            last_fix_ = obs.fix;
            LogWarn << "zipline/fired/no_launch" << VAR(moved) << VAR(elapsed_ms) << VAR(pitch_tier_) << VAR(aim_bias_deg_);
            return classify(observer, actuator, now);
        }
        return {};
    }
    if (++miss_streak_ >= kZiplineRideLostFixes) {
        riding_entered_ = true;
        // 跟踪器留着起点旧位置会把真落点当远跳拒掉
        observer.resetTracking();
        LogInfo << "zipline/fired/riding" << VAR(elapsed_ms);
        enterStage(ZiplineStage::Riding, now);
        return {};
    }
    if (elapsed_ms > kZiplineRideTimeoutMs) {
        last_fix_.reset();
        return classify(observer, actuator, now);
    }
    return {};
}

StageResult ZiplineRideMachine::tickRiding(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (obs.fix && !minimap_back_at_) {
        minimap_back_at_ = now;
        LogInfo << "zipline/riding/minimap_back" << VAR(obs.fix->x) << VAR(obs.fix->y) << VAR_AS(StageElapsedMs(now), stageElapsedMs(now));
    }
    if (minimap_back_at_ && ElapsedMs(*minimap_back_at_, now) < kZiplineLandingSettleMs) {
        // 定位器会把冷却期内的位置记成跟踪锚点
        observer.resetTracking();
        return {};
    }
    if (obs.fix) {
        last_fix_ = obs.fix;
        settle_hits_ = 0;
        LogInfo << "zipline/riding/fix_back" << VAR(obs.fix->x) << VAR(obs.fix->y) << VAR_AS(StageElapsedMs(now), stageElapsedMs(now));
        enterStage(ZiplineStage::Landed, now);
        return {};
    }
    if (stageElapsedMs(now) > kZiplineRideTimeoutMs) {
        LogWarn << "zipline/riding/timeout" << VAR(kZiplineRideTimeoutMs);
        last_fix_.reset();
        return classify(observer, actuator, now);
    }
    return {};
}

StageResult ZiplineRideMachine::tickLanded(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (obs.fix) {
        settle_hits_ = last_fix_ && distance_wu(*obs.fix, *last_fix_) < kZiplineSettleMoveWu ? settle_hits_ + 1 : 1;
        last_fix_ = obs.fix;
        if (settle_hits_ >= kZiplineSettleFixes) {
            return classify(observer, actuator, now);
        }
    }
    else {
        settle_hits_ = 0;
    }
    if (stageElapsedMs(now) > kZiplineUnknownTimeoutMs) {
        LogWarn << "zipline/landed/unsettled" << VAR(settle_hits_) << VAR(last_fix_.has_value());
        last_fix_.reset();
        return classify(observer, actuator, now);
    }
    return {};
}

// 下索会沿按键时的朝向带出一段动量, 所以先摆朝向; 这一步失败也照常下索
StageResult ZiplineRideMachine::tickExitAiming(const ZiplineObservation& obs, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (stageElapsedMs(now) > exit_aim_budget_ms_) {
        LogWarn << "zipline/exit_aim/timeout" << VAR_AS(StageElapsedMs(now), stageElapsedMs(now)) << VAR(exit_aim_budget_ms_)
                << VAR(turn_pending_) << VAR(stable_heading_hits_);
        return leaveTowerAfterAim(actuator, now);
    }
    if (turn_pending_ && ElapsedMs(turn_sent_at_, now) < kWaitAfterFirstTurnMs) {
        return {};
    }
    const std::optional<double> settled = settledHeading(obs);
    if (!settled) {
        return {};
    }
    const double heading = *settled;
    noteTurnResponse(heading);
    turn_pending_ = false;

    const double target_heading = NaviMath::calcTargetRotation(obs.fix->x, obs.fix->y, exit_aim_.x, exit_aim_.y);
    const double residual = NaviMath::normalizeAngle(target_heading - heading);
    if (std::abs(residual) > kZiplineExitAimToleranceDeg) {
        const std::optional<double> issued = issueYawTurn(actuator, heading, residual, now);
        if (!issued) {
            LogWarn << "zipline/exit_aim/turn_rejected" << VAR(target_heading) << VAR(heading) << VAR(residual);
            return leaveTowerAfterAim(actuator, now);
        }
        const double response_ratio = yaw_response_ratio_.value_or(1.0);
        LogInfo << "zipline/exit_aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued) << VAR(response_ratio);
        return {};
    }
    LogInfo << "zipline/exit_aim/aligned" << VAR(target_heading) << VAR(heading) << VAR(residual)
            << VAR_AS(StageElapsedMs(now), stageElapsedMs(now));
    return leaveTowerAfterAim(actuator, now);
}

double ZiplineRideMachine::residualToExitAim() const
{
    if (!last_fix_) {
        return 0.0;
    }
    const double target_heading = NaviMath::calcTargetRotation(last_fix_->x, last_fix_->y, exit_aim_.x, exit_aim_.y);
    return NaviMath::normalizeAngle(target_heading - last_fix_->angle);
}

int32_t ZiplineRideMachine::turnBudgetMs(double residual) const
{
    return kZiplineTurnBudgetBaseMs + static_cast<int32_t>(std::lround(std::abs(residual) * kZiplineTurnBudgetPerDegMs));
}

StageResult ZiplineRideMachine::startExitAim(const HopCompleted& done, Clock::time_point now)
{
    resetStableHeading();
    yaw_response_ratio_.reset();
    exit_aim_ = *plan_.exit_aim;
    exit_aim_budget_ms_ = turnBudgetMs(residualToExitAim());
    pending_exit_ = done;
    enterStage(ZiplineStage::ExitAiming, now);
    LogInfo << "zipline/exit_aim/begin" << VAR(exit_aim_.x) << VAR(exit_aim_.y) << VAR(exit_aim_budget_ms_)
            << VAR(last_fix_ ? last_fix_->angle : 0.0);
    return {};
}

StageResult ZiplineRideMachine::leaveTowerAfterAim(IZiplineActuator& actuator, Clock::time_point now)
{
    StageResult exit = std::move(pending_exit_);
    return startDismount(actuator, std::move(exit), now);
}

StageResult ZiplineRideMachine::classify(IZiplineObserver& observer, IZiplineActuator& actuator, Clock::time_point now)
{
    enterStage(ZiplineStage::Classified, now);
    ZiplineNodeRef reached;
    const LandingClass landing = ClassifyLanding(last_fix_, origin_, target_, knownNodes(), riding_entered_, &reached);
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
            commitRecord(HopOutcome::Completed, now);
            const HopCompleted done { .at = *last_fix_, .still_on_tower = plan_.chain_continues };
            if (plan_.chain_continues) {
                parked_on_ = plan_.landing;
                pending_exit_ = done;
                enterStage(ZiplineStage::Handoff, now);
                return handoff(now);
            }
            if (plan_.exit_aim) {
                return startExitAim(done, now);
            }
            return startDismount(actuator, done, now);
        }
        // 回程时 target_ 是上索架, 到了就是滑回来了
        const double wrong_bearing = bearing_deg(plan_.mount, origin_);
        record_.wrong_rope_bearings_deg.push_back(wrong_bearing);
        if (hop_retry_count_ < kZiplineHopRetryBudget) {
            ++hop_retry_count_;
            returning_ = false;
            pitch_tier_ = 0;
            aim_bias_deg_ = escalatedAimBiasDeg(wrong_bearing);
            LogInfo << "zipline/aim/bias_escalated" << VAR(wrong_bearing) << VAR(aim_bias_deg_) << VAR(hop_retry_count_);
            enterStage(ZiplineStage::OnTower, now);
            return {};
        }
        commitRecord(HopOutcome::WrongRope, now);
        return parkForReplan(plan_.mount, now);
    }
    case LandingClass::AtOther:
    case LandingClass::AtStrayTower: {
        if (!returning_) {
            if (landing == LandingClass::AtStrayTower) {
                discovered_towers_.push_back(reached);
            }
            const std::optional<ZiplineNodeRef> previous = previous_wrong_tower(record_.launches);
            if (previous && previous->sameTower(reached)) {
                hop_retry_count_ = kZiplineHopRetryBudget;
                LogInfo << "zipline/aim/same_wrong_tower" << VAR(reached.x) << VAR(reached.y) << VAR(record_.launches.size());
            }
            if (hop_retry_count_ >= kZiplineHopRetryBudget) {
                record_.wrong_rope_bearings_deg.push_back(bearing_deg(plan_.mount, reached));
                LogInfo << "zipline/aim/park_on_wrong_tower" << VAR(reached.x) << VAR(reached.y) << VAR(hop_retry_count_);
                commitRecord(HopOutcome::WrongRope, now);
                return parkForReplan(reached, now);
            }
            origin_ = reached;
            returning_ = true;
            pitch_tier_ = 0;
            enterStage(ZiplineStage::OnTower, now);
            return {};
        }
        commitRecord(HopOutcome::WrongRope, now);
        commitFailedReturn(HopOutcome::WrongRope, now);
        return parkForReplan(reached, now);
    }
    case LandingClass::AtOrigin: {
        if (!returning_ && std::abs(aim_bias_deg_) / 2.0 >= kZiplineAimBiasMinDeg) {
            aim_bias_deg_ /= 2.0;
            LogInfo << "zipline/aim/bias_retracted" << VAR(aim_bias_deg_) << VAR(pitch_tier_);
            enterStage(ZiplineStage::OnTower, now);
            return {};
        }
        if (pitch_tier_ + 1 < kZiplineLaunchAttempts) {
            ++pitch_tier_;
            enterStage(ZiplineStage::OnTower, now);
            return {};
        }
        if (returning_) {
            commitRecord(HopOutcome::WrongRope, now);
            commitFailedReturn(HopOutcome::NoLaunch, now);
            return parkForReplan(origin_, now);
        }
        // 留在架上等重规划, 新路线用不上这根架子时才下来
        commitRecord(HopOutcome::NoLaunch, now);
        return parkForReplan(plan_.mount, now);
    }
    case LandingClass::Unknown: {
        if (!unknown_deadline_) {
            unknown_deadline_ = now + std::chrono::milliseconds(kZiplineUnknownTimeoutMs);
            observer.resetTracking();
        }
        if (now < *unknown_deadline_) {
            settle_hits_ = 0;
            enterStage(ZiplineStage::Landed, now);
            return {};
        }
        commitRecord(HopOutcome::Lost, now);
        return startDismount(actuator, ChainAbandoned { "zipline/landing/lost" }, now);
    }
    case LandingClass::Pending:
        break;
    }
    return {};
}

// 下索键偶尔不生效, 人留在架上定位也稳, 所以还要读不到架上引导才算下来
StageResult ZiplineRideMachine::tickDismounting(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (obs.fix) {
        const bool same = dismount_stable_pos_ && distance_wu(*obs.fix, *dismount_stable_pos_) <= kZiplineRecoveryStableRadiusWu;
        dismount_stable_hits_ = same ? dismount_stable_hits_ + 1 : 1;
        dismount_stable_pos_ = obs.fix;
        if (dismount_stable_hits_ >= kZiplineRecoveryStableFixes && observer.checkMounted() != MountVerdict::OnTower) {
            enterStage(ZiplineStage::Handoff, now);
            return handoff(now);
        }
    }
    const int64_t elapsed_ms = stageElapsedMs(now);
    if (dismount_presses_ == 1 && elapsed_ms > 2 * kZiplineDismountTimeoutMs) {
        LogWarn << "zipline/dismount/repress" << VAR(elapsed_ms) << VAR(dismount_stable_hits_);
        actuator.dismount();
        ++dismount_presses_;
        return {};
    }
    if (elapsed_ms > 4 * kZiplineDismountTimeoutMs) {
        LogWarn << "zipline/dismount/stuck" << VAR(elapsed_ms) << VAR(dismount_presses_) << VAR(dismount_stable_hits_);
        pending_exit_ = ChainAbandoned { "zipline/dismount/stuck" };
        enterStage(ZiplineStage::Failed, now);
        return handoff(now);
    }
    return {};
}

StageResult ZiplineRideMachine::failAim(IZiplineActuator& actuator, const char* reason, Clock::time_point now)
{
    LogWarn << "zipline/aim/failed" << VAR(reason) << VAR(returning_) << VAR(pitch_tier_);
    commitRecord(HopOutcome::AimFailed, now);
    return startDismount(actuator, ChainAbandoned { reason }, now);
}

StageResult ZiplineRideMachine::startDismount(IZiplineActuator& actuator, StageResult exit, Clock::time_point now)
{
    pending_exit_ = std::move(exit);
    actuator.dismount();
    dismount_presses_ = 1;
    dismount_stable_pos_.reset();
    dismount_stable_hits_ = 0;
    enterStage(ZiplineStage::Dismounting, now);
    return {};
}

StageResult ZiplineRideMachine::parkForReplan(const ZiplineNodeRef& tower, Clock::time_point now)
{
    parked_on_ = tower;
    pending_exit_ = ReplanRequested { .still_on_tower = true, .on_tower = tower };
    enterStage(ZiplineStage::Handoff, now);
    return handoff(now);
}

StageResult ZiplineRideMachine::handoff(Clock::time_point now)
{
    LogInfo << "zipline/handoff" << VAR_AS(StageName(stage_), stage_name(stage_)) << VAR(pending_exit_.index())
            << VAR(parked_on_.has_value()) << VAR_AS(StageElapsedMs(now), stageElapsedMs(now));
    StageResult exit = std::move(pending_exit_);
    const std::optional<ZiplineNodeRef> parked = parked_on_;
    reset();
    parked_on_ = parked;
    return exit;
}

} // namespace mapnavigator
