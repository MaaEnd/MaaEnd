#pragma once

#include <chrono>
#include <optional>

#include <MaaUtils/NoWarningCV.hpp>

#include "MapTypes.h"

namespace maplocator
{

struct MotionTrack
{
    TrackingConfig config;
    std::optional<MapPosition> last_known_pos;
    int lost_count = MaxLostTrackingCount + 1;
    double velocity_x = 0.0;
    double velocity_y = 0.0;
    std::chrono::steady_clock::time_point last_time {};
};

MotionTrack MakeMotionTrack(const TrackingConfig& config);

void UpdateTrack(MotionTrack* track, const MapPosition& newPos, std::chrono::steady_clock::time_point now);
void HoldTrack(MotionTrack* track, const MapPosition& oldPos, std::chrono::steady_clock::time_point now);
void MarkTrackLost(MotionTrack* track, int increment);
void ForceTrackLost(MotionTrack* track);
void ClearTrackVelocity(MotionTrack* track);

bool IsTracking(const MotionTrack& track, int maxAllowedLost);

cv::Rect PredictNextSearchRect(
    const MotionTrack& track,
    double trackScale,
    int templCols,
    int templRows,
    std::chrono::steady_clock::time_point now);
double PredictedX(const MotionTrack& track, std::chrono::steady_clock::time_point now);
double PredictedY(const MotionTrack& track, std::chrono::steady_clock::time_point now);

} // namespace maplocator
