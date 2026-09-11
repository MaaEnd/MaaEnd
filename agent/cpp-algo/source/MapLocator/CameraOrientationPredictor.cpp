#include <algorithm>
#include <array>
#include <cmath>
#include <numbers>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#include "CameraOrientationPredictor.h"

namespace maplocator
{

namespace
{
// 以下几何与解码约定均与 cameraorientation.onnx 绑定；改动任何
// 一项都必须与模型同步重新导出，不得单方面修改。

// 极坐标条带：每列 1°，列 0 = 正北，顺时针；行 0 = 内径，行 i 采样半径
// 为 kInnerRadius + (i + 0.5) * step，step = (kOuterRadius - kInnerRadius) / kStripHeight。
constexpr int kStripWidth = 360;
constexpr int kStripHeight = 42;
constexpr double kInnerRadius = 12.0;
constexpr double kOuterRadius = 54.0;
// argmax 定峰后 ±5° 窗口内按 pmf 加权圆均值，得到亚度级解码角。
constexpr int kRefineRadius = 5;

// 节点名是导出时定死的图属性；本模型没有 cls.onnx 那样的 json sidecar，直接写死。
constexpr const char* kInputName = "strip";
constexpr const char* kOutputName = "pmf";

} // namespace

CameraOrientationPredictor::CameraOrientationPredictor(const std::string& modelPath, int threads)
{
    if (modelPath.empty()) {
        return;
    }

    try {
        ortEnv = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "MapLocatorCameraOrientation");
        Ort::SessionOptions sessionOptions;
        sessionOptions.SetIntraOpNumThreads(std::max(1, threads));
        sessionOptions.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_ALL);

        auto osModelPath = MAA_NS::to_osstring(modelPath);
        ortSession = std::make_unique<Ort::Session>(*ortEnv, osModelPath.c_str(), sessionOptions);
        isModelLoaded_ = true;
        LogInfo << "CameraOrientation model loaded successfully.";
    }
    catch (const Ort::Exception& e) {
        LogError << "CameraOrientation: failed to load model" << VAR(modelPath) << VAR(e.what());
        ortEnv.reset();
        ortSession.reset();
        isModelLoaded_ = false;
    }
}

std::optional<CameraOrientation> CameraOrientationPredictor::predict(const cv::Mat& minimap)
{
    std::lock_guard<std::mutex> lock(predictMutex);

    if (!isModelLoaded_ || !ortSession) {
        LogError << "CameraOrientation Error: Model is NOT loaded.";
        return std::nullopt;
    }
    if (minimap.empty()) {
        LogError << "CameraOrientation Error: Input minimap is empty.";
        return std::nullopt;
    }

    // 极点为小地图几何中心（720p 基准下即全图坐标 (108, 111)）。
    const double cx = minimap.cols / 2.0;
    const double cy = minimap.rows / 2.0;
    // 双线性插值需要在源图内取到邻居像素：要求整个圆盘加一圈邻居都在图内。
    if (kOuterRadius + 1.0 > cx || kOuterRadius + 1.0 > cy || cx > minimap.cols - kOuterRadius - 1.0
        || cy > minimap.rows - kOuterRadius - 1.0) {
        LogError << "CameraOrientation: minimap too small for the orientation ring" << VAR(minimap.cols) << VAR(minimap.rows);
        return std::nullopt;
    }

    // 源坐标映射只依赖小地图尺寸，按尺寸缓存：
    // src_x = cx + r * sin(theta)，src_y = cy - r * cos(theta)，theta = 列序号（度）。
    if (mapXScratch.size() != minimap.size()) {
        mapXScratch.create(kStripHeight, kStripWidth, CV_32FC1);
        mapYScratch.create(kStripHeight, kStripWidth, CV_32FC1);
        const double step = (kOuterRadius - kInnerRadius) / kStripHeight;
        for (int i = 0; i < kStripHeight; ++i) {
            const double radius = kInnerRadius + (i + 0.5) * step;
            for (int j = 0; j < kStripWidth; ++j) {
                const double theta = j * (std::numbers::pi / 180.0);
                mapXScratch.at<float>(i, j) = static_cast<float>(cx + radius * std::sin(theta));
                mapYScratch.at<float>(i, j) = static_cast<float>(cy - radius * std::cos(theta));
            }
        }
    }

    // 模型输入契约是 BGR HWC uint8；BGRA 先转 3 通道再采样，避免 4 通道双线性
    // 插值的无谓开销。
    cv::Mat source = minimap;
    cv::Mat converted;
    if (source.channels() == 4) {
        cv::cvtColor(source, converted, cv::COLOR_BGRA2BGR);
        source = converted;
    }
    // BORDER_REPLICATE 使越界采样取边界像素，避免圆盘边缘插值失真。
    cv::remap(source, stripScratch, mapXScratch, mapYScratch, cv::INTER_LINEAR, cv::BORDER_REPLICATE);

    // /255 已折入模型首层卷积权重，输入保持 0..255 数值域；remap 输出总是连续内存。
    constexpr std::array<int64_t, 4> kInputShape { 1, kStripHeight, kStripWidth, 3 };
    auto memoryInfo = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
    Ort::Value inputTensor = Ort::Value::CreateTensor<std::uint8_t>(
        memoryInfo,
        stripScratch.data,
        static_cast<size_t>(kStripHeight) * kStripWidth * 3,
        kInputShape.data(),
        kInputShape.size());

    const char* inputNames[] = { kInputName };
    const char* outputNames[] = { kOutputName };
    auto outputTensors = ortSession->Run(Ort::RunOptions { nullptr }, inputNames, &inputTensor, 1, outputNames, 1);
    if (outputTensors.empty()) {
        LogError << "CameraOrientation: empty inference output.";
        return std::nullopt;
    }

    const float* pmf = outputTensors.front().GetTensorData<float>();
    const size_t count = outputTensors.front().GetTensorTypeAndShapeInfo().GetElementCount();
    return decodePmf(pmf, count);
}

std::optional<CameraOrientation> CameraOrientationPredictor::decodePmf(const float* pmf, size_t count) const
{
    if (pmf == nullptr || count != static_cast<size_t>(kStripWidth)) {
        LogError << "CameraOrientation: unexpected pmf size" << VAR(count);
        return std::nullopt;
    }

    // 全 360 bin 方向向量（方向 = bin 方位角，长度 = 概率）合成，用于置信度。
    double resultantSin = 0.0;
    double resultantCos = 0.0;
    for (int j = 0; j < kStripWidth; ++j) {
        const double theta = j * (std::numbers::pi / 180.0);
        resultantSin += pmf[j] * std::sin(theta);
        resultantCos += pmf[j] * std::cos(theta);
    }

    int center = 0;
    for (int j = 1; j < kStripWidth; ++j) {
        if (pmf[j] > pmf[center]) {
            center = j;
        }
    }

    // argmax ±kRefineRadius 窗口内按 pmf 加权圆均值；接缝两侧靠取模跨 0/360。
    double windowSin = 0.0;
    double windowCos = 0.0;
    for (int offset = -kRefineRadius; offset <= kRefineRadius; ++offset) {
        const int col = (center + offset + kStripWidth) % kStripWidth;
        const double theta = col * (std::numbers::pi / 180.0);
        windowSin += pmf[col] * std::sin(theta);
        windowCos += pmf[col] * std::cos(theta);
    }

    double decoded = std::atan2(windowSin, windowCos) * (180.0 / std::numbers::pi);
    decoded = std::fmod(decoded + 360.0, 360.0);
    if (decoded >= 360.0) {
        decoded = 0.0;
    }

    // 置信度 = 合成模长 × cos(解码方向与合成方向的夹角)；
    // 分布集中且窗口均值对准合成方向时接近 1，均匀、多峰对消时趋 0。
    // 夹角超过 90° 时为负，语义上属"强烈不一致"，对外统一裁到 0，只在日志里保留。
    const double resultantAngle = std::atan2(resultantSin, resultantCos) * (180.0 / std::numbers::pi);
    const double resultantLength = std::hypot(resultantSin, resultantCos);
    const double alignmentCos = std::cos(std::abs(decoded - resultantAngle) * (std::numbers::pi / 180.0));
    const double confidence = std::clamp(resultantLength * alignmentCos, 0.0, 1.0);
    if (alignmentCos < 0.0) {
        LogWarn << "CameraOrientation: decoded direction diverges from resultant" << VAR(decoded) << VAR(resultantAngle);
    }

    LogTrace << "CameraOrientation pmf:" << std::vector<float>(pmf, pmf + count);
    LogDebug << "CameraOrientation:" << VAR(decoded) << VAR(confidence) << VAR(center);

    return CameraOrientation { .rot = decoded, .confidence = confidence };
}

} // namespace maplocator
