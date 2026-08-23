#pragma once

#include <chrono>
#include <cstddef>
#include <limits>
#include <string>

#include "navi_domain_types.h"

namespace mapnavigator
{

struct RouteTrackerState
{
    size_t passed_waypoint_idx = std::numeric_limits<size_t>::max();
    bool passed_waypoint_latched = false;
    double best_projection_on_segment = 0.0;
    size_t last_segment_from_idx = std::numeric_limits<size_t>::max();
    size_t last_segment_to_idx = std::numeric_limits<size_t>::max();
    NaviPosition startup_anchor_pos {};
    bool startup_anchor_initialized = false;
    bool startup_motion_confirmed = false;

    void ResetTracking()
    {
        passed_waypoint_idx = std::numeric_limits<size_t>::max();
        passed_waypoint_latched = false;
        best_projection_on_segment = 0.0;
        last_segment_from_idx = std::numeric_limits<size_t>::max();
        last_segment_to_idx = std::numeric_limits<size_t>::max();
    }

    void Reset()
    {
        ResetTracking();
        startup_anchor_pos = {};
        startup_anchor_initialized = false;
        startup_motion_confirmed = false;
    }
};

struct FlowState
{
    std::chrono::steady_clock::time_point navigate_started_at {};
    std::chrono::steady_clock::time_point last_auto_sprint_time {};
    // Entry stamp of the previous navigate tick, so the tick log can report the real loop period. Stamped on
    // every entry including the ones that bail out early, and only ever reported from the steering tick, so a
    // large gap means the ticks in between returned early rather than that this one ran long.
    std::chrono::steady_clock::time_point last_tick_started_at {};
    // Counts navigate ticks. Lets staleness be judged in control opportunities rather than wall clock, which
    // is what keeps the rule meaning the same thing on a fast machine and on one with slow frame capture.
    uint64_t tick_seq = 0;
    // Consecutive steering ticks that held forward, issued no turn, and saw the fix stay put. The forward hold
    // is edge-triggered, so a dropped keydown looks exactly like this and never repairs itself.
    int32_t motionless_hold_ticks = 0;
    // Consecutive re-sends of that hold that moved the agent nowhere. Cleared by any motion, any turn, or a
    // recovery escape, so it only ever counts a hold that is provably not the thing holding it up.
    int32_t futile_forward_reasserts = 0;
    NaviPosition last_steer_position {};
    bool has_last_steer_position = false;
};

struct SemanticState
{
    std::chrono::steady_clock::time_point transfer_wait_started {};
    NaviPosition transfer_anchor_pos {};
    int transfer_stable_hits = 0;
    bool portal_transit_active = false;
    bool portal_transit_keep_moving_until_fix = false;
    bool portal_transit_needs_reacquire = false;
    std::chrono::steady_clock::time_point portal_transit_started {};
    std::chrono::steady_clock::time_point zipline_ride_started {};
    // 按下起滑那一刻人站在哪儿。滑一趟必然离开这里, 所以它是「到底滑没滑起来」的唯一凭据
    NaviPosition zipline_mount_pos {};
    ZiplineTarget zipline_landing {};
    int zipline_landing_hits = 0;
    // 起滑按了几次。按下去没滑走多半是俯仰没对上, 抬头角是开环发的, 只能换一档再按; 试满就退索
    int zipline_launch_attempts = 0;
    // 上索起算, 镜头一共被抬了多少度。俯仰读不到, 只能自己记着发出去的量, 好让下一次按增量补
    double zipline_pitch_deg = 0.0;
    // 上一拍的位置和它连着重合了几次。滑行停稳却不在落点, 就是这趟滑岔了
    NaviPosition zipline_last_pos {};
    int zipline_settle_hits = 0;
    // 滑反了正在原路滑回上索点。回去了就退索走路, 不会再滑第二趟
    bool zipline_returning = false;
    // 这一次上索是行进预筛叫停的, 人可能还差几步。此时认不出提示只说明预筛看错了, 不该丢链
    bool zipline_prompt_probe = false;
    // 人是不是站在架子上。链首上索时置位, 链尾下索或中途退索时清掉。站着时不能直接走路,
    // 得先右键离开架子, 否则移动指令被架子上的选点状态吃掉
    bool zipline_mounted = false;
    std::string held_zone_candidate;
    int held_zone_hits = 0;

    void ResetTransient()
    {
        transfer_wait_started = {};
        transfer_anchor_pos = {};
        transfer_stable_hits = 0;
        portal_transit_active = false;
        portal_transit_keep_moving_until_fix = false;
        portal_transit_needs_reacquire = false;
        portal_transit_started = {};
        zipline_ride_started = {};
        zipline_mount_pos = {};
        zipline_landing = {};
        zipline_landing_hits = 0;
        zipline_launch_attempts = 0;
        zipline_pitch_deg = 0.0;
        zipline_last_pos = {};
        zipline_settle_hits = 0;
        zipline_returning = false;
        zipline_prompt_probe = false;
        zipline_mounted = false;
        held_zone_candidate.clear();
        held_zone_hits = 0;
    }
};

struct DynamicRecoveryState
{
    NaviPosition anchor_pos {};
    std::chrono::steady_clock::time_point started_at {};
    std::chrono::steady_clock::time_point last_replan_at {};
    size_t anchor_index = std::numeric_limits<size_t>::max();
    bool active = false;

    void Reset()
    {
        anchor_pos = {};
        started_at = {};
        last_replan_at = {};
        anchor_index = std::numeric_limits<size_t>::max();
        active = false;
    }
};

// Recovery ladder position (device removal -> jump -> navmesh detour -> physical unstick), keyed on the
// corridor anchor the agent is stuck against. Top-level so a dynamic replan, which renumbers the path and
// clears the DynamicRecoveryState episode, cannot rewind it; cleared only by a genuine escape, a waypoint
// advance or a new navigation.
struct RecoveryEscalationState
{
    size_t anchor_index = std::numeric_limits<size_t>::max();
    // Device removals tried here. Deliberately not read by the escalation gate below: the device step runs
    // ahead of the jump rather than in place of it, so it can never postpone the detour.
    int device_attempt_count = 0;
    int jump_attempt_count = 0;
    int detour_attempt_count = 0;

    void Reset()
    {
        anchor_index = std::numeric_limits<size_t>::max();
        device_attempt_count = 0;
        jump_attempt_count = 0;
        detour_attempt_count = 0;
    }
};

struct LocalizationLossState
{
    std::chrono::steady_clock::time_point started_at {};
    std::chrono::steady_clock::time_point last_unstick_at {};
    bool saw_black_screen = false;

    void Reset()
    {
        started_at = {};
        last_unstick_at = {};
        saw_black_screen = false;
    }
};

// River-fall recovery latch: a black-screen loss = fell in water + force-teleport to shore facing the water.
// Armed on both re-acquire paths, consumed in TickNavigate. See navigator-river-fall-teleport-gap.
struct RiverFallRecoveryState
{
    NaviPosition anchor_pos {};
    // Facing read AFTER the settle, not at arm time (minimap arrow = toward water); the about-face turns 180 off it.
    double water_heading = 0.0;
    bool pending = false;
    // Stood still long enough for the arrow to be trustworthy again / the 180 has gone out. Both one-shot: the turn
    // must not be recomputed from a half-turned arrow, only committed by walking.
    bool settled = false;
    bool turned = false;

    void Reset()
    {
        anchor_pos = {};
        water_heading = 0.0;
        pending = false;
        settled = false;
        turned = false;
    }
};

// Physical lateral-bypass escalation. Deliberately persists across recovery.Reset() so consecutive bypasses
// at the same stuck spot grow the step and alternate sides; cleared only on genuine progress (waypoint
// advance / new navigation) or once the agent has moved away from `origin`.
struct LateralBypassState
{
    NaviPosition origin {};
    int count = 0;
    bool active = false;

    void Reset()
    {
        origin = {};
        count = 0;
        active = false;
    }
};

// Previous-tick heading, used to estimate the agent's own turn rate for the implausible-rate glitch check. Only the
// physical heading is tracked here; the rate is gated at the call site on the elapsed gap and on plausibility,
// so a stale entry after a recovery / relocation pause simply yields a zero rate that tick rather than a spike.
// The cmd_* fields are diagnostic only: they hold the last turn actually sent to the controller so a later tick
// can report how much heading the turn really produced. Nothing steers off them.
struct SteeringRateState
{
    double prev_heading_deg = 0.0;
    bool has_prev = false;
    std::chrono::steady_clock::time_point at {};
    uint64_t at_tick = 0;
    double cmd_heading_deg = 0.0;
    double cmd_delta_deg = 0.0;
    bool has_cmd = false;
    std::chrono::steady_clock::time_point cmd_at {};
    // Which way a near-about-face turn was committed to, held until it is well under way. Zero means free.
    int turn_latch_sign = 0;
    // Degrees already sent that the heading has not shown yet, and the heading they are counted from. A turn
    // takes a few ticks to finish while the controller re-decides every tick, so without this running total the
    // same error is commanded over and over before any of it lands.
    double pending_turn_deg = 0.0;
    double pending_ref_heading_deg = 0.0;

    void Reset()
    {
        prev_heading_deg = 0.0;
        has_prev = false;
        at = {};
        at_tick = 0;
        cmd_heading_deg = 0.0;
        cmd_delta_deg = 0.0;
        has_cmd = false;
        cmd_at = {};
        turn_latch_sign = 0;
        pending_turn_deg = 0.0;
        pending_ref_heading_deg = 0.0;
    }
};

// Off-route wedge watchdog clock. Fed straight-line distance to the current waypoint and only run while the agent
// is off the route corridor, so it grows only during a genuine no-progress wedge that the corridor-fed stall
// clocks miss. Drives a replan, then a fail-fast.
struct OffRouteWedgeState
{
    std::chrono::steady_clock::time_point since {};
    std::chrono::steady_clock::time_point last_replan_at {};
    double best_distance = std::numeric_limits<double>::max();
    bool active = false;

    void Reset()
    {
        since = {};
        last_replan_at = {};
        best_distance = std::numeric_limits<double>::max();
        active = false;
    }
};

// Cross-tier escape. The agent fell onto a wrong FLOORED tier (one the route never planned for); we plan ONE
// navmesh corridor from that tier fix back to a reachable authored waypoint and follow it, tolerating the
// open-air shaft's tier<->base oscillation as a live guard rather than re-planning on every flip. Everything is
// gated on `active`: when false the navigator and the real-loss handling are byte-for-byte unchanged.
// `anchor_zone` is the tier we fell into; it defines the same-geometry span we tolerate flipping within. `goal_*`
// is the base-pixel rejoin waypoint (arrival exits the mode). A continuously-stuck escape is bounded at the call
// site by the hard-progress stall clock (no field needed here); a recover<->re-lose thrash is bounded by the
// top-level re-acquire streak below.
struct CrossTierEscapeState
{
    bool active = false;
    std::string anchor_zone;
    double goal_x = 0.0;
    double goal_y = 0.0;

    void Reset()
    {
        active = false;
        anchor_zone.clear();
        goal_x = 0.0;
        goal_y = 0.0;
    }
};

struct ProgressIdentityState
{
    static constexpr size_t kSerialKeyBias = std::numeric_limits<size_t>::max() / 2;

    bool from_anchor = false;
    size_t source_reset_idx = std::numeric_limits<size_t>::max();

    void Reset()
    {
        from_anchor = false;
        source_reset_idx = std::numeric_limits<size_t>::max();
    }
};

// 走向上索点这一路上重规划了几次。上索点是必到的语义点, 重规划总是瞄回它, 所以架子够不着时
// 会一路重规划到楔死超时把整趟导航掐掉。身份记的是 canonical 下标而不是路径下标: 重规划本身
// 就会改路径下标, 挂在它上面的计数永远攒不满。
struct ZiplineApproachState
{
    size_t anchor_index = std::numeric_limits<size_t>::max();
    int32_t replans = 0;
    // 到点按过一次没认出来。原地再按还是同一个答案, 所以改瞄备用站位并收紧判定圈, 让人挪一下再认
    bool press_missed = false;

    void Reset()
    {
        anchor_index = std::numeric_limits<size_t>::max();
        replans = 0;
        press_missed = false;
    }
};

struct NavigationRuntimeState
{
    RouteTrackerState route;
    FlowState flow;
    SemanticState semantic;
    DynamicRecoveryState recovery;
    RecoveryEscalationState recovery_escalation;
    LocalizationLossState localization_loss;
    RiverFallRecoveryState river_fall;
    LateralBypassState bypass;
    SteeringRateState steering_rate;
    OffRouteWedgeState offroute;
    CrossTierEscapeState cross_tier_escape;
    // 顶层且不进任何一个 Reset: 它数的正是重规划本身, 跟着重规划清零就永远数不满。换了上索点
    // 由它自己按身份清, 换了整趟导航由 BeginNavigation 清
    ZiplineApproachState zipline_approach;
    // Consecutive global re-acquires (the navigation_state_machine "recovered via global re-acquire" path) since
    // the last genuine waypoint advance. Top-level on purpose: the loss/escape/overlay Resets that fire all through
    // a wrong-tier thrash storm never clear it — only real forward progress does — so it is the one storm-proof
    // fast-fail signal. Reset in OnWaypointAdvance / BeginNavigation only.
    int global_reacquire_streak = 0;
    bool dynamic_replan_requested = false;
    bool nav_run_dirty = true;
    ProgressIdentityState progress_identity;

    void ResetNavigationAssistState()
    {
        route.ResetTracking();
        recovery.Reset();
        recovery_escalation.Reset();
        steering_rate.Reset();
        offroute.Reset();
        dynamic_replan_requested = false;
        nav_run_dirty = true;
    }

    void BeginNavigation(const std::chrono::steady_clock::time_point& now)
    {
        route.Reset();
        semantic.ResetTransient();
        recovery.Reset();
        recovery_escalation.Reset();
        localization_loss.Reset();
        river_fall.Reset();
        bypass.Reset();
        steering_rate.Reset();
        offroute.Reset();
        cross_tier_escape.Reset();
        zipline_approach.Reset();
        progress_identity.Reset();
        global_reacquire_streak = 0;
        dynamic_replan_requested = false;
        nav_run_dirty = true;
        flow.navigate_started_at = now;
        flow.last_auto_sprint_time = {};
        flow.last_tick_started_at = {};
    }

    void OnWaypointAdvance()
    {
        route.ResetTracking();
        recovery.Reset();
        recovery_escalation.Reset();
        river_fall.Reset();
        bypass.Reset();
        offroute.Reset();
        global_reacquire_streak = 0;
        dynamic_replan_requested = false;
        nav_run_dirty = true;
        flow.last_auto_sprint_time = {};
    }
};

} // namespace mapnavigator
