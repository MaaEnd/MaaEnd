#include "MotionTracker.h"
#include <algorithm>

namespace maplocator {

MotionTracker::MotionTracker(const TrackingConfig& cfg)
    : trackingCfg(cfg), lostTrackingCount(MaxLostTrackingCount + 1), velocityX(0), velocityY(0) {}

void MotionTracker::update(const MapPosition& newPos, std::chrono::steady_clock::time_point now) {
    if (lastKnownPos.has_value() && lostTrackingCount == 0) {
        std::chrono::duration<double> dt = now - lastTime;
        double dtSec = dt.count();
        // 16ms~maxDt: 正常帧间隔; 超出范围说明速度不可信
        if (dtSec > 0.016 && dtSec < trackingCfg.maxDtForPrediction) {
            double rawVx = (newPos.x - lastKnownPos->x) / dtSec;
            double rawVy = (newPos.y - lastKnownPos->y) / dtSec;
            double alpha = trackingCfg.velocitySmoothingAlpha;
            velocityX = velocityX * (1.0 - alpha) + rawVx * alpha;
            velocityY = velocityY * (1.0 - alpha) + rawVy * alpha;
        }
    }
    lastKnownPos = newPos;
    lastTime = now;
    lostTrackingCount = 0;
}

void MotionTracker::hold(const MapPosition& oldPos, std::chrono::steady_clock::time_point now) {
    lastKnownPos = oldPos;
    lastTime = now;
    lostTrackingCount++;
}

void MotionTracker::markLost(int increment) {
    lostTrackingCount += increment;
}

void MotionTracker::forceLost() {
    lostTrackingCount = MaxLostTrackingCount + 100;
    lastKnownPos = std::nullopt;
}

bool MotionTracker::isTracking(int maxAllowedLost) const {
    return lastKnownPos.has_value() && lostTrackingCount <= maxAllowedLost;
}

double MotionTracker::getPredictedX(std::chrono::steady_clock::time_point now) const {
    if (!lastKnownPos.has_value()) return 0;
    std::chrono::duration<double> dt = now - lastTime;
    double dtSec = dt.count();
    if (dtSec > trackingCfg.maxDtForPrediction) return lastKnownPos->x;
    return lastKnownPos->x + velocityX * dtSec;
}

double MotionTracker::getPredictedY(std::chrono::steady_clock::time_point now) const {
    if (!lastKnownPos.has_value()) return 0;
    std::chrono::duration<double> dt = now - lastTime;
    double dtSec = dt.count();
    if (dtSec > trackingCfg.maxDtForPrediction) return lastKnownPos->y;
    return lastKnownPos->y + velocityY * dtSec;
}

cv::Rect MotionTracker::predictNextSearchRect(double trackScale, int templCols, int templRows, std::chrono::steady_clock::time_point now) const {
    double predX = getPredictedX(now);
    double predY = getPredictedY(now);
    int pad = static_cast<int>(MobileSearchRadius + std::max(templCols, templRows) * trackScale / 2.0);
    return cv::Rect((int)predX - pad, (int)predY - pad, pad * 2, pad * 2);
}

} // namespace maplocator
