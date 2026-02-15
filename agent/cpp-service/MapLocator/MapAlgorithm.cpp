#include "MapAlgorithm.h"
#include "MapTypes.h"
#include <algorithm>
#include <cmath>
#include <vector>

namespace maplocator {

cv::Mat GenerateMinimapMask(const cv::Mat &minimap) {
  int w = minimap.cols;
  int h = minimap.rows;
  cv::Mat mask = cv::Mat::zeros(h, w, CV_8UC1);

  int centerX = w / 2;
  int centerY = h / 2;
  int radius = std::min(w, h) / 2;
  int radiusSq = radius * radius;

  for (int y = 0; y < h; y++) {
    for (int x = 0; x < w; x++) {
      int dx = x - centerX;
      int dy = y - centerY;
      if (dx * dx + dy * dy <= radiusSq)
        mask.at<uchar>(y, x) = 255;
    }
  }

  // 40: 黄/蓝图标与地图底色的最小色差, 低于此值可能误伤地图本身的彩色区域
  const int DiffThreshold = 40;
  
  cv::Mat workImg = minimap;
  cv::Mat tempBGR;
  if (workImg.channels() == 4) {
      cv::cvtColor(workImg, tempBGR, cv::COLOR_BGRA2BGR);
      workImg = tempBGR;
  }

  for (int y = 0; y < h; y++) {
    uchar *maskRow = mask.ptr<uchar>(y);
    const cv::Vec3b *imgRow = workImg.ptr<cv::Vec3b>(y);
    
    for (int x = 0; x < w; x++) {
      if (maskRow[x] == 0) continue;

      int b = imgRow[x][0], g = imgRow[x][1], r = imgRow[x][2];

      bool isIcon = false;
      if (r > 100 && g > 100) {
        if ((std::min(r, g) - b) > DiffThreshold) isIcon = true;
      }
      if (!isIcon && b > 100) {
        if ((b - std::max(r, g)) > DiffThreshold) isIcon = true;
      }

      if (isIcon) maskRow[x] = 0;
    }
  }

  // 10px: 覆盖玩家箭头, 实测箭头半径约8px, 留2px余量
  cv::circle(mask, cv::Point(w / 2, h / 2), 10, cv::Scalar(0), -1);

  return mask;
}

} // namespace maplocator
