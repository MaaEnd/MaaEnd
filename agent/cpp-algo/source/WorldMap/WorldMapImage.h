#pragma once

#include <optional>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

#include "MapLocator/MatchStrategy.h"

namespace worldmap
{

constexpr int kMinTemplateSide = 24;

struct ScanHit
{
    double score = -1.0;
    double psr = 0.0;
    double scale = 0.0;
    cv::Point2d loc { 0.0, 0.0 };
    cv::Size size { 0, 0 };
};

cv::Mat ToGray(const cv::Mat& src);

// HSV 的 S 通道。地图底色也能很艳，所以只在模板圈定的那些像素上取
cv::Mat SaturationOf(const cv::Mat& src);

std::vector<double> LinearLadder(double lo, double hi, int steps);
cv::Size ScaledSize(const cv::Mat& image, double scale);
cv::Mat ResizeTo(const cv::Mat& image, const cv::Size& size);
void KeepBest(std::optional<ScanHit>* best, const maplocator::MatchResultRaw& hit, double scale, const cv::Size& size);

} // namespace worldmap
