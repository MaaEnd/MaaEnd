#pragma once

#include "MapTypes.h"
#include <MaaUtils/NoWarningCV.hpp>
#include <chrono>
#include <memory>

namespace maplocator
{

class MapLocator
{
public:
    MapLocator();
    ~MapLocator();

    bool initialize(const MapLocatorConfig& config);
    bool isInitialized() const;
    LocateResult locate(const cv::Mat& minimap, const LocateOptions& options = LocateOptions());

    void resetTrackingState();

    std::optional<MapPosition> getLastKnownPos() const;
    double getVelocityX() const;
    double getVelocityY() const;

private:
    class Impl;
    std::unique_ptr<Impl> pimpl;
};

} // namespace maplocator
