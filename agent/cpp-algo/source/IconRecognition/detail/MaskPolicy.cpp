#include "MaskPolicy.h"

#include <algorithm>
#include <cmath>
#include <vector>

namespace iconrecognition::detail
{

namespace
{

constexpr int kShipmentQuantityBarHeight = 20;
constexpr int kShipmentQuantityBarMinPixels = 500;
constexpr int kValuablesSlotSize = 96;
const cv::Rect kValuablesPortraitDetectionRect { 60, 0, 36, 42 };
const cv::Point kValuablesPortraitCenter { 81, 15 };
constexpr int kValuablesPortraitRadius = 20;
// 武器头像圆只会出现在槽位右上角；参数按 96px 贵重品槽位截图标定。
constexpr double kPortraitHoughDp = 1.0;
constexpr double kPortraitHoughMinDistance = 16.0;
constexpr double kPortraitHoughCannyThreshold = 100.0;
constexpr double kPortraitHoughAccumulatorThreshold = 16.0;
constexpr int kPortraitHoughMinRadius = 14;
constexpr int kPortraitHoughMaxRadius = 22;
constexpr double kPortraitCenterMinX = 70.0;
constexpr double kPortraitCenterMaxX = 96.0;
constexpr double kPortraitCenterMinY = 0.0;
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
        { RoundHalfToEven(0.5 * maximum), 0 }, { maximum, RoundHalfToEven(0.5 * maximum) }, { maximum, RoundHalfToEven(0.7 * maximum) },
        { 0, RoundHalfToEven(0.7 * maximum) }, { 0, RoundHalfToEven(0.5 * maximum) },
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
        cv::circle(mask, kValuablesPortraitCenter, kValuablesPortraitRadius, cv::Scalar(0), cv::FILLED);
    }
}

cv::Mat BuildMask(const cv::Mat& image, int target_size, GridType grid_type, MaskKind kind)
{
    cv::Mat mask = BuildLowerExtendedMask(target_size);
    if (mask.empty()) {
        return mask;
    }
    if (kind == MaskKind::ShipmentTopBar && HasShipmentTopBar(image)) {
        mask.rowRange(0, std::min(kShipmentQuantityBarHeight, target_size)).setTo(cv::Scalar(0));
    }
    if (kind == MaskKind::ValuablesWeapon && grid_type == GridType::Valuables) {
        ClearValuablesWeaponPortrait(mask, image);
    }
    return mask;
}

} // namespace iconrecognition::detail
