#include "MaskPolicy.h"

#include <algorithm>
#include <cmath>
#include <vector>

namespace iconrecognition::detail
{

namespace
{

// 送货数量条检测和遮罩的顶部高度（像素）；调大覆盖更多数量条，也会裁掉更多图标。
constexpr int kShipmentQuantityBarHeight = 20;
// 顶部区域判为黄色数量条所需的最少像素数；调高减少误触发，调低可识别残缺数量条。
constexpr int kShipmentQuantityBarMinPixels = 500;
// 武器头像清理规则只适用于 96px 贵重品槽位，其他尺寸不执行圆检测。
constexpr int kValuablesSlotSize = 96;
// 96px 槽位右上角用于寻找武器头像圆的局部区域。
const cv::Rect kValuablesPortraitDetectionRect { 60, 0, 36, 42 };
// 检出头像后从匹配 mask 中清除的圆心，按 720p 贵重品槽位标定。
const cv::Point kValuablesPortraitCenter { 81, 15 };
// 从匹配 mask 中清除的头像圆半径；调大减少头像干扰，也会损失更多武器图标信息。
constexpr int kValuablesPortraitRadius = 20;
// 下扩 mask 上部斜边的转折比例；调大缩小上半部覆盖，调小会纳入更多角落背景。
constexpr double kLowerExtendedMaskTopRatio = 0.5;
// 下扩 mask 的底边比例；调大保留更多图标下部，也会纳入更多文字或背景。
constexpr double kLowerExtendedMaskBottomRatio = 0.7;
// Hough 累加器分辨率相对输入图的反比；保持 1.0 表示不降采样。
constexpr double kPortraitHoughDp = 1.0;
// Hough 候选圆心的最小间距；调大减少重复圆，调小会产生更多相邻候选。
constexpr double kPortraitHoughMinDistance = 16.0;
// Hough 内部 Canny 高阈值；调高只保留强边缘，调低可召回弱圆但增加噪声。
constexpr double kPortraitHoughCannyThreshold = 100.0;
// Hough 圆心累加器阈值；调高减少误检，调低提高弱头像圆召回。
constexpr double kPortraitHoughAccumulatorThreshold = 16.0;
// 可接受头像圆的最小半径；调低会把小型圆形纹理当作头像。
constexpr int kPortraitHoughMinRadius = 14;
// 可接受头像圆的最大半径；调高会接纳更大的非头像圆形结构。
constexpr int kPortraitHoughMaxRadius = 22;
// 头像圆心在完整槽位中的最小 x 坐标，限制候选位于右上角。
constexpr double kPortraitCenterMinX = 70.0;
// 头像圆心在完整槽位中的最大 x 坐标；放宽会允许圆心越过槽位右边缘。
constexpr double kPortraitCenterMaxX = 96.0;
// 头像圆心在完整槽位中的最小 y 坐标，0 表示允许圆心贴近顶边。
constexpr double kPortraitCenterMinY = 0.0;
// 头像圆心在完整槽位中的最大 y 坐标；调大可能接受图标主体内的圆形纹理。
constexpr double kPortraitCenterMaxY = 30.0;

int RoundHalfToEven(double value)
{
    // 固定采用 ties-to-even，避免 cvRound 在半整数顶点上扩大多边形边界。
    const double lower = std::floor(value);
    const double fraction = value - lower;
    if (fraction < 0.5) {
        return static_cast<int>(lower);
    }
    if (fraction > 0.5) {
        return static_cast<int>(lower + 1.0);
    }
    const int lower_int = static_cast<int>(lower);
    return (lower_int % 2 == 0) ? lower_int : lower_int + 1;
}

} // namespace

cv::Mat BuildLowerExtendedMask(int target_size)
{
    if (target_size <= 0) {
        return {};
    }
    cv::Mat mask = cv::Mat::zeros(target_size, target_size, CV_8UC1);
    const int maximum = target_size - 1;
    const std::vector<cv::Point> polygon {
        { RoundHalfToEven(kLowerExtendedMaskTopRatio * maximum), 0 },
        { maximum, RoundHalfToEven(kLowerExtendedMaskTopRatio * maximum) },
        { maximum, RoundHalfToEven(kLowerExtendedMaskBottomRatio * maximum) },
        { 0, RoundHalfToEven(kLowerExtendedMaskBottomRatio * maximum) },
        { 0, RoundHalfToEven(kLowerExtendedMaskTopRatio * maximum) },
    };
    cv::fillPoly(mask, std::vector<std::vector<cv::Point>> { polygon }, cv::Scalar(255));
    return mask;
}

bool HasShipmentTopBar(const cv::Mat& image)
{
    if (image.empty() || image.rows < 4 || image.cols < 4) {
        return false;
    }
    cv::Mat bgr;
    if (image.channels() == 4) {
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
    }
    else if (image.channels() == 3) {
        bgr = image;
    }
    else {
        cv::cvtColor(image, bgr, cv::COLOR_GRAY2BGR);
    }
    cv::Mat hsv;
    cv::cvtColor(bgr, hsv, cv::COLOR_BGR2HSV);
    cv::Mat selected;
    cv::inRange(hsv, cv::Scalar(20, 100, 150), cv::Scalar(40, 255, 255), selected);
    const int top_height = std::min(kShipmentQuantityBarHeight, image.rows);
    return cv::countNonZero(selected.rowRange(0, top_height)) >= kShipmentQuantityBarMinPixels;
}

void ApplyShipmentTopBarMask(cv::Mat& mask)
{
    if (!mask.empty()) {
        mask.rowRange(0, std::min(kShipmentQuantityBarHeight, mask.rows)).setTo(cv::Scalar(0));
    }
}

void ApplyValuablesWeaponPortraitMask(cv::Mat& mask)
{
    if (!mask.empty()) {
        cv::circle(mask, kValuablesPortraitCenter, kValuablesPortraitRadius, cv::Scalar(0), cv::FILLED);
    }
}

void ClearValuablesWeaponPortrait(cv::Mat& mask, const cv::Mat& slot)
{
    if (mask.empty() || slot.empty() || mask.rows != kValuablesSlotSize || mask.cols != kValuablesSlotSize) {
        return;
    }
    cv::Mat gray;
    if (slot.channels() == 4) {
        cv::cvtColor(slot(kValuablesPortraitDetectionRect), gray, cv::COLOR_BGRA2GRAY);
    }
    else if (slot.channels() == 3) {
        cv::cvtColor(slot(kValuablesPortraitDetectionRect), gray, cv::COLOR_BGR2GRAY);
    }
    else {
        gray = slot(kValuablesPortraitDetectionRect);
    }
    std::vector<cv::Vec3f> circles;
    cv::HoughCircles(
        gray,
        circles,
        cv::HOUGH_GRADIENT,
        kPortraitHoughDp,
        kPortraitHoughMinDistance,
        kPortraitHoughCannyThreshold,
        kPortraitHoughAccumulatorThreshold,
        kPortraitHoughMinRadius,
        kPortraitHoughMaxRadius);
    const bool detected = std::ranges::any_of(circles, [](const cv::Vec3f& circle) {
        const double absolute_x = circle[0] + kValuablesPortraitDetectionRect.x;
        const double absolute_y = circle[1];
        return absolute_x >= kPortraitCenterMinX && absolute_x <= kPortraitCenterMaxX && absolute_y >= kPortraitCenterMinY
               && absolute_y <= kPortraitCenterMaxY && circle[2] >= kPortraitHoughMinRadius && circle[2] <= kPortraitHoughMaxRadius;
    });
    if (detected) {
        ApplyValuablesWeaponPortraitMask(mask);
    }
}

cv::Mat BuildMask(const cv::Mat& image, int target_size, GridType grid_type, MaskKind kind)
{
    cv::Mat mask = BuildLowerExtendedMask(target_size);
    if (mask.empty()) {
        return mask;
    }
    if (kind == MaskKind::ShipmentTopBar && HasShipmentTopBar(image)) {
        ApplyShipmentTopBarMask(mask);
    }
    if (kind == MaskKind::ValuablesWeapon && grid_type == GridType::Valuables) {
        ClearValuablesWeaponPortrait(mask, image);
    }
    return mask;
}

} // namespace iconrecognition::detail
