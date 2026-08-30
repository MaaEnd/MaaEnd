#pragma once

#include <array>
#include <chrono>
#include <functional>
#include <memory>
#include <optional>
#include <string>
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

class IActionExecutor;
class ActionWrapper;
class MotionController;
class PositionProvider;
class RoiTemplateScanner;
struct RouteTrackingState;

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
        IActionExecutor* action_executor,
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
    };

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
    // 走不到的上索点在这里让路: 判成够不着就丢掉这条链改走路, 返回 true 表示这一拍已经处理完。
    bool GiveUpUnreachableZipline(const char* reason);
    bool HandleZiplineRecoveryReplan();
    // 滑索恢复的下一级回退: 剩余展开路径全够不着时, 切出剩余作者路线重新展开并整条换路。
    // 滑索照常参与重展开, 只有判死过的跳被封禁; 弃索次数太多才整段退回纯走路。
    bool TryReplanRemainingAuthoredRoute(const char* reason);
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
    // 架子的交互提示出现就算够得着了。预筛看错时返回 false, 这一拍照常往前走
    bool TryZiplineMountPrompt(const Waypoint& waypoint, const RouteTrackingState& route);
    void UpdatePromptSprintSuppression();
    // Distance and kind of the nearest prompt-driven point; distance_sq is -1 when the route has none.
    PromptDistance NearestPromptDistance() const;
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
    // 上索提示的行进预筛。只复用扫描器, 不走提示动作那套: 那套认不出也照样推进路线,
    // 而滑索认不出提示就得丢链改徒步
    std::unique_ptr<RoiTemplateScanner> zipline_mount_scanner_;
    // Declared last so its destructor runs first — restores jogging while its collaborators are still alive.
    walkmode::Toggle walk_mode_;
};

} // namespace mapnavigator
