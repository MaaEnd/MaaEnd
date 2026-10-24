#pragma once

#include <chrono>
#include <cstdint>
#include <limits>
#include <optional>
#include <string>
#include <type_traits>
#include <utility>

#include <MaaUtils/Logger.h>

#include "../navi_log.h"
#include "../planning/nav_run_controller.h"
#include "../planning/route_tracker.h"
#include "navigation_state_machine.h"

namespace mapnavigator
{

using DynamicAnchor = std::pair<size_t, Waypoint>;

constexpr size_t kUnaddressableAnchorIndex = std::numeric_limits<size_t>::max();

bool ZonesDiffer(const std::string& a, const std::string& b);
bool IsZoneCompatible(const Waypoint& waypoint, const std::string& current_zone_id);
bool IsRequiredSemanticAnchor(const Waypoint& waypoint);
std::optional<DynamicAnchor> ResolveCurrentAnchorFrom(NavigationSession* session, const NaviPosition& position, size_t start_index);
std::optional<DynamicAnchor> ResolveCurrentAnchor(NavigationSession* session, const NaviPosition& position);
std::optional<DynamicAnchor> ResolveReachableNavmeshAnchor(
    const NaviParam& param,
    NavigationSession* session,
    const NaviPosition& position,
    size_t start_index,
    const char* reason);
double EffectiveRouteHeading(const NavRunTickResult& nav_run_result, const RouteTrackingState& route);

// Abort 让 Run 立即以失败收场
enum class TickStep
{
    Next,
    EndTick,
    Abort,
};

constexpr TickStep EndTickWith(bool keep_running)
{
    return keep_running ? TickStep::EndTick : TickStep::Abort;
}

// 依次执行, 返回第一个不是 Next 的结果; 没有返回值的步骤算作 Next
template <typename... Steps>
TickStep FirstToClaim(Steps&&... steps)
{
    TickStep result = TickStep::Next;
    const auto run = [&result](auto& step) {
        if constexpr (std::is_void_v<decltype(step())>) {
            step();
        }
        else {
            result = step();
        }
        return result == TickStep::Next;
    };
    static_cast<void>((run(steps) && ...));
    return result;
}

struct TickFix
{
    bool startup_grace_elapsed = false;
    double current_heading = 0.0;
    bool drops_turn_sends = false;
    bool arrow_lags_camera = false;
    bool steering_on_camera = false;
    double steer_heading = 0.0;
    bool degraded_fix = false;
    int64_t fix_age_ms = 0;
};

struct NavigationStateMachine::NavigateTick
{
    std::chrono::steady_clock::time_point started_at {};
    int64_t gap_ms = 0;
    int64_t capture_ms = 0;
    std::chrono::steady_clock::time_point now {};
    TickFix fix;
    RouteTrackingState route {};
    NavRunTickResult nav_run_result {};
    std::optional<size_t> nav_run_anchor_index;
    double effective_progress = 0.0;
    int64_t stalled_ms = 0;
    Waypoint waypoint {};
    double arrival_distance = 0.0;
    double heading_error = 0.0;
};

} // namespace mapnavigator
