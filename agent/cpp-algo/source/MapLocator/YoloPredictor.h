#pragma once

#include <string>
#include <vector>
#include <memory>
#include <unordered_map>
#include <mutex>
#include <opencv2/opencv.hpp>
#include <onnxruntime/onnxruntime_cxx_api.h>

namespace maplocator {

class YoloPredictor {
public:
    explicit YoloPredictor(const std::string& yoloModelPath, double confThreshold = 0.60);
    ~YoloPredictor() = default;

    std::string predictZoneByYOLO(const cv::Mat &minimap);
    bool isLoaded() const { return isYoloLoaded; }
    void SetConfThreshold(double threshold) { yoloConfThreshold = threshold; }
    
    // Convert a YOLO class name to an internal zone ID.
    std::string convertYoloNameToZoneId(const std::string &yoloName);

private:
    std::unique_ptr<Ort::Env> ortEnv;
    std::unique_ptr<Ort::Session> ortSession;
    std::vector<std::string> inputNodeNames;
    std::vector<std::string> outputNodeNames;

    bool isYoloLoaded = false;
    std::vector<std::string> yoloClassNames;
    std::unordered_map<std::string, std::string> regionMapping;
    
    std::mutex yoloMutex;
    double yoloConfThreshold;
};

} // namespace maplocator
