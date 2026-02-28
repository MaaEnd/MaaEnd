#include "YoloPredictor.h"
#include <MaaUtils/Logger.h>
#include <filesystem>
#include <MaaUtils/Platform.h>
#include <fstream>
#include <regex>
#include <atomic>
#include <iomanip>
#include <sstream>
#include <meojson/json.hpp>

#include <iostream>
#include <algorithm>
using Json = json::value;
namespace fs = std::filesystem;

namespace maplocator {

YoloPredictor::YoloPredictor(const std::string& yoloModelPath, double confThreshold)
    : yoloConfThreshold(confThreshold) {
    if (!yoloModelPath.empty()) {
        ortEnv = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "MapLocatorYolo");
        Ort::SessionOptions sessionOptions;
        sessionOptions.SetIntraOpNumThreads(1);
        sessionOptions.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_BASIC);

        auto osModelPath = MAA_NS::to_osstring(yoloModelPath);
        ortSession = std::make_unique<Ort::Session>(*ortEnv, osModelPath.c_str(), sessionOptions);
        isYoloLoaded = true;

        fs::path modelPath = MAA_NS::path(yoloModelPath);
        fs::path jsonPath = modelPath;
        jsonPath.replace_extension(".json");

        auto j_opt = json::open(jsonPath);
        if (j_opt) {
            Json j = *j_opt;

            if (j.contains("input_name"))
                inputNodeNames.push_back(j["input_name"].as<std::string>());
            if (j.contains("output_name"))
                outputNodeNames.push_back(j["output_name"].as<std::string>());

            if (j.contains("classes"))
                yoloClassNames = j["classes"].as<std::vector<std::string>>();
            if (j.contains("region_mapping")) {
                for (auto& [key, val] : j["region_mapping"].as_object()) {
                    regionMapping[key] = val.as<std::string>();
                }
            }
            LogInfo << "Loaded config from: " << jsonPath;
        } else {
            LogWarn << "Config file not found or invalid json: " << jsonPath;
        }

        LogInfo << "YOLO Model loaded successfully.";
    }
}

std::string YoloPredictor::convertYoloNameToZoneId(const std::string &yoloName) {
    std::string prefix = yoloName.length() >= 5 ? yoloName.substr(0, 5) : yoloName;

    auto it = regionMapping.find(prefix);
    if (it != regionMapping.end()) {
        std::string regionName = it->second;
        if (yoloName.find("Base") != std::string::npos && yoloName.find("Map") != std::string::npos) {
            return regionName + "_Base";
        }
        std::regex re(R"((Map\d+)Lv0*(\d+)Tier0*(\d+))");
        std::smatch match;
        if (std::regex_search(yoloName, match, re)) {
            return regionName + "_L" + match[2].str() + "_" + match[3].str();
        }
    }

    return yoloName;
}

std::string YoloPredictor::predictZoneByYOLO(const cv::Mat &minimap) {
    std::lock_guard<std::mutex> lock(yoloMutex);

    if (!isYoloLoaded || !ortSession) {
        LogError << "YOLO Error: Model is NOT loaded.";
        return "";
    }
    if (minimap.empty()) {
        LogError << "YOLO Error: Input minimap is empty.";
        return "";
    }

    const int OUTPUT_SIZE = 128;
    const int MASK_DIAMETER = 106; // 小地图有效区域直径

    cv::Mat img3C;
    if (minimap.channels() == 4) cv::cvtColor(minimap, img3C, cv::COLOR_BGRA2BGR);
    else img3C = minimap.clone();

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

    cv::Mat rgb_img;
    cv::cvtColor(processed_img, rgb_img, cv::COLOR_BGR2RGB);

    // Convert to Float and Normalize [0, 1]
    cv::Mat floatImg;
    rgb_img.convertTo(floatImg, CV_32F, 1.0 / 255.0);

    // Prepare input tensor (NCHW: 1x3x128x128)
    std::vector<float> inputTensorValues(1 * 3 * OUTPUT_SIZE * OUTPUT_SIZE);
    
    // HWC to CHW
    for (int c = 0; c < 3; c++) {
        for (int i = 0; i < OUTPUT_SIZE; i++) {
            for (int j = 0; j < OUTPUT_SIZE; j++) {
                inputTensorValues[c * OUTPUT_SIZE * OUTPUT_SIZE + i * OUTPUT_SIZE + j] = 
                    floatImg.at<cv::Vec3f>(i, j)[c];
            }
        }
    }

    std::vector<int64_t> inputShape = {1, 3, OUTPUT_SIZE, OUTPUT_SIZE};
    
    auto memoryInfo = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
    Ort::Value inputTensor = Ort::Value::CreateTensor<float>(memoryInfo, inputTensorValues.data(), inputTensorValues.size(), inputShape.data(), inputShape.size());

    if (inputNodeNames.empty() || outputNodeNames.empty()) {
        LogError << "YOLO Error: input/output node names are not configured. Check model JSON sidecar.";
        return "";
    }

    // Run Inference
    const char* inName  = inputNodeNames[0].c_str();
    const char* outName = outputNodeNames[0].c_str();
    auto outputTensors = ortSession->Run(Ort::RunOptions{nullptr}, &inName, &inputTensor, 1, &outName, 1);

    if (outputTensors.empty()) return "";
    
    float* outputData = outputTensors.front().GetTensorMutableData<float>();
    size_t outputCount = outputTensors.front().GetTensorTypeAndShapeInfo().GetElementCount();

    // Find max confidence
    int maxIdx = -1;
    float maxConf = -1.0f;
    
    for (size_t i = 0; i < outputCount; i++) {
        if (outputData[i] > maxConf) {
            maxConf = outputData[i];
            maxIdx = (int)i;
        }
    }

    std::string predictedName = "Unknown";
    if (maxIdx >= 0 && maxIdx < (int)yoloClassNames.size())
        predictedName = yoloClassNames[maxIdx];

    LogInfo << "YOLO Raw: Class=" << predictedName <<
                 " (" << std::to_string(maxIdx) << "), Conf=" << std::to_string(maxConf);

    if (predictedName == "None") {
        LogInfo << "YOLO Predicted 'None', skipping localization.";
        return "None";
    }

    if (maxConf > yoloConfThreshold && maxIdx < (int)yoloClassNames.size()) {
        std::string zoneId = convertYoloNameToZoneId(predictedName);
        std::string succMsg = "YOLO Success: " + predictedName + " -> ZoneId: " + zoneId + 
                              " (Conf: " + std::to_string(maxConf * 100.0) + "%)";
        LogInfo << succMsg;
        return zoneId;
    }
    if (maxConf <= yoloConfThreshold) {
        LogInfo << "YOLO Fail: Low Confidence (" << std::to_string(maxConf) <<
                " <= " << std::to_string(yoloConfThreshold) << ")";
    } else {
        LogInfo << "YOLO Fail: Index Out of Bounds (" << std::to_string(maxIdx) <<
                "/" << std::to_string(yoloClassNames.size()) << ")";
    }

    return "";
}

} // namespace maplocator
