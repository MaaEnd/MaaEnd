#pragma once

#include "navi_domain_types.h"

namespace mapnavigator
{

double CalcTargetRotation(double from_x, double from_y, double to_x, double to_y);
double CalcDeltaRotation(double current, double target);
double NormalizeAngle(double angle);
double NormalizeHeading(double angle);
double CalcElevation(const ZiplineNodeRef& from, const ZiplineNodeRef& to);

} // namespace mapnavigator
