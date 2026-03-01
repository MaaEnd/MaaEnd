#pragma once

#include "MapTypes.h"
#include <chrono>
#include <optional>
#include <opencv2/opencv.hpp>

namespace maplocator {

class MotionTracker {
public:
    explicit MotionTracker(const TrackingConfig& cfg);

    void update(const MapPosition& newPos, std::chrono::steady_clock::time_point now);
    void hold(const MapPosition& oldPos, std::chrono::steady_clock::time_point now);
    void markLost(int increment = 1);
    void forceLost();

    bool isTracking(int maxAllowedLost) const;

    cv::Rect predictNextSearchRect(double trackScale, int templCols, int templRows, std::chrono::steady_clock::time_point now) const;

    std::optional<MapPosition> getLastPos() const { return lastKnownPos; }
    int getLostCount() const { return lostTrackingCount; }
    
    double getPredictedX(std::chrono::steady_clock::time_point now) const;
    double getPredictedY(std::chrono::steady_clock::time_point now) const;

    double getVelocityX() const { return velocityX; }
    double getVelocityY() const { return velocityY; }
    std::chrono::steady_clock::time_point getLastTime() const { return lastTime; }

    void clearVelocity() {
        velocityX = 0;
        velocityY = 0;
    }

private:
    TrackingConfig trackingCfg;
    std::optional<MapPosition> lastKnownPos;
    int lostTrackingCount;
    double velocityX;
    double velocityY;
    std::chrono::steady_clock::time_point lastTime;
};

} // namespace maplocator
