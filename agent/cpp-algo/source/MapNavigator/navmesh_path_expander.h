#pragma once

#include <string>
#include <vector>

#include "navi_domain_types.h"

namespace mapnavigator
{

struct NaviParam;

std::string InitialExpectedZone(const NaviParam& param);
bool ExpandNavmeshWaypoints(const NaviParam& param, const NaviPosition& initial_pos, std::vector<Waypoint>& out_path);

} // namespace mapnavigator
