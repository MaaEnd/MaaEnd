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
struct RouteMeasurement;
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

    bool run();
    ~NavigationStateMachine();

private:
    struct PromptDistance
    {
        double distance_sq = -1.0;
        bool is_zipline = false;
        double dig_distance_sq = -1.0;
    };

    struct NavigateTick;

    semantic_nodes::Context semanticContext();
    bool tickPhase(NaviPhase phase);
    bool tickNavigate();
    NavigateTick beginNavigateTick();
    TickStep runSemanticFlow();
    TickStep concludeSemanticStep(const semantic_nodes::Result& result);
    TickStep runInlineSemantics();
    TickStep idleWhileNotWalking();
    TickStep finishNavigateTick(const NavigateTick& tick);
    void selectPhaseForCurrentWaypoint(const char* reason);
    bool resumeAfterEscape(const char* reason);
    void stopMotion();
    bool failNavigation(const char* reason, const char* log_message, double current_distance, double yaw_error, int64_t stalled_ms);

    bool bootstrap();

    bool captureCurrentPosition(bool force_global_search = false);
    void updateDwellWatchdog(bool captured);
    TickStep captureNavigateFix(NavigateTick& tick);
    // 丢定位由落水造成时返回 true, 此时落水自救已挂上
    bool endLocalizationLoss();
    TickStep checkDwellWatchdog();
    bool handleLocalizationLoss();
    bool resumeAfterGlobalReacquire(const std::string& prior_zone, std::chrono::milliseconds loss_elapsed);
    bool armRiverFallRecoveryIfBlackScreenLoss(const char* via);
    TickStep runRiverFallRecovery(const NavigateTick& tick);
    bool tryEnterCrossTierEscape();
    bool planCrossTierEscapeCorridorFromHere(const char* reason);
    void endCrossTierEscapeAtRejoin();

    void trackRoute(NavigateTick& tick);
    TickFix readFix(std::chrono::steady_clock::time_point now);
    void tickNavRun(NavigateTick& tick);
    void consumeCorridorPassedWaypoints(NavigateTick& tick);
    TickStep checkRouteProgress(NavigateTick& tick);
    double observeNavigationProgress(
        const RouteMeasurement& route,
        double straight_to_anchor,
        const std::optional<size_t>& anchor_index,
        const std::chrono::steady_clock::time_point& now);
    double resolveArrivalDistance(const NavigateTick& tick);
    TickStep handleWaypointArrival(NavigateTick& tick);
    TickStep settleAtArrival(const NavigateTick& tick);
    TickStep passArrivedWaypoints(NavigateTick& tick);

    bool tryApplyDynamicOverlayToAnchor(
        const char* reason,
        size_t continue_index,
        const Waypoint& anchor,
        bool emit_interior_corners = false);
    bool tryApplyDynamicOverlayToNextAnchor(const char* reason);
    // 在 origin 前方放一块禁区再规划到锚点; 这里是唯一通路时把禁区标成 push_through 并返回 false
    bool tryVirtualNoGoReplan(
        const char* reason,
        size_t continue_index,
        const Waypoint& anchor,
        const NaviPosition& origin,
        double stuck_heading);
    // 判成够不着就丢链改走路, 返回 true 表示这一拍已处理完
    bool giveUpUnreachableZipline(const char* reason);
    bool handleZiplineRecoveryReplan();
    bool tryReplanRemainingAuthoredRoute(const char* reason);
    bool abandonRope(uint32_t uid);
    bool handleDynamicReplanRequest(const char* reason);

    TickStep checkOffRouteWedge(const NavigateTick& tick);
    TickStep runObstacleRecovery(const NavigateTick& tick);
    bool climbRecoveryLadder(const NavigateTick& tick, bool far_or_strict);
    bool escalatePastJump(const NavigateTick& tick);
    bool executePhysicalUnstick(double stuck_heading);

    // 返回本拍的朝向误差
    double steerAlongCorridor(const NavigateTick& tick);
    void updateAutoSprint(const NavigateTick& tick);
    void updateWalkMode(NaviPhase phase);

    TickStep handlePrompts(const NavigateTick& tick);
    std::array<AsyncPromptAction*, 2> promptActions();
    void startScanners();
    void stopScanners();
    void startPromptScanners();
    void startDeviceProbe();
    void preWarmPromptRecognition();
    bool tryZiplineMountPrompt(const Waypoint& waypoint, const RouteMeasurement& route);
    // 路线上没有需要减速接近的点时 distance_sq 为 -1
    PromptDistance nearestPromptDistance() const;
    void updatePromptSprintSuppression();
    bool tryRunPromptSubtaskWhileWalking(const RouteMeasurement& route);
    void completeWaypointAfterPromptTrigger();
    void tryRunPromptSubtaskAtRouteTail();

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
