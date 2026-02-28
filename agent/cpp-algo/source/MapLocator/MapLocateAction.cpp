#include "MapLocateAction.h"
#include <MaaUtils/Logger.h>
#include "MapLocator.h"

#include "MaaFramework/MaaAPI.h"

#include <iostream>
#include <filesystem>
#include <mutex>
#include <MaaUtils/NoWarningCV.hpp>
#include <vector>
#include <unordered_map>

#ifdef _WIN32
#include <Windows.h>
#endif

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace fs = std::filesystem;

namespace maplocator {

static fs::path getExeDir() {
#ifdef _WIN32
  wchar_t buf[4096] = {0};
  GetModuleFileNameW(nullptr, buf, 4096);
  return fs::path(buf).parent_path();
#else
  return fs::read_symlink("/proc/self/exe").parent_path();
#endif
}

static std::mutex g_locatorMutex;
static std::unordered_map<MaaContext*, std::shared_ptr<MapLocator>> g_locators;

static std::shared_ptr<MapLocator> getOrInitLocator(MaaContext* context) {
    std::lock_guard<std::mutex> lock(g_locatorMutex);
    auto it = g_locators.find(context);
    if (it != g_locators.end()) return it->second;

    fs::path exeDir = getExeDir();
    fs::path mapRoot = exeDir / ".." / "resource" / "image" / "Map";
    fs::path yoloModel = exeDir / ".." / "resource" / "model" / "map" / "cls.onnx";

    std::string mapRootStr   = fs::absolute(mapRoot).string();
    std::string yoloModelStr = fs::exists(yoloModel) ? fs::absolute(yoloModel).string() : "";

    LogInfo << "[MapLocator] Auto-init: mapRoot=" << mapRootStr;
    LogInfo << "[MapLocator] Auto-init: yoloModel=" << (yoloModelStr.empty() ? "(not found)" : yoloModelStr);

    MapLocatorConfig cfg;
    cfg.mapResourceDir = mapRootStr;
    cfg.yoloModelPath = yoloModelStr;
    cfg.yoloThreads = 1;

    auto locator = std::make_shared<MapLocator>();
    bool ok = locator->initialize(cfg);
    if (!ok) {
        LogError << "[MapLocator] Initialize failed!";
    }
    
    g_locators[context] = locator;
    return locator;
}

MaaBool MAA_CALL MapLocateRecognitionRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi_param,
    void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail) {
  (void)context;
  (void)task_id;
  (void)node_name;
  (void)custom_recognition_name;
  (void)custom_recognition_param;
  (void)roi_param;
  (void)trans_arg;

  LocateOptions options;
  if (custom_recognition_param && std::strlen(custom_recognition_param) > 0) {
      try {
          auto paramJ = json::parse(custom_recognition_param);
          if (paramJ.has_value()) {
              auto obj = paramJ->as_object();
              if (obj.contains("loc_threshold")) options.minScoreThreshold = obj["loc_threshold"].as_double();
              if (obj.contains("yolo_threshold")) options.yoloConfThreshold = obj["yolo_threshold"].as_double();
              if (obj.contains("force_global_search")) options.forceGlobalSearch = obj["force_global_search"].as_boolean();
              if (obj.contains("expected_zone")) options.expectedZoneId = obj["expected_zone"].as_string();
              if (obj.contains("max_lost_frames")) options.maxLostFrames = obj["max_lost_frames"].as_integer();
          }
      } catch (...) {
          LogWarn << "[MapLocator] Failed to parse custom_recognition_param as json";
      }
  }

  auto locator = getOrInitLocator(context);
  if (!locator) {
    LogError << "MapLocateAction: Locator init failed";
    return MAA_FALSE;
  }

  if (MaaImageBufferIsEmpty(image)) {
    LogError << "MapLocateRecognition: Image buffer is empty";
    return MAA_FALSE;
  }

  int w = MaaImageBufferWidth(image);
  int h = MaaImageBufferHeight(image);
  int channels = MaaImageBufferChannels(image);
  void *raw = MaaImageBufferGetRawData(image);

  cv::Mat img;
  if (channels == 3) {
    cv::Mat temp(h, w, CV_8UC3, raw);
    cv::cvtColor(temp, img, cv::COLOR_BGR2BGRA);
  } else if (channels == 4) {
    img = cv::Mat(h, w, CV_8UC4, raw);
  } else {
    LogError << "MapLocateRecognition: Unsupported channels " << std::to_string(channels);
    return MAA_FALSE;
  }

  cv::Rect roi(MinimapROIOriginX, MinimapROIOriginY, MinimapROIWidth, MinimapROIHeight);
  cv::Rect imgBounds(0, 0, w, h);
  roi = roi & imgBounds;

  if (roi.empty()) {
    LogError << "MapLocateRecognition: ROI empty";
    return MAA_FALSE;
  }

  cv::Mat subImg = img(roi);
  LocateResult result = locator->locate(subImg, options);

  if (out_detail) {
      json::object outObj;
      outObj["status"] = static_cast<int>(result.status);
      outObj["message"] = result.debugMessage;
      
      if (result.position.has_value()) {
          auto& pos = result.position.value();
          outObj["mapName"] = pos.zoneId;
          outObj["x"] = static_cast<int>(pos.x);
          outObj["y"] = static_cast<int>(pos.y);
          outObj["rot"] = pos.angle;
          outObj["locConf"] = pos.score;
          outObj["latencyMs"] = pos.latencyMs;
      }
      
      std::string jsonStr = json::value(outObj).dumps();
      MaaStringBufferSet(out_detail, jsonStr.c_str());
  }

  if (result.status == LocateStatus::Success) {
      if (out_box && result.position.has_value()) {
          *out_box = { (int)result.position->x, (int)result.position->y, 1, 1 };
      }
      std::string succMsg = "[MapLocator] OK zone=" + result.position->zoneId +
                            " x=" + std::to_string(result.position->x) +
                            " y=" + std::to_string(result.position->y) +
                            " angle=" + std::to_string(result.position->angle) +
                            " score=" + std::to_string(result.position->score) +
                            " latencyMs=" + std::to_string(result.position->latencyMs) + "ms";
      LogInfo << succMsg;
      return MAA_TRUE;
  } else if (result.status == LocateStatus::ScreenBlocked) {
      LogWarn << "[MapLocator] Screen Blocked";
      return MAA_FALSE;
  } else {
      LogWarn << "[MapLocator] failed: " << result.debugMessage;
      return MAA_FALSE;
  }
}

} // namespace maplocator
