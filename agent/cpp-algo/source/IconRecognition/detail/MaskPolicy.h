#pragma once

#include <string>

#include <MaaUtils/NoWarningCV.hpp>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

enum class MaskKind
{
    LowerExtended,
    ShipmentTopBar,
    ValuablesWeapon,
};

cv::Mat BuildLowerExtendedMask(int target_size);
cv::Mat BuildMask(const cv::Mat& image, int target_size, GridType grid_type, MaskKind kind = MaskKind::LowerExtended);
bool HasShipmentTopBar(const cv::Mat& image);
void ClearValuablesWeaponPortrait(cv::Mat& mask, const cv::Mat& slot);

} // namespace iconrecognition::detail
