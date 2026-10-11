#include "zipline_ride_machine.h"

#include <algorithm>
#include <cmath>
#include <limits>
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

    // 起滑后跟踪器仍停在上索点, 小地图隐藏帧会被跟踪出贴着上索点的假位置
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
    unknown_deadline_.reset();
    ResetStableHeading();
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

// 规划器只按每条记录的两端封索, 回程索不单独记就会被重规划再派回去
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
    on_ground_hits_ = verdict == MountVerdict::OnGround && settled ? on_ground_hits_ + 1 : 0;

    // 架上那几条操作引导只在人已经站上架子之后才出现, 而它逐帧能不能读出来随机位起落, 故读到一帧即认
    if (verdict == MountVerdict::OnTower) {
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
    }
    else {
        origin_ = plan_.mount;
        target_ = plan_.landing;
        seed_elevation_deg_ = plan_.planned_elevation_deg;
    }
    ResetStableHeading();
    pitch_lowered_ = false;
    // 俯仰读不回来, 每次发射前都先拉到上限, 从这个已知位置开环往下调
    if (!actuator.ResetPitchToMaximum()) {
        return FailAim(actuator, "zipline/aim/pitch_reset_failed", now);
    }
    LogInfo << "zipline/on_tower" << VAR(returning_) << VAR(pitch_tier_) << VAR(seed_elevation_deg_) << VAR(aim_bias_deg_) << VAR(target_.x)
            << VAR(target_.y);
    EnterStage(returning_ ? ZiplineStage::ReturnAiming : ZiplineStage::Aiming, now);
    return {};
}

// 闭环转向的共用部件: 一次只发一个后端批次, 等读数跟上并连续两帧一致才认这个朝向。
// 链条瞄准与离索朝向共用它, 区别只在对准之后做什么
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

std::optional<double> ZiplineRideMachine::IssueYawTurn(IZiplineActuator& actuator, double residual, Clock::time_point now)
{
    const std::optional<double> issued = actuator.TurnYaw(residual);
    if (!issued) {
        return std::nullopt;
    }
    turn_sent_at_ = now;
    turn_pending_ = true;
    stable_heading_hits_ = 0;
    prev_heading_.reset();
    return issued;
}

// 站在架子上瞄准。一次只发一个后端批次, 等朝向读数跟上并连着两帧一致再算剩余角。对准后俯仰开环、左键起滑
StageResult ZiplineRideMachine::TickAiming(const ZiplineObservation& obs, IZiplineActuator& actuator)
{
    const auto now = obs.at;
    if (StageElapsedMs(now) > kZiplineAimHeadingTimeoutMs) {
        LogWarn << "zipline/aim/timeout" << VAR(returning_) << VAR(turn_pending_) << VAR(stable_heading_hits_);
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

    turn_pending_ = false;

    const double bias = returning_ ? 0.0 : aim_bias_deg_;
    const bool sweep_pending = bias != 0.0 && !pitch_lowered_;
    const double target_heading = NaviMath::CalcTargetRotation(obs.fix->x, obs.fix->y, target_.x, target_.y) + bias
                                  + (sweep_pending ? std::copysign(kZiplineAimSweepLeadDeg, bias) : 0.0);
    const double residual = NaviMath::NormalizeAngle(target_heading - heading);
    if (std::abs(residual) > kZiplineAimToleranceDeg) {
        const std::optional<double> issued = IssueYawTurn(actuator, residual, now);
        if (!issued) {
            return FailAim(actuator, "zipline/aim/turn_rejected", now);
        }
        LogInfo << "zipline/aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued) << VAR(pitch_lowered_);
        return {};
    }

    // 对准了。镜头此刻在俯仰上限, 从那里开环调到这一档的目标角
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
    unknown_deadline_.reset();
    LogInfo << "zipline/fired" << VAR(returning_) << VAR(pitch_tier_) << VAR(heading) << VAR(target_heading) << VAR(bias)
            << VAR(pitch_target) << VAR(record_.launches.size());
    EnterStage(ZiplineStage::Fired, fired_at);
    return {};
}

// 起滑后: 滑行中小地图整个隐藏, 定位连着断掉就是滑出去了; 空响没滑走时人还站在上索架上, 小地图照常显示,
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
            LogWarn << "zipline/fired/no_launch" << VAR(moved) << VAR(elapsed_ms) << VAR(pitch_tier_) << VAR(aim_bias_deg_);
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

// 链尾落地后先把朝向摆到下一段的走路方向, 再按下索键。下索动作自身带出沿按键那刻朝向的一段
// 动量: 朝向偏多少, 动量就偏多少, 其中切向的偏差(约 90 度)最不利 —— 整段动量都在横向上, 人
// 横着离开要走的那条线; 沿走路方向的偏差(含 180 度整段反着走)只是多走或少走一段直线。
// 定位读不到、批次发不出去、预算耗满都照常下索: 这一步只省掉下索后那次转向, 不该让一条本来
// 走得通的路失败
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
    turn_pending_ = false;

    const double heading = *settled;
    const double target_heading = NaviMath::CalcTargetRotation(obs.fix->x, obs.fix->y, exit_aim_.x, exit_aim_.y);
    const double residual = NaviMath::NormalizeAngle(target_heading - heading);
    if (std::abs(residual) > kZiplineExitAimToleranceDeg) {
        const std::optional<double> issued = IssueYawTurn(actuator, residual, now);
        if (!issued) {
            LogWarn << "zipline/exit_aim/turn_rejected" << VAR(target_heading) << VAR(heading) << VAR(residual);
            return LeaveTowerAfterAim(actuator, now);
        }
        LogInfo << "zipline/exit_aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued);
        return {};
    }
    LogInfo << "zipline/exit_aim/aligned" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(StageElapsedMs(now));
    return LeaveTowerAfterAim(actuator, now);
}

// 目标点相对落地定位的初始残差。预算按它放大: 背对着走要十批左右, 固定预算在那种落地上不够用
double ZiplineRideMachine::ResidualToExitAim() const
{
    if (!last_fix_) {
        return 0.0;
    }
    const double target_heading = NaviMath::CalcTargetRotation(last_fix_->x, last_fix_->y, exit_aim_.x, exit_aim_.y);
    return NaviMath::NormalizeAngle(target_heading - last_fix_->angle);
}

int32_t ZiplineRideMachine::ExitAimBudgetMs(double residual) const
{
    return kZiplineExitAimBudgetBaseMs + static_cast<int32_t>(std::lround(std::abs(residual) * kZiplineExitAimBudgetPerDegMs));
}

StageResult ZiplineRideMachine::StartExitAim(const HopCompleted& done, Clock::time_point now)
{
    ResetStableHeading();
    exit_aim_ = *plan_.exit_aim;
    exit_aim_budget_ms_ = ExitAimBudgetMs(ResidualToExitAim());
    pending_exit_ = done;
    EnterStage(ZiplineStage::ExitAiming, now);
    LogInfo << "zipline/exit_aim/begin" << VAR(exit_aim_.x) << VAR(exit_aim_.y) << VAR(exit_aim_budget_ms_)
            << VAR(last_fix_ ? last_fix_->angle : 0.0);
    return {};
}

// 这一段的收场只有一种: 把这一跳的出口交回下索流程。对准与否只进日志
StageResult ZiplineRideMachine::LeaveTowerAfterAim(IZiplineActuator& actuator, Clock::time_point now)
{
    StageResult exit = std::move(pending_exit_);
    return StartDismount(actuator, std::move(exit), now);
}

// 决策表。到了就交回; 滑错了先滑回来再按预算重试; 没发出去先收偏置再换一档俯仰, 档用完先重新站一次上索点,
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
            // 链尾: 下索动作带出的动量沿按键那刻的朝向, 有可朝的点就先把它摆到下一段走路方向上
            if (plan_.exit_aim) {
                return StartExitAim(done, now);
            }
            return StartDismount(actuator, done, now);
        }
        // 滑回上索架了: 记下滑错的方向, 还有预算就换个瞄法再来, 没有就站在架子上等换路
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
        // 人根本没滑出去, 还站在上索架上。先下来再重规划要白付一次上索, 而重规划本身就会看新路线
        // 用不用得上脚下这根架子, 用不上时才下来
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

// 定位稳定、且读不到架上的操作引导才算下来了: 下索键偶尔不生效, 人留在架上时定位同样稳定。
// 久等下不来再按一次, 还下不来就是卡住了
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

StageResult ZiplineRideMachine::ParkForReplan(const ZiplineNodeRef& tower, Clock::time_point now)
{
    parked_on_ = tower;
    pending_exit_ = ReplanRequested { .still_on_tower = true, .on_tower = tower };
    EnterStage(ZiplineStage::Handoff, now);
    return Handoff(now);
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
