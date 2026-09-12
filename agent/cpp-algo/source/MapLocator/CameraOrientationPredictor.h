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

// 摄像机朝向识别：把小地图中心圆环展开为极坐标条带，交给 cameraorientation.onnx 或
// cao_ref.onnx 推理。两个模型接收同一极坐标几何的条带，区别只在输入通道：
//   - polar：观测条带 42x360x3 BGR，输入 cameraorientation.onnx；
//   - ref：观测 + 参考底图配对 42x360x7 `[obs.BGR, ref.BGR, ref.A]`，输入 cao_ref.onnx。
// 参考由定位结果 (x, y) 处的 zone 底图裁剪、黑底合成与观测背底合成后同几何展开。
// 分派规则：参考 alpha 展开环内缺失占比严格大于 kReferenceGapDispatchThreshold 时用
// polar，否则用 ref；参考资产或 ref 模型不可用时全部回退 polar。
// 识别目标与角色箭头（InferYellowArrowRotation）完全无关，结果仅供上层参考，
// 不参与定位匹配与遮挡判定。
class CameraOrientationPredictor
{
public:
    explicit CameraOrientationPredictor(const std::string& polarModelPath, const std::string& refModelPath, int threads = 1);
    ~CameraOrientationPredictor() = default;

    // 输入 minimap 应为 TryExtractMinimap 产物（720p 基准下 118x120 的小地图）。
    // referenceAsset 为 zone 底图（BGRA）；(x, y) 为定位结果，scale 为
    // ZoneTemplateScale(zoneId)。模型未加载、输入不合法或推理失败时返回 std::nullopt。
    std::optional<CameraOrientation>
        predict(const cv::Mat& minimap, const cv::Mat& referenceAsset, double x, double y, double scale, const std::string& zoneId);

    bool isLoaded() const { return isPolarModelLoaded_ || isRefModelLoaded_; }

private:
    bool loadSession(
        const std::string& modelPath,
        const char* tag,
        const Ort::SessionOptions& options,
        std::unique_ptr<Ort::Session>* out_session);

    // 校验 minimap 尺寸并在尺寸变化时重建极坐标映射表；极点为小地图几何中心。
    bool prepareRingGeometry(const cv::Mat& minimap);
    // 观测条带：remap 到 stripScratch。
    void buildObservedStrip(const cv::Mat& observedBgr);
    // 参考输入：以 (x, y) 为中心裁剪 zone 底图，黑底合成、观测背底合成后同几何展开，
    // 写入 referenceStripScratch / referenceAlphaStripScratch；(*)gap 为展开 alpha < 255 占比。
    void buildReferenceInput(const cv::Mat& referenceAsset, const cv::Mat& observedBgr, double x, double y, double scale, double* out_gap);
    // 7 通道 NHWC 输入 `[obs.BGR, ref.BGR, ref.A]`，写入 refInputScratch。
    void assembleRefInput();

    std::optional<CameraOrientation> decodePmf(const float* pmf, size_t count) const;

    std::unique_ptr<Ort::Env> ortEnv;
    std::unique_ptr<Ort::Session> polarSession;
    std::unique_ptr<Ort::Session> refSession;

    bool isPolarModelLoaded_ = false;
    bool isRefModelLoaded_ = false;
    // Ort::Session::Run 线程安全，但展开与解码共用的 scratch 不是；防多帧 locate 并发。
    std::mutex predictMutex;
    cv::Mat mapXScratch;
    cv::Mat mapYScratch;
    float mapCenterX = 0.0f;
    float mapCenterY = 0.0f;
    cv::Mat stripScratch;               // 观测条带 42x360 BGR
    cv::Mat referenceCropScratch;       // 参考裁剪：黑底合成 BGR -> 观测背底合成 BGR
    cv::Mat referenceAlphaScratch;      // 参考裁剪：原始 alpha
    cv::Mat referenceStripScratch;      // 参考 BGR 条带 42x360
    cv::Mat referenceAlphaStripScratch; // 参考 alpha 条带 42x360
    cv::Mat refInputScratch;            // 7 通道 NHWC 输入 42x360x7
};

} // namespace maplocator
