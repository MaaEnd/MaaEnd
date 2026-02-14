#pragma once

#include "MapTypes.h"
#include <chrono>
#include <map>
#include <memory>
#include <opencv2/opencv.hpp>
#include <opencv2/dnn.hpp>
#include <optional>
#include <string>
#include <vector>
#include <future>
#include <mutex>

namespace maplocator {

struct MatchResultRaw;

class MapLocator {
public:
  explicit MapLocator(const std::string &mapRoot, const std::string &yoloModelPath = "");
  ~MapLocator();

  std::optional<MapPosition> locate(const cv::Mat &minimap);
  std::optional<MapPosition> getLastKnownPos() const;

private:
  struct PreparedTemplate {
    cv::Mat templ;
    cv::Mat templRaw;       // 给YOLO吃的未预处理原图
    cv::Mat blurredTempl;
    cv::Mat weightMask;
    double weightedPixels;  // SQDIFF归一化分母
  };
  PreparedTemplate prepareTemplate(const cv::Mat &minimap);

  std::optional<MapPosition> tryTracking(
      const PreparedTemplate &tmpl,
      std::chrono::steady_clock::time_point now);

  struct TrackingValidation {
    bool isValid;
    bool isEdgeSnapped;
    bool isTeleported;
    bool isScreenBlocked;
    double absX, absY;
  };
  TrackingValidation validateTracking(
      const MatchResultRaw &trackResult,
      const cv::Mat &searchRoi,
      const cv::Mat &templ,
      const cv::Rect &searchRect,
      std::chrono::duration<double> dt);

  std::optional<MapPosition> tryGlobalSearch(const PreparedTemplate &tmpl);

  std::optional<MapPosition> evaluateAndAcceptResult(
      const MatchResultRaw &fineRes,
      const cv::Rect &validFineRect,
      const cv::Mat &templ,
      const std::string &targetZoneId);

  void loadAvailableZones(const std::string &rootDesc);
  void updateMotionModel(const MapPosition &newPos, std::chrono::steady_clock::time_point now);
  std::string predictZoneByYOLO(const cv::Mat &minimap);
  std::string convertYoloNameToZoneId(const std::string &yoloName);
  cv::Mat GeneratePerfectWeightMask(const cv::Mat &minimap);

  std::map<std::string, cv::Mat> zones;
  std::string currentZoneId;
  std::optional<MapPosition> lastKnownPos;
  int lostTrackingCount;
  double velocityX;
  double velocityY;
  std::chrono::steady_clock::time_point lastTime;

  cv::dnn::Net yoloNet;
  bool isYoloLoaded = false;
  std::vector<std::string> yoloClassNames;
  
  std::mutex yoloMutex;
  std::mutex taskMutex;
  std::future<std::string> asyncYoloTask;
  std::chrono::steady_clock::time_point lastYoloCheckTime;

  TrackingConfig trackingCfg;
  MatchConfig matchCfg;
  ImageProcessingConfig imgCfg;
};

void SetGlobalLocator(std::shared_ptr<MapLocator> locator);
std::shared_ptr<MapLocator> GetGlobalLocator();

} // namespace maplocator
