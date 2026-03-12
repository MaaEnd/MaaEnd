#pragma once

namespace mapnavigator
{

class NaviMath
{
public:
    static double CalcTargetRotation(double from_x, double from_y, double to_x, double to_y);
    static double CalcDeltaRotation(double current, double target);
    static double NormalizeAngle(double angle);
};

} // namespace mapnavigator
