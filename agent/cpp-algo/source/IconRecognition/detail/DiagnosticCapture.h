#pragma once

#include <cstdint>
#include <memory>
#include <string>

#include <MaaFramework/MaaDef.h>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

class DiagnosticCapture
{
public:
    DiagnosticCapture();
    ~DiagnosticCapture();
    DiagnosticCapture(const DiagnosticCapture&) = delete;
    DiagnosticCapture& operator=(const DiagnosticCapture&) = delete;

    // 在当前识别回调内调用 Screencap，将控制器缓存整图无损保存为 PNG。
    void submit(
        MaaContext* context,
        const cv::Mat& image,
        const RecognitionRequest& request,
        const RecognitionResult& result,
        std::uint64_t task_id,
        const std::string& node_name,
        const std::string& controller_type) noexcept;

private:
    class Impl;
    std::unique_ptr<Impl> impl_;
};

DiagnosticCapture& GetDiagnosticCapture();

} // namespace iconrecognition::detail
