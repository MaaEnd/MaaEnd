#include "transit_wait.h"

#include <chrono>
#include <cmath>
#include <limits>
#include <string>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "../sensing/position_provider.h"
#include "semantic_helpers.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

size_t find_future_zone_declaration(const Context& ctx, const std::string& zone_id)
{
    const std::vector<Waypoint>& path = ctx.session->currentPath();
    for (size_t index = ctx.session->currentNodeIdx(); index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (waypoint.isZoneDeclaration() && !waypoint.zone_id.empty() && waypoint.zone_id == zone_id) {
            return index;
        }
    }
    return std::numeric_limits<size_t>::max();
}

Result finalize_portal_transit_zone(const Context& ctx, const std::string& zone_id, size_t matched_zone_index)
{
    if (matched_zone_index > ctx.session->currentNodeIdx()) {
        ctx.session->skipPastWaypoint(matched_zone_index - 1, "portal_zone_fast_forward");
    }
    ctx.session->updateCurrentZone(zone_id);
    ctx.session->resetProgress();
    ctx.runtime_state->onWaypointAdvance();
    ConsumeMatchedZoneNodes(ctx);
    StopMotionAndCommitment(ctx);
    ctx.position_provider->resetTracking();
    ctx.runtime_state->semantic.portal.phase = PortalTransitPhase::Reacquire;
    LogInfo << "Portal transit accepted zone transition." << VAR(zone_id) << VAR(matched_zone_index);
    return Handled();
}

} // namespace

Result TickPortalTransit(const Context& ctx)
{
    PortalTransitState& portal = ctx.runtime_state->semantic.portal;
    if (!portal.active()) {
        return {};
    }

    if (ElapsedMs(portal.started) > kZoneConfirmTimeoutMs) {
        return Failure("portal_transit_timeout", "PORTAL transit timed out before a valid zone landing was confirmed.");
    }

    if (portal.phase == PortalTransitPhase::Reacquire) {
        if (!ctx.position_provider->capture(ctx.position, false, ctx.session->currentZoneId())
            || ctx.position->zone_id != ctx.session->currentZoneId()) {
            return WaitNextFix(kZoneConfirmRetryIntervalMs);
        }
        portal = {};
        ctx.runtime_state->dynamic_replan_requested = true;
        LogInfo << "Portal transit landing confirmed." << VAR(ctx.position->zone_id);
        return Handled();
    }

    ctx.motion_controller->setForwardState(true);

    NaviPosition candidate;
    if (!ctx.position_provider->capture(&candidate, true, {})) {
        return WaitNextFix(kZoneConfirmRetryIntervalMs);
    }

    *ctx.position = candidate;
    if (candidate.zone_id.empty() || candidate.zone_id == ctx.session->currentZoneId()) {
        return WaitNextFix(kZoneConfirmRetryIntervalMs);
    }

    const size_t matched_zone_index = find_future_zone_declaration(ctx, candidate.zone_id);
    if (matched_zone_index == std::numeric_limits<size_t>::max()) {
        StopMotionAndCommitment(ctx);
        return WaitNextFix(kZoneConfirmRetryIntervalMs);
    }
    return finalize_portal_transit_zone(ctx, candidate.zone_id, matched_zone_index);
}

Result TickTransferWait(const Context& ctx)
{
    TransferWaitState& transfer = ctx.runtime_state->semantic.transfer;
    if (!ctx.session->hasCurrentWaypoint()) {
        transfer = {};
        ctx.session->noteRouteTailConsumed(*ctx.position, "route_tail_consumed");
        return Handled();
    }

    ctx.motion_controller->setForwardState(false);

    const auto now = std::chrono::steady_clock::now();
    if (IsUnset(transfer.started)) {
        transfer.started = now;
    }

    if (!ctx.position_provider->capture(ctx.position, false, {})) {
        if (ElapsedMs(transfer.started, now) > kRelocationWaitTimeoutMs) {
            return Failure("transfer_wait_timeout", "TRANSFER wait timed out before capture stabilized.");
        }
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    const int64_t waited_ms = ElapsedMs(transfer.started, now);
    const double moved_from_anchor = std::hypot(ctx.position->x - transfer.anchor.x, ctx.position->y - transfer.anchor.y);
    const bool movement_observed = ctx.position->zone_id != transfer.anchor.zone_id || moved_from_anchor >= kRelocationResumeMinDistance;
    if (!movement_observed) {
        transfer.moved_reads = 0;
        if (waited_ms > kRelocationWaitTimeoutMs) {
            return Failure("transfer_wait_timeout", "TRANSFER wait timed out without external movement.");
        }
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    if (++transfer.moved_reads < kRelocationStableFixes) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    transfer = {};
    return ResumeAfterRelocation(ctx, "transfer_wait_complete");
}

// 坐升降索时小地图坐标几乎不动, 所以按乘坐时长等满再要求连续取位成功
Result TickRopeRide(const Context& ctx)
{
    RopeRideState& rope = ctx.runtime_state->semantic.rope;
    ctx.motion_controller->setForwardState(false);
    const int64_t waited_ms = ElapsedMs(rope.started);
    if (waited_ms < rope.ride_ms) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }
    if (waited_ms > rope.ride_ms + kRelocationWaitTimeoutMs) {
        return Failure("rope_ride_timeout", "Rope ride finished but the position never stabilized.");
    }
    if (!ctx.position_provider->capture(ctx.position, false, {})) {
        rope.stable_reads = 0;
        return WaitNextFix(kRelocationRetryIntervalMs);
    }
    if (++rope.stable_reads < kRelocationStableFixes) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    LogInfo << "Rope ride finished." << VAR(waited_ms) << VAR(ctx.position->x) << VAR(ctx.position->y) << VAR(ctx.position->zone_id);
    rope = {};
    if (ctx.session->hasCurrentWaypoint() && ctx.session->currentWaypoint().rope_top) {
        ctx.session->updateCurrentZone(ctx.position->zone_id);
        const Waypoint& top = ctx.session->currentWaypoint();
        return CompleteArrival(ctx, top, ctx.session->currentAbsoluteNodeIndex(), "rope_ride_completed");
    }
    return ResumeAfterRelocation(ctx, "rope_ride_completed");
}

} // namespace semantic_nodes

} // namespace mapnavigator
