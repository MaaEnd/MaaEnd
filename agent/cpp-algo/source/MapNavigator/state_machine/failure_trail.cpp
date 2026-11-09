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

void PruneFailureTrail(FailureTrail* trail, TimePoint now)
{
    const auto horizon = now - std::chrono::milliseconds(kFailureTrailWindowMs);
    while (!trail->samples.empty() && trail->samples.front().at < horizon) {
        trail->samples.pop_front();
    }
    while (!trail->events.empty() && trail->events.front().at < horizon) {
        trail->events.pop_front();
    }
}

void NoteTimedFailure(FailureTrail* trail, TimePoint now, std::string what, int64_t elapsed_ms)
{
    trail->events.push_back(FailureTrailEvent { .at = now, .what = std::move(what), .elapsed_ms = elapsed_ms });
    PruneFailureTrail(trail, now);
}

void NoteFailure(FailureTrail* trail, TimePoint now, std::string what)
{
    NoteTimedFailure(trail, now, std::move(what), -1);
}

void SampleFailureTrail(
    FailureTrail* trail,
    TimePoint now,
    const NaviPosition& position,
    bool captured,
    std::string_view locate_message,
    size_t node_idx,
    std::string_view phase)
{
    if (captured && position.zone_id != trail->zone_id) {
        trail->zone_id = position.zone_id;
        NoteFailure(trail, now, "zone " + trail->zone_id);
    }
    if (phase != trail->phase) {
        trail->phase = phase;
        NoteFailure(trail, now, "phase " + trail->phase);
    }

    if (!trail->samples.empty() && now - trail->samples.back().at < std::chrono::milliseconds(kFailureTrailSampleIntervalMs)) {
        return;
    }
    trail->samples.push_back(FailureTrailSample {
        .at = now,
        .x = position.x,
        .y = position.y,
        .score = position.score,
        .captured = captured,
        .locate_message = std::string(locate_message),
        .node_idx = node_idx,
    });
    PruneFailureTrail(trail, now);
}

void LogFailureTrail(const FailureTrail& trail, TimePoint now)
{
    std::vector<std::string> samples;
    samples.reserve(trail.samples.size());
    for (const FailureTrailSample& sample : trail.samples) {
        const std::string head = std::format("{:+.1f}s wp{}", offset_seconds(sample.at, now), sample.node_idx);
        if (!sample.captured) {
            samples.push_back(std::format("{} lost {}", head, sample.locate_message));
            continue;
        }
        samples.push_back(std::format("{} {:.1f},{:.1f} {:.2f} {}", head, sample.x, sample.y, sample.score, sample.locate_message));
    }
    std::vector<std::string> events;
    events.reserve(trail.events.size());
    for (const FailureTrailEvent& event : trail.events) {
        const std::string head = std::format("{:+.1f}s {}", offset_seconds(event.at, now), event.what);
        events.push_back(event.elapsed_ms < 0 ? head : std::format("{} {}ms", head, event.elapsed_ms));
    }
    LogInfo << "Navigation failure trail." << VAR(trail.zone_id) << VAR(trail.phase) << VAR(samples);
    LogInfo << "Navigation failure events." << VAR(events);
}

} // namespace mapnavigator
