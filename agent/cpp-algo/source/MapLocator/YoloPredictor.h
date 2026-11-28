#pragma once

#include <memory>
#include <mutex>
#include <onnxruntime/onnxruntime_cxx_api.h>
#include <string>
#include <unordered_map>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

#include "MapTypes.h"

namespace maplocator
{

struct YoloTileRegion
{
    std::string base_class;
    int x = 0;
    int y = 0;
    int w = 0;
    int h = 0;
    int infer_margin = 0;

    MEO_JSONIZATION(MEO_OPT base_class, MEO_OPT x, MEO_OPT y, MEO_OPT w, MEO_OPT h, MEO_OPT infer_margin)
};

struct YoloModel
{
    std::unique_ptr<Ort::Env> env;
    std::unique_ptr<Ort::Session> session;
    std::vector<std::string> input_names;
    std::vector<std::string> output_names;

    bool loaded = false;
    std::vector<std::string> class_names;
    std::unordered_map<std::string, std::string> region_mapping;
    std::unordered_map<std::string, YoloTileRegion> tile_regions;

    std::mutex mutex;
    cv::Mat canvas_scratch;
    cv::Mat mask_scratch;
    cv::Mat processed_scratch;
    cv::Mat rgb_scratch;
    cv::Mat float_scratch;
    std::vector<float> input_tensor_scratch;
    double conf_threshold = 0.60;
};

std::unique_ptr<YoloModel> LoadYoloModel(const std::string& yoloModelPath, double confThreshold, int threads);

YoloCoarseResult PredictCoarseByYolo(YoloModel* model, const cv::Mat& minimap);

// Convert a YOLO class name to an internal zone ID.
std::string YoloNameToZoneId(const YoloModel& model, const std::string& yoloName);

} // namespace maplocator
