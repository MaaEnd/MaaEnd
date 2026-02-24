#include "image_binarization.h"

#include <algorithm>
#include <cstdint>
#include <string>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>

#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

static cv::Mat to_mat(const MaaImageBuffer* buffer)
{
    return cv::Mat(
        MaaImageBufferHeight(buffer),
        MaaImageBufferWidth(buffer),
        MaaImageBufferType(buffer),
        MaaImageBufferGetRawData(buffer));
}

static cv::Scalar parse_hex_color(const std::string& hex)
{
    std::string h = hex;
    if (!h.empty() && h[0] == '#') {
        h = h.substr(1);
    }
    if (h.size() != 6) {
        LogWarn << "Invalid hex color:" << hex;
        return { 0, 0, 0 };
    }

    int r = std::stoi(h.substr(0, 2), nullptr, 16);
    int g = std::stoi(h.substr(2, 2), nullptr, 16);
    int b = std::stoi(h.substr(4, 2), nullptr, 16);
    return { static_cast<double>(b), static_cast<double>(g), static_cast<double>(r) };
}

static void mask_colors_as_background(
    cv::Mat& img,
    const std::vector<std::string>& bg_colors,
    int tolerance,
    const cv::Scalar& bg_fill)
{
    for (const auto& hex : bg_colors) {
        cv::Scalar target = parse_hex_color(hex);
        cv::Scalar lower(
            std::max(0.0, target[0] - tolerance),
            std::max(0.0, target[1] - tolerance),
            std::max(0.0, target[2] - tolerance));
        cv::Scalar upper(
            std::min(255.0, target[0] + tolerance),
            std::min(255.0, target[1] + tolerance),
            std::min(255.0, target[2] + tolerance));

        cv::Mat mask;
        cv::inRange(img, lower, upper, mask);
        img.setTo(bg_fill, mask);
    }
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

// Extracts the best OCR text from the recognition detail and writes it
// directly to the detail buffer as a plain string.
// Go side expects customResult.Detail to be a simple text like "1843",
// because MaaFramework wraps out_detail into the Custom result's detail field.
static void extract_best_text_for_custom(MaaStringBuffer* detail_buf)
{
    auto raw = MaaStringBufferGet(detail_buf);
    if (!raw) {
        return;
    }

    auto parsed = json::parse(raw);
    if (!parsed.has_value()) {
        return;
    }

    const auto& root = parsed.value();
    std::string text;

    // Try "best" first (single object), then first item of "filtered", then "all"
    if (root.contains("best") && root.at("best").is_object()) {
        const auto& best = root.at("best");
        if (best.contains("text") && best.at("text").is_string()) {
            text = best.at("text").as_string();
        }
    }
    if (text.empty() && root.contains("filtered") && root.at("filtered").is_array()) {
        for (const auto& item : root.at("filtered").as_array()) {
            if (item.contains("text") && item.at("text").is_string()) {
                text = item.at("text").as_string();
                break;
            }
        }
    }
    if (text.empty() && root.contains("all") && root.at("all").is_array()) {
        for (const auto& item : root.at("all").as_array()) {
            if (item.contains("text") && item.at("text").is_string()) {
                text = item.at("text").as_string();
                break;
            }
        }
    }

    MaaStringBufferSet(detail_buf, text.c_str());
}

static json::value build_ocr_params(const json::value& params, const MaaRect* roi)
{
    json::value ocr;

    if (params.contains("expected")) {
        ocr["expected"] = params.at("expected");
    }
    if (params.contains("threshold")) {
        ocr["threshold"] = params.at("threshold");
    }
    if (params.contains("order_by")) {
        ocr["order_by"] = params.at("order_by");
    }
    if (params.contains("replace")) {
        ocr["replace"] = params.at("replace");
    }
    if (params.contains("index")) {
        ocr["index"] = params.at("index");
    }
    if (params.contains("only_rec")) {
        ocr["only_rec"] = params.at("only_rec");
    }
    if (params.contains("model")) {
        ocr["model"] = params.at("model");
    }

    if (roi) {
        int x = MaaRectGetX(roi);
        int y = MaaRectGetY(roi);
        int w = MaaRectGetW(roi);
        int h = MaaRectGetH(roi);
        if (w > 0 && h > 0) {
            ocr["roi"] = json::array { x, y, w, h };
        }
    }

    return ocr;
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
