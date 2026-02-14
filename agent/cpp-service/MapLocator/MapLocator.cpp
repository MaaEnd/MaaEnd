#include "MapLocator.h"
#include "../Logger.h"

#include <algorithm>
#include <filesystem>
#include <iostream>
#include <sstream>
#include <regex>
#include <vector>
#include <unordered_map>

namespace fs = std::filesystem;

namespace maplocator {

static std::shared_ptr<MapLocator> g_globalLocator;

void SetGlobalLocator(std::shared_ptr<MapLocator> locator) {
  g_globalLocator = locator;
}

std::shared_ptr<MapLocator> GetGlobalLocator() { return g_globalLocator; }

MapLocator::MapLocator(const std::string &mapRoot, const std::string &yoloModelPath)
    : lostTrackingCount(MaxLostTrackingCount + 1), velocityX(0), velocityY(0) {
  loadAvailableZones(mapRoot);

  if (!yoloModelPath.empty()) {
    try {
      yoloNet = cv::dnn::readNetFromONNX(yoloModelPath);
      isYoloLoaded = !yoloNet.empty();
      // 顺序必须与ONNX导出时的class_names一致
      yoloClassNames = {
          "Map01Base", "Map01Lv001Tier114", "Map01Lv001Tier115", "Map01Lv001Tier171",
          "Map01Lv001Tier172", "Map01Lv001Tier173", "Map01Lv001Tier174", "Map01Lv001Tier189",
          "Map01Lv002Tier120", "Map01Lv003Tier17", "Map01Lv003Tier18", "Map01Lv003Tier19",
          "Map01Lv003Tier31", "Map01Lv005Tier175", "Map01Lv005Tier177", "Map01Lv006Tier107",
          "Map01Lv006Tier109", "Map01Lv006Tier86", "Map01Lv006Tier87", "Map01Lv007Tier130",
          "Map01Lv007Tier133", "Map01Lv007Tier135", "Map01Lv007Tier81", "Map02Base",
          "Map02Lv001Tier277", "Map02Lv002Tier254", "Map02Lv002Tier255", "Map02Lv002Tier257",
          "Map02Lv002Tier258", "Map02Lv002Tier259", "Map02Lv002Tier275", "Map02Lv002Tier276"
      };
      Logger::Info("[MapLocator] YOLO Model loaded successfully.");
    } catch (const cv::Exception& e) {
      Logger::Error("[MapLocator] YOLO model load failed: " + std::string(e.what()));
    }
  }
}

// 将地图暗部/虚空统一压成纯黑, 避免与小地图的黑色背景产生误匹配
static void PreprocessMap(cv::Mat &img, const ImageProcessingConfig &cfg) {
  if (img.empty()) return;

  cv::Mat gray;
  if (img.channels() == 4)
    cv::cvtColor(img, gray, cv::COLOR_BGRA2GRAY);
  else
    cv::cvtColor(img, gray, cv::COLOR_BGR2GRAY);

  double thresholdValue = cfg.darkMapThreshold;

  cv::Mat darkMask;
  cv::threshold(gray, darkMask, thresholdValue, 255, cv::THRESH_BINARY_INV);

  if (img.channels() == 4) {
    std::vector<cv::Mat> channels;
    cv::split(img, channels);
    channels[0].setTo(0, darkMask);
    channels[1].setTo(0, darkMask);
    channels[2].setTo(0, darkMask);
    cv::merge(channels, img);
  } else {
    img.setTo(cv::Scalar(0, 0, 0), darkMask);
  }
}

struct MatchResultRaw {
    double avgDiff;
    cv::Point loc;
};

// blurredTempl由调用方预先模糊, 避免每次CoreMatch重复计算
static std::optional<MatchResultRaw> CoreMatch(
    const cv::Mat& searchImg,
    const cv::Mat& blurredTempl,
    const cv::Mat& weightMask,
    double weightedPixels,
    int blurSize = 5
) {
    if (searchImg.rows < blurredTempl.rows || searchImg.cols < blurredTempl.cols)
        return std::nullopt;

    cv::Mat blurredMap;
    cv::GaussianBlur(searchImg, blurredMap, cv::Size(blurSize, blurSize), 0);

    cv::Mat result;
    cv::matchTemplate(blurredMap, blurredTempl, result, cv::TM_SQDIFF, weightMask);

    double minVal;
    cv::Point minLoc;
    cv::minMaxLoc(result, &minVal, nullptr, &minLoc, nullptr);

    // *3.0: BGR三通道, weightedPixels是mask权重平方和
    double avgDiff = minVal / (weightedPixels * 3.0);
    return MatchResultRaw{avgDiff, minLoc};
}

std::string MapLocator::convertYoloNameToZoneId(const std::string &yoloName) {
  // YOLO标签名(Map01Base)到文件系统Zone名(ValleyIV_Base)的映射
  static const std::unordered_map<std::string, std::string> regionDict = {
      {"Map01", "ValleyIV"},
      {"Map02", "Wuling"}
  };

  std::string prefix = yoloName.length() >= 5 ? yoloName.substr(0, 5) : yoloName;

  std::string regionName;
  auto it = regionDict.find(prefix);
  if (it != regionDict.end()) regionName = it->second;
  else regionName = prefix;

  if (yoloName.find("Base") != std::string::npos)
    return regionName + "_Base";

  std::regex re(R"((Map\d+)Lv0*(\d+)Tier0*(\d+))");
  std::smatch match;
  if (std::regex_search(yoloName, match, re))
    return regionName + "_L" + match[2].str() + "_" + match[3].str();

  return yoloName;
}

std::string MapLocator::predictZoneByYOLO(const cv::Mat &minimap) {
  std::lock_guard<std::mutex> lock(yoloMutex);

  if (!isYoloLoaded) {
      Logger::Error("[MapLocator] YOLO Error: Model is NOT loaded.");
      return "";
  }
  if (minimap.empty()) {
      Logger::Error("[MapLocator] YOLO Error: Input minimap is empty.");
      return "";
  }

  const int OUTPUT_SIZE = 128;
  const int MASK_DIAMETER = 106; // 小地图有效区域直径

  cv::Mat img3C;
  if (minimap.channels() == 4) cv::cvtColor(minimap, img3C, cv::COLOR_BGRA2BGR);
  else img3C = minimap.clone();

  // 居中填充到128x128黑底, 复现训练时的预处理
  cv::Mat canvas = cv::Mat::zeros(OUTPUT_SIZE, OUTPUT_SIZE, CV_8UC3);
  int h = img3C.rows, w = img3C.cols;
  int start_y = std::max(0, (OUTPUT_SIZE - h) / 2);
  int start_x = std::max(0, (OUTPUT_SIZE - w) / 2);
  int crop_h = std::min(h, OUTPUT_SIZE);
  int crop_w = std::min(w, OUTPUT_SIZE);

  cv::Rect canvas_roi(start_x, start_y, crop_w, crop_h);
  cv::Rect img_roi((w - crop_w) / 2, (h - crop_h) / 2, crop_w, crop_h);
  img3C(img_roi).copyTo(canvas(canvas_roi));

  cv::Mat mask = cv::Mat::zeros(OUTPUT_SIZE, OUTPUT_SIZE, CV_8UC1);
  cv::circle(mask, cv::Point(OUTPUT_SIZE / 2, OUTPUT_SIZE / 2), MASK_DIAMETER / 2, cv::Scalar(255), -1);

  cv::Mat processed_img;
  cv::bitwise_and(canvas, canvas, processed_img, mask);

  cv::Mat blob;
  cv::dnn::blobFromImage(processed_img, blob, 1.0 / 255.0, cv::Size(OUTPUT_SIZE, OUTPUT_SIZE), cv::Scalar(0, 0, 0), true, false);
  yoloNet.setInput(blob);
  cv::Mat output = yoloNet.forward();

  cv::Point classIdPoint;
  double top1_conf;
  cv::minMaxLoc(output, nullptr, &top1_conf, nullptr, &classIdPoint);

  std::string predictedName = "Unknown";
  if (classIdPoint.x >= 0 && classIdPoint.x < (int)yoloClassNames.size())
      predictedName = yoloClassNames[classIdPoint.x];

  Logger::Info("[MapLocator] YOLO Raw: Class=" + predictedName +
               " (" + std::to_string(classIdPoint.x) + "), Conf=" + std::to_string(top1_conf));

  if (top1_conf > matchCfg.yoloConfThreshold && classIdPoint.x < (int)yoloClassNames.size()) {
    std::string zoneId = convertYoloNameToZoneId(predictedName);
    Logger::Info("[MapLocator] YOLO Success: " + predictedName + " -> ZoneId: " + zoneId +
                 " (Conf: " + std::to_string(top1_conf * 100.0) + "%)");
    return zoneId;
  } else {
      if (top1_conf <= matchCfg.yoloConfThreshold)
          Logger::Info("[MapLocator] YOLO Fail: Low Confidence (" + std::to_string(top1_conf) +
                       " <= " + std::to_string(matchCfg.yoloConfThreshold) + ")");
      else
          Logger::Info("[MapLocator] YOLO Fail: Index Out of Bounds (" + std::to_string(classIdPoint.x) +
                       "/" + std::to_string(yoloClassNames.size()) + ")");
  }

  return "";
}

void MapLocator::loadAvailableZones(const std::string &root) {
  if (!fs::exists(root)) return;

  std::regex layerFileRegex(R"(Lv(\d+)Tier(\d+)\.(png|jpg|webp)$)", std::regex_constants::icase);

  for (const auto &entry : fs::recursive_directory_iterator(root)) {
    if (entry.is_directory()) continue;
    std::string filename = entry.path().filename().string();
    std::string parentName = entry.path().parent_path().filename().string();

    std::string key;
    std::string filenameLower = filename;
    std::transform(filenameLower.begin(), filenameLower.end(), filenameLower.begin(), ::tolower);

    if (filenameLower == "base.png") {
      key = parentName + "_Base";
    } else {
      std::smatch matches;
      if (std::regex_search(filename, matches, layerFileRegex)) {
        std::string lv = matches[1].str();
        std::string tier = matches[2].str();
        lv.erase(0, std::min(lv.find_first_not_of('0'), lv.size() - 1));
        tier.erase(0, std::min(tier.find_first_not_of('0'), tier.size() - 1));
        key = parentName + "_L" + lv + "_" + tier;
      } else {
        continue;
      }
    }

    // 完整处理后再插入zones, 避免异常导致半成品残留
    cv::Mat img = cv::imread(entry.path().string(), cv::IMREAD_UNCHANGED);
    if (img.empty()) continue;
    if (img.channels() == 3) cv::cvtColor(img, img, cv::COLOR_BGR2BGRA);
    PreprocessMap(img, imgCfg);
    zones[key] = std::move(img);
    Logger::Info("Loaded Map: " + key);
  }
}

cv::Mat MapLocator::GeneratePerfectWeightMask(const cv::Mat &minimap) {
  int w = minimap.cols, h = minimap.rows;
  cv::Mat baseMask = cv::Mat::zeros(h, w, CV_8UC1);
  int centerX = w / 2, centerY = h / 2;
  int radiusSq = (std::min(w, h) / 2) * (std::min(w, h) / 2);

  // at()太慢, ptr行指针+圆内判定手动迭代
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
      if (r > 100 && g > 100 && std::min(r, g) - b > imgCfg.iconDiffThreshold) isIcon = true;
      if (!isIcon && b > 100 && b - std::max(r, g) > imgCfg.iconDiffThreshold) isIcon = true;
      if (!isIcon) maskRow[x] = 255;
    }
  }
  cv::circle(baseMask, cv::Point(centerX, centerY), imgCfg.centerMaskRadius, cv::Scalar(0), -1);

  cv::Mat gray;
  if (minimap.channels() == 4) cv::cvtColor(minimap, gray, cv::COLOR_BGRA2GRAY);
  else cv::cvtColor(minimap, gray, cv::COLOR_BGR2GRAY);

  // 暗部剔除: 与PreprocessMap阈值对齐, 否则双方都变黑后产生黑吃黑误匹配
  cv::Mat darkMask;
  cv::threshold(gray, darkMask, imgCfg.minimapDarkMaskThreshold, 255, cv::THRESH_BINARY_INV);
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

  cv::add(gradMag, imgCfg.gradientBaseWeight, gradMag);
  cv::threshold(gradMag, gradMag, 1.0, 1.0, cv::THRESH_TRUNC);

  cv::Mat finalMask;
  cv::multiply(floatMask, gradMag, finalMask);
  return finalMask;
}

MapLocator::PreparedTemplate MapLocator::prepareTemplate(const cv::Mat &minimap) {
  PreparedTemplate result;

  if (minimap.channels() == 4)
    cv::cvtColor(minimap, result.templRaw, cv::COLOR_BGRA2BGR);
  else
    result.templRaw = minimap.clone();

  // 小地图也要PreprocessMap, 否则与大地图暗部不一致导致SQDIFF偏高
  result.templRaw.copyTo(result.templ);
  PreprocessMap(result.templ, imgCfg);

  result.weightMask = GeneratePerfectWeightMask(minimap);

  // weightedPixels = sum(mask^2), 作为SQDIFF归一化分母
  cv::Mat maskSq;
  cv::multiply(result.weightMask, result.weightMask, maskSq);
  result.weightedPixels = cv::sum(maskSq)[0];
  if (result.weightedPixels < 1.0) result.weightedPixels = 1.0;

  cv::GaussianBlur(result.templ, result.blurredTempl,
                   cv::Size(matchCfg.blurSize, matchCfg.blurSize), 0);

  return result;
}

MapLocator::TrackingValidation MapLocator::validateTracking(
    const MatchResultRaw &trackResult,
    const cv::Mat &searchRoi,
    const cv::Mat &templ,
    const cv::Rect &searchRect,
    std::chrono::duration<double> dt) {

  TrackingValidation v;

  // 边缘吸附: 结果贴边说明目标在搜索区域外
  int maxX = searchRoi.cols - templ.cols;
  int maxY = searchRoi.rows - templ.rows;
  bool hitEdgeX = (trackResult.loc.x <= trackingCfg.edgeSnapMargin ||
                   trackResult.loc.x >= maxX - trackingCfg.edgeSnapMargin);
  bool hitEdgeY = (trackResult.loc.y <= trackingCfg.edgeSnapMargin ||
                   trackResult.loc.y >= maxY - trackingCfg.edgeSnapMargin);
  v.isEdgeSnapped = hitEdgeX || hitEdgeY;

  // 速度校验: 超过物理极限说明是传送/切图
  v.absX = (double)searchRect.x + trackResult.loc.x + templ.cols / 2.0;
  v.absY = (double)searchRect.y + trackResult.loc.y + templ.rows / 2.0;
  double dx = v.absX - lastKnownPos->x;
  double dy = v.absY - lastKnownPos->y;
  double distanceMoved = std::sqrt(dx * dx + dy * dy);
  double dtSec = dt.count();
  if (dtSec < 0.001) dtSec = 0.001;
  double currentSpeed = distanceMoved / dtSec;
  v.isTeleported = currentSpeed > trackingCfg.maxNormalSpeed;

  v.isScreenBlocked = trackResult.avgDiff > trackingCfg.screenBlockedThreshold;

  v.isValid = !v.isEdgeSnapped && !v.isTeleported && !v.isScreenBlocked;

  if (!v.isValid) {
    std::stringstream ss;
    ss << "[MapLocator] Tracking Lost! Reason: ";
    if (v.isScreenBlocked) ss << "Screen Blocked (Score: " << trackResult.avgDiff << "). ";
    if (v.isEdgeSnapped) ss << "Edge Snapped (loc: " << trackResult.loc.x << "," << trackResult.loc.y << "). ";
    if (v.isTeleported) ss << "Impossible Speed (" << currentSpeed << " px/s).";
    Logger::Info(ss.str());
  }

  return v;
}

std::optional<MapPosition> MapLocator::tryTracking(
    const PreparedTemplate &tmpl,
    std::chrono::steady_clock::time_point now) {

  if (currentZoneId.empty() || !lastKnownPos.has_value() ||
      lostTrackingCount > MaxLostTrackingCount)
    return std::nullopt;

  auto it = zones.find(currentZoneId);
  if (it == zones.end()) return std::nullopt;

  const cv::Mat &zoneMap = it->second;

  std::chrono::duration<double> dt = now - lastTime;
  double dtSec = dt.count();
  if (dtSec > trackingCfg.maxDtForPrediction) { dtSec = 0; velocityX = 0; velocityY = 0; }
  double predX = lastKnownPos->x + velocityX * dtSec;
  double predY = lastKnownPos->y + velocityY * dtSec;

  int pad = static_cast<int>(MobileSearchRadius) + std::max(tmpl.templ.cols, tmpl.templ.rows) / 2;
  cv::Rect searchRect((int)predX - pad, (int)predY - pad, pad * 2, pad * 2);

  // 黑色填充处理越界: searchRect可能超出地图边界
  cv::Mat searchRoiWithPad(searchRect.size(), zoneMap.type(), cv::Scalar(0, 0, 0, 0));
  cv::Rect mapBounds(0, 0, zoneMap.cols, zoneMap.rows);
  cv::Rect validRoi = searchRect & mapBounds;
  if (!validRoi.empty()) {
      zoneMap(validRoi).copyTo(searchRoiWithPad(cv::Rect(
          validRoi.x - searchRect.x, validRoi.y - searchRect.y,
          validRoi.width, validRoi.height)));
  }

  cv::Mat searchRoi;
  if (searchRoiWithPad.channels() == 4)
    cv::cvtColor(searchRoiWithPad, searchRoi, cv::COLOR_BGRA2BGR);
  else
    searchRoi = searchRoiWithPad;

  auto trackResult = CoreMatch(searchRoi, tmpl.blurredTempl, tmpl.weightMask,
                               tmpl.weightedPixels, matchCfg.blurSize);
  if (!trackResult) return std::nullopt;

  auto validation = validateTracking(*trackResult, searchRoi, tmpl.templ, searchRect, dt);

  if (validation.isValid) {
    MapPosition pos;
    pos.zoneId = currentZoneId;
    pos.x = validation.absX;
    pos.y = validation.absY;
    pos.avgDiff = trackResult->avgDiff;
    updateMotionModel(pos, now);
    lastKnownPos = pos;
    lastTime = now;
    lostTrackingCount = 0;
    return pos;
  }

  return std::nullopt;
}

std::optional<MapPosition> MapLocator::evaluateAndAcceptResult(
    const MatchResultRaw &fineRes,
    const cv::Rect &validFineRect,
    const cv::Mat &templ,
    const std::string &targetZoneId) {

  cv::Point absoluteLoc(validFineRect.x + fineRes.loc.x, validFineRect.y + fineRes.loc.y);
  double finalAvgDiff = fineRes.avgDiff;

  if (finalAvgDiff > matchCfg.passThreshold) {
    Logger::Info("[MapLocator] Rejected by Score. AvgDiff: " + std::to_string(finalAvgDiff));
    return std::nullopt;
  }

  MapPosition pos;
  pos.zoneId = targetZoneId;
  pos.x = (double)absoluteLoc.x + templ.cols / 2.0;
  pos.y = (double)absoluteLoc.y + templ.rows / 2.0;
  pos.avgDiff = finalAvgDiff;
  return pos;
}

std::optional<MapPosition> MapLocator::tryGlobalSearch(
    const PreparedTemplate &tmpl) {

  std::string targetZoneId = predictZoneByYOLO(tmpl.templRaw);

  if (targetZoneId.empty()) {
      Logger::Info("[MapLocator] Global Search Aborted: YOLO returned no result.");
      return std::nullopt;
  }

  if (zones.find(targetZoneId) == zones.end()) {
      Logger::Info("[MapLocator] Global Search Aborted: YOLO predicted '" + targetZoneId +
                   "', but this map is NOT loaded in 'zones'.");
      return std::nullopt;
  }

  const cv::Mat &bigMap = zones.at(targetZoneId);
  cv::Mat bigMapSearch;
  if (bigMap.channels() == 4) cv::cvtColor(bigMap, bigMapSearch, cv::COLOR_BGRA2BGR);
  else bigMapSearch = bigMap;

  double scale = matchCfg.coarseScale;
  cv::Mat smallTempl, smallWeightMask;
  cv::resize(tmpl.templ, smallTempl, cv::Size(), scale, scale, cv::INTER_AREA);
  cv::resize(tmpl.weightMask, smallWeightMask, cv::Size(), scale, scale, cv::INTER_AREA);

  // 粗搜
  cv::Mat smallMap, smallResult;
  cv::resize(bigMapSearch, smallMap, cv::Size(), scale, scale, cv::INTER_AREA);
  cv::matchTemplate(smallMap, smallTempl, smallResult, cv::TM_SQDIFF, smallWeightMask);

  double coarseMin; cv::Point coarseMinLoc;
  cv::minMaxLoc(smallResult, &coarseMin, nullptr, &coarseMinLoc, nullptr);
  int coarseX = static_cast<int>(coarseMinLoc.x / scale);
  int coarseY = static_cast<int>(coarseMinLoc.y / scale);

  // 精搜
  int searchRadius = matchCfg.fineSearchRadius;
  cv::Rect fineRect(coarseX - searchRadius, coarseY - searchRadius,
                    tmpl.templ.cols + searchRadius * 2, tmpl.templ.rows + searchRadius * 2);
  cv::Rect mapBounds(0, 0, bigMapSearch.cols, bigMapSearch.rows);
  cv::Rect validFineRect = fineRect & mapBounds;

  if (validFineRect.empty()) return std::nullopt;

  cv::Mat fineMap = bigMapSearch(validFineRect);
  auto fineRes = CoreMatch(fineMap, tmpl.blurredTempl, tmpl.weightMask,
                           tmpl.weightedPixels, matchCfg.blurSize);

  if (!fineRes) return std::nullopt;

  return evaluateAndAcceptResult(*fineRes, validFineRect, tmpl.templ, targetZoneId);
}

std::optional<MapPosition> MapLocator::locate(const cv::Mat &minimap) {
  auto tmpl = prepareTemplate(minimap);
  auto now = std::chrono::steady_clock::now();

  // Check async YOLO result
  if (asyncYoloTask.valid() && asyncYoloTask.wait_for(std::chrono::seconds(0)) == std::future_status::ready) {
      std::string predictedZone = asyncYoloTask.get();
            if (!predictedZone.empty() && !currentZoneId.empty() && predictedZone != currentZoneId) {
                Logger::Info("[MapLocator] Async YOLO detected zone change: " + currentZoneId + " -> " + predictedZone);
                lostTrackingCount = MaxLostTrackingCount + 1;
                lastKnownPos = std::nullopt;
            }  }

  // Dispatch new async YOLO task
  if (!asyncYoloTask.valid()) {
      auto elapsed = std::chrono::duration_cast<std::chrono::seconds>(now - lastYoloCheckTime).count();
      if (elapsed >= 3 && isYoloLoaded) {
          lastYoloCheckTime = now;
          cv::Mat yoloInput = tmpl.templRaw.clone();
          asyncYoloTask = std::async(std::launch::async, [this, yoloInput]() {
              return predictZoneByYOLO(yoloInput);
          });
      }
  }

  auto trackingResult = tryTracking(tmpl, now);
  if (trackingResult) return trackingResult;

  Logger::Info("[MapLocator] Tracking Missed. Invoking YOLO...");
  auto globalResult = tryGlobalSearch(tmpl);

  if (!globalResult) {
    lostTrackingCount++;
    if (lostTrackingCount > MaxLostTrackingCount) lastKnownPos.reset();
    return std::nullopt;
  }

  if (currentZoneId != globalResult->zoneId) {
    velocityX = 0; velocityY = 0;
  } else {
    updateMotionModel(*globalResult, now);
  }

  currentZoneId = globalResult->zoneId;
  lastKnownPos = globalResult;
  lastTime = now;
  lostTrackingCount = 0;
  return globalResult;
}

void MapLocator::updateMotionModel(const MapPosition &newPos, std::chrono::steady_clock::time_point now) {
  if (lastKnownPos.has_value() && lostTrackingCount == 0) {
    std::chrono::duration<double> dt = now - lastTime;
    double dtSec = dt.count();
    // 16ms~maxDt: 正常帧间隔; 超出范围说明卡顿或刚启动, 速度不可信
    if (dtSec > 0.016 && dtSec < trackingCfg.maxDtForPrediction) {
      double rawVx = (newPos.x - lastKnownPos->x) / dtSec;
      double rawVy = (newPos.y - lastKnownPos->y) / dtSec;
      double alpha = trackingCfg.velocitySmoothingAlpha;
      velocityX = velocityX * (1.0 - alpha) + rawVx * alpha;
      velocityY = velocityY * (1.0 - alpha) + rawVy * alpha;
      return;
    }
  }
  if (lostTrackingCount > 0) { velocityX = 0; velocityY = 0; }
}

std::optional<MapPosition> MapLocator::getLastKnownPos() const { return lastKnownPos; }

} // namespace maplocator
