#include "navi_domain_types.h"

#include <algorithm>
#include <utility>

#include "navi_config.h"

namespace mapnavigator
{

ActionTraits TraitsOf(ActionType action)
{
    switch (action) {
    case ActionType::RUN:
        return { .settles_at_arrival = true, .walk_approach = true };
    case ActionType::SPRINT:
        return { .strict_arrival = true, .settles_at_arrival = true };
    case ActionType::JUMP:
        return { .strict_arrival = true, .settles_at_arrival = true };
    case ActionType::FIGHT:
        return { .strict_arrival = true, .settles_at_arrival = true };
    case ActionType::INTERACT:
        return { .strict_arrival = true, .settles_at_arrival = true, .walk_approach = true };
    case ActionType::TRANSFER:
        return { .strict_arrival = true, .settles_at_arrival = true };
    // 不做末端纠正, 纠正途中就可能被跳板弹走
    case ActionType::JUMP_PAD:
        return { .strict_arrival = true, .walk_approach = true };
    case ActionType::PORTAL:
        return { .strict_arrival = true, .commit_distance = kPortalCommitDistance };
    case ActionType::HEADING:
        return {};
    case ActionType::NAVMESH:
        return { .strict_arrival = true, .settles_at_arrival = true, .walk_approach = true };
    case ActionType::ZONE:
        return {};
    case ActionType::COLLECT:
        return { .walk_approach = true };
    case ActionType::DIG:
        return {
            .strict_arrival = true,
            .settles_at_arrival = true,
            .walk_approach = true,
            .walk_at_startup = true,
        };
    case ActionType::FIND:
        return { .walk_approach = true };
    case ActionType::ZIPLINE:
        return { .strict_arrival = true, .walk_approach = true };
    case ActionType::TRIGGER:
        return { .strict_arrival = true, .settles_at_arrival = true };
    }
    return {};
}

bool MayOpenUi(ActionType action)
{
    switch (action) {
    case ActionType::RUN:
    case ActionType::SPRINT:
    case ActionType::JUMP:
    case ActionType::HEADING:
    case ActionType::NAVMESH:
    case ActionType::ZONE:
        return false;
    case ActionType::FIGHT:
    case ActionType::INTERACT:
    case ActionType::TRANSFER:
    case ActionType::JUMP_PAD:
    case ActionType::PORTAL:
    case ActionType::COLLECT:
    case ActionType::DIG:
    case ActionType::FIND:
    case ActionType::ZIPLINE:
    case ActionType::TRIGGER:
        return true;
    }
    return true;
}

bool RequiresStrictArrival(const Waypoint& waypoint)
{
    return waypoint.has_position && (waypoint.strict_arrival || TraitsOf(waypoint.action).strict_arrival);
}

double GetLookahead(const Waypoint& waypoint)
{
    if (!waypoint.has_position) {
        return 0.0;
    }
    if (RequiresStrictArrival(waypoint)) {
        return kStrictArrivalLookaheadRadius;
    }
    return kLookaheadRadius;
}

bool IsAsyncInteract(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::INTERACT && !waypoint.interact_text.empty();
}

bool IsRecInteract(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::INTERACT && waypoint.interact_rec;
}

bool StopsOnPromptDetection(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::COLLECT || IsAsyncInteract(waypoint);
}

double BaseArrivalBand(const Waypoint& waypoint, double position_quantum)
{
    const bool strict = RequiresStrictArrival(waypoint);
    return strict ? GetLookahead(waypoint) + position_quantum : GetLookahead(waypoint) + kWaypointArrivalSlack + position_quantum;
}

double ClampArrivalBandToCorridor(const Waypoint& waypoint, double band)
{
    if (RequiresStrictArrival(waypoint) || waypoint.corridor_clearance <= 0.0) {
        return band;
    }
    return std::min(band, std::max(waypoint.corridor_clearance, kMinArrivalBand));
}

double ArrivalBand(const Waypoint& waypoint, double position_quantum)
{
    double band = BaseArrivalBand(waypoint, position_quantum);
    if (StopsOnPromptDetection(waypoint)) {
        band = std::min(band, kCollectArrivalBandWu);
    }
    return ClampArrivalBandToCorridor(waypoint, band);
}

double UntightenedArrivalBand(const Waypoint& waypoint, double position_quantum)
{
    return ClampArrivalBandToCorridor(waypoint, BaseArrivalBand(waypoint, position_quantum));
}

bool SettlesAtArrival(const Waypoint& waypoint)
{
    if (!waypoint.has_position || !TraitsOf(waypoint.action).settles_at_arrival) {
        return false;
    }
    if (waypoint.action == ActionType::INTERACT) {
        return !IsAsyncInteract(waypoint);
    }
    return waypoint.authored_strict_arrival;
}

bool IsContinuousRun(const Waypoint& waypoint)
{
    return waypoint.has_position && waypoint.action == ActionType::RUN && !RequiresStrictArrival(waypoint);
}

bool IsHeadingOnly(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::HEADING;
}

bool IsFindPoint(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::FIND;
}

bool IsTriggerPoint(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::TRIGGER;
}

bool IsMovementPoint(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::RUN || waypoint.action == ActionType::NAVMESH;
}

bool EndsReplaceableSpan(const Waypoint& waypoint)
{
    return waypoint.route_required || !IsMovementPoint(waypoint);
}

bool IsZoneDeclaration(const Waypoint& waypoint)
{
    return waypoint.action == ActionType::ZONE;
}

Waypoint HeadingWaypoint(double angle)
{
    Waypoint waypoint;
    waypoint.action = ActionType::HEADING;
    waypoint.has_position = false;
    waypoint.strict_arrival = false;
    waypoint.heading_uses_target = false;
    waypoint.heading_angle = angle;
    return waypoint;
}

Waypoint HeadingToTargetWaypoint(double target_x, double target_y)
{
    Waypoint waypoint;
    waypoint.x = target_x;
    waypoint.y = target_y;
    waypoint.action = ActionType::HEADING;
    waypoint.has_position = false;
    waypoint.strict_arrival = false;
    waypoint.heading_uses_target = true;
    waypoint.heading_angle = 0.0;
    return waypoint;
}

Waypoint ZoneWaypoint(std::string zone)
{
    Waypoint waypoint;
    waypoint.action = ActionType::ZONE;
    waypoint.has_position = false;
    waypoint.strict_arrival = false;
    waypoint.zone_id = std::move(zone);
    return waypoint;
}

} // namespace mapnavigator
