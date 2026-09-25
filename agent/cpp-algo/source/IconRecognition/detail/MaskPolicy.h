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
// 返回实际 mask 组合的诊断名称；复合图标会保留基础 union 与界面专用 mask 两层信息。
std::string DescribeMaskKind(MaskKind kind, bool composite);
bool HasShipmentTopBar(const cv::Mat& image);
// 仅根据贵重品槽位截图判断右上角是否存在武器头像，不依赖任意候选模板的透明区域。
bool HasValuablesWeaponPortrait(const cv::Mat& slot);
// 在每个模板的临时 clone 上清除送货数量条区域；未来扩展识别范围应在同一 mask 上做 union。
void ApplyShipmentTopBarMask(cv::Mat& mask);
// 在每个模板的临时 clone 上清除贵重品武器头像区域；不改变 catalog 中的基础 mask。
void ApplyValuablesWeaponPortraitMask(cv::Mat& mask);

} // namespace iconrecognition::detail
