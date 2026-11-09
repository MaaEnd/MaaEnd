#pragma once

#include <chrono>
#include <cstdint>
#include <thread>

namespace mapnavigator
{

using TimePoint = std::chrono::steady_clock::time_point;

inline void SleepFor(int millis)
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

inline int64_t ElapsedMs(TimePoint from, TimePoint to)
{
    return std::chrono::duration_cast<std::chrono::milliseconds>(to - from).count();
}

inline int64_t ElapsedMs(TimePoint since)
{
    return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - since).count();
}

inline bool IsUnset(TimePoint time)
{
    return time.time_since_epoch().count() == 0;
}

} // namespace mapnavigator
