#pragma once

#include <optional>
#include <string>
#include <vector>

#include <meojson/json.hpp>
#include <opencv2/core.hpp>

struct MaaContext;

namespace mapnavigator
{

// 返回的 ROI 是基础帧坐标
bool TryReadNodeRoi(MaaContext* context, const std::string& node_name, cv::Rect* out);

// 读不到节点返回空串, 且不打日志
std::string ReadNodeRecognitionType(MaaContext* context, const std::string& node_name);

// 只读 OCR 节点的 expected, 不派发该节点
bool TryReadNodeInteractTexts(MaaContext* context, const std::string& node_name, std::vector<std::string>* out);

// purpose 为 nullptr 时不打日志
std::optional<json::object> ReadNodeObject(MaaContext* context, const std::string& node_name, const char* purpose);

std::string ReadRecognitionType(const json::object& node);
const json::value* FindRecognitionParam(const json::object& node, const char* key);

bool ReadRoiArray(const json::value& holder, cv::Rect* out);
// 作者可以写单值, 但框架回吐时 template 与 threshold 恒为数组
bool ReadStringList(const json::value& holder, std::vector<std::string>* out);
bool ReadBool(const json::value& holder, bool* out);
bool ReadNumberList(const json::value& holder, std::vector<double>* out);

} // namespace mapnavigator
