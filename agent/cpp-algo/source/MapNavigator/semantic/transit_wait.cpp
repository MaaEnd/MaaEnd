#include "transit_wait.h"

#include <chrono>
#include <cmath>
#include <optional>
#include <string>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../control/motion_controller.h"
#include "../navi_config.h"
#include "../navi_time.h"
#include "../sensing/position_provider.h"
#include "../state_machine/navigation_state_machine.h"
#include "semantic_helpers.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

std::optional<size_t> find_future_zone_declaration(Navigator* nav, const std::string& zone_id)
{
    const std::vector<Waypoint>& path = nav->session->current_path;
    for (size_t index = nav->session->current_node_idx; index < path.size(); ++index) {
        const Waypoint& waypoint = path[index];
        if (IsZoneDeclaration(waypoint) && !waypoint.zone_id.empty() && waypoint.zone_id == zone_id) {
            return index;
        }
    }
    return std::nullopt;
}

Result finalize_portal_transit_zone(Navigator* nav, const std::string& zone_id, size_t matched_zone_index)
{
    if (matched_zone_index > nav->session->current_node_idx) {
        NoteInteraction(nav->session);
        LeaveWaypointsThrough(nav->session, matched_zone_index - 1, "portal_zone_fast_forward");
    }
    UpdateCurrentZone(nav->session, zone_id);
    ResetProgress(nav->session);
    ResetOnWaypointAdvance(&nav->runtime_state);
    ConsumeMatchedZoneNodes(nav);
    HoldAndArmCameraAlign(nav);
    ResetSensorTracking(nav->position_provider);
    nav->runtime_state.semantic.portal.phase = PortalTransitPhase::Reacquire;
    LogInfo << "Portal transit accepted zone transition." << VAR(zone_id) << VAR(matched_zone_index);
    return Handled();
}

} // namespace

Result TickPortalTransit(Navigator* nav)
{
    PortalTransitState& portal = nav->runtime_state.semantic.portal;
    if (!IsPortalTransitActive(portal)) {
        return {};
    }

    if (ElapsedMs(portal.started) > kZoneConfirmTimeoutMs) {
        return Failure("portal_transit_timeout", "PORTAL transit timed out before a valid zone landing was confirmed.");
    }

    if (portal.phase == PortalTransitPhase::Reacquire) {
        const bool captured = CaptureTracked(CapturePartsOf(*nav), nav->position, nav->session->current_zone_id);
        if (!captured || nav->position->zone_id != nav->session->current_zone_id) {
            return WaitNextFix(kZoneConfirmRetryIntervalMs);
        }
        EndPortalTransit(&portal);
        RequestDynamicReplan(&nav->runtime_state);
        LogInfo << "Portal transit landing confirmed." << VAR(nav->position->zone_id);
        return Handled();
    }

    MoveForward(nav->motion_controller);

    NaviPosition candidate;
    if (!CaptureGlobal(CapturePartsOf(*nav), &candidate, {})) {
        return WaitNextFix(kZoneConfirmRetryIntervalMs);
    }

    *nav->position = candidate;
    if (candidate.zone_id.empty() || candidate.zone_id == nav->session->current_zone_id) {
        return WaitNextFix(kZoneConfirmRetryIntervalMs);
    }

    const std::optional<size_t> matched_zone_index = find_future_zone_declaration(nav, candidate.zone_id);
    if (!matched_zone_index) {
        HoldAndArmCameraAlign(nav);
        return WaitNextFix(kZoneConfirmRetryIntervalMs);
    }
    return finalize_portal_transit_zone(nav, candidate.zone_id, *matched_zone_index);
}

Result TickTransferWait(Navigator* nav)
{
    TransferWaitState& transfer = nav->runtime_state.semantic.transfer;
    if (!HasCurrentWaypoint(*nav->session)) {
        ClearTransferWait(&transfer);
        ConcludeRouteTail(nav->session, *nav->position, "route_tail_consumed");
        return Handled();
    }

    HoldPosition(nav->motion_controller);

    const auto now = std::chrono::steady_clock::now();
    if (IsUnset(transfer.started)) {
        transfer.started = now;
    }

    if (!CaptureTracked(CapturePartsOf(*nav), nav->position, {})) {
        if (ElapsedMs(transfer.started, now) > kRelocationWaitTimeoutMs) {
            return Failure("transfer_wait_timeout", "TRANSFER wait timed out before capture stabilized.");
        }
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    const int64_t waited_ms = ElapsedMs(transfer.started, now);
    const double moved_from_anchor = std::hypot(nav->position->x - transfer.anchor.x, nav->position->y - transfer.anchor.y);
    const bool movement_observed = nav->position->zone_id != transfer.anchor.zone_id || moved_from_anchor >= kRelocationResumeMinDistance;
    if (!movement_observed) {
        transfer.moved_reads = 0;
        if (waited_ms > kRelocationWaitTimeoutMs) {
            return Failure("transfer_wait_timeout", "TRANSFER wait timed out without external movement.");
        }
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    ++transfer.moved_reads;
    if (transfer.moved_reads < kRelocationStableFixes) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    ClearTransferWait(&transfer);
    ResumeAfterRelocation(nav, "transfer_wait_complete");
    return Handled();
}

// 坐升降索时小地图坐标几乎不动, 所以按乘坐时长等满再要求连续取位成功
Result TickRopeRide(Navigator* nav)
{
    RopeRideState& rope = nav->runtime_state.semantic.rope;
    HoldPosition(nav->motion_controller);
    const int64_t waited_ms = ElapsedMs(rope.started);
    if (waited_ms < rope.ride_ms) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }
    if (waited_ms > rope.ride_ms + kRelocationWaitTimeoutMs) {
        return Failure("rope_ride_timeout", "Rope ride finished but the position never stabilized.");
    }
    if (!CaptureTracked(CapturePartsOf(*nav), nav->position, {})) {
        rope.stable_reads = 0;
        return WaitNextFix(kRelocationRetryIntervalMs);
    }
    ++rope.stable_reads;
    if (rope.stable_reads < kRelocationStableFixes) {
        return WaitNextFix(kRelocationRetryIntervalMs);
    }

    LogInfo << "Rope ride finished." << VAR(waited_ms) << VAR(nav->position->x) << VAR(nav->position->y) << VAR(nav->position->zone_id);
    rope = {};
    if (HasCurrentWaypoint(*nav->session) && CurrentWaypoint(*nav->session).rope_top) {
        UpdateCurrentZone(nav->session, nav->position->zone_id);
        const Waypoint& top = CurrentWaypoint(*nav->session);
        CompleteArrival(nav, top, CurrentAbsoluteNodeIndex(*nav->session), "rope_ride_completed");
        return Handled();
    }
    ResumeAfterRelocation(nav, "rope_ride_completed");
    return Handled();
}

} // namespace semantic_nodes

} // namespace mapnavigator
