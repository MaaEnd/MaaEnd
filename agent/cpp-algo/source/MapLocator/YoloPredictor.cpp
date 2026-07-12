#include <algorithm>
#include <filesystem>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <boost/regex.hpp>
#include <meojson/json.hpp>

#include "YoloPredictor.h"

using Json = json::value;
namespace fs = std::filesystem;

namespace maplocator
{

YoloPredictor::YoloPredictor(const std::string& yoloModelPath, double confThreshold, int threads)
    : yoloConfThreshold(confThreshold)
{
    if (!yoloModelPath.empty()) {
        ortEnv = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "MapLocatorYolo");
        Ort::SessionOptions sessionOptions;
        sessionOptions.SetIntraOpNumThreads(std::max(1, threads));
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

            if (j.contains("input_name")) {
                inputNodeNames.push_back(j["input_name"].as<std::string>());
            }
            if (j.contains("output_name")) {
                outputNodeNames.push_back(j["output_name"].as<std::string>());
            }

            if (j.contains("classes")) {
                yoloClassNames = j["classes"].as<std::vector<std::string>>();
            }
            if (j.contains("region_mapping")) {
                for (auto& [key, val] : j["region_mapping"].as_object()) {
                    regionMapping[key] = val.as<std::string>();
                }
            }
            LogInfo << "Loaded config from: " << jsonPath;
        }
        else {
            LogWarn << "Config file not found or invalid json: " << jsonPath;
        }

        fs::path tileMappingPath = modelPath;
        tileMappingPath.replace_filename(MAA_NS::path("tile_mapping.json"));
        auto tileMappingOpt = json::open(tileMappingPath);
        if (tileMappingOpt) {
            const Json& tileMapping = *tileMappingOpt;
            for (auto& [key, val] : tileMapping.as_object()) {
                tileRegions.emplace(key, val.as<TileRegion>());
            }
            const std::string tileMappingPathUtf8 = MAA_NS::path_to_utf8_string(tileMappingPath);
            LogInfo << "Loaded tile mapping" << VAR(tileMappingPathUtf8) << VAR(tileRegions.size());
        }
        else {
            const std::string tileMappingPathUtf8 = MAA_NS::path_to_utf8_string(tileMappingPath);
            LogWarn << "Tile mapping file not found or invalid json" << VAR(tileMappingPathUtf8);
        }

        LogInfo << "YOLO Model loaded successfully.";
    }
}

std::string YoloPredictor::convertYoloNameToZoneId(const std::string& yoloName)
{
    const std::string prefix = yoloName.length() >= 5 ? yoloName.substr(0, 5) : yoloName;

    auto it = regionMapping.find(prefix);
    if (it != regionMapping.end()) {
        const std::string& regionName = it->second;
        if (yoloName.find("Base") != std::string::npos && yoloName.find("Map") != std::string::npos) {
            return regionName + "_Base";
        }
        static const boost::regex kTierRegex(R"((Map\d+)Lv0*(\d+)Tier0*(\d+))");
        boost::smatch match;
        if (boost::regex_search(yoloName, match, kTierRegex)) {
            return regionName + "_L" + match[2].str() + "_" + match[3].str();
        }
    }

    return yoloName;
}

YoloCoarseResult YoloPredictor::predictCoarseByYOLO(const cv::Mat& minimap)
{
    std::lock_guard<std::mutex> lock(yoloMutex);
    YoloCoarseResult result;

    if (!isYoloLoaded || !ortSession) {
        LogError << "YOLO Error: Model is NOT loaded.";
        return result;
    }
    if (minimap.empty()) {
        LogError << "YOLO Error: Input minimap is empty.";
        return result;
    }

    const int OUTPUT_SIZE = 128;
    // 限定有效裁剪直径 106：游戏小地图 UI 外围存在固定装饰元素或黑边，只取中心干净视野避免黑边干扰 YOLO 分类
    const int MASK_DIAMETER = 106; // 小地图有效区域直径

    cv::Mat img3C;
    if (minimap.channels() == 4) {
        cv::cvtColor(minimap, img3C, cv::COLOR_BGRA2BGR);
    }
    else {
        img3C = minimap.clone();
    }

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
                inputTensorValues[c * OUTPUT_SIZE * OUTPUT_SIZE + i * OUTPUT_SIZE + j] = floatImg.at<cv::Vec3f>(i, j)[c];
            }
        }
    }

    std::vector<int64_t> inputShape = { 1, 3, OUTPUT_SIZE, OUTPUT_SIZE };

    auto memoryInfo = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
    Ort::Value inputTensor = Ort::Value::CreateTensor<float>(
        memoryInfo,
        inputTensorValues.data(),
        inputTensorValues.size(),
        inputShape.data(),
        inputShape.size());

    if (inputNodeNames.empty() || outputNodeNames.empty()) {
        LogError << "YOLO Error: input/output node names are not configured. Check model JSON sidecar.";
        return result;
    }

    // Run Inference
    const char* inName = inputNodeNames[0].c_str();
    const char* outName = outputNodeNames[0].c_str();
    auto outputTensors = ortSession->Run(Ort::RunOptions { nullptr }, &inName, &inputTensor, 1, &outName, 1);

    if (outputTensors.empty()) {
        return result;
    }

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
    // 保护性越界判断：即便模型输出的 maxIdx 超出实际 classes 的范围，也能优雅 fallback
    if (maxIdx >= 0 && static_cast<size_t>(maxIdx) < yoloClassNames.size()) {
        predictedName = yoloClassNames[maxIdx];
    }

    LogInfo << "YOLO Raw All:" << yoloClassNames << std::vector<float>(outputData, outputData + outputCount);
    LogInfo << "YOLO Raw:" << VAR(predictedName) << VAR(maxIdx) << VAR(maxConf);
    result.raw_class = predictedName;
    result.confidence = maxConf;

    if (predictedName == "None") {
        LogInfo << "YOLO Predicted 'None', skipping localization.";
        result.valid = true;
        result.is_none = true;
        result.zone_id = "None";
        return result;
    }

    if (maxIdx >= 0 && static_cast<size_t>(maxIdx) < yoloClassNames.size()) {
        result.valid = true;
        result.zone_id = convertYoloNameToZoneId(predictedName);

        auto tileIt = tileRegions.find(predictedName);
        if (tileIt != tileRegions.end()) {
            result.base_class = tileIt->second.base_class;
            result.has_roi = true;
            result.roi_x = tileIt->second.x;
            result.roi_y = tileIt->second.y;
            result.roi_w = tileIt->second.w;
            result.roi_h = tileIt->second.h;
            result.infer_margin = tileIt->second.infer_margin;
        }

        LogInfo << "YOLO Success" << VAR(predictedName) << VAR(result.zone_id) << VAR(maxConf) << VAR(result.has_roi);
        if (result.has_roi) {
            LogInfo << "YOLO ROI"
                    << VAR(result.roi_x)
                    << VAR(result.roi_y)
                    << VAR(result.roi_w)
                    << VAR(result.roi_h)
                    << VAR(result.infer_margin);
        }
        return result;
    }

    LogInfo << "YOLO Fail: Index Out of Bounds" << VAR(maxIdx) << VAR(yoloClassNames.size());

    return result;
}

} // namespace maplocator
