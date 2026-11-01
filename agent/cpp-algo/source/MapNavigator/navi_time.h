#pragma once

#include <chrono>
#include <cstdint>
#include <thread>

namespace mapnavigator
{

inline void SleepFor(int millis)
{
    if (millis <= 0) {
        return;
    }
    std::this_thread::sleep_for(std::chrono::milliseconds(millis));
}

inline int64_t ElapsedMs(std::chrono::steady_clock::time_point from, std::chrono::steady_clock::time_point to)
{
    return std::chrono::duration_cast<std::chrono::milliseconds>(to - from).count();
}

inline int64_t ElapsedMs(std::chrono::steady_clock::time_point since)
{
    return std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::steady_clock::now() - since).count();
}

inline bool IsUnset(std::chrono::steady_clock::time_point time)
{
    return time.time_since_epoch().count() == 0;
}

} // namespace mapnavigator
