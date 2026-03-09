#include "navi_math.h"
#include "navi_domain_types.h"

namespace mapnavigator
{

double NaviMath::CalcTargetRotation(double from_x, double from_y, double to_x, double to_y)
{
    double dx = to_x - from_x;
    double dy = to_y - from_y;
    double angle_deg = std::atan2(dx, -dy) * 180.0 / kPi;
    if (angle_deg < 0) {
        angle_deg += 360.0;
    }
    return std::fmod(std::round(angle_deg), 360.0);
}

double NaviMath::CalcDeltaRotation(double current, double target)
{
    return NormalizeAngle(target - current);
}

double NaviMath::NormalizeAngle(double angle)
{
    while (angle > 180.0) {
        angle -= 360.0;
    }
    while (angle <= -180.0) {
        angle += 360.0;
    }
    return angle;
}

} // namespace mapnavigator
