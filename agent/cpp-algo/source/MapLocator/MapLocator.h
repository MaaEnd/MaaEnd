#pragma once

#include "MapTypes.h"
#include <chrono>
#include <memory>
#include <MaaUtils/NoWarningCV.hpp>

namespace maplocator {

class MapLocator {
public:
    MapLocator();
    ~MapLocator();

    bool initialize(const MapLocatorConfig& config);
    bool isInitialized() const;
    LocateResult locate(const cv::Mat& minimap, const LocateOptions& options = LocateOptions());

    void resetTrackingState(); 

    std::optional<MapPosition> getLastKnownPos() const;

private:
    class Impl;
    std::unique_ptr<Impl> pimpl;
};

} // namespace maplocator
