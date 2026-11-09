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
    if (attempt == 0) {
        return elevation_deg;
    }
    if (attempt == 1) {
        return -elevation_deg;
    }
    return 0.0;
}

double bearing_deg(const ZiplineNodeRef& from, const ZiplineNodeRef& to)
{
    return CalcTargetRotation(from.x, from.y, to.x, to.y);
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

std::optional<double> settled_heading(YawAim* aim, const ZiplineObservation& obs)
{
    if (!obs.fix) {
        RestartYawAim(aim);
        return std::nullopt;
    }
    const double heading = obs.fix->angle;
    const bool agrees = aim->prev_heading && std::abs(NormalizeAngle(heading - *aim->prev_heading)) <= kHeadingStableReadToleranceDeg;
    aim->stable_heading_hits = agrees ? aim->stable_heading_hits + 1 : 1;
    aim->prev_heading = heading;
    if (aim->stable_heading_hits < 2) {
        return std::nullopt;
    }
    return heading;
}

void note_turn_response(YawAim* aim, double heading)
{
    if (!aim->turn_pending || std::abs(aim->turn_cmd_deg) < kZiplineAimResponseMinCmdDeg) {
        return;
    }
    const double achieved = NormalizeAngle(heading - aim->turn_ref_heading);
    if (achieved * aim->turn_cmd_deg <= 0.0) {
        return;
    }
    aim->yaw_response_ratio = std::clamp(achieved / aim->turn_cmd_deg, 1.0, kZiplineAimResponseRatioMax);
}

void enter_stage(ZiplineRide* ride, ZiplineStage stage, std::chrono::steady_clock::time_point now)
{
    ride->stage = stage;
    ride->stage_entered_at = now;
    LogInfo << "zipline/stage" << VAR_AS(StageName(stage), stage_name(stage)) << VAR_AS(returning_, ride->returning)
            << VAR_AS(pitch_tier_, ride->pitch_tier) << VAR_AS(hop_retry_count_, ride->hop_retry_count);
}

int64_t stage_elapsed_ms(const ZiplineRide& ride, std::chrono::steady_clock::time_point now)
{
    return ElapsedMs(ride.stage_entered_at, now);
}

void commit_record(ZiplineRide* ride, HopOutcome outcome, std::chrono::steady_clock::time_point now)
{
    ride->record.outcome = outcome;
    ride->record.ended_at = now;
    ride->ledger.push_back(ride->record);
    ride->hop_open = false;
    LogInfo << "zipline/hop_recorded" << VAR(static_cast<int>(outcome)) << VAR_AS(record_.launches.size(), ride->record.launches.size())
            << VAR_AS(record_.wrong_rope_bearings_deg.size(), ride->record.wrong_rope_bearings_deg.size())
            << VAR_AS(ledger_.size(), ride->ledger.size());
}

// 规划器只按每条记录的两端封索, 回程索不单独记会被重规划再派回去
void commit_failed_return(ZiplineRide* ride, HopOutcome outcome, std::chrono::steady_clock::time_point now)
{
    ZiplineHopRecord record;
    record.plan.mount = ride->origin;
    record.plan.landing = ride->plan.mount;
    record.outcome = outcome;
    record.began_at = now;
    record.ended_at = now;
    ride->ledger.push_back(record);
    LogWarn << "zipline/return/failed" << VAR(static_cast<int>(outcome)) << VAR_AS(origin_.x, ride->origin.x)
            << VAR_AS(origin_.y, ride->origin.y) << VAR_AS(plan_.mount.x, ride->plan.mount.x) << VAR_AS(plan_.mount.y, ride->plan.mount.y)
            << VAR_AS(ledger_.size(), ride->ledger.size());
}

// 起点额外按起滑时的实际定位放一份, 台面比节点大, 站偏了也要认出是原地
std::vector<ZiplineNodeRef> known_nodes(const ZiplineRide& ride)
{
    std::vector<ZiplineNodeRef> known;
    if (ride.landing.launch_fix) {
        ZiplineNodeRef here = ride.origin;
        here.x = ride.landing.launch_fix->x;
        here.y = ride.landing.launch_fix->y;
        known.push_back(here);
    }
    known.push_back(ride.plan.mount);
    known.push_back(ride.plan.landing);
    known.insert(known.end(), ride.plan.siblings.begin(), ride.plan.siblings.end());
    known.insert(known.end(), ride.discovered_towers.begin(), ride.discovered_towers.end());
    for (const ZiplineHopRecord& record : ride.ledger) {
        known.push_back(record.plan.mount);
        known.push_back(record.plan.landing);
    }
    return known;
}

std::vector<double> neighbor_offsets_deg(const ZiplineRide& ride)
{
    std::vector<ZiplineNodeRef> nodes = ride.plan.siblings;
    nodes.insert(nodes.end(), ride.discovered_towers.begin(), ride.discovered_towers.end());
    for (const ZiplineLaunch& launch : ride.record.launches) {
        if (launch.reached) {
            nodes.push_back(*launch.reached);
        }
    }
    const double planned = bearing_deg(ride.plan.mount, ride.plan.landing);
    std::vector<double> offsets;
    for (const ZiplineNodeRef& node : nodes) {
        if (SameTower(node, ride.plan.mount) || SameTower(node, ride.plan.landing)) {
            continue;
        }
        offsets.push_back(NormalizeAngle(bearing_deg(ride.plan.mount, node) - planned));
    }
    return offsets;
}

double first_shot_aim_bias_deg(const ZiplineRide& ride)
{
    const std::vector<double> offsets = neighbor_offsets_deg(ride);
    const auto nearest = std::ranges::min_element(offsets, {}, [](double offset) { return std::abs(offset); });
    if (nearest == offsets.end() || *nearest == 0.0 || std::abs(*nearest) >= 2.0 * kZiplineAimToleranceDeg) {
        return 0.0;
    }
    const double away = *nearest > 0.0 ? -1.0 : 1.0;
    return away * std::min(kZiplineAimBiasStepDeg, away_side_cap_deg(offsets, away));
}

double escalated_aim_bias_deg(const ZiplineRide& ride, double wrong_bearing_deg)
{
    const double offset = NormalizeAngle(wrong_bearing_deg - bearing_deg(ride.plan.mount, ride.plan.landing));
    if (offset == 0.0) {
        return ride.aim_bias_deg;
    }
    const bool overshot = offset * ride.aim_bias_deg > 0.0;
    const double away = overshot ? std::copysign(1.0, ride.aim_bias_deg) : std::copysign(1.0, -offset);
    const double magnitude = overshot ? std::abs(ride.aim_bias_deg) / 2.0 : std::abs(ride.aim_bias_deg) + kZiplineAimBiasStepDeg;
    return away * std::min(magnitude, away_side_cap_deg(neighbor_offsets_deg(ride), away));
}

double residual_to_exit_aim(const ZiplineRide& ride)
{
    if (!ride.last_fix) {
        return 0.0;
    }
    const double target_heading = CalcTargetRotation(ride.last_fix->x, ride.last_fix->y, ride.exit_aim.x, ride.exit_aim.y);
    return NormalizeAngle(target_heading - ride.last_fix->angle);
}

StageResult handoff(ZiplineRide* ride, std::chrono::steady_clock::time_point now)
{
    LogInfo << "zipline/handoff" << VAR_AS(StageName(stage_), stage_name(ride->stage))
            << VAR_AS(pending_exit_.index(), static_cast<int>(ride->pending_exit.kind))
            << VAR_AS(parked_on_.has_value(), ride->parked_on.has_value()) << VAR_AS(StageElapsedMs(now), stage_elapsed_ms(*ride, now));
    StageResult exit = std::move(ride->pending_exit);
    const std::optional<ZiplineNodeRef> parked = ride->parked_on;
    ResetZiplineHop(ride);
    ride->parked_on = parked;
    return exit;
}

StageResult park_and_handoff(ZiplineRide* ride, const ZiplineNodeRef& tower, StageResult exit, std::chrono::steady_clock::time_point now)
{
    ride->parked_on = tower;
    ride->pending_exit = std::move(exit);
    enter_stage(ride, ZiplineStage::Handoff, now);
    return handoff(ride, now);
}

StageResult park_for_replan(ZiplineRide* ride, const ZiplineNodeRef& tower, std::chrono::steady_clock::time_point now)
{
    return park_and_handoff(
        ride,
        tower,
        StageResult {
            .kind = StageResultKind::ReplanRequested,
            .replan_requested = ReplanRequested { .still_on_tower = true, .on_tower = tower },
        },
        now);
}

StageResult start_dismount(ZiplineRide* ride, ZiplineIo* io, StageResult exit, std::chrono::steady_clock::time_point now)
{
    ride->pending_exit = std::move(exit);
    ZiplineIoDismount(io);
    ArmDismountWatch(&ride->dismount);
    enter_stage(ride, ZiplineStage::Dismounting, now);
    return {};
}

StageResult fail_aim(ZiplineRide* ride, ZiplineIo* io, const char* reason, std::chrono::steady_clock::time_point now)
{
    LogWarn << "zipline/aim/failed" << VAR(reason) << VAR_AS(returning_, ride->returning) << VAR_AS(pitch_tier_, ride->pitch_tier);
    commit_record(ride, HopOutcome::AimFailed, now);
    return start_dismount(
        ride,
        io,
        StageResult { .kind = StageResultKind::ChainAbandoned, .chain_abandoned = ChainAbandoned { reason } },
        now);
}

StageResult leave_tower_after_aim(ZiplineRide* ride, ZiplineIo* io, std::chrono::steady_clock::time_point now)
{
    StageResult exit = std::move(ride->pending_exit);
    return start_dismount(ride, io, std::move(exit), now);
}

StageResult start_exit_aim(ZiplineRide* ride, const HopCompleted& done, std::chrono::steady_clock::time_point now)
{
    ride->aim = {};
    ride->exit_aim = *ride->plan.exit_aim;
    ride->exit_aim_budget_ms = YawTurnBudgetMs(residual_to_exit_aim(*ride));
    ride->pending_exit = StageResult { .kind = StageResultKind::HopCompleted, .hop_completed = done };
    enter_stage(ride, ZiplineStage::ExitAiming, now);
    LogInfo << "zipline/exit_aim/begin" << VAR_AS(exit_aim_.x, ride->exit_aim.x) << VAR_AS(exit_aim_.y, ride->exit_aim.y)
            << VAR_AS(exit_aim_budget_ms_, ride->exit_aim_budget_ms)
            << VAR_AS(last_fix_ ? last_fix_->angle : 0.0, ride->last_fix ? ride->last_fix->angle : 0.0);
    return {};
}

void stamp_launch(ZiplineRide* ride, const ZiplineNodeRef& reached, LandingClass landing)
{
    if (ride->record.launches.empty()) {
        return;
    }
    ZiplineLaunch& launch = ride->record.launches.back();
    launch.result = landing;
    launch.stopped_at = ride->last_fix;
    if (landing != LandingClass::Unknown) {
        launch.reached = reached;
    }
}

StageResult on_at_target(ZiplineRide* ride, ZiplineIo* io, std::chrono::steady_clock::time_point now)
{
    if (!ride->returning) {
        commit_record(ride, HopOutcome::Completed, now);
        const HopCompleted done { .at = *ride->last_fix, .still_on_tower = ride->plan.chain_continues };
        if (ride->plan.chain_continues) {
            return park_and_handoff(
                ride,
                ride->plan.landing,
                StageResult { .kind = StageResultKind::HopCompleted, .hop_completed = done },
                now);
        }
        if (ride->plan.exit_aim) {
            return start_exit_aim(ride, done, now);
        }
        return start_dismount(ride, io, StageResult { .kind = StageResultKind::HopCompleted, .hop_completed = done }, now);
    }
    // 回程时 target 是上索架, 到了就是滑回来了
    const double wrong_bearing = bearing_deg(ride->plan.mount, ride->origin);
    ride->record.wrong_rope_bearings_deg.push_back(wrong_bearing);
    ++ride->hop_retry_count;
    ride->returning = false;
    ride->pitch_tier = 0;
    ride->aim_bias_deg = escalated_aim_bias_deg(*ride, wrong_bearing);
    LogInfo << "zipline/aim/bias_escalated" << VAR(wrong_bearing) << VAR_AS(aim_bias_deg_, ride->aim_bias_deg)
            << VAR_AS(hop_retry_count_, ride->hop_retry_count);
    enter_stage(ride, ZiplineStage::OnTower, now);
    return {};
}

StageResult
    on_wrong_tower(ZiplineRide* ride, const ZiplineNodeRef& reached, LandingClass landing, std::chrono::steady_clock::time_point now)
{
    if (!ride->returning) {
        if (landing == LandingClass::AtStrayTower) {
            ride->discovered_towers.push_back(reached);
        }
        const std::optional<ZiplineNodeRef> previous = previous_wrong_tower(ride->record.launches);
        if (previous && SameTower(*previous, reached)) {
            ride->hop_retry_count = kZiplineHopRetryBudget;
            LogInfo << "zipline/aim/same_wrong_tower" << VAR(reached.x) << VAR(reached.y)
                    << VAR_AS(record_.launches.size(), ride->record.launches.size());
        }
        if (ride->hop_retry_count >= kZiplineHopRetryBudget) {
            ride->record.wrong_rope_bearings_deg.push_back(bearing_deg(ride->plan.mount, reached));
            LogInfo << "zipline/aim/park_on_wrong_tower" << VAR(reached.x) << VAR(reached.y)
                    << VAR_AS(hop_retry_count_, ride->hop_retry_count);
            commit_record(ride, HopOutcome::WrongRope, now);
            return park_for_replan(ride, reached, now);
        }
        ride->origin = reached;
        ride->returning = true;
        ride->pitch_tier = 0;
        enter_stage(ride, ZiplineStage::OnTower, now);
        return {};
    }
    commit_record(ride, HopOutcome::WrongRope, now);
    commit_failed_return(ride, HopOutcome::WrongRope, now);
    return park_for_replan(ride, reached, now);
}

StageResult on_back_at_origin(ZiplineRide* ride, std::chrono::steady_clock::time_point now)
{
    if (!ride->returning && std::abs(ride->aim_bias_deg) / 2.0 >= kZiplineAimBiasMinDeg) {
        ride->aim_bias_deg /= 2.0;
        LogInfo << "zipline/aim/bias_retracted" << VAR_AS(aim_bias_deg_, ride->aim_bias_deg) << VAR_AS(pitch_tier_, ride->pitch_tier);
        enter_stage(ride, ZiplineStage::OnTower, now);
        return {};
    }
    if (ride->pitch_tier + 1 < kZiplineLaunchAttempts) {
        ++ride->pitch_tier;
        enter_stage(ride, ZiplineStage::OnTower, now);
        return {};
    }
    if (ride->returning) {
        commit_record(ride, HopOutcome::WrongRope, now);
        commit_failed_return(ride, HopOutcome::NoLaunch, now);
        return park_for_replan(ride, ride->origin, now);
    }
    // 留在架上等重规划, 新路线用不上这根架子时才下来
    commit_record(ride, HopOutcome::NoLaunch, now);
    return park_for_replan(ride, ride->plan.mount, now);
}

StageResult on_unknown(ZiplineRide* ride, ZiplineIo* io, std::chrono::steady_clock::time_point now)
{
    if (!ride->landing.unknown_deadline) {
        ride->landing.unknown_deadline = now + std::chrono::milliseconds(kZiplineUnknownTimeoutMs);
        ZiplineIoResetTracking(io);
    }
    if (now < *ride->landing.unknown_deadline) {
        ride->landing.settle_hits = 0;
        enter_stage(ride, ZiplineStage::Landed, now);
        return {};
    }
    commit_record(ride, HopOutcome::Lost, now);
    return start_dismount(
        ride,
        io,
        StageResult { .kind = StageResultKind::ChainAbandoned, .chain_abandoned = ChainAbandoned { "zipline/landing/lost" } },
        now);
}

StageResult classify(ZiplineRide* ride, ZiplineIo* io, std::chrono::steady_clock::time_point now)
{
    enter_stage(ride, ZiplineStage::Classified, now);
    ZiplineNodeRef reached;
    const LandingClass landing =
        ClassifyLanding(ride->last_fix, ride->origin, ride->target, known_nodes(*ride), ride->landing.riding_entered, &reached);
    stamp_launch(ride, reached, landing);
    LogInfo << "zipline/classified" << VAR(static_cast<int>(landing)) << VAR_AS(returning_, ride->returning)
            << VAR_AS(riding_entered_, ride->landing.riding_entered) << VAR_AS(pitch_tier_, ride->pitch_tier)
            << VAR_AS(hop_retry_count_, ride->hop_retry_count)
            << VAR_AS(last_fix_ ? last_fix_->x : 0.0, ride->last_fix ? ride->last_fix->x : 0.0)
            << VAR_AS(last_fix_ ? last_fix_->y : 0.0, ride->last_fix ? ride->last_fix->y : 0.0) << VAR(reached.x) << VAR(reached.y);

    switch (landing) {
    case LandingClass::AtTarget:
        return on_at_target(ride, io, now);
    case LandingClass::AtOther:
    case LandingClass::AtStrayTower:
        return on_wrong_tower(ride, reached, landing, now);
    case LandingClass::AtOrigin:
        return on_back_at_origin(ride, now);
    case LandingClass::Unknown:
        return on_unknown(ride, io, now);
    case LandingClass::Pending:
        break;
    }
    return {};
}

StageResult remount(ZiplineRide* ride, ZiplineIo* io, const char* reason, std::chrono::steady_clock::time_point now)
{
    const int64_t elapsed_ms = stage_elapsed_ms(*ride, now);
    bool pressed = false;
    if (ride->mount_presses < kZiplineMountPressBudget) {
        pressed = ZiplineIoPressMount(io);
    }
    if (pressed) {
        ++ride->mount_presses;
        ride->on_ground_hits = 0;
        LogWarn << "zipline/mount/repress" << VAR(reason) << VAR(elapsed_ms) << VAR_AS(mount_presses_, ride->mount_presses);
        enter_stage(ride, ZiplineStage::Mounting, now);
        return {};
    }
    LogWarn << "zipline/mount/unmounted" << VAR(reason) << VAR(elapsed_ms) << VAR_AS(mount_presses_, ride->mount_presses)
            << VAR_AS(plan_.mount_spots.size(), ride->plan.mount_spots.size());
    commit_record(ride, HopOutcome::NotMounted, now);
    enter_stage(ride, ZiplineStage::Idle, now);
    return StageResult { .kind = StageResultKind::NeedsReposition };
}

// 被架子锁住时无法移动, 所以能移动即在地面, 零位移本身说明不了什么
StageResult tick_mounting(ZiplineRide* ride, ZiplineIo* io, const ZiplineObservation& obs)
{
    const auto now = obs.at;
    const int64_t elapsed_ms = stage_elapsed_ms(*ride, now);
    const bool walking = obs.fix && ride->last_fix && distance_wu(*obs.fix, *ride->last_fix) >= kZiplineMountMinMoveWu;
    if (obs.fix) {
        ride->last_fix = obs.fix;
    }
    MountVerdict verdict = MountVerdict::OnGround;
    if (!walking) {
        verdict = ZiplineIoCheckMounted(io);
    }
    // settle 内按钮可能还没收起, 地面读数不计入连续帧
    const bool settled = elapsed_ms >= kZiplineMountSettleMs;
    ride->on_ground_hits = verdict == MountVerdict::OnGround && settled ? ride->on_ground_hits + 1 : 0;

    // 架上操作引导只在站上架子后出现, 但逐帧时有时无, 所以读到一帧即认
    if (verdict == MountVerdict::OnTower) {
        LogInfo << "zipline/mount/confirmed" << VAR(elapsed_ms) << VAR_AS(mount_presses_, ride->mount_presses);
        enter_stage(ride, ZiplineStage::OnTower, now);
        return {};
    }
    // 还在动时重按无效, 这个阶段外面没有看门狗, 窗口耗满必须自己交回导航
    if (walking) {
        if (elapsed_ms <= kZiplineMountWindowMs) {
            return {};
        }
        LogWarn << "zipline/mount/still_moving" << VAR(elapsed_ms) << VAR_AS(mount_presses_, ride->mount_presses);
        commit_record(ride, HopOutcome::NotMounted, now);
        enter_stage(ride, ZiplineStage::Idle, now);
        return StageResult { .kind = StageResultKind::NeedsReposition };
    }
    if (ride->on_ground_hits >= kZiplineMountOnGroundFixes) {
        return remount(ride, io, "zipline/mount/on_ground", now);
    }
    if (elapsed_ms <= kZiplineMountWindowMs) {
        return {};
    }
    // 判不清时按已上架但漏读提示处理, 先下索回到可判定的地面态再重规划
    if (verdict == MountVerdict::Unclear) {
        LogWarn << "zipline/mount/unreadable" << VAR(elapsed_ms) << VAR_AS(mount_presses_, ride->mount_presses);
        commit_record(ride, HopOutcome::NotMounted, now);
        return start_dismount(
            ride,
            io,
            StageResult {
                .kind = StageResultKind::ReplanRequested,
                .replan_requested = ReplanRequested { .still_on_tower = false },
            },
            now);
    }
    return remount(ride, io, "zipline/mount/window_expired", now);
}

StageResult tick_on_tower(ZiplineRide* ride, ZiplineIo* io, std::chrono::steady_clock::time_point now)
{
    if (ride->returning) {
        ride->target = ride->plan.mount;
        ride->seed_elevation_deg = CalcElevation(ride->origin, ride->target);
    }
    else {
        ride->origin = ride->plan.mount;
        ride->target = ride->plan.landing;
        ride->seed_elevation_deg = ride->plan.planned_elevation_deg;
    }
    ride->aim = {};
    ride->aim_budget_ms.reset();
    ride->pitch_lowered = false;
    if (!ZiplineIoResetPitchToMaximum(io)) {
        return fail_aim(ride, io, "zipline/aim/pitch_reset_failed", now);
    }
    LogInfo << "zipline/on_tower" << VAR_AS(returning_, ride->returning) << VAR_AS(pitch_tier_, ride->pitch_tier)
            << VAR_AS(seed_elevation_deg_, ride->seed_elevation_deg) << VAR_AS(aim_bias_deg_, ride->aim_bias_deg)
            << VAR_AS(target_.x, ride->target.x) << VAR_AS(target_.y, ride->target.y);
    enter_stage(ride, ride->returning ? ZiplineStage::ReturnAiming : ZiplineStage::Aiming, now);
    return {};
}

StageResult tick_aiming(ZiplineRide* ride, ZiplineIo* io, const ZiplineObservation& obs)
{
    const auto now = obs.at;
    const int32_t budget_ms = ride->aim_budget_ms.value_or(kZiplineAimHeadingTimeoutMs);
    if (stage_elapsed_ms(*ride, now) > budget_ms) {
        LogWarn << "zipline/aim/timeout" << VAR_AS(returning_, ride->returning) << VAR_AS(turn_pending_, ride->aim.turn_pending)
                << VAR_AS(stable_heading_hits_, ride->aim.stable_heading_hits) << VAR(budget_ms);
        return fail_aim(ride, io, "zipline/aim/timeout", now);
    }
    const std::optional<double> settled = PollYawAim(&ride->aim, obs, now);
    if (!settled) {
        return {};
    }
    const double heading = *settled;

    const double bias = ride->returning ? 0.0 : ride->aim_bias_deg;
    const bool sweep_pending = bias != 0.0 && !ride->pitch_lowered;
    const double target_heading = CalcTargetRotation(obs.fix->x, obs.fix->y, ride->target.x, ride->target.y) + bias
                                  + (sweep_pending ? std::copysign(kZiplineAimSweepLeadDeg, bias) : 0.0);
    const double residual = NormalizeAngle(target_heading - heading);
    if (!ride->aim_budget_ms) {
        const int64_t elapsed_ms = stage_elapsed_ms(*ride, now);
        ride->aim_budget_ms = std::max(kZiplineAimHeadingTimeoutMs, static_cast<int32_t>(elapsed_ms) + YawTurnBudgetMs(residual));
        LogInfo << "zipline/aim/budget" << VAR(residual) << VAR(elapsed_ms) << VAR_AS(*aim_budget_ms_, *ride->aim_budget_ms);
    }
    if (std::abs(residual) > kZiplineAimToleranceDeg) {
        const std::optional<double> issued = IssueYawTurn(&ride->aim, io, heading, residual, now);
        if (!issued) {
            return fail_aim(ride, io, "zipline/aim/turn_rejected", now);
        }
        const double response_ratio = YawResponseRatio(ride->aim);
        LogInfo << "zipline/aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued) << VAR(response_ratio)
                << VAR_AS(pitch_lowered_, ride->pitch_lowered);
        return {};
    }

    // 镜头此刻在俯仰上限, 从那里开环调到目标角
    const double pitch_target = pitch_target_for_attempt(ride->seed_elevation_deg, ride->pitch_tier);
    if (!ride->pitch_lowered) {
        if (!ZiplineIoLowerPitchTo(io, pitch_target)) {
            return fail_aim(ride, io, "zipline/aim/pitch_rejected", now);
        }
        ZiplineIoWait(io, kWaitAfterFirstTurnMs);
        ride->pitch_lowered = true;
        if (sweep_pending) {
            LogInfo << "zipline/aim/sweep" << VAR(target_heading) << VAR(heading) << VAR(bias) << VAR(pitch_target);
            // 横扫是新一段闭环, 重新计时
            ride->stage_entered_at = std::chrono::steady_clock::now();
            ride->aim_budget_ms.reset();
            RestartYawAim(&ride->aim);
            return {};
        }
    }
    ZiplineIoFireLaunch(io);
    const auto fired_at = std::chrono::steady_clock::now();
    ride->record.launches.push_back(
        ZiplineLaunch {
            .fired_at = fired_at,
            .heading_deg = heading,
            .pitch_tier = ride->pitch_tier,
            .aim_bias_deg = bias,
        });
    ArmLandingWatch(&ride->landing, obs.fix);
    ride->last_fix.reset();
    LogInfo << "zipline/fired" << VAR_AS(returning_, ride->returning) << VAR_AS(pitch_tier_, ride->pitch_tier) << VAR(heading)
            << VAR(target_heading) << VAR(bias) << VAR(pitch_target) << VAR_AS(record_.launches.size(), ride->record.launches.size());
    enter_stage(ride, ZiplineStage::Fired, fired_at);
    return {};
}

// 滑行中小地图隐藏, 定位连断就是滑出去了; 空响时人还在上索架上, 小地图照常显示
StageResult tick_fired(ZiplineRide* ride, ZiplineIo* io, const ZiplineObservation& obs)
{
    const auto now = obs.at;
    const int64_t elapsed_ms = stage_elapsed_ms(*ride, now);
    if (elapsed_ms < kZiplineLaunchSettleMs) {
        return {};
    }
    if (obs.fix) {
        ride->landing.miss_streak = 0;
        const double moved = ride->landing.launch_fix ? distance_wu(*obs.fix, *ride->landing.launch_fix) : kZiplineMountMinMoveWu;
        if (moved >= kZiplineMountMinMoveWu) {
            ride->landing.riding_entered = true;
            ride->last_fix = obs.fix;
            ride->landing.settle_hits = 0;
            LogInfo << "zipline/fired/moved" << VAR(moved) << VAR(elapsed_ms);
            enter_stage(ride, ZiplineStage::Landed, now);
            return {};
        }
        if (elapsed_ms > kZiplineLaunchConfirmMs) {
            ride->last_fix = obs.fix;
            LogWarn << "zipline/fired/no_launch" << VAR(moved) << VAR(elapsed_ms) << VAR_AS(pitch_tier_, ride->pitch_tier)
                    << VAR_AS(aim_bias_deg_, ride->aim_bias_deg);
            return classify(ride, io, now);
        }
        return {};
    }
    if (++ride->landing.miss_streak >= kZiplineRideLostFixes) {
        ride->landing.riding_entered = true;
        // 跟踪器留着起点旧位置会把真落点当远跳拒掉
        ZiplineIoResetTracking(io);
        LogInfo << "zipline/fired/riding" << VAR(elapsed_ms);
        enter_stage(ride, ZiplineStage::Riding, now);
        return {};
    }
    if (elapsed_ms > kZiplineRideTimeoutMs) {
        return classify(ride, io, now);
    }
    return {};
}

StageResult tick_riding(ZiplineRide* ride, ZiplineIo* io, const ZiplineObservation& obs)
{
    const auto now = obs.at;
    if (obs.fix && !ride->landing.minimap_back_at) {
        ride->landing.minimap_back_at = now;
        LogInfo << "zipline/riding/minimap_back" << VAR(obs.fix->x) << VAR(obs.fix->y)
                << VAR_AS(StageElapsedMs(now), stage_elapsed_ms(*ride, now));
    }
    if (ride->landing.minimap_back_at && ElapsedMs(*ride->landing.minimap_back_at, now) < kZiplineLandingSettleMs) {
        // 定位器会把冷却期内的位置记成跟踪锚点
        ZiplineIoResetTracking(io);
        return {};
    }
    if (obs.fix) {
        ride->last_fix = obs.fix;
        ride->landing.settle_hits = 0;
        LogInfo << "zipline/riding/fix_back" << VAR(obs.fix->x) << VAR(obs.fix->y)
                << VAR_AS(StageElapsedMs(now), stage_elapsed_ms(*ride, now));
        enter_stage(ride, ZiplineStage::Landed, now);
        return {};
    }
    if (stage_elapsed_ms(*ride, now) > kZiplineRideTimeoutMs) {
        LogWarn << "zipline/riding/timeout" << VAR(kZiplineRideTimeoutMs);
        return classify(ride, io, now);
    }
    return {};
}

StageResult tick_landed(ZiplineRide* ride, ZiplineIo* io, const ZiplineObservation& obs)
{
    const auto now = obs.at;
    if (obs.fix) {
        ride->landing.settle_hits =
            ride->last_fix && distance_wu(*obs.fix, *ride->last_fix) < kZiplineSettleMoveWu ? ride->landing.settle_hits + 1 : 1;
        ride->last_fix = obs.fix;
        if (ride->landing.settle_hits >= kZiplineSettleFixes) {
            return classify(ride, io, now);
        }
    }
    else {
        ride->landing.settle_hits = 0;
    }
    if (stage_elapsed_ms(*ride, now) > kZiplineUnknownTimeoutMs) {
        LogWarn << "zipline/landed/unsettled" << VAR_AS(settle_hits_, ride->landing.settle_hits)
                << VAR_AS(last_fix_.has_value(), ride->last_fix.has_value());
        ride->last_fix.reset();
        return classify(ride, io, now);
    }
    return {};
}

// 下索会沿按键时的朝向带出一段动量, 所以先摆朝向; 这一步失败也照常下索
StageResult tick_exit_aiming(ZiplineRide* ride, ZiplineIo* io, const ZiplineObservation& obs)
{
    const auto now = obs.at;
    if (stage_elapsed_ms(*ride, now) > ride->exit_aim_budget_ms) {
        LogWarn << "zipline/exit_aim/timeout" << VAR_AS(StageElapsedMs(now), stage_elapsed_ms(*ride, now))
                << VAR_AS(exit_aim_budget_ms_, ride->exit_aim_budget_ms) << VAR_AS(turn_pending_, ride->aim.turn_pending)
                << VAR_AS(stable_heading_hits_, ride->aim.stable_heading_hits);
        return leave_tower_after_aim(ride, io, now);
    }
    const std::optional<double> settled = PollYawAim(&ride->aim, obs, now);
    if (!settled) {
        return {};
    }
    const double heading = *settled;

    const double target_heading = CalcTargetRotation(obs.fix->x, obs.fix->y, ride->exit_aim.x, ride->exit_aim.y);
    const double residual = NormalizeAngle(target_heading - heading);
    if (std::abs(residual) > kZiplineExitAimToleranceDeg) {
        const std::optional<double> issued = IssueYawTurn(&ride->aim, io, heading, residual, now);
        if (!issued) {
            LogWarn << "zipline/exit_aim/turn_rejected" << VAR(target_heading) << VAR(heading) << VAR(residual);
            return leave_tower_after_aim(ride, io, now);
        }
        const double response_ratio = YawResponseRatio(ride->aim);
        LogInfo << "zipline/exit_aim/turn" << VAR(target_heading) << VAR(heading) << VAR(residual) << VAR(*issued) << VAR(response_ratio);
        return {};
    }
    LogInfo << "zipline/exit_aim/aligned" << VAR(target_heading) << VAR(heading) << VAR(residual)
            << VAR_AS(StageElapsedMs(now), stage_elapsed_ms(*ride, now));
    return leave_tower_after_aim(ride, io, now);
}

// 下索键偶尔不生效, 人留在架上定位也稳, 所以还要读不到架上引导才算下来
StageResult tick_dismounting(ZiplineRide* ride, ZiplineIo* io, const ZiplineObservation& obs)
{
    const auto now = obs.at;
    if (obs.fix) {
        const bool same = ride->dismount.stable_pos && distance_wu(*obs.fix, *ride->dismount.stable_pos) <= kZiplineRecoveryStableRadiusWu;
        ride->dismount.stable_hits = same ? ride->dismount.stable_hits + 1 : 1;
        ride->dismount.stable_pos = obs.fix;
        bool left_tower = false;
        if (ride->dismount.stable_hits >= kZiplineRecoveryStableFixes) {
            left_tower = ZiplineIoCheckMounted(io) != MountVerdict::OnTower;
        }
        if (left_tower) {
            enter_stage(ride, ZiplineStage::Handoff, now);
            return handoff(ride, now);
        }
    }
    const int64_t elapsed_ms = stage_elapsed_ms(*ride, now);
    if (ride->dismount.presses == 1 && elapsed_ms > 2 * kZiplineDismountTimeoutMs) {
        LogWarn << "zipline/dismount/repress" << VAR(elapsed_ms) << VAR_AS(dismount_stable_hits_, ride->dismount.stable_hits);
        ZiplineIoDismount(io);
        ++ride->dismount.presses;
        return {};
    }
    if (elapsed_ms > 4 * kZiplineDismountTimeoutMs) {
        LogWarn << "zipline/dismount/stuck" << VAR(elapsed_ms) << VAR_AS(dismount_presses_, ride->dismount.presses)
                << VAR_AS(dismount_stable_hits_, ride->dismount.stable_hits);
        ride->pending_exit = StageResult {
            .kind = StageResultKind::ChainAbandoned,
            .chain_abandoned = ChainAbandoned { "zipline/dismount/stuck" },
        };
        enter_stage(ride, ZiplineStage::Failed, now);
        return handoff(ride, now);
    }
    return {};
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
        if (SameTower(*nearest, target)) {
            return LandingClass::AtTarget;
        }
        if (SameTower(*nearest, origin)) {
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

int32_t YawTurnBudgetMs(double residual)
{
    return kZiplineTurnBudgetBaseMs + static_cast<int32_t>(std::lround(std::abs(residual) * kZiplineTurnBudgetPerDegMs));
}

void RestartYawAim(YawAim* aim)
{
    aim->prev_heading.reset();
    aim->stable_heading_hits = 0;
    aim->turn_pending = false;
    aim->turn_sent_at = {};
}

std::optional<double> PollYawAim(YawAim* aim, const ZiplineObservation& obs, std::chrono::steady_clock::time_point now)
{
    if (aim->turn_pending && ElapsedMs(aim->turn_sent_at, now) < kWaitAfterFirstTurnMs) {
        return std::nullopt;
    }
    const std::optional<double> heading = settled_heading(aim, obs);
    if (heading) {
        note_turn_response(aim, *heading);
        aim->turn_pending = false;
    }
    return heading;
}

std::optional<double> IssueYawTurn(YawAim* aim, ZiplineIo* io, double heading, double residual, std::chrono::steady_clock::time_point now)
{
    const std::optional<double> issued = ZiplineIoTurnYaw(io, residual / aim->yaw_response_ratio.value_or(1.0));
    if (!issued) {
        return std::nullopt;
    }
    aim->turn_ref_heading = heading;
    aim->turn_cmd_deg = *issued;
    aim->turn_sent_at = now;
    aim->turn_pending = true;
    aim->stable_heading_hits = 0;
    aim->prev_heading.reset();
    return issued;
}

double YawResponseRatio(const YawAim& aim)
{
    return aim.yaw_response_ratio.value_or(1.0);
}

void ArmLandingWatch(LandingWatch* watch, const std::optional<NaviPosition>& fix)
{
    *watch = {};
    watch->launch_fix = fix;
}

void ArmDismountWatch(DismountWatch* watch)
{
    *watch = {};
    watch->presses = 1;
}

// Mounting 不算在架上, 它正在判定是否已上架
bool ZiplineOnTower(const ZiplineRide& ride)
{
    return ride.parked_on.has_value() || ride.stage == ZiplineStage::OnTower || ride.stage == ZiplineStage::Aiming
           || ride.stage == ZiplineStage::Fired || ride.stage == ZiplineStage::ExitAiming || ride.stage == ZiplineStage::ReturnAiming;
}

std::optional<ZiplineNodeRef> ZiplineTowerUnderfoot(const ZiplineRide& ride)
{
    if (ride.parked_on) {
        return ride.parked_on;
    }
    if (ZiplineOnTower(ride)) {
        return ride.origin;
    }
    return std::nullopt;
}

void MarkZiplineMountUnreachable(ZiplineRide* ride, const ZiplineHopPlan& plan)
{
    const std::chrono::steady_clock::time_point now = std::chrono::steady_clock::now();
    ZiplineHopRecord record;
    record.plan = plan;
    record.outcome = HopOutcome::Unboardable;
    record.began_at = now;
    record.ended_at = now;
    ride->ledger.push_back(record);
    LogWarn << "zipline/mount/unboardable" << VAR(plan.mount.x) << VAR(plan.mount.y) << VAR(plan.mount_spots.size())
            << VAR_AS(ledger_.size(), ride->ledger.size());
}

void RecordDismissedHop(ZiplineRide* ride)
{
    // 发射过却被叫下来只可能是重站后仍没发出去, 按滑不动记
    if (ride->hop_open) {
        commit_record(
            ride,
            ride->record.launches.empty() ? HopOutcome::Dismounted : HopOutcome::NoLaunch,
            std::chrono::steady_clock::now());
    }
}

void ResetZiplineHop(ZiplineRide* ride)
{
    ride->stage = ZiplineStage::Idle;
    ride->stage_entered_at = {};
    ride->plan = {};
    ride->record = {};
    ride->hop_open = false;
    ride->origin = {};
    ride->target = {};
    ride->seed_elevation_deg = 0.0;
    ride->aim_bias_deg = 0.0;
    ride->pitch_lowered = false;
    ride->pitch_tier = 0;
    ride->returning = false;
    ride->hop_retry_count = 0;
    ride->mount_presses = 0;
    ride->on_ground_hits = 0;
    ride->discovered_towers.clear();
    ride->parked_on.reset();
    ride->last_fix.reset();
    ride->landing = {};
    ride->aim = {};
    ride->exit_aim = {};
    ride->exit_aim_budget_ms = 0;
    ride->dismount = {};
    ride->pending_exit = {};
}

void ResetZiplineNavigation(ZiplineRide* ride)
{
    ResetZiplineHop(ride);
    ride->ledger.clear();
}

void BeginZiplineHop(ZiplineRide* ride, const ZiplineHopPlan& plan)
{
    const auto now = std::chrono::steady_clock::now();
    const bool standing = ZiplineOnTower(*ride);
    // 重新站过上索点再回来仍算同一跳, 记录接着写
    const bool resume = ride->hop_open && SameTower(ride->plan.mount, plan.mount) && SameTower(ride->plan.landing, plan.landing);
    if (!resume) {
        if (ride->hop_open) {
            commit_record(ride, HopOutcome::Dismounted, now);
        }
        ResetZiplineHop(ride);
        ride->plan = plan;
        ride->record = {};
        ride->record.plan = plan;
        ride->record.began_at = now;
        ride->hop_open = true;
        ride->aim_bias_deg = first_shot_aim_bias_deg(*ride);
    }
    ride->parked_on.reset();
    ride->origin = ride->plan.mount;
    ride->target = ride->plan.landing;
    ride->returning = false;
    ride->pitch_tier = 0;
    ride->last_fix.reset();
    ride->landing = {};
    ride->pending_exit = {};
    LogInfo << "zipline/begin" << VAR(resume) << VAR_AS(plan_.mount.x, ride->plan.mount.x) << VAR_AS(plan_.mount.y, ride->plan.mount.y)
            << VAR_AS(plan_.landing.x, ride->plan.landing.x) << VAR_AS(plan_.landing.y, ride->plan.landing.y)
            << VAR_AS(plan_.planned_elevation_deg, ride->plan.planned_elevation_deg)
            << VAR_AS(plan_.siblings.size(), ride->plan.siblings.size()) << VAR_AS(aim_bias_deg_, ride->aim_bias_deg)
            << VAR_AS(plan_.chain_continues, ride->plan.chain_continues) << VAR(standing);
    enter_stage(ride, standing ? ZiplineStage::OnTower : ZiplineStage::Mounting, now);
}

StageResult TickZiplineHop(ZiplineRide* ride, ZiplineIo* io)
{
    const auto now = std::chrono::steady_clock::now();
    switch (ride->stage) {
    case ZiplineStage::OnTower:
        return tick_on_tower(ride, io, now);
    case ZiplineStage::Idle:
        return {};
    default:
        break;
    }

    const std::vector<ZiplineNodeRef> nodes = known_nodes(*ride);
    // Fired 阶段跟踪器还停在上索点, 会把小地图隐藏帧跟出贴着上索点的假位置
    const ZiplineObservation obs =
        ride->stage == ZiplineStage::Fired ? ZiplineIoObserveGlobal(io, nodes) : ZiplineIoObserveTracked(io, nodes);
    switch (ride->stage) {
    case ZiplineStage::Mounting:
        return tick_mounting(ride, io, obs);
    case ZiplineStage::Aiming:
    case ZiplineStage::ReturnAiming:
        return tick_aiming(ride, io, obs);
    case ZiplineStage::Fired:
        return tick_fired(ride, io, obs);
    case ZiplineStage::Riding:
        return tick_riding(ride, io, obs);
    case ZiplineStage::Landed:
        return tick_landed(ride, io, obs);
    case ZiplineStage::ExitAiming:
        return tick_exit_aiming(ride, io, obs);
    case ZiplineStage::Dismounting:
        return tick_dismounting(ride, io, obs);
    default:
        return {};
    }
}

} // namespace mapnavigator
