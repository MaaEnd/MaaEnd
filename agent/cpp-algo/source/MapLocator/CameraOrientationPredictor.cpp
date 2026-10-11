#include "CameraOrientationPredictor.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>
#include <iterator>
#include <numbers>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

namespace maplocator
{

namespace
{
// 交付工件定死的输入/输出名；改动即契约变更。
constexpr const char* kClassifierInputName = "strip";
constexpr const char* kClassifierOutputName = "pmf";

// PMF 解码的定峰窗口半径（bin）：argmax 后在该窗口内按概率加权求圆均值。
constexpr int kRefineRadius = 5;
// 候选峰的两条门限只用来把噪声排除在候选之外，不做「次峰要够高才算数」的判断：正确识别的非主峰
// 信号低于 0.001，而真双峰的峰高差距可以很极端。典型峰高为 [0.1, 0.2)，5% 相对门限落在 0.005~0.01：
// 低于主峰 5% 的次模（例如主峰 0.15、次峰 0.006）按现行门限不算合格次峰，先验在这类帧上不启用。
constexpr float kMinPeakHeight = 0.005f;
constexpr float kSecondaryPeakRatio = 0.05f;

// 参考资产缺失或非 BGRA 时的占位输入：1x1 全 0 BGRA。采样窗不可能落在这块资产里，
// 参考条带据此全为「参考缺失」。
const cv::Mat kUnavailableAsset(1, 1, CV_8UC4, cv::Scalar::all(0));
} // namespace

CameraOrientationPredictor::CameraOrientationPredictor(const std::string& refModelPath, int threads)
{
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

    if (!loadSession(refModelPath, "polar_with_ref", sessionOptions, &refSession)) {
        LogError << "CameraOrientation: predictor disabled";
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
    const std::string& zoneId,
    std::optional<double> camera_heading_prior)
{
    const bool assetUsable = !referenceAsset.empty() && referenceAsset.type() == CV_8UC4;
    const cv::Mat& asset = assetUsable ? referenceAsset : kUnavailableAsset;
    return infer(minimap, asset, x, y, scale, zoneId, camera_heading_prior);
}

std::optional<CameraOrientation> CameraOrientationPredictor::infer(
    const cv::Mat& minimap,
    const cv::Mat& asset,
    double x,
    double y,
    double scale,
    const std::string& zoneId,
    std::optional<double> camera_heading_prior)
{
    std::lock_guard<std::mutex> lock(predictMutex);

    if (!isLoaded()) {
        LogError << "CameraOrientation Error: Model is NOT loaded.";
        return std::nullopt;
    }
    if (minimap.empty() || (minimap.channels() != 3 && minimap.channels() != 4)) {
        LogError << "CameraOrientation Error: invalid minimap input" << VAR(minimap.cols) << VAR(minimap.rows) << VAR(minimap.channels());
        return std::nullopt;
    }

    // 预处理输入契约是 BGR HWC uint8；BGRA 先转 3 通道（机械类型转换）。
    cv::Mat minimapBgr = minimap;
    cv::Mat converted;
    if (minimapBgr.channels() == 4) {
        cv::cvtColor(minimapBgr, converted, cv::COLOR_BGRA2BGR);
        minimapBgr = converted;
    }

    // 几何、参考采样与合成、取整约定与预处理定义逐字节一致（见 CameraOrientationPreprocess.h）。
    if (!BuildOrientationStrips(minimapBgr, asset, static_cast<float>(x), static_cast<float>(y), static_cast<float>(scale), stripScratch)) {
        LogError << "CameraOrientation: invalid preprocess input" << VAR(minimapBgr.cols) << VAR(minimapBgr.rows) << VAR(asset.cols)
                 << VAR(asset.rows) << VAR(x) << VAR(y) << VAR(scale);
        return std::nullopt;
    }

    try {
        auto memoryInfo = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);

        const std::uint8_t* referenceData = stripScratch.reference.ptr<std::uint8_t>();
        const size_t stripPixels = stripScratch.reference.total();

        // 缺口占比：参考条带 alpha（第 4 通道）< 255 的像素占比，仅作诊断日志。
        int64_t gapPixels = 0;
        for (size_t i = 0; i < stripPixels; ++i) {
            if (referenceData[i * 4 + 3] < 255) {
                ++gapPixels;
            }
        }
        const double gapFraction = static_cast<double>(gapPixels) / static_cast<double>(stripPixels);
        LogInfo << "CameraOrientation ref:" << VAR(zoneId) << VAR(x) << VAR(y) << VAR(scale) << VAR(gapFraction);

        // 分类器输入固定为 7 通道参考配对 [obs.BGR, ref.BGR, ref.A]。
        refInputScratch.create(kOrientationStripHeight, kOrientationStripWidth, CV_MAKETYPE(CV_8U, 7));
        const cv::Mat sources[] = { stripScratch.observed, stripScratch.reference };
        // 源通道跨矩阵连续编号：[0,3) 观测 BGR、[3,7) 参考 BGR + alpha，因此恒等映射。
        const int fromTo[] = { 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6 };
        cv::mixChannels(sources, std::size(sources), &refInputScratch, 1, fromTo, 7);
        std::uint8_t* classifierData = refInputScratch.ptr<std::uint8_t>();
        const int classifierChannels = 7;

        const std::array<int64_t, 4> classifierShape { 1, kOrientationStripHeight, kOrientationStripWidth, classifierChannels };
        Ort::Value classifierInput = Ort::Value::CreateTensor<std::uint8_t>(
            memoryInfo,
            classifierData,
            stripPixels * classifierChannels,
            classifierShape.data(),
            classifierShape.size());
        const char* classifierInputNames[] = { kClassifierInputName };
        const char* classifierOutputNames[] = { kClassifierOutputName };
        auto outputTensors =
            refSession->Run(Ort::RunOptions { nullptr }, classifierInputNames, &classifierInput, 1, classifierOutputNames, 1);
        if (outputTensors.empty()) {
            LogError << "CameraOrientation: empty inference output.";
            return std::nullopt;
        }

        const float* pmf = outputTensors.front().GetTensorData<float>();
        const size_t count = outputTensors.front().GetTensorTypeAndShapeInfo().GetElementCount();
        return decodePmf(pmf, count, camera_heading_prior);
    }
    catch (const Ort::Exception& e) {
        LogError << "CameraOrientation: inference failed" << VAR(zoneId) << VAR(e.what());
        return std::nullopt;
    }
}

std::optional<CameraOrientation>
    CameraOrientationPredictor::decodePmf(const float* pmf, size_t count, std::optional<double> camera_heading_prior) const
{
    if (pmf == nullptr || count == 0) {
        LogError << "CameraOrientation: unexpected pmf size" << VAR(count);
        return std::nullopt;
    }

    // 分类器契约是均匀方位 bin（列 j = 方位角 j 度）；bin 数从输出张量读出，不复刻条带几何。
    const double radianPerBin = 2.0 * std::numbers::pi / static_cast<double>(count);
    if (std::any_of(pmf, pmf + count, [](float value) { return !std::isfinite(value) || value < 0.0f; })) {
        LogError << "CameraOrientation: invalid pmf values";
        return std::nullopt;
    }

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

    const size_t primary_center = center;
    bool used_prior = false;
    if (camera_heading_prior && std::isfinite(*camera_heading_prior)) {
        const double prior = std::fmod(std::fmod(*camera_heading_prior, 360.0) + 360.0, 360.0);
        const double prior_bin = prior * static_cast<double>(count) / 360.0;
        const auto circular_distance = [count](double a, double b) {
            const double distance = std::abs(a - b);
            return std::min(distance, static_cast<double>(count) - distance);
        };
        size_t prior_peak = primary_center;
        double nearest_distance = static_cast<double>(kRefineRadius) + 1.0;
        bool has_prior_peak = false;
        bool has_secondary_peak = false;
        for (size_t j = 0; j < count; ++j) {
            // 绝对和相对高度都检查峰顶，不检查先验角度处的信号。
            if (pmf[j] < kMinPeakHeight || pmf[j] < pmf[primary_center] * kSecondaryPeakRatio) {
                continue;
            }
            const double primary_distance = circular_distance(static_cast<double>(j), static_cast<double>(primary_center));
            if (j != primary_center && primary_distance <= 2 * kRefineRadius) {
                continue;
            }
            // 相邻肩部不重复计峰；平台只取上升沿，跨接缝同样按圆周判断。
            if (pmf[j] <= pmf[(j + count - 1) % count] || pmf[j] < pmf[(j + 1) % count]) {
                continue;
            }
            bool local_maximum = true;
            for (int offset = -kRefineRadius; offset <= kRefineRadius; ++offset) {
                const size_t col = static_cast<size_t>(
                    (static_cast<long long>(j) + offset % static_cast<long long>(count) + static_cast<long long>(count))
                    % static_cast<long long>(count));
                if (pmf[col] > pmf[j]) {
                    local_maximum = false;
                    break;
                }
            }
            if (!local_maximum) {
                continue;
            }
            has_secondary_peak = has_secondary_peak || primary_distance > 2 * kRefineRadius;
            // 仅关联现有精修窗口范围内的峰，不能将远处的“最近峰”当作先验方向的峰。
            const double distance = circular_distance(static_cast<double>(j), prior_bin);
            if (distance <= kRefineRadius && distance < nearest_distance) {
                prior_peak = j;
                nearest_distance = distance;
                has_prior_peak = true;
            }
        }
        // 次峰间隔没有保证，故不设对跖前提：只要求先验落在某个合格峰的精修窗内。
        if (has_secondary_peak && has_prior_peak) {
            center = prior_peak;
            used_prior = true;
        }
    }

    // 所选峰 ±kRefineRadius 窗口内按 pmf 加权圆均值；接缝两侧靠取模跨 0/360。
    double windowSin = 0.0;
    double windowCos = 0.0;
    double window_mass = 0.0;
    for (int offset = -kRefineRadius; offset <= kRefineRadius; ++offset) {
        const long long col = (static_cast<long long>(center) + offset % static_cast<long long>(count) + static_cast<long long>(count))
                              % static_cast<long long>(count);
        const double theta = static_cast<double>(col) * radianPerBin;
        windowSin += pmf[col] * std::sin(theta);
        windowCos += pmf[col] * std::cos(theta);
        window_mass += pmf[col];
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
    // 先验已在真实双峰间消歧时，置信度衡量所选峰内部的集中程度，避免另一峰把它抵消。
    const double confidence = used_prior && window_mass > 0.0 ? std::clamp(std::hypot(windowSin, windowCos) / window_mass, 0.0, 1.0)
                                                              : std::clamp(resultantLength * alignmentCos, 0.0, 1.0);
    if (!used_prior && alignmentCos < 0.0) {
        LogWarn << "CameraOrientation: decoded direction diverges from resultant" << VAR(decoded) << VAR(resultantAngle);
    }

    LogTrace << "CameraOrientation pmf:" << std::vector<float>(pmf, pmf + count);
    LogDebug << "CameraOrientation:" << VAR(decoded) << VAR(confidence) << VAR(center) << VAR(primary_center) << VAR(used_prior)
             << VAR(camera_heading_prior);

    return CameraOrientation { .rot = decoded, .confidence = confidence };
}

} // namespace maplocator
