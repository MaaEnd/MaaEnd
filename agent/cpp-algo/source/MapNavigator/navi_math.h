#pragma once

#include <chrono>
#include <thread>

#include "navi_domain_types.h"

namespace mapnavigator
{

class NaviMath
{
public:
    static double CalcTargetRotation(double from_x, double from_y, double to_x, double to_y);
    static double CalcDeltaRotation(double current, double target);
    static double NormalizeAngle(double angle);
    static double NormalizeHeading(double angle);
    static double BlendAngle(double from, double to, double weight);
};

namespace utils
{

inline void SleepFor(int millis)
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

} // namespace utils

} // namespace mapnavigator
