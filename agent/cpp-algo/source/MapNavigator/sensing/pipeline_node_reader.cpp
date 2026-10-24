#include "pipeline_node_reader.h"

#include <algorithm>
#include <cmath>
#include <utility>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../control/controller_type_utils.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace
{

// 框架回吐的节点恒为 { "recognition": { "type": ..., "param": { ... } } }
const json::object* RecognitionOf(const json::object& node)
{
    if (!node.contains("recognition") || !node.at("recognition").is_object()) {
        return nullptr;
    }
    return &node.at("recognition").as_object();
}

const json::object* RecognitionParam(const json::object& node)
{
    const json::object* reco = RecognitionOf(node);
    if (reco == nullptr || !reco->contains("param") || !reco->at("param").is_object()) {
        return nullptr;
    }
    return &reco->at("param").as_object();
}

} // namespace

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

const json::value* FindRecognitionParam(const json::object& node, const char* key)
{
    const json::object* param = RecognitionParam(node);
    return param != nullptr && param->contains(key) ? &param->at(key) : nullptr;
}

std::string ReadRecognitionType(const json::object& node)
{
    const json::object* reco = RecognitionOf(node);
    return reco != nullptr && reco->contains("type") && reco->at("type").is_string() ? reco->at("type").as_string() : std::string {};
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

bool ReadStringList(const json::value& holder, std::vector<std::string>* out)
{
    if (!holder.is_array() || holder.as_array().empty()) {
        return false;
    }
    std::vector<std::string> values;
    for (const json::value& item : holder.as_array()) {
        if (!item.is_string() || item.as_string().empty()) {
            return false;
        }
        values.push_back(item.as_string());
    }
    *out = std::move(values);
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

bool ReadNumberList(const json::value& holder, std::vector<double>* out)
{
    if (!holder.is_array() || holder.as_array().empty()) {
        return false;
    }
    std::vector<double> values;
    for (const json::value& item : holder.as_array()) {
        if (!item.is_number()) {
            return false;
        }
        values.push_back(item.as_double());
    }
    *out = std::move(values);
    return true;
}

std::string ReadNodeRecognitionType(MaaContext* context, const std::string& node_name)
{
    const auto node = ReadNodeObject(context, node_name, nullptr);
    return node ? ReadRecognitionType(*node) : std::string {};
}

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

} // namespace mapnavigator
