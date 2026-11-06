#include "failure_trail.h"

#include <format>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>

#include "../navi_config.h"
#include "../navi_position.h"

namespace mapnavigator
{

namespace
{

double offset_seconds(std::chrono::steady_clock::time_point at, std::chrono::steady_clock::time_point now)
{
    return std::chrono::duration<double>(at - now).count();
}

} // namespace

void FailureTrail::sample(
    std::chrono::steady_clock::time_point now,
    const NaviPosition& position,
    bool captured,
    std::string_view locate_message,
    size_t node_idx,
    std::string_view phase)
{
    if (captured && position.zone_id != zone_id_) {
        zone_id_ = position.zone_id;
        note(now, "zone " + zone_id_);
    }
    if (phase != phase_) {
        phase_ = phase;
        note(now, "phase " + phase_);
    }

    if (!samples_.empty() && now - samples_.back().at < std::chrono::milliseconds(kFailureTrailSampleIntervalMs)) {
        return;
    }
    samples_.push_back(Sample {
        .at = now,
        .x = position.x,
        .y = position.y,
        .score = position.score,
        .captured = captured,
        .locate_message = std::string(locate_message),
        .node_idx = node_idx,
    });
    prune(now);
}

void FailureTrail::note(std::chrono::steady_clock::time_point now, std::string what, int64_t elapsed_ms)
{
    events_.push_back(Event { .at = now, .what = std::move(what), .elapsed_ms = elapsed_ms });
    prune(now);
}

void FailureTrail::log(std::chrono::steady_clock::time_point now)
{
    prune(now);
    std::vector<std::string> samples;
    samples.reserve(samples_.size());
    for (const Sample& sample : samples_) {
        const std::string head = std::format("{:+.1f}s wp{}", offset_seconds(sample.at, now), sample.node_idx);
        if (!sample.captured) {
            samples.push_back(std::format("{} lost {}", head, sample.locate_message));
            continue;
        }
        samples.push_back(std::format("{} {:.1f},{:.1f} {:.2f} {}", head, sample.x, sample.y, sample.score, sample.locate_message));
    }
    std::vector<std::string> events;
    events.reserve(events_.size());
    for (const Event& event : events_) {
        const std::string head = std::format("{:+.1f}s {}", offset_seconds(event.at, now), event.what);
        events.push_back(event.elapsed_ms < 0 ? head : std::format("{} {}ms", head, event.elapsed_ms));
    }
    LogInfo << "Navigation failure trail." << VAR(zone_id_) << VAR(phase_) << VAR(samples);
    LogInfo << "Navigation failure events." << VAR(events);
}

void FailureTrail::prune(std::chrono::steady_clock::time_point now)
{
    const auto horizon = now - std::chrono::milliseconds(kFailureTrailWindowMs);
    while (!samples_.empty() && samples_.front().at < horizon) {
        samples_.pop_front();
    }
    while (!events_.empty() && events_.front().at < horizon) {
        events_.pop_front();
    }
}

} // namespace mapnavigator
