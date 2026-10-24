#pragma once

#include <array>
#include <chrono>
#include <cstdint>
#include <functional>
#include <memory>
#include <optional>
#include <string>
#include <vector>

#include "../control/obstacle_device_recovery.h"
#include "../control/walk_mode.h"
#include "../navi_controller.h"
#include "../planning/nav_run_controller.h"
#include "../planning/navigation_session.h"
#include "../semantic/async_prompt_action.h"
#include "navigation_runtime_state.h"

namespace mapnavigator
{

class ActionExecutor;
class ActionWrapper;
class MotionController;
class PositionProvider;
class RoiTemplateScanner;
struct RouteTrackingState;
struct TickFix;
enum class TickStep;

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

    semantic_nodes::Context SemanticContext();
    bool TickPhase(NaviPhase phase);
    bool TickNavigate();
    NavigateTick BeginNavigateTick();
    TickStep RunSemanticFlow();
    TickStep ConcludeSemanticStep(const semantic_nodes::Result& result);
    TickStep RunInlineSemantics();
    TickStep IdleWhileNotWalking();
    TickStep FinishNavigateTick(const NavigateTick& tick);
    void SelectPhaseForCurrentWaypoint(const char* reason);
    bool ResumeAfterEscape(const char* reason);
    void StopMotion();
    bool FailNavigation(const char* reason, const char* log_message, double current_distance, double yaw_error, int64_t stalled_ms);

    bool Bootstrap();

    bool CaptureCurrentPosition(bool force_global_search = false);
    void UpdateDwellWatchdog(bool captured);
    TickStep CaptureNavigateFix(NavigateTick& tick);
    // 丢定位由落水造成时返回 true, 此时落水自救已挂上
    bool EndLocalizationLoss();
    TickStep CheckDwellWatchdog();
    bool HandleLocalizationLoss();
    bool ResumeAfterGlobalReacquire(const std::string& prior_zone, std::chrono::milliseconds loss_elapsed);
    bool ArmRiverFallRecoveryIfBlackScreenLoss(const char* via);
    TickStep RunRiverFallRecovery(const NavigateTick& tick);
    bool TryEnterCrossTierEscape();
    bool PlanCrossTierEscapeCorridorFromHere(const char* reason);
    void EndCrossTierEscapeAtRejoin();

    void TrackRoute(NavigateTick& tick);
    TickFix ReadFix(std::chrono::steady_clock::time_point now);
    void TickNavRun(NavigateTick& tick);
    void ConsumeCorridorPassedWaypoints(NavigateTick& tick);
    TickStep CheckRouteProgress(NavigateTick& tick);
    double ObserveNavigationProgress(
        const RouteTrackingState& route,
        double straight_to_anchor,
        const std::optional<size_t>& anchor_index,
        const std::chrono::steady_clock::time_point& now);
    double ResolveArrivalDistance(const NavigateTick& tick);
    TickStep HandleWaypointArrival(NavigateTick& tick);
    TickStep SettleAtArrival(const NavigateTick& tick);
    TickStep PassArrivedWaypoints(NavigateTick& tick);

    bool TryApplyDynamicOverlayToAnchor(
        const char* reason,
        size_t continue_index,
        const Waypoint& anchor,
        bool emit_interior_corners = false);
    bool TryApplyDynamicOverlayToNextAnchor(const char* reason);
    // 在 origin 前方放一块禁区再规划到锚点; 这里是唯一通路时把禁区标成 push_through 并返回 false
    bool TryVirtualNoGoReplan(
        const char* reason,
        size_t continue_index,
        const Waypoint& anchor,
        const NaviPosition& origin,
        double stuck_heading);
    // 判成够不着就丢链改走路, 返回 true 表示这一拍已处理完
    bool GiveUpUnreachableZipline(const char* reason);
    bool HandleZiplineRecoveryReplan();
    bool TryReplanRemainingAuthoredRoute(const char* reason);
    bool AbandonRope(uint32_t uid);
    bool HandleDynamicReplanRequest(const char* reason);

    TickStep CheckOffRouteWedge(const NavigateTick& tick);
    TickStep RunObstacleRecovery(const NavigateTick& tick);
    bool ClimbRecoveryLadder(const NavigateTick& tick, bool far_or_strict);
    bool EscalatePastJump(const NavigateTick& tick);
    bool ExecutePhysicalUnstick(double stuck_heading);

    // 返回本拍的朝向误差
    double SteerAlongCorridor(const NavigateTick& tick);
    void UpdateAutoSprint(const NavigateTick& tick);
    void UpdateWalkMode(NaviPhase phase);

    TickStep HandlePrompts(const NavigateTick& tick);
    std::array<AsyncPromptAction*, 2> PromptActions();
    void StartScanners();
    void StopScanners();
    void StartPromptScanners();
    void StartDeviceProbe();
    void PreWarmPromptRecognition();
    bool TryZiplineMountPrompt(const Waypoint& waypoint, const RouteTrackingState& route);
    // 路线上没有需要减速接近的点时 distance_sq 为 -1
    PromptDistance NearestPromptDistance() const;
    void UpdatePromptSprintSuppression();
    bool TryRunPromptSubtaskWhileWalking(const RouteTrackingState& route);
    void CompleteWaypointAfterPromptTrigger();
    void TryRunPromptSubtaskAtRouteTail();

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
    std::optional<uint64_t> prior_observation_epoch_;
    std::optional<NaviPhase> last_tick_phase_;
    // 只用于诊断
    int consecutive_prior_uses_ = 0;
    // 本次导航封禁的升降索底端 uid
    std::vector<uint32_t> rope_off_;

    AsyncPromptAction collect_prompt_;
    AsyncPromptAction interact_prompt_;

    ObstacleDeviceRecovery device_recovery_;
    std::unique_ptr<RoiTemplateScanner> zipline_mount_scanner_;
    // 放在最后, 析构时最先执行, 恢复慢跑时用到的成员都还在
    walkmode::Toggle walk_mode_;
};

} // namespace mapnavigator
