#include "zipline_action.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstring>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "position_provider.h"
#include "semantic_helpers.h"

#include "../utils.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

// 跑一个 pipeline 节点, 回答这一趟里点名的那个节点认没认出来。识别不中的节点不会进这一趟的
// 节点表, 所以「表里有且 completed」等于提示确实在屏幕上、动作也确实发了出去。
bool RunNodeAndReportHit(MaaContext* context, const char* entry, const char* node, const std::string& pipeline_override)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        return false;
    }
    const MaaTaskId task_id = MaaContextRunTask(context, entry, pipeline_override.c_str());
    if (task_id == MaaInvalidId) {
        LogWarn << "Zipline subtask failed to dispatch." << VAR(entry);
        return false;
    }

    ScopedStringBuffer entry_name;
    MaaSize node_count = 0;
    MaaStatus status = MaaStatus_Invalid;
    if (entry_name.Get() == nullptr || !MaaTaskerGetTaskDetail(tasker, task_id, entry_name.Get(), nullptr, &node_count, &status)
        || node_count == 0) {
        return false;
    }
    std::vector<MaaNodeId> node_ids(node_count);
    if (!MaaTaskerGetTaskDetail(tasker, task_id, entry_name.Get(), node_ids.data(), &node_count, &status)) {
        return false;
    }

    for (const MaaNodeId node_id : node_ids) {
        ScopedStringBuffer node_name;
        MaaRecoId reco_id = 0;
        MaaActId action_id = 0;
        MaaBool completed = 0;
        if (node_name.Get() == nullptr || !MaaTaskerGetNodeDetail(tasker, node_id, node_name.Get(), &reco_id, &action_id, &completed)) {
            continue;
        }
        const char* raw = MaaStringBufferGet(node_name.Get());
        if (raw != nullptr && std::strcmp(raw, node) == 0) {
            return completed != 0;
        }
    }
    return false;
}

// 出口的 next 截断掉, 子任务跑到那儿就返回引擎; roi、动作、按键一律留在 pipeline 里。
std::string BuildMountOverride()
{
    json::object exit_node;
    exit_node["next"] = json::array {};
    json::object root;
    root[kZiplineMountExitNode] = std::move(exit_node);
    return json::value(std::move(root)).dumps();
}

// 认出架子的交互提示才按下去。认不出就是这根架子不在跟前, 这一趟不该有任何按键发出去。
bool PressMountPrompt(MaaContext* context)
{
    return RunNodeAndReportHit(context, kZiplineMountEntryNode, kZiplineMountRecognitionNode, BuildMountOverride());
}

// 上索提示还在不在。站上架子后这条提示就没了, 所以它同时是「上没上去」和「还站不站着」的凭据。
// 只认图标, 认不出这个面板属于架子还是属于旁边那台设备 —— 有疑问时拿 MountTextVisible 复核。
bool MountPromptVisible(MaaContext* context)
{
    return RunNodeAndReportHit(context, kZiplineMountScanEntryNode, kZiplineMountScanNode, "{}");
}

// 同一个识别, 但把交互键摘掉: 复核问的是「这条提示还在不在」, 不该再按一次。
std::string BuildMountProbeOverride()
{
    json::object exit_node;
    exit_node["next"] = json::array {};
    json::object probe_node;
    probe_node["action"] = "DoNothing";
    json::object root;
    root[kZiplineMountExitNode] = std::move(exit_node);
    root[kZiplineMountRecognitionNode] = std::move(probe_node);
    return json::value(std::move(root)).dumps();
}

// 「登上滑索架」这几个字还在不在。比图标贵, 只在图标说「还在」时问一次。
bool MountTextVisible(MaaContext* context)
{
    return RunNodeAndReportHit(context, kZiplineMountEntryNode, kZiplineMountRecognitionNode, BuildMountProbeOverride());
}

// 右键下索。链尾和每一条异常出口都得先走这一步, 否则后面的移动指令全被架子吃掉。
void LeaveTower(const Context& ctx)
{
    if (!ctx.runtime_state->semantic.zipline_mounted) {
        return;
    }
    ctx.action_wrapper->MouseRightDownSync(kZiplineDismountHoldMs);
    ctx.action_wrapper->MouseRightUpSync(0);
    utils::SleepFor(kZiplineLaunchSettleMs);
    ctx.runtime_state->semantic.zipline_mounted = false;
}

void ClearRideState(const Context& ctx)
{
    ctx.runtime_state->semantic.zipline_ride_started = {};
    ctx.runtime_state->semantic.zipline_mount_pos = {};
    ctx.runtime_state->semantic.zipline_landing = {};
    ctx.runtime_state->semantic.zipline_landing_hits = 0;
    ctx.runtime_state->semantic.zipline_launch_attempts = 0;
    ctx.runtime_state->semantic.zipline_pitch_deg = 0.0;
    ctx.runtime_state->semantic.zipline_last_pos = {};
    ctx.runtime_state->semantic.zipline_settle_hits = 0;
    ctx.runtime_state->semantic.zipline_returning = false;
}

// 一跳里第几次按左键该把镜头抬到多少度。俯仰读不回来, dy 的正负也没实机核过, 所以第二次直接
// 反着来, 第三次干脆不动俯仰——三次里必有一次踩在对的那一侧。
double PitchTargetForAttempt(double elevation_deg, int attempt)
{
    if (std::abs(elevation_deg) < kZiplinePitchDeadbandDeg) {
        return 0.0;
    }
    const double aim = std::clamp(elevation_deg, -kZiplinePitchMaxDeg, kZiplinePitchMaxDeg);
    if (attempt == 0) {
        return aim;
    }
    if (attempt == 1) {
        return -aim;
    }
    return 0.0;
}

// 站在架子上瞄准。水平方向闭环收进容差, 俯仰按算好的仰角开环发。转镜头不带前进脉冲: 架子上
// 转镜头就能带动小地图朝向, 而站在架子上按前进是没验证过的输入。读不到稳定朝向就返回 false,
// 宁可退索走路也不盲按左键——按下去就滑走了, 没有第二次机会。
bool AimAtLanding(const Context& ctx, const ZiplineTarget& landing, int attempt)
{
    const double target_heading = NaviMath::CalcTargetRotation(ctx.position->x, ctx.position->y, landing.x, landing.y);
    double achieved = NaviMath::NormalizeAngle(ctx.position->angle);
    double residual = NaviMath::NormalizeAngle(target_heading - achieved);
    for (int correction = 0; correction <= kZiplineAimMaxRetries; ++correction) {
        if (!TurnToHeadingOnce(ctx, residual)) {
            LogWarn << "Zipline aim: the view turn was rejected." << VAR(residual);
            return false;
        }
        utils::SleepFor(kWaitAfterFirstTurnMs);
        if (!CaptureStableHeading(ctx, &achieved)) {
            LogWarn << "Zipline aim: no stable heading on the tower." << VAR(target_heading);
            return false;
        }
        residual = NaviMath::NormalizeAngle(target_heading - achieved);
        if (std::abs(residual) <= kZiplineAimToleranceDeg) {
            break;
        }
    }
    if (std::abs(residual) > kZiplineAimToleranceDeg) {
        LogWarn << "Zipline aim: the heading never settled." << VAR(target_heading) << VAR(achieved) << VAR(residual);
        return false;
    }

    const double pitch_target = PitchTargetForAttempt(landing.elevation_deg, attempt);
    const double pitch_delta = pitch_target - ctx.runtime_state->semantic.zipline_pitch_deg;
    if (std::abs(pitch_delta) >= 1.0) {
        // 屏幕坐标里 dy 向下为正, 抬头看上坡要往上拉, 所以取负
        const int units = static_cast<int>(std::lround(-pitch_delta * ctx.action_wrapper->DefaultPitchUnitsPerDegree()));
        if (units != 0 && !ctx.action_wrapper->SendViewDeltaSync(0, units)) {
            LogWarn << "Zipline aim: the pitch delta was rejected." << VAR(pitch_delta) << VAR(units);
            return false;
        }
        ctx.runtime_state->semantic.zipline_pitch_deg = pitch_target;
        utils::SleepFor(kWaitAfterFirstTurnMs);
    }

    LogInfo << "Zipline aim settled." << VAR(attempt) << VAR(target_heading) << VAR(achieved) << VAR(landing.elevation_deg)
            << VAR(pitch_target);
    return true;
}

// 起滑就是对着瞄好的方向按一下左键
void FireLaunch(const Context& ctx)
{
    ctx.motion_controller->SetForwardState(false);
    ctx.action_wrapper->ClickMouseLeftSync();
    utils::SleepFor(kZiplineLaunchSettleMs);
    ++ctx.runtime_state->semantic.zipline_launch_attempts;
}

} // namespace

// 滑索的每一条异常出口都从这里走。索是捷径不是必经之路，捷径走不成的正确答案永远是走路，
// 不是让整趟导航失败——人挂在索上或者卡在架子边上时，失败等于原地不动到超时。
Result AbandonZipline(const Context& ctx, const char* reason, const char* detail)
{
    Result result;
    StopMotionAndCommitment(ctx);
    LeaveTower(ctx);

    // 还没走完的接近段全是走廊上的普通点，链的头一跳就跟在它们后面。先碰到别的语义点就说明
    // 前面根本没有链——最后一跳出事时就是这样，剩下的路本来就是走路，一个点都不该丢。
    const std::vector<Waypoint>& path = ctx.session->current_path();
    size_t hop = ctx.session->current_node_idx();
    while (hop < path.size() && path[hop].HasPosition() && path[hop].action == ActionType::RUN && !path[hop].RequiresStrictArrival()) {
        ++hop;
    }
    // 整条链一起丢。留下任何一跳，重规划都会把人送回索边再试一次，而刚失败的正是这条索。
    size_t dropped = 0;
    while (hop + dropped < path.size() && path[hop + dropped].action == ActionType::ZIPLINE) {
        ++dropped;
    }
    if (dropped != 0) {
        ctx.session->SkipPastWaypoint(hop + dropped - 1, reason);
    }

    LogWarn << "Action: ZIPLINE given up, walking the rest of the way." << VAR(reason) << VAR(detail) << VAR(dropped)
            << VAR(ctx.position->x) << VAR(ctx.position->y);

    ClearRideState(ctx);
    ctx.runtime_state->zipline_approach.Reset();
    ctx.runtime_state->OnWaypointAdvance();
    ctx.runtime_state->route.Reset();
    ctx.position_provider->ResetTracking();
    ctx.session->ResetProgress();
    // 剩下的路是照着「从落点出发」规划的，人却还在索这一头，得重新规划一条过去。
    // OnWaypointAdvance 会清掉这个标志，所以只能压在它后面。
    ctx.runtime_state->dynamic_replan_requested = true;

    SelectPhaseForCurrentWaypoint(ctx, reason);
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

Result StartZiplineHop(
    const Context& ctx,
    const Waypoint& waypoint,
    double actual_distance,
    const std::optional<size_t>& arrived_absolute_node_idx)
{
    Result result;
    StopMotionAndCommitment(ctx);

    // 落点是规划器算出来写进点里的，手写路线写不出来。缺了就没有可对准的方向，
    // 与其对着 (0,0) 转镜头再瞎按一下，不如当这根索不存在
    if (!waypoint.zipline_target) {
        return AbandonZipline(ctx, "zipline_target_missing", "waypoint carries no landing point");
    }

    // 链首要先站上架子; 中途落下来人已经站在下一根上, 直接接着瞄就行
    if (!ctx.runtime_state->semantic.zipline_mounted) {
        if (ctx.maa_context == nullptr) {
            return AbandonZipline(ctx, "zipline_no_context", "no pipeline context to recognize the mount prompt");
        }
        if (!PressMountPrompt(ctx.maa_context)) {
            // 预筛叫停的这一次人还没走到, 认不出就是那个图标不属于滑索架: 当没发生, 接着走
            if (ctx.runtime_state->semantic.zipline_prompt_probe) {
                LogInfo << "Zipline mount pre-filter did not hold up; keeping the approach." << VAR(actual_distance);
                return result;
            }
            // 面板给的是离身位最近的那台设备, 原地重按拿到的还是同一个答案 —— 认不出只有两种走法:
            // 人还差一点点没走到跟前, 或者架子边上那根供电桩把面板占着。两种都得靠挪身位解决, 所以
            // 记一笔交回导航: 有备用站位就改瞄它(从供电桩那侧让开一点, 顺带也更近了), 没有就只把
            // 判定圈收紧, 让人把差的那点走完。预筛一路开着, 提示先冒出来就先按下去
            if (!ctx.runtime_state->zipline_approach.press_missed) {
                ctx.runtime_state->zipline_approach.press_missed = true;
                const bool restood =
                    waypoint.mount_restand
                    && ctx.session->RetargetCurrentWaypoint(waypoint.mount_restand->x, waypoint.mount_restand->y, "zipline_mount_restand");
                LogInfo << "No mount prompt at the tower; walking a bit around it for another look." << VAR(actual_distance) << VAR(restood)
                        << VAR(kZiplineRestandBandWu);
                result.consumed = true;
                result.stay_in_current_tick = true;
                return result;
            }
            return AbandonZipline(ctx, "zipline_prompt_missing", "no mount prompt at the tower");
        }
        // 交互键已经发出去了, 从这里起就得当人可能已经站在架子上: 认错方向的代价是走不动路,
        // 反过来白按一次右键什么也不会发生
        ctx.runtime_state->semantic.zipline_mounted = true;
        bool mounted = false;
        for (int attempt = 0; attempt < kZiplineMountConfirmAttempts && !mounted; ++attempt) {
            utils::SleepFor(kZiplineMountConfirmIntervalMs);
            mounted = !MountPromptVisible(ctx.maa_context);
        }
        // 图标还在不代表没上去: 人一站上架子, 面板就让给近旁的供电桩, 那个图标长得一模一样。
        // 判死之前按文字复核一次 ——「登上滑索架」没了就是上去了, 还在才是真没按上
        if (!mounted && !MountTextVisible(ctx.maa_context)) {
            LogInfo << "Mount icon is still up but the zipline text is gone; the panel went to a device next to the tower.";
            mounted = true;
        }
        if (!mounted) {
            return AbandonZipline(ctx, "zipline_mount_failed", "the mount prompt is still up after the press");
        }
        ctx.runtime_state->zipline_approach.press_missed = false;
    }

    const ZiplineTarget& landing = *waypoint.zipline_target;
    // 站上架子时镜头俯仰是多少没人知道, 这一跳的抬头量一律从当下这个姿态起算
    ctx.runtime_state->semantic.zipline_launch_attempts = 0;
    ctx.runtime_state->semantic.zipline_pitch_deg = 0.0;
    if (!AimAtLanding(ctx, landing, 0)) {
        return AbandonZipline(ctx, "zipline_aim_failed", "could not aim the view at the landing point");
    }
    FireLaunch(ctx);
    LogInfo << "Action: ZIPLINE launched toward the landing point." << VAR(landing.x) << VAR(landing.y) << VAR(landing.height)
            << VAR(landing.elevation_deg) << VAR(actual_distance);

    ctx.session->NoteCanonicalFinalGoalConsumed(arrived_absolute_node_idx, *ctx.position, "zipline_ride_started");
    ctx.session->AdvanceToNextWaypoint(ActionType::ZIPLINE, "zipline_ride_started");
    ctx.runtime_state->OnWaypointAdvance();
    ctx.runtime_state->semantic.zipline_ride_started = std::chrono::steady_clock::now();
    ctx.runtime_state->semantic.zipline_mount_pos = *ctx.position;
    ctx.runtime_state->semantic.zipline_landing = landing;
    ctx.runtime_state->semantic.zipline_landing_hits = 0;
    ctx.position_provider->ResetTracking();

    // 起滑那一刻人还在上索点, 这条链就算已经是路线的尾巴也不能在这里收工:
    // 收工要按落点判成没到终点, 而人正悬在半空往那儿滑。一律等落地再说
    ctx.session->UpdatePhase(NaviPhase::WaitZipline, "zipline_ride_started");

    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

// 滑行中不许对位置做任何解释：人悬在索上，小地图上的点一路在动，任何「动了就算完成」的判据
// 都会在起滑后一瞬间成立。完成只认一件事——落点圈里连着读到稳定定位。
Result TickZiplineRide(const Context& ctx)
{
    Result result;
    ctx.motion_controller->SetForwardState(false);

    const auto now = std::chrono::steady_clock::now();
    if (ctx.runtime_state->semantic.zipline_ride_started.time_since_epoch().count() == 0) {
        ctx.runtime_state->semantic.zipline_ride_started = now;
    }
    const int64_t waited_ms =
        std::chrono::duration_cast<std::chrono::milliseconds>(now - ctx.runtime_state->semantic.zipline_ride_started).count();

    if (!ctx.position_provider->Capture(ctx.position, false, {}) || ctx.position_provider->LastCaptureWasHeld()) {
        ctx.runtime_state->semantic.zipline_landing_hits = 0;
        ctx.runtime_state->semantic.zipline_settle_hits = 0;
        if (waited_ms > kZiplineRideTimeoutMs) {
            return AbandonZipline(ctx, "zipline_ride_timeout", "no usable locator fix for the whole ride");
        }
        result.stay_in_current_tick = true;
        utils::SleepFor(kZiplineRideRetryIntervalMs);
        return result;
    }

    const NaviPosition previous_fix = ctx.runtime_state->semantic.zipline_last_pos;
    ctx.runtime_state->semantic.zipline_last_pos = *ctx.position;

    const ZiplineTarget& landing = ctx.runtime_state->semantic.zipline_landing;
    const double distance_to_landing = std::hypot(ctx.position->x - landing.x, ctx.position->y - landing.y);
    if (distance_to_landing > kZiplineLandingBandWu) {
        ctx.runtime_state->semantic.zipline_landing_hits = 0;
        if (previous_fix.valid && std::hypot(ctx.position->x - previous_fix.x, ctx.position->y - previous_fix.y) < kZiplineSettleMoveWu) {
            ++ctx.runtime_state->semantic.zipline_settle_hits;
        }
        else {
            ctx.runtime_state->semantic.zipline_settle_hits = 0;
        }
        // 索没通电、或者两端压根没挂上索时，起滑那一下是空响，人还站在架子上。滑一趟必然是大位移，
        // 所以「过了确认时间还在原地」只可能是没滑起来；这一条把它跟「滑起来了但没滑到」分开，
        // 不必在架子上干等满整个滑行超时。
        const NaviPosition& mount = ctx.runtime_state->semantic.zipline_mount_pos;
        const double moved = std::hypot(ctx.position->x - mount.x, ctx.position->y - mount.y);
        if (mount.valid && waited_ms > kZiplineMountConfirmMs && moved < kZiplineMountMinMoveWu) {
            // 人还在架子上。俯仰是开环发的, 所以先按下一档抬头角重瞄重按, 试满次数还起不来就
            // 当这根索用不了, 退索走路
            if (ctx.runtime_state->semantic.zipline_launch_attempts >= kZiplineLaunchAttempts) {
                return AbandonZipline(ctx, "zipline_launch_exhausted", "still standing on the tower after every aim attempt");
            }
            const int attempt = ctx.runtime_state->semantic.zipline_launch_attempts;
            if (!AimAtLanding(ctx, landing, attempt)) {
                return AbandonZipline(ctx, "zipline_aim_failed", "could not re-aim the view after a dead launch");
            }
            FireLaunch(ctx);
            LogWarn << "Action: ZIPLINE did not take, re-aimed and fired again." << VAR(attempt) << VAR(landing.elevation_deg)
                    << VAR(moved);
            ctx.runtime_state->semantic.zipline_ride_started = std::chrono::steady_clock::now();
            result.stay_in_current_tick = true;
            utils::SleepFor(kZiplineRideRetryIntervalMs);
            return result;
        }
        // 滑走了, 人却停在既不是落点也不是架子的地方, 这趟就是滑岔了。离落点更近说明方向没错,
        // 就地退索走路; 离上索点更近说明滑反了, 索是双向的, 原路滑回去再走
        if (ctx.runtime_state->semantic.zipline_settle_hits >= kZiplineSettleFixes && moved >= kZiplineMountMinMoveWu) {
            const double distance_to_mount = std::hypot(ctx.position->x - mount.x, ctx.position->y - mount.y);
            LogWarn << "Action: ZIPLINE stopped away from the landing point." << VAR(distance_to_landing) << VAR(distance_to_mount)
                    << VAR(waited_ms) << VAR(ctx.runtime_state->semantic.zipline_returning);
            if (ctx.runtime_state->semantic.zipline_returning || distance_to_landing <= distance_to_mount) {
                return AbandonZipline(ctx, "zipline_landed_off_target", "the ride stopped somewhere other than the landing point");
            }
            // 滑回去也是一根索, 仰角反过来只是个种子: 真正滑的是哪根索没人知道, 对不上就靠
            // 起滑那三档重试换角度
            const ZiplineTarget back { .x = mount.x, .y = mount.y, .elevation_deg = -landing.elevation_deg };
            ctx.runtime_state->semantic.zipline_returning = true;
            ctx.runtime_state->semantic.zipline_mount_pos = *ctx.position;
            ctx.runtime_state->semantic.zipline_landing = back;
            ctx.runtime_state->semantic.zipline_landing_hits = 0;
            ctx.runtime_state->semantic.zipline_settle_hits = 0;
            // 滑完一趟镜头俯仰又不知道是多少了, 跟刚上索时一样从当下这个姿态起算
            ctx.runtime_state->semantic.zipline_launch_attempts = 0;
            ctx.runtime_state->semantic.zipline_pitch_deg = 0.0;
            if (!AimAtLanding(ctx, back, 0)) {
                return AbandonZipline(ctx, "zipline_aim_failed", "could not aim back at the mount tower");
            }
            FireLaunch(ctx);
            LogWarn << "Action: ZIPLINE rode the wrong way, heading back to the mount tower." << VAR(back.x) << VAR(back.y);
            ctx.runtime_state->semantic.zipline_ride_started = std::chrono::steady_clock::now();
            result.stay_in_current_tick = true;
            utils::SleepFor(kZiplineRideRetryIntervalMs);
            return result;
        }
        if (waited_ms > kZiplineRideTimeoutMs) {
            return AbandonZipline(ctx, "zipline_ride_timeout", "rode off but never reached the landing point");
        }
        result.stay_in_current_tick = true;
        utils::SleepFor(kZiplineRideRetryIntervalMs);
        return result;
    }

    ++ctx.runtime_state->semantic.zipline_landing_hits;
    if (ctx.runtime_state->semantic.zipline_landing_hits < kZiplineLandingStableFixes) {
        result.stay_in_current_tick = true;
        utils::SleepFor(kZiplineRideRetryIntervalMs);
        return result;
    }

    // 滑回上索点了。这根索刚证明滑不对, 剩下的路一律走过去
    if (ctx.runtime_state->semantic.zipline_returning) {
        return AbandonZipline(ctx, "zipline_rode_back", "rode back to the mount tower after a wrong landing");
    }

    // 落在中继架子上就直接接着瞄下一根, 只有链尾才下索。下早了下一跳还得重新上一次。
    const bool chain_continues = ctx.session->HasCurrentWaypoint() && ctx.session->CurrentWaypoint().action == ActionType::ZIPLINE;
    if (!chain_continues) {
        LeaveTower(ctx);
    }

    LogInfo << "Action: ZIPLINE ride landed." << VAR(landing.x) << VAR(landing.y) << VAR(distance_to_landing) << VAR(waited_ms)
            << VAR(chain_continues);
    if (!ctx.position->zone_id.empty()) {
        ctx.session->UpdateCurrentZone(ctx.position->zone_id);
    }
    ctx.session->ResetProgress();
    ctx.runtime_state->ResetNavigationAssistState();
    ctx.runtime_state->route.Reset();
    // 滑行本身就是一次实打实的位移，落点还是连着几帧稳定定位确认过的，起步闸没有再拦一次的
    // 道理。链上的下一跳尤其：上索点就在脚下，拦住就等于要求人先走开再走回来
    ctx.runtime_state->route.startup_anchor_pos = *ctx.position;
    ctx.runtime_state->route.startup_anchor_initialized = true;
    ctx.runtime_state->route.startup_motion_confirmed = true;
    ClearRideState(ctx);
    ctx.position_provider->ResetTracking();

    if (!ctx.session->HasCurrentWaypoint()) {
        ctx.session->NoteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        result.consumed = true;
        result.stay_in_current_tick = true;
        return result;
    }

    SelectPhaseForCurrentWaypoint(ctx, "zipline_ride_complete");
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
