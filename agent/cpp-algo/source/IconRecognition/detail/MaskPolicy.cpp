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
const cv::Point kValuablesPortraitCenter { 81, 15 };
constexpr int kValuablesPortraitRadius = 20;

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
    if (mask.empty() || slot.empty() || mask.rows != 96 || mask.cols != 96) {
        return;
    }
    const cv::Rect detection_rect(60, 0, 36, 42);
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
    cv::HoughCircles(gray, circles, cv::HOUGH_GRADIENT, 1.0, 16.0, 100.0, 16.0, 14, 22);
    const bool detected = std::ranges::any_of(circles, [](const cv::Vec3f& circle) {
        const double absolute_x = circle[0] + 60.0;
        const double absolute_y = circle[1];
        return absolute_x >= 70.0 && absolute_x <= 96.0 && absolute_y >= 0.0 && absolute_y <= 30.0 && circle[2] >= 14.0
               && circle[2] <= 22.0;
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
