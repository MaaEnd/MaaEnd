#pragma once

#include <memory>

#include "MapLocator/MapLocator.h"

namespace mapnavigator::backend::adb
{

class AdbZoneGuard
{
public:
    explicit AdbZoneGuard(std::shared_ptr<maplocator::MapLocator> locator);

    maplocator::YoloCoarseResult ProbeYolo(const cv::Mat& image) const;

private:
    bool ExtractMinimap(const cv::Mat& image, cv::Mat* out_minimap) const;

    std::shared_ptr<maplocator::MapLocator> locator_;
};

} // namespace mapnavigator::backend::adb
