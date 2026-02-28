#include "MapLocator.h"
#include "MapAlgorithm.h"
#include <MaaUtils/Logger.h>
#include "YoloPredictor.h"
#include "MotionTracker.h"
#include "MatchStrategy.h"

#include <algorithm>
#include <filesystem>
#include <MaaUtils/Platform.h>
#include <MaaUtils/ImageIo.h>
#include <boost/regex.hpp>
#include <vector>
#include <meojson/json.hpp>
#include <future>
#include <mutex>

using Json = json::value;

namespace fs = std::filesystem;

namespace maplocator {

class MapLocator::Impl {
public:
  Impl() = default;
  ~Impl() {
      if (asyncYoloTask.valid()) {
          asyncYoloTask.wait();
      }
  }

  bool initialize(const MapLocatorConfig& cfg);
  bool getIsInitialized() const { return isInitialized; }
  LocateResult locate(const cv::Mat &minimap, const LocateOptions& options);
  void resetTrackingState();
  std::optional<MapPosition> getLastKnownPos() const;

private:
  std::optional<MapPosition> tryTracking(
      const MatchFeature &tmplFeat,
      IMatchStrategy* strategy,
      std::chrono::steady_clock::time_point now,
      const LocateOptions& options,
      MapPosition* outRawPos = nullptr);

  std::optional<MapPosition> tryGlobalSearch(
      const MatchFeature &tmplFeat,
      IMatchStrategy* strategy,
      const std::string &targetZoneId,
      MapPosition* outRawPos = nullptr);

  std::optional<MapPosition> evaluateAndAcceptResult(
      const MatchResultRaw &fineRes,
      const cv::Rect &validFineRect,
      const cv::Mat &templ,
      IMatchStrategy* strategy,
      const std::string &targetZoneId);

  void loadAvailableZones(const std::string &root);

  bool isInitialized = false;
  MapLocatorConfig config;

  std::map<std::string, cv::Mat> zones;
  std::string currentZoneId;

  std::unique_ptr<MotionTracker> motionTracker;
  std::unique_ptr<YoloPredictor> zoneClassifier;
  std::mutex taskMutex;
  std::future<std::string> asyncYoloTask;
  std::chrono::steady_clock::time_point lastYoloCheckTime;

  TrackingConfig trackingCfg;
  MatchConfig matchCfg;
  ImageProcessingConfig baseImgCfg = {
      .darkMapThreshold = 20.0,
      .iconDiffThreshold = 40,
      .centerMaskRadius = 18,
      .gradientBaseWeight = 0.1,
      .minimapDarkMaskThreshold = 20,
      .borderMargin = 10,
      .whiteDilate = 11,
      .colorDilate = 3,
      .useHsvWhiteMask = true
  };

  ImageProcessingConfig tierImgCfg = {
      .darkMapThreshold = 20.0,
      .iconDiffThreshold = 40,
      .centerMaskRadius = 8,
      .gradientBaseWeight = 0.1,
      .minimapDarkMaskThreshold = 15,
      .borderMargin = 8,
      .whiteDilate = 9,
      .colorDilate = 3,
      .useHsvWhiteMask = false
  };
};

bool MapLocator::Impl::initialize(const MapLocatorConfig& cfg) {
    if (isInitialized) return true;
    config = cfg;
    
    motionTracker = std::make_unique<MotionTracker>(trackingCfg);
    loadAvailableZones(config.mapResourceDir);

    if (!config.yoloModelPath.empty()) {
        zoneClassifier = std::make_unique<YoloPredictor>(config.yoloModelPath, matchCfg.yoloConfThreshold);
    }
    
    isInitialized = true;
    return true;
}

void MapLocator::Impl::loadAvailableZones(const std::string &root) {
  if (!fs::exists(MAA_NS::path(root))) return;

  boost::regex layerFileRegex(R"(Lv(\d+)Tier(\d+)\.(png|jpg|webp)$)", boost::regex::icase);

  for (const auto &entry : fs::recursive_directory_iterator(MAA_NS::path(root))) {
    if (entry.is_directory()) continue;
    std::string filename = MAA_NS::path_to_utf8_string(entry.path().filename());
    std::string parentName = MAA_NS::path_to_utf8_string(entry.path().parent_path().filename());

    std::string key;
    std::string filenameLower = filename;
    std::transform(filenameLower.begin(), filenameLower.end(), filenameLower.begin(), ::tolower);

    if (filenameLower == "base.png") {
      key = parentName + "_Base";
    } else {
      boost::smatch matches;
      if (boost::regex_search(filename, matches, layerFileRegex)) {
        std::string lv = matches[1].str();
        std::string tier = matches[2].str();
        lv.erase(0, std::min(lv.find_first_not_of('0'), lv.size() - 1));
        tier.erase(0, std::min(tier.find_first_not_of('0'), tier.size() - 1));
        key = parentName + "_L" + lv + "_" + tier;
      } else {
        key = MAA_NS::path_to_utf8_string(entry.path().stem());
      }
    }

    cv::Mat img = MAA_NS::imread(entry.path(), cv::IMREAD_UNCHANGED);
    if (img.empty()) {
        LogError << "Failed to load map: " << MAA_NS::path_to_utf8_string(entry.path());
        continue;
    }
    if (img.channels() == 3) cv::cvtColor(img, img, cv::COLOR_BGR2BGRA);
    zones[key] = std::move(img);
    LogInfo << "Loaded Map: " << key;
  }
}

std::optional<MapPosition> MapLocator::Impl::tryTracking(
    const MatchFeature &tmplFeat,
    IMatchStrategy* strategy,
    std::chrono::steady_clock::time_point now,
    const LocateOptions& options,
    MapPosition* outRawPos) {

  if (!strategy) return std::nullopt;

  int maxAllowedLost = (currentZoneId.find("OMVBase") != std::string::npos) ? 10 : options.max_lost_frames;
  if (currentZoneId.empty() || !motionTracker->isTracking(maxAllowedLost))
    return std::nullopt;

  auto it = zones.find(currentZoneId);
  if (it == zones.end()) return std::nullopt;

  const cv::Mat &zoneMap = it->second;

  std::chrono::duration<double> dt = now - motionTracker->getLastTime();
  
  double trackScale = motionTracker->getLastPos()->scale;
  if (trackScale <= 0.0) trackScale = 1.0;

  cv::Rect searchRect = motionTracker->predictNextSearchRect(trackScale, tmplFeat.image.cols, tmplFeat.image.rows, now);

  cv::Mat searchRoiWithPad(searchRect.size(), zoneMap.type(), cv::Scalar(0, 0, 0, 0));
  cv::Rect mapBounds(0, 0, zoneMap.cols, zoneMap.rows);
  cv::Rect validRoi = searchRect & mapBounds;
  if (!validRoi.empty()) {
      zoneMap(validRoi).copyTo(searchRoiWithPad(cv::Rect(
          validRoi.x - searchRect.x, validRoi.y - searchRect.y,
          validRoi.width, validRoi.height)));
  }

  auto searchFeature = strategy->extractSearchFeature(searchRoiWithPad);

  cv::Mat scaledTempl, scaledWeightMask;
  if (std::abs(trackScale - 1.0) > 0.001) {
      cv::resize(tmplFeat.image, scaledTempl, cv::Size(), trackScale, trackScale, cv::INTER_LINEAR);
      cv::resize(tmplFeat.mask, scaledWeightMask, cv::Size(), trackScale, trackScale, cv::INTER_NEAREST);
  } else {
      scaledTempl = tmplFeat.image;
      scaledWeightMask = tmplFeat.mask;
  }

  auto trackResult = CoreMatch(searchFeature.image, scaledTempl, scaledWeightMask, matchCfg.blurSize);
  if (!trackResult) {
      LogInfo << "tryTracking: CoreMatch returned nullopt.";
      return std::nullopt;
  }

  LogInfo << "tryTracking NCC=" << trackResult->score
            << " PSR=" << trackResult->psr
            << " delta=" << trackResult->delta
            << " second=" << trackResult->secondScore
            << " scale=" << trackScale;

  auto validation = strategy->validateTracking(*trackResult, dt, motionTracker->getLastPos(), searchRect, scaledTempl.cols, scaledTempl.rows);

  if (outRawPos) {
      outRawPos->zoneId = currentZoneId;
      outRawPos->x = validation.absX;
      outRawPos->y = validation.absY;
      outRawPos->score = trackResult->score;
      outRawPos->scale = trackScale;
  }

  bool onlyAmbiguous = (!validation.isScreenBlocked && !validation.isEdgeSnapped && !validation.isTeleported);
  
  if (!validation.isValid && strategy->needsChamferCompensation()) {
      cv::Mat templGray, bgrTempl;
      if (std::abs(trackScale - 1.0) > 0.001) {
          cv::resize(tmplFeat.templRaw, bgrTempl, cv::Size(), trackScale, trackScale, cv::INTER_LINEAR);
      } else {
          bgrTempl = tmplFeat.templRaw;
      }
      if (bgrTempl.channels() == 3) cv::cvtColor(bgrTempl, templGray, cv::COLOR_BGR2GRAY);
      else if (bgrTempl.channels() == 4) cv::cvtColor(bgrTempl, templGray, cv::COLOR_BGRA2GRAY);
      else templGray = bgrTempl.clone();
      
      cv::Mat templEdge;
      cv::Canny(templGray, templEdge, 100, 200);
      cv::bitwise_and(templEdge, scaledWeightMask, templEdge);
      
      cv::Rect matchedRect(trackResult->loc.x, trackResult->loc.y, bgrTempl.cols, bgrTempl.rows);
      matchedRect &= cv::Rect(0, 0, searchRoiWithPad.cols, searchRoiWithPad.rows);
      
      cv::Mat patchGray;
      if (searchRoiWithPad.channels() == 3) cv::cvtColor(searchRoiWithPad(matchedRect), patchGray, cv::COLOR_BGR2GRAY);
      else if (searchRoiWithPad.channels() == 4) cv::cvtColor(searchRoiWithPad(matchedRect), patchGray, cv::COLOR_BGRA2GRAY);
      else patchGray = searchRoiWithPad(matchedRect).clone();
      
      cv::Mat patchEdge;
      cv::Canny(patchGray, patchEdge, 100, 200);
      
      cv::Mat distTrans;
      cv::Mat patchEdgeInv;
      cv::bitwise_not(patchEdge, patchEdgeInv);
      cv::distanceTransform(patchEdgeInv, distTrans, cv::DIST_L2, 3);
      
      cv::Scalar meanDistScalar = cv::mean(distTrans, templEdge(cv::Rect(0,0,matchedRect.width, matchedRect.height)));
      double meanDist = meanDistScalar[0];
      
      LogInfo << "Chamfer mean distance: " << meanDist;
      
      if (meanDist < 4.5) { 
          validation.isValid = true;
          validation.isScreenBlocked = false;
          onlyAmbiguous = false;
          trackResult->score = std::max(trackResult->score, 0.43); 
      }
  }

  if (onlyAmbiguous && motionTracker->isTracking(maxAllowedLost) && !validation.isValid) {
      auto hold = *motionTracker->getLastPos();
      hold.score = trackResult->score;
      hold.scale = trackScale;
      motionTracker->hold(hold, now);
      LogInfo << "Tracking ambiguous -> HOLD last pos. NCC=" << trackResult->score
              << " PSR=" << trackResult->psr << " delta=" << trackResult->delta;
      return hold;
  }
  
  if (!validation.isValid) {
      return std::nullopt;
  }

  if (validation.isValid) {
    MapPosition pos;
    pos.zoneId = currentZoneId;
    pos.x = validation.absX;
    pos.y = validation.absY;
    pos.score = trackResult->score;
    pos.scale = trackScale;
    motionTracker->update(pos, now);
    return pos;
  }

  return std::nullopt;
}

std::optional<MapPosition> MapLocator::Impl::evaluateAndAcceptResult(
    const MatchResultRaw &fineRes,
    const cv::Rect &validFineRect,
    const cv::Mat &templ,
    IMatchStrategy* strategy,
    const std::string &targetZoneId) {

  double absLeft = validFineRect.x + fineRes.loc.x;
  double absTop  = validFineRect.y + fineRes.loc.y;

  double finalScore = 0.0;
  if (!strategy->validateGlobalSearch(fineRes, finalScore)) {
      std::string msg = "Global Rejected. Score too low: s=" + std::to_string(fineRes.score) + " d=" + std::to_string(fineRes.delta) + " p=" + std::to_string(fineRes.psr);
      LogInfo << msg;
      return std::nullopt;
  }

  MapPosition pos;
  pos.zoneId = targetZoneId;
  pos.x = absLeft + templ.cols / 2.0;
  pos.y = absTop + templ.rows / 2.0;
  pos.score = finalScore;
  return pos;
}

std::optional<MapPosition> MapLocator::Impl::tryGlobalSearch(
    const MatchFeature &tmplFeat,
    IMatchStrategy* strategy,
    const std::string &targetZoneId,
    MapPosition* outRawPos) {

  if (!strategy || targetZoneId.empty()) {
      LogInfo << "Global Search Aborted: YOLO returned no result.";
      return std::nullopt;
  }

  if (zones.find(targetZoneId) == zones.end()) {
      std::string msg = "Global Search Aborted: YOLO predicted '" + targetZoneId + "', but this map is NOT loaded in 'zones'.";
      LogInfo << msg;
      return std::nullopt;
  }

  const cv::Mat &bigMap = zones.at(targetZoneId);

  double coarseScale = matchCfg.coarseScale;

  cv::Mat smallMap;
  cv::resize(bigMap, smallMap, cv::Size(), coarseScale, coarseScale, cv::INTER_AREA);

  auto coarseSearchFeat = strategy->extractSearchFeature(smallMap);
  cv::Mat mapToUse;
  if (coarseSearchFeat.image.channels() == 3) cv::cvtColor(coarseSearchFeat.image, mapToUse, cv::COLOR_BGR2GRAY);
  else if (coarseSearchFeat.image.channels() == 4) cv::cvtColor(coarseSearchFeat.image, mapToUse, cv::COLOR_BGRA2GRAY);
  else mapToUse = coarseSearchFeat.image.clone();

  if (matchCfg.blurSize > 0 && !strategy->needsChamferCompensation()) {
      cv::GaussianBlur(mapToUse, mapToUse, cv::Size(matchCfg.blurSize, matchCfg.blurSize), 0);
  }

  cv::Mat tmplGrayToUse;
  if (tmplFeat.image.channels() == 3) cv::cvtColor(tmplFeat.image, tmplGrayToUse, cv::COLOR_BGR2GRAY);
  else if (tmplFeat.image.channels() == 4) cv::cvtColor(tmplFeat.image, tmplGrayToUse, cv::COLOR_BGRA2GRAY);
  else tmplGrayToUse = tmplFeat.image.clone();

  struct CoarseCand {
      double s;
      double score;
      cv::Point loc;
  };
  std::vector<CoarseCand> cands;
  int topNPerScale = 3;
  int topK = 8;
  double coarseMin = 0.20;

  for (double s = 0.90; s <= 1.101; s += 0.02) {
      double currentScale = coarseScale * s;
      cv::Mat smallTempl, smallWeightMask;
      cv::resize(tmplGrayToUse, smallTempl, cv::Size(), currentScale, currentScale, cv::INTER_LINEAR);
      cv::resize(tmplFeat.mask, smallWeightMask, cv::Size(), currentScale, currentScale, cv::INTER_NEAREST);

      if (cv::countNonZero(smallWeightMask) < 5) continue;

      cv::Mat smallResult;
      cv::matchTemplate(mapToUse, smallTempl, smallResult, cv::TM_CCOEFF_NORMED, smallWeightMask);
      cv::patchNaNs(smallResult, -1.0f);

      int sr = std::max(4, std::min(smallTempl.cols, smallTempl.rows) / 2);

      for (int i=0; i<topNPerScale; ++i) {
          double mv; cv::Point ml;
          cv::minMaxLoc(smallResult, nullptr, &mv, nullptr, &ml);
          if (!std::isfinite(mv) || mv < coarseMin) break;

          cands.push_back({s, mv, ml});

          cv::Rect sup(ml.x - sr, ml.y - sr, sr*2+1, sr*2+1);
          sup &= cv::Rect(0,0,smallResult.cols, smallResult.rows);
          smallResult(sup).setTo(-2.0f);
      }
  }

  if (cands.empty()) return std::nullopt;

  std::sort(cands.begin(), cands.end(), [](auto& a, auto& b){ return a.score > b.score; });
  if ((int)cands.size() > topK) cands.resize(topK);

  double bestFine = -1.0;
  double bestScale = 1.0;
  MatchResultRaw bestFineRes;
  cv::Rect bestValidFineRect;
  cv::Mat bestScaledTempl, bestScaledMask;

  double fallbackScore = -1.0;
  double fallbackScale = 1.0;
  MatchResultRaw fallbackFineRes;
  cv::Rect fallbackValidFineRect;
  cv::Mat fallbackScaledTempl, fallbackScaledMask;

  int searchRadius = matchCfg.fineSearchRadius;

  for (auto& cand : cands) {
      double s = cand.s;
      int coarseX = static_cast<int>(cand.loc.x / coarseScale);
      int coarseY = static_cast<int>(cand.loc.y / coarseScale);

      cv::Mat scaledTempl, scaledWeightMask;
      if (std::abs(s - 1.0) > 0.001) {
          cv::resize(tmplFeat.image, scaledTempl, cv::Size(), s, s, cv::INTER_LINEAR);
          cv::resize(tmplFeat.mask, scaledWeightMask, cv::Size(), s, s, cv::INTER_NEAREST);
      } else {
          scaledTempl = tmplFeat.image;
          scaledWeightMask = tmplFeat.mask;
      }

      cv::Rect fineRect(coarseX - searchRadius, coarseY - searchRadius,
                        scaledTempl.cols + searchRadius * 2, scaledTempl.rows + searchRadius * 2);
      cv::Rect mapBounds(0, 0, bigMap.cols, bigMap.rows);
      cv::Rect validFineRect = fineRect & mapBounds;

      if (validFineRect.empty()) continue;

      cv::Mat fineMap = bigMap(validFineRect);
      
      auto fineSearchFeat = strategy->extractSearchFeature(fineMap);
      auto fineRes = CoreMatch(fineSearchFeat.image, scaledTempl, scaledWeightMask, matchCfg.blurSize);

      if (!fineRes) continue;
      
      if (fineRes->score > fallbackScore) {
          fallbackScore = fineRes->score;
          fallbackScale = s;
          fallbackFineRes = *fineRes;
          fallbackValidFineRect = validFineRect;
          fallbackScaledTempl = scaledTempl;
          fallbackScaledMask = scaledWeightMask;
      }

      bool ambiguous = false;
      if (strategy->needsChamferCompensation()) { // i.e. PathHeatmap
          ambiguous = (fineRes->psr < 6.0) || (fineRes->delta < 0.04);
          if (fineRes->score < 0.45 && ambiguous) continue;
      } else {
          double lowScoreCut = (targetZoneId.find("Base") != std::string::npos) ? 0.85 : 0.75;
          ambiguous = (fineRes->score < lowScoreCut) && (fineRes->psr < 6.0 || fineRes->delta < 0.02);
          if (ambiguous) continue;
      }

      if (fineRes->score > bestFine) {
          bestFine = fineRes->score;
          bestScale = s;
          bestFineRes = *fineRes;
          bestValidFineRect = validFineRect;
          bestScaledTempl = scaledTempl;
          bestScaledMask = scaledWeightMask;
      }
  }

  if (bestFine < 0) {
      if (fallbackScore < 0) return std::nullopt;
      bestFine = fallbackScore;
      bestScale = fallbackScale;
      bestFineRes = fallbackFineRes;
      bestValidFineRect = fallbackValidFineRect;
      bestScaledTempl = fallbackScaledTempl;
      bestScaledMask = fallbackScaledMask;
      LogInfo << "Global Search: All candidates ambiguous, using fallback (score " << fallbackScore << ")";
  }

  if (outRawPos && bestFine >= 0.0) {
      outRawPos->zoneId = targetZoneId;
      outRawPos->x = bestValidFineRect.x + bestFineRes.loc.x + bestScaledTempl.cols / 2.0;
      outRawPos->y = bestValidFineRect.y + bestFineRes.loc.y + bestScaledTempl.rows / 2.0;
      outRawPos->score = bestFine;
      outRawPos->scale = bestScale;
  }

  auto res = evaluateAndAcceptResult(bestFineRes, bestValidFineRect, bestScaledTempl, strategy, targetZoneId);
  if (res) {
      res->scale = bestScale;
  }
  return res;
}

LocateResult MapLocator::Impl::locate(const cv::Mat &minimap, const LocateOptions& options) {
    auto now = std::chrono::steady_clock::now();
    LocateResult result;
    result.status = LocateStatus::TrackingLost;
    
    if (!isInitialized) {
        result.status = LocateStatus::NotInitialized;
        result.debugMessage = "MapLocator not initialized.";
        return result;
    }

    matchCfg.passThreshold = options.loc_threshold;
    matchCfg.yoloConfThreshold = options.yolo_threshold;
    if (zoneClassifier) {
        zoneClassifier->SetConfThreshold(options.yolo_threshold);
    }

    std::unique_ptr<IMatchStrategy> strategy;

    if (!options.force_global_search) {
        {
            std::lock_guard<std::mutex> lock(taskMutex);
            if (asyncYoloTask.valid() && asyncYoloTask.wait_for(std::chrono::seconds(0)) == std::future_status::ready) {
                std::string predictedZone = asyncYoloTask.get();
                if (!predictedZone.empty() && !currentZoneId.empty() && predictedZone != currentZoneId) {
                    LogInfo << "Async YOLO detected zone change: " << currentZoneId << " -> " << predictedZone;
                    motionTracker->forceLost();
                }
            }
            if (!asyncYoloTask.valid()) {
                auto elapsed = std::chrono::duration_cast<std::chrono::seconds>(now - lastYoloCheckTime).count();
                if (elapsed >= 3 && zoneClassifier && zoneClassifier->isLoaded()) {
                    lastYoloCheckTime = now;
                    cv::Mat yoloInput = minimap.clone();
                    asyncYoloTask = std::async(std::launch::async, [this, yoloInput]() {
                        return zoneClassifier->predictZoneByYOLO(yoloInput);
                    });
                }
            }
        }

        bool isNativePathHeatmap = (!currentZoneId.empty() && currentZoneId.find("OMVBase") != std::string::npos);

        if (!currentZoneId.empty()) {
            strategy = MatchStrategyFactory::create(currentZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg);
        }

        if (strategy) {
            MapPosition rawPrimaryPos{};
            auto trackingTmpl = strategy->extractTemplateFeature(minimap);
            auto trackingResult = tryTracking(trackingTmpl, strategy.get(), now, options, &rawPrimaryPos);

            if (trackingResult) {
                trackingResult->angle = InferYellowArrowRotation(minimap);
                result.position = trackingResult;
                result.status = LocateStatus::Success;
                result.debugMessage = "Tracking Success";
                return result;
            } else if (!isNativePathHeatmap && rawPrimaryPos.score > 0.1) {
                auto fallbackStrategy = MatchStrategyFactory::create(currentZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg, MatchMode::ForcePathHeatmap);
                auto fallbackTmpl = fallbackStrategy->extractTemplateFeature(minimap);
                
                MapPosition rawFallbackPos;
                tryTracking(fallbackTmpl, fallbackStrategy.get(), now, options, &rawFallbackPos);

                double dist = std::hypot(rawPrimaryPos.x - rawFallbackPos.x, rawPrimaryPos.y - rawFallbackPos.y);
                
                if (rawFallbackPos.score > 0.1 && dist <= 2.0) {
                    LogInfo << "Dual-Mode Tracking Verified! Coords matched. Dist: " << dist;
                    MapPosition verifiedPos = rawPrimaryPos;
                    verifiedPos.score = std::max(rawPrimaryPos.score, rawFallbackPos.score);
                    
                    motionTracker->update(verifiedPos, now);
                    verifiedPos.angle = InferYellowArrowRotation(minimap);
                    
                    result.position = verifiedPos;
                    result.status = LocateStatus::Success;
                    result.debugMessage = "Dual-Mode Tracking Success";
                    return result;
                }
            }
        }
    }

    std::string targetZoneId = options.expected_zone;
    if (targetZoneId.empty()) {
        targetZoneId = zoneClassifier ? zoneClassifier->predictZoneByYOLO(minimap) : "";
    }
    
    if (targetZoneId.empty()) {
        result.status = LocateStatus::YoloFailed;
        result.debugMessage = "YOLO inference failed or no result.";
        return result;
    }
    if (targetZoneId == "None") {
        LogInfo << "YOLO explicitly identified 'None', assuming UI occlusion.";

        if (motionTracker->getLastPos()) {
            motionTracker->hold(*motionTracker->getLastPos(), now);
        }

        MapPosition nonePos;
        nonePos.zoneId = "None";
        nonePos.x = 0;
        nonePos.y = 0;
        nonePos.score = 1.0;

        result.status = LocateStatus::Success;
        result.position = nonePos;
        result.debugMessage = "Occluded by UI (None)";

        return result;
    }

    bool isNativePathHeatmap = (targetZoneId.find("OMVBase") != std::string::npos);
    auto nextStrategy = MatchStrategyFactory::create(targetZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg);
    auto globalTmpl = nextStrategy->extractTemplateFeature(minimap);
    
    MapPosition rawGlobalPrimaryPos{};
    auto globalResult = tryGlobalSearch(globalTmpl, nextStrategy.get(), targetZoneId, &rawGlobalPrimaryPos);

    if (!globalResult && !isNativePathHeatmap && rawGlobalPrimaryPos.score > 0.1) {
        auto fallbackStrategy = MatchStrategyFactory::create(targetZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg, MatchMode::ForcePathHeatmap);
        auto fallbackTmpl = fallbackStrategy->extractTemplateFeature(minimap);
        
        MapPosition rawGlobalFallbackPos;
        tryGlobalSearch(fallbackTmpl, fallbackStrategy.get(), targetZoneId, &rawGlobalFallbackPos);
        
        double dist = std::hypot(rawGlobalPrimaryPos.x - rawGlobalFallbackPos.x, rawGlobalPrimaryPos.y - rawGlobalFallbackPos.y);
        if (rawGlobalFallbackPos.score > 0.1 && dist <= 5.0) {
            LogInfo << "Dual-Mode Global Search Verified! Dist: " << dist;
            globalResult = rawGlobalPrimaryPos;
            globalResult->score = std::max(rawGlobalPrimaryPos.score, rawGlobalFallbackPos.score);
        }
    }

    int maxAllowedLost = (targetZoneId.find("OMVBase") != std::string::npos) ? 10 : options.max_lost_frames;
    if (!globalResult) {
      motionTracker->markLost();
      if (motionTracker->getLostCount() > maxAllowedLost) {
          motionTracker->forceLost();
      }
      result.status = LocateStatus::TrackingLost;
      result.debugMessage = "Global search failed.";
      return result;
    }

    if (currentZoneId != globalResult->zoneId) {
      motionTracker->clearVelocity();
    }

    currentZoneId = globalResult->zoneId;
    globalResult->angle = InferYellowArrowRotation(minimap);
    
    motionTracker->update(*globalResult, now);
    
    result.status = LocateStatus::Success;
    result.position = globalResult;
    result.debugMessage = "Global Search Success";
    return result;
}

void MapLocator::Impl::resetTrackingState() {
    if (motionTracker) {
        motionTracker->forceLost();
    }
    currentZoneId = "";
}

std::optional<MapPosition> MapLocator::Impl::getLastKnownPos() const {
    if (motionTracker) return motionTracker->getLastPos();
    return std::nullopt;
}

// ======================================
// MapLocator Public Interface 
// ======================================

MapLocator::MapLocator() : pimpl(std::make_unique<Impl>()) {}
MapLocator::~MapLocator() = default;

bool MapLocator::initialize(const MapLocatorConfig& config) {
    return pimpl->initialize(config);
}

bool MapLocator::isInitialized() const {
    return pimpl->getIsInitialized();
}

LocateResult MapLocator::locate(const cv::Mat &minimap, const LocateOptions& options) {
    auto start = std::chrono::high_resolution_clock::now();
    LocateResult res = pimpl->locate(minimap, options);
    auto end = std::chrono::high_resolution_clock::now();
    if (res.position.has_value()) {
        res.position->latencyMs = std::chrono::duration_cast<std::chrono::milliseconds>(end - start).count();
    }
    return res;
}

void MapLocator::resetTrackingState() {
    pimpl->resetTrackingState();
}

std::optional<MapPosition> MapLocator::getLastKnownPos() const {
    return pimpl->getLastKnownPos();
}

} // namespace maplocator
