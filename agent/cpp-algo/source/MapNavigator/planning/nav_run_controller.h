#pragma once

#include <chrono>
#include <cstddef>
#include <cstdint>
#include <functional>
#include <limits>
#include <optional>
#include <string>
#include <vector>

#include "../../Navmesh/BaseNavPlanner.h"
#include "../navi_domain_types.h"

namespace mapnavigator
{

struct NaviParam;
struct NavigationSession;
struct NavigationRuntimeState;
struct RouteTrackingState;

enum class NavRunReplanReason
{
    None,
    AnchorChanged,
    ZoneChanged,
    OffCorridor,
    ProgressRegression,
};

struct NavRunSpeedSample
{
    std::chrono::steady_clock::time_point at {};
    uint64_t tick_seq = 0;
    double x = 0.0;
    double y = 0.0;
};

struct NavRunPlan
{
    bool valid = false;
    std::string zone_id;
    size_t anchor_index = std::numeric_limits<size_t>::max();
    NaviPosition anchor_pos {};
    navmesh::WorldPath path;
    std::vector<double> corridor_arc_prefix;
    // path 直接取手写 RUN 线, 未经 navmesh 规划
    bool literal = false;
    size_t cursor = 0;
    std::chrono::steady_clock::time_point planned_at {};
    std::chrono::steady_clock::time_point last_soft_replan_at {};
    int soft_replan_attempts = 0;
};

// distance 是投影点沿走廊到拐点的距离, turn_deg 相对当前所在那段累计
struct SharpCorner
{
    double distance = 0.0;
    double turn_deg = 0.0;
    double lead_distance = 0.0;
    navmesh::WorldPoint point {};
};

struct NavRunTickResult
{
    bool has_corridor_heading = false;
    double corridor_heading = 0.0;
    navmesh::WorldPoint lookahead_point {};
    // 走廊上的垂足, cross_track 不带符号, 靠它和 lookahead_point 判左右
    navmesh::WorldPoint projection_point {};
    double cross_track = std::numeric_limits<double>::infinity();
    double remaining_to_anchor = std::numeric_limits<double>::infinity();
    // 有锚点就有值, 走廊不可用时也算, 不能和走廊弧长 remaining_to_anchor 混用
    double straight_to_anchor = std::numeric_limits<double>::infinity();
    double upcoming_turn_deg = 0.0;
    std::optional<SharpCorner> corner;
    std::optional<double> step_per_tick;
    NavRunReplanReason replanned_with = NavRunReplanReason::None;
    size_t passed_run_waypoints = 0;
    // 本拍同步规划耗时, 调用方要把无进展计时顺延这么久
    std::chrono::steady_clock::duration planning {};
};

class NavRunController
{
public:
    // halt 在同步规划前调用, 用来松开前进键
    NavRunTickResult tick(
        NavigationSession* session,
        NavigationRuntimeState* runtime,
        const NaviPosition& position,
        const RouteTrackingState& route,
        const NaviParam& param,
        size_t anchor_index,
        const Waypoint& anchor,
        std::chrono::steady_clock::time_point now,
        const std::function<void()>& halt);

    void invalidate();

    const NavRunPlan& plan() const { return plan_; }

private:
    bool buildPlan(
        const NaviParam& param,
        const NavigationSession& session,
        const NavigationRuntimeState& runtime,
        const NaviPosition& position,
        size_t anchor_index,
        const Waypoint& anchor,
        NavRunReplanReason reason,
        std::chrono::steady_clock::time_point now,
        const std::function<void()>& halt,
        std::chrono::steady_clock::duration& planning);

    NavRunReplanReason detectReplanTrigger(const RouteTrackingState& route, std::chrono::steady_clock::time_point now) const;

    double chooseLookaheadDistance(const RouteTrackingState& route, std::optional<double> step) const;
    double chooseTurnCommitDistance(double lookahead_distance, std::optional<double> step) const;

    void recordSpeedSample(const NaviPosition& position, std::chrono::steady_clock::time_point now);

    std::optional<double> estimateStepPerTick() const;

    NavRunPlan plan_;
    std::vector<NavRunSpeedSample> speed_samples_;
    uint64_t tick_seq_ = 0;
    std::chrono::steady_clock::time_point last_progress_seen_ {};
    double last_remaining_to_anchor_ = std::numeric_limits<double>::infinity();
    size_t failed_build_anchor_ = std::numeric_limits<size_t>::max();
    std::chrono::steady_clock::time_point failed_build_at_ {};
};

} // namespace mapnavigator
