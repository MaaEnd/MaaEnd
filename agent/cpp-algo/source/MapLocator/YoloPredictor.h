#pragma once

#include <memory>
#include <mutex>
#include <onnxruntime/onnxruntime_cxx_api.h>
#include <opencv2/opencv.hpp>
#include <string>
#include <unordered_map>
#include <vector>

#include "MapTypes.h"

namespace maplocator
{

class YoloPredictor
{
public:
    explicit YoloPredictor(const std::string& yoloModelPath, double confThreshold = 0.60, int threads = 1);
    ~YoloPredictor() = default;

    YoloCoarseResult predictCoarseByYOLO(const cv::Mat& minimap);

    bool isLoaded() const { return isYoloLoaded; }

    void SetConfThreshold(double threshold) { yoloConfThreshold = threshold; }

    // Convert a YOLO class name to an internal zone ID.
    std::string convertYoloNameToZoneId(const std::string& yoloName);

private:
    struct TileRegion
    {
        std::string base_class;
        int x = 0;
        int y = 0;
        int w = 0;
        int h = 0;
        int infer_margin = 0;

        MEO_JSONIZATION(MEO_OPT base_class, MEO_OPT x, MEO_OPT y, MEO_OPT w, MEO_OPT h, MEO_OPT infer_margin)
    };

    std::unique_ptr<Ort::Env> ortEnv;
    std::unique_ptr<Ort::Session> ortSession;
    std::vector<std::string> inputNodeNames;
    std::vector<std::string> outputNodeNames;

    bool isYoloLoaded = false;
    std::vector<std::string> yoloClassNames;
    std::unordered_map<std::string, std::string> regionMapping;
    std::unordered_map<std::string, TileRegion> tileRegions;

    std::mutex yoloMutex;
    double yoloConfThreshold;
};

} // namespace maplocator
