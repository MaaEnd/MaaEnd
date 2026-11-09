#include <algorithm>
#include <array>
#include <atomic>
#include <cmath>
#include <condition_variable>
#include <cstdint>
#include <deque>
#include <exception>
#include <filesystem>
#include <format>
#include <functional>
#include <future>
#include <limits>
#include <mutex>
#include <string_view>
#include <thread>
#include <vector>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <boost/regex.hpp>
#include <meojson/json.hpp>

#include "CameraOrientationPredictor.h"
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

bool IsSupportedMapImage(const fs::path& path)
{
    static constexpr std::array<std::string_view, 5> kMapImageExtensions { ".png", ".jpg", ".jpeg", ".webp", ".bmp" };
    std::string ext = MAA_NS::path_to_utf8_string(path.extension());
    std::transform(ext.begin(), ext.end(), ext.begin(), ::tolower);
    return std::ranges::any_of(kMapImageExtensions, [&ext](std::string_view candidate) { return candidate == ext; });
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

std::string NormalizeExpectedZoneId(const std::string& expected_zone_selector, const YoloModel* predictor)
{
    if (expected_zone_selector.empty() || predictor == nullptr) {
        return expected_zone_selector;
    }
    return YoloNameToZoneId(*predictor, expected_zone_selector);
}

struct FineMatchResult
{
    bool hasRawResult = false;
    MatchResultRaw fineRes;
    cv::Size scaledTemplSize;
};

struct GlobalSearchAttempt
{
    std::optional<MapPosition> result;
    MapPosition rawPos {};
    TemplateFeatureKind kind = TemplateFeatureKind::StandardBase;
    MatchResultRaw window;
    MatchResultRaw refined;
};

struct AsyncYoloHandle
{
    std::uint64_t frameId = 0;
    std::shared_future<YoloCoarseResult> future;
};

class FrameTemplateFeatureCache
{
public:
    const MatchFeature& get(const cv::Mat& minimap, const MatchStrategy& strategy)
    {
        const auto kind = TemplateKindOf(strategy);
        Slot& slot = state->slots.at(static_cast<size_t>(kind));
        std::call_once(slot.once, [&]() { slot.feature = ExtractTemplateFeature(strategy, minimap); });
        return slot.feature;
    }

private:
    struct Slot
    {
        std::once_flag once;
        MatchFeature feature;
    };

    struct State
    {
        std::array<Slot, 4> slots;
    };

    std::shared_ptr<State> state = std::make_shared<State>();
};

using TimePoint = std::chrono::steady_clock::time_point;

// 精修窗围绕粗扫峰位的半径，取值远大于实测的粗扫/精修位置差
constexpr int kGlobalRefineRadius = 24;

// 池子大小不只定并发上限，还是两处分流常数的标定口径：executor 的 globalConcurrencyLimit
// 按 workerCount/3 给全局搜索留额（不足 5 个 worker 时退化成不限流），ensureFrameSearch 要求
// 至少 6 个才肯起协调器。缩池子会连带改掉这两条，得单独实测，不跟着尺度搜索一起动。
constexpr size_t kMatchWorkerLimit = 11;

// 掩膜是半径 min(w,h)/2 - borderMargin 的圆，取最小的 borderMargin(tier 图为 8) 估圆的外接框，
// 保证按它算出的补边量对所有图都不小于实际裁剪后的模板
constexpr int kMinMaskBorderMargin = 8;

// 模板中心只能落在搜索窗内缩 模板/2 的地方，搜索窗要按这个量补边，infer_margin 才是真的可达余量
int GlobalSearchRoiPad(const cv::Size& templSize)
{
    const int discSide = std::min(templSize.width, templSize.height) - 2 * kMinMaskBorderMargin + 1;
    const int side = std::clamp(discSide, 1, std::max(templSize.width, templSize.height));
    return static_cast<int>(std::ceil(side / 2.0)) + 1;
}

class GlobalSearchBatch : public std::enable_shared_from_this<GlobalSearchBatch>
{
public:
    GlobalSearchBatch(
        MatchFeature templateFeature,
        PreparedSearchFeature searchFeature,
        cv::Size searchSize,
        double templateScale,
        ScaleTaskPriority priority,
        PeakRefineMode refineMode = PeakRefineMode::Parabola,
        bool reserveTrackingCapacity = false)
        : templateFeature(std::move(templateFeature))
        , searchFeature(std::move(searchFeature))
        , searchSize(searchSize)
        , templateScale(templateScale)
        , refineMode(refineMode)
        , control(std::make_shared<ScaleTaskControl>())
    {
        control->priority.store(priority, std::memory_order_relaxed);
        control->reserveTrackingCapacity.store(reserveTrackingCapacity, std::memory_order_relaxed);
    }

    void start(MapLocatorScaleExecutor& executor)
    {
        remaining.store(1, std::memory_order_relaxed);
        const auto self = shared_from_this();
        executor.submit(control, ScaleTaskClass::Global, [self]() { self->executeMatch(); });
    }

    void wait()
    {
        std::unique_lock<std::mutex> lock(completionMutex);
        completionCondition.wait(lock, [this]() { return remaining.load(std::memory_order_acquire) == 0; });
        if (exception) {
            std::rethrow_exception(exception);
        }
    }

    void cancel(MapLocatorScaleExecutor& executor) { executor.cancel(control); }

    void releaseTrackingCapacity(MapLocatorScaleExecutor& executor) { executor.releaseTrackingCapacity(control); }

    const FineMatchResult& getResult() const { return result; }

    const MatchFeature& getTemplateFeature() const { return templateFeature; }

    bool hasRawResult() const { return result.hasRawResult; }

private:
    void executeMatch()
    {
        try {
            if (!control->canceled.load(std::memory_order_relaxed)) {
                computeMatch();
            }
        }
        catch (...) {
            std::lock_guard<std::mutex> lock(completionMutex);
            if (!exception) {
                exception = std::current_exception();
            }
        }

        if (remaining.fetch_sub(1, std::memory_order_acq_rel) == 1) {
            completionCondition.notify_all();
        }
    }

    void computeMatch()
    {
        cv::Mat scaledTemplate;
        cv::Mat scaledWeightMask;
        if (std::abs(templateScale - 1.0) > 0.001) {
            cv::resize(templateFeature.image, scaledTemplate, cv::Size(), templateScale, templateScale, cv::INTER_LINEAR);
            cv::resize(templateFeature.mask, scaledWeightMask, cv::Size(), templateScale, templateScale, cv::INTER_NEAREST);
        }
        else {
            scaledTemplate = templateFeature.image;
            scaledWeightMask = templateFeature.mask;
        }

        // 掩膜是模板中心的圆，圆外恒为 0，对带掩膜的 NCC 零贡献。先缩放再裁掉全零边框，
        // 结果逐位相同，但模板更小、搜索窗可以少补一圈。
        const cv::Rect valid = scaledWeightMask.empty() ? cv::Rect {} : cv::boundingRect(scaledWeightMask);
        if (valid.empty()) {
            return;
        }
        const cv::Mat croppedTemplate = scaledTemplate(valid);
        const cv::Mat croppedWeightMask = scaledWeightMask(valid);

        if (croppedTemplate.cols > searchSize.width || croppedTemplate.rows > searchSize.height) {
            return;
        }

        const auto match = CoreMatchPrepared(searchFeature, croppedTemplate, croppedWeightMask, refineMode);
        if (!match) {
            return;
        }

        result.hasRawResult = true;
        result.fineRes = *match;
        // loc 换算回未裁剪模板的左上角，外面的 “loc + scaledTemplSize/2” 口径保持不变
        result.fineRes.loc.x -= valid.x;
        result.fineRes.loc.y -= valid.y;
        result.scaledTemplSize = scaledTemplate.size();
    }

    MatchFeature templateFeature;
    PreparedSearchFeature searchFeature;
    cv::Size searchSize;
    double templateScale = 1.0;
    PeakRefineMode refineMode;
    std::shared_ptr<ScaleTaskControl> control;
    FineMatchResult result;
    std::atomic<size_t> remaining = 0;
    std::mutex completionMutex;
    std::condition_variable completionCondition;
    std::exception_ptr exception;
};

struct TrackingMatch
{
    std::optional<MatchResultRaw> match;
    cv::Mat scaledTemplate;
    cv::Mat scaledWeightMask;
};

TrackingMatch MatchTrackingTemplate(
    const MatchFeature& templateFeature,
    const PreparedSearchFeature& searchFeature,
    const cv::Size& searchSize,
    double templateScale)
{
    TrackingMatch out;
    if (templateScale <= 0.0) {
        return out;
    }

    if (std::abs(templateScale - 1.0) > 0.001) {
        cv::resize(templateFeature.image, out.scaledTemplate, cv::Size(), templateScale, templateScale, cv::INTER_LINEAR);
        cv::resize(templateFeature.mask, out.scaledWeightMask, cv::Size(), templateScale, templateScale, cv::INTER_NEAREST);
    }
    else {
        out.scaledTemplate = templateFeature.image;
        out.scaledWeightMask = templateFeature.mask;
    }

    if (out.scaledTemplate.cols > searchSize.width || out.scaledTemplate.rows > searchSize.height) {
        return out;
    }

    out.match = CoreMatchPrepared(searchFeature, out.scaledTemplate, out.scaledWeightMask, PeakRefineMode::Continuous);
    return out;
}

struct GlobalSearchComputation
{
    std::shared_ptr<const MatchStrategy> strategy;
    cv::Rect constrainedRect {};
    std::string targetZoneId;
    std::shared_ptr<GlobalSearchBatch> batch;
};

// 这一批给出的模板中心（未裁剪模板口径）
std::optional<cv::Point2d> BestRawCenter(const GlobalSearchComputation& computation)
{
    const FineMatchResult& best = computation.batch->getResult();
    if (!best.hasRawResult) {
        return std::nullopt;
    }
    return cv::Point2d(
        computation.constrainedRect.x + best.fineRes.loc.x + best.scaledTemplSize.width / 2.0,
        computation.constrainedRect.y + best.fineRes.loc.y + best.scaledTemplSize.height / 2.0);
}

struct GlobalSearchCandidates
{
    std::uint64_t frameId = 0;
    std::string targetZoneId;
    SearchConstraint constraint;
    GlobalSearchComputation primary;
};

bool SearchConstraintsEqual(const SearchConstraint& lhs, const SearchConstraint& rhs)
{
    return lhs.mode == rhs.mode && lhs.roi == rhs.roi && lhs.yolo_validated == rhs.yolo_validated;
}

class FrameSearchCoordinator : public std::enable_shared_from_this<FrameSearchCoordinator>
{
public:
    using CoarseFunction = std::function<YoloCoarseResult()>;
    using ComputeFunction = std::function<std::optional<GlobalSearchCandidates>(const YoloCoarseResult&)>;

    FrameSearchCoordinator(CoarseFunction coarse, MapLocatorScaleExecutor& executor, ComputeFunction compute)
        : coarse(std::move(coarse))
        , executor(executor)
        , compute(std::move(compute))
        , control(std::make_shared<ScaleTaskControl>())
    {
        control->priority.store(ScaleTaskPriority::Coordinator, std::memory_order_relaxed);
    }

    void start()
    {
        if (started.exchange(true, std::memory_order_relaxed)) {
            return;
        }
        const auto self = shared_from_this();
        executor.submit(control, ScaleTaskClass::Coordinator, [self]() { self->run(); });
    }

    YoloCoarseResult getCoarse()
    {
        std::unique_lock<std::mutex> lock(mutex);
        condition.wait(lock, [this]() { return coarseReady || completed; });
        if (exception) {
            std::rethrow_exception(exception);
        }
        return coarseResult.value_or(YoloCoarseResult {});
    }

    std::optional<GlobalSearchCandidates>
        takeCandidates(std::uint64_t expectedFrameId, const std::string& targetZoneId, const SearchConstraint& constraint)
    {
        std::unique_lock<std::mutex> lock(mutex);
        condition.wait(lock, [this]() { return completed; });
        if (exception) {
            std::rethrow_exception(exception);
        }
        if (!candidates.has_value() || candidates->frameId != expectedFrameId || candidates->targetZoneId != targetZoneId
            || !SearchConstraintsEqual(candidates->constraint, constraint)) {
            return std::nullopt;
        }
        GlobalSearchCandidates result = std::move(*candidates);
        candidates.reset();
        return result;
    }

    void cancel()
    {
        canceled.store(true, std::memory_order_relaxed);
        executor.cancel(control);
        {
            std::lock_guard<std::mutex> lock(mutex);
            if (candidates.has_value()) {
                cancelCandidates(*candidates);
            }
        }
    }

private:
    void cancelCandidates(GlobalSearchCandidates& value)
    {
        if (value.primary.batch) {
            value.primary.batch->cancel(executor);
        }
    }

    void run()
    {
        std::optional<GlobalSearchCandidates> computed;
        std::exception_ptr caught;
        std::optional<YoloCoarseResult> predicted;
        try {
            if (!canceled.load(std::memory_order_relaxed)) {
                predicted = coarse();
                {
                    std::lock_guard<std::mutex> lock(mutex);
                    coarseResult = predicted;
                    coarseReady = true;
                }
                condition.notify_all();
                if (!canceled.load(std::memory_order_relaxed)) {
                    computed = compute(*predicted);
                }
            }
        }
        catch (...) {
            caught = std::current_exception();
        }

        {
            std::lock_guard<std::mutex> lock(mutex);
            candidates = std::move(computed);
            exception = caught;
            if (canceled.load(std::memory_order_relaxed) && candidates.has_value()) {
                cancelCandidates(*candidates);
            }
            completed = true;
        }
        condition.notify_all();
    }

    CoarseFunction coarse;
    MapLocatorScaleExecutor& executor;
    ComputeFunction compute;
    std::shared_ptr<ScaleTaskControl> control;
    std::atomic<bool> started = false;
    std::atomic<bool> canceled = false;
    std::mutex mutex;
    std::condition_variable condition;
    std::optional<YoloCoarseResult> coarseResult;
    std::optional<GlobalSearchCandidates> candidates;
    std::exception_ptr exception;
    bool coarseReady = false;
    bool completed = false;
};

class FrameSearchCancelGuard
{
public:
    explicit FrameSearchCancelGuard(std::shared_ptr<FrameSearchCoordinator>* coordinator)
        : coordinator(coordinator)
    {
    }

    ~FrameSearchCancelGuard()
    {
        if (coordinator && *coordinator) {
            (*coordinator)->cancel();
        }
    }

private:
    std::shared_ptr<FrameSearchCoordinator>* coordinator = nullptr;
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

// 同峰时只取交付分高的一份：没看全这座峰的窗口，次峰会落在同一座峰的山坡上
GlobalSearchAttempt MergeHintAttempt(const GlobalSearchAttempt& current, const GlobalSearchAttempt& hint, double passThreshold)
{
    if (std::hypot(hint.rawPos.x - current.rawPos.x, hint.rawPos.y - current.rawPos.y) <= kPositionConsensusRadius) {
        return hint.rawPos.score > current.rawPos.score ? hint : current;
    }
    const bool hintWins = hint.refined.score > current.refined.score;
    const GlobalSearchAttempt& winner = hintWins ? hint : current;
    const GlobalSearchAttempt& loser = hintWins ? current : hint;
    GlobalSearchAttempt merged = winner;
    merged.window.secondScore = std::max(winner.window.secondScore, loser.window.score);
    merged.rawPos.score = DeliveredScore(merged.kind, merged.window, merged.refined);
    merged.result = std::nullopt;
    if (merged.rawPos.score >= passThreshold) {
        merged.result = merged.rawPos;
    }
    return merged;
}

double QuantizeToHundredth(double value)
{
    return std::round(value * 100.0) / 100.0;
}

MapPosition QuantizePosition(MapPosition position)
{
    position.x = QuantizeToHundredth(position.x);
    position.y = QuantizeToHundredth(position.y);
    return position;
}

// 这一帧建策略用的配置快照，后台搜索拿它的拷贝
struct StrategyConfigs
{
    TrackingConfig tracking;
    MatchConfig match;
    ImageProcessingConfig base_image {};
    ImageProcessingConfig tier_image {};
};

StrategyConfigs current_strategy_configs(const MapLocator& locator)
{
    return StrategyConfigs {
        .tracking = locator.resources.tracking_config,
        .match = locator.match_config,
        .base_image = locator.resources.base_image_config,
        .tier_image = locator.resources.tier_image_config,
    };
}

MatchStrategy make_strategy(const std::string& zoneId, const StrategyConfigs& configs)
{
    return MakeMatchStrategy(zoneId, configs.tracking, configs.match, configs.base_image, configs.tier_image);
}

void clear_global_search_feature_cache(LocatorResources* resources)
{
    for (auto& cache : resources->search_feature_cache) {
        std::lock_guard<std::mutex> lock(cache.mutex);
        cache.key.reset();
        cache.feature = {};
    }
}

void load_available_zones(LocatorResources* resources, const std::string& root)
{
    ++resources->zone_generation;
    clear_global_search_feature_cache(resources);
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
        std::string filenameLower = MAA_NS::path_to_utf8_string(entryPath.filename());
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
            if (IsSupportedMapImage(entryPath)) {
                LogError << "Failed to load map: " << MAA_NS::path_to_utf8_string(entryPath);
            }
            continue;
        }
        if (img.channels() == 1) {
            cv::cvtColor(img, img, cv::COLOR_GRAY2BGRA);
        }
        else if (img.channels() == 3) {
            cv::cvtColor(img, img, cv::COLOR_BGR2BGRA);
        }
        resources->zones[key] = std::move(img);
        LogInfo << "Loaded Map: " << key;
    }
}

MatchFeature get_global_search_feature(
    LocatorResources* resources,
    const std::string& targetZoneId,
    const cv::Rect& roi,
    const cv::Mat& mapRoi,
    const MatchStrategy& strategy)
{
    const TemplateFeatureKind kind = TemplateKindOf(strategy);
    const bool isPathHeatmap = kind == TemplateFeatureKind::PathHeatmapBase || kind == TemplateFeatureKind::PathHeatmapTier;
    CachedGlobalSearchFeature& cache = resources->search_feature_cache.at(isPathHeatmap ? 1 : 0);
    const GlobalSearchFeatureCacheKey key {
        .zoneId = targetZoneId,
        .kind = kind,
        .roi = roi,
        .generation = resources->zone_generation,
    };

    std::lock_guard<std::mutex> lock(cache.mutex);
    if (!cache.key.has_value() || *cache.key != key) {
        cache.feature = ExtractSearchFeature(strategy, mapRoi);
        cache.key = key;
    }
    return cache.feature;
}

MapPosition stabilize_position(std::optional<MapPosition>* stable, const MapPosition& raw)
{
    if (raw.zoneId == "None") {
        return raw;
    }

    if (!stable->has_value() || (*stable)->zoneId != raw.zoneId) {
        *stable = QuantizePosition(raw);
        return **stable;
    }

    const double dist = std::hypot(raw.x - (*stable)->x, raw.y - (*stable)->y);
    const double stableMin = ZoneScoreLines(raw.zoneId).stableMin;
    if (raw.score >= stableMin && dist <= kStableDeadband) {
        MapPosition out = raw;
        out.x = (*stable)->x;
        out.y = (*stable)->y;
        return out;
    }
    if (raw.score >= stableMin && dist < kStableReleaseDist) {
        MapPosition out = raw;
        out.x = (*stable)->x;
        out.y = (*stable)->y;
        return out;
    }

    *stable = QuantizePosition(raw);
    return **stable;
}

// 经静止锁定后记进跟踪器
MapPosition accept_position(MapLocator* locator, const MapPosition& raw, const LocateOptions& options, TimePoint now)
{
    MapPosition stable = stabilize_position(&locator->stable_position, raw);
    UpdateTrack(&locator->track, stable, now);
    return options.precise_position ? raw : stable;
}

struct TrackingOutcome
{
    std::optional<MapPosition> position;
    bool hold_pending = false;
    // 本帧跟踪不可信或分数不到线，调用方应起全局搜索；本帧照常返回
    bool wants_global_search = false;
};

TrackingOutcome track_frame(
    MapLocator* locator,
    const MatchFeature& tmplFeat,
    const MatchStrategy& strategy,
    const LocateOptions& options,
    TimePoint now)
{
    TrackingOutcome outcome;
    int maxAllowedLost = IsPathHeatmapZone(locator->zone_id) ? 10 : options.max_lost_frames;
    if (locator->zone_id.empty() || !IsTracking(locator->track, maxAllowedLost)) {
        return outcome;
    }

    auto it = locator->resources.zones.find(locator->zone_id);
    if (it == locator->resources.zones.end()) {
        return outcome;
    }

    const cv::Mat& zoneMap = it->second;

    std::chrono::duration<double> dt = now - locator->track.last_time;

    const double templateScale = ZoneTemplateScale(locator->zone_id);

    cv::Rect searchRect = PredictNextSearchRect(locator->track, templateScale, tmplFeat.image.cols, tmplFeat.image.rows, now);

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

    auto searchFeature = ExtractSearchFeature(strategy, searchRoiWithPad);
    const PreparedSearchFeature preparedSearch = PrepareSearchFeature(searchFeature.image);

    TrackingMatch tracking = MatchTrackingTemplate(tmplFeat, preparedSearch, searchFeature.image.size(), templateScale);
    std::optional<MatchResultRaw> trackResult = tracking.match;
    cv::Mat scaledTempl = std::move(tracking.scaledTemplate);
    cv::Mat scaledWeightMask = std::move(tracking.scaledWeightMask);
    if (trackResult) {
        trackResult->score = DeliveredScore(TemplateKindOf(strategy), *trackResult, *trackResult);
    }

    // 分数不到线就让上层考虑改走全局搜索，这一拍自己仍然照常返回
    if (!trackResult || trackResult->score < ZoneScoreLines(locator->zone_id).fastTrackingPass) {
        outcome.wants_global_search = true;
    }

    if (!trackResult) {
        LogInfo << "tryTracking: CoreMatch returned nullopt.";
        return outcome;
    }

    LogInfo << "tryTracking" << VAR(trackResult->score) << VAR(trackResult->psr) << VAR(trackResult->delta)
            << VAR(trackResult->secondScore);

    auto validation =
        ValidateTracking(strategy, *trackResult, dt, locator->track.last_known_pos, searchRect, scaledTempl.cols, scaledTempl.rows);

    bool onlyAmbiguous = (!validation.isScreenBlocked && !validation.isEdgeSnapped && !validation.isTeleported);

    if (!validation.isValid && NeedsChamferCompensation(strategy)) {
        cv::Mat templGray, bgrTempl;
        if (std::abs(templateScale - 1.0) > 0.001) {
            cv::resize(tmplFeat.templRaw, bgrTempl, cv::Size(), templateScale, templateScale, cv::INTER_LINEAR);
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

        cv::Rect matchedRect(
            static_cast<int>(std::lround(trackResult->loc.x)),
            static_cast<int>(std::lround(trackResult->loc.y)),
            bgrTempl.cols,
            bgrTempl.rows);
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

    // 本帧不可信：交付上一帧坐标保持导航输入连续，同时记一次丢失。连续攒满 max_lost_frames
    // 后 IsTracking 失效，自然翻进全局重新观测，不会一直吃陈旧坐标。
    if (onlyAmbiguous && IsTracking(locator->track, maxAllowedLost) && !validation.isValid) {
        outcome.wants_global_search = true;
        auto hold = *locator->track.last_known_pos;
        hold.score = trackResult->score;
        HoldTrack(&locator->track, hold, now);
        MarkTrackLost(&locator->track, 1);
        outcome.hold_pending = true;
        LogInfo << "Tracking ambiguous -> HOLD last pos." << VAR(trackResult->score) << VAR(trackResult->psr) << VAR(trackResult->delta);
        return outcome;
    }

    if (!validation.isValid) {
        outcome.wants_global_search = true;
        return outcome;
    }

    // 坐标 outlier 拒绝：跳变距离超过阈值且分数不足以支撑大幅位移时，hold 上一帧而非接受
    if (auto last = locator->track.last_known_pos; last && trackResult->score < ZoneScoreLines(locator->zone_id).trackingOutlierMin) {
        const double jumpDist = std::hypot(validation.absX - last->x, validation.absY - last->y);
        if (jumpDist > kTrackingOutlierDistance) {
            outcome.wants_global_search = true;
            auto held = *last;
            held.score = trackResult->score;
            HoldTrack(&locator->track, held, now);
            MarkTrackLost(&locator->track, 1);
            outcome.hold_pending = true;
            LogInfo << "Tracking outlier rejected, holding last pos." << VAR(jumpDist) << VAR(trackResult->score);
            return outcome;
        }
    }

    MapPosition pos;
    pos.zoneId = locator->zone_id;
    pos.x = validation.absX;
    pos.y = validation.absY;
    pos.score = trackResult->score;
    outcome.position = accept_position(locator, pos, options, now);
    return outcome;
}

std::optional<MapPosition> global_position_if_valid(
    const MatchResultRaw& fineRes,
    double score,
    const cv::Rect& validFineRect,
    const cv::Size& templSize,
    const MatchStrategy& strategy,
    const std::string& targetZoneId)
{
    double absLeft = validFineRect.x + fineRes.loc.x;
    double absTop = validFineRect.y + fineRes.loc.y;

    if (!ValidateGlobalSearch(strategy, score)) {
        LogDebug << "Global Rejected. Score too low:" << VAR(score) << VAR(fineRes.delta) << VAR(fineRes.psr);
        return std::nullopt;
    }

    MapPosition pos;
    pos.zoneId = targetZoneId;
    pos.x = absLeft + templSize.width / 2.0;
    pos.y = absTop + templSize.height / 2.0;
    pos.score = score;
    return pos;
}

// 在粗扫峰位周围开一个小窗做连续求极大；相关面的格点不再是精度上限，代价只是一个小窗
GlobalSearchComputation start_refine_search(LocatorResources* resources, const GlobalSearchComputation& coarse, const cv::Point2d& center)
{
    const auto zoneIt = resources->zones.find(coarse.targetZoneId);
    if (zoneIt == resources->zones.end()) {
        return {};
    }

    const cv::Mat& bigMap = zoneIt->second;
    const MatchFeature& tmplFeat = coarse.batch->getTemplateFeature();
    const int half = kGlobalRefineRadius + GlobalSearchRoiPad(tmplFeat.image.size());
    const cv::Rect refineRect =
        cv::Rect(static_cast<int>(std::lround(center.x)) - half, static_cast<int>(std::lround(center.y)) - half, half * 2, half * 2)
        & cv::Rect(0, 0, bigMap.cols, bigMap.rows);
    if (refineRect.empty()) {
        return {};
    }

    GlobalSearchComputation refined {
        .strategy = coarse.strategy,
        .constrainedRect = refineRect,
        .targetZoneId = coarse.targetZoneId,
    };
    // 精修窗逐帧都不一样，走缓存只会把粗扫那份挤掉
    MatchFeature searchFeature = ExtractSearchFeature(*coarse.strategy, bigMap(refineRect));
    refined.batch = std::make_shared<GlobalSearchBatch>(
        tmplFeat,
        PrepareSearchFeature(searchFeature.image),
        searchFeature.image.size(),
        ZoneTemplateScale(coarse.targetZoneId),
        ScaleTaskPriority::GlobalPrimary,
        PeakRefineMode::Continuous);
    refined.batch->start(*resources->executor);
    return refined;
}

GlobalSearchAttempt finish_global_search(LocatorResources* resources, const GlobalSearchComputation& computation)
{
    GlobalSearchAttempt attempt;
    if (!computation.batch || !computation.strategy) {
        return attempt;
    }

    computation.batch->wait();

    // 粗扫只负责挑位置，随后在它周围的小窗里连续求极大定坐标；精修开不起来就直接用粗扫结果
    const GlobalSearchComputation* evaluated = &computation;
    GlobalSearchComputation refined;
    if (const auto center = BestRawCenter(computation)) {
        refined = start_refine_search(resources, computation, *center);
        if (refined.batch) {
            refined.batch->wait();
            if (refined.batch->hasRawResult()) {
                evaluated = &refined;
            }
        }
    }

    const FineMatchResult& fine = evaluated->batch->getResult();
    if (!fine.hasRawResult) {
        LogInfo << "Global Search: constrained ROI direct fine failed, no coarse fallback will be used.";
        return attempt;
    }

    attempt.kind = TemplateKindOf(*evaluated->strategy);
    attempt.window = computation.batch->getResult().fineRes;
    attempt.refined = fine.fineRes;
    const double score = DeliveredScore(attempt.kind, attempt.window, attempt.refined);
    attempt.rawPos.zoneId = evaluated->targetZoneId;
    attempt.rawPos.x = evaluated->constrainedRect.x + fine.fineRes.loc.x + fine.scaledTemplSize.width / 2.0;
    attempt.rawPos.y = evaluated->constrainedRect.y + fine.fineRes.loc.y + fine.scaledTemplSize.height / 2.0;
    attempt.rawPos.score = score;

    auto bestValidPosition = global_position_if_valid(
        fine.fineRes,
        score,
        evaluated->constrainedRect,
        fine.scaledTemplSize,
        *evaluated->strategy,
        evaluated->targetZoneId);
    if (!bestValidPosition) {
        LogInfo << "Global Search: constrained ROI direct fine rejected." << VAR(score);
        return attempt;
    }

    LogInfo << "Global Search: direct fine search accepted inside constrained ROI." << VAR(bestValidPosition->score);
    attempt.result = bestValidPosition;
    return attempt;
}

GlobalSearchComputation start_global_search(
    LocatorResources* resources,
    const MatchFeature& tmplFeat,
    const std::string& targetZoneId,
    const SearchConstraint& constraint,
    std::shared_ptr<const MatchStrategy> strategy,
    ScaleTaskPriority priority,
    bool reserveTrackingCapacity)
{
    GlobalSearchComputation computation {
        .strategy = std::move(strategy),
        .targetZoneId = targetZoneId,
    };
    if (!computation.strategy || targetZoneId.empty()) {
        LogInfo << "Global Search Aborted: YOLO returned no result.";
        return computation;
    }

    if (resources->zones.find(targetZoneId) == resources->zones.end()) {
        std::string msg = "Global Search Aborted: YOLO predicted '" + targetZoneId + "', but this map is NOT loaded in 'zones'.";
        LogInfo << msg;
        return computation;
    }

    if (!constraint.yolo_validated) {
        LogInfo << "Global Search Aborted: no validated YOLO constraint." << VAR(targetZoneId);
        return computation;
    }

    const cv::Mat& bigMap = resources->zones.at(targetZoneId);
    const cv::Rect mapBounds(0, 0, bigMap.cols, bigMap.rows);
    MatchFeature searchFeature;
    if (constraint.mode == GlobalSearchMode::RoiFine) {
        // 不补边的话 infer_margin 名义上 64、实际只剩十来格，YOLO 报到相邻格时正确位置根本不在候选集里
        const int pad = GlobalSearchRoiPad(tmplFeat.image.size());
        const cv::Rect paddedRoi(
            constraint.roi.x - pad,
            constraint.roi.y - pad,
            constraint.roi.width + pad * 2,
            constraint.roi.height + pad * 2);
        const cv::Rect constrainedRect = paddedRoi & mapBounds;
        if (constrainedRect.empty()) {
            LogInfo << "Global Search Aborted: coarse ROI is outside of map bounds.";
            return computation;
        }
        computation.constrainedRect = constrainedRect;
        searchFeature = get_global_search_feature(resources, targetZoneId, constrainedRect, bigMap(constrainedRect), *computation.strategy);
    }
    else {
        computation.constrainedRect = mapBounds;
        searchFeature = ExtractSearchFeature(*computation.strategy, bigMap(mapBounds));
    }

    PreparedSearchFeature preparedSearch = PrepareSearchFeature(searchFeature.image);
    computation.batch = std::make_shared<GlobalSearchBatch>(
        tmplFeat,
        std::move(preparedSearch),
        searchFeature.image.size(),
        ZoneTemplateScale(targetZoneId),
        priority,
        PeakRefineMode::Parabola,
        reserveTrackingCapacity);
    computation.batch->start(*resources->executor);
    return computation;
}

YoloCoarseResult predict_coarse(YoloModel* yolo, const cv::Mat& minimap)
{
    if (!yolo || !yolo->loaded) {
        return {};
    }
    return PredictCoarseByYolo(yolo, minimap);
}

// 后台 YOLO 说小地图没了或换区了，就丢掉跟踪
void apply_zone_guard(MapLocator* locator, const YoloCoarseResult& predicted)
{
    if (predicted.valid && predicted.is_none) {
        LogInfo << "Async YOLO sees no minimap; dropping tracking.";
        ForceTrackLost(&locator->track);
        locator->stable_position.reset();
    }
    else if (predicted.valid && !predicted.zone_id.empty() && !locator->zone_id.empty() && predicted.zone_id != locator->zone_id) {
        LogInfo << "Async YOLO detected zone change: " << locator->zone_id << " -> " << predicted.zone_id;
        ForceTrackLost(&locator->track);
        locator->stable_position.reset();
    }
}

// 上一次后台 YOLO 出了结果就先应用它的换区判断，到了间隔再发起新的一次，返回本帧发起的那一次
std::optional<AsyncYoloHandle> apply_and_relaunch_periodic_yolo(MapLocator* locator, const cv::Mat& minimap, TimePoint now)
{
    YoloModel* yolo = locator->resources.yolo.get();
    if (!yolo || !yolo->loaded) {
        return std::nullopt;
    }

    PeriodicYolo& periodic = locator->periodic_yolo;
    std::unique_lock<std::mutex> lock(periodic.mutex, std::try_to_lock);
    if (!lock.owns_lock()) {
        return std::nullopt;
    }

    if (periodic.pending.has_value() && periodic.pending->future.valid()) {
        const bool isReady = periodic.pending->future.wait_for(std::chrono::seconds(0)) == std::future_status::ready;
        if (isReady && !periodic.pending->zoneGuardApplied) {
            const YoloCoarseResult predicted = periodic.pending->future.get();
            apply_zone_guard(locator, predicted);
            periodic.pending->zoneGuardApplied = true;
        }

        if (periodic.pending->frameId == locator->active_frame_id) {
            return AsyncYoloHandle { .frameId = periodic.pending->frameId, .future = periodic.pending->future };
        }
        if (!isReady) {
            return std::nullopt;
        }
    }

    const auto elapsed = std::chrono::duration_cast<std::chrono::seconds>(now - periodic.last_check).count();
    if (elapsed < 3) {
        return std::nullopt;
    }

    // 限制频次：YOLO CPU 推理存在开销，区域大范围切换并非瞬发，降低频率足以应对漂移容错并显著降低资源负担
    periodic.last_check = now;
    cv::Mat yoloInput = minimap.clone();
    auto future = std::async(std::launch::async, [yolo, yoloInput]() { return PredictCoarseByYolo(yolo, yoloInput); }).share();
    periodic.pending = AsyncYoloState { .frameId = locator->active_frame_id, .future = future };
    return AsyncYoloHandle { .frameId = locator->active_frame_id, .future = std::move(future) };
}

SearchConstraint build_search_constraint(
    const LocatorResources& resources,
    const std::string& expectedZoneSelector,
    const std::string& targetZoneId,
    const YoloCoarseResult& coarse)
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
        constraint.mode = GlobalSearchMode::FullMapFine;
        return constraint;
    }

    if (!coarse.has_roi) {
        constraint.mode = GlobalSearchMode::FullMapFine;
        return constraint;
    }

    const auto zoneIt = resources.zones.find(targetZoneId);
    if (zoneIt == resources.zones.end()) {
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
    return constraint;
}

void log_search_constraint(
    const SearchConstraint& constraint,
    const std::string& expectedZoneSelector,
    const std::string& targetZoneId,
    const YoloCoarseResult& coarse)
{
    if (!constraint.yolo_validated) {
        return;
    }
    if (IsPathHeatmapZone(targetZoneId)) {
        LogInfo << "YOLO validated path-heatmap zone; using full-map direct fine search." << VAR(expectedZoneSelector)
                << VAR(coarse.raw_class) << VAR(targetZoneId);
        return;
    }
    if (!coarse.has_roi) {
        LogInfo << "YOLO validated zone without ROI mapping; using full-map direct fine search." << VAR(expectedZoneSelector)
                << VAR(coarse.raw_class) << VAR(targetZoneId);
        return;
    }
    if (constraint.mode == GlobalSearchMode::RoiFine) {
        LogInfo << "YOLO constrained global search to ROI" << VAR(expectedZoneSelector) << VAR(coarse.raw_class) << VAR(targetZoneId)
                << VAR(constraint.roi.x) << VAR(constraint.roi.y) << VAR(constraint.roi.width) << VAR(constraint.roi.height);
    }
}

GlobalSearchCandidates start_global_search_candidates(
    LocatorResources* resources,
    const cv::Mat& minimap,
    const std::string& targetZoneId,
    const SearchConstraint& constraint,
    const StrategyConfigs& configs,
    FrameTemplateFeatureCache featureCache,
    std::uint64_t searchFrameId,
    bool reserveTrackingCapacity)
{
    GlobalSearchCandidates candidates {
        .frameId = searchFrameId,
        .targetZoneId = targetZoneId,
        .constraint = constraint,
    };
    std::shared_ptr<const MatchStrategy> strategy = std::make_shared<const MatchStrategy>(make_strategy(targetZoneId, configs));

    const MatchFeature& globalTmpl = featureCache.get(minimap, *strategy);
    candidates.primary = start_global_search(
        resources,
        globalTmpl,
        targetZoneId,
        constraint,
        std::move(strategy),
        ScaleTaskPriority::GlobalPrimary,
        reserveTrackingCapacity);
    return candidates;
}

GlobalSearchAttempt finish_global_search_candidates(LocatorResources* resources, GlobalSearchCandidates candidates)
{
    // Speculative global work stays deliberately narrow while Tracking may still
    // win the frame. Once the original control flow reaches Global, let the
    // batch use the full executor because there is no foreground Tracking work
    // left to protect.
    if (candidates.primary.batch) {
        candidates.primary.batch->releaseTrackingCapacity(*resources->executor);
    }

    return finish_global_search(resources, candidates.primary);
}

GlobalSearchAttempt try_global_search(
    MapLocator* locator,
    const cv::Mat& minimap,
    const std::string& targetZoneId,
    const SearchConstraint& constraint,
    FrameTemplateFeatureCache featureCache)
{
    GlobalSearchCandidates candidates = start_global_search_candidates(
        &locator->resources,
        minimap,
        targetZoneId,
        constraint,
        current_strategy_configs(*locator),
        std::move(featureCache),
        locator->active_frame_id,
        false);
    return finish_global_search_candidates(&locator->resources, std::move(candidates));
}

// 一帧里各步骤共用的输入和中间结果
struct FrameContext
{
    cv::Mat minimap;
    LocateOptions options;
    TimePoint now {};
    std::string expected_zone_selector;
    std::string expected_zone_id;
    FrameTemplateFeatureCache feature_cache;
    std::optional<AsyncYoloHandle> same_frame_yolo;
    std::shared_ptr<FrameSearchCoordinator> coordinator;
};

void apply_frame_thresholds(MapLocator* locator, const LocateOptions& options)
{
    locator->match_config.passThreshold = options.loc_threshold;
    locator->match_config.yoloConfThreshold = options.yolo_threshold;
    if (locator->resources.yolo) {
        locator->resources.yolo->conf_threshold = options.yolo_threshold;
    }
}

// 箭头不可见时，小地图可能被横幅遮挡；在匹配及更新追踪状态前拒帧。
// 超时放行以免长期遮挡处卡死；镜头朝向导航由调用方显式跳过此拦截。
std::optional<LocateResult>
    gate_occluded_frame(OcclusionGate* gate, const LocateOptions& options, const std::shared_future<double>& angle_future, TimePoint now)
{
    if (options.reject_occluded_frames && angle_future.get() < 0.0) {
        if (gate->since == TimePoint {}) {
            gate->since = now;
            gate->timed_out = false;
            LogWarn << "Minimap occluded: character arrow not visible; holding until it clears.";
        }
        if (now - gate->since < std::chrono::milliseconds(kOcclusionRejectTimeoutMs)) {
            return LocateResult { .status = LocateStatus::ScreenBlocked, .debugMessage = "Minimap occluded: arrow not visible." };
        }
        if (!gate->timed_out) {
            gate->timed_out = true;
            LogWarn << "Minimap occlusion outlasted the reject timeout; locating on occluded frames again.";
        }
        return std::nullopt;
    }
    gate->since = {};
    gate->timed_out = false;
    return std::nullopt;
}

// camRot 由参考配对模型同步推理；它需要 (x, y, zone)，定位失败帧没有参考可采。
LocateResult attach_cam_rot(LocatorResources* resources, const FrameContext& frame, LocateResult result)
{
    if (!resources->orientation || !OrientationModelLoaded(*resources->orientation) || !result.position.has_value()) {
        return result;
    }
    const std::string& zoneId = result.position->zoneId;
    if (zoneId == "None") {
        // None 是遮挡占位，没有对应的底图资产。
        return result;
    }
    const auto zoneIt = resources->zones.find(zoneId);
    const cv::Mat referenceAsset = zoneIt != resources->zones.end() ? zoneIt->second : cv::Mat();
    result.camRot = PredictCameraOrientation(
        resources->orientation.get(),
        frame.minimap,
        referenceAsset,
        result.position->x,
        result.position->y,
        ZoneTemplateScale(zoneId),
        zoneId,
        frame.options.camera_heading_prior);
    return result;
}

bool can_start_frame_search(const MapLocator& locator, const FrameContext& frame)
{
    return !frame.coordinator && locator.resources.executor->workerCount() >= 6;
}

// 强制全局、跟踪已丢或不在期望区域时，全局搜索可以和跟踪尝试并行先起
bool should_search_before_tracking(const MapLocator& locator, const FrameContext& frame)
{
    const int trackingLostLimit = IsPathHeatmapZone(locator.zone_id) ? 10 : frame.options.max_lost_frames;
    const bool trackerUnavailable = locator.zone_id.empty() || !IsTracking(locator.track, trackingLostLimit);
    const bool expectedZoneMismatch = !frame.expected_zone_id.empty() && locator.zone_id != frame.expected_zone_id;
    return frame.options.force_global_search || trackerUnavailable || expectedZoneMismatch;
}

// 在后台推测性地起这一帧的全局搜索，跟踪赢下这一帧时会被取消
void start_frame_search(MapLocator* locator, FrameContext* frame, bool reserveTrackingCapacity)
{
    const std::uint64_t searchFrameId = locator->active_frame_id;
    cv::Mat frameMinimap = frame->minimap.clone();
    FrameSearchCoordinator::CoarseFunction predictForFrame;
    if (frame->same_frame_yolo.has_value() && frame->same_frame_yolo->frameId == searchFrameId && frame->same_frame_yolo->future.valid()) {
        const std::shared_future<YoloCoarseResult> future = frame->same_frame_yolo->future;
        predictForFrame = [future]() {
            return future.get();
        };
    }
    else {
        cv::Mat yoloInput = frameMinimap;
        predictForFrame = [yolo = locator->resources.yolo.get(), yoloInput = std::move(yoloInput)]() {
            return predict_coarse(yolo, yoloInput);
        };
    }
    FrameTemplateFeatureCache frameFeatureCache = frame->feature_cache;
    const StrategyConfigs configs = current_strategy_configs(*locator);
    auto compute = [resources = &locator->resources,
                    frameMinimap = std::move(frameMinimap),
                    frameFeatureCache = std::move(frameFeatureCache),
                    expectedZoneSelector = frame->expected_zone_selector,
                    expectedZoneId = frame->expected_zone_id,
                    searchFrameId,
                    configs,
                    reserveTrackingCapacity](const YoloCoarseResult& coarse) mutable -> std::optional<GlobalSearchCandidates> {
        if (coarse.valid && coarse.is_none) {
            return std::nullopt;
        }

        std::string targetZoneId = expectedZoneId;
        if (targetZoneId.empty() && coarse.valid) {
            targetZoneId = coarse.zone_id;
        }
        if (targetZoneId.empty() || targetZoneId == "None") {
            return std::nullopt;
        }

        const SearchConstraint constraint = build_search_constraint(*resources, expectedZoneSelector, targetZoneId, coarse);
        if (coarse.valid && !coarse.is_none && !constraint.yolo_validated) {
            return std::nullopt;
        }
        const bool isPathHeatmapZone = IsPathHeatmapZone(targetZoneId);
        if (coarse.valid && !coarse.is_none && coarse.has_roi && !isPathHeatmapZone && constraint.mode != GlobalSearchMode::RoiFine) {
            return std::nullopt;
        }

        return start_global_search_candidates(
            resources,
            frameMinimap,
            targetZoneId,
            constraint,
            configs,
            frameFeatureCache,
            searchFrameId,
            reserveTrackingCapacity);
    };

    frame->coordinator =
        std::make_shared<FrameSearchCoordinator>(std::move(predictForFrame), *locator->resources.executor, std::move(compute));
    frame->coordinator->start();
}

struct TrackingLocate
{
    std::optional<LocateResult> result;
    bool wants_global_search = false;
};

TrackingLocate try_tracking_locate(MapLocator* locator, FrameContext* frame)
{
    TrackingLocate out;
    if (frame->options.force_global_search) {
        return out;
    }
    if (locator->zone_id.empty()) {
        return out;
    }
    if (!frame->expected_zone_id.empty() && locator->zone_id != frame->expected_zone_id) {
        return out;
    }

    const MatchStrategy strategy = make_strategy(locator->zone_id, current_strategy_configs(*locator));

    const MatchFeature& trackingTmpl = frame->feature_cache.get(frame->minimap, strategy);
    const TrackingOutcome tracked = track_frame(locator, trackingTmpl, strategy, frame->options, frame->now);
    out.wants_global_search = tracked.wants_global_search;

    if (tracked.position) {
        out.result = LocateResult {
            .status = LocateStatus::Success,
            .position = tracked.position,
            .debugMessage = "Tracking Success",
        };
        return out;
    }

    if (!tracked.hold_pending) {
        return out;
    }
    // 精确取位只要本帧实测：这帧直接判没认出，不交旧坐标，也不落到后面的全局搜索去改写跟踪状态。
    if (frame->options.precise_position) {
        out.result = LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = "Tracking Hold" };
        return out;
    }

    // 交付 tracker 里保留的上一帧坐标。丢失已在 track_frame 里记账。
    out.result =
        LocateResult { .status = LocateStatus::Success, .position = locator->track.last_known_pos, .debugMessage = "Tracking Hold" };
    return out;
}

YoloCoarseResult frame_coarse(MapLocator* locator, const FrameContext& frame)
{
    if (frame.coordinator) {
        return frame.coordinator->getCoarse();
    }
    if (frame.same_frame_yolo.has_value() && frame.same_frame_yolo->frameId == locator->active_frame_id
        && frame.same_frame_yolo->future.valid()) {
        return frame.same_frame_yolo->future.get();
    }
    return predict_coarse(locator->resources.yolo.get(), frame.minimap);
}

// YOLO 认定画面里没有小地图：停跟，免得下一帧跟踪在遮挡画面上挑个近处的假峰接着跟
LocateResult drop_tracking_for_absent_minimap(MapLocator* locator)
{
    ForceTrackLost(&locator->track);
    locator->stable_position.reset();
    return LocateResult {
        .status = LocateStatus::TrackingLost,
        .debugMessage = kColdStartCollectingMessage,
        .minimap_absent = true,
    };
}

LocateResult hold_for_ui_occlusion(MapLocator* locator, TimePoint now)
{
    LogInfo << "YOLO explicitly identified 'None', assuming UI occlusion.";

    if (locator->track.last_known_pos) {
        HoldTrack(&locator->track, *locator->track.last_known_pos, now);
    }

    MapPosition nonePos;
    nonePos.zoneId = "None";
    nonePos.x = 0;
    nonePos.y = 0;
    nonePos.score = 1.0;
    return LocateResult { .status = LocateStatus::Success, .position = nonePos, .debugMessage = "Occluded by UI (None)" };
}

// 调用方给了先验位置时，在每个先验附近再搜一小窗，跟 YOLO 格窗比原始峰分，高者胜出。
// YOLO 报到相邻格时正确位置根本不在它的窗里，窗内最优再高也是错的，分数门看不出来。
// 只在 YOLO 验过区域的帧上搜：区域都没认出来的帧上，小窗撞出的峰没有对照。
GlobalSearchAttempt search_hint_windows(
    MapLocator* locator,
    FrameContext* frame,
    const std::string& targetZoneId,
    const SearchConstraint& constraint,
    GlobalSearchAttempt global)
{
    if (!constraint.yolo_validated) {
        return global;
    }
    for (const SearchHint& hint : frame->options.search_hints) {
        const int radius = static_cast<int>(std::lround(hint.radius));
        if (hint.zone_id != targetZoneId || radius <= 0) {
            continue;
        }
        SearchConstraint hintConstraint;
        hintConstraint.mode = GlobalSearchMode::RoiFine;
        hintConstraint.yolo_validated = true;
        hintConstraint.roi = cv::Rect(
            static_cast<int>(std::lround(hint.x)) - radius,
            static_cast<int>(std::lround(hint.y)) - radius,
            radius * 2 + 1,
            radius * 2 + 1);
        GlobalSearchAttempt hintAttempt = try_global_search(locator, frame->minimap, targetZoneId, hintConstraint, frame->feature_cache);
        LogInfo << "Global Search: hint window." << VAR(hint.x) << VAR(hint.y) << VAR(hint.radius) << VAR(hintAttempt.rawPos.x)
                << VAR(hintAttempt.rawPos.y) << VAR(hintAttempt.refined.score) << VAR(global.rawPos.x) << VAR(global.rawPos.y)
                << VAR(global.refined.score);
        global = MergeHintAttempt(global, hintAttempt, locator->match_config.passThreshold);
    }
    return global;
}

GlobalSearchAttempt
    search_frame_globally(MapLocator* locator, FrameContext* frame, const std::string& targetZoneId, const SearchConstraint& constraint)
{
    GlobalSearchAttempt global;
    bool prefetchedCandidatesConsumed = false;
    if (frame->coordinator) {
        auto prefetchedCandidates = frame->coordinator->takeCandidates(locator->active_frame_id, targetZoneId, constraint);
        if (prefetchedCandidates.has_value()) {
            prefetchedCandidatesConsumed = true;
            global = finish_global_search_candidates(&locator->resources, std::move(*prefetchedCandidates));
        }
    }
    if (!prefetchedCandidatesConsumed) {
        global = try_global_search(locator, frame->minimap, targetZoneId, constraint, frame->feature_cache);
    }
    return search_hint_windows(locator, frame, targetZoneId, constraint, std::move(global));
}

LocateResult fail_global_search(MapLocator* locator, int maxAllowedLost)
{
    MarkTrackLost(&locator->track, 1);
    if (locator->track.lost_count > maxAllowedLost) {
        ForceTrackLost(&locator->track);
        locator->stable_position.reset();
    }
    return LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = "Global search failed." };
}

LocateResult reject_far_jump(MapLocator* locator, int maxAllowedLost)
{
    MarkTrackLost(&locator->track, 1);
    if (locator->track.lost_count > maxAllowedLost) {
        ForceTrackLost(&locator->track);
        locator->cold_start.clear();
        locator->stable_position.reset();
    }
    return LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = "Far-jump rejected." };
}

// 换区或高置信远跳后重新播种
void reseed_tracking(MapLocator* locator)
{
    locator->stable_position.reset();
    ClearTrackVelocity(&locator->track);
    // 先 MarkTrackLost 让 UpdateTrack 跳过速度 EMA，重播种的跳变不计入速度
    MarkTrackLost(&locator->track, 1);
}

LocateResult accept_global_result(
    MapLocator* locator,
    const FrameContext& frame,
    const std::shared_future<double>& angle_future,
    MapPosition globalResult,
    int maxAllowedLost)
{
    const auto lastPosOpt = locator->track.last_known_pos;
    const bool zoneChanged = locator->zone_id != globalResult.zoneId;
    const bool hasLast = lastPosOpt.has_value() && !zoneChanged;
    const double jumpDist = hasLast ? std::hypot(globalResult.x - lastPosOpt->x, globalResult.y - lastPosOpt->y) : 0.0;
    const bool farJump = hasLast && jumpDist > kFarJumpRejectDistance;
    const bool highConf = globalResult.score >= ZoneScoreLines(globalResult.zoneId).highConfidence;

    if (farJump && !highConf) {
        LogInfo << "Global Search: far-jump rejected." << VAR(jumpDist) << VAR(globalResult.score);
        return reject_far_jump(locator, maxAllowedLost);
    }

    if (!hasLast && !highConf) {
        if (locator->cold_start.size() >= static_cast<size_t>(kColdStartConsensusFrames)) {
            locator->cold_start.erase(locator->cold_start.begin());
        }
        locator->cold_start.push_back(globalResult);
        if (!IsTightCluster(locator->cold_start, kPositionConsensusRadius)) {
            LogInfo << "Cold-start: collecting." << VAR(locator->cold_start.size()) << VAR(globalResult.x) << VAR(globalResult.y)
                    << VAR(globalResult.score);
            return LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = kColdStartCollectingMessage };
        }
        LogInfo << "Cold-start: consensus." << VAR(globalResult.x) << VAR(globalResult.y) << VAR(globalResult.score);
    }
    locator->cold_start.clear();

    if (farJump || zoneChanged) {
        reseed_tracking(locator);
        if (farJump) {
            LogInfo << "Global Search: high-conf reseed." << VAR(jumpDist) << VAR(globalResult.score);
        }
    }

    locator->zone_id = globalResult.zoneId;
    globalResult.angle = angle_future.get();
    MapPosition accepted = accept_position(locator, globalResult, frame.options, frame.now);
    return attach_cam_rot(
        &locator->resources,
        frame,
        LocateResult { .status = LocateStatus::Success, .position = accepted, .debugMessage = "Global Search Success" });
}

// 跟踪没给出结果时：按 YOLO 定区域、校验约束，再全局搜索并决定是否采用
LocateResult locate_globally(MapLocator* locator, FrameContext* frame, const std::shared_future<double>& angle_future)
{
    std::string targetZoneId = frame->expected_zone_id;
    const YoloCoarseResult coarse = frame_coarse(locator, *frame);
    if (coarse.valid && coarse.is_none) {
        return drop_tracking_for_absent_minimap(locator);
    }

    if (targetZoneId.empty() && coarse.valid) {
        targetZoneId = coarse.zone_id;
    }

    if (targetZoneId.empty()) {
        const std::string debugMessage =
            frame->expected_zone_id.empty() ? "YOLO inference failed or no result." : "Expected zone is empty and YOLO inference failed.";
        return LocateResult { .status = LocateStatus::YoloFailed, .debugMessage = debugMessage };
    }

    if ((frame->expected_zone_id.empty() && coarse.valid && coarse.is_none) || targetZoneId == "None") {
        return hold_for_ui_occlusion(locator, frame->now);
    }

    const SearchConstraint constraint = build_search_constraint(locator->resources, frame->expected_zone_selector, targetZoneId, coarse);
    log_search_constraint(constraint, frame->expected_zone_selector, targetZoneId, coarse);
    if (coarse.valid && !coarse.is_none && !constraint.yolo_validated) {
        return LocateResult { .status = LocateStatus::YoloFailed,
                              .debugMessage = "YOLO is confident but zone validation failed. Aborting before broad search." };
    }
    const bool isPathHeatmapZone = IsPathHeatmapZone(targetZoneId);
    if (coarse.valid && !coarse.is_none && coarse.has_roi && !isPathHeatmapZone && constraint.mode != GlobalSearchMode::RoiFine) {
        return LocateResult { .status = LocateStatus::YoloFailed,
                              .debugMessage = "YOLO is confident but ROI constraint validation failed. Aborting to avoid broad search." };
    }

    int maxAllowedLost = IsPathHeatmapZone(targetZoneId) ? 10 : frame->options.max_lost_frames;
    const GlobalSearchAttempt global = search_frame_globally(locator, frame, targetZoneId, constraint);
    std::optional<MapPosition> globalResult = global.result;
    if (!globalResult) {
        if (global.refined.score > kSeamFallbackMinPeakScore) {
            globalResult = global.rawPos;
            LogInfo << "Global gate low-confidence: releasing best raw peak to avoid cold-start deadlock." << VAR(global.rawPos.x)
                    << VAR(global.rawPos.y) << VAR(global.rawPos.score);
        }
        else {
            return fail_global_search(locator, maxAllowedLost);
        }
    }

    return accept_global_result(locator, *frame, angle_future, *globalResult, maxAllowedLost);
}

LocateResult
    locate_frame(MapLocator* locator, const cv::Mat& minimap, const LocateOptions& options, const std::shared_future<double>& angle_future)
{
    const auto now = std::chrono::steady_clock::now();
    locator->active_frame_id = ++locator->frame_id;

    if (!locator->initialized) {
        return LocateResult { .status = LocateStatus::NotInitialized, .debugMessage = "MapLocator not initialized." };
    }

    apply_frame_thresholds(locator, options);
    if (std::optional<LocateResult> blocked = gate_occluded_frame(&locator->occlusion, options, angle_future, now)) {
        return *blocked;
    }

    FrameContext frame {
        .minimap = minimap,
        .options = options,
        .now = now,
        .expected_zone_selector = options.expected_zone_id,
        .expected_zone_id = NormalizeExpectedZoneId(options.expected_zone_id, locator->resources.yolo.get()),
    };
    FrameSearchCancelGuard frameSearchCancelGuard(&frame.coordinator);

    // 要在后台 YOLO 改写跟踪状态之前判断
    const bool searchBeforeTracking = should_search_before_tracking(*locator, frame) && can_start_frame_search(*locator, frame);
    if (!options.force_global_search) {
        frame.same_frame_yolo = apply_and_relaunch_periodic_yolo(locator, minimap, now);
    }
    if (searchBeforeTracking) {
        start_frame_search(locator, &frame, false);
    }

    const TrackingLocate tracked = try_tracking_locate(locator, &frame);
    if (tracked.wants_global_search && can_start_frame_search(*locator, frame)) {
        start_frame_search(locator, &frame, true);
    }
    if (tracked.result) {
        LocateResult trackingResult = *tracked.result;
        if (trackingResult.position.has_value()) {
            trackingResult.position->angle = angle_future.get();
        }
        return attach_cam_rot(&locator->resources, frame, std::move(trackingResult));
    }

    return locate_globally(locator, &frame, angle_future);
}

} // namespace

bool InitializeLocator(MapLocator* locator, const MapLocatorConfig& cfg)
{
    if (locator->initialized) {
        return true;
    }
    LocatorResources& resources = locator->resources;
    resources.config = cfg;

    locator->track = MakeMotionTrack(resources.tracking_config);
    const size_t matchWorkerCount = std::min(kMatchWorkerLimit, static_cast<size_t>(std::max(1U, std::thread::hardware_concurrency())));
    resources.executor = std::make_unique<MapLocatorScaleExecutor>(matchWorkerCount);
    load_available_zones(&resources, resources.config.mapResourceDir);

    if (!resources.config.yoloModelPath.empty()) {
        resources.yolo =
            LoadYoloModel(resources.config.yoloModelPath, locator->match_config.yoloConfThreshold, resources.config.yoloThreads);
    }

    // 摄像机朝向：预处理在 C++ 里执行，参考配对分类器的推理是本阶段的主要开销，故用 2 个
    // intra-op 线程并行，缩短同步帧追加的定位延迟。
    if (!resources.config.cameraOrientationRefModelPath.empty()) {
        resources.orientation = LoadOrientationModel(resources.config.cameraOrientationRefModelPath, 2);
    }

    locator->initialized = true;
    return true;
}

LocateResult Locate(MapLocator* locator, const cv::Mat& minimap, const LocateOptions& options)
{
    auto start = std::chrono::high_resolution_clock::now();
    // 角色朝向独立识别，在统一出口汇总，位置识别提前失败时也不丢弃本帧朝向。
    const auto angle_future = std::async(std::launch::async, [&minimap]() { return InferYellowArrowRotation(minimap); }).share();
    LocateResult res = locate_frame(locator, minimap, options, angle_future);
    const double angle = angle_future.get();
    if (std::isfinite(angle) && angle >= 0.0) {
        res.rot = angle;
    }
    if (res.position.has_value()) {
        res.position->angle = res.rot.value_or(-1.0);
    }
    auto end = std::chrono::high_resolution_clock::now();
    const long long latencyMs = std::chrono::duration_cast<std::chrono::milliseconds>(end - start).count();
    if (res.position.has_value()) {
        res.position->latencyMs = latencyMs;
    }
    return res;
}

void ResetTrackingState(MapLocator* locator)
{
    ForceTrackLost(&locator->track);
    ClearTrackVelocity(&locator->track);
    locator->zone_id = "";
    locator->occlusion = {};
    locator->cold_start.clear();
    locator->stable_position.reset();
}

} // namespace maplocator
