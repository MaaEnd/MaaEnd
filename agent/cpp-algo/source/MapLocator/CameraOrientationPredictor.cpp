#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>
#include <iterator>
#include <numbers>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#include "CameraOrientationPredictor.h"

namespace maplocator
{

namespace
{
// 三件套工件定死的输入/输出名；改动即契约变更。
constexpr const char* kPreprocessInputNames[] = { "minimap", "asset", "x", "y", "scale" };
constexpr const char* kPreprocessOutputNames[] = { "observed", "reference" };
constexpr const char* kClassifierInputName = "strip";
constexpr const char* kClassifierOutputName = "pmf";

// 缺口占比分派阈值：参考条带 alpha（< 255）占比严格大于该值时用观测分类器，
// 否则用参考配对分类器。与工件口径绑定，不可单方面修改。
constexpr double kReferenceGapDispatchThreshold = 0.3;
// PMF 解码的定峰窗口半径（bin）：argmax 后在该窗口内按概率加权求圆均值。
constexpr int kRefineRadius = 5;

// 参考资产不可用时的占位输入：1x1 全 0 BGRA。该路线只用于产出观测条带，参考
// 条带不参与判断，分派走观测分类器。
const cv::Mat kUnavailableAsset(1, 1, CV_8UC4, cv::Scalar::all(0));
} // namespace

CameraOrientationPredictor::CameraOrientationPredictor(
    const std::string& preprocessModelPath,
    const std::string& polarModelPath,
    const std::string& refModelPath,
    int threads)
{
    // 前处理图是观测条带的唯一来源；缺失时预测器不可用，无需加载分类器。
    if (preprocessModelPath.empty()) {
        LogError << "CameraOrientation: preprocess model path is empty; predictor disabled.";
        return;
    }

    try {
        ortEnv = std::make_unique<Ort::Env>(ORT_LOGGING_LEVEL_WARNING, "MapLocatorCameraOrientation");
    }
    catch (const Ort::Exception& e) {
        LogError << "CameraOrientation: failed to create ONNX environment" << VAR(e.what());
        return;
    }

    Ort::SessionOptions sessionOptions;
    sessionOptions.SetIntraOpNumThreads(std::max(1, threads));
    sessionOptions.SetGraphOptimizationLevel(GraphOptimizationLevel::ORT_ENABLE_ALL);

    isPreprocessModelLoaded_ = loadSession(preprocessModelPath, "preprocess", sessionOptions, &preprocessSession);
    isPolarModelLoaded_ = loadSession(polarModelPath, "polar", sessionOptions, &polarSession);
    isRefModelLoaded_ = loadSession(refModelPath, "polar_with_ref", sessionOptions, &refSession);

    if (!isLoaded()) {
        LogError << "CameraOrientation: predictor disabled" << VAR(isPreprocessModelLoaded_) << VAR(isPolarModelLoaded_)
                 << VAR(isRefModelLoaded_);
        ortEnv.reset();
    }
}

bool CameraOrientationPredictor::loadSession(
    const std::string& modelPath,
    const char* tag,
    const Ort::SessionOptions& options,
    std::unique_ptr<Ort::Session>* out_session)
{
    if (modelPath.empty()) {
        return false;
    }

    try {
        auto osModelPath = MAA_NS::to_osstring(modelPath);
        *out_session = std::make_unique<Ort::Session>(*ortEnv, osModelPath.c_str(), options);
        LogInfo << "CameraOrientation model loaded successfully." << VAR(tag) << VAR(modelPath);
        return true;
    }
    catch (const Ort::Exception& e) {
        LogError << "CameraOrientation: failed to load model" << VAR(tag) << VAR(modelPath) << VAR(e.what());
        out_session->reset();
        return false;
    }
}

std::optional<CameraOrientation> CameraOrientationPredictor::predict(
    const cv::Mat& minimap,
    const cv::Mat& referenceAsset,
    double x,
    double y,
    double scale,
    const std::string& zoneId)
{
    std::lock_guard<std::mutex> lock(predictMutex);

    if (!isLoaded() || !preprocessSession) {
        LogError << "CameraOrientation Error: Model is NOT loaded.";
        return std::nullopt;
    }
    if (minimap.empty() || !minimap.isContinuous() || (minimap.channels() != 3 && minimap.channels() != 4)) {
        LogError << "CameraOrientation Error: invalid minimap input" << VAR(minimap.cols) << VAR(minimap.rows) << VAR(minimap.channels());
        return std::nullopt;
    }

    // 模型输入契约是 BGR HWC uint8；BGRA 先转 3 通道（机械类型转换）。
    cv::Mat minimapBgr = minimap;
    cv::Mat converted;
    if (minimapBgr.channels() == 4) {
        cv::cvtColor(minimapBgr, converted, cv::COLOR_BGRA2BGR);
        minimapBgr = converted;
    }

    // 参考分类器未加载 / 资产缺失或非 BGRA 时参考不可用：缺口记 -1，走观测分类器。
    const bool assetUsable =
        isRefModelLoaded_ && !referenceAsset.empty() && referenceAsset.channels() == 4 && referenceAsset.isContinuous();
    const cv::Mat& asset = assetUsable ? referenceAsset : kUnavailableAsset;

    try {
        auto memoryInfo = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);

        // 前处理图：几何、参考采样与合成、取整约定全部在图内，这里只零拷贝喂入。
        const std::array<int64_t, 4> minimapShape { 1, minimapBgr.rows, minimapBgr.cols, minimapBgr.channels() };
        const std::array<int64_t, 4> assetShape { 1, asset.rows, asset.cols, asset.channels() };
        float xValue = static_cast<float>(x);
        float yValue = static_cast<float>(y);
        float scaleValue = static_cast<float>(scale);
        Ort::Value preprocessInputs[] = {
            Ort::Value::CreateTensor<std::uint8_t>(
                memoryInfo,
                minimapBgr.data,
                minimapBgr.total() * minimapBgr.channels(),
                minimapShape.data(),
                minimapShape.size()),
            Ort::Value::CreateTensor<std::uint8_t>(
                memoryInfo,
                asset.data,
                asset.total() * asset.channels(),
                assetShape.data(),
                assetShape.size()),
            Ort::Value::CreateTensor<float>(memoryInfo, &xValue, 1, nullptr, 0),
            Ort::Value::CreateTensor<float>(memoryInfo, &yValue, 1, nullptr, 0),
            Ort::Value::CreateTensor<float>(memoryInfo, &scaleValue, 1, nullptr, 0),
        };
        auto strips = preprocessSession->Run(
            Ort::RunOptions { nullptr },
            kPreprocessInputNames,
            preprocessInputs,
            std::size(kPreprocessInputNames),
            kPreprocessOutputNames,
            std::size(kPreprocessOutputNames));
        if (strips.size() != 2) {
            LogError << "CameraOrientation: unexpected preprocess output count" << VAR(strips.size());
            return std::nullopt;
        }

        const auto observedInfo = strips[0].GetTensorTypeAndShapeInfo();
        const auto referenceInfo = strips[1].GetTensorTypeAndShapeInfo();
        const auto observedShape = observedInfo.GetShape();
        const auto referenceShape = referenceInfo.GetShape();
        if (observedShape.size() != 4 || referenceShape.size() != 4 || observedShape[1] != referenceShape[1]
            || observedShape[2] != referenceShape[2] || observedShape[3] != 3 || referenceShape[3] != 4
            || observedInfo.GetElementType() != ONNX_TENSOR_ELEMENT_DATA_TYPE_UINT8
            || referenceInfo.GetElementType() != ONNX_TENSOR_ELEMENT_DATA_TYPE_UINT8) {
            LogError << "CameraOrientation: unexpected preprocess output shape";
            return std::nullopt;
        }

        std::uint8_t* observedData = strips[0].GetTensorMutableData<std::uint8_t>();
        std::uint8_t* referenceData = strips[1].GetTensorMutableData<std::uint8_t>();
        const int64_t stripHeight = observedShape[1];
        const int64_t stripWidth = observedShape[2];
        const size_t stripPixels = static_cast<size_t>(stripHeight * stripWidth);

        // 缺口占比：参考条带 alpha（第 4 通道）< 255 的像素占比。
        double gapFraction = -1.0;
        if (assetUsable) {
            int64_t gapPixels = 0;
            for (size_t i = 0; i < stripPixels; ++i) {
                if (referenceData[i * 4 + 3] < 255) {
                    ++gapPixels;
                }
            }
            gapFraction = static_cast<double>(gapPixels) / static_cast<double>(stripPixels);
        }
        const bool usePolar = !assetUsable || gapFraction > kReferenceGapDispatchThreshold;
        const char* modelSource = usePolar ? "polar" : "polar_with_ref";
        LogInfo << "CameraOrientation dispatch:" << VAR(zoneId) << VAR(x) << VAR(y) << VAR(scale) << VAR(gapFraction) << VAR(modelSource);

        // 分类器输入：polar 直接零拷贝用观测条带；ref 拼接 [obs.BGR, ref.BGR, ref.A]。
        std::uint8_t* classifierData = observedData;
        int classifierChannels = 3;
        if (!usePolar) {
            refInputScratch.create(static_cast<int>(stripHeight), static_cast<int>(stripWidth), CV_MAKETYPE(CV_8U, 7));
            cv::Mat observedMat(static_cast<int>(stripHeight), static_cast<int>(stripWidth), CV_8UC3, observedData);
            cv::Mat referenceMat(static_cast<int>(stripHeight), static_cast<int>(stripWidth), CV_8UC4, referenceData);
            cv::Mat sources[] = { observedMat, referenceMat };
            // 源通道跨矩阵连续编号：[0,3) 观测 BGR、[3,7) 参考 BGR + alpha，因此恒等映射。
            const int fromTo[] = { 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6 };
            cv::mixChannels(sources, std::size(sources), &refInputScratch, 1, fromTo, 7);
            classifierData = refInputScratch.ptr<std::uint8_t>();
            classifierChannels = 7;
        }

        Ort::Session* classifierSession = usePolar ? polarSession.get() : refSession.get();
        if (!classifierSession) {
            LogError << "CameraOrientation: classifier unavailable for dispatch" << VAR(zoneId) << VAR(gapFraction) << VAR(modelSource);
            return std::nullopt;
        }

        const std::array<int64_t, 4> classifierShape { 1, stripHeight, stripWidth, classifierChannels };
        Ort::Value classifierInput = Ort::Value::CreateTensor<std::uint8_t>(
            memoryInfo,
            classifierData,
            stripPixels * classifierChannels,
            classifierShape.data(),
            classifierShape.size());
        const char* classifierInputNames[] = { kClassifierInputName };
        const char* classifierOutputNames[] = { kClassifierOutputName };
        auto outputTensors =
            classifierSession->Run(Ort::RunOptions { nullptr }, classifierInputNames, &classifierInput, 1, classifierOutputNames, 1);
        if (outputTensors.empty()) {
            LogError << "CameraOrientation: empty inference output.";
            return std::nullopt;
        }

        const float* pmf = outputTensors.front().GetTensorData<float>();
        const size_t count = outputTensors.front().GetTensorTypeAndShapeInfo().GetElementCount();
        return decodePmf(pmf, count);
    }
    catch (const Ort::Exception& e) {
        LogError << "CameraOrientation: inference failed" << VAR(zoneId) << VAR(e.what());
        return std::nullopt;
    }
}

std::optional<CameraOrientation> CameraOrientationPredictor::decodePmf(const float* pmf, size_t count) const
{
    if (pmf == nullptr || count == 0) {
        LogError << "CameraOrientation: unexpected pmf size" << VAR(count);
        return std::nullopt;
    }

    // 分类器契约是均匀方位 bin（列 j = 方位角 j 度）；bin 数从输出张量读出，不复刻条带几何。
    const double radianPerBin = 2.0 * std::numbers::pi / static_cast<double>(count);

    // 全 bin 方向向量（方向 = bin 方位角，长度 = 概率）合成，用于置信度。
    double resultantSin = 0.0;
    double resultantCos = 0.0;
    for (size_t j = 0; j < count; ++j) {
        const double theta = static_cast<double>(j) * radianPerBin;
        resultantSin += pmf[j] * std::sin(theta);
        resultantCos += pmf[j] * std::cos(theta);
    }

    size_t center = 0;
    for (size_t j = 1; j < count; ++j) {
        if (pmf[j] > pmf[center]) {
            center = j;
        }
    }

    // argmax ±kRefineRadius 窗口内按 pmf 加权圆均值；接缝两侧靠取模跨 0/360。
    double windowSin = 0.0;
    double windowCos = 0.0;
    for (int offset = -kRefineRadius; offset <= kRefineRadius; ++offset) {
        const long long col = (static_cast<long long>(center) + offset + static_cast<long long>(count)) % static_cast<long long>(count);
        const double theta = static_cast<double>(col) * radianPerBin;
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
