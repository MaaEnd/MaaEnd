#pragma once

#include <chrono>
#include <cstddef>
#include <deque>
#include <limits>
#include <optional>
#include <string>
#include <vector>

#include "../../Navmesh/NavmeshTypes.h"
#include "../navi_domain_types.h"
#include "../navi_time.h"
#include "../sensing/prompt_scan_profile.h"
#include "../zipline/zipline_ride_machine.h"

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
};

inline void ResetRouteTracking(RouteTrackerState* route)
{
    route->passed_waypoint_idx = std::numeric_limits<size_t>::max();
    route->passed_waypoint_latched = false;
    route->best_projection_on_segment = 0.0;
    route->last_segment_from_idx = std::numeric_limits<size_t>::max();
    route->last_segment_to_idx = std::numeric_limits<size_t>::max();
}

inline void RestartRouteTracking(RouteTrackerState* route)
{
    *route = {};
}

inline void ConfirmStartupAt(RouteTrackerState* route, const NaviPosition& position)
{
    route->startup_anchor_pos = position;
    route->startup_anchor_initialized = true;
    route->startup_motion_confirmed = true;
}

struct FlowState
{
    std::chrono::steady_clock::time_point navigate_started_at {};
    std::chrono::steady_clock::time_point last_auto_sprint_time {};
    // 每个导航拍都记, 提前返回的拍也记
    std::chrono::steady_clock::time_point last_tick_started_at {};
    uint64_t tick_seq = 0;
    // 按住前进、没转向、位置也没变的连续拍数
    int32_t motionless_hold_ticks = 0;
    // 补按前进之后仍然没动的次数
    int32_t futile_forward_reasserts = 0;
    NaviPosition last_steer_position {};
    bool has_last_steer_position = false;
    // 相邻两个转向拍之间的间隔
    std::vector<int64_t> steer_periods_ms;
    std::chrono::steady_clock::time_point last_steer_tick_at {};
    // 上一个刹过车的弯道顶点, 每个弯只刹一次
    std::optional<navmesh::WorldPoint> braked_corner;
    // 刹过车的那个弯还在前面
    bool corner_walk = false;
};

inline void ResetStallCounters(FlowState* flow)
{
    flow->motionless_hold_ticks = 0;
    flow->futile_forward_reasserts = 0;
}

// pad 的 x/y 是板心, zone_id 是起跳所在区
struct JumpPadState
{
    NaviPosition pad {};
    std::chrono::steady_clock::time_point started {};
    std::chrono::steady_clock::time_point jumped_at {};
    std::chrono::steady_clock::time_point launched_at {};
    std::optional<NaviPosition> last_fix;
    int still_reads = 0;
    int jumps = 0;
    int nudges = 0;
    bool launched = false;
};

struct TransferWaitState
{
    std::chrono::steady_clock::time_point started {};
    NaviPosition anchor {};
    int moved_reads = 0;
};

enum class PortalTransitPhase
{
    Idle,
    AwaitZone,
    Reacquire,
};

struct PortalTransitState
{
    PortalTransitPhase phase = PortalTransitPhase::Idle;
    std::chrono::steady_clock::time_point started {};
};

inline bool IsPortalTransitActive(const PortalTransitState& portal)
{
    return portal.phase != PortalTransitPhase::Idle;
}

struct RopeRideState
{
    std::chrono::steady_clock::time_point started {};
    int64_t ride_ms = 0;
    int stable_reads = 0;
};

struct SemanticState
{
    JumpPadState jump_pad;
    TransferWaitState transfer;
    PortalTransitState portal;
    RopeRideState rope;
    // 这次上索由行进预筛叫停, 认不出提示时只说明预筛看错了
    bool zipline_prompt_probe = false;
};

struct DynamicRecoveryState
{
    NaviPosition anchor_pos {};
    std::chrono::steady_clock::time_point started_at {};
    std::chrono::steady_clock::time_point last_replan_at {};
    size_t anchor_index = std::numeric_limits<size_t>::max();
    bool active = false;
};

inline void RestartDynamicRecovery(DynamicRecoveryState* recovery, const NaviPosition& position, size_t anchor, TimePoint now)
{
    *recovery = {};
    recovery->active = true;
    recovery->anchor_pos = position;
    recovery->started_at = now;
    recovery->anchor_index = anchor;
}

inline void ClearDynamicRecovery(DynamicRecoveryState* recovery)
{
    *recovery = {};
}

// FIND 的进度。按点计: 推进点位或重开导航就清
struct FindState
{
    std::chrono::steady_clock::time_point started_at {};
    int32_t steps = 0;
    // 连续漏认的拍数, 见 kFindMissGraceTicks
    int32_t miss_streak = 0;
    // 上次看到目标时框中心在中线哪一侧 (+1 右 / -1 左 / 0 没见过), 搜索第一步朝这边转
    int32_t last_seen_side = 0;
    // 停车判据的模板预筛, 为空时探测直接跑识别
    std::optional<PromptScanProfile> stop_probe;
};

struct TriggerState
{
    std::chrono::steady_clock::time_point last_probe_at {};
};

// 卡死恢复走到了第几级, 按走廊锚点记; 动态重规划不清它
struct RecoveryEscalationState
{
    size_t anchor_index = std::numeric_limits<size_t>::max();
    // 升级判定不看这个计数, 挪设备不会推迟绕行
    int device_attempt_count = 0;
    int jump_attempt_count = 0;
    int detour_attempt_count = 0;
    std::string blocked_reason;
};

inline void RetargetRecoveryEscalation(RecoveryEscalationState* escalation, size_t anchor)
{
    *escalation = {};
    escalation->anchor_index = anchor;
}

struct LocalizationLossState
{
    std::chrono::steady_clock::time_point started_at {};
    std::chrono::steady_clock::time_point last_unstick_at {};
    // 取位失败只会清 valid, 坐标还是最后一次的
    NaviPosition last_fix {};
    bool saw_black_screen = false;
};

// 丢定位以黑屏收尾时挂上: 人落水后被拉回了岸边
struct RiverFallRecoveryState
{
    NaviPosition anchor_pos {};
    // 站定之后读到的朝向, 转身按它转 180°
    double water_heading = 0.0;
    bool pending = false;
    // 都只做一次, 免得拿转到一半的箭头重算转身
    bool settled = false;
    bool turned = false;
};

inline void ArmRiverFallRecovery(RiverFallRecoveryState* river_fall, const NaviPosition& position, double heading)
{
    *river_fall = {};
    river_fall->pending = true;
    river_fall->anchor_pos = position;
    river_fall->water_heading = heading;
}

// 清 recovery 时不清它, 同一处反复脱困时方向会轮换
struct LateralBypassState
{
    NaviPosition origin {};
    int count = 0;
    bool active = false;
};

inline void BeginLateralBypass(LateralBypassState* bypass, const NaviPosition& position)
{
    bypass->active = true;
    bypass->origin = position;
    bypass->count = 0;
}

// cmd_* 只用于诊断
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
    // 接近掉头时锁定的转向方向, 转出一段后才放开; 0 表示没锁
    int turn_latch_sign = 0;
    // 已发出但朝向还没体现的转角, 从 pending_ref_heading_deg 算起
    double pending_turn_deg = 0.0;
    double pending_ref_heading_deg = 0.0;

    // 还在存活期内的转向指令; 左右两个方向各自限住上面的总量
    struct InFlightTurn
    {
        double delta_deg = 0.0;
        std::chrono::steady_clock::time_point sent_at {};
    };

    std::deque<InFlightTurn> in_flight;
};

// 只在离开走廊时计时, 补上走廊停滞计时漏掉的卡死
struct OffRouteWedgeState
{
    std::chrono::steady_clock::time_point since {};
    std::chrono::steady_clock::time_point last_replan_at {};
    // 看门狗上次运行后的第一个盲拍, 下次运行时把 since 往后挪过这一段
    std::chrono::steady_clock::time_point blind_since {};
    double best_distance = std::numeric_limits<double>::max();
    bool active = false;
};

enum class WedgeAction
{
    None,
    Replan,
    Fail,
};

inline void PauseWedge(OffRouteWedgeState* wedge, TimePoint now)
{
    if (wedge->active && wedge->blind_since == std::chrono::steady_clock::time_point {}) {
        wedge->blind_since = now;
    }
}

// 返回扣掉的盲区毫秒数, 没暂停过返回 0
inline int64_t ResumeWedge(OffRouteWedgeState* wedge, TimePoint now)
{
    if (wedge->blind_since == std::chrono::steady_clock::time_point {}) {
        return 0;
    }
    const auto blind = now - wedge->blind_since;
    wedge->since += blind;
    if (wedge->last_replan_at != std::chrono::steady_clock::time_point {}) {
        wedge->last_replan_at += blind;
    }
    wedge->blind_since = {};
    return std::chrono::duration_cast<std::chrono::milliseconds>(blind).count();
}

// 换了走廊后 best_distance 下一拍重取, 计时照走
inline void DropWedgeBaseline(OffRouteWedgeState* wedge)
{
    wedge->best_distance = std::numeric_limits<double>::max();
}

inline bool HasWedgeBaseline(const OffRouteWedgeState& wedge)
{
    return wedge.best_distance != std::numeric_limits<double>::max();
}

inline WedgeAction StepWedge(OffRouteWedgeState* wedge, double progress, TimePoint now)
{
    const double progress_epsilon = std::max(kNoProgressDistanceEpsilon, kMeasurementDefaultPositionQuantum);
    const bool retaking_baseline = wedge->active && !HasWedgeBaseline(*wedge);
    if (!wedge->active || retaking_baseline || progress + progress_epsilon < wedge->best_distance) {
        wedge->active = true;
        wedge->best_distance = progress;
        if (!retaking_baseline) {
            wedge->since = now;
        }
    }
    const int64_t wedge_ms = ElapsedMs(wedge->since, now);
    if (wedge_ms >= kOffRouteWedgeFailMs) {
        return WedgeAction::Fail;
    }
    const bool replan_cooling =
        wedge->last_replan_at.time_since_epoch().count() > 0 && ElapsedMs(wedge->last_replan_at, now) < kOffRouteWedgeReplanCooldownMs;
    if (wedge_ms >= kOffRouteWedgeReplanMs && !replan_cooling) {
        wedge->last_replan_at = now;
        return WedgeAction::Replan;
    }
    return WedgeAction::None;
}

// 在一个固定的世界坐标圆里待了多久, 只在相邻两次有效取位之间累计
struct DwellWatchdogState
{
    double center_x = 0.0;
    double center_y = 0.0;
    std::string center_zone;
    bool latched = false;
    int64_t dwell_ms = 0;
    std::chrono::steady_clock::time_point last_usable {};
};

struct ProgressIdentityState
{
    static constexpr size_t kSerialKeyBias = std::numeric_limits<size_t>::max() / 2;

    bool from_anchor = false;
    size_t source_reset_idx = std::numeric_limits<size_t>::max();
};

// 走向上索点途中的重规划次数; 身份记 canonical 下标, 因为重规划会改路径下标
struct ZiplineApproachState
{
    size_t anchor_index = std::numeric_limits<size_t>::max();
    int32_t replans = 0;
    // 到点按过一次没认出, 改瞄下一个站位并收紧判定圈
    bool press_missed = false;
    // 正在试计划里的第几个站位, 连同这个进度算的是哪根架子
    size_t spot_index = 0;
    ZiplineNodeRef spot_tower;
};

inline void RetargetZiplineApproach(ZiplineApproachState* approach, size_t anchor)
{
    approach->anchor_index = anchor;
    approach->replans = 0;
    approach->press_missed = false;
    approach->spot_index = 0;
}

// 游标只对 spot_tower 那根架子有效, 换了架子从头试
inline void FollowMountTower(ZiplineApproachState* approach, const ZiplineNodeRef& mount)
{
    if (!approach->spot_tower.sameTower(mount)) {
        approach->spot_tower = mount;
        approach->spot_index = 0;
        approach->press_missed = false;
    }
}

// 到点认空过一次的交互点, 身份记 canonical 下标
struct InteractApproachState
{
    std::optional<size_t> prompt_missed_node;
    // 收紧的判定圈从认空这一刻起算
    std::chrono::steady_clock::time_point missed_at {};
};

inline bool PromptMissedAt(const InteractApproachState& approach, const std::optional<size_t>& node)
{
    return node.has_value() && approach.prompt_missed_node == node;
}

struct PromptRepeatState
{
    std::optional<size_t> node;
    size_t prompt = 0;
    int32_t recognized = 0;
};

// 退索后要连续几次稳定的新定位重新确认位置, 才接回剩余路线
struct ZiplineRecoveryState
{
    std::chrono::steady_clock::time_point started_at {};
    NaviPosition stable_pos {};
    int32_t stable_hits = 0;
    bool pending = false;
};

inline void BeginZiplineRecovery(ZiplineRecoveryState* recovery, TimePoint now)
{
    recovery->started_at = now;
    recovery->stable_pos = {};
    recovery->stable_hits = 0;
    recovery->pending = true;
}

struct NavigationRuntimeState
{
    RouteTrackerState route;
    FlowState flow;
    SemanticState semantic;
    DynamicRecoveryState recovery;
    RecoveryEscalationState recovery_escalation;
    LocalizationLossState localization_loss;
    // 每次导航只交给场景管理一次
    bool scene_recovery_attempted = false;
    RiverFallRecoveryState river_fall;
    LateralBypassState bypass;
    SteeringRateState steering_rate;
    OffRouteWedgeState offroute;
    // 不进任何 reset, 只在走出盘或换区时自清, 或由 BeginNavigation 清
    DwellWatchdogState dwell;
    // 推进点位或重开导航就清
    FindState find;
    // 只由 BeginNavigation 清, 推进点位不重置识别节拍
    TriggerState trigger;
    // 不进任何 reset, 换了上索点按身份自清, 或由 BeginNavigation 清
    ZiplineApproachState zipline_approach;
    InteractApproachState interact_approach;
    PromptRepeatState prompt_repeat;
    ZiplineRecoveryState zipline_recovery;
    // 账本跨越丢链和重规划, 只由 BeginNavigation 清
    ZiplineRide zipline_ride;
    // 整趟导航有效, 只由 BeginNavigation 清
    std::vector<VirtualNoGoDisc> virtual_no_go;
    // 上次真正推进路点以来的全局重定位次数, 只由 ResetOnWaypointAdvance 和 BeginNavigation 清
    int global_reacquire_streak = 0;
    bool dynamic_replan_requested = false;
    bool nav_run_dirty = true;
    // 只由 ArmCameraAlign 置位, 下一个 navigate 拍消费, 不进任何 reset
    bool camera_align_pending = false;
    ProgressIdentityState progress_identity;
};

inline void ArmCameraAlign(NavigationRuntimeState* runtime)
{
    runtime->camera_align_pending = true;
}

inline void ClearCameraAlignRequest(NavigationRuntimeState* runtime)
{
    runtime->camera_align_pending = false;
}

inline void RequestDynamicReplan(NavigationRuntimeState* runtime)
{
    runtime->dynamic_replan_requested = true;
}

inline void ClearDynamicReplanRequest(NavigationRuntimeState* runtime)
{
    runtime->dynamic_replan_requested = false;
}

inline void MarkNavRunDirty(NavigationRuntimeState* runtime)
{
    runtime->nav_run_dirty = true;
}

inline void ClearNavRunDirty(NavigationRuntimeState* runtime)
{
    runtime->nav_run_dirty = false;
}

inline void CountGlobalReacquire(NavigationRuntimeState* runtime)
{
    ++runtime->global_reacquire_streak;
}

inline void ResetGlobalReacquireStreak(NavigationRuntimeState* runtime)
{
    runtime->global_reacquire_streak = 0;
}

inline void ResetNavigationAssistState(NavigationRuntimeState* runtime)
{
    ResetRouteTracking(&runtime->route);
    ClearDynamicRecovery(&runtime->recovery);
    runtime->recovery_escalation = {};
    runtime->steering_rate = {};
    runtime->offroute = {};
    runtime->zipline_recovery = {};
    ClearDynamicReplanRequest(runtime);
    MarkNavRunDirty(runtime);
}

inline void BeginNavigation(NavigationRuntimeState* runtime, TimePoint now)
{
    RestartRouteTracking(&runtime->route);
    runtime->semantic = {};
    ClearDynamicRecovery(&runtime->recovery);
    runtime->recovery_escalation = {};
    runtime->localization_loss = {};
    runtime->scene_recovery_attempted = false;
    runtime->river_fall = {};
    runtime->bypass = {};
    runtime->steering_rate = {};
    runtime->offroute = {};
    runtime->dwell = {};
    runtime->zipline_approach = {};
    runtime->interact_approach.prompt_missed_node.reset();
    runtime->prompt_repeat = {};
    runtime->zipline_recovery = {};
    runtime->find = {};
    runtime->trigger = {};
    ResetZiplineNavigation(&runtime->zipline_ride);
    runtime->virtual_no_go.clear();
    runtime->progress_identity = {};
    ResetGlobalReacquireStreak(runtime);
    ClearDynamicReplanRequest(runtime);
    MarkNavRunDirty(runtime);
    ArmCameraAlign(runtime);
    runtime->flow.navigate_started_at = now;
    runtime->flow.last_auto_sprint_time = {};
    runtime->flow.last_tick_started_at = {};
    runtime->flow.steer_periods_ms.clear();
    runtime->flow.last_steer_tick_at = {};
    runtime->flow.braked_corner.reset();
    runtime->flow.corner_walk = false;
}

inline void ResetOnWaypointAdvance(NavigationRuntimeState* runtime)
{
    ResetRouteTracking(&runtime->route);
    ClearDynamicRecovery(&runtime->recovery);
    runtime->recovery_escalation = {};
    runtime->river_fall = {};
    runtime->bypass = {};
    runtime->offroute = {};
    runtime->zipline_recovery = {};
    runtime->find = {};
    ResetGlobalReacquireStreak(runtime);
    ClearDynamicReplanRequest(runtime);
    MarkNavRunDirty(runtime);
    runtime->flow.last_auto_sprint_time = {};
}

// 人站在滑索架上。这时移动指令会被架子吃掉, 上索提示也不用再认
inline bool IsZiplineMounted(const NavigationRuntimeState& runtime)
{
    return ZiplineOnTower(runtime.zipline_ride);
}

} // namespace mapnavigator
