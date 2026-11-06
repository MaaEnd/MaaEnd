#pragma once

#include <chrono>
#include <cstddef>
#include <cstdint>
#include <deque>
#include <string>
#include <string_view>

namespace mapnavigator
{

struct NaviPosition;

class FailureTrail
{
public:
    void sample(
        std::chrono::steady_clock::time_point now,
        const NaviPosition& position,
        bool captured,
        std::string_view locate_message,
        size_t node_idx,
        std::string_view phase);
    void note(std::chrono::steady_clock::time_point now, std::string what, int64_t elapsed_ms = -1);
    void log(std::chrono::steady_clock::time_point now);

private:
    struct Sample
    {
        std::chrono::steady_clock::time_point at;
        double x = 0.0;
        double y = 0.0;
        double score = 0.0;
        bool captured = false;
        std::string locate_message;
        size_t node_idx = 0;
    };

    struct Event
    {
        std::chrono::steady_clock::time_point at;
        std::string what;
        int64_t elapsed_ms = -1;
    };

    void prune(std::chrono::steady_clock::time_point now);

    std::deque<Sample> samples_;
    std::deque<Event> events_;
    std::string zone_id_;
    std::string phase_;
};

} // namespace mapnavigator
