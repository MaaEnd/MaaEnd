#include "MotionTracker.h"
#include <algorithm>
#include <cmath>

namespace maplocator
{

MotionTrack MakeMotionTrack(const TrackingConfig& config)
{
    MotionTrack track;
    track.config = config;
    return track;
}

void UpdateTrack(MotionTrack* track, const MapPosition& newPos, std::chrono::steady_clock::time_point now)
{
    if (track->last_known_pos.has_value() && track->lost_count == 0) {
        std::chrono::duration<double> dt = now - track->last_time;
        double dtSec = dt.count();
        const double dx = newPos.x - track->last_known_pos->x;
        const double dy = newPos.y - track->last_known_pos->y;
        if (std::hypot(dx, dy) < kVelocityDeadband) {
            track->velocity_x = 0.0;
            track->velocity_y = 0.0;
        }
        // 仅在帧间隔合理且匹配分数达标时更新速度，保证速度估计的可靠性
        else if (
            dtSec > 0.016 && dtSec < track->config.max_dt_for_prediction
            && newPos.score >= ZoneScoreLines(newPos.zone_id).velocity_update_min) {
            double rawVx = dx / dtSec;
            double rawVy = dy / dtSec;
            double alpha = track->config.velocity_smoothing_alpha;
            track->velocity_x = track->velocity_x * (1.0 - alpha) + rawVx * alpha;
            track->velocity_y = track->velocity_y * (1.0 - alpha) + rawVy * alpha;
        }
    }
    track->last_known_pos = newPos;
    track->last_time = now;
    track->lost_count = 0;
}

void HoldTrack(MotionTrack* track, const MapPosition& oldPos, std::chrono::steady_clock::time_point now)
{
    track->last_known_pos = oldPos;
    track->last_time = now;
}

void MarkTrackLost(MotionTrack* track, int increment)
{
    track->lost_count += increment;
}

void ForceTrackLost(MotionTrack* track)
{
    track->lost_count = kMaxLostTrackingCount + 100;
    track->last_known_pos = std::nullopt;
    ClearTrackVelocity(track);
}

void ClearTrackVelocity(MotionTrack* track)
{
    track->velocity_x = 0;
    track->velocity_y = 0;
}

bool IsTracking(const MotionTrack& track, int maxAllowedLost)
{
    return track.last_known_pos.has_value() && track.lost_count <= maxAllowedLost;
}

double PredictedX(const MotionTrack& track, std::chrono::steady_clock::time_point now)
{
    if (!track.last_known_pos.has_value()) {
        return 0;
    }
    std::chrono::duration<double> dt = now - track.last_time;
    double dtSec = dt.count();
    if (dtSec > track.config.max_dt_for_prediction) {
        return track.last_known_pos->x;
    }
    return track.last_known_pos->x + track.velocity_x * dtSec;
}

double PredictedY(const MotionTrack& track, std::chrono::steady_clock::time_point now)
{
    if (!track.last_known_pos.has_value()) {
        return 0;
    }
    std::chrono::duration<double> dt = now - track.last_time;
    double dtSec = dt.count();
    if (dtSec > track.config.max_dt_for_prediction) {
        return track.last_known_pos->y;
    }
    return track.last_known_pos->y + track.velocity_y * dtSec;
}

cv::Rect PredictNextSearchRect(
    const MotionTrack& track,
    double trackScale,
    int templCols,
    int templRows,
    std::chrono::steady_clock::time_point now)
{
    double predX = PredictedX(track, now);
    double predY = PredictedY(track, now);
    int pad = static_cast<int>(kMobileSearchRadius + std::max(templCols, templRows) * trackScale / 2.0);
    return cv::Rect((int)predX - pad, (int)predY - pad, pad * 2, pad * 2);
}

} // namespace maplocator
