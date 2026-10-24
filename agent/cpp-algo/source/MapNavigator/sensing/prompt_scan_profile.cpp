#include "prompt_scan_profile.h"

#include <algorithm>
#include <filesystem>
#include <optional>
#include <string_view>
#include <utility>
#include <vector>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>
#include <opencv2/imgproc.hpp>

#include "../control/controller_type_utils.h"
#include "../navi_config.h"
#include "../navi_log.h"
#include "pipeline_node_reader.h"

namespace mapnavigator
{

namespace
{

bool ReadScanThresholds(
    const json::object& node,
    const std::string& scan_node,
    const std::vector<std::string>& relative_paths,
    std::vector<double>* out)
{
    std::vector<double> thresholds { kPromptIconMatchThreshold };
    const json::value* threshold_value = FindRecognitionParam(node, "threshold");
    if (threshold_value != nullptr && !ReadNumberList(*threshold_value, &thresholds)) {
        LogError << "Prompt scan threshold must be numbers." << VAR(scan_node);
        return false;
    }
    if (thresholds.size() != 1 && thresholds.size() != relative_paths.size()) {
        LogError << "Prompt scan node needs one threshold, or one per template." << VAR(scan_node) << VAR(relative_paths.size())
                 << VAR(thresholds.size());
        return false;
    }
    if (std::ranges::any_of(thresholds, [](double threshold) { return threshold <= 0.0 || threshold > 1.0; })) {
        LogError << "Prompt scan threshold must sit in (0, 1]." << VAR(scan_node) << VAR(thresholds);
        return false;
    }
    *out = std::move(thresholds);
    return true;
}

bool LoadScanIcon(
    const std::string& scan_node,
    const std::string& relative_path,
    const std::filesystem::path& resolved,
    double threshold,
    bool green_mask,
    const cv::Rect& base_roi,
    ScanIcon* out)
{
    // 纯绿像素不参与匹配, 与 pipeline 的 TemplateMatch 一致
    ScanIcon icon { .threshold = threshold };
    if (green_mask) {
        const cv::Mat color = MAA_NS::imread(resolved, cv::IMREAD_COLOR);
        if (!color.empty()) {
            cv::inRange(color, cv::Scalar(0, 255, 0), cv::Scalar(0, 255, 0), icon.mask);
            icon.mask = ~icon.mask;
            if (cv::countNonZero(icon.mask) == icon.mask.rows * icon.mask.cols) {
                icon.mask.release();
            }
            cv::cvtColor(color, icon.templ, cv::COLOR_BGR2GRAY);
        }
    }
    else {
        icon.templ = MAA_NS::imread(resolved, cv::IMREAD_GRAYSCALE);
    }
    if (icon.templ.empty()) {
        LogError << "Prompt scan template failed to decode." << VAR(scan_node)
                 << VAR_AS(MAA_NS::path_to_utf8_string(*resolved), MAA_NS::path_to_utf8_string(resolved));
        return false;
    }
    // cv::matchTemplate 的硬前提, 漏到后台线程会每帧触发
    if (icon.templ.cols > base_roi.width || icon.templ.rows > base_roi.height) {
        LogError << "Prompt scan template is larger than its roi." << VAR(scan_node) << VAR(relative_path) << VAR(icon.templ.cols)
                 << VAR(icon.templ.rows) << VAR(base_roi.width) << VAR(base_roi.height);
        return false;
    }
    *out = std::move(icon);
    return true;
}

} // namespace

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
    // 只收基础帧内的绝对 ROI, 预筛每帧独立跑, 相对偏移没有参照物
    if (base_roi.x < 0 || base_roi.y < 0 || base_roi.x + base_roi.width > kPipelineRoiBaseWidth
        || base_roi.y + base_roi.height > kPipelineRoiBaseHeight) {
        LogError << "Prompt scan roi must be absolute and inside the authored base frame." << VAR(scan_node) << VAR(base_roi.x)
                 << VAR(base_roi.y) << VAR(base_roi.width) << VAR(base_roi.height);
        return false;
    }

    std::vector<std::string> relative_paths;
    const json::value* template_value = FindRecognitionParam(*node, "template");
    if (template_value == nullptr || !ReadStringList(*template_value, &relative_paths)) {
        LogError << "Prompt scan node needs at least one template path." << VAR(scan_node);
        return false;
    }

    std::vector<double> thresholds;
    if (!ReadScanThresholds(*node, scan_node, relative_paths, &thresholds)) {
        return false;
    }

    bool green_mask = false;
    const json::value* green_mask_value = FindRecognitionParam(*node, "green_mask");
    if (green_mask_value != nullptr && !ReadBool(*green_mask_value, &green_mask)) {
        LogError << "Prompt scan green_mask must be a boolean." << VAR(scan_node);
        return false;
    }

    const std::vector<std::filesystem::path> roots = ResourceImageRoots(controller_type);
    std::vector<ScanIcon> icons;
    std::vector<std::string> resolved_paths;
    for (size_t i = 0; i < relative_paths.size(); ++i) {
        const std::string& relative_path = relative_paths[i];
        const std::optional<std::filesystem::path> resolved = ResolveResourceImage(roots, relative_path);
        if (!resolved) {
            LogError << "Prompt scan template not found under any loaded resource tree." << VAR(scan_node) << VAR(relative_path)
                     << VAR(DescribeRoots(roots));
            return false;
        }
        ScanIcon icon;
        const double threshold = thresholds.size() == 1 ? thresholds.front() : thresholds[i];
        if (!LoadScanIcon(scan_node, relative_path, *resolved, threshold, green_mask, base_roi, &icon)) {
            return false;
        }
        resolved_paths.push_back(MAA_NS::path_to_utf8_string(*resolved));
        icons.push_back(std::move(icon));
    }

    out->scan_node = scan_node;
    out->base_roi = base_roi;
    out->icons = std::move(icons);
    LogInfo << "Prompt scan profile loaded from the pipeline." << VAR(scan_node) << VAR(resolved_paths) << VAR(thresholds)
            << VAR(green_mask) << VAR(base_roi.x) << VAR(base_roi.y) << VAR(base_roi.width) << VAR(base_roi.height);
    return true;
}

} // namespace mapnavigator
