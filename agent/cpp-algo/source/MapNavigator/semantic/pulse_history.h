#pragma once

#include <deque>
#include <mutex>

namespace mapnavigator
{

namespace semantic_nodes
{

struct PulseSample
{
    double hold_ms = 0.0;
    double travel = 0.0;
};

struct PulseHistory
{
    std::mutex mutex;
    std::deque<PulseSample> samples;
};

} // namespace semantic_nodes

} // namespace mapnavigator
