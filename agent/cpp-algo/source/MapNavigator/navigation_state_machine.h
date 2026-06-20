#pragma once

#include <chrono>
#include <functional>
#include <memory>
#include <string>

#include "nav_run_controller.h"
#include "navi_controller.h"
#include "navigation_runtime_state.h"
#include "navigation_session.h"

namespace mapnavigator
{

class IActionExecutor;
class ActionWrapper;
class MotionController;
class PositionProvider;
class CollectibleScanner;
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
        double route_heading = 0.0);
    bool TryApplyDynamicOverlayToNextAnchor(const char* reason, bool use_detour, double route_heading = 0.0);
    bool HandleDynamicReplanRequest(const char* reason);
    void SelectPhaseForCurrentWaypoint(const char* reason);
    void StopMotion();
    bool FailNavigation(const char* reason, const char* log_message, double current_distance, double yaw_error, int64_t stalled_ms);

    bool TryScanApproachCollect(const RouteTrackingState& route, const Waypoint& waypoint);
    void PreWarmCollectOcr();
    void StartCollectScanner();
    void StopCollectScanner();
    void UpdateCollectSprintSuppression();

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

    std::unique_ptr<CollectibleScanner> collect_scanner_;
    std::chrono::steady_clock::time_point collect_scan_last_at_ {};
    // Anti-stuck: position of the last detection-triggered collect attempt.
    NaviPosition collect_attempt_pos_ {};
    bool collect_attempt_pos_valid_ = false;
};

} // namespace mapnavigator
