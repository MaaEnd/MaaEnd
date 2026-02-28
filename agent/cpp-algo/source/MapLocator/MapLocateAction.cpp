#include "MapLocateAction.h"
#include <MaaUtils/Logger.h>
#include "MapLocator.h"

#include "MaaFramework/MaaAPI.h"

#include <filesystem>
#include <MaaUtils/Platform.h>
#include <MaaUtils/NoWarningCV.hpp>
#include "../utils.h"
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

static std::shared_ptr<MapLocator> getOrInitLocator() {
    static std::shared_ptr<MapLocator> locator = nullptr;
    if (locator) return locator;

    fs::path exeDir = getExeDir();
    fs::path mapRoot = exeDir / ".." / "resource" / "image" / "Map";
    fs::path yoloModel = exeDir / ".." / "resource" / "model" / "map" / "cls.onnx";

    std::string mapRootStr   = MAA_NS::path_to_utf8_string(fs::absolute(mapRoot));
    std::string yoloModelStr = fs::exists(yoloModel) ? MAA_NS::path_to_utf8_string(fs::absolute(yoloModel)) : "";

    LogInfo << "Auto-init: mapRoot=" << mapRootStr;
    LogInfo << "Auto-init: yoloModel=" << (yoloModelStr.empty() ? "(not found)" : yoloModelStr);

    MapLocatorConfig cfg;
    cfg.mapResourceDir = mapRootStr;
    cfg.yoloModelPath = yoloModelStr;
    cfg.yoloThreads = 1;

    locator = std::make_shared<MapLocator>();
    bool ok = locator->initialize(cfg);
    if (!ok) {
        LogError << "Initialize failed!";
    }
    
    return locator;
}

MaaBool MAA_CALL MapLocateRecognitionRun(
    [[maybe_unused]] MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi_param,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail) {

  LocateOptions options;
  if (custom_recognition_param && std::strlen(custom_recognition_param) > 0) {
      options = json::parse(custom_recognition_param)
          .value_or(json::object{})
          .as<LocateOptions>();
  }

  auto locator = getOrInitLocator();
  if (!locator) {
    LogError << "MapLocateAction: Locator init failed";
    return MAA_FALSE;
  }

  if (MaaImageBufferIsEmpty(image)) {
    LogError << "MapLocateRecognition: Image buffer is empty";
    return MAA_FALSE;
  }

  cv::Mat img = to_mat(image);

  cv::Rect roi(MinimapROIOriginX, MinimapROIOriginY, MinimapROIWidth, MinimapROIHeight);
  cv::Rect imgBounds(0, 0, img.cols, img.rows);
  roi = roi & imgBounds;

  if (roi.empty()) {
    LogError << "MapLocateRecognition: ROI empty";
    return MAA_FALSE;
  }

  cv::Mat subImg = img(roi);
  LocateResult result = locator->locate(subImg, options);

  if (out_detail) {
      struct LocateOutput {
          int status = 0;
          std::string message;
          std::optional<std::string> mapName;
          std::optional<int> x;
          std::optional<int> y;
          std::optional<double> rot;
          std::optional<double> locConf;
          std::optional<int> latencyMs;

          MEO_JSONIZATION(status, message, mapName, x, y, rot, locConf, latencyMs)
      };

      LocateOutput out;
      out.status = static_cast<int>(result.status);
      out.message = result.debugMessage;
      
      if (result.position.has_value()) {
          auto& pos = result.position.value();
          out.mapName = pos.zoneId;
          out.x = static_cast<int>(pos.x);
          out.y = static_cast<int>(pos.y);
          out.rot = pos.angle;
          out.locConf = pos.score;
          out.latencyMs = static_cast<int>(pos.latencyMs);
      }
      
      std::string jsonStr = json::value(out).dumps();
      MaaStringBufferSet(out_detail, jsonStr.c_str());
  }

  if (result.status == LocateStatus::Success) {
      if (out_box && result.position.has_value()) {
          *out_box = { (int)result.position->x, (int)result.position->y, 1, 1 };
      }
      LogInfo << "OK " 
              << VAR(result.position->zoneId)
              << VAR(result.position->x)
              << VAR(result.position->y)
              << VAR(result.position->angle)
              << VAR(result.position->score)
              << VAR(result.position->latencyMs);
      return MAA_TRUE;
  } else if (result.status == LocateStatus::ScreenBlocked) {
      LogWarn << "Screen Blocked";
      return MAA_FALSE;
  } else {
      LogWarn << "failed: " << result.debugMessage;
      return MAA_FALSE;
  }
}

} // namespace maplocator
