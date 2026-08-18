#include "prompt_scan_profile.h"

#include <algorithm>
#include <cmath>
#include <filesystem>
#include <optional>
#include <string_view>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>
#include <opencv2/imgproc.hpp>

#include "controller_type_utils.h"
#include "navi_config.h"

#include "../utils.h"

namespace mapnavigator
{

namespace
{

// purpose == nullptr 时一声不出: 存在性探测本来就允许没有这个节点。
std::optional<json::object> ReadNodeObject(MaaContext* context, const std::string& node_name, const char* purpose)
{
    if (context == nullptr || node_name.empty()) {
        return std::nullopt;
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, node_name.c_str(), buffer.Get())) {
        if (purpose != nullptr) {
            LogWarn << purpose << ": MaaContextGetNodeData failed." << VAR(node_name);
        }
        return std::nullopt;
    }
    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || raw[0] == '\0') {
        if (purpose != nullptr) {
            LogWarn << purpose << ": empty node data." << VAR(node_name);
        }
        return std::nullopt;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        if (purpose != nullptr) {
            LogWarn << purpose << ": node JSON is not an object." << VAR(node_name);
        }
        return std::nullopt;
    }
    return parsed->as_object();
}

// 框架回吐的节点恒为 { "recognition": { "type": ..., "param": { ... } } }, 不接受别的形状。
const json::object* RecognitionParam(const json::object& node)
{
    if (!node.contains("recognition") || !node.at("recognition").is_object()) {
        return nullptr;
    }
    const auto& reco = node.at("recognition").as_object();
    if (!reco.contains("param") || !reco.at("param").is_object()) {
        return nullptr;
    }
    return &reco.at("param").as_object();
}

const json::value* FindRecognitionParam(const json::object& node, const char* key)
{
    const json::object* param = RecognitionParam(node);
    return param != nullptr && param->contains(key) ? &param->at(key) : nullptr;
}

std::string ReadRecognitionType(const json::object& node)
{
    if (!node.contains("recognition") || !node.at("recognition").is_object()) {
        return {};
    }
    const auto& reco = node.at("recognition").as_object();
    return reco.contains("type") && reco.at("type").is_string() ? reco.at("type").as_string() : std::string {};
}

bool ReadRoiArray(const json::value& holder, cv::Rect* out)
{
    if (!holder.is_array()) {
        return false;
    }
    const auto& arr = holder.as_array();
    if (arr.size() < 4) {
        return false;
    }
    for (size_t i = 0; i < 4; ++i) {
        if (!arr.at(i).is_number()) {
            return false;
        }
    }
    out->x = static_cast<int>(std::lround(arr.at(0).as_double()));
    out->y = static_cast<int>(std::lround(arr.at(1).as_double()));
    out->width = static_cast<int>(std::lround(arr.at(2).as_double()));
    out->height = static_cast<int>(std::lround(arr.at(3).as_double()));
    return out->width > 0 && out->height > 0;
}

// 作者可以写单值, 但框架回吐时 template 与 threshold 恒为数组。预筛只跑一张图, 多给的当写错处理。
bool ReadSingleString(const json::value& holder, std::string* out)
{
    if (!holder.is_array() || holder.as_array().size() != 1 || !holder.as_array().at(0).is_string()) {
        return false;
    }
    *out = holder.as_array().at(0).as_string();
    return true;
}

bool ReadBool(const json::value& holder, bool* out)
{
    if (!holder.is_boolean()) {
        return false;
    }
    *out = holder.as_boolean();
    return true;
}

bool ReadSingleNumber(const json::value& holder, double* out)
{
    if (!holder.is_array() || holder.as_array().size() != 1 || !holder.as_array().at(0).is_number()) {
        return false;
    }
    *out = holder.as_array().at(0).as_double();
    return true;
}

} // namespace

bool TryReadNodeRoi(MaaContext* context, const std::string& node_name, cv::Rect* out)
{
    const auto node = ReadNodeObject(context, node_name, "Pipeline ROI");
    if (!node) {
        return false;
    }

    const json::value* roi = FindRecognitionParam(*node, "roi");
    if (roi == nullptr || !ReadRoiArray(*roi, out)) {
        LogWarn << "Pipeline ROI: no usable roi array in node data." << VAR(node_name);
        return false;
    }
    return true;
}

bool TryReadNodeInteractTexts(MaaContext* context, const std::string& node_name, std::vector<std::string>* out)
{
    const auto node = ReadNodeObject(context, node_name, "Interact text node");
    if (!node) {
        return false;
    }

    // 停车后那次确认走的是 OCR; 指到别的识别器上是作者指错了节点, 而不是这里该去猜。
    const std::string type = ReadRecognitionType(*node);
    if (!EqualsIgnoreCase(type, "OCR")) {
        LogError << "Interact text node must recognize by OCR." << VAR(node_name) << VAR(type);
        return false;
    }

    const json::value* expected = FindRecognitionParam(*node, "expected");
    if (expected == nullptr || !expected->is<std::vector<std::string>>()) {
        LogError << "Interact text node has no usable expected list." << VAR(node_name);
        return false;
    }

    std::vector<std::string> texts = expected->as<std::vector<std::string>>();
    if (texts.empty() || std::any_of(texts.begin(), texts.end(), [](const std::string& text) { return text.empty(); })) {
        LogError << "Interact text node expected list is empty or holds an empty entry." << VAR(node_name);
        return false;
    }

    *out = std::move(texts);
    return true;
}

bool TryLoadPromptScanProfile(MaaContext* context, const std::string& scan_node, std::string_view controller_type, PromptScanProfile* out)
{
    const auto node = ReadNodeObject(context, scan_node, "Prompt scan node");
    if (!node) {
        return false;
    }

    const std::string type = ReadRecognitionType(*node);
    if (!EqualsIgnoreCase(type, "TemplateMatch")) {
        LogError << "Prompt scan node must recognize by TemplateMatch." << VAR(scan_node) << VAR(type);
        return false;
    }

    cv::Rect base_roi;
    const json::value* roi_value = FindRecognitionParam(*node, "roi");
    if (roi_value == nullptr || !ReadRoiArray(*roi_value, &base_roi)) {
        LogError << "Prompt scan node has no usable roi array." << VAR(scan_node);
        return false;
    }
    // 相对上一个节点的偏移 ROI 在这里没有参照物: 预筛每帧独立跑, 只收基础帧里的绝对坐标。
    if (base_roi.x < 0 || base_roi.y < 0 || base_roi.x + base_roi.width > kPipelineRoiBaseWidth
        || base_roi.y + base_roi.height > kPipelineRoiBaseHeight) {
        LogError << "Prompt scan roi must be absolute and inside the authored base frame." << VAR(scan_node) << VAR(base_roi.x)
                 << VAR(base_roi.y) << VAR(base_roi.width) << VAR(base_roi.height);
        return false;
    }

    std::string relative_path;
    const json::value* template_value = FindRecognitionParam(*node, "template");
    if (template_value == nullptr || !ReadSingleString(*template_value, &relative_path) || relative_path.empty()) {
        LogError << "Prompt scan node needs exactly one template path." << VAR(scan_node);
        return false;
    }

    double threshold = kPromptIconMatchThreshold;
    const json::value* threshold_value = FindRecognitionParam(*node, "threshold");
    if (threshold_value != nullptr && !ReadSingleNumber(*threshold_value, &threshold)) {
        LogError << "Prompt scan node needs exactly one threshold." << VAR(scan_node);
        return false;
    }
    if (threshold <= 0.0 || threshold > 1.0) {
        LogError << "Prompt scan threshold must sit in (0, 1]." << VAR(scan_node) << VAR(threshold);
        return false;
    }

    const std::vector<std::filesystem::path> roots = ResourceImageRoots(controller_type);
    const std::optional<std::filesystem::path> resolved = ResolveResourceImage(roots, relative_path);
    if (!resolved) {
        LogError << "Prompt scan template not found under any loaded resource tree." << VAR(scan_node) << VAR(relative_path)
                 << VAR(DescribeRoots(roots));
        return false;
    }

    bool green_mask = false;
    const json::value* green_mask_value = FindRecognitionParam(*node, "green_mask");
    if (green_mask_value != nullptr && !ReadBool(*green_mask_value, &green_mask)) {
        LogError << "Prompt scan green_mask must be a boolean." << VAR(scan_node);
        return false;
    }

    // 纯绿被排除在匹配之外, 与 pipeline 的 TemplateMatch 同判据; 全不透明时按没有掩码处理。
    cv::Mat mask;
    cv::Mat templ;
    if (green_mask) {
        const cv::Mat color = MAA_NS::imread(*resolved, cv::IMREAD_COLOR);
        if (!color.empty()) {
            cv::inRange(color, cv::Scalar(0, 255, 0), cv::Scalar(0, 255, 0), mask);
            mask = ~mask;
            if (cv::countNonZero(mask) == mask.rows * mask.cols) {
                mask.release();
            }
            cv::cvtColor(color, templ, cv::COLOR_BGR2GRAY);
        }
    }
    else {
        templ = MAA_NS::imread(*resolved, cv::IMREAD_GRAYSCALE);
    }
    if (templ.empty()) {
        LogError << "Prompt scan template failed to decode." << VAR(scan_node) << VAR(MAA_NS::path_to_utf8_string(*resolved));
        return false;
    }
    // cv::matchTemplate 的硬前提。这里不放过, 后台线程每帧都会踩上去; 而 cpp-algo 不留 try/catch。
    if (templ.cols > base_roi.width || templ.rows > base_roi.height) {
        LogError << "Prompt scan template is larger than its roi." << VAR(scan_node) << VAR(templ.cols) << VAR(templ.rows)
                 << VAR(base_roi.width) << VAR(base_roi.height);
        return false;
    }

    out->scan_node = scan_node;
    out->base_roi = base_roi;
    out->templ = templ;
    out->mask = mask;
    out->threshold = threshold;
    LogInfo << "Prompt scan profile loaded from the pipeline." << VAR(scan_node) << VAR(MAA_NS::path_to_utf8_string(*resolved))
            << VAR(threshold) << VAR(green_mask) << VAR(base_roi.x) << VAR(base_roi.y) << VAR(base_roi.width)
            << VAR(base_roi.height);
    return true;
}

} // namespace mapnavigator
