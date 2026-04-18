#include <algorithm>
#include <exception>
#include <filesystem>
#include <format>
#include <future>
#include <mutex>
#include <thread>
#include <vector>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <boost/regex.hpp>
#include <meojson/json.hpp>

#include "MapAlgorithm.h"
#include "MapLocator.h"
#include "MatchStrategy.h"
#include "MotionTracker.h"
#include "YoloPredictor.h"

using Json = json::value;

namespace fs = std::filesystem;

namespace maplocator
{

namespace
{

std::string TrimLeadingZeros(std::string value)
{
    value.erase(0, std::min(value.find_first_not_of('0'), value.size() - 1));
    return value;
}

bool MatchesExpectedZoneSelector(const std::string& expected_zone_selector, const YoloCoarseResult& coarse)
{
    if (expected_zone_selector.empty()) {
        return true;
    }
    if (coarse.zone_id == expected_zone_selector || coarse.base_class == expected_zone_selector
        || coarse.raw_class == expected_zone_selector) {
        return true;
    }
    return coarse.raw_class.starts_with(expected_zone_selector);
}

std::string NormalizeExpectedZoneId(const std::string& expected_zone_selector, YoloPredictor* predictor)
{
    if (expected_zone_selector.empty() || predictor == nullptr) {
        return expected_zone_selector;
    }
    return predictor->convertYoloNameToZoneId(expected_zone_selector);
}

struct FineScaleSearchResult
{
    double scale = 1.0;
    bool hasRawResult = false;
    MatchResultRaw fineRes;
    cv::Mat scaledTempl;
};

struct GlobalSearchAttempt
{
    std::optional<MapPosition> result;
    MapPosition rawPos {};
};

using TimePoint = std::chrono::steady_clock::time_point;

struct SearchExecutionContext
{
    const MatchFeature& tmplFeat;
    IMatchStrategy* strategy = nullptr;
    const cv::Mat& bigMap;
    cv::Rect constrainedRect {};
    const std::string& targetZoneId;
    MapPosition* outRawPos = nullptr;
};

bool IsTightCluster(const std::vector<MapPosition>& buf, double radius)
{
    if (buf.size() < static_cast<size_t>(kColdStartConsensusFrames)) {
        return false;
    }
    const auto& ref = buf.back();
    return std::all_of(buf.end() - kColdStartConsensusFrames, buf.end(), [&](const MapPosition& p) {
        return std::hypot(p.x - ref.x, p.y - ref.y) <= radius;
    });
}

} // namespace

class MapLocator::Impl
{
public:
    Impl() = default;

    ~Impl()
    {
        if (asyncYoloTask.valid()) {
            asyncYoloTask.wait();
        }
        drainBackgroundGlobalSearchTasks();
    }

    bool initialize(const MapLocatorConfig& cfg);

    bool getIsInitialized() const { return isInitialized; }

    LocateResult locate(const cv::Mat& minimap, const LocateOptions& options);
    YoloCoarseResult predictCoarse(const cv::Mat& minimap) const;
    void resetTrackingState();
    std::optional<MapPosition> getLastKnownPos() const;

private:
    std::optional<MapPosition> tryTracking(
        const MatchFeature& tmplFeat,
        IMatchStrategy* strategy,
        TimePoint now,
        const LocateOptions& options,
        MapPosition* outRawPos = nullptr);

    std::optional<MapPosition> tryGlobalSearch(
        const MatchFeature& tmplFeat,
        IMatchStrategy* strategy,
        const std::string& targetZoneId,
        const SearchConstraint& constraint = {},
        MapPosition* outRawPos = nullptr);

    std::optional<MapPosition> evaluateAndAcceptResult(
        const MatchResultRaw& fineRes,
        const cv::Rect& validFineRect,
        const cv::Mat& templ,
        IMatchStrategy* strategy,
        const std::string& targetZoneId);
    std::optional<MapPosition> tryConstrainedFineSearch(const SearchExecutionContext& ctx);
    std::optional<MapPosition> tryLegacyCoarseSearch(const SearchExecutionContext& ctx);

    void refreshAsyncYoloState(const cv::Mat& minimap, TimePoint now);
    std::optional<LocateResult>
        tryTrackingLocate(const cv::Mat& minimap, const LocateOptions& options, const std::string& expectedZoneId, TimePoint now);
    SearchConstraint buildSearchConstraint(
        const std::string& expectedZoneSelector,
        const std::string& targetZoneId,
        const YoloCoarseResult& coarse) const;
    std::optional<MapPosition>
        tryGlobalSearchWithFallback(const cv::Mat& minimap, const std::string& targetZoneId, const SearchConstraint& constraint);
    void drainBackgroundGlobalSearchTasks();

    void loadAvailableZones(const std::string& root);

    bool isInitialized = false;
    MapLocatorConfig config;

    std::map<std::string, cv::Mat> zones;
    std::string currentZoneId;

    std::unique_ptr<MotionTracker> motionTracker;
    std::unique_ptr<YoloPredictor> zoneClassifier;
    std::mutex taskMutex;
    std::future<YoloCoarseResult> asyncYoloTask;
    std::vector<std::future<GlobalSearchAttempt>> backgroundGlobalSearchTasks;
    std::chrono::steady_clock::time_point lastYoloCheckTime;

    std::vector<MapPosition> coldStartBuffer;

    TrackingConfig trackingCfg;
    MatchConfig matchCfg;
    ImageProcessingConfig baseImgCfg = { .darkMapThreshold = 20.0,
                                         .iconDiffThreshold = 40,
                                         .centerMaskRadius = 18,
                                         .gradientBaseWeight = 0.1,
                                         .minimapDarkMaskThreshold = 20,
                                         .borderMargin = 10,
                                         .whiteDilate = 11,
                                         .colorDilate = 3,
                                         .useHsvWhiteMask = true };

    ImageProcessingConfig tierImgCfg = { .darkMapThreshold = 20.0,
                                         .iconDiffThreshold = 40,
                                         .centerMaskRadius = 8,
                                         .gradientBaseWeight = 0.1,
                                         .minimapDarkMaskThreshold = 15,
                                         .borderMargin = 8,
                                         .whiteDilate = 9,
                                         .colorDilate = 3,
                                         .useHsvWhiteMask = false };
};

bool MapLocator::Impl::initialize(const MapLocatorConfig& cfg)
{
    if (isInitialized) {
        return true;
    }
    config = cfg;

    motionTracker = std::make_unique<MotionTracker>(trackingCfg);
    loadAvailableZones(config.mapResourceDir);

    if (!config.yoloModelPath.empty()) {
        zoneClassifier = std::make_unique<YoloPredictor>(config.yoloModelPath, matchCfg.yoloConfThreshold, config.yoloThreads);
    }

    isInitialized = true;
    return true;
}

void MapLocator::Impl::loadAvailableZones(const std::string& root)
{
    if (!fs::exists(MAA_NS::path(root))) {
        return;
    }

    boost::regex layerFileRegex(R"(Lv(\d+)Tier(\d+)\.(png|jpg|webp)$)", boost::regex::icase);

    for (const auto& entry : fs::recursive_directory_iterator(MAA_NS::path(root))) {
        if (entry.is_directory()) {
            continue;
        }
        const auto& entryPath = entry.path();
        const std::string filename = MAA_NS::path_to_utf8_string(entryPath);
        const std::string parentName = MAA_NS::path_to_utf8_string(entryPath.parent_path().filename());

        std::string key;
        std::string filenameLower = entryPath.filename().string();
        std::transform(filenameLower.begin(), filenameLower.end(), filenameLower.begin(), ::tolower);

        if (filenameLower == "base.png") {
            key = std::format("{}_Base", parentName);
        }
        else {
            boost::smatch matches;
            if (boost::regex_search(filename, matches, layerFileRegex)) {
                key = std::format("{}_L{}_{}", parentName, TrimLeadingZeros(matches[1].str()), TrimLeadingZeros(matches[2].str()));
            }
            else {
                key = MAA_NS::path_to_utf8_string(entryPath.stem());
            }
        }

        cv::Mat img = MAA_NS::imread(entryPath, cv::IMREAD_UNCHANGED);
        if (img.empty()) {
            LogError << "Failed to load map: " << MAA_NS::path_to_utf8_string(entryPath);
            continue;
        }
        if (img.channels() == 3) {
            cv::cvtColor(img, img, cv::COLOR_BGR2BGRA);
        }
        zones[key] = std::move(img);
        LogInfo << "Loaded Map: " << key;
    }
}

void MapLocator::Impl::drainBackgroundGlobalSearchTasks()
{
    for (auto& task : backgroundGlobalSearchTasks) {
        if (!task.valid()) {
            continue;
        }
        try {
            task.wait();
            task.get();
        }
        catch (const std::exception& e) {
            LogError << "Background global search task failed: " << e.what();
        }
    }
    backgroundGlobalSearchTasks.clear();
}

std::optional<MapPosition> MapLocator::Impl::tryTracking(
    const MatchFeature& tmplFeat,
    IMatchStrategy* strategy,
    TimePoint now,
    const LocateOptions& options,
    MapPosition* outRawPos)
{
    if (!strategy) {
        return std::nullopt;
    }

    int maxAllowedLost = IsPathHeatmapZone(currentZoneId) ? 10 : options.max_lost_frames;
    if (currentZoneId.empty() || !motionTracker->isTracking(maxAllowedLost)) {
        return std::nullopt;
    }

    auto it = zones.find(currentZoneId);
    if (it == zones.end()) {
        return std::nullopt;
    }

    const cv::Mat& zoneMap = it->second;

    std::chrono::duration<double> dt = now - motionTracker->getLastTime();

    double trackScale = motionTracker->getLastPos()->scale;
    if (trackScale <= 0.0) {
        trackScale = 1.0;
    }

    cv::Rect searchRect = motionTracker->predictNextSearchRect(trackScale, tmplFeat.image.cols, tmplFeat.image.rows, now);

    cv::Rect mapBounds(0, 0, zoneMap.cols, zoneMap.rows);
    cv::Rect validRoi = searchRect & mapBounds;
    cv::Mat searchRoiWithPad;
    if (validRoi.empty()) {
        searchRoiWithPad = cv::Mat(searchRect.size(), zoneMap.type(), cv::Scalar(0, 0, 0, 0));
    }
    else {
        const int top = validRoi.y - searchRect.y;
        const int bottom = searchRect.y + searchRect.height - (validRoi.y + validRoi.height);
        const int left = validRoi.x - searchRect.x;
        const int right = searchRect.x + searchRect.width - (validRoi.x + validRoi.width);
        cv::copyMakeBorder(zoneMap(validRoi), searchRoiWithPad, top, bottom, left, right, cv::BORDER_CONSTANT, cv::Scalar(0, 0, 0, 0));
    }

    auto searchFeature = strategy->extractSearchFeature(searchRoiWithPad);

    cv::Mat scaledTempl, scaledWeightMask;
    if (std::abs(trackScale - 1.0) > 0.001) {
        cv::resize(tmplFeat.image, scaledTempl, cv::Size(), trackScale, trackScale, cv::INTER_LINEAR);
        cv::resize(tmplFeat.mask, scaledWeightMask, cv::Size(), trackScale, trackScale, cv::INTER_NEAREST);
    }
    else {
        scaledTempl = tmplFeat.image;
        scaledWeightMask = tmplFeat.mask;
    }

    auto trackResult = CoreMatch(searchFeature.image, scaledTempl, scaledWeightMask, matchCfg.blurSize);
    if (!trackResult) {
        LogInfo << "tryTracking: CoreMatch returned nullopt.";
        return std::nullopt;
    }

    LogInfo << "tryTracking" << VAR(trackResult->score) << VAR(trackResult->psr) << VAR(trackResult->delta) << VAR(trackResult->secondScore)
            << VAR(trackScale);

    auto validation =
        strategy->validateTracking(*trackResult, dt, motionTracker->getLastPos(), searchRect, scaledTempl.cols, scaledTempl.rows);

    if (outRawPos) {
        outRawPos->zoneId = currentZoneId;
        outRawPos->x = validation.absX;
        outRawPos->y = validation.absY;
        outRawPos->score = trackResult->score;
        outRawPos->scale = trackScale;
    }

    bool onlyAmbiguous = (!validation.isScreenBlocked && !validation.isEdgeSnapped && !validation.isTeleported);

    if (!validation.isValid && strategy->needsChamferCompensation()) {
        cv::Mat templGray, bgrTempl;
        if (std::abs(trackScale - 1.0) > 0.001) {
            cv::resize(tmplFeat.templRaw, bgrTempl, cv::Size(), trackScale, trackScale, cv::INTER_LINEAR);
        }
        else {
            bgrTempl = tmplFeat.templRaw;
        }
        if (bgrTempl.channels() == 3) {
            cv::cvtColor(bgrTempl, templGray, cv::COLOR_BGR2GRAY);
        }
        else if (bgrTempl.channels() == 4) {
            cv::cvtColor(bgrTempl, templGray, cv::COLOR_BGRA2GRAY);
        }
        else {
            templGray = bgrTempl.clone();
        }

        cv::Mat templEdge;
        cv::Canny(templGray, templEdge, 100, 200);
        cv::bitwise_and(templEdge, scaledWeightMask, templEdge);

        cv::Rect matchedRect(trackResult->loc.x, trackResult->loc.y, bgrTempl.cols, bgrTempl.rows);
        matchedRect &= cv::Rect(0, 0, searchRoiWithPad.cols, searchRoiWithPad.rows);

        cv::Mat patchGray;
        if (searchRoiWithPad.channels() == 3) {
            cv::cvtColor(searchRoiWithPad(matchedRect), patchGray, cv::COLOR_BGR2GRAY);
        }
        else if (searchRoiWithPad.channels() == 4) {
            cv::cvtColor(searchRoiWithPad(matchedRect), patchGray, cv::COLOR_BGRA2GRAY);
        }
        else {
            patchGray = searchRoiWithPad(matchedRect).clone();
        }

        cv::Mat patchEdge;
        cv::Canny(patchGray, patchEdge, 100, 200);

        cv::Mat distTrans;
        cv::Mat patchEdgeInv;
        cv::bitwise_not(patchEdge, patchEdgeInv);
        cv::distanceTransform(patchEdgeInv, distTrans, cv::DIST_L2, 3);

        // 倒角匹配降级补偿：
        // 当发生大比例旋转、透明UI遮罩异常或者光影畸变时，纯基于像素灰度的NCC会退化甚至失败 (分数低于阈值)。
        // 此时提取搜索区与模板图的 Canny 强边缘，计算搜索图边缘距离变换场在该模板轮廓覆盖下的平均距离。
        // 它衡量两者线框的拓扑拟合程度，若平均几何距离小(<4.5像素)，则说明其实地形拓扑依然吻合，仅是色度失真，强制保送及格。
        cv::Scalar meanDistScalar = cv::mean(distTrans, templEdge(cv::Rect(0, 0, matchedRect.width, matchedRect.height)));
        double meanDist = meanDistScalar[0];

        LogInfo << "Chamfer mean distance: " << meanDist;

        if (meanDist < 4.5) {
            validation.isValid = true;
            validation.isScreenBlocked = false;
            onlyAmbiguous = false;
            trackResult->score = std::max(trackResult->score, 0.43);
        }
    }

    if (onlyAmbiguous && motionTracker->isTracking(maxAllowedLost) && !validation.isValid) {
        auto hold = *motionTracker->getLastPos();
        hold.score = trackResult->score;
        hold.scale = trackScale;
        hold.isHeld = true;
        motionTracker->hold(hold, now);
        LogInfo << "Tracking ambiguous -> HOLD last pos." << VAR(trackResult->score) << VAR(trackResult->psr) << VAR(trackResult->delta);
        return hold;
    }

    if (!validation.isValid) {
        return std::nullopt;
    }

    if (validation.isValid) {
        MapPosition pos;
        pos.zoneId = currentZoneId;
        pos.x = validation.absX;
        pos.y = validation.absY;
        pos.score = trackResult->score;
        pos.scale = trackScale;
        pos.isHeld = false;
        motionTracker->update(pos, now);
        return pos;
    }

    return std::nullopt;
}

std::optional<MapPosition> MapLocator::Impl::evaluateAndAcceptResult(
    const MatchResultRaw& fineRes,
    const cv::Rect& validFineRect,
    const cv::Mat& templ,
    IMatchStrategy* strategy,
    const std::string& targetZoneId)
{
    double absLeft = validFineRect.x + fineRes.loc.x;
    double absTop = validFineRect.y + fineRes.loc.y;

    double finalScore = 0.0;
    if (!strategy->validateGlobalSearch(fineRes, finalScore)) {
        LogInfo << "Global Rejected. Score too low:" << VAR(fineRes.score) << VAR(fineRes.delta) << VAR(fineRes.psr);
        return std::nullopt;
    }

    MapPosition pos;
    pos.zoneId = targetZoneId;
    pos.x = absLeft + templ.cols / 2.0;
    pos.y = absTop + templ.rows / 2.0;
    pos.score = finalScore;
    return pos;
}

std::optional<MapPosition> MapLocator::Impl::tryConstrainedFineSearch(const SearchExecutionContext& ctx)
{
    cv::Mat fineMap = ctx.bigMap(ctx.constrainedRect);
    auto fineSearchFeat = ctx.strategy->extractSearchFeature(fineMap);
    std::vector<double> scales;
    for (double s = 0.90; s <= 1.101; s += 0.02) {
        scales.push_back(s);
    }

    std::vector<FineScaleSearchResult> scaleResults(scales.size());
    for (size_t i = 0; i < scales.size(); ++i) {
        scaleResults[i].scale = scales[i];
    }

    auto processScaleRange = [&](size_t beginIndex, size_t endIndex) {
        for (size_t i = beginIndex; i < endIndex; ++i) {
            const double s = scales[i];
            auto& scaleResult = scaleResults[i];

            cv::Mat scaledTempl, scaledWeightMask;
            if (std::abs(s - 1.0) > 0.001) {
                cv::resize(ctx.tmplFeat.image, scaledTempl, cv::Size(), s, s, cv::INTER_LINEAR);
                cv::resize(ctx.tmplFeat.mask, scaledWeightMask, cv::Size(), s, s, cv::INTER_NEAREST);
            }
            else {
                scaledTempl = ctx.tmplFeat.image;
                scaledWeightMask = ctx.tmplFeat.mask;
            }

            if (scaledTempl.cols > fineSearchFeat.image.cols || scaledTempl.rows > fineSearchFeat.image.rows
                || cv::countNonZero(scaledWeightMask) < 5) {
                continue;
            }

            auto fineRes = CoreMatch(fineSearchFeat.image, scaledTempl, scaledWeightMask, matchCfg.blurSize);
            if (!fineRes) {
                continue;
            }

            scaleResult.hasRawResult = true;
            scaleResult.fineRes = *fineRes;
            scaleResult.scaledTempl = scaledTempl;
        }
    };

    const unsigned hardwareThreads = std::max(1U, std::thread::hardware_concurrency());
    const size_t workerCount = std::min(scales.size(), static_cast<size_t>(hardwareThreads));
    if (workerCount <= 1) {
        processScaleRange(0, scales.size());
    }
    else {
        const size_t chunkSize = (scales.size() + workerCount - 1) / workerCount;
        std::vector<std::future<void>> workers;
        workers.reserve(workerCount - 1);

        size_t beginIndex = 0;
        for (size_t workerIndex = 0; workerIndex < workerCount && beginIndex < scales.size(); ++workerIndex) {
            const size_t endIndex = std::min(scales.size(), beginIndex + chunkSize);
            if (workerIndex + 1 == workerCount) {
                processScaleRange(beginIndex, endIndex);
            }
            else {
                workers.emplace_back(std::async(std::launch::async, processScaleRange, beginIndex, endIndex));
            }
            beginIndex = endIndex;
        }

        for (auto& worker : workers) {
            worker.get();
        }
    }

    double bestValidScore = -1.0;
    double bestRawScore = -1.0;
    double bestScale = 1.0;
    MatchResultRaw bestFineRes;
    cv::Mat bestScaledTempl;

    // Preserve the original scan order and tie-breaks while parallelizing the expensive CoreMatch calls.
    for (const auto& scaleResult : scaleResults) {
        if (!scaleResult.hasRawResult) {
            continue;
        }

        if (scaleResult.fineRes.score > bestRawScore) {
            bestRawScore = scaleResult.fineRes.score;
            bestScale = scaleResult.scale;
            bestFineRes = scaleResult.fineRes;
            bestScaledTempl = scaleResult.scaledTempl;
        }

        auto directResult =
            evaluateAndAcceptResult(scaleResult.fineRes, ctx.constrainedRect, scaleResult.scaledTempl, ctx.strategy, ctx.targetZoneId);
        if (!directResult) {
            continue;
        }

        if (directResult->score > bestValidScore) {
            bestValidScore = directResult->score;
            bestScale = scaleResult.scale;
            bestFineRes = scaleResult.fineRes;
            bestScaledTempl = scaleResult.scaledTempl;
        }
    }

    if (ctx.outRawPos && bestRawScore >= 0.0) {
        ctx.outRawPos->zoneId = ctx.targetZoneId;
        ctx.outRawPos->x = ctx.constrainedRect.x + bestFineRes.loc.x + bestScaledTempl.cols / 2.0;
        ctx.outRawPos->y = ctx.constrainedRect.y + bestFineRes.loc.y + bestScaledTempl.rows / 2.0;
        ctx.outRawPos->score = bestRawScore;
        ctx.outRawPos->scale = bestScale;
    }

    if (bestValidScore < 0.0) {
        LogInfo << "Global Search: constrained ROI direct fine failed, no coarse fallback will be used." << VAR(bestRawScore);
        return std::nullopt;
    }

    auto directResult = evaluateAndAcceptResult(bestFineRes, ctx.constrainedRect, bestScaledTempl, ctx.strategy, ctx.targetZoneId);
    if (!directResult) {
        LogInfo << "Global Search: constrained ROI direct fine failed, no coarse fallback will be used." << VAR(bestRawScore);
        return std::nullopt;
    }

    directResult->scale = bestScale;
    LogInfo << "Global Search: direct fine search accepted inside constrained ROI." << VAR(bestScale) << VAR(bestValidScore);
    return directResult;
}

std::optional<MapPosition> MapLocator::Impl::tryLegacyCoarseSearch(const SearchExecutionContext& ctx)
{
    const cv::Rect mapBounds(0, 0, ctx.bigMap.cols, ctx.bigMap.rows);

    // 图像金字塔：全图匹配耗时极高，因此粗搜先固定在 coarseScale (约 0.2~0.3) 的降采样级别寻找可能的高分岛
    double coarseScale = matchCfg.coarseScale;

    cv::Mat constrainedMap = ctx.bigMap(ctx.constrainedRect);
    cv::Mat smallMap;
    cv::resize(constrainedMap, smallMap, cv::Size(), coarseScale, coarseScale, cv::INTER_AREA);

    auto coarseSearchFeat = ctx.strategy->extractSearchFeature(smallMap);
    cv::Mat mapToUse;
    if (coarseSearchFeat.image.channels() == 3) {
        cv::cvtColor(coarseSearchFeat.image, mapToUse, cv::COLOR_BGR2GRAY);
    }
    else if (coarseSearchFeat.image.channels() == 4) {
        cv::cvtColor(coarseSearchFeat.image, mapToUse, cv::COLOR_BGRA2GRAY);
    }
    else {
        mapToUse = coarseSearchFeat.image.clone();
    }

    if (matchCfg.blurSize > 0 && !ctx.strategy->needsChamferCompensation()) {
        cv::GaussianBlur(mapToUse, mapToUse, cv::Size(matchCfg.blurSize, matchCfg.blurSize), 0);
    }

    cv::Mat tmplGrayToUse;
    if (ctx.tmplFeat.image.channels() == 3) {
        cv::cvtColor(ctx.tmplFeat.image, tmplGrayToUse, cv::COLOR_BGR2GRAY);
    }
    else if (ctx.tmplFeat.image.channels() == 4) {
        cv::cvtColor(ctx.tmplFeat.image, tmplGrayToUse, cv::COLOR_BGRA2GRAY);
    }
    else {
        tmplGrayToUse = ctx.tmplFeat.image.clone();
    }

    struct CoarseCand
    {
        double s;
        double score;
        cv::Point loc;
    };

    std::vector<CoarseCand> cands;
    int topNPerScale = 3;
    int topK = 8;
    double coarseMin = 0.20;

    for (double s = 0.90; s <= 1.101; s += 0.02) {
        double currentScale = coarseScale * s;
        cv::Mat smallTempl, smallWeightMask;
        cv::resize(tmplGrayToUse, smallTempl, cv::Size(), currentScale, currentScale, cv::INTER_LINEAR);
        cv::resize(ctx.tmplFeat.mask, smallWeightMask, cv::Size(), currentScale, currentScale, cv::INTER_NEAREST);

        if (cv::countNonZero(smallWeightMask) < 5) {
            continue;
        }

        cv::Mat smallResult;
        cv::matchTemplate(mapToUse, smallTempl, smallResult, cv::TM_CCOEFF_NORMED, smallWeightMask);
        cv::patchNaNs(smallResult, -1.0f);

        // NMS 非极大值抑制的变体：
        // 在同一尺度下，同一位置附近极容易出现多个连块的高分点。
        // 我们用当前小模板尺寸的一半做为排异屏蔽半径 sr，取出一个最高分后便将其原位“挖去” (设为 -2)，再取下一个。
        // 这能保证获取的一批候选点分别位于不同的地形特征块中，增加后续回大图细搜抗错抓的鲁棒度。
        int sr = std::max(4, std::min(smallTempl.cols, smallTempl.rows) / 2);

        for (int i = 0; i < topNPerScale; ++i) {
            double mv;
            cv::Point ml;
            cv::minMaxLoc(smallResult, nullptr, &mv, nullptr, &ml);
            if (!std::isfinite(mv) || mv < coarseMin) {
                break;
            }

            cands.push_back({ s, mv, ml });

            cv::Rect sup(ml.x - sr, ml.y - sr, sr * 2 + 1, sr * 2 + 1);
            sup &= cv::Rect(0, 0, smallResult.cols, smallResult.rows);
            smallResult(sup).setTo(-2.0f);
        }
    }

    if (cands.empty()) {
        return std::nullopt;
    }

    std::sort(cands.begin(), cands.end(), [](auto& a, auto& b) { return a.score > b.score; });
    if ((int)cands.size() > topK) {
        cands.resize(topK);
    }

    double bestFine = -1.0;
    double bestScale = 1.0;
    MatchResultRaw bestFineRes;
    cv::Rect bestValidFineRect;
    cv::Mat bestScaledTempl, bestScaledMask;

    double fallbackScore = -1.0;
    double fallbackScale = 1.0;
    MatchResultRaw fallbackFineRes;
    cv::Rect fallbackValidFineRect;
    cv::Mat fallbackScaledTempl, fallbackScaledMask;

    int searchRadius = matchCfg.fineSearchRadius;

    for (auto& cand : cands) {
        double s = cand.s;
        int coarseX = static_cast<int>(cand.loc.x / coarseScale) + ctx.constrainedRect.x;
        int coarseY = static_cast<int>(cand.loc.y / coarseScale) + ctx.constrainedRect.y;

        cv::Mat scaledTempl, scaledWeightMask;
        if (std::abs(s - 1.0) > 0.001) {
            cv::resize(ctx.tmplFeat.image, scaledTempl, cv::Size(), s, s, cv::INTER_LINEAR);
            cv::resize(ctx.tmplFeat.mask, scaledWeightMask, cv::Size(), s, s, cv::INTER_NEAREST);
        }
        else {
            scaledTempl = ctx.tmplFeat.image;
            scaledWeightMask = ctx.tmplFeat.mask;
        }

        cv::Rect fineRect(
            coarseX - searchRadius,
            coarseY - searchRadius,
            scaledTempl.cols + searchRadius * 2,
            scaledTempl.rows + searchRadius * 2);
        cv::Rect validFineRect = fineRect & mapBounds;

        if (validFineRect.empty()) {
            continue;
        }

        cv::Mat fineMap = ctx.bigMap(validFineRect);

        auto fineSearchFeat = ctx.strategy->extractSearchFeature(fineMap);
        auto fineRes = CoreMatch(fineSearchFeat.image, scaledTempl, scaledWeightMask, matchCfg.blurSize);

        if (!fineRes) {
            continue;
        }

        if (fineRes->score > fallbackScore) {
            fallbackScore = fineRes->score;
            fallbackScale = s;
            fallbackFineRes = *fineRes;
            fallbackValidFineRect = validFineRect;
            fallbackScaledTempl = scaledTempl;
            fallbackScaledMask = scaledWeightMask;
        }

        bool ambiguous = false;
        if (ctx.strategy->needsChamferCompensation()) { // i.e. PathHeatmap
            ambiguous = (fineRes->psr < 6.0) || (fineRes->delta < 0.04);
            if (fineRes->score < 0.45 && ambiguous) {
                continue;
            }
        }
        else {
            double lowScoreCut = (ctx.targetZoneId.find("Base") != std::string::npos) ? 0.85 : 0.75;
            ambiguous = (fineRes->score < lowScoreCut) && (fineRes->psr < 6.0 || fineRes->delta < 0.02);
            if (ambiguous) {
                continue;
            }
        }

        if (fineRes->score > bestFine) {
            bestFine = fineRes->score;
            bestScale = s;
            bestFineRes = *fineRes;
            bestValidFineRect = validFineRect;
            bestScaledTempl = scaledTempl;
            bestScaledMask = scaledWeightMask;
        }
    }

    if (bestFine < 0) {
        if (fallbackScore < 0) {
            return std::nullopt;
        }
        bestFine = fallbackScore;
        bestScale = fallbackScale;
        bestFineRes = fallbackFineRes;
        bestValidFineRect = fallbackValidFineRect;
        bestScaledTempl = fallbackScaledTempl;
        bestScaledMask = fallbackScaledMask;
        LogInfo << "Global Search: All candidates ambiguous, using fallback (score " << fallbackScore << ")";
    }

    if (ctx.outRawPos && bestFine >= 0.0) {
        ctx.outRawPos->zoneId = ctx.targetZoneId;
        ctx.outRawPos->x = bestValidFineRect.x + bestFineRes.loc.x + bestScaledTempl.cols / 2.0;
        ctx.outRawPos->y = bestValidFineRect.y + bestFineRes.loc.y + bestScaledTempl.rows / 2.0;
        ctx.outRawPos->score = bestFine;
        ctx.outRawPos->scale = bestScale;
    }

    auto res = evaluateAndAcceptResult(bestFineRes, bestValidFineRect, bestScaledTempl, ctx.strategy, ctx.targetZoneId);
    if (res) {
        res->scale = bestScale;
    }
    return res;
}

std::optional<MapPosition> MapLocator::Impl::tryGlobalSearch(
    const MatchFeature& tmplFeat,
    IMatchStrategy* strategy,
    const std::string& targetZoneId,
    const SearchConstraint& constraint,
    MapPosition* outRawPos)
{
    if (!strategy || targetZoneId.empty()) {
        LogInfo << "Global Search Aborted: YOLO returned no result.";
        return std::nullopt;
    }

    if (zones.find(targetZoneId) == zones.end()) {
        std::string msg = "Global Search Aborted: YOLO predicted '" + targetZoneId + "', but this map is NOT loaded in 'zones'.";
        LogInfo << msg;
        return std::nullopt;
    }

    const cv::Mat& bigMap = zones.at(targetZoneId);
    const cv::Rect mapBounds(0, 0, bigMap.cols, bigMap.rows);
    if (constraint.mode == GlobalSearchMode::RoiFine) {
        const cv::Rect constrainedRect = constraint.roi & mapBounds;
        if (constrainedRect.empty()) {
            LogInfo << "Global Search Aborted: coarse ROI is outside of map bounds.";
            return std::nullopt;
        }
        return tryConstrainedFineSearch({ tmplFeat, strategy, bigMap, constrainedRect, targetZoneId, outRawPos });
    }

    if (constraint.mode == GlobalSearchMode::FullMapFine) {
        return tryConstrainedFineSearch({ tmplFeat, strategy, bigMap, mapBounds, targetZoneId, outRawPos });
    }

    return tryLegacyCoarseSearch({ tmplFeat, strategy, bigMap, mapBounds, targetZoneId, outRawPos });
}

YoloCoarseResult MapLocator::Impl::predictCoarse(const cv::Mat& minimap) const
{
    if (!zoneClassifier || !zoneClassifier->isLoaded()) {
        return {};
    }
    return zoneClassifier->predictCoarseByYOLO(minimap);
}

void MapLocator::Impl::refreshAsyncYoloState(const cv::Mat& minimap, TimePoint now)
{
    if (!zoneClassifier || !zoneClassifier->isLoaded()) {
        return;
    }

    std::unique_lock<std::mutex> lock(taskMutex, std::try_to_lock);
    if (!lock.owns_lock()) {
        return;
    }

    if (asyncYoloTask.valid() && asyncYoloTask.wait_for(std::chrono::seconds(0)) == std::future_status::ready) {
        YoloCoarseResult predicted = asyncYoloTask.get();
        if (predicted.valid && !predicted.is_none && !predicted.zone_id.empty() && !currentZoneId.empty()
            && predicted.zone_id != currentZoneId) {
            LogInfo << "Async YOLO detected zone change: " << currentZoneId << " -> " << predicted.zone_id;
            motionTracker->forceLost();
        }
    }

    if (asyncYoloTask.valid()) {
        return;
    }

    const auto elapsed = std::chrono::duration_cast<std::chrono::seconds>(now - lastYoloCheckTime).count();
    if (elapsed < 3) {
        return;
    }

    // 限制频次：YOLO CPU 推理存在开销，区域大范围切换并非瞬发，降低频率足以应对漂移容错并显著降低资源负担
    lastYoloCheckTime = now;
    cv::Mat yoloInput = minimap.clone();
    asyncYoloTask = std::async(std::launch::async, [this, yoloInput]() { return zoneClassifier->predictCoarseByYOLO(yoloInput); });
}

std::optional<LocateResult> MapLocator::Impl::tryTrackingLocate(
    const cv::Mat& minimap,
    const LocateOptions& options,
    const std::string& expectedZoneId,
    TimePoint now)
{
    if (options.force_global_search) {
        return std::nullopt;
    }

    refreshAsyncYoloState(minimap, now);

    if (currentZoneId.empty()) {
        return std::nullopt;
    }
    if (!expectedZoneId.empty() && currentZoneId != expectedZoneId) {
        return std::nullopt;
    }

    auto primaryStrategy = MatchStrategyFactory::create(currentZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg);
    if (!primaryStrategy) {
        return std::nullopt;
    }

    const bool isPathHeatmapZone = IsPathHeatmapZone(currentZoneId);
    MapPosition rawPrimaryPos {};
    auto trackingTmpl = primaryStrategy->extractTemplateFeature(minimap);
    auto trackingResult = tryTracking(trackingTmpl, primaryStrategy.get(), now, options, &rawPrimaryPos);
    const bool trackingHeld = trackingResult.has_value() && trackingResult->isHeld;

    if (trackingResult && !trackingHeld) {
        return LocateResult { .status = LocateStatus::Success, .position = trackingResult, .debugMessage = "Tracking Success" };
    }

    const bool shouldTryDualTracking = !isPathHeatmapZone && rawPrimaryPos.score > 0.1 && (!trackingResult || trackingHeld);
    if (shouldTryDualTracking) {
        auto fallbackStrategy =
            MatchStrategyFactory::create(currentZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg, MatchMode::ForcePathHeatmap);
        auto fallbackTmpl = fallbackStrategy->extractTemplateFeature(minimap);

        MapPosition rawFallbackPos {};
        auto fallbackResult = tryTracking(fallbackTmpl, fallbackStrategy.get(), now, options, &rawFallbackPos);
        const bool fallbackHeld = fallbackResult.has_value() && fallbackResult->isHeld;
        const double dist = std::hypot(rawPrimaryPos.x - rawFallbackPos.x, rawPrimaryPos.y - rawFallbackPos.y);

        if (rawFallbackPos.score > 0.1 && dist <= 5.0) {
            LogInfo << "Dual-Mode Tracking Verified! Coords matched. Dist: " << dist;

            MapPosition verifiedPos = rawPrimaryPos;
            verifiedPos.score = std::max(rawPrimaryPos.score, rawFallbackPos.score);
            motionTracker->update(verifiedPos, now);

            return LocateResult {
                .status = LocateStatus::Success,
                .position = verifiedPos,
                .debugMessage = "Dual-Mode Tracking Success",
            };
        }

        if (fallbackResult && !fallbackHeld) {
            LogInfo << "Dual-Mode Tracking: accepted fallback strategy result independently. Dist was " << dist;
            return LocateResult { .status = LocateStatus::Success, .position = fallbackResult, .debugMessage = "Tracking Success" };
        }
    }

    if (!trackingHeld) {
        return std::nullopt;
    }

    return LocateResult { .status = LocateStatus::Success, .position = trackingResult, .debugMessage = "Tracking Hold" };
}

SearchConstraint MapLocator::Impl::buildSearchConstraint(
    const std::string& expectedZoneSelector,
    const std::string& targetZoneId,
    const YoloCoarseResult& coarse) const
{
    SearchConstraint constraint;
    if (!coarse.valid) {
        return constraint;
    }

    constraint.yolo_validated = coarse.zone_id == targetZoneId && MatchesExpectedZoneSelector(expectedZoneSelector, coarse);
    if (!constraint.yolo_validated) {
        return constraint;
    }

    const bool isPathHeatmapZone = IsPathHeatmapZone(targetZoneId);
    if (isPathHeatmapZone) {
        LogInfo << "YOLO validated path-heatmap zone; keeping legacy coarse heatmap search." << VAR(expectedZoneSelector)
                << VAR(coarse.raw_class) << VAR(targetZoneId);
        return constraint;
    }

    if (!coarse.has_roi) {
        constraint.mode = GlobalSearchMode::FullMapFine;
        LogInfo << "YOLO validated zone without ROI mapping; using full-map direct fine search." << VAR(expectedZoneSelector)
                << VAR(coarse.raw_class) << VAR(targetZoneId);
        return constraint;
    }

    const auto zoneIt = zones.find(targetZoneId);
    if (zoneIt == zones.end()) {
        return constraint;
    }

    const cv::Mat& zoneMap = zoneIt->second;
    const cv::Rect mapBounds(0, 0, zoneMap.cols, zoneMap.rows);
    const cv::Rect expandedRoi(
        coarse.roi_x - coarse.infer_margin,
        coarse.roi_y - coarse.infer_margin,
        coarse.roi_w + coarse.infer_margin * 2,
        coarse.roi_h + coarse.infer_margin * 2);
    const cv::Rect constrainedRoi = expandedRoi & mapBounds;
    if (constrainedRoi.empty()) {
        return constraint;
    }

    constraint.mode = GlobalSearchMode::RoiFine;
    constraint.roi = constrainedRoi;
    LogInfo << "YOLO constrained global search to ROI" << VAR(expectedZoneSelector) << VAR(coarse.raw_class) << VAR(targetZoneId)
            << VAR(constraint.roi.x) << VAR(constraint.roi.y) << VAR(constraint.roi.width) << VAR(constraint.roi.height);
    return constraint;
}

std::optional<MapPosition> MapLocator::Impl::tryGlobalSearchWithFallback(
    const cv::Mat& minimap,
    const std::string& targetZoneId,
    const SearchConstraint& constraint)
{
    const bool isPathHeatmapZone = IsPathHeatmapZone(targetZoneId);
    const unsigned hardwareThreads = std::max(1U, std::thread::hardware_concurrency());
    const bool canSpeculateDualMode = !isPathHeatmapZone && constraint.mode != GlobalSearchMode::LegacyCoarse && hardwareThreads >= 8;

    auto runSearch = [this, &constraint, &targetZoneId](const cv::Mat& searchMinimap, MatchMode mode) -> GlobalSearchAttempt {
        GlobalSearchAttempt attempt;
        auto strategy = MatchStrategyFactory::create(targetZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg, mode);
        if (!strategy) {
            return attempt;
        }

        auto globalTmpl = strategy->extractTemplateFeature(searchMinimap);
        attempt.result = tryGlobalSearch(globalTmpl, strategy.get(), targetZoneId, constraint, &attempt.rawPos);
        return attempt;
    };

    std::future<GlobalSearchAttempt> fallbackTask;
    if (canSpeculateDualMode) {
        cv::Mat fallbackMinimap = minimap.clone();
        SearchConstraint fallbackConstraint = constraint;
        std::string fallbackZoneId = targetZoneId;
        fallbackTask = std::async(std::launch::async, [this, fallbackMinimap, fallbackConstraint, fallbackZoneId]() {
            GlobalSearchAttempt attempt;
            auto fallbackStrategy =
                MatchStrategyFactory::create(fallbackZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg, MatchMode::ForcePathHeatmap);
            if (!fallbackStrategy) {
                return attempt;
            }

            auto fallbackTmpl = fallbackStrategy->extractTemplateFeature(fallbackMinimap);
            attempt.result = tryGlobalSearch(fallbackTmpl, fallbackStrategy.get(), fallbackZoneId, fallbackConstraint, &attempt.rawPos);
            return attempt;
        });
    }

    auto primaryAttempt = runSearch(minimap, MatchMode::Auto);
    auto globalResult = primaryAttempt.result;
    const MapPosition& rawGlobalPrimaryPos = primaryAttempt.rawPos;

    const bool shouldTryDualMode =
        !globalResult && !isPathHeatmapZone && (constraint.mode != GlobalSearchMode::LegacyCoarse || rawGlobalPrimaryPos.score > 0.1);
    if (!shouldTryDualMode) {
        if (fallbackTask.valid()) {
            // Keep the future alive so its destructor cannot block the successful path.
            backgroundGlobalSearchTasks.emplace_back(std::move(fallbackTask));
        }
        return globalResult;
    }

    GlobalSearchAttempt fallbackAttempt;
    if (fallbackTask.valid()) {
        fallbackAttempt = fallbackTask.get();
    }
    else {
        fallbackAttempt = runSearch(minimap, MatchMode::ForcePathHeatmap);
    }

    auto fallbackResult = fallbackAttempt.result;
    const MapPosition& rawGlobalFallbackPos = fallbackAttempt.rawPos;
    const double dist = std::hypot(rawGlobalPrimaryPos.x - rawGlobalFallbackPos.x, rawGlobalPrimaryPos.y - rawGlobalFallbackPos.y);

    // 双策略验证：正常图传和梯度图传独立得出的坐标若极度相近（误差<5像素），说明虽然个别策略信心不足，但互为佐证，此即确信坐标
    if (rawGlobalPrimaryPos.score > 0.1 && rawGlobalFallbackPos.score > 0.1 && dist <= 5.0) {
        LogInfo << "Dual-Mode Global Search Verified! Dist: " << dist;
        globalResult = rawGlobalPrimaryPos;
        globalResult->score = std::max(rawGlobalPrimaryPos.score, rawGlobalFallbackPos.score);
        return globalResult;
    }

    if (!globalResult && fallbackResult) {
        LogInfo << "Global Search: accepted fallback strategy result inside same ROI/path.";
        return fallbackResult;
    }

    return globalResult;
}

LocateResult MapLocator::Impl::locate(const cv::Mat& minimap, const LocateOptions& options)
{
    const auto now = std::chrono::steady_clock::now();

    if (!isInitialized) {
        return LocateResult { .status = LocateStatus::NotInitialized, .debugMessage = "MapLocator not initialized." };
    }

    drainBackgroundGlobalSearchTasks();

    matchCfg.passThreshold = options.loc_threshold;
    matchCfg.yoloConfThreshold = options.yolo_threshold;
    if (zoneClassifier) {
        zoneClassifier->SetConfThreshold(options.yolo_threshold);
    }

    std::future<double> angleFuture = std::async(std::launch::async, [&minimap]() { return InferYellowArrowRotation(minimap); });
    std::optional<double> resolvedAngle;
    auto resolveAngle = [&]() -> double {
        if (!resolvedAngle.has_value()) {
            resolvedAngle = angleFuture.get();
        }
        return *resolvedAngle;
    };

    std::optional<YoloCoarseResult> angleGuardCoarse;
    const std::string expectedZoneSelector = options.expected_zone_id;
    const std::string expectedZoneId = NormalizeExpectedZoneId(expectedZoneSelector, zoneClassifier.get());
    if (auto trackingResult = tryTrackingLocate(minimap, options, expectedZoneId, now)) {
        if (trackingResult->position.has_value()) {
            trackingResult->position->angle = resolveAngle();
        }
        return *trackingResult;
    }

    const double inferredAngle = resolveAngle();
    if (inferredAngle < 0.0) {
        angleGuardCoarse = predictCoarse(minimap);
        LogInfo << "Angle inference failed; forcing synchronous YOLO refresh." << VAR(angleGuardCoarse->valid)
                << VAR(angleGuardCoarse->is_none) << VAR(angleGuardCoarse->zone_id);
    }

    std::string targetZoneId = expectedZoneId;
    const YoloCoarseResult coarse = angleGuardCoarse.value_or(predictCoarse(minimap));
    if (targetZoneId.empty() && coarse.valid) {
        targetZoneId = coarse.zone_id;
    }

    if (targetZoneId.empty()) {
        const std::string debugMessage =
            expectedZoneId.empty() ? "YOLO inference failed or no result." : "Expected zone is empty and YOLO inference failed.";
        return LocateResult { .status = LocateStatus::YoloFailed, .debugMessage = debugMessage };
    }

    if ((expectedZoneId.empty() && coarse.valid && coarse.is_none) || targetZoneId == "None") {
        LogInfo << "YOLO explicitly identified 'None', assuming UI occlusion.";

        if (motionTracker->getLastPos()) {
            motionTracker->hold(*motionTracker->getLastPos(), now);
        }

        MapPosition nonePos;
        nonePos.zoneId = "None";
        nonePos.x = 0;
        nonePos.y = 0;
        nonePos.score = 1.0;
        return LocateResult { .status = LocateStatus::Success, .position = nonePos, .debugMessage = "Occluded by UI (None)" };
    }

    const SearchConstraint constraint = buildSearchConstraint(expectedZoneSelector, targetZoneId, coarse);
    if (coarse.valid && !coarse.is_none && !constraint.yolo_validated) {
        return LocateResult { .status = LocateStatus::YoloFailed,
                              .debugMessage = "YOLO is confident but zone validation failed. Aborting before broad search." };
    }
    const bool isPathHeatmapZone = IsPathHeatmapZone(targetZoneId);
    if (coarse.valid && !coarse.is_none && coarse.has_roi && !isPathHeatmapZone && constraint.mode != GlobalSearchMode::RoiFine) {
        return LocateResult { .status = LocateStatus::YoloFailed,
                              .debugMessage = "YOLO is confident but ROI constraint validation failed. Aborting to avoid broad search." };
    }

    int maxAllowedLost = IsPathHeatmapZone(targetZoneId) ? 10 : options.max_lost_frames;
    auto globalResult = tryGlobalSearchWithFallback(minimap, targetZoneId, constraint);
    if (!globalResult) {
        motionTracker->markLost();
        if (motionTracker->getLostCount() > maxAllowedLost) {
            motionTracker->forceLost();
        }
        return LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = "Global search failed." };
    }

    const auto lastPosOpt = motionTracker->getLastPos();
    const bool zoneChanged = currentZoneId != globalResult->zoneId;
    const bool hasLast = lastPosOpt.has_value() && !zoneChanged;
    const double jumpDist = hasLast ? std::hypot(globalResult->x - lastPosOpt->x, globalResult->y - lastPosOpt->y) : 0.0;
    const bool farJump = hasLast && jumpDist > kFarJumpRejectDistance;
    const bool highConf = globalResult->score >= kHighConfidenceOverride;

    if (farJump && !highConf) {
        LogInfo << "Global Search: far-jump rejected." << VAR(jumpDist) << VAR(globalResult->score);
        motionTracker->markLost();
        if (motionTracker->getLostCount() > maxAllowedLost) {
            motionTracker->forceLost();
            coldStartBuffer.clear();
        }
        return LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = "Far-jump rejected." };
    }

    if (!hasLast && !highConf) {
        if (coldStartBuffer.size() >= static_cast<size_t>(kColdStartConsensusFrames)) {
            coldStartBuffer.erase(coldStartBuffer.begin());
        }
        coldStartBuffer.push_back(*globalResult);
        if (!IsTightCluster(coldStartBuffer, kPositionConsensusRadius)) {
            LogInfo << "Cold-start: collecting." << VAR(coldStartBuffer.size()) << VAR(globalResult->x) << VAR(globalResult->y)
                    << VAR(globalResult->score);
            return LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = "Cold-start collecting." };
        }
        LogInfo << "Cold-start: consensus." << VAR(globalResult->x) << VAR(globalResult->y) << VAR(globalResult->score);
    }
    coldStartBuffer.clear();

    if (farJump || zoneChanged) {
        motionTracker->clearVelocity();
        if (farJump) {
            LogInfo << "Global Search: high-conf reseed." << VAR(jumpDist) << VAR(globalResult->score);
        }
    }

    currentZoneId = globalResult->zoneId;
    globalResult->angle = inferredAngle;
    motionTracker->update(*globalResult, now);
    return LocateResult { .status = LocateStatus::Success, .position = globalResult, .debugMessage = "Global Search Success" };
}

void MapLocator::Impl::resetTrackingState()
{
    if (motionTracker) {
        motionTracker->forceLost();
        motionTracker->clearVelocity();
    }
    currentZoneId = "";
    coldStartBuffer.clear();
}

std::optional<MapPosition> MapLocator::Impl::getLastKnownPos() const
{
    if (motionTracker) {
        return motionTracker->getLastPos();
    }
    return std::nullopt;
}

// ======================================
// MapLocator Public Interface
// ======================================

MapLocator::MapLocator()
    : pimpl(std::make_unique<Impl>())
{
}

MapLocator::~MapLocator() = default;

bool MapLocator::initialize(const MapLocatorConfig& config)
{
    return pimpl->initialize(config);
}

bool MapLocator::isInitialized() const
{
    return pimpl->getIsInitialized();
}

LocateResult MapLocator::locate(const cv::Mat& minimap, const LocateOptions& options)
{
    auto start = std::chrono::high_resolution_clock::now();
    LocateResult res = pimpl->locate(minimap, options);
    auto end = std::chrono::high_resolution_clock::now();
    if (res.position.has_value()) {
        res.position->latencyMs = std::chrono::duration_cast<std::chrono::milliseconds>(end - start).count();
    }
    return res;
}

YoloCoarseResult MapLocator::predictCoarse(const cv::Mat& minimap) const
{
    return pimpl->predictCoarse(minimap);
}

void MapLocator::resetTrackingState()
{
    pimpl->resetTrackingState();
}

std::optional<MapPosition> MapLocator::getLastKnownPos() const
{
    return pimpl->getLastKnownPos();
}

} // namespace maplocator
