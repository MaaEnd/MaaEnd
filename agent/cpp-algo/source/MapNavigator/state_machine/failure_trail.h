#pragma once

#include <chrono>
#include <cstddef>
#include <cstdint>
#include <deque>
#include <string>
#include <string_view>

#include "../navi_time.h"

namespace mapnavigator
{

struct NaviPosition;

struct FailureTrailSample
{
    std::chrono::steady_clock::time_point at;
    double x = 0.0;
    double y = 0.0;
    double score = 0.0;
    bool captured = false;
    std::string locate_message;
    size_t node_idx = 0;
};

struct FailureTrailEvent
{
    std::chrono::steady_clock::time_point at;
    std::string what;
    int64_t elapsed_ms = -1;
};

struct FailureTrail
{
    std::deque<FailureTrailSample> samples;
    std::deque<FailureTrailEvent> events;
    std::string zone_id;
    std::string phase;
};

void SampleFailureTrail(
    FailureTrail* trail,
    TimePoint now,
    const NaviPosition& position,
    bool captured,
    std::string_view locate_message,
    size_t node_idx,
    std::string_view phase);
void NoteFailure(FailureTrail* trail, TimePoint now, std::string what);
void NoteTimedFailure(FailureTrail* trail, TimePoint now, std::string what, int64_t elapsed_ms);
void PruneFailureTrail(FailureTrail* trail, TimePoint now);
void LogFailureTrail(const FailureTrail& trail, TimePoint now);

} // namespace mapnavigator
