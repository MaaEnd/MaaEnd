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

// 摄像机朝向推理：消费交付工件 map/cameraorientation/{preprocess,polar,polar_with_ref}.onnx。
//
// preprocess.onnx 承载前处理的唯一实现（极坐标几何、参考采样与条带域合成、采样与
// 取整约定）：输入观测 ROI + zone 底图资产 + 定位 (x, y, scale)，输出观测条带与参考
// 条带（含参考原始 alpha）。polar.onnx / polar_with_ref.onnx 分别消费 3 / 7 通道条带
// （7 通道 = [obs.BGR, ref.BGR, ref.A]），输出同构的方位角概率分布。
//
// 本类只保留三项判断：参考 alpha 缺口占比、严格阈值分派、PMF 解码；其余是机械动作
// （会话加载、张量创建/零拷贝、通道拼接、类型转换、日志）。参考分类器未加载或参考
// 底图不可用时走 polar.onnx；分派后所选分类器不可用或推理失败返回 std::nullopt
// （上层该帧无 camRot），不做跨模型回退。
//
// 识别目标与角色箭头（InferYellowArrowRotation）完全无关，结果仅供上层参考，
// 不参与定位匹配与遮挡判定。
class CameraOrientationPredictor
{
public:
    explicit CameraOrientationPredictor(
        const std::string& preprocessModelPath,
        const std::string& polarModelPath,
        const std::string& refModelPath,
        int threads = 1);
    ~CameraOrientationPredictor() = default;

    // 输入 minimap 应为 TryExtractMinimap 产物（720p 基准下 118x120 的小地图）。
    // referenceAsset 为 zone 底图（BGRA）；(x, y) 为定位结果，scale 为
    // ZoneTemplateScale(zoneId)。模型未加载、输入不合法或推理失败时返回 std::nullopt。
    std::optional<CameraOrientation>
        predict(const cv::Mat& minimap, const cv::Mat& referenceAsset, double x, double y, double scale, const std::string& zoneId);

    // 前处理图与至少一个分类器同时可用才允许推理。
    bool isLoaded() const { return isPreprocessModelLoaded_ && (isPolarModelLoaded_ || isRefModelLoaded_); }

private:
    bool loadSession(
        const std::string& modelPath,
        const char* tag,
        const Ort::SessionOptions& options,
        std::unique_ptr<Ort::Session>* out_session);

    std::optional<CameraOrientation> decodePmf(const float* pmf, size_t count) const;

    std::unique_ptr<Ort::Env> ortEnv;
    std::unique_ptr<Ort::Session> preprocessSession;
    std::unique_ptr<Ort::Session> polarSession;
    std::unique_ptr<Ort::Session> refSession;

    bool isPreprocessModelLoaded_ = false;
    bool isPolarModelLoaded_ = false;
    bool isRefModelLoaded_ = false;
    // Ort::Session::Run 线程安全，但预测共用的拼接 scratch 不是；防多帧 locate 并发。
    std::mutex predictMutex;
    cv::Mat refInputScratch; // 7 通道 NHWC 拼接输入
};

} // namespace maplocator
