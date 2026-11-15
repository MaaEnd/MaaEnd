#include "MapAlgorithm.h"
#include "MapTypes.h"
#include <algorithm>
#include <filesystem>
#include <vector>

namespace maplocator
{

namespace
{

constexpr double kLocalContrastSigma = 3.0;
constexpr double kLocalContrastStdFloor = 6.0;
constexpr double kLocalContrastClip = 3.0;

} // namespace

cv::Mat GenerateMinimapMask(const cv::Mat& minimap, const ImageProcessingConfig& cfg, bool withUiMask, bool withCenterMask)
{
    int w = minimap.cols, h = minimap.rows;
    int centerX = w / 2, centerY = h / 2;
    int radius = std::min(w, h) / 2 - cfg.border_margin;
    if (radius < 0) {
        radius = 0;
    }

    cv::Mat baseMask = cv::Mat::zeros(h, w, CV_8UC1);
    cv::circle(baseMask, cv::Point(centerX, centerY), radius, cv::Scalar(255), -1);

    cv::Mat workImg = minimap;
    cv::Mat tempBGR;
    if (workImg.channels() == 4) {
        cv::cvtColor(workImg, tempBGR, cv::COLOR_BGRA2BGR);
        workImg = tempBGR;
    }

    if (withUiMask) {
        cv::Mat whiteMask = cv::Mat::zeros(h, w, CV_8UC1);
        cv::Mat colorIconMask = cv::Mat::zeros(h, w, CV_8UC1);

        cv::inRange(workImg, cv::Scalar::all(255), cv::Scalar::all(255), whiteMask);

        for (int y = 0; y < h; y++) {
            uchar* colorRow = colorIconMask.ptr<uchar>(y);
            const uchar* baseRow = baseMask.ptr<uchar>(y);
            const cv::Vec3b* imgRow = workImg.ptr<cv::Vec3b>(y);

            for (int x = 0; x < w; x++) {
                if (baseRow[x] == 0) {
                    continue;
                }

                int b = imgRow[x][0], g = imgRow[x][1], r = imgRow[x][2];
                // 提取干扰色块：基于特定游戏 UI 色彩分布进行消除，暖色系(高 R/G)及冷色系特异区域(高 B)
                if ((r > 100 && g > 100 && std::min(r, g) - b > cfg.icon_diff_threshold) || (b > 140 && b > r + 50)) {
                    colorRow[x] = 255;
                }
            }
        }

        int cD = std::max(1, cfg.color_dilate);
        // 颜色掩膜膨胀：小地图 UI 往往伴随半透明发光或抗锯齿像素，直接提取容易留有彩色残边（形成强烈的虚假梯度），膨胀能确保干扰被完全剔除
        cv::dilate(colorIconMask, colorIconMask, cv::getStructuringElement(cv::MORPH_ELLIPSE, cv::Size(cD, cD)));
        baseMask.setTo(0, colorIconMask);

        int wD = std::max(1, cfg.white_dilate);
        // 白色掩膜膨胀：进一步覆盖白色 UI 周围残余的黑边或阴影
        cv::dilate(whiteMask, whiteMask, cv::getStructuringElement(cv::MORPH_ELLIPSE, cv::Size(wD, wD)));
        baseMask.setTo(0, whiteMask);
    }

    if (withCenterMask) {
        cv::circle(baseMask, cv::Point(centerX, centerY), cfg.center_mask_radius, cv::Scalar(0), -1);
    }

    return baseMask;
}

cv::Mat MinimapRingMask(const cv::Size& size, const ImageProcessingConfig& cfg)
{
    const cv::Point center(size.width / 2, size.height / 2);
    const int radius = std::max(0, std::min(size.width, size.height) / 2 - cfg.border_margin);
    cv::Mat mask = cv::Mat::zeros(size, CV_8UC1);
    cv::circle(mask, center, radius, cv::Scalar(255), -1);
    cv::circle(mask, center, cfg.center_mask_radius, cv::Scalar(0), -1);
    return mask;
}

cv::Mat LocalContrastNormalize(const cv::Mat& gray)
{
    cv::Mat image;
    gray.convertTo(image, CV_32F);
    cv::Mat mean;
    cv::Mat meanSq;
    cv::GaussianBlur(image, mean, cv::Size(), kLocalContrastSigma);
    cv::GaussianBlur(image.mul(image), meanSq, cv::Size(), kLocalContrastSigma);
    cv::Mat stdDev;
    cv::sqrt(cv::max(meanSq - mean.mul(mean), 0.0), stdDev);
    cv::Mat normalized = (image - mean) / cv::max(stdDev, kLocalContrastStdFloor);
    return cv::min(cv::max(normalized, -kLocalContrastClip), kLocalContrastClip);
}

namespace
{

bool estimate_arrow_forward(const std::vector<cv::Point>& contour, const cv::Point2f& centroid, cv::Point2f* outForward)
{
    if (outForward == nullptr) {
        return false;
    }

    std::vector<cv::Point> hullPts;
    cv::convexHull(contour, hullPts);

    const double cornerEps[] = { 0.05, 0.04, 0.06, 0.03, 0.08, 0.02, 0.10 };
    const double peri = cv::arcLength(hullPts, true);
    std::vector<cv::Point> corners;
    for (double eps : cornerEps) {
        std::vector<cv::Point> approx;
        cv::approxPolyDP(hullPts, approx, eps * peri, true);
        if (approx.size() == 3) {
            corners = std::move(approx);
            break;
        }
    }
    if (corners.size() != 3) {
        return false;
    }

    int sharpest = -1;
    double maxCos = -2.0;
    for (int i = 0; i < 3; ++i) {
        cv::Point2f v1((float)(corners[(i + 2) % 3].x - corners[i].x), (float)(corners[(i + 2) % 3].y - corners[i].y));
        cv::Point2f v2((float)(corners[(i + 1) % 3].x - corners[i].x), (float)(corners[(i + 1) % 3].y - corners[i].y));
        double n = std::sqrt(((double)v1.x * v1.x + (double)v1.y * v1.y) * ((double)v2.x * v2.x + (double)v2.y * v2.y));
        double c = (n < 1e-9) ? -1.0 : ((double)v1.x * v2.x + (double)v1.y * v2.y) / n;
        if (c > maxCos) {
            maxCos = c;
            sharpest = i;
        }
    }

    cv::Point2f fwd((float)corners[sharpest].x - centroid.x, (float)corners[sharpest].y - centroid.y);
    double n = std::sqrt((double)fwd.x * fwd.x + (double)fwd.y * fwd.y);
    if (n < 1e-6) {
        return false;
    }
    *outForward = cv::Point2f((float)(fwd.x / n), (float)(fwd.y / n));
    return true;
}

} // namespace

double InferYellowArrowRotation(const cv::Mat& minimap)
{
    if (minimap.empty()) {
        return -1.0;
    }

    int cx = minimap.cols / 2;
    int cy = minimap.rows / 2;
    // 限制取样半径为 12 像素：假定代表人物方向的中心箭头仅存在于该极小区域内，避免误拾取外围的其他白色元素
    int radius = 12;

    if (cx - radius < 0 || cy - radius < 0 || cx + radius > minimap.cols || cy + radius > minimap.rows) {
        return -1.0;
    }

    cv::Rect roi(cx - radius, cy - radius, radius * 2, radius * 2);
    cv::Mat patch = minimap(roi);

    cv::Mat patchBGR;
    if (patch.channels() == 4) {
        cv::cvtColor(patch, patchBGR, cv::COLOR_BGRA2BGR);
    }
    else {
        patchBGR = patch.clone();
    }

    cv::Mat whiteMask;
    cv::inRange(patchBGR, cv::Scalar(220, 220, 220), cv::Scalar(255, 255, 255), whiteMask);

    std::vector<std::vector<cv::Point>> contours;
    cv::findContours(whiteMask, contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);

    if (contours.empty()) {
        return -1.0;
    }

    cv::Point2f centerPt((float)radius, (float)radius);
    size_t bestContourIdx = SIZE_MAX;
    double minDistSq = 1e9;

    for (size_t i = 0; i < contours.size(); ++i) {
        auto mu_temp = cv::moments(contours[i]);
        cv::Point2f c;
        if (mu_temp.m00 > 0) {
            c = cv::Point2f((float)(mu_temp.m10 / mu_temp.m00), (float)(mu_temp.m01 / mu_temp.m00));
        }
        else {
            c = cv::Point2f((float)contours[i][0].x, (float)contours[i][0].y);
        }

        double dSq = (c.x - centerPt.x) * (c.x - centerPt.x) + (c.y - centerPt.y) * (c.y - centerPt.y);
        if (dSq < minDistSq) {
            minDistSq = dSq;
            bestContourIdx = i;
        }
    }

    // 如果找到的最佳轮廓距离中心点太远 (平方距离 > 25)，说明这不是中心箭头，而是其他噪点，因此丢弃
    if (bestContourIdx == SIZE_MAX || minDistSq > 25.0) {
        return -1.0;
    }

    cv::Mat isolatedMask = cv::Mat::zeros(whiteMask.size(), CV_8UC1);
    cv::drawContours(isolatedMask, contours, static_cast<int>(bestContourIdx), cv::Scalar(255), cv::FILLED);

    cv::Mat highResMask;
    // 上采样16倍：原箭头极小且有像素锯齿，放大16倍效果最好，以利用亚像素级平滑计算边界质心，提升角度结算的精确度
    cv::resize(isolatedMask, highResMask, cv::Size(), 16.0, 16.0, cv::INTER_CUBIC);

    cv::threshold(highResMask, highResMask, 127, 255, cv::THRESH_BINARY);

    std::vector<std::vector<cv::Point>> hrContours;
    cv::findContours(highResMask, hrContours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
    if (hrContours.empty()) {
        return -1.0;
    }

    size_t hrBestIdx = 0;
    double maxArea = 0;
    for (size_t i = 0; i < hrContours.size(); ++i) {
        double area = cv::contourArea(hrContours[i]);
        if (area > maxArea) {
            maxArea = area;
            hrBestIdx = i;
        }
    }

    auto mu = cv::moments(hrContours[hrBestIdx]);
    if (mu.m00 <= 0) {
        return -1.0;
    }
    cv::Point2f centroid((float)(mu.m10 / mu.m00), (float)(mu.m01 / mu.m00));

    std::vector<cv::Point2f> triangle;
    // 使用最小外接三角形框定箭头，因为游戏的玩家箭头标志为等腰三角形变体，利用几何包络性过滤圆滑或异形的像素轮廓
    cv::minEnclosingTriangle(hrContours[hrBestIdx], triangle);
    if (triangle.size() != 3) {
        return -1.0;
    }

    int tipIdx = 0;
    cv::Point2f forward;
    if (estimate_arrow_forward(hrContours[hrBestIdx], centroid, &forward)) {
        double maxDot = -2.0;
        for (int i = 0; i < 3; ++i) {
            cv::Point2f d = triangle[i] - centroid;
            double n = std::sqrt((double)d.x * d.x + (double)d.y * d.y);
            double s = (n < 1e-9) ? -1.0 : ((double)d.x * forward.x + (double)d.y * forward.y) / n;
            if (s > maxDot) {
                maxDot = s;
                tipIdx = i;
            }
        }
    }
    else {
        double maxDistSq = -1.0;
        for (int i = 0; i < 3; ++i) {
            double distSq =
                (triangle[i].x - centroid.x) * (triangle[i].x - centroid.x) + (triangle[i].y - centroid.y) * (triangle[i].y - centroid.y);
            if (distSq > maxDistSq) {
                maxDistSq = distSq;
                tipIdx = i;
            }
        }
    }

    cv::Point2f tip = triangle[tipIdx];

    double dx = tip.x - centroid.x;
    double dy = tip.y - centroid.y;

    double angleDeg = std::atan2(dx, -dy) * 180.0 / CV_PI;
    if (angleDeg < 0) {
        angleDeg += 360.0;
    }
    return angleDeg;
}

} // namespace maplocator
