#include "MapLocateAction.h"
#include "Logger.h"
#include "MapLocator.h"

#include "MaaFramework/MaaAPI.h"

#include <iostream>
#include <opencv2/opencv.hpp>
#include <vector>

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace maplocator {

MaaBool MAA_CALL MapLocateActionRun(MaaContext *context, MaaTaskId task_id,
                                    const char *node_name,
                                    const char *custom_action_name,
                                    const char *custom_action_param,
                                    MaaRecoId reco_id, const MaaRect *box,
                                    void *trans_arg) {
  auto locator = GetGlobalLocator();
  if (!locator) {
    Logger::Error("MapLocateAction: Global locator not initialized");
    return MAA_FALSE;
  }

  MaaTasker *tasker = MaaContextGetTasker(context);
  if (!tasker) {
    Logger::Error("MapLocateAction: Failed to get tasker");
    return MAA_FALSE;
  }

  MaaController *ctrl = MaaTaskerGetController(tasker);
  if (!ctrl) {
    Logger::Error("MapLocateAction: Failed to get controller");
    return MAA_FALSE;
  }

  MaaImageBuffer *imgBuff = MaaImageBufferCreate();
  if (!MaaControllerCachedImage(ctrl, imgBuff)) {
    Logger::Error("MapLocateAction: Failed to get cached image");
    MaaImageBufferDestroy(imgBuff);
    return MAA_FALSE;
  }

  if (MaaImageBufferIsEmpty(imgBuff)) {
    Logger::Error("MapLocateAction: Image buffer is empty");
    MaaImageBufferDestroy(imgBuff);
    return MAA_FALSE;
  }

  int w = MaaImageBufferWidth(imgBuff);
  int h = MaaImageBufferHeight(imgBuff);
  int channels = MaaImageBufferChannels(imgBuff);
  void *raw = MaaImageBufferGetRawData(imgBuff);

  cv::Mat img;
  if (channels == 3) {
    cv::Mat temp(h, w, CV_8UC3, raw);
    cv::cvtColor(temp, img, cv::COLOR_BGR2BGRA);
  } else if (channels == 4) {
    img = cv::Mat(h, w, CV_8UC4, raw);
  } else {
    Logger::Error("MapLocateAction: Unsupported channels " +
                  std::to_string(channels));
    MaaImageBufferDestroy(imgBuff);
    return MAA_FALSE;
  }

  // (49,51,117,120): Go端实测的小地图ROI坐标, 与分辨率绑定
  cv::Rect roi(MinimapROIOriginX, MinimapROIOriginY, MinimapROIWidth,
               MinimapROIHeight);
  cv::Rect imgBounds(0, 0, w, h);
  roi = roi & imgBounds;

  if (roi.empty()) {
    Logger::Error("MapLocateAction: ROI empty");
    MaaImageBufferDestroy(imgBuff);
    return MAA_FALSE;
  }

  cv::Mat subImg = img(roi);

  auto start = std::chrono::high_resolution_clock::now();
  auto result = locator->locate(subImg);

  // locate内部引用了imgBuff的内存, 必须在locate完成后才能销毁
  MaaImageBufferDestroy(imgBuff);

  auto end = std::chrono::high_resolution_clock::now();
  long long latency =
      std::chrono::duration_cast<std::chrono::milliseconds>(end - start)
          .count();

  if (result) {
    Logger::Info("MapLocateAction: Position Found Zone=" + result->zoneId +
                 " X=" + std::to_string(result->x) +
                 " Y=" + std::to_string(result->y));

    std::string msg = "Located: zone=" + result->zoneId +
                      " x=" + std::to_string(result->x) +
                      " y=" + std::to_string(result->y) +
                      " avgDiff=" + std::to_string(result->avgDiff) +
                      " latency=" + std::to_string(latency) + "ms";

    std::string param = R"({
            "MapShowMessage": {
                "recognition": "DirectHit",
                "action": "DoNothing",
                "focus": {
                    "Node.Action.Starting": ")" +
                        msg + R"("
                }
            }
        })";

    MaaContextRunTask(context, "MapShowMessage", param.c_str());

  } else {
    Logger::Error("MapLocateAction: Position Not Found");
    return MAA_FALSE;
  }

  return MAA_TRUE;
}

} // namespace maplocator
