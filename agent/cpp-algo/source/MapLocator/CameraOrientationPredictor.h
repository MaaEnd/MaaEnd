#pragma once

#include <memory>
#include <mutex>
#include <onnxruntime/onnxruntime_cxx_api.h>
#include <optional>
#include <string>

#include <MaaUtils/NoWarningCV.hpp>

#include "MapTypes.h"

namespace maplocator
{

// 摄像机朝向推理：消费交付工件 map/cameraorientation/{preprocess,polar_with_ref}.onnx。
//
// preprocess.onnx 承载前处理的唯一实现（极坐标几何、参考采样与条带域合成、采样与
// 取整约定）：输入观测 ROI + zone 底图资产 + 定位 (x, y, scale)，输出观测条带与参考
// 条带。参考 BGR 在资产透明处按白底合成，alpha 保留资产原始值；资产透明与裁剪越界
// 同属「参考缺失」，由 ref.A（0 = 缺失）表达。polar_with_ref.onnx 消费 7 通道
// [obs.BGR, ref.BGR, ref.A] 参考配对，输出 360 bin 方位角概率分布。
//
// 参考缺失已在配对里表达、由模型自行处理，故这里不选路也不回退：底图资产缺失或非
// BGRA 时喂全透明占位资产，配对即为全缺失。
//
// 参考配对依赖定位结果 (x, y, zone)，只在定位成功且 zone 非 None 的帧调用。
//
// 识别目标与角色箭头（InferYellowArrowRotation）完全无关，结果仅供上层参考，
// 不参与定位匹配与遮挡判定。
class CameraOrientationPredictor
{
public:
    explicit CameraOrientationPredictor(const std::string& preprocessModelPath, const std::string& refModelPath, int threads = 2);
    ~CameraOrientationPredictor() = default;

    // 输入 minimap 应为 TryExtractMinimap 产物（720p 基准下 118x120 的小地图）。
    // referenceAsset 为 zone 底图（BGRA）；缺失或非 BGRA 时以全透明占位资产喂入。
    // (x, y) 为定位结果，scale 为 ZoneTemplateScale(zoneId)。模型未加载、输入不
    // 合法或推理失败时返回 std::nullopt。
    std::optional<CameraOrientation>
        predict(const cv::Mat& minimap, const cv::Mat& referenceAsset, double x, double y, double scale, const std::string& zoneId);

    // 前处理图与参考配对分类器同时可用才允许推理。
    bool isLoaded() const { return isPreprocessModelLoaded_ && isRefModelLoaded_; }

private:
    std::optional<CameraOrientation>
        infer(const cv::Mat& minimap, const cv::Mat& asset, double x, double y, double scale, const std::string& zoneId);
    bool loadSession(
        const std::string& modelPath,
        const char* tag,
        const Ort::SessionOptions& options,
        std::unique_ptr<Ort::Session>* out_session);

    std::optional<CameraOrientation> decodePmf(const float* pmf, size_t count) const;

    std::unique_ptr<Ort::Env> ortEnv;
    std::unique_ptr<Ort::Session> preprocessSession;
    std::unique_ptr<Ort::Session> refSession;

    bool isPreprocessModelLoaded_ = false;
    bool isRefModelLoaded_ = false;
    // Ort::Session::Run 线程安全，但预测共用的拼接 scratch 不是；防多帧 locate 并发。
    std::mutex predictMutex;
    cv::Mat refInputScratch; // 7 通道 NHWC 拼接输入
};

} // namespace maplocator
