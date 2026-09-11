#include "zipline_ride_machine.h"

#include <algorithm>
#include <cmath>
#include <utility>

#include <MaaUtils/Logger.h>

#include "navi_config.h"
#include "navi_math.h"

namespace mapnavigator
{

namespace
{

// 一跳里第几次按左键该把镜头抬到多少度。俯仰读不回来, 下降索按规划仰角朝下瞄已经能发出去, 上升索
// 的正仰角还没核过, 所以第二次反着来, 第三次干脆不动俯仰——三次里必有一次踩在对的那一侧
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

// 回程的俯仰种子。两端都有世界坐标才算得出仰角, 现发现的架子没有, 就平着按
double ElevationDeg(const ZiplineNodeRef& from, const ZiplineNodeRef& to)
{
    if (!from.has_world || !to.has_world) {
        return 0.0;
    }
    const double run = std::hypot(to.world_x - from.world_x, to.world_z - from.world_z);
    return std::atan2(to.world_y - from.world_y, run) * 180.0 / kPi;
}

double DistanceWu(const NaviPosition& a, const NaviPosition& b)
{
    return std::hypot(a.x - b.x, a.y - b.y);
}

int64_t ElapsedMs(std::chrono::steady_clock::time_point from, std::chrono::steady_clock::time_point to)
{
    return std::chrono::duration_cast<std::chrono::milliseconds>(to - from).count();
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
    // 链中续跳与滑回原架时角色已在架上, 直接进入瞄准; 其余情况调用方刚发出上索按键, 需先确认已上架
    const bool standing = OnTower();
    // 上索点重新站过一次再回来的是同一跳, 记录接着写; 换了跳就把上一跳按下索了结
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
            << VAR(plan_.planned_elevation_deg) << VAR(plan_.siblings.size()) << VAR(plan_.chain_continues) << VAR(standing);
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

    const ZiplineObservation obs = observer.Observe(KnownNodes());
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
    case ZiplineStage::Dismounting:
        return TickDismounting(obs, actuator);
    default:
        return {};
    }
}

void ZiplineRideMachine::Dismount(IZiplineActuator& actuator)
{
    if (OnTower()) {
        actuator.Dismount();
    }
    // 发射过却被外面叫下来的, 只剩「重新站过一次还是没发出去」这一种情形: 按滑不动记, 重规划别再选它
    if (hop_open_) {
        CommitRecord(record_.launches.empty() ? HopOutcome::Dismounted : HopOutcome::NoLaunch, Clock::now());
    }
    Reset();
}

// Mounting 不计入在架上: 该阶段正在判定是否已上架, 计入会退回到按键即认定
bool ZiplineRideMachine::OnTower() const
{
    return parked_on_.has_value() || stage_ == ZiplineStage::OnTower || stage_ == ZiplineStage::Aiming || stage_ == ZiplineStage::Fired
           || stage_ == ZiplineStage::ReturnAiming;
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
    pitch_tier_ = 0;
    returning_ = false;
    hop_retry_count_ = 0;
    mount_presses_ = 0;
    on_tower_hits_ = 0;
    on_ground_hits_ = 0;
    discovered_towers_.clear();
    parked_on_.reset();
    launch_fix_.reset();
    last_fix_.reset();
    miss_streak_ = 0;
    settle_hits_ = 0;
    riding_entered_ = false;
    unknown_deadline_.reset();
    yaw_gain_.reset();
    prev_heading_.reset();
    stable_heading_hits_ = 0;
    turn_pending_ = false;
    turn_ref_heading_ = 0.0;
    turn_cmd_deg_ = 0.0;
    turn_sent_at_ = {};
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

// 分类和落地搜索先验用的全部已知节点: 这一跳两端、同架其它索的落点、途中发现的架子、账本里的架子。
// 起点还额外按起滑时的实际定位放一份, 架子台面比节点像素大, 站偏一点也得认出是原地
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

// 上一次滑错的那根索跟规划的方向差得不多时, 瞄准往另一侧让半个容差, 别再挂上同一根
double ZiplineRideMachine::AimBiasDeg() const
{
    if (record_.wrong_rope_bearings_deg.empty()) {
        return 0.0;
    }
    const double planned = BearingDeg(plan_.mount, plan_.landing);
    const double delta = NaviMath::NormalizeAngle(record_.wrong_rope_bearings_deg.back() - planned);
    if (delta == 0.0 || std::abs(delta) >= 2.0 * kZiplineAimToleranceDeg) {
        return 0.0;
    }
    return delta > 0.0 ? -kZiplineAimToleranceDeg / 2.0 : kZiplineAimToleranceDeg / 2.0;
}

// 上索按键发出后, 先确认已上架再放开俯仰与左键。两个判定各需连续若干帧一致才落定。位移只作否决用:
// 角色被架子锁住时无法移动, 故能移动即判定在地面, 零位移本身是二义的
StageResult ZiplineRideMachine::TickMounting(const ZiplineObservation& obs, IZiplineObserver& observer, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    const int64_t elapsed_ms = StageElapsedMs(now);
    const bool walking = obs.fix && last_fix_ && DistanceWu(*obs.fix, *last_fix_) >= kZiplineMountMinMoveWu;
    if (obs.fix) {
        last_fix_ = obs.fix;
    }
    const MountVerdict verdict = walking ? MountVerdict::OnGround : observer.CheckMounted();
    // settle 之内的地面读数不予采信: 按钮尚未收起时, 角色可能正在上架过程中。故这段时间内的读数
    // 一律不计入连续帧数, 重按所依据的若干帧全部取自窗口之后
    const bool settled = elapsed_ms >= kZiplineMountSettleMs;
    on_tower_hits_ = verdict == MountVerdict::OnTower ? on_tower_hits_ + 1 : 0;
    on_ground_hits_ = verdict == MountVerdict::OnGround && settled ? on_ground_hits_ + 1 : 0;

    if (on_tower_hits_ >= kZiplineMountOnTowerFixes) {
        LogInfo << "zipline/mount/confirmed" << VAR(elapsed_ms) << VAR(mount_presses_);
        EnterStage(ZiplineStage::OnTower, now);
        return {};
    }
    // 余速未停时不重按: 带着惯性发出的交互正是这一跳落空的成因, 此时重按同样不会生效。一路都在动
    // 说明人没被架子锁住, 窗口耗满就交回导航换站位 —— 这个相位外头没有看门狗, 等不到别人来收场
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
    // 两个信号都未命中在窗口内只当过渡态, 等窗口耗满再判定
    if (elapsed_ms <= kZiplineMountWindowMs) {
        return {};
    }
    // 窗口耗满时架上一侧的连续读数仍在累计: 等它满足或中断, 避免在上架完成的瞬间重按上索键
    if (verdict == MountVerdict::OnTower) {
        return {};
    }
    // 窗口耗满、两个信号都未命中且无位移: 按「已被架子锁住而提示漏读」处理, 先发下索键回到可判定的地面态
    // 再重规划, 避免在位置未定的状态下继续瞄准和发射
    if (verdict == MountVerdict::Unclear) {
        LogWarn << "zipline/mount/unreadable" << VAR(elapsed_ms) << VAR(mount_presses_);
        CommitRecord(HopOutcome::NotMounted, now);
        return StartDismount(actuator, ReplanRequested { .still_on_tower = false }, now);
    }
    return Remount(actuator, "zipline/mount/window_expired", now);
}

// 判定未上架后的第一级处置是重按上索键。两道防护避免对已上架的角色重按: 地面态须由 InWorld 命中,
// 以及识别不到架子的交互提示时不发按键。预算用尽仍未上架才交回导航, 由其调整站位后重来
StageResult ZiplineRideMachine::Remount(IZiplineActuator& actuator, const char* reason, Clock::time_point now)
{
    // elapsed_ms 是这次按键到判定未上架的实际耗时, settle 与窗口两个时限按它核准
    const int64_t elapsed_ms = StageElapsedMs(now);
    if (mount_presses_ < kZiplineMountPressBudget && actuator.PressMount()) {
        ++mount_presses_;
        on_tower_hits_ = 0;
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

// 这根架子的站位全试过了, 一次提示都没出来。记一笔让重规划别再拿它当上索点; 当落点不受影响
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
        aim_bias_deg_ = 0.0;
    }
    else {
        origin_ = plan_.mount;
        target_ = plan_.landing;
        seed_elevation_deg_ = plan_.planned_elevation_deg;
        aim_bias_deg_ = AimBiasDeg();
    }
    yaw_gain_.reset();
    prev_heading_.reset();
    stable_heading_hits_ = 0;
    turn_pending_ = false;
    // 俯仰读不回来, 每次发射前都先拉到上限, 从这个已知位置开环往下调
    if (!actuator.ResetPitchToMaximum()) {
        return FailAim(actuator, "zipline/aim/pitch_reset_failed", now);
    }
    LogInfo << "zipline/on_tower" << VAR(returning_) << VAR(pitch_tier_) << VAR(seed_elevation_deg_) << VAR(aim_bias_deg_) << VAR(target_.x)
            << VAR(target_.y);
    EnterStage(returning_ ? ZiplineStage::ReturnAiming : ZiplineStage::Aiming, now);
    return {};
}

// 站在架子上瞄准。一次只发一个后端批次, 等朝向读数跟上并连着两帧一致再算剩余角; 第一批转完
// 顺手量一次「发了多少转了多少」, 后面的 yaw 和俯仰都按这个增益缩放。对准后俯仰开环、左键起滑
StageResult ZiplineRideMachine::TickAiming(const ZiplineObservation& obs, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (StageElapsedMs(now) > kZiplineAimHeadingTimeoutMs) {
        LogWarn << "zipline/aim/timeout" << VAR(returning_) << VAR(turn_pending_) << VAR(stable_heading_hits_)
                << VAR(yaw_gain_.value_or(1.0));
        return FailAim(actuator, "zipline/aim/timeout", now);
    }
    if (turn_pending_ && ElapsedMs(turn_sent_at_, now) < kWaitAfterFirstTurnMs) {
        return {};
    }
    if (!obs.fix) {
        stable_heading_hits_ = 0;
        prev_heading_.reset();
        return {};
    }
    const double heading = obs.fix->angle;
    const bool agrees = prev_heading_ && std::abs(NaviMath::NormalizeAngle(heading - *prev_heading_)) <= kHeadingStableReadToleranceDeg;
    stable_heading_hits_ = agrees ? stable_heading_hits_ + 1 : 1;
    prev_heading_ = heading;
    if (stable_heading_hits_ < 2) {
        return {};
    }

    if (turn_pending_) {
        turn_pending_ = false;
        const double achieved = NaviMath::NormalizeAngle(heading - turn_ref_heading_);
        if (std::abs(turn_cmd_deg_) >= kZiplineAimGainMinTurnDeg && achieved * turn_cmd_deg_ > 0.0) {
            yaw_gain_ = std::clamp(achieved / turn_cmd_deg_, kZiplineAimGainMin, kZiplineAimGainMax);
        }
        LogInfo << "zipline/aim/turned" << VAR(turn_cmd_deg_) << VAR(achieved) << VAR(yaw_gain_.value_or(1.0));
    }
    const double gain = yaw_gain_.value_or(1.0);

    const double target_heading = NaviMath::CalcTargetRotation(obs.fix->x, obs.fix->y, target_.x, target_.y) + aim_bias_deg_;
    const double residual = NaviMath::NormalizeAngle(target_heading - heading);
    if (std::abs(residual) > kZiplineAimToleranceDeg) {
        const std::optional<double> issued = actuator.TurnYaw(residual / gain);
        if (!issued) {
            return FailAim(actuator, "zipline/aim/turn_rejected", now);
        }
        LogInfo << "zipline/aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued) << VAR(gain);
        turn_ref_heading_ = heading;
        turn_cmd_deg_ = *issued;
        turn_sent_at_ = now;
        turn_pending_ = true;
        stable_heading_hits_ = 0;
        prev_heading_.reset();
        return {};
    }

    // 对准了。镜头此刻在俯仰上限, 从那里开环调到这一档的目标角
    const double pitch_target = PitchTargetForAttempt(seed_elevation_deg_, pitch_tier_);
    const double pitch_delta = pitch_target - kZiplinePitchMaximumElevationDeg;
    if (std::abs(pitch_delta) >= 1.0) {
        if (!actuator.TurnPitch(pitch_delta / gain)) {
            return FailAim(actuator, "zipline/aim/pitch_rejected", now);
        }
        actuator.Wait(kWaitAfterFirstTurnMs);
    }
    actuator.FireLaunch();
    const auto fired_at = Clock::now();
    record_.launches.push_back(ZiplineLaunch {
        .fired_at = fired_at,
        .heading_deg = heading,
        .pitch_tier = pitch_tier_,
        .aim_bias_deg = aim_bias_deg_,
    });
    launch_fix_ = obs.fix;
    last_fix_.reset();
    miss_streak_ = 0;
    settle_hits_ = 0;
    riding_entered_ = false;
    unknown_deadline_.reset();
    LogInfo << "zipline/fired" << VAR(returning_) << VAR(pitch_tier_) << VAR(heading) << VAR(target_heading) << VAR(pitch_target)
            << VAR(gain) << VAR(record_.launches.size());
    EnterStage(ZiplineStage::Fired, fired_at);
    return {};
}

// 起滑后: 滑行中小地图整个隐藏, 定位连着断掉就是滑出去了; 站在架子上没滑走时跟踪不会断,
// 过了确认时间定位还在起点就是空响
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
            LogWarn << "zipline/fired/no_launch" << VAR(moved) << VAR(elapsed_ms) << VAR(pitch_tier_);
            return Classify(observer, actuator, now);
        }
        return {};
    }
    if (++miss_streak_ >= kZiplineRideLostFixes) {
        riding_entered_ = true;
        // 落地帧离起点一整跨, 起点的旧位置留在跟踪器里会把真落点当远跳拒掉; 清掉, 落地走冷启动
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

// 定位回来了, 连着几帧几乎不动才算停稳。一直稳不下来就是定位对不上, 交给分类当 Unknown 处理
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

// 决策表。到了就交回; 滑错了先滑回来再按预算重试; 没发出去换一档俯仰, 档用完先重新站一次上索点,
// 再不行这根索就是滑不动, 人留在架子上等重规划; 定位对不上给一次冷启动的机会, 超时就丢
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
            return StartDismount(actuator, done, now);
        }
        // 滑回上索架了: 记下滑错的方向, 还有预算就换个瞄法再来, 没有就站在架子上等换路
        record_.wrong_rope_bearings_deg.push_back(BearingDeg(plan_.mount, origin_));
        if (hop_retry_count_ < kZiplineHopRetryBudget) {
            ++hop_retry_count_;
            returning_ = false;
            pitch_tier_ = 0;
            EnterStage(ZiplineStage::OnTower, now);
            return {};
        }
        CommitRecord(HopOutcome::WrongRope, now);
        parked_on_ = plan_.mount;
        pending_exit_ = ReplanRequested { .still_on_tower = true, .on_tower = plan_.mount };
        EnterStage(ZiplineStage::Handoff, now);
        return Handoff(now);
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
        return StartDismount(actuator, ChainAbandoned { "zipline/return/off_target" }, now);
    }
    case LandingClass::AtOrigin: {
        if (pitch_tier_ + 1 < kZiplineLaunchAttempts) {
            ++pitch_tier_;
            EnterStage(ZiplineStage::OnTower, now);
            return {};
        }
        if (returning_) {
            CommitRecord(HopOutcome::WrongRope, now);
            return StartDismount(actuator, ChainAbandoned { "zipline/return/no_launch" }, now);
        }
        // 人根本没滑出去, 还站在上索架上。先下来再重规划要白付一次上索, 而重规划本身就会看新路线
        // 用不用得上脚下这根架子, 用不上时才下来
        CommitRecord(HopOutcome::NoLaunch, now);
        parked_on_ = plan_.mount;
        pending_exit_ = ReplanRequested { .still_on_tower = true, .on_tower = plan_.mount };
        EnterStage(ZiplineStage::Handoff, now);
        return Handoff(now);
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

// 下索键按出去就当下来了, 只等定位稳定。久等不稳再按一次, 还不稳就是卡住了
StageResult ZiplineRideMachine::TickDismounting(const ZiplineObservation& obs, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (obs.fix) {
        const bool same = dismount_stable_pos_ && DistanceWu(*obs.fix, *dismount_stable_pos_) <= kZiplineRecoveryStableRadiusWu;
        dismount_stable_hits_ = same ? dismount_stable_hits_ + 1 : 1;
        dismount_stable_pos_ = obs.fix;
        if (dismount_stable_hits_ >= kZiplineRecoveryStableFixes) {
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
    CommitRecord(HopOutcome::Dismounted, now);
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

// 交回导航。这一跳的一切都清掉, 只留账本和「人还站在哪根架子上」
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
