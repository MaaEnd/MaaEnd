#pragma once

#include <chrono>
#include <cstdint>
#include <memory>
#include <optional>
#include <string>
#include <vector>

#include "../control/obstacle_device_recovery.h"
#include "../control/walk_mode.h"
#include "../navi_param.h"
#include "../planning/nav_run_controller.h"
#include "../planning/navigation_session.h"
#include "../semantic/async_prompt_action.h"
#include "failure_trail.h"
#include "navigation_runtime_state.h"

namespace mapnavigator
{

struct Motion;
struct PositionSensor;
class RoiTemplateScanner;
struct InputBackend;

std::optional<size_t> ResolveRouteResumeIndex(const std::vector<Waypoint>& path, const NaviPosition& position);

struct Navigator
{
    const NaviParam* param = nullptr;
    InputBackend* input_backend = nullptr;
    PositionSensor* position_provider = nullptr;
    Session* session = nullptr;
    Motion* motion_controller = nullptr;
    NaviPosition* position = nullptr;
    MaaContext* maa_context = nullptr;
    NavigationRuntimeState runtime_state {};
    NavRun nav_run {};
    FailureTrail failure_trail {};
    std::chrono::steady_clock::time_point last_global_relocalize_at {};
    std::optional<uint64_t> prior_observation_epoch {};
    std::optional<NaviPhase> last_tick_phase {};
    // 只用于诊断
    int consecutive_prior_uses = 0;
    // 本次导航封禁的升降索底端 uid
    std::vector<uint32_t> rope_off {};

    AsyncPromptAction collect_prompt {};
    AsyncPromptAction interact_prompt {};

    ObstacleDeviceRecovery device_recovery {};
    std::unique_ptr<RoiTemplateScanner> zipline_mount_scanner {};
    walkmode::Toggle walk_mode {};
};

// 调用方须紧接着用 walkmode::RestoreJogOnExit 守住 walk_mode
Navigator MakeNavigator(
    const NaviParam& param,
    InputBackend* input_backend,
    PositionSensor* position_provider,
    Session* session,
    Motion* motion_controller,
    NaviPosition* position,
    MaaContext* maa_context);
bool RunNavigator(Navigator* nav);
void AdvanceHeadingEpochOnPhaseChange(Navigator* nav, NaviPhase phase);
bool TickPhase(Navigator* nav, NaviPhase phase);
void StopScanners(Navigator* nav);
bool ResumeAfterEscape(Navigator* nav, const char* reason);
bool FailNavigation(
    Navigator* nav,
    const char* reason,
    const char* log_message,
    double current_distance,
    double yaw_error,
    int64_t stalled_ms);

} // namespace mapnavigator
