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
// 在每个模板的临时 clone 上清除送货数量条区域；未来扩展识别范围应在同一 mask 上做 union。
void ApplyShipmentTopBarMask(cv::Mat& mask);
// 在每个模板的临时 clone 上清除贵重品武器头像区域；不改变 catalog 中的基础 mask。
void ApplyValuablesWeaponPortraitMask(cv::Mat& mask);
void ClearValuablesWeaponPortrait(cv::Mat& mask, const cv::Mat& slot);

} // namespace iconrecognition::detail
