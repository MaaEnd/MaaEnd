#pragma once

#include <array>
#include <chrono>
#include <functional>
#include <memory>
#include <optional>
#include <string>

#include "async_prompt_action.h"
#include "nav_run_controller.h"
#include "navi_controller.h"
#include "navigation_runtime_state.h"
#include "navigation_session.h"
#include "obstacle_device_recovery.h"
#include "walk_mode.h"

namespace mapnavigator
{

class IActionExecutor;
class ActionWrapper;
class MotionController;
class PositionProvider;
struct RouteTrackingState;

class NavigationStateMachine
{
public:
    NavigationStateMachine(
        const NaviParam& param,
        ActionWrapper* action_wrapper,
        PositionProvider* position_provider,
        NavigationSession* session,
        MotionController* motion_controller,
        IActionExecutor* action_executor,
        NaviPosition* position,
        std::function<bool()> should_stop,
        MaaContext* maa_context);

    bool Run();
    ~NavigationStateMachine();

private:
    bool Bootstrap();
    bool TickNavigate();
    bool TickPhase(NaviPhase phase);
    bool CaptureCurrentPosition(bool force_global_search = false);
    bool HandleLocalizationLoss();
    bool ArmRiverFallRecoveryIfBlackScreenLoss(const char* via);
    bool TryApplyDynamicOverlayToAnchor(
        const char* reason,
        size_t continue_index,
        const Waypoint& anchor,
        bool use_detour,
        double route_heading = 0.0,
        bool emit_interior_corners = false);
    bool TryApplyDynamicOverlayToNextAnchor(const char* reason, bool use_detour, double route_heading = 0.0);
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
    void UpdatePromptSprintSuppression();
    // Squared distance to the nearest prompt-driven point; -1 when the route has none or the agent is unlocalized.
    double NearestPromptDistanceSq() const;
    void UpdateWalkMode(NaviPhase phase);

    const NaviParam& param_;
    ActionWrapper* action_wrapper_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    MotionController* motion_controller_;
    IActionExecutor* action_executor_;
    NaviPosition* position_;
    std::function<bool()> should_stop_;
    MaaContext* maa_context_;
    NavigationRuntimeState runtime_state_ {};
    NavRunController nav_run_controller_ {};
    std::chrono::steady_clock::time_point last_global_relocalize_at_ {};

    // Two instances of one flow; they differ only in the pipeline node names and who supplies the text.
    AsyncPromptAction collect_prompt_;
    AsyncPromptAction interact_prompt_;

    ObstacleDeviceRecovery device_recovery_;
    // Declared last so its destructor runs first — restores jogging while its collaborators are still alive.
    walkmode::Toggle walk_mode_;
};

} // namespace mapnavigator
