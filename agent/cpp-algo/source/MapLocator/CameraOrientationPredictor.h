#pragma once

#include <memory>
#include <mutex>
#include <onnxruntime/onnxruntime_cxx_api.h>
#include <optional>
#include <string>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>

#include "MapTypes.h"

namespace maplocator
{

// 摄像机朝向识别：把小地图中心圆环展开为极坐标条带，用 cameraorientation.onnx
// 输出摄像机朝向。识别目标与角色箭头（InferYellowArrowRotation）完全无关，
// 结果仅供上层参考，不参与定位匹配与遮挡判定。
class CameraOrientationPredictor
{
public:
    explicit CameraOrientationPredictor(const std::string& modelPath, int threads = 1);
    ~CameraOrientationPredictor() = default;

    // 输入应为 TryExtractMinimap 产物（720p 基准下 118x120 的小地图）。
    // 模型未加载、输入不合法或推理失败时返回 std::nullopt。
    std::optional<CameraOrientation> predict(const cv::Mat& minimap);

    bool isLoaded() const { return isModelLoaded_; }

private:
    std::optional<CameraOrientation> decodePmf(const float* pmf, size_t count) const;

    std::unique_ptr<Ort::Env> ortEnv;
    std::unique_ptr<Ort::Session> ortSession;

    bool isModelLoaded_ = false;
    // Ort::Session::Run 线程安全，但展开与解码共用的 scratch 不是；防多帧 locate 并发。
    std::mutex predictMutex;
    cv::Mat mapXScratch;
    cv::Mat mapYScratch;
    cv::Mat stripScratch;
};

} // namespace maplocator
