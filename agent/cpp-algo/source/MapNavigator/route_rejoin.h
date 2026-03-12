#pragma once

#include <vector>

#include "navi_domain_types.h"

namespace mapnavigator
{

enum class RejoinDecisionType
{
    Abort,
    Waypoint,
    Segment,
};

struct RouteRejoinCandidate
{
    RejoinDecisionType decision = RejoinDecisionType::Waypoint;
    size_t continue_index = 0;
    double route_distance = 0.0;
    double score = 0.0;
};

struct RouteRejoinPlan
{
    bool abort = false;
    double nearest_route_distance = 0.0;
    std::vector<RouteRejoinCandidate> candidates;
};

class RouteRejoinPlanner
{
public:
    RouteRejoinPlanner(double abort_distance, int candidate_limit, int backtrack_window);

    RouteRejoinPlan Plan(const NaviPosition& pos, double heading_degrees, const std::vector<Waypoint>& path, size_t preferred_index) const;

private:
    double abort_distance_;
    int candidate_limit_;
    int backtrack_window_;
};

} // namespace mapnavigator
