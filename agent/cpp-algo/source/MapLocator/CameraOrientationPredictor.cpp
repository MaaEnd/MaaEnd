#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>
#include <numbers>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#include "CameraOrientationPredictor.h"

namespace maplocator
{

namespace
{
// 以下几何与解码约定均与 cameraorientation.onnx / cao_ref.onnx 绑定；改动任何
// 一项都必须与模型同步重新导出，不得单方面修改。

// 极坐标条带：每列 1°，列 0 = 正北，顺时针；行 0 = 内径，行 i 采样半径
// 为 kInnerRadius + (i + 0.5) * step，step = (kOuterRadius - kInnerRadius) / kStripHeight。
constexpr int kStripWidth = 360;
constexpr int kStripHeight = 42;
constexpr double kInnerRadius = 12.0;
constexpr double kOuterRadius = 54.0;
// argmax 定峰后 ±5° 窗口内按 pmf 加权圆均值，得到亚度级解码角。
constexpr int kRefineRadius = 5;

// 参考裁剪的目标几何：与 720p 基准的观测小地图同视野、同尺度。
constexpr int kReferenceRoiWidth = 118;
constexpr int kReferenceRoiHeight = 120;
// 展开列角度的 float32 步长 π/180，半径与角度全程 float32，与模型输入约定一致。
constexpr float kDegreeToRadian = 0.017453292f;
// 参考 alpha 展开环内缺失占比分派阈值：严格大于走 polar 观测模型，否则走 ref
// 参考配对模型。0.3 与 cao_ref.onnx 的输入约定绑定，不可单方面修改。
constexpr double kReferenceGapDispatchThreshold = 0.3;

// 节点名是导出时定死的图属性；两个模型共用同一输入/输出名。
constexpr const char* kInputName = "strip";
constexpr const char* kOutputName = "pmf";

// Python round() 与 OpenCV saturate_cast 均为半偶舍入；std::nearbyint 默认舍入模式
// 也是 to-nearest-even。不要用 std::lround（半远离零）。
int RoundHalfEven(double value)
{
    return static_cast<int>(std::nearbyint(value));
}

// 极坐标展开的精确双线性采样：模型输入约定为精确双线性——x0 = floor(x)、
// y0 = floor(y)、fx = x - x0、fy = y - y0，v = p00*(1-fx)*(1-fy) + p01*fx*(1-fy)
// + p10*(1-fx)*fy + p11*fx*fy，权重与累加全程 float32、按该顺序求值且禁止 FMA
// 合并，最后半偶舍入回 uint8。OpenCV 4.x 的 cv::remap(INTER_LINEAR) 对 8U 走
// 1/32 定点权重，同一坐标最多差 4/255，无法满足逐点对齐；这里按约定公式自行
// 采样。采样点始终落在圆盘及其一圈邻居内，越界索引只做边界复制兜底。
#if defined(__clang__)
#pragma clang fp contract(off)
#elif defined(_MSC_VER)
#pragma fp_contract(off)
#endif
void RemapStrip(const cv::Mat& source, cv::Mat& dst, const cv::Mat& mapX, const cv::Mat& mapY)
{
    const int channels = source.channels();
    dst.create(kStripHeight, kStripWidth, source.type());
    const int maxX = source.cols - 1;
    const int maxY = source.rows - 1;
    for (int row = 0; row < kStripHeight; ++row) {
        const float* mapXRow = mapX.ptr<float>(row);
        const float* mapYRow = mapY.ptr<float>(row);
        std::uint8_t* dstRow = dst.ptr<std::uint8_t>(row);
        for (int col = 0; col < kStripWidth; ++col) {
            const float x = mapXRow[col];
            const float y = mapYRow[col];
            const float floorX = std::floor(x);
            const float floorY = std::floor(y);
            const int x0 = std::clamp(static_cast<int>(floorX), 0, maxX);
            const int y0 = std::clamp(static_cast<int>(floorY), 0, maxY);
            const int x1 = std::min(x0 + 1, maxX);
            const int y1 = std::min(y0 + 1, maxY);
            const float fx = x - floorX;
            const float fy = y - floorY;
            const float weight00 = (1.0f - fx) * (1.0f - fy);
            const float weight01 = fx * (1.0f - fy);
            const float weight10 = (1.0f - fx) * fy;
            const float weight11 = fx * fy;
            const std::uint8_t* row0 = source.ptr<std::uint8_t>(y0);
            const std::uint8_t* row1 = source.ptr<std::uint8_t>(y1);
            const std::uint8_t* p00 = row0 + x0 * channels;
            const std::uint8_t* p01 = row0 + x1 * channels;
            const std::uint8_t* p10 = row1 + x0 * channels;
            const std::uint8_t* p11 = row1 + x1 * channels;
            for (int channel = 0; channel < channels; ++channel) {
                const float value = static_cast<float>(p00[channel]) * weight00 + static_cast<float>(p01[channel]) * weight01
                                    + static_cast<float>(p10[channel]) * weight10 + static_cast<float>(p11[channel]) * weight11;
                dstRow[col * channels + channel] = cv::saturate_cast<std::uint8_t>(value);
            }
        }
    }
}
#if defined(__clang__)
#pragma clang fp contract(on)
#elif defined(_MSC_VER)
#pragma fp_contract(on)
#endif

} // namespace

CameraOrientationPredictor::CameraOrientationPredictor(const std::string& polarModelPath, const std::string& refModelPath, int threads)
{
    if (polarModelPath.empty() && refModelPath.empty()) {
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

    isPolarModelLoaded_ = loadSession(polarModelPath, "polar", sessionOptions, &polarSession);
    isRefModelLoaded_ = loadSession(refModelPath, "ref", sessionOptions, &refSession);

    if (!isPolarModelLoaded_ && !isRefModelLoaded_) {
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

    if (!isLoaded()) {
        LogError << "CameraOrientation Error: Model is NOT loaded.";
        return std::nullopt;
    }
    if (minimap.empty()) {
        LogError << "CameraOrientation Error: Input minimap is empty.";
        return std::nullopt;
    }
    if (!prepareRingGeometry(minimap)) {
        return std::nullopt;
    }

    // 模型输入契约是 BGR HWC uint8；BGRA 先转 3 通道再采样，避免 4 通道双线性
    // 插值的无谓开销。
    cv::Mat source = minimap;
    cv::Mat converted;
    if (source.channels() == 4) {
        cv::cvtColor(source, converted, cv::COLOR_BGRA2BGR);
        source = converted;
    }
    buildObservedStrip(source);

    // 参考裁剪几何按 720p 基准的 118x120 观测 ROI 定义；minimap 尺寸不符时无法对齐参考。
    const bool referenceUsable = isRefModelLoaded_ && !referenceAsset.empty() && referenceAsset.channels() == 4
                                 && minimap.cols == kReferenceRoiWidth && minimap.rows == kReferenceRoiHeight;
    // 参考模型未加载、参考资产缺失或尺寸不符时全部走 polar。
    double gapFraction = -1.0; // < 0 表示参考不可用、未计算
    if (referenceUsable) {
        buildReferenceInput(referenceAsset, source, x, y, scale, &gapFraction);
    }
    const bool usePolar = !referenceUsable || gapFraction > kReferenceGapDispatchThreshold;
    const char* modelSource = usePolar ? "polar" : "ref";
    LogInfo << "CameraOrientation dispatch:" << VAR(zoneId) << VAR(x) << VAR(y) << VAR(scale) << VAR(gapFraction) << VAR(modelSource);

    Ort::Session* session = nullptr;
    cv::Mat* inputMat = nullptr;
    int inputChannels = 0;
    if (usePolar) {
        if (!isPolarModelLoaded_ || !polarSession) {
            LogError << "CameraOrientation: polar model unavailable for dispatch" << VAR(zoneId) << VAR(gapFraction);
            return std::nullopt;
        }
        session = polarSession.get();
        inputMat = &stripScratch;
        inputChannels = 3;
    }
    else {
        if (!refSession) {
            LogError << "CameraOrientation: ref model unavailable for dispatch" << VAR(zoneId) << VAR(gapFraction);
            return std::nullopt;
        }
        assembleRefInput();
        session = refSession.get();
        inputMat = &refInputScratch;
        inputChannels = 7;
    }

    // /255 已折入模型首层卷积权重，输入保持 0..255 数值域；展开输出总是连续内存。
    const std::array<int64_t, 4> inputShape { 1, kStripHeight, kStripWidth, inputChannels };
    auto memoryInfo = Ort::MemoryInfo::CreateCpu(OrtArenaAllocator, OrtMemTypeDefault);
    Ort::Value inputTensor = Ort::Value::CreateTensor<std::uint8_t>(
        memoryInfo,
        inputMat->data,
        static_cast<size_t>(kStripHeight) * kStripWidth * inputChannels,
        inputShape.data(),
        inputShape.size());

    const char* inputNames[] = { kInputName };
    const char* outputNames[] = { kOutputName };
    auto outputTensors = session->Run(Ort::RunOptions { nullptr }, inputNames, &inputTensor, 1, outputNames, 1);
    if (outputTensors.empty()) {
        LogError << "CameraOrientation: empty inference output.";
        return std::nullopt;
    }

    const float* pmf = outputTensors.front().GetTensorData<float>();
    const size_t count = outputTensors.front().GetTensorTypeAndShapeInfo().GetElementCount();
    return decodePmf(pmf, count);
}

bool CameraOrientationPredictor::prepareRingGeometry(const cv::Mat& minimap)
{
    // 极点为小地图几何中心（720p 基准下即全图坐标 (108, 111)）。
    const double cx = minimap.cols / 2.0;
    const double cy = minimap.rows / 2.0;
    // 双线性插值需要在源图内取到邻居像素：要求整个圆盘加一圈邻居都在图内。
    if (kOuterRadius + 1.0 > cx || kOuterRadius + 1.0 > cy || cx > minimap.cols - kOuterRadius - 1.0
        || cy > minimap.rows - kOuterRadius - 1.0) {
        LogError << "CameraOrientation: minimap too small for the orientation ring" << VAR(minimap.cols) << VAR(minimap.rows);
        return false;
    }

    // 源坐标映射只依赖小地图尺寸，按尺寸缓存：
    // src_x = cx + r * sin(theta)，src_y = cy - r * cos(theta)，theta = 列序号（度）。
    // 半径与角度全程 float32，与模型输入约定的数值路径一致。
    mapCenterX = static_cast<float>(cx);
    mapCenterY = static_cast<float>(cy);
    if (mapXScratch.size() != minimap.size()) {
        mapXScratch.create(kStripHeight, kStripWidth, CV_32FC1);
        mapYScratch.create(kStripHeight, kStripWidth, CV_32FC1);
        const float step = static_cast<float>((kOuterRadius - kInnerRadius) / kStripHeight);
        for (int i = 0; i < kStripHeight; ++i) {
            const float radius = static_cast<float>(kInnerRadius) + step * (static_cast<float>(i) + 0.5f);
            for (int j = 0; j < kStripWidth; ++j) {
                const float theta = static_cast<float>(j) * kDegreeToRadian;
                const float offsetX = radius * std::sin(theta);
                const float offsetY = radius * std::cos(theta);
                mapXScratch.at<float>(i, j) = mapCenterX + offsetX;
                mapYScratch.at<float>(i, j) = mapCenterY - offsetY;
            }
        }
    }
    return true;
}

void CameraOrientationPredictor::buildObservedStrip(const cv::Mat& observedBgr)
{
    RemapStrip(observedBgr, stripScratch, mapXScratch, mapYScratch);
}

void CameraOrientationPredictor::buildReferenceInput(
    const cv::Mat& referenceAsset,
    const cv::Mat& observedBgr,
    double x,
    double y,
    double scale,
    double* out_gap)
{
    // zone 尺度比非 1 时（如 ValleyIV_Base 15/16）底图相对观测整体缩放：裁 ROI*scale
    // 的资产窗口再缩回 ROI，与观测同视野。尺寸按半偶舍入取整（120*15/16=112.5 -> 112）。
    const int cropWidth = std::max(1, RoundHalfEven(kReferenceRoiWidth * scale));
    const int cropHeight = std::max(1, RoundHalfEven(kReferenceRoiHeight * scale));

    // 以定位结果 (x, y) 为裁剪中心；越界外侧 BGR 与 alpha 同为 0（参考缺失）。
    referenceCropScratch.create(cropHeight, cropWidth, CV_8UC3);
    referenceCropScratch.setTo(0);
    referenceAlphaScratch.create(cropHeight, cropWidth, CV_8UC1);
    referenceAlphaScratch.setTo(0);

    const int originX = RoundHalfEven(x) - cropWidth / 2;
    const int originY = RoundHalfEven(y) - cropHeight / 2;
    for (int row = 0; row < cropHeight; ++row) {
        const int assetY = originY + row;
        if (assetY < 0 || assetY >= referenceAsset.rows) {
            continue;
        }
        cv::Vec3b* cropRow = referenceCropScratch.ptr<cv::Vec3b>(row);
        std::uint8_t* alphaRow = referenceAlphaScratch.ptr<std::uint8_t>(row);
        const cv::Vec4b* assetRow = referenceAsset.ptr<cv::Vec4b>(assetY);
        for (int col = 0; col < cropWidth; ++col) {
            const int assetX = originX + col;
            if (assetX < 0 || assetX >= referenceAsset.cols) {
                continue;
            }
            const cv::Vec4b& pixel = assetRow[assetX];
            // 黑底合成：round(rgb * a/255)，alpha 保持原始连续值，不二值化。
            const float alpha = static_cast<float>(pixel[3]) / 255.0f;
            for (int channel = 0; channel < 3; ++channel) {
                const float black = static_cast<float>(pixel[channel]) * alpha;
                cropRow[col][channel] = cv::saturate_cast<std::uint8_t>(black);
            }
            alphaRow[col] = pixel[3];
        }
    }

    if (scale != 1.0) {
        cv::resize(referenceCropScratch, referenceCropScratch, cv::Size(kReferenceRoiWidth, kReferenceRoiHeight), 0, 0, cv::INTER_LINEAR);
        cv::resize(referenceAlphaScratch, referenceAlphaScratch, cv::Size(kReferenceRoiWidth, kReferenceRoiHeight), 0, 0, cv::INTER_LINEAR);
    }

    // 观测背底合成：ref.BGR = black_ref + obs*(1 - alpha/255)，alpha==0 处逐像素
    // 等于观测、alpha==255 处等于黑底合成。float32 计算 + 半偶舍入 + 饱和，不回绕。
    for (int row = 0; row < kReferenceRoiHeight; ++row) {
        cv::Vec3b* cropRow = referenceCropScratch.ptr<cv::Vec3b>(row);
        const cv::Vec3b* observedRow = observedBgr.ptr<cv::Vec3b>(row);
        const std::uint8_t* alphaRow = referenceAlphaScratch.ptr<std::uint8_t>(row);
        for (int col = 0; col < kReferenceRoiWidth; ++col) {
            const float weight = 1.0f - static_cast<float>(alphaRow[col]) / 255.0f;
            for (int channel = 0; channel < 3; ++channel) {
                const float blend = static_cast<float>(observedRow[col][channel]) * weight;
                const float composed = static_cast<float>(cropRow[col][channel]) + blend;
                cropRow[col][channel] = cv::saturate_cast<std::uint8_t>(composed);
            }
        }
    }

    // 展开与观测条带同一几何：极点 = ROI 中心。
    RemapStrip(referenceCropScratch, referenceStripScratch, mapXScratch, mapYScratch);
    RemapStrip(referenceAlphaScratch, referenceAlphaStripScratch, mapXScratch, mapYScratch);

    // 缺口占比 = 展开 alpha 中 < 255 的像素占比（42x360 各半径等权）。
    int gapPixels = 0;
    for (int row = 0; row < kStripHeight; ++row) {
        const std::uint8_t* alphaRow = referenceAlphaStripScratch.ptr<std::uint8_t>(row);
        for (int col = 0; col < kStripWidth; ++col) {
            if (alphaRow[col] < 255) {
                ++gapPixels;
            }
        }
    }
    *out_gap = static_cast<double>(gapPixels) / (kStripHeight * kStripWidth);
}

void CameraOrientationPredictor::assembleRefInput()
{
    // 7 通道 NHWC `[obs.BGR, ref.BGR, ref.A]`，与模型输入契约一致。
    // mixChannels 的源通道号跨所有源矩阵连续编号：[0,3) 观测、[3,6) 参考 BGR、
    // [6,7) 参考 alpha，因此 fromTo 是全局恒等映射。
    refInputScratch.create(kStripHeight, kStripWidth, CV_MAKETYPE(CV_8U, 7));
    cv::Mat sources[] = { stripScratch, referenceStripScratch, referenceAlphaStripScratch };
    const int fromTo[] = { 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6 };
    cv::mixChannels(sources, 3, &refInputScratch, 1, fromTo, 7);
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
