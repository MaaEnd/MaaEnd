#include "MaskPolicy.h"

#include <algorithm>
#include <cmath>
#include <vector>

namespace iconrecognition::detail
{

namespace
{

// 送货数量条检测窗口占 cell 高度的比例；20/64 覆盖完整黄底、黑字、勾选区和下沿。
constexpr double kShipmentQuantityBarHeightRatio = 0.3125;
// 64px 基准 cell 的完整黄条叠加区高 20px，包含黄底、黑字、勾选区和下沿；调大减少叠加干扰，也会裁掉更多图标主体。
constexpr double kShipmentQuantityBarMaskHeightRatio = 0.3125;
// 64x20 基准检测带中至少 500 个黄色像素，对其他 cell 尺寸按检测带面积等比换算。
constexpr double kShipmentQuantityBarMinimumCoverage = 0.390625;
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
    const int top_height = std::clamp(cvRound(image.rows * kShipmentQuantityBarHeightRatio), 1, image.rows);
    const cv::Mat top = selected.rowRange(0, top_height);
    const int minimum_yellow_pixels = cvRound(top.total() * kShipmentQuantityBarMinimumCoverage);
    if (cv::countNonZero(top) < minimum_yellow_pixels) {
        return false;
    }
    return true;
}

void ApplyShipmentTopBarMask(cv::Mat& mask)
{
    if (!mask.empty()) {
        const int mask_height = std::clamp(cvRound(mask.rows * kShipmentQuantityBarMaskHeightRatio), 1, mask.rows);
        mask.rowRange(0, mask_height).setTo(cv::Scalar(0));
    }
}

void ApplyValuablesWeaponPortraitMask(cv::Mat& mask)
{
    if (!mask.empty()) {
        const double scale = static_cast<double>(std::min(mask.rows, mask.cols)) / kValuablesSlotSize;
        const cv::Point center(cvRound(kValuablesPortraitCenter.x * scale), cvRound(kValuablesPortraitCenter.y * scale));
        const int radius = std::max(1, cvRound(kValuablesPortraitRadius * scale));
        cv::circle(mask, center, radius, cv::Scalar(0), cv::FILLED);
    }
}

std::string DescribeMaskKind(MaskKind kind, bool composite)
{
    std::string active_kind;
    switch (kind) {
    case MaskKind::LowerExtended:
        active_kind = "lower_extended";
        break;
    case MaskKind::ShipmentTopBar:
        active_kind = "shipment_top_bar";
        break;
    case MaskKind::ValuablesWeapon:
        active_kind = "valuables_weapon";
        break;
    }
    if (!composite) {
        return active_kind;
    }
    return kind == MaskKind::LowerExtended ? "composite_union" : "composite_union+" + active_kind;
}

bool HasValuablesWeaponPortrait(const cv::Mat& slot)
{
    if (slot.empty() || slot.rows != slot.cols) {
        return false;
    }
    const double scale = static_cast<double>(slot.rows) / kValuablesSlotSize;
    const int detection_x1 = cvRound(kValuablesPortraitDetectionRect.x * scale);
    const int detection_y1 = cvRound(kValuablesPortraitDetectionRect.y * scale);
    const int detection_x2 = cvRound((kValuablesPortraitDetectionRect.x + kValuablesPortraitDetectionRect.width) * scale);
    const int detection_y2 = cvRound((kValuablesPortraitDetectionRect.y + kValuablesPortraitDetectionRect.height) * scale);
    const cv::Rect detection_rect(detection_x1, detection_y1, detection_x2 - detection_x1, detection_y2 - detection_y1);
    const cv::Rect bounds(0, 0, slot.cols, slot.rows);
    if (detection_rect.empty() || (detection_rect & bounds) != detection_rect) {
        return false;
    }
    cv::Mat gray;
    if (slot.channels() == 4) {
        cv::cvtColor(slot(detection_rect), gray, cv::COLOR_BGRA2GRAY);
    }
    else if (slot.channels() == 3) {
        cv::cvtColor(slot(detection_rect), gray, cv::COLOR_BGR2GRAY);
    }
    else {
        gray = slot(detection_rect);
    }
    std::vector<cv::Vec3f> circles;
    cv::HoughCircles(
        gray,
        circles,
        cv::HOUGH_GRADIENT,
        kPortraitHoughDp,
        kPortraitHoughMinDistance * scale,
        kPortraitHoughCannyThreshold,
        kPortraitHoughAccumulatorThreshold,
        std::max(1, cvRound(kPortraitHoughMinRadius * scale)),
        std::max(1, cvRound(kPortraitHoughMaxRadius * scale)));
    return std::ranges::any_of(circles, [&](const cv::Vec3f& circle) {
        const double absolute_x = circle[0] + detection_rect.x;
        const double absolute_y = circle[1];
        return absolute_x >= kPortraitCenterMinX * scale && absolute_x <= kPortraitCenterMaxX * scale
               && absolute_y >= kPortraitCenterMinY * scale && absolute_y <= kPortraitCenterMaxY * scale
               && circle[2] >= kPortraitHoughMinRadius * scale && circle[2] <= kPortraitHoughMaxRadius * scale;
    });
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
    if (kind == MaskKind::ValuablesWeapon && grid_type == GridType::Valuables && HasValuablesWeaponPortrait(image)) {
        ApplyValuablesWeaponPortraitMask(mask);
    }
    return mask;
}

} // namespace iconrecognition::detail
