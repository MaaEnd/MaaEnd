#include <algorithm>
#include <array>
#include <filesystem>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <boost/regex.hpp>
#include <meojson/json.hpp>

#include "../Common/JsoncFile.h"
#include "YoloPredictor.h"

using Json = json::value;
namespace fs = std::filesystem;

namespace maplocator
{

std::unique_ptr<YoloModel> LoadYoloModel(const std::string& yoloModelPath, double confThreshold, int threads)
{
    auto model = std::make_unique<YoloModel>();
    model->conf_threshold = confThreshold;

    if (!yoloModelPath.empty()) {
        model->env = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "MapLocatorYolo");
        Ort::SessionOptions sessionOptions;
        sessionOptions.SetIntraOpNumThreads(std::max(1, threads));
        sessionOptions.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_BASIC);

        auto osModelPath = MAA_NS::to_osstring(yoloModelPath);
        model->session = std::make_unique<Ort::Session>(*model->env, osModelPath.c_str(), sessionOptions);
        model->loaded = true;

        fs::path modelPath = MAA_NS::path(yoloModelPath);
        fs::path jsonPath = modelPath;
        jsonPath.replace_extension(".json");

        auto j_opt = common::OpenJsoncFile(jsonPath);
        if (j_opt) {
            Json j = *j_opt;

            if (j.contains("input_name")) {
                model->input_names.push_back(j["input_name"].as<std::string>());
            }
            if (j.contains("output_name")) {
                model->output_names.push_back(j["output_name"].as<std::string>());
            }

            if (j.contains("classes")) {
                model->class_names = j["classes"].as<std::vector<std::string>>();
            }
            if (j.contains("region_mapping")) {
                for (auto& [key, val] : j["region_mapping"].as_object()) {
                    model->region_mapping[key] = val.as<std::string>();
                }
            }
            LogInfo << "Loaded config from: " << jsonPath;
        }
        else {
            LogWarn << "Config file not found or invalid json: " << jsonPath;
        }

        fs::path tileMappingPath = modelPath;
        tileMappingPath.replace_filename(MAA_NS::path("tile_mapping.json"));
        auto tileMappingOpt = common::OpenJsoncFile(tileMappingPath);
        if (tileMappingOpt) {
            const Json& tileMapping = *tileMappingOpt;
            for (auto& [key, val] : tileMapping.as_object()) {
                model->tile_regions.emplace(key, val.as<YoloTileRegion>());
            }
            const std::string tileMappingPathUtf8 = MAA_NS::path_to_utf8_string(tileMappingPath);
            LogInfo << "Loaded tile mapping" << VAR(tileMappingPathUtf8) << VAR(model->tile_regions.size());
        }
        else {
            const std::string tileMappingPathUtf8 = MAA_NS::path_to_utf8_string(tileMappingPath);
            LogWarn << "Tile mapping file not found or invalid json" << VAR(tileMappingPathUtf8);
        }

        LogInfo << "YOLO Model loaded successfully.";
    }

    return model;
}

std::string YoloNameToZoneId(const YoloModel& model, const std::string& yoloName)
{
    const std::string prefix = yoloName.length() >= 5 ? yoloName.substr(0, 5) : yoloName;

    auto it = model.region_mapping.find(prefix);
    if (it != model.region_mapping.end()) {
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

YoloCoarseResult PredictCoarseByYolo(YoloModel* model, const cv::Mat& minimap)
{
    std::lock_guard<std::mutex> lock(model->mutex);
    YoloCoarseResult result;

    if (!model->loaded || !model->session) {
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

    model->canvas_scratch.create(OUTPUT_SIZE, OUTPUT_SIZE, CV_8UC3);
    model->canvas_scratch.setTo(cv::Scalar::all(0));
    int h = img3C.rows, w = img3C.cols;
    int start_y = std::max(0, (OUTPUT_SIZE - h) / 2);
    int start_x = std::max(0, (OUTPUT_SIZE - w) / 2);
    int crop_h = std::min(h, OUTPUT_SIZE);
    int crop_w = std::min(w, OUTPUT_SIZE);

    cv::Rect canvas_roi(start_x, start_y, crop_w, crop_h);
    cv::Rect img_roi((w - crop_w) / 2, (h - crop_h) / 2, crop_w, crop_h);
    img3C(img_roi).copyTo(model->canvas_scratch(canvas_roi));

    if (model->mask_scratch.size() != cv::Size(OUTPUT_SIZE, OUTPUT_SIZE) || model->mask_scratch.type() != CV_8UC1) {
        model->mask_scratch.create(OUTPUT_SIZE, OUTPUT_SIZE, CV_8UC1);
        model->mask_scratch.setTo(cv::Scalar::all(0));
        cv::circle(model->mask_scratch, cv::Point(OUTPUT_SIZE / 2, OUTPUT_SIZE / 2), MASK_DIAMETER / 2, cv::Scalar(255), -1);
    }

    model->processed_scratch.create(OUTPUT_SIZE, OUTPUT_SIZE, CV_8UC3);
    model->processed_scratch.setTo(cv::Scalar::all(0));
    cv::bitwise_and(model->canvas_scratch, model->canvas_scratch, model->processed_scratch, model->mask_scratch);

    cv::cvtColor(model->processed_scratch, model->rgb_scratch, cv::COLOR_BGR2RGB);

    // Convert to Float and Normalize [0, 1]
    model->rgb_scratch.convertTo(model->float_scratch, CV_32F, 1.0 / 255.0);

    // Prepare input tensor (NCHW: 1x3x128x128)
    model->input_tensor_scratch.resize(1 * 3 * OUTPUT_SIZE * OUTPUT_SIZE);

    // HWC to CHW
    for (int c = 0; c < 3; c++) {
        for (int i = 0; i < OUTPUT_SIZE; i++) {
            for (int j = 0; j < OUTPUT_SIZE; j++) {
                model->input_tensor_scratch[c * OUTPUT_SIZE * OUTPUT_SIZE + i * OUTPUT_SIZE + j] =
                    model->float_scratch.at<cv::Vec3f>(i, j)[c];
            }
        }
    }

    constexpr std::array<int64_t, 4> kInputShape { 1, 3, OUTPUT_SIZE, OUTPUT_SIZE };

    auto memoryInfo = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
    Ort::Value inputTensor = Ort::Value::CreateTensor<float>(
        memoryInfo,
        model->input_tensor_scratch.data(),
        model->input_tensor_scratch.size(),
        kInputShape.data(),
        kInputShape.size());

    if (model->input_names.empty() || model->output_names.empty()) {
        LogError << "YOLO Error: input/output node names are not configured. Check model JSON sidecar.";
        return result;
    }

    // Run Inference
    const char* inName = model->input_names[0].c_str();
    const char* outName = model->output_names[0].c_str();
    auto outputTensors = model->session->Run(Ort::RunOptions { nullptr }, &inName, &inputTensor, 1, &outName, 1);

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
    if (maxIdx >= 0 && static_cast<size_t>(maxIdx) < model->class_names.size()) {
        predictedName = model->class_names[maxIdx];
    }

    LogTrace << "YOLO Raw All:" << model->class_names << std::vector<float>(outputData, outputData + outputCount);
    LogDebug << "YOLO Raw:" << VAR(predictedName) << VAR(maxIdx) << VAR(maxConf);
    result.raw_class = predictedName;
    result.confidence = maxConf;

    if (predictedName == "None") {
        LogInfo << "YOLO Predicted 'None', skipping localization.";
        result.valid = true;
        result.is_none = true;
        result.zone_id = "None";
        return result;
    }

    if (maxIdx >= 0 && static_cast<size_t>(maxIdx) < model->class_names.size()) {
        result.valid = true;
        result.zone_id = YoloNameToZoneId(*model, predictedName);

        auto tileIt = model->tile_regions.find(predictedName);
        if (tileIt != model->tile_regions.end()) {
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
            LogInfo << "YOLO ROI" << VAR(result.roi_x) << VAR(result.roi_y) << VAR(result.roi_w) << VAR(result.roi_h)
                    << VAR(result.infer_margin);
        }
        return result;
    }

    LogInfo << "YOLO Fail: Index Out of Bounds" << VAR(maxIdx) << VAR(model->class_names.size());

    return result;
}

} // namespace maplocator
