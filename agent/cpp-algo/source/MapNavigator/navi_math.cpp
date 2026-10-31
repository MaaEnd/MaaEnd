#include <cmath>

#include "navi_math.h"

namespace mapnavigator
{

double NaviMath::calcTargetRotation(double from_x, double from_y, double to_x, double to_y)
{
    const double dx = to_x - from_x;
    const double dy = to_y - from_y;
    double angle_deg = std::atan2(dx, -dy) * 180.0 / kPi;
    if (angle_deg < 0.0) {
        angle_deg += 360.0;
    }
    return std::fmod(std::round(angle_deg), 360.0);
}

double NaviMath::calcDeltaRotation(double current, double target)
{
    return normalizeAngle(target - current);
}

double NaviMath::normalizeAngle(double angle)
{
    while (angle > 180.0) {
        angle -= 360.0;
    }
    while (angle <= -180.0) {
        angle += 360.0;
    }
    return angle;
}

double NaviMath::normalizeHeading(double angle)
{
    angle = std::fmod(angle, 360.0);
    if (angle < 0.0) {
        angle += 360.0;
    }
    return angle;
}

// 仰角只能用世界坐标算, 平面 x/y 按地图比例缩放过, 和高度不同尺
double NaviMath::calcElevation(const ZiplineNodeRef& from, const ZiplineNodeRef& to)
{
    if (!from.has_world || !to.has_world) {
        return 0.0;
    }
    const double run = std::hypot(to.world_x - from.world_x, to.world_z - from.world_z);
    return std::atan2(to.world_y - from.world_y, run) * 180.0 / kPi;
}

} // namespace mapnavigator
