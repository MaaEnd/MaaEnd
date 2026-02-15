#include "MapAlgorithm.h"
#include "MapTypes.h"
#include <algorithm>
#include <cmath>
#include <vector>

namespace maplocator {

cv::Mat GenerateMinimapMask(const cv::Mat &minimap, const ImageProcessingConfig &cfg) {
  int w = minimap.cols, h = minimap.rows;
  cv::Mat baseMask = cv::Mat::zeros(h, w, CV_8UC1);
  int centerX = w / 2, centerY = h / 2;
  int radiusSq = (std::min(w, h) / 2) * (std::min(w, h) / 2);

  cv::Mat workImg = minimap;
  cv::Mat tempBGR;
  if (workImg.channels() == 4) {
      cv::cvtColor(workImg, tempBGR, cv::COLOR_BGRA2BGR);
      workImg = tempBGR;
  }

  // 圆形遮罩 + 黄蓝图标剔除
  for (int y = 0; y < h; y++) {
    uchar *maskRow = baseMask.ptr<uchar>(y);
    const cv::Vec3b *imgRow = workImg.ptr<cv::Vec3b>(y);
    for (int x = 0; x < w; x++) {
      if ((x - centerX) * (x - centerX) + (y - centerY) * (y - centerY) > radiusSq) continue;
      int b = imgRow[x][0], g = imgRow[x][1], r = imgRow[x][2];
      bool isIcon = false;
      if (r > 100 && g > 100 && std::min(r, g) - b > cfg.iconDiffThreshold) isIcon = true;
      if (!isIcon && b > 100 && b - std::max(r, g) > cfg.iconDiffThreshold) isIcon = true;
      if (!isIcon) maskRow[x] = 255;
    }
  }
  cv::circle(baseMask, cv::Point(centerX, centerY), cfg.centerMaskRadius, cv::Scalar(0), -1);

  cv::Mat gray;
  if (minimap.channels() == 4) cv::cvtColor(minimap, gray, cv::COLOR_BGRA2GRAY);
  else cv::cvtColor(minimap, gray, cv::COLOR_BGR2GRAY);

  // 暗部剔除
  cv::Mat darkMask;
  cv::threshold(gray, darkMask, cfg.minimapDarkMaskThreshold, 255, cv::THRESH_BINARY_INV);
  baseMask.setTo(0, darkMask);

  cv::Mat floatMask;
  baseMask.convertTo(floatMask, CV_32F, 1.0 / 255.0);

  // 梯度加权: 纹理丰富区域贡献更大, 平坦区保底0.1防零贡献
  cv::Mat gradX, gradY, gradMag;
  cv::Sobel(gray, gradX, CV_32F, 1, 0);
  cv::Sobel(gray, gradY, CV_32F, 0, 1);
  gradMag = cv::abs(gradX) + cv::abs(gradY);

  double maxVal;
  cv::minMaxLoc(gradMag, nullptr, &maxVal);
  if (maxVal > 0) gradMag /= maxVal;

  cv::add(gradMag, cfg.gradientBaseWeight, gradMag);
  cv::threshold(gradMag, gradMag, 1.0, 1.0, cv::THRESH_TRUNC);

  cv::Mat finalMask;
  cv::multiply(floatMask, gradMag, finalMask);
  return finalMask;
}

} // namespace maplocator
