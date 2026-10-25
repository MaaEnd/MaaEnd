#pragma once

#include "navi_domain_types.h"

namespace mapnavigator
{

class NaviMath
{
public:
    static double calcTargetRotation(double from_x, double from_y, double to_x, double to_y);
    static double calcDeltaRotation(double current, double target);
    static double normalizeAngle(double angle);
    static double normalizeHeading(double angle);
};

} // namespace mapnavigator
