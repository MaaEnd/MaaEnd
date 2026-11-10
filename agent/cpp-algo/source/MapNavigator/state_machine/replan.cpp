#include "replan.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <optional>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../planning/navmesh_path_expander.h"
#include "../planning/navmesh_queries.h"
#include "../semantic/semantic_helpers.h"
#include "../semantic/semantic_nodes.h"
#include "../zipline/zipline_action.h"
#include "internal.h"
#include "localization.h"

namespace mapnavigator
{

namespace
{

double arrival_band_for_startup_bypass(const Waypoint& waypoint)
{
    return std::max(ArrivalBand(waypoint, kMeasurementDefaultPositionQuantum), TraitsOf(waypoint.action).commit_distance);
}

TimePoint pause_for_planning(Navigator* nav)
{
    const TimePoint started_at = std::chrono::steady_clock::now();
    HoldPosition(nav->motion_controller);
    PauseWedge(&nav->runtime_state.offroute, started_at);
    return started_at;
}

void resume_after_planning(Navigator* nav, TimePoint started_at)
{
    ExcludeFromStallClocks(nav->session, std::chrono::steady_clock::now() - started_at);
}

bool apply_overlay_to_anchor(
    Navigator* nav,
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    const GeneratedWaypointOptions& options)
{
    if (!anchor.has_position) {
        LogWarn << "Dynamic navmesh overlay skipped: anchor has no position." << VAR(reason) << VAR(continue_index);
        return false;
    }

    const navmesh::WorldPoint start { .x = nav->position->x, .y = nav->position->y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    // 人不在网格上时规划会给出网格上的入口点, 先直走上去
    std::optional<navmesh::WorldPoint> start_recovery_entry;
    const TimePoint pause_started_at = pause_for_planning(nav);
    const auto route = PlanNavmeshRoute(
        *nav->param,
        nav->position->zone_id,
        {
            .start = start,
            .start_floor_y = WalkedFloorY(*nav->session, *nav->position),
            .start_deck_y = LandedTowerDeckY(*nav->session, *nav->position),
            .goal = goal,
            .goal_deck_y = anchor.target_deck_y,
            .zone_floor_y = nav->position->zone_floor_y,
        },
        nullptr,
        &nav->runtime_state.virtual_no_go,
        &start_recovery_entry);
    resume_after_planning(nav, pause_started_at);
    if (!route) {
        return false;
    }

    std::vector<Waypoint> generated_prefix;
    if (start_recovery_entry) {
        generated_prefix.push_back(Waypoint { .x = start_recovery_entry->x, .y = start_recovery_entry->y, .action = ActionType::RUN });
        generated_prefix.back().strict_arrival = false;
    }
    if (!AppendGeneratedNavmeshWaypoints(&generated_prefix, *nav->param, nav->position->zone_id, *route, options)) {
        LogWarn << "Dynamic navmesh overlay skipped: generated path is unusable." << VAR(reason) << VAR(continue_index)
                << VAR(route->path.points.size());
        return false;
    }
    if (generated_prefix.empty()
        && std::hypot(anchor.x - nav->position->x, anchor.y - nav->position->y) > arrival_band_for_startup_bypass(anchor)) {
        generated_prefix.push_back(Waypoint { .x = nav->position->x, .y = nav->position->y, .action = ActionType::RUN });
        LogInfo << "Dynamic overlay seeded leading node to avoid single-point path." << VAR(reason) << VAR(continue_index)
                << VAR(nav->position->x) << VAR(nav->position->y);
    }
    const size_t generated_count = generated_prefix.size();
    ApplyDynamicOverlay(nav->session, std::move(generated_prefix), continue_index, *nav->position);
    RestartRouteTracking(&nav->runtime_state.route);
    DropWedgeBaseline(&nav->runtime_state.offroute);
    MarkNavRunDirty(&nav->runtime_state);
    if (generated_count == 0
        && std::hypot(anchor.x - nav->position->x, anchor.y - nav->position->y) <= arrival_band_for_startup_bypass(anchor)) {
        ConfirmStartupAt(&nav->runtime_state.route, *nav->position);
    }
    ClearDynamicReplanRequest(&nav->runtime_state);
    const size_t planned_points = route->path.points.size();
    LogInfo << "Dynamic navmesh overlay selected." << VAR(reason) << VAR(continue_index) << VAR(generated_count) << VAR(planned_points)
            << VAR(nav->runtime_state.virtual_no_go.size()) << VAR(anchor.x) << VAR(anchor.y);
    return true;
}

// 圆心放在卡死点前方再让出 standoff, 卡死点本身不进禁区
double no_go_center(double origin, double direction, double radius)
{
    return origin + direction * (radius + kVirtualNoGoStandoff);
}

double fit_no_go_outside(const NaviPosition& position, const NaviPosition& origin, double dir_x, double dir_y, double radius)
{
    while (radius >= kVirtualNoGoRadiusMin
           && std::hypot(position.x - no_go_center(origin.x, dir_x, radius), position.y - no_go_center(origin.y, dir_y, radius)) < radius) {
        radius *= 0.5;
    }
    return radius;
}

// 判成够不着就丢链改走路, 返回 true 表示这一拍已处理完
bool give_up_unreachable_zipline(Navigator* nav, const char* reason)
{
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(*nav->param, *nav->session, *nav->position);
    if (!anchor || anchor->second.action != ActionType::ZIPLINE) {
        return false;
    }
    ZiplineApproachState& approach = nav->runtime_state.zipline_approach;
    if (approach.anchor_index != anchor->first) {
        RetargetZiplineApproach(&approach, anchor->first);
    }
    CountZiplineApproachReplan(&approach);
    if (approach.replans <= kZiplineApproachReplanBudget) {
        return false;
    }

    LogWarn << "Zipline mount tower unreachable after repeated replans; walking instead." << VAR(reason) << VAR(approach.replans)
            << VAR(anchor->second.x) << VAR(anchor->second.y) << VAR(nav->position->x) << VAR(nav->position->y);
    ClearDynamicReplanRequest(&nav->runtime_state);
    semantic_nodes::AbandonZipline(nav, "zipline_unreachable", reason);
    return true;
}

std::optional<size_t> find_authored_slice_begin(const Navigator* nav, const std::vector<Waypoint>& authored, const char* reason)
{
    size_t slice_begin = authored.size();
    for (size_t index = nav->session->current_node_idx; index < nav->session->current_path.size(); ++index) {
        const std::optional<size_t> canonical_index = CanonicalIndexAtCurrentPath(*nav->session, index);
        if (!canonical_index) {
            continue;
        }
        const size_t stamp = nav->session->original_path[*canonical_index].authored_group_begin;
        if (stamp < authored.size()) {
            slice_begin = stamp;
            break;
        }
    }
    if (slice_begin >= authored.size()) {
        LogWarn << "Authored route replan unavailable: no authored origin recorded past the current node." << VAR(reason)
                << VAR(nav->session->current_node_idx) << VAR(nav->session->current_path.size());
        return std::nullopt;
    }
    return slice_begin;
}

NaviParam make_replan_param(const Navigator* nav, const std::vector<Waypoint>& authored, size_t slice_begin)
{
    NaviParam replan_param = *nav->param;
    const HopLedger& ledger = nav->runtime_state.zipline_ride.ledger;
    const int32_t rope_failures = CountZiplineRopeFailures(ledger);
    if (rope_failures >= kZiplineAbandonWalkFallbackCount) {
        replan_param.zipline_enabled = false;
        LogWarn << "Authored route replan disables ziplines: too many rope failures this run." << VAR(rope_failures);
    }
    else {
        replan_param.zipline_ledger = ledger;
    }
    replan_param.rope_off.insert(replan_param.rope_off.end(), nav->rope_off.begin(), nav->rope_off.end());
    replan_param.path.assign(authored.begin() + static_cast<std::ptrdiff_t>(slice_begin), authored.end());
    return replan_param;
}

bool try_replan_remaining_authored_route(Navigator* nav, const char* reason)
{
    const std::vector<Waypoint>& authored = nav->param->authored_path;
    if (authored.empty()) {
        return false;
    }

    const std::optional<size_t> found_slice_begin = find_authored_slice_begin(nav, authored, reason);
    if (!found_slice_begin) {
        return false;
    }
    const size_t slice_begin = *found_slice_begin;

    const NaviParam replan_param = make_replan_param(nav, authored, slice_begin);
    std::vector<Waypoint> replanned;
    const TimePoint expand_started_at = pause_for_planning(nav);
    const bool expanded = ExpandNavmeshWaypoints(
        &replanned,
        nullptr,
        nav->zipline_outcome,
        replan_param,
        *nav->position,
        nav->maa_context,
        nullptr,
        &nav->runtime_state.virtual_no_go,
        LandedTowerDeckY(*nav->session, *nav->position));
    resume_after_planning(nav, expand_started_at);
    if (!expanded || replanned.empty()) {
        LogWarn << "Authored route replan failed to expand the remaining route." << VAR(reason) << VAR(slice_begin)
                << VAR(replan_param.path.size());
        return false;
    }
    // 展开出来的组首下标是切片内的偏移, 换算回完整作者路线
    OffsetAuthoredGroups(&replanned, slice_begin);

    ReplaceRoute(nav->session, std::move(replanned), *nav->position, reason);
    RestartRouteTracking(&nav->runtime_state.route);
    DropWedgeBaseline(&nav->runtime_state.offroute);
    MarkNavRunDirty(&nav->runtime_state);
    ClearDynamicReplanRequest(&nav->runtime_state);

    if (IsZiplineMounted(nav->runtime_state)) {
        if (semantic_nodes::SkipToHopUnderfoot(nav, reason)) {
            LogInfo << "Zipline recovery re-expanded the remaining authored route; the next hop leaves from this tower." << VAR(reason)
                    << VAR(slice_begin) << VAR(nav->session->current_path.size());
            return true;
        }
        semantic_nodes::LeaveZiplineTower(nav);
    }

    const TimePoint resolve_started_at = pause_for_planning(nav);
    const std::optional<DynamicAnchor> anchor = ResolveReachableNavmeshAnchor(*nav->param, *nav->session, *nav->position, 0, reason);
    resume_after_planning(nav, resolve_started_at);
    if (anchor) {
        TryApplyDynamicOverlayToAnchor(nav, reason, anchor->first, anchor->second);
    }
    LogInfo << "Zipline recovery re-expanded the remaining authored route." << VAR(reason) << VAR(slice_begin)
            << VAR(nav->session->current_path.size()) << VAR(nav->position->x) << VAR(nav->position->y) << VAR(nav->position->zone_id);
    return true;
}

} // namespace

bool TryApplyDynamicOverlayToAnchor(Navigator* nav, const char* reason, size_t continue_index, const Waypoint& anchor)
{
    return apply_overlay_to_anchor(nav, reason, continue_index, anchor, { .include_goal = false, .strict_segment_breaks = false });
}

bool TryApplyDynamicOverlayToNextAnchor(Navigator* nav, const char* reason)
{
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(*nav->param, *nav->session, *nav->position);
    if (!anchor) {
        ClearDynamicReplanRequest(&nav->runtime_state);
        LogInfo << "Dynamic navmesh overlay skipped: no future anchor." << VAR(reason) << VAR(nav->position->x) << VAR(nav->position->y)
                << VAR(nav->position->zone_id);
        return false;
    }
    return TryApplyDynamicOverlayToAnchor(nav, reason, anchor->first, anchor->second);
}

bool TryVirtualNoGoReplan(
    Navigator* nav,
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    const NaviPosition& origin,
    double stuck_heading)
{
    std::vector<VirtualNoGoDisc>& discs = nav->runtime_state.virtual_no_go;
    // 朝向是罗盘角: 0 朝上, 顺时针
    const double rad = NormalizeHeading(stuck_heading) * kPi / 180.0;
    const double dir_x = std::sin(rad);
    const double dir_y = -std::cos(rad);

    double radius = kVirtualNoGoRadius;
    bool grown = false;
    for (const VirtualNoGoDisc& disc : discs) {
        if (disc.zone_id != origin.zone_id || disc.push_through) {
            continue;
        }
        if (std::hypot(
                no_go_center(origin.x, dir_x, kVirtualNoGoRadius) - disc.x,
                no_go_center(origin.y, dir_y, kVirtualNoGoRadius) - disc.y)
            <= disc.radius + kVirtualNoGoRadiusStep) {
            radius = std::max(radius, std::min(disc.radius + kVirtualNoGoRadiusStep, kVirtualNoGoRadiusMax));
            grown = true;
        }
    }
    // 按锚点正好在前方的最坏情况取上界: 距离 - standoff - 半径 ≥ 半径 + 净空
    const double anchor_distance = std::hypot(anchor.x - origin.x, anchor.y - origin.y);
    const double radius_cap = (anchor_distance - kVirtualNoGoStandoff - kVirtualNoGoGoalClearance) * 0.5;
    radius = fit_no_go_outside(*nav->position, origin, dir_x, dir_y, std::min(radius, radius_cap));
    if (radius < kVirtualNoGoRadiusMin) {
        LogInfo << "Virtual no-go skipped: no room for a disc between here and the anchor." << VAR(reason) << VAR(anchor_distance)
                << VAR(radius);
        return false;
    }

    discs.push_back(
        { .zone_id = origin.zone_id,
          .x = no_go_center(origin.x, dir_x, radius),
          .y = no_go_center(origin.y, dir_y, radius),
          .radius = radius });
    VirtualNoGoDisc* disc = &discs.back();
    LogInfo << "Virtual no-go stamped." << VAR(reason) << VAR(grown) << VAR(disc->x) << VAR(disc->y) << VAR(disc->radius)
            << VAR(stuck_heading) << VAR(discs.size());

    if (TryApplyDynamicOverlayToAnchor(nav, reason, continue_index, anchor)) {
        return true;
    }
    const double shrunk = fit_no_go_outside(*nav->position, origin, dir_x, dir_y, disc->radius * 0.5);
    if (shrunk >= kVirtualNoGoRadiusMin) {
        disc->radius = shrunk;
        disc->x = no_go_center(origin.x, dir_x, shrunk);
        disc->y = no_go_center(origin.y, dir_y, shrunk);
        LogInfo << "Virtual no-go shrunk after replan failure." << VAR(reason) << VAR(disc->radius);
        if (TryApplyDynamicOverlayToAnchor(nav, reason, continue_index, anchor)) {
            return true;
        }
    }
    disc->push_through = true;
    LogWarn << "Virtual no-go blocks the only passage; marked push-through." << VAR(reason) << VAR(disc->x) << VAR(disc->y)
            << VAR(disc->radius);
    return false;
}

namespace
{

std::optional<bool> rejoin_authored_route(Navigator* nav, bool on_tower, bool on_mesh, int64_t elapsed_ms)
{
    bool rejoined = try_replan_remaining_authored_route(nav, "zipline_recovery_reexpand");
    if (!rejoined && on_tower) {
        LogWarn << "Zipline recovery could not re-expand from the tower; stepping down to retry from the ground." << VAR(elapsed_ms)
                << VAR(nav->position->x) << VAR(nav->position->y);
        semantic_nodes::LeaveZiplineTower(nav);
        BeginZiplineRecovery(&nav->runtime_state.zipline_recovery, std::chrono::steady_clock::now());
        return true;
    }
    if (!rejoined && on_mesh) {
        const TimePoint resolve_started_at = pause_for_planning(nav);
        const std::optional<DynamicAnchor> anchor =
            ResolveReachableNavmeshAnchor(*nav->param, *nav->session, *nav->position, nav->session->current_node_idx, "zipline_recovery");
        resume_after_planning(nav, resolve_started_at);
        if (anchor) {
            rejoined = TryApplyDynamicOverlayToAnchor(nav, "zipline_recovery", anchor->first, anchor->second);
        }
    }
    if (!rejoined) {
        return FailNavigation(
            nav,
            "zipline_recovery_route_unavailable",
            "Zipline recovery found no reachable point in the remaining route and could not re-expand the authored route; "
            "refusing to follow the stale departure route.",
            0.0,
            0.0,
            elapsed_ms);
    }
    return std::nullopt;
}

} // namespace

bool HandleZiplineRecoveryReplan(Navigator* nav)
{
    ZiplineRecoveryState& recovery = nav->runtime_state.zipline_recovery;
    HoldPosition(nav->motion_controller);
    const auto now = std::chrono::steady_clock::now();
    const int64_t elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - recovery.started_at).count();
    if (elapsed_ms >= kZiplineRecoveryTimeoutMs) {
        return FailNavigation(
            nav,
            "zipline_recovery_localization_timeout",
            "Zipline recovery could not obtain a stable on-mesh position; refusing to follow the stale departure route.",
            0.0,
            0.0,
            elapsed_ms);
    }

    const bool captured = recovery.stable_hits == 0 ? CaptureGlobalFix(nav) : CaptureTrackedFix(nav);
    if (!captured) {
        SleepFor(kZiplineRecoveryRetryIntervalMs);
        return true;
    }

    // 人在架子上时脚下就是架子, 不要求贴回网格
    const bool on_tower = IsZiplineMounted(nav->runtime_state);
    const navmesh::WorldPoint fix { .x = nav->position->x, .y = nav->position->y };
    const auto snap = NavmeshSnapAt(*nav->param, nav->position->zone_id, fix, nav->param->navmesh_snap_radius);
    const bool on_mesh = on_tower || (snap && snap->distance <= nav->param->navmesh_snap_radius);
    const bool same_fix =
        recovery.stable_hits > 0 && recovery.stable_pos.zone_id == nav->position->zone_id
        && std::hypot(recovery.stable_pos.x - nav->position->x, recovery.stable_pos.y - nav->position->y) <= kZiplineRecoveryStableRadiusWu;
    recovery.stable_pos = *nav->position;
    recovery.stable_hits = same_fix ? recovery.stable_hits + 1 : 1;
    if (recovery.stable_hits < kZiplineRecoveryStableFixes) {
        SleepFor(kZiplineRecoveryRetryIntervalMs);
        return true;
    }

    if (!nav->position->zone_id.empty()) {
        UpdateCurrentZone(nav->session, nav->position->zone_id);
    }
    LogInfo << "Zipline recovery position stabilized." << VAR(elapsed_ms) << VAR(recovery.stable_hits) << VAR(nav->position->x)
            << VAR(nav->position->y) << VAR(nav->position->zone_id);

    if (const std::optional<bool> done = rejoin_authored_route(nav, on_tower, on_mesh, elapsed_ms)) {
        return *done;
    }

    recovery = {};
    if (IsZiplineMounted(nav->runtime_state)) {
        semantic_nodes::StartZiplineHop(nav, CurrentWaypoint(*nav->session), 0.0);
        return true;
    }
    semantic_nodes::SelectPhaseOrFinishRoute(nav, "zipline_recovery");
    return true;
}

bool AbandonRope(Navigator* nav, uint32_t uid)
{
    nav->rope_off.push_back(uid);
    LogWarn << "Rope unavailable at the bottom; re-expanding the remaining route without it." << VAR(uid) << VAR(nav->position->x)
            << VAR(nav->position->y) << VAR(nav->rope_off.size());
    if (!try_replan_remaining_authored_route(nav, "rope_unavailable")) {
        return FailNavigation(
            nav,
            "rope_unavailable",
            "Rope prompt not recognized at the bottom and the remaining route could not be re-expanded without it.",
            0.0,
            0.0,
            0);
    }
    semantic_nodes::SelectPhaseOrFinishRoute(nav, "rope_unavailable");
    return true;
}

bool HandleDynamicReplanRequest(Navigator* nav, const char* reason)
{
    NoteFailure(&nav->failure_trail, std::chrono::steady_clock::now(), std::string("replan ") + reason);
    if (give_up_unreachable_zipline(nav, reason)) {
        return true;
    }
    if (TryApplyDynamicOverlayToNextAnchor(nav, reason)) {
        return true;
    }

    ClearDynamicReplanRequest(&nav->runtime_state);
    ResetRouteTracking(&nav->runtime_state.route);
    ResetProgress(nav->session);
    LogWarn << "Dynamic navmesh replan unavailable; falling back to current route." << VAR(reason) << VAR(nav->position->x)
            << VAR(nav->position->y) << VAR(nav->position->zone_id);
    semantic_nodes::SelectPhaseOrFinishRoute(nav, "dynamic_replan_fallback");
    return true;
}

} // namespace mapnavigator
