#include "image_binarization.h"

#include <cstdint>
#include <string>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>

#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "cv_utils.h"
#include "ocr_utils.h"

static cv::Mat to_mat(const MaaImageBuffer* buffer)
{
    return cv::Mat(
        MaaImageBufferHeight(buffer),
        MaaImageBufferWidth(buffer),
        MaaImageBufferType(buffer),
        MaaImageBufferGetRawData(buffer));
}

static cv::Mat binarize(
    const cv::Mat& src,
    const std::string& mode,
    const std::vector<std::string>& bg_colors,
    int color_tolerance)
{
    cv::Mat working = src.clone();

    bool dark_bg = (mode == "dark_bg");
    cv::Scalar bg_fill = dark_bg ? cv::Scalar(0, 0, 0) : cv::Scalar(255, 255, 255);

    if (!bg_colors.empty()) {
        mask_colors_as_background(working, bg_colors, color_tolerance, bg_fill);
    }

    cv::Mat gray;
    cv::cvtColor(working, gray, cv::COLOR_BGR2GRAY);

    cv::Mat binary;
    if (dark_bg) {
        cv::threshold(gray, binary, 0, 255, cv::THRESH_BINARY | cv::THRESH_OTSU);
    }
    else {
        cv::threshold(gray, binary, 0, 255, cv::THRESH_BINARY_INV | cv::THRESH_OTSU);
    }

    // OCR expects 3-channel image: black text on white background
    cv::Mat inverted;
    cv::bitwise_not(binary, inverted);

    cv::Mat result;
    cv::cvtColor(inverted, result, cv::COLOR_GRAY2BGR);
    return result;
}

MaaBool ImageBinarizationCallback(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi,
    void* trans_arg,
    /* out */ MaaRect* out_box,
    /* out */ MaaStringBuffer* out_detail)
{
    std::ignore = task_id;
    std::ignore = custom_recognition_name;
    std::ignore = trans_arg;

    LogInfo << "ImageBinarization:" << node_name;

    auto params = json::parse(custom_recognition_param).value_or(json::value());

    std::string mode = params.get("mode", std::string("light_bg"));
    int color_tolerance = params.get("color_tolerance", 30);

    std::vector<std::string> bg_colors;
    if (auto arr = params.find("bg_colors"); arr && arr->is_array()) {
        for (const auto& c : arr->as_array()) {
            if (c.is_string()) {
                bg_colors.push_back(c.as_string());
            }
        }
    }

    cv::Mat src = to_mat(image);
    if (src.empty()) {
        LogError << "Empty image";
        return false;
    }

    cv::Mat processed = binarize(src, mode, bg_colors, color_tolerance);

    MaaImageBuffer* processed_buf = MaaImageBufferCreate();
    if (!processed_buf) {
        LogError << "Failed to create image buffer";
        return false;
    }

    MaaImageBufferSetRawData(
        processed_buf,
        processed.data,
        processed.cols,
        processed.rows,
        processed.type());

    json::value ocr_params = build_ocr_params(params, roi);
    std::string ocr_params_str = ocr_params.dumps();

    LogDebug << "OCR params:" << ocr_params_str;

    MaaRecoId reco_id = MaaContextRunRecognitionDirect(
        context,
        "OCR",
        ocr_params_str.c_str(),
        processed_buf);

    MaaImageBufferDestroy(processed_buf);

    if (reco_id == 0) {
        LogError << "RunRecognitionDirect failed";
        return false;
    }

    MaaTasker* tasker = MaaContextGetTasker(context);
    MaaBool hit = false;

    MaaBool got = MaaTaskerGetRecognitionDetail(
        tasker,
        reco_id,
        nullptr,
        nullptr,
        &hit,
        out_box,
        out_detail,
        nullptr,
        nullptr);

    if (!got) {
        LogError << "GetRecognitionDetail failed for reco_id:" << reco_id;
        return false;
    }

    if (hit) {
        extract_best_text_for_custom(out_detail);
    }

    LogInfo << "ImageBinarization result: hit=" << hit;
    return hit;
}
