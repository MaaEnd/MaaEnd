#pragma once

#include <string>
#include <string_view>
#include <vector>

#include <opencv2/core.hpp>

#include "roi_template_scanner.h"

struct MaaContext;

namespace mapnavigator
{

struct PromptScanProfile
{
    std::string scan_node;       // 空表示默认节点
    cv::Rect base_roi {};        // 基础帧坐标
    std::vector<ScanIcon> icons; // 每个模板一个, 任一命中即算有提示
};

// 返回的 ROI 是基础帧坐标
bool TryReadNodeRoi(MaaContext* context, const std::string& node_name, cv::Rect* out);

// 读不到节点返回空串, 且不打日志
std::string ReadNodeRecognitionType(MaaContext* context, const std::string& node_name);

// 匹配器与扫描器的前提都在这里校验, controller_type 决定模板按哪层资源解析
bool TryLoadPromptScanProfile(MaaContext* context, const std::string& scan_node, std::string_view controller_type, PromptScanProfile* out);

// 只读 OCR 节点的 expected, 不派发该节点
bool TryReadNodeInteractTexts(MaaContext* context, const std::string& node_name, std::vector<std::string>* out);

} // namespace mapnavigator
