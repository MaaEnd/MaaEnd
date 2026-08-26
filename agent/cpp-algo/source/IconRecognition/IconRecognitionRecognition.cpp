#include "IconRecognitionRecognition.h"

#include <cstdint>
#include <cstring>
#include <memory>
#include <mutex>
#include <stdexcept>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../utils.h"
#include "IconRecognizer.h"
#include "detail/DebugCapture.h"
#include "detail/GridProfiles.h"

#ifndef MAA_TRUE
#define MAA_TRUE 1
#define MAA_FALSE 0
#endif

namespace iconrecognition
{
namespace
{

CandidateFilter ReadCandidates(const json::object& object)
{
    CandidateFilter candidates;
    const auto read = [&object](const char* key) {
        std::vector<std::string> values;
        if (!object.contains(key)) {
            return values;
        }
        if (!object.at(key).is_array()) {
            throw std::invalid_argument(std::string("IconRecognition ") + key + " must be an array of strings");
        }
        for (const auto& value : object.at(key).as_array()) {
            if (!value.is_string()) {
                throw std::invalid_argument(std::string("IconRecognition ") + key + " must be an array of strings");
            }
            values.push_back(value.as_string());
        }
        return values;
    };
    candidates.item_ids = read("item_ids");
    candidates.item_filters = read("item_filters");
    candidates.additional_item_filters = read("additional_item_filters");
    candidates.excluded_item_ids = read("excluded_item_ids");
    candidates.item_recheck_filters = read("item_recheck_filters");
    return candidates;
}

double ReadDouble(const json::object& object, const char* key, double fallback)
{
    if (!object.contains(key)) {
        return fallback;
    }
    if (!object.at(key).is_number()) {
        throw std::invalid_argument(std::string("IconRecognition ") + key + " must be a number");
    }
    return object.at(key).as_double();
}

bool ReadBool(const json::object& object, const char* key, bool fallback)
{
    if (!object.contains(key)) {
        return fallback;
    }
    if (!object.at(key).is_boolean()) {
        throw std::invalid_argument(std::string("IconRecognition ") + key + " must be a boolean");
    }
    return object.at(key).as_boolean();
}

std::string ReadString(const json::object& object, const char* key, std::string fallback)
{
    if (!object.contains(key)) {
        return fallback;
    }
    if (!object.at(key).is_string()) {
        throw std::invalid_argument(std::string("IconRecognition ") + key + " must be a string");
    }
    return object.at(key).as_string();
}

IconRecognizer& GetRecognizer()
{
    static std::once_flag flag;
    static std::unique_ptr<IconRecognizer> recognizer;
    std::call_once(flag, [] {
        recognizer = std::make_unique<IconRecognizer>(get_exe_dir() / ".." / "data" / "IconRecognition");
        recognizer->initialize();
    });
    return *recognizer;
}

std::string ControllerTypeFromContext(MaaContext* context)
{
    if (context == nullptr) {
        return {};
    }
    MaaTasker* tasker = MaaContextGetTasker(context);
    MaaController* controller = tasker == nullptr ? nullptr : MaaTaskerGetController(tasker);
    if (controller == nullptr) {
        return {};
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaControllerGetInfo(controller, buffer.Get()) || MaaStringBufferIsEmpty(buffer.Get())) {
        return {};
    }
    const char* raw = MaaStringBufferGet(buffer.Get());
    const auto parsed = raw == nullptr ? std::optional<json::value> {} : json::parse(raw);
    if (!parsed || !parsed->is_object() || !parsed->as_object().contains("type")) {
        return {};
    }
    const auto& type = parsed->as_object().at("type");
    return type.is_string() ? type.as_string() : std::string {};
}

void WriteDetail(MaaStringBuffer* buffer, const RecognitionResult& result)
{
    if (buffer == nullptr) {
        return;
    }
    const std::string text = json::value(result).dumps();
    MaaStringBufferSet(buffer, text.c_str());
}

void SaveDebugCaptureBestEffort(const cv::Mat& image, const RecognitionResult& result, MaaTaskId task_id) noexcept
{
    try {
        const auto root = get_exe_dir() / ".." / "debug" / "vision" / "IconRecognition";
        if (!detail::SaveDebugCapture(root, image, result, static_cast<std::uint64_t>(task_id))) {
            LogWarn << "IconRecognition debug capture failed" << VAR(task_id) << VAR(root);
        }
    }
    catch (const std::exception& error) {
        LogWarn << "IconRecognition debug capture failed" << VAR(task_id) << VAR(error.what());
    }
    catch (...) {
        LogWarn << "IconRecognition debug capture failed with an unknown error" << VAR(task_id);
    }
}

} // namespace

MaaBool MAA_CALL IconRecognitionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    std::optional<GridType> parsed_grid_type;
    bool debug_requested = false;
    if (image == nullptr || MaaImageBufferIsEmpty(image)) {
        RecognitionResult result;
        result.has_grid_type = false;
        result.error_code = "invalid_image";
        result.message = "Input image is empty";
        WriteDetail(out_detail, result);
        return MAA_FALSE;
    }
    try {
        const auto parsed = custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0
                                ? std::optional<json::value>(json::object {})
                                : json::parse(custom_recognition_param);
        if (!parsed || !parsed->is_object()) {
            throw std::invalid_argument("IconRecognition custom param must be a JSON object");
        }
        const auto& object = parsed->as_object();
        if (!object.contains("grid_type")) {
            throw std::invalid_argument("IconRecognition grid_type is required");
        }
        parsed_grid_type = ParseGridType(ReadString(object, "grid_type", {}));
        if (!parsed_grid_type) {
            throw std::invalid_argument("unknown IconRecognition grid_type");
        }
        if (roi == nullptr) {
            throw std::invalid_argument("IconRecognition roi is required");
        }
        if (roi->width <= 0 || roi->height <= 0) {
            throw std::invalid_argument("IconRecognition roi width and height must be positive");
        }
        if (object.contains("grid_scale")) {
            throw std::invalid_argument("IconRecognition grid_scale is not supported; controller profile is selected automatically");
        }
        const bool debug = ReadBool(object, "debug", false);
        debug_requested = debug;
        RecognitionRequest request;
        request.grid_type = *parsed_grid_type;
        request.roi = cv::Rect(roi->x, roi->y, roi->width, roi->height);
        request.candidates = ReadCandidates(object);
        request.grid_scale_hint = detail::GridScaleForControllerType(ControllerTypeFromContext(context));
        request.threshold = ReadDouble(object, "threshold", request.threshold);
        request.subpixel_threshold = ReadDouble(object, "subpixel_threshold", request.subpixel_threshold);
        request.deduplicate = ReadBool(object, "deduplicate", request.deduplicate);
        request.recognize_region_unavailable = ReadBool(
            object,
            "recognize_region_unavailable",
            request.recognize_region_unavailable);
        request.debug = debug;
        RecognitionResult result = GetRecognizer().recognize(to_mat(image), request);
        if (debug) {
            SaveDebugCaptureBestEffort(to_mat(image), result, task_id);
        }
        WriteDetail(out_detail, result);
        if (!result.matched || result.matches.empty()) {
            return MAA_FALSE;
        }
        if (out_box != nullptr) {
            *out_box = MaaRect { result.matches.front().cell_box.x,
                                 result.matches.front().cell_box.y,
                                 result.matches.front().cell_box.width,
                                 result.matches.front().cell_box.height };
        }
        return MAA_TRUE;
    }
    catch (const std::invalid_argument& e) {
        RecognitionResult result;
        if (parsed_grid_type) {
            result.grid_type = *parsed_grid_type;
        }
        else {
            result.has_grid_type = false;
        }
        result.error_code = "invalid_argument";
        result.message = e.what();
        if (debug_requested && image != nullptr && !MaaImageBufferIsEmpty(image)) {
            SaveDebugCaptureBestEffort(to_mat(image), result, task_id);
        }
        WriteDetail(out_detail, result);
        LogError << "IconRecognition rejected invalid input" << VAR(e.what());
        return MAA_FALSE;
    }
    catch (const std::exception& e) {
        RecognitionResult result;
        if (parsed_grid_type) {
            result.grid_type = *parsed_grid_type;
        }
        else {
            result.has_grid_type = false;
        }
        result.error_code = "exception";
        result.message = e.what();
        if (debug_requested && image != nullptr && !MaaImageBufferIsEmpty(image)) {
            SaveDebugCaptureBestEffort(to_mat(image), result, task_id);
        }
        WriteDetail(out_detail, result);
        LogError << "IconRecognition failed" << VAR(e.what());
        return MAA_FALSE;
    }
}

} // namespace iconrecognition
