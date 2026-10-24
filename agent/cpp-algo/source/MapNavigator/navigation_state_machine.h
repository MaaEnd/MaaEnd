#pragma once

#include <array>
#include <chrono>
#include <cstdint>
#include <functional>
#include <memory>
#include <optional>
#include <string>
#include <utility>
#include <vector>

#include "async_prompt_action.h"
#include "nav_run_controller.h"
#include "navi_controller.h"
#include "navigation_runtime_state.h"
#include "navigation_session.h"
#include "obstacle_device_recovery.h"
#include "walk_mode.h"

namespace mapnavigator
{

class ActionExecutor;
class ActionWrapper;
class MotionController;
class PositionProvider;
class RoiTemplateScanner;
struct RouteTrackingState;
struct SteeringCommand;

namespace semantic_nodes
{
struct Context;
struct Result;
} // namespace semantic_nodes

std::optional<size_t> ResolveRouteResumeIndex(const std::vector<Waypoint>& path, const NaviPosition& position);

class NavigationStateMachine
{
public:
    NavigationStateMachine(
        const NaviParam& param,
        ActionWrapper* action_wrapper,
        PositionProvider* position_provider,
        NavigationSession* session,
        MotionController* motion_controller,
        ActionExecutor* action_executor,
        NaviPosition* position,
        std::function<bool()> should_stop,
        MaaContext* maa_context);

    bool Run();
    ~NavigationStateMachine();

private:
    struct PromptDistance
    {
        double distance_sq = -1.0;
        bool is_zipline = false;
        double dig_distance_sq = -1.0;
    };

    struct NavigateTick;
    // 有值表示这一步结束了本拍, 值就是 TickNavigate 的返回值
    using TickExit = std::optional<bool>;

    bool Bootstrap();
    bool TickNavigate();
    NavigateTick BeginNavigateTick();
    TickExit RunSemanticFlow();
    TickExit CaptureNavigateFix(NavigateTick& tick);
    // 丢定位由落水造成时返回 true, 此时落水自救已挂上
    bool EndLocalizationLoss();
    TickExit CheckDwellWatchdog();
    TickExit RunInlineSemantics();
    void EndCrossTierEscapeAtRejoin();
    TickExit IdleWhileNotWalking();
    void TrackRoute(NavigateTick& tick);
    void ObserveFix(NavigateTick& tick);
    void TickNavRun(NavigateTick& tick);
    void ConsumeCorridorPassedWaypoints(NavigateTick& tick);
    TickExit CheckRouteProgress(NavigateTick& tick);
    TickExit HandlePrompts(NavigateTick& tick);
    TickExit HandleWaypointArrival(NavigateTick& tick);
    double ResolveArrivalDistance(const NavigateTick& tick);
    TickExit SettleAtArrival(const NavigateTick& tick);
    TickExit PassArrivedWaypoints(NavigateTick& tick);
    TickExit RunRiverFallRecovery(NavigateTick& tick);
    TickExit CheckOffRouteWedge(NavigateTick& tick);
    TickExit RunObstacleRecovery(NavigateTick& tick);
    bool ClimbRecoveryLadder(const NavigateTick& tick, bool far_or_strict);
    bool EscalatePastJump(const NavigateTick& tick);
    void SteerAlongCorridor(NavigateTick& tick);
    void SettlePendingTurn(
        double steer_heading,
        std::chrono::steady_clock::time_point now,
        bool drops_turn_sends,
        std::optional<int64_t> steer_period_ms);
    bool BrakeForSharpCorner(const NavRunTickResult& nav_run_result, bool slow_loop);
    std::pair<double, int64_t> SendSteering(
        const SteeringCommand& steering,
        int64_t tick_gap_ms,
        double steer_heading,
        std::chrono::steady_clock::time_point now,
        bool drops_turn_sends);
    void UpdateForwardHold(double issued_delta_deg, double heading_error);
    bool FinishNavigateTick(const NavigateTick& tick);
    void UpdateAutoSprint(const NavigateTick& tick);
    semantic_nodes::Context SemanticContext();
    TickExit ConcludeSemanticStep(const semantic_nodes::Result& result);
    bool TickPhase(NaviPhase phase);
    bool CaptureCurrentPosition(bool force_global_search = false);
    void UpdateDwellWatchdog(bool captured);
    bool HandleLocalizationLoss();
    bool ResumeAfterGlobalReacquire(const std::string& prior_zone, std::chrono::milliseconds loss_elapsed);
    bool ArmRiverFallRecoveryIfBlackScreenLoss(const char* via);
    bool TryApplyDynamicOverlayToAnchor(
        const char* reason,
        size_t continue_index,
        const Waypoint& anchor,
        bool emit_interior_corners = false);
    bool TryApplyDynamicOverlayToNextAnchor(const char* reason);
    // 在 origin 前方放一块存续整趟导航的禁区再规划到锚点, 收缩后仍失败则标为 push_through 并返回 false
    bool TryVirtualNoGoReplan(
        const char* reason,
        size_t continue_index,
        const Waypoint& anchor,
        const NaviPosition& origin,
        double stuck_heading);
    // 判成够不着就丢链改走路, 返回 true 表示这一拍已处理完
    bool GiveUpUnreachableZipline(const char* reason);
    bool HandleZiplineRecoveryReplan();
    // 切出剩余作者路线, 从脚下重新展开并整条换路
    bool TryReplanRemainingAuthoredRoute(const char* reason);
    // 本次导航封禁这根升降索, 剩余作者路线从脚下重新展开
    bool AbandonRope(uint32_t uid);
    bool HandleDynamicReplanRequest(const char* reason);
    bool TryEnterCrossTierEscape();
    bool PlanCrossTierEscapeCorridorFromHere(const char* reason);
    bool ExecutePhysicalUnstick(double stuck_heading);
    void SelectPhaseForCurrentWaypoint(const char* reason);
    bool ResumeAfterEscape(const char* reason);
    double ObserveNavigationProgress(
        const RouteTrackingState& route,
        double straight_to_anchor,
        const std::optional<size_t>& anchor_index,
        const std::chrono::steady_clock::time_point& now);
    void StopMotion();
    bool FailNavigation(const char* reason, const char* log_message, double current_distance, double yaw_error, int64_t stalled_ms);

    std::array<AsyncPromptAction*, 2> PromptActions();
    bool TryRunPromptSubtaskWhileWalking(const RouteTrackingState& route);
    void CompleteWaypointAfterPromptTrigger();
    void TryRunPromptSubtaskAtRouteTail();
    void PreWarmPromptRecognition();
    void StartScanners();
    void StopScanners();
    void StartPromptScanners();
    void StartDeviceProbe();
    // 提示出现即算够得着; 预筛看错时返回 false
    bool TryZiplineMountPrompt(const Waypoint& waypoint, const RouteTrackingState& route);
    void UpdatePromptSprintSuppression();
    // Distance and kind of the nearest point needing a slow approach; distance_sq is -1 when the route has none.
    PromptDistance NearestPromptDistance() const;
    void UpdateWalkMode(NaviPhase phase);

    const NaviParam& param_;
    ActionWrapper* action_wrapper_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    ActionExecutor* action_executor_;
    NaviPosition* position_;
    std::function<bool()> should_stop_;
    MaaContext* maa_context_;
    NavigationRuntimeState runtime_state_ {};
    NavRunController nav_run_controller_ {};
    std::chrono::steady_clock::time_point last_global_relocalize_at_ {};
    // 上次成功取位时的朝向纪元, 为空表示先验不可用
    std::optional<uint64_t> prior_observation_epoch_;
    // 相位切换时作废先验观测
    std::optional<NaviPhase> last_tick_phase_;
    // 只用于诊断
    int consecutive_prior_uses_ = 0;
    // 本次导航封禁的升降索底端 uid
    std::vector<uint32_t> rope_off_;

    // Two instances of one flow; they differ only in the pipeline node names and who supplies the text.
    AsyncPromptAction collect_prompt_;
    AsyncPromptAction interact_prompt_;

    ObstacleDeviceRecovery device_recovery_;
    // 上索提示认不出时要丢链改走路, 所以单独用扫描器预筛
    std::unique_ptr<RoiTemplateScanner> zipline_mount_scanner_;
    // Declared last so its destructor runs first — restores jogging while its collaborators are still alive.
    walkmode::Toggle walk_mode_;
};

} // namespace mapnavigator
