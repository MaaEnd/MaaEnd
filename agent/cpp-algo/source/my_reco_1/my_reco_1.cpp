#include "my_reco_1.h"

#include <iostream>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>

// MaaUtils 里有很多通用工具，请优先考虑使用。
// 对于通用需求，也欢迎提交 issue 或 PR 给 MaaUtils 仓库。
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

cv::Mat to_mat(const MaaImageBuffer* buffer)
{
    return cv::Mat(
        MaaImageBufferHeight(buffer),
        MaaImageBufferWidth(buffer),
        MaaImageBufferChannels(buffer),
        MaaImageBufferGetRawData(buffer));
}

MaaBool ChildCustomRecognitionCallback(
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
    // sample for logging
    LogInfo << VAR(context) << VAR(task_id) << VAR(node_name) << VAR(custom_recognition_name) << VAR(custom_recognition_param) << VAR(image)
            << VAR(roi) << VAR(trans_arg);

    // sample for MaaFramework APIs
    auto* tasker = MaaContextGetTasker(context);
    std::ignore = tasker;

    // sample for OpenCV APIs
    cv::Mat img = to_mat(image);
    cv::Mat hsv;
    cv::cvtColor(img, hsv, cv::COLOR_BGR2HSV);

    // output result
    if (out_box) {
        *out_box = { 100, 100, 10, 10 };
    }
    if (out_detail) {
        json::value j;
        j["key"] = "value";

        MaaStringBufferSet(out_detail, j.dumps().c_str());
    }

    // means success, otherwise return false and set error message in out_detail
    return true;
}