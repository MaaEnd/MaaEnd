#include "WorldMapImage.h"

#include <cmath>

namespace worldmap
{

cv::Mat ToGray(const cv::Mat& src)
{
    if (src.channels() == 4) {
        cv::Mat gray;
        cv::cvtColor(src, gray, cv::COLOR_BGRA2GRAY);
        return gray;
    }
    if (src.channels() == 3) {
        cv::Mat gray;
        cv::cvtColor(src, gray, cv::COLOR_BGR2GRAY);
        return gray;
    }
    return src.clone();
}

cv::Mat SaturationOf(const cv::Mat& src)
{
    cv::Mat bgr = src;
    if (src.channels() == 4) {
        cv::cvtColor(src, bgr, cv::COLOR_BGRA2BGR);
    }
    if (bgr.channels() != 3) {
        return {};
    }

    cv::Mat hsv;
    cv::cvtColor(bgr, hsv, cv::COLOR_BGR2HSV);
    std::vector<cv::Mat> planes;
    cv::split(hsv, planes);
    return planes[1];
}

std::vector<double> LinearLadder(double lo, double hi, int steps)
{
    std::vector<double> out;
    if (steps <= 0) {
        return out;
    }
    if (steps == 1) {
        out.push_back(lo);
        return out;
    }
    for (int i = 0; i < steps; ++i) {
        out.push_back(lo + (hi - lo) * i / (steps - 1));
    }
    return out;
}

cv::Size ScaledSize(const cv::Mat& image, double scale)
{
    return { static_cast<int>(std::lround(image.cols * scale)), static_cast<int>(std::lround(image.rows * scale)) };
}

cv::Mat ResizeTo(const cv::Mat& image, const cv::Size& size)
{
    cv::Mat resized;
    cv::resize(image, resized, size, 0.0, 0.0, size.width < image.cols ? cv::INTER_AREA : cv::INTER_LINEAR);
    return resized;
}

void KeepBest(std::optional<ScanHit>* best, const maplocator::MatchResultRaw& hit, double scale, const cv::Size& size)
{
    if (!*best || hit.score > (*best)->score) {
        *best = ScanHit { .score = hit.score, .psr = hit.psr, .scale = scale, .loc = hit.loc, .size = size };
    }
}

} // namespace worldmap
