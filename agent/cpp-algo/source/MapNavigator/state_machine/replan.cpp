#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <limits>
#include <optional>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_math.h"
#include "../navi_time.h"
#include "../planning/navmesh_path_expander.h"
#include "../semantic/semantic_nodes.h"
#include "../zipline/zipline_action.h"
#include "internal.h"

namespace mapnavigator
{

namespace
{

double ArrivalBandForStartupBypass(const Waypoint& waypoint)
{
    return std::max(waypoint.ArrivalBand(kMeasurementDefaultPositionQuantum), waypoint.Traits().commit_distance);
}

template <typename Plan>
auto WithPlanningPause(MotionController* motion, NavigationRuntimeState* runtime, NavigationSession* session, Plan&& plan)
{
    const auto started_at = std::chrono::steady_clock::now();
    motion->SetForwardState(false);
    runtime->offroute.PauseAt(started_at);
    auto result = plan();
    session->ExcludeFromStallClocks(std::chrono::steady_clock::now() - started_at);
    return result;
}

} // namespace

bool NavigationStateMachine::TryApplyDynamicOverlayToAnchor(
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    bool emit_interior_corners)
{
    if (!anchor.HasPosition()) {
        LogWarn << "Dynamic navmesh overlay skipped: anchor has no position." << VAR(reason) << VAR(continue_index);
        return false;
    }

    const navmesh::WorldPoint start { .x = position_->x, .y = position_->y };
    const navmesh::WorldPoint goal { .x = anchor.x, .y = anchor.y };
    // 人不在网格上时规划会给出网格上的入口点, 先直走上去
    std::optional<navmesh::WorldPoint> start_recovery_entry;
    const auto route = WithPlanningPause(motion_controller_, &runtime_state_, session_, [&] {
        return PlanNavmeshRoute(
            param_,
            position_->zone_id,
            start,
            goal,
            anchor.target_deck_y,
            session_->WalkedFloorY(*position_),
            nullptr,
            &runtime_state_.virtual_no_go,
            session_->LandedTowerDeckY(*position_),
            &start_recovery_entry);
    });
    if (!route) {
        return false;
    }

    std::vector<Waypoint> generated_prefix;
    if (start_recovery_entry) {
        generated_prefix.emplace_back(start_recovery_entry->x, start_recovery_entry->y, ActionType::RUN);
        generated_prefix.back().strict_arrival = false;
    }
    if (!AppendGeneratedNavmeshWaypoints(
            param_,
            position_->zone_id,
            *route,
            generated_prefix,
            /*include_goal=*/false,
            emit_interior_corners,
            /*strict_segment_breaks=*/false)) {
        LogWarn << "Dynamic navmesh overlay skipped: generated path is unusable." << VAR(reason) << VAR(continue_index)
                << VAR(route->path.points.size());
        return false;
    }
    if (generated_prefix.empty() && std::hypot(anchor.x - position_->x, anchor.y - position_->y) > ArrivalBandForStartupBypass(anchor)) {
        generated_prefix.emplace_back(position_->x, position_->y, ActionType::RUN);
        LogInfo << "Dynamic overlay seeded leading node to avoid single-point path." << VAR(reason) << VAR(continue_index)
                << VAR(position_->x) << VAR(position_->y);
    }
    const size_t generated_count = generated_prefix.size();
    session_->ApplyDynamicOverlay(std::move(generated_prefix), continue_index, *position_);
    runtime_state_.route.Reset();
    runtime_state_.offroute.InvalidateBaseline();
    runtime_state_.nav_run_dirty = true;
    if (generated_count == 0 && std::hypot(anchor.x - position_->x, anchor.y - position_->y) <= ArrivalBandForStartupBypass(anchor)) {
        runtime_state_.route.ConfirmStartupAt(*position_);
    }
    runtime_state_.dynamic_replan_requested = false;
    const size_t planned_points = route->path.points.size();
    LogInfo << "Dynamic navmesh overlay selected." << VAR(reason) << VAR(continue_index) << VAR(generated_count) << VAR(planned_points)
            << VAR(runtime_state_.virtual_no_go.size()) << VAR(anchor.x) << VAR(anchor.y);
    return true;
}

bool NavigationStateMachine::TryApplyDynamicOverlayToNextAnchor(const char* reason)
{
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    if (!anchor) {
        runtime_state_.dynamic_replan_requested = false;
        LogInfo << "Dynamic navmesh overlay skipped: no future anchor." << VAR(reason) << VAR(position_->x) << VAR(position_->y)
                << VAR(position_->zone_id);
        return false;
    }
    return TryApplyDynamicOverlayToAnchor(reason, anchor->first, anchor->second, /*emit_interior_corners=*/false);
}

bool NavigationStateMachine::TryVirtualNoGoReplan(
    const char* reason,
    size_t continue_index,
    const Waypoint& anchor,
    const NaviPosition& origin,
    double stuck_heading)
{
    std::vector<VirtualNoGoDisc>& discs = runtime_state_.virtual_no_go;
    // 朝向是罗盘角: 0 朝上, 顺时针
    const double rad = NaviMath::NormalizeHeading(stuck_heading) * kPi / 180.0;
    const double dir_x = std::sin(rad);
    const double dir_y = -std::cos(rad);
    // 圆心放在卡死点前方再让出 standoff, 卡死点本身不进禁区
    const auto center_x = [&](double radius) {
        return origin.x + dir_x * (radius + kVirtualNoGoStandoff);
    };
    const auto center_y = [&](double radius) {
        return origin.y + dir_y * (radius + kVirtualNoGoStandoff);
    };
    const auto fit_outside = [&](double radius) {
        while (radius >= kVirtualNoGoRadiusMin && std::hypot(position_->x - center_x(radius), position_->y - center_y(radius)) < radius) {
            radius *= 0.5;
        }
        return radius;
    };

    double radius = kVirtualNoGoRadius;
    bool grown = false;
    for (const VirtualNoGoDisc& disc : discs) {
        if (disc.zone_id != origin.zone_id || disc.push_through) {
            continue;
        }
        if (std::hypot(center_x(kVirtualNoGoRadius) - disc.x, center_y(kVirtualNoGoRadius) - disc.y)
            <= disc.radius + kVirtualNoGoRadiusStep) {
            radius = std::max(radius, std::min(disc.radius + kVirtualNoGoRadiusStep, kVirtualNoGoRadiusMax));
            grown = true;
        }
    }
    // 按锚点正好在前方的最坏情况取上界: 距离 - standoff - 半径 ≥ 半径 + 净空
    const double anchor_distance = std::hypot(anchor.x - origin.x, anchor.y - origin.y);
    const double radius_cap = (anchor_distance - kVirtualNoGoStandoff - kVirtualNoGoGoalClearance) * 0.5;
    radius = fit_outside(std::min(radius, radius_cap));
    if (radius < kVirtualNoGoRadiusMin) {
        LogInfo << "Virtual no-go skipped: no room for a disc between here and the anchor." << VAR(reason) << VAR(anchor_distance)
                << VAR(radius);
        return false;
    }

    discs.push_back({ .zone_id = origin.zone_id, .x = center_x(radius), .y = center_y(radius), .radius = radius });
    VirtualNoGoDisc* disc = &discs.back();
    LogInfo << "Virtual no-go stamped." << VAR(reason) << VAR(grown) << VAR(disc->x) << VAR(disc->y) << VAR(disc->radius)
            << VAR(stuck_heading) << VAR(discs.size());

    if (TryApplyDynamicOverlayToAnchor(reason, continue_index, anchor)) {
        return true;
    }
    const double shrunk = fit_outside(disc->radius * 0.5);
    if (shrunk >= kVirtualNoGoRadiusMin) {
        disc->radius = shrunk;
        disc->x = center_x(shrunk);
        disc->y = center_y(shrunk);
        LogInfo << "Virtual no-go shrunk after replan failure." << VAR(reason) << VAR(disc->radius);
        if (TryApplyDynamicOverlayToAnchor(reason, continue_index, anchor)) {
            return true;
        }
    }
    disc->push_through = true;
    LogWarn << "Virtual no-go blocks the only passage; marked push-through." << VAR(reason) << VAR(disc->x) << VAR(disc->y)
            << VAR(disc->radius);
    return false;
}

bool NavigationStateMachine::GiveUpUnreachableZipline(const char* reason)
{
    const std::optional<DynamicAnchor> anchor = ResolveCurrentAnchor(session_, *position_);
    if (!anchor || anchor->second.action != ActionType::ZIPLINE) {
        return false;
    }
    ZiplineApproachState& approach = runtime_state_.zipline_approach;
    if (approach.anchor_index != anchor->first) {
        approach.Retarget(anchor->first);
    }
    if (++approach.replans <= kZiplineApproachReplanBudget) {
        return false;
    }

    LogWarn << "Zipline mount tower unreachable after repeated replans; walking instead." << VAR(reason) << VAR(approach.replans)
            << VAR(anchor->second.x) << VAR(anchor->second.y) << VAR(position_->x) << VAR(position_->y);
    runtime_state_.dynamic_replan_requested = false;
    semantic_nodes::AbandonZipline(SemanticContext(), "zipline_unreachable", reason);
    return true;
}

bool NavigationStateMachine::HandleZiplineRecoveryReplan()
{
    ZiplineRecoveryState& recovery = runtime_state_.zipline_recovery;
    StopMotion();
    const auto now = std::chrono::steady_clock::now();
    const int64_t elapsed_ms = std::chrono::duration_cast<std::chrono::milliseconds>(now - recovery.started_at).count();
    if (elapsed_ms >= kZiplineRecoveryTimeoutMs) {
        return FailNavigation(
            "zipline_recovery_localization_timeout",
            "Zipline recovery could not obtain a stable on-mesh position; refusing to follow the stale departure route.",
            0.0,
            0.0,
            elapsed_ms);
    }

    const bool force_global_search = recovery.stable_hits == 0;
    if (!CaptureCurrentPosition(force_global_search)) {
        SleepFor(kZiplineRecoveryRetryIntervalMs);
        return true;
    }

    // 人在架子上时脚下就是架子, 不要求贴回网格
    const bool on_tower = runtime_state_.IsZiplineMounted();
    const navmesh::WorldPoint fix { .x = position_->x, .y = position_->y };
    const auto snap = NavmeshSnapAt(param_, position_->zone_id, fix, param_.navmesh_snap_radius);
    const bool on_mesh = on_tower || (snap && snap->distance <= param_.navmesh_snap_radius);
    const bool same_fix =
        recovery.stable_hits > 0 && recovery.stable_pos.zone_id == position_->zone_id
        && std::hypot(recovery.stable_pos.x - position_->x, recovery.stable_pos.y - position_->y) <= kZiplineRecoveryStableRadiusWu;
    recovery.stable_pos = *position_;
    recovery.stable_hits = same_fix ? recovery.stable_hits + 1 : 1;
    if (recovery.stable_hits < kZiplineRecoveryStableFixes) {
        SleepFor(kZiplineRecoveryRetryIntervalMs);
        return true;
    }

    if (!position_->zone_id.empty()) {
        session_->UpdateCurrentZone(position_->zone_id);
    }
    LogInfo << "Zipline recovery position stabilized." << VAR(elapsed_ms) << VAR(recovery.stable_hits) << VAR(position_->x)
            << VAR(position_->y) << VAR(position_->zone_id);

    bool rejoined = TryReplanRemainingAuthoredRoute("zipline_recovery_reexpand");
    if (!rejoined && on_tower) {
        LogWarn << "Zipline recovery could not re-expand from the tower; stepping down to retry from the ground." << VAR(elapsed_ms)
                << VAR(position_->x) << VAR(position_->y);
        semantic_nodes::LeaveZiplineTower(SemanticContext());
        recovery.Begin(std::chrono::steady_clock::now());
        return true;
    }
    if (!rejoined && on_mesh) {
        const std::optional<DynamicAnchor> anchor = WithPlanningPause(motion_controller_, &runtime_state_, session_, [&] {
            return ResolveReachableNavmeshAnchor(param_, session_, *position_, session_->current_node_idx(), "zipline_recovery");
        });
        rejoined = anchor && TryApplyDynamicOverlayToAnchor("zipline_recovery", anchor->first, anchor->second);
    }
    if (!rejoined) {
        return FailNavigation(
            "zipline_recovery_route_unavailable",
            "Zipline recovery found no reachable point in the remaining route and could not re-expand the authored route; "
            "refusing to follow the stale departure route.",
            0.0,
            0.0,
            elapsed_ms);
    }

    recovery.Reset();
    if (runtime_state_.IsZiplineMounted()) {
        semantic_nodes::StartZiplineHop(SemanticContext(), session_->CurrentWaypoint(), 0.0);
        return true;
    }
    SelectPhaseForCurrentWaypoint("zipline_recovery");
    return true;
}

bool NavigationStateMachine::TryReplanRemainingAuthoredRoute(const char* reason)
{
    const std::vector<Waypoint>& authored = param_.authored_path;
    if (authored.empty()) {
        return false;
    }

    size_t slice_begin = authored.size();
    for (size_t index = session_->current_node_idx(); index < session_->current_path().size(); ++index) {
        const std::optional<size_t> canonical_index = session_->CanonicalIndexAtCurrentPath(index);
        if (!canonical_index) {
            continue;
        }
        const size_t stamp = session_->original_path()[*canonical_index].authored_group_begin;
        if (stamp < authored.size()) {
            slice_begin = stamp;
            break;
        }
    }
    if (slice_begin >= authored.size()) {
        LogWarn << "Authored route replan unavailable: no authored origin recorded past the current node." << VAR(reason)
                << VAR(session_->current_node_idx()) << VAR(session_->current_path().size());
        return false;
    }

    NaviParam replan_param = param_;
    const HopLedger& ledger = runtime_state_.zipline_ride.ledger();
    const int32_t rope_failures = CountZiplineRopeFailures(ledger);
    if (rope_failures >= kZiplineAbandonWalkFallbackCount) {
        replan_param.zipline_enabled = false;
        LogWarn << "Authored route replan disables ziplines: too many rope failures this run." << VAR(rope_failures);
    }
    else {
        replan_param.zipline_ledger = ledger;
    }
    replan_param.rope_off.insert(replan_param.rope_off.end(), rope_off_.begin(), rope_off_.end());
    replan_param.path.assign(authored.begin() + static_cast<std::ptrdiff_t>(slice_begin), authored.end());
    std::vector<Waypoint> replanned;
    const bool expanded = WithPlanningPause(motion_controller_, &runtime_state_, session_, [&] {
        return ExpandNavmeshWaypoints(
            replan_param,
            *position_,
            should_stop_,
            replanned,
            nullptr,
            &runtime_state_.virtual_no_go,
            session_->LandedTowerDeckY(*position_));
    });
    if (!expanded || replanned.empty()) {
        LogWarn << "Authored route replan failed to expand the remaining route." << VAR(reason) << VAR(slice_begin)
                << VAR(replan_param.path.size());
        return false;
    }
    // 展开出来的组首下标是切片内的偏移, 换算回完整作者路线
    for (Waypoint& waypoint : replanned) {
        if (waypoint.authored_group_begin != std::numeric_limits<size_t>::max()) {
            waypoint.authored_group_begin += slice_begin;
        }
    }

    session_->ReplaceRoute(std::move(replanned), *position_, reason);
    runtime_state_.route.Reset();
    runtime_state_.offroute.InvalidateBaseline();
    runtime_state_.nav_run_dirty = true;
    runtime_state_.dynamic_replan_requested = false;

    if (runtime_state_.IsZiplineMounted()) {
        const semantic_nodes::Context ctx = SemanticContext();
        if (semantic_nodes::SkipToHopUnderfoot(ctx, reason)) {
            LogInfo << "Zipline recovery re-expanded the remaining authored route; the next hop leaves from this tower." << VAR(reason)
                    << VAR(slice_begin) << VAR(session_->current_path().size());
            return true;
        }
        semantic_nodes::LeaveZiplineTower(ctx);
    }

    const std::optional<DynamicAnchor> anchor = WithPlanningPause(motion_controller_, &runtime_state_, session_, [&] {
        return ResolveReachableNavmeshAnchor(param_, session_, *position_, 0, reason);
    });
    if (anchor) {
        TryApplyDynamicOverlayToAnchor(reason, anchor->first, anchor->second);
    }
    LogInfo << "Zipline recovery re-expanded the remaining authored route." << VAR(reason) << VAR(slice_begin)
            << VAR(session_->current_path().size()) << VAR(position_->x) << VAR(position_->y) << VAR(position_->zone_id);
    return true;
}

bool NavigationStateMachine::AbandonRope(uint32_t uid)
{
    rope_off_.push_back(uid);
    LogWarn << "Rope unavailable at the bottom; re-expanding the remaining route without it." << VAR(uid) << VAR(position_->x)
            << VAR(position_->y) << VAR(rope_off_.size());
    if (!TryReplanRemainingAuthoredRoute("rope_unavailable")) {
        return FailNavigation(
            "rope_unavailable",
            "Rope prompt not recognized at the bottom and the remaining route could not be re-expanded without it.",
            0.0,
            0.0,
            0);
    }
    SelectPhaseForCurrentWaypoint("rope_unavailable");
    return true;
}

bool NavigationStateMachine::HandleDynamicReplanRequest(const char* reason)
{
    if (GiveUpUnreachableZipline(reason)) {
        return true;
    }
    if (TryApplyDynamicOverlayToNextAnchor(reason)) {
        return true;
    }

    runtime_state_.dynamic_replan_requested = false;
    runtime_state_.route.ResetTracking();
    session_->ResetProgress();
    LogWarn << "Dynamic navmesh replan unavailable; falling back to current route." << VAR(reason) << VAR(position_->x) << VAR(position_->y)
            << VAR(position_->zone_id);
    SelectPhaseForCurrentWaypoint("dynamic_replan_fallback");
    return true;
}

} // namespace mapnavigator
