#include <utility>

#include "adb_zone_guard.h"

namespace mapnavigator::backend::adb
{

AdbZoneGuard::AdbZoneGuard(std::shared_ptr<maplocator::MapLocator> locator)
    : locator_(std::move(locator))
{
}

bool AdbZoneGuard::ExtractMinimap(const cv::Mat& image, cv::Mat* out_minimap) const
{
    if (out_minimap == nullptr || image.empty() || locator_ == nullptr || !locator_->isInitialized()) {
        return false;
    }

    return maplocator::TryExtractMinimap(image, true, out_minimap);
}

maplocator::YoloCoarseResult AdbZoneGuard::ProbeYolo(const cv::Mat& image) const
{
    cv::Mat minimap;
    if (!ExtractMinimap(image, &minimap)) {
        return {};
    }

    return locator_->predictCoarse(minimap);
}

} // namespace mapnavigator::backend::adb
