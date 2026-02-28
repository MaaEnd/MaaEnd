#include "MapAlgorithm.h"
#include "MapTypes.h"
#include <algorithm>
#include <vector>
#include <filesystem>

namespace maplocator {

cv::Mat GenerateMinimapMask(const cv::Mat &minimap, const ImageProcessingConfig &cfg, bool withUiMask, bool withCenterMask) {
  int w = minimap.cols, h = minimap.rows;
  int centerX = w / 2, centerY = h / 2;
  int radius = std::min(w, h) / 2 - cfg.borderMargin;
  if (radius < 0) radius = 0;

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

      cv::inRange(workImg, cv::Scalar(255, 255, 255), cv::Scalar(255, 255, 255), whiteMask);

      if (cfg.useHsvWhiteMask) {
          cv::Mat hsvImg, hsvWhite;
          cv::cvtColor(workImg, hsvImg, cv::COLOR_BGR2HSV);
          cv::inRange(hsvImg, cv::Scalar(0, 0, 200), cv::Scalar(180, 60, 255), hsvWhite);
          cv::bitwise_or(whiteMask, hsvWhite, whiteMask);
      }

      for (int y = 0; y < h; y++) {
          uchar *colorRow = colorIconMask.ptr<uchar>(y);
          const uchar *baseRow = baseMask.ptr<uchar>(y);
          const cv::Vec3b *imgRow = workImg.ptr<cv::Vec3b>(y);
          
          for (int x = 0; x < w; x++) {
              if (baseRow[x] == 0) continue;
              
              int b = imgRow[x][0], g = imgRow[x][1], r = imgRow[x][2];
              if ((r > 100 && g > 100 && std::min(r, g) - b > cfg.iconDiffThreshold) || 
                  (b > 140 && b > r + 50)) {
                  colorRow[x] = 255;
              }
          }
      }

      int cD = std::max(1, cfg.colorDilate);
      cv::dilate(colorIconMask, colorIconMask, cv::getStructuringElement(cv::MORPH_ELLIPSE, cv::Size(cD, cD)));
      baseMask.setTo(0, colorIconMask);

      int wD = std::max(1, cfg.whiteDilate);
      cv::dilate(whiteMask, whiteMask, cv::getStructuringElement(cv::MORPH_ELLIPSE, cv::Size(wD, wD)));
      baseMask.setTo(0, whiteMask);
  }

  if (withCenterMask) {
      cv::circle(baseMask, cv::Point(centerX, centerY), cfg.centerMaskRadius, cv::Scalar(0), -1);
  }

  cv::Mat gray;
  if (minimap.channels() == 4) cv::cvtColor(minimap, gray, cv::COLOR_BGRA2GRAY);
  else cv::cvtColor(minimap, gray, cv::COLOR_BGR2GRAY);

  cv::Mat darkMask;
  cv::threshold(gray, darkMask, cfg.minimapDarkMaskThreshold, 255, cv::THRESH_BINARY_INV);
  baseMask.setTo(0, darkMask);

  return baseMask;
}

double InferYellowArrowRotation(const cv::Mat& minimap) {
    if (minimap.empty()) return -1.0;

    int cx = minimap.cols / 2;
    int cy = minimap.rows / 2;
    int radius = 12; 

    if (cx - radius < 0 || cy - radius < 0 || cx + radius > minimap.cols || cy + radius > minimap.rows) {
        return -1.0;
    }

    cv::Rect roi(cx - radius, cy - radius, radius * 2, radius * 2);
    cv::Mat patch = minimap(roi);

    cv::Mat patchBGR;
    if (patch.channels() == 4) {
        cv::cvtColor(patch, patchBGR, cv::COLOR_BGRA2BGR);
    } else {
        patchBGR = patch.clone();
    }

    cv::Mat whiteMask;
    cv::inRange(patchBGR, cv::Scalar(220, 220, 220), cv::Scalar(255, 255, 255), whiteMask);

    std::vector<std::vector<cv::Point>> contours;
    cv::findContours(whiteMask, contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);

    if (contours.empty()) return -1.0;

    cv::Point2f centerPt((float)radius, (float)radius);
    int bestContourIdx = -1;
    double minDistSq = 1e9;

    for (int i = 0; i < contours.size(); ++i) {
        auto mu_temp = cv::moments(contours[i]);
        cv::Point2f c;
        if (mu_temp.m00 > 0) {
            c = cv::Point2f((float)(mu_temp.m10 / mu_temp.m00), (float)(mu_temp.m01 / mu_temp.m00));
        } else {
            c = cv::Point2f((float)contours[i][0].x, (float)contours[i][0].y);
        }

        double dSq = (c.x - centerPt.x) * (c.x - centerPt.x) + (c.y - centerPt.y) * (c.y - centerPt.y);
        if (dSq < minDistSq) {
            minDistSq = dSq;
            bestContourIdx = i;
        }
    }

    if (bestContourIdx == -1 || minDistSq > 25.0) return -1.0; 

    cv::Mat isolatedMask = cv::Mat::zeros(whiteMask.size(), CV_8UC1);
    cv::drawContours(isolatedMask, contours, bestContourIdx, cv::Scalar(255), cv::FILLED);

    cv::Mat highResMask;
    cv::resize(isolatedMask, highResMask, cv::Size(), 16.0, 16.0, cv::INTER_CUBIC);

    cv::threshold(highResMask, highResMask, 127, 255, cv::THRESH_BINARY);

    std::vector<std::vector<cv::Point>> hrContours;
    cv::findContours(highResMask, hrContours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
    if (hrContours.empty()) return -1.0;

    int hrBestIdx = 0;
    double maxArea = 0;
    for (int i = 0; i < hrContours.size(); ++i) {
        double area = cv::contourArea(hrContours[i]);
        if (area > maxArea) { maxArea = area; hrBestIdx = i; }
    }

    auto mu = cv::moments(hrContours[hrBestIdx]);
    if (mu.m00 <= 0) return -1.0;
    cv::Point2f centroid((float)(mu.m10 / mu.m00), (float)(mu.m01 / mu.m00));

    std::vector<cv::Point2f> triangle;
    cv::minEnclosingTriangle(hrContours[hrBestIdx], triangle);
    if (triangle.size() != 3) return -1.0;

    int tipIdx = 0;
    double maxDistSq = -1.0;
    for (int i = 0; i < 3; ++i) {
        double distSq = (triangle[i].x - centroid.x) * (triangle[i].x - centroid.x) + 
                        (triangle[i].y - centroid.y) * (triangle[i].y - centroid.y);
        if (distSq > maxDistSq) {
            maxDistSq = distSq;
            tipIdx = i;
        }
    }

    cv::Point2f tip = triangle[tipIdx];

    double dx = tip.x - centroid.x;
    double dy = tip.y - centroid.y;

    double angleDeg = std::atan2(dx, -dy) * 180.0 / CV_PI;
    if (angleDeg < 0) angleDeg += 360.0;
    return angleDeg;
}

} // namespace maplocator
