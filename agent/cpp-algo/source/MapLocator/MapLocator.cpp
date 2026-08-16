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
    std::string ext = path.extension().string();
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

std::string NormalizeExpectedZoneId(const std::string& expected_zone_selector, YoloPredictor* predictor)
{
    if (expected_zone_selector.empty() || predictor == nullptr) {
        return expected_zone_selector;
    }
    return predictor->convertYoloNameToZoneId(expected_zone_selector);
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
};

struct AsyncYoloHandle
{
    std::uint64_t frameId = 0;
    std::shared_future<YoloCoarseResult> future;
};

struct AsyncYoloState
{
    std::uint64_t frameId = 0;
    std::shared_future<YoloCoarseResult> future;
    bool zoneGuardApplied = false;
};

class FrameTemplateFeatureCache
{
public:
    const MatchFeature& get(const cv::Mat& minimap, IMatchStrategy* strategy)
    {
        const auto kind = strategy->templateFeatureKind();
        Slot& slot = state->slots.at(static_cast<size_t>(kind));
        std::call_once(slot.once, [&]() { slot.feature = strategy->extractTemplateFeature(minimap); });
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

enum class ScaleTaskPriority : int
{
    Discarded = 0,
    GlobalFallback = 1,
    GlobalPrimary = 2,
    Tracking = 3,
    Coordinator = 4,
};

enum class ScaleTaskClass
{
    Tracking,
    Global,
    Coordinator,
};

struct ScaleTaskControl
{
    std::atomic<ScaleTaskPriority> priority = ScaleTaskPriority::Discarded;
    std::atomic<bool> canceled = false;
    std::atomic<bool> reserveTrackingCapacity = false;
};

class MapLocatorScaleExecutor
{
public:
    explicit MapLocatorScaleExecutor(size_t workerCount)
        : globalConcurrencyLimit(workerCount > 4 ? std::max<size_t>(1, workerCount / 3) : workerCount)
    {
        workers.reserve(workerCount);
        for (size_t index = 0; index < workerCount; ++index) {
            workers.emplace_back([this]() { workerLoop(); });
        }
    }

    ~MapLocatorScaleExecutor()
    {
        {
            std::lock_guard<std::mutex> lock(mutex);
            stopping = true;
            queue.clear();
        }
        condition.notify_all();
        for (auto& worker : workers) {
            if (worker.joinable()) {
                worker.join();
            }
        }
    }

    MapLocatorScaleExecutor(const MapLocatorScaleExecutor&) = delete;
    MapLocatorScaleExecutor& operator=(const MapLocatorScaleExecutor&) = delete;

    void submit(const std::shared_ptr<ScaleTaskControl>& control, ScaleTaskClass taskClass, std::function<void()> function)
    {
        {
            std::lock_guard<std::mutex> lock(mutex);
            queue.emplace_back(QueuedTask {
                .control = control,
                .taskClass = taskClass,
                .sequence = nextSequence++,
                .function = std::move(function),
            });
        }
        condition.notify_one();
    }

    void reprioritize(const std::shared_ptr<ScaleTaskControl>& control, ScaleTaskPriority priority)
    {
        control->priority.store(priority, std::memory_order_relaxed);
        condition.notify_all();
    }

    void cancel(const std::shared_ptr<ScaleTaskControl>& control)
    {
        control->canceled.store(true, std::memory_order_relaxed);
        reprioritize(control, ScaleTaskPriority::Discarded);
    }

    void releaseTrackingCapacity(const std::shared_ptr<ScaleTaskControl>& control)
    {
        control->reserveTrackingCapacity.store(false, std::memory_order_relaxed);
        condition.notify_all();
    }

    size_t workerCount() const { return workers.size(); }

private:
    struct QueuedTask
    {
        std::shared_ptr<ScaleTaskControl> control;
        ScaleTaskClass taskClass = ScaleTaskClass::Global;
        std::uint64_t sequence = 0;
        std::function<void()> function;
    };

    auto findNextTask()
    {
        auto selected = queue.end();
        int selectedPriority = std::numeric_limits<int>::min();
        std::uint64_t selectedSequence = std::numeric_limits<std::uint64_t>::max();
        for (auto it = queue.begin(); it != queue.end(); ++it) {
            if (it->taskClass == ScaleTaskClass::Global && it->control->reserveTrackingCapacity.load(std::memory_order_relaxed)
                && activeGlobalTasks >= globalConcurrencyLimit) {
                continue;
            }
            if (it->taskClass == ScaleTaskClass::Coordinator && activeCoordinatorTasks >= 1) {
                continue;
            }
            const int priority = static_cast<int>(it->control->priority.load(std::memory_order_relaxed));
            if (priority > selectedPriority || (priority == selectedPriority && it->sequence < selectedSequence)) {
                selected = it;
                selectedPriority = priority;
                selectedSequence = it->sequence;
            }
        }
        return selected;
    }

    void workerLoop()
    {
        while (true) {
            QueuedTask task;
            {
                std::unique_lock<std::mutex> lock(mutex);
                condition.wait(lock, [this]() { return stopping || !queue.empty(); });
                if (stopping) {
                    return;
                }

                auto selected = findNextTask();
                if (selected == queue.end()) {
                    condition.wait(lock);
                    continue;
                }
                task = std::move(*selected);
                queue.erase(selected);
                if (task.taskClass == ScaleTaskClass::Global) {
                    ++activeGlobalTasks;
                }
                else if (task.taskClass == ScaleTaskClass::Tracking) {
                    ++activeTrackingTasks;
                }
                else if (task.taskClass == ScaleTaskClass::Coordinator) {
                    ++activeCoordinatorTasks;
                }
            }

            task.function();

            {
                std::lock_guard<std::mutex> lock(mutex);
                if (task.taskClass == ScaleTaskClass::Global) {
                    --activeGlobalTasks;
                }
                else if (task.taskClass == ScaleTaskClass::Tracking) {
                    --activeTrackingTasks;
                }
                else if (task.taskClass == ScaleTaskClass::Coordinator) {
                    --activeCoordinatorTasks;
                }
            }
            condition.notify_all();
        }
    }

    std::vector<std::thread> workers;
    std::deque<QueuedTask> queue;
    mutable std::mutex mutex;
    std::condition_variable condition;
    size_t activeTrackingTasks = 0;
    size_t activeGlobalTasks = 0;
    size_t activeCoordinatorTasks = 0;
    size_t globalConcurrencyLimit = 1;
    std::uint64_t nextSequence = 0;
    bool stopping = false;
};

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

    void promote(MapLocatorScaleExecutor& executor) { executor.reprioritize(control, ScaleTaskPriority::GlobalPrimary); }

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
    std::shared_ptr<IMatchStrategy> strategy;
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
    std::optional<GlobalSearchComputation> fallback;
    std::function<GlobalSearchComputation()> deferredFallback;
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
        if (value.fallback.has_value() && value.fallback->batch) {
            value.fallback->batch->cancel(executor);
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

struct GlobalSearchFeatureCacheKey
{
    std::string zoneId;
    TemplateFeatureKind kind = TemplateFeatureKind::StandardBase;
    cv::Rect roi {};
    std::uint64_t generation = 0;

    bool operator==(const GlobalSearchFeatureCacheKey&) const = default;
};

struct CachedGlobalSearchFeature
{
    std::mutex mutex;
    std::optional<GlobalSearchFeatureCacheKey> key;
    MatchFeature feature;
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

} // namespace

class MapLocator::Impl
{
public:
    Impl() = default;

    ~Impl()
    {
        if (asyncYoloState.has_value() && asyncYoloState->future.valid()) {
            asyncYoloState->future.wait();
        }
        scaleExecutor.reset();
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
        const std::function<void()>& slowPathSignal,
        MapPosition* outRawPos = nullptr);

    GlobalSearchComputation startGlobalSearch(
        const MatchFeature& tmplFeat,
        std::shared_ptr<IMatchStrategy> strategy,
        const std::string& targetZoneId,
        const SearchConstraint& constraint,
        ScaleTaskPriority priority,
        bool reserveTrackingCapacity);
    GlobalSearchComputation startRefineSearch(const GlobalSearchComputation& coarse, const cv::Point2d& center);

    std::optional<MapPosition> evaluateAndAcceptResult(
        const MatchResultRaw& fineRes,
        const cv::Rect& validFineRect,
        const cv::Size& templSize,
        IMatchStrategy* strategy,
        const std::string& targetZoneId);
    GlobalSearchAttempt finishGlobalSearch(const GlobalSearchComputation& computation);

    std::optional<AsyncYoloHandle> refreshAsyncYoloState(const cv::Mat& minimap, TimePoint now);
    std::optional<LocateResult> tryTrackingLocate(
        const cv::Mat& minimap,
        const LocateOptions& options,
        const std::string& expectedZoneId,
        TimePoint now,
        FrameTemplateFeatureCache& featureCache,
        std::optional<AsyncYoloHandle>* sameFrameYolo,
        bool yoloRefreshAlreadyPerformed,
        const std::function<void()>& slowPathSignal);
    SearchConstraint buildSearchConstraint(
        const std::string& expectedZoneSelector,
        const std::string& targetZoneId,
        const YoloCoarseResult& coarse,
        bool emitLog = true) const;
    GlobalSearchCandidates startGlobalSearchCandidates(
        const cv::Mat& minimap,
        const std::string& targetZoneId,
        const SearchConstraint& constraint,
        FrameTemplateFeatureCache featureCache,
        std::uint64_t searchFrameId,
        const TrackingConfig& trackingConfig,
        const MatchConfig& matchConfig,
        const ImageProcessingConfig& baseImageConfig,
        const ImageProcessingConfig& tierImageConfig,
        bool reserveTrackingCapacity);
    std::optional<MapPosition> finishGlobalSearchCandidates(GlobalSearchCandidates candidates, MapPosition* outBestRaw = nullptr);
    std::optional<MapPosition> tryGlobalSearchWithFallback(
        const cv::Mat& minimap,
        const std::string& targetZoneId,
        const SearchConstraint& constraint,
        FrameTemplateFeatureCache& featureCache,
        MapPosition* outBestRaw = nullptr);
    MapPosition stabilizePosition(const MapPosition& raw);
    MapPosition acceptPosition(const MapPosition& raw, TimePoint now);
    MatchFeature
        getGlobalSearchFeature(const std::string& targetZoneId, const cv::Rect& roi, const cv::Mat& mapRoi, IMatchStrategy* strategy);
    void clearGlobalSearchFeatureCache();

    void loadAvailableZones(const std::string& root);

    bool isInitialized = false;
    MapLocatorConfig config;

    std::map<std::string, cv::Mat> zones;
    std::string currentZoneId;
    std::array<CachedGlobalSearchFeature, 2> globalSearchFeatureCache;
    std::uint64_t zoneGeneration = 0;

    std::unique_ptr<MotionTracker> motionTracker;
    std::unique_ptr<YoloPredictor> zoneClassifier;
    std::unique_ptr<MapLocatorScaleExecutor> scaleExecutor;
    std::mutex taskMutex;
    std::optional<AsyncYoloState> asyncYoloState;
    std::chrono::steady_clock::time_point lastYoloCheckTime;
    std::uint64_t frameId = 0;
    std::uint64_t activeFrameId = 0;

    std::vector<MapPosition> coldStartBuffer;
    std::optional<MapPosition> stablePosition;

    // dual 裁判连续判主策略"离预测远"时，记下主策略自己报的位置，用于识别预测被喂错的死锁
    std::optional<MapPosition> arbiterRejectedPrimary;
    int arbiterRejectedPrimaryStreak = 0;

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
    const size_t matchWorkerCount = std::min(kMatchWorkerLimit, static_cast<size_t>(std::max(1U, std::thread::hardware_concurrency())));
    scaleExecutor = std::make_unique<MapLocatorScaleExecutor>(matchWorkerCount);
    loadAvailableZones(config.mapResourceDir);

    if (!config.yoloModelPath.empty()) {
        zoneClassifier = std::make_unique<YoloPredictor>(config.yoloModelPath, matchCfg.yoloConfThreshold, config.yoloThreads);
    }

    isInitialized = true;
    return true;
}

void MapLocator::Impl::loadAvailableZones(const std::string& root)
{
    ++zoneGeneration;
    clearGlobalSearchFeatureCache();
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
            if (IsSupportedMapImage(entryPath)) {
                LogError << "Failed to load map: " << MAA_NS::path_to_utf8_string(entryPath);
            }
            continue;
        }
        if (img.channels() == 3) {
            cv::cvtColor(img, img, cv::COLOR_BGR2BGRA);
        }
        zones[key] = std::move(img);
        LogInfo << "Loaded Map: " << key;
    }
}

MatchFeature MapLocator::Impl::getGlobalSearchFeature(
    const std::string& targetZoneId,
    const cv::Rect& roi,
    const cv::Mat& mapRoi,
    IMatchStrategy* strategy)
{
    const TemplateFeatureKind kind = strategy->templateFeatureKind();
    const bool isPathHeatmap = kind == TemplateFeatureKind::PathHeatmapBase || kind == TemplateFeatureKind::PathHeatmapTier;
    CachedGlobalSearchFeature& cache = globalSearchFeatureCache.at(isPathHeatmap ? 1 : 0);
    const GlobalSearchFeatureCacheKey key {
        .zoneId = targetZoneId,
        .kind = kind,
        .roi = roi,
        .generation = zoneGeneration,
    };

    std::lock_guard<std::mutex> lock(cache.mutex);
    if (!cache.key.has_value() || *cache.key != key) {
        cache.feature = strategy->extractSearchFeature(mapRoi);
        cache.key = key;
    }
    return cache.feature;
}

void MapLocator::Impl::clearGlobalSearchFeatureCache()
{
    for (auto& cache : globalSearchFeatureCache) {
        std::lock_guard<std::mutex> lock(cache.mutex);
        cache.key.reset();
        cache.feature = {};
    }
}

MapPosition MapLocator::Impl::stabilizePosition(const MapPosition& raw)
{
    if (raw.zoneId == "None") {
        return raw;
    }

    if (!stablePosition.has_value() || stablePosition->zoneId != raw.zoneId) {
        stablePosition = QuantizePosition(raw);
        return *stablePosition;
    }

    const double dist = std::hypot(raw.x - stablePosition->x, raw.y - stablePosition->y);
    if (raw.score >= kStableMinScore && dist <= kStableDeadband) {
        MapPosition out = raw;
        out.x = stablePosition->x;
        out.y = stablePosition->y;
        return out;
    }
    if (raw.score >= kStableMinScore && dist < kStableReleaseDist) {
        MapPosition out = raw;
        out.x = stablePosition->x;
        out.y = stablePosition->y;
        return out;
    }

    stablePosition = QuantizePosition(raw);
    return *stablePosition;
}

MapPosition MapLocator::Impl::acceptPosition(const MapPosition& raw, TimePoint now)
{
    MapPosition stable = stabilizePosition(raw);
    motionTracker->update(stable, now);
    return stable;
}

std::optional<MapPosition> MapLocator::Impl::tryTracking(
    const MatchFeature& tmplFeat,
    IMatchStrategy* strategy,
    TimePoint now,
    const LocateOptions& options,
    const std::function<void()>& slowPathSignal,
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

    const double templateScale = ZoneTemplateScale(currentZoneId);

    cv::Rect searchRect = motionTracker->predictNextSearchRect(templateScale, tmplFeat.image.cols, tmplFeat.image.rows, now);

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
    const PreparedSearchFeature preparedSearch = PrepareSearchFeature(searchFeature.image);

    bool slowPathSignaled = false;
    auto signalSlowPath = [&]() {
        if (!slowPathSignaled && slowPathSignal) {
            slowPathSignaled = true;
            slowPathSignal();
        }
    };

    TrackingMatch tracking = MatchTrackingTemplate(tmplFeat, preparedSearch, searchFeature.image.size(), templateScale);
    std::optional<MatchResultRaw> trackResult = tracking.match;
    cv::Mat scaledTempl = std::move(tracking.scaledTemplate);
    cv::Mat scaledWeightMask = std::move(tracking.scaledWeightMask);

    // 分数不到线就让上层考虑改走全局搜索，这一拍自己仍然照常返回
    if (!trackResult || trackResult->score < kFastTrackingPassScore) {
        signalSlowPath();
    }

    if (!trackResult) {
        LogInfo << "tryTracking: CoreMatch returned nullopt.";
        return std::nullopt;
    }

    LogInfo << "tryTracking" << VAR(trackResult->score) << VAR(trackResult->psr) << VAR(trackResult->delta)
            << VAR(trackResult->secondScore);

    auto validation =
        strategy->validateTracking(*trackResult, dt, motionTracker->getLastPos(), searchRect, scaledTempl.cols, scaledTempl.rows);

    if (outRawPos) {
        outRawPos->zoneId = currentZoneId;
        outRawPos->x = validation.absX;
        outRawPos->y = validation.absY;
        outRawPos->score = trackResult->score;
    }

    bool onlyAmbiguous = (!validation.isScreenBlocked && !validation.isEdgeSnapped && !validation.isTeleported);

    if (!validation.isValid && strategy->needsChamferCompensation()) {
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

    if (onlyAmbiguous && motionTracker->isTracking(maxAllowedLost) && !validation.isValid) {
        signalSlowPath();
        auto hold = *motionTracker->getLastPos();
        hold.score = trackResult->score;
        hold.isHeld = true;
        motionTracker->hold(hold, now);
        LogInfo << "Tracking ambiguous -> HOLD last pos." << VAR(trackResult->score) << VAR(trackResult->psr) << VAR(trackResult->delta);
        return hold;
    }

    if (!validation.isValid) {
        signalSlowPath();
        return std::nullopt;
    }

    if (validation.isValid) {
        // 坐标 outlier 拒绝：跳变距离超过阈值且分数不足以支撑大幅位移时，hold 上一帧而非接受
        if (auto last = motionTracker->getLastPos(); last && trackResult->score < kTrackingOutlierMinScore) {
            const double jumpDist = std::hypot(validation.absX - last->x, validation.absY - last->y);
            if (jumpDist > kTrackingOutlierDistance) {
                signalSlowPath();
                auto held = *last;
                held.score = trackResult->score;
                held.isHeld = true;
                motionTracker->hold(held, now);
                motionTracker->markLost();
                LogInfo << "Tracking outlier rejected, holding last pos." << VAR(jumpDist) << VAR(trackResult->score);
                return held;
            }
        }

        MapPosition pos;
        pos.zoneId = currentZoneId;
        pos.x = validation.absX;
        pos.y = validation.absY;
        pos.score = trackResult->score;
        pos.isHeld = false;
        return acceptPosition(pos, now);
    }

    return std::nullopt;
}

std::optional<MapPosition> MapLocator::Impl::evaluateAndAcceptResult(
    const MatchResultRaw& fineRes,
    const cv::Rect& validFineRect,
    const cv::Size& templSize,
    IMatchStrategy* strategy,
    const std::string& targetZoneId)
{
    double absLeft = validFineRect.x + fineRes.loc.x;
    double absTop = validFineRect.y + fineRes.loc.y;

    double finalScore = 0.0;
    if (!strategy->validateGlobalSearch(fineRes, finalScore)) {
        LogDebug << "Global Rejected. Score too low:" << VAR(fineRes.score) << VAR(fineRes.delta) << VAR(fineRes.psr);
        return std::nullopt;
    }

    MapPosition pos;
    pos.zoneId = targetZoneId;
    pos.x = absLeft + templSize.width / 2.0;
    pos.y = absTop + templSize.height / 2.0;
    pos.score = finalScore;
    return pos;
}

GlobalSearchAttempt MapLocator::Impl::finishGlobalSearch(const GlobalSearchComputation& computation)
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
        refined = startRefineSearch(computation, *center);
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

    attempt.rawPos.zoneId = evaluated->targetZoneId;
    attempt.rawPos.x = evaluated->constrainedRect.x + fine.fineRes.loc.x + fine.scaledTemplSize.width / 2.0;
    attempt.rawPos.y = evaluated->constrainedRect.y + fine.fineRes.loc.y + fine.scaledTemplSize.height / 2.0;
    attempt.rawPos.score = fine.fineRes.score;

    auto bestValidPosition = evaluateAndAcceptResult(
        fine.fineRes,
        evaluated->constrainedRect,
        fine.scaledTemplSize,
        evaluated->strategy.get(),
        evaluated->targetZoneId);
    if (!bestValidPosition) {
        LogInfo << "Global Search: constrained ROI direct fine rejected." << VAR(fine.fineRes.score);
        return attempt;
    }

    LogInfo << "Global Search: direct fine search accepted inside constrained ROI." << VAR(bestValidPosition->score);
    attempt.result = bestValidPosition;
    return attempt;
}

GlobalSearchComputation MapLocator::Impl::startGlobalSearch(
    const MatchFeature& tmplFeat,
    std::shared_ptr<IMatchStrategy> strategy,
    const std::string& targetZoneId,
    const SearchConstraint& constraint,
    ScaleTaskPriority priority,
    bool reserveTrackingCapacity)
{
    GlobalSearchComputation computation {
        .strategy = std::move(strategy),
        .targetZoneId = targetZoneId,
    };
    IMatchStrategy* strategyPtr = computation.strategy.get();
    if (!strategyPtr || targetZoneId.empty()) {
        LogInfo << "Global Search Aborted: YOLO returned no result.";
        return computation;
    }

    if (zones.find(targetZoneId) == zones.end()) {
        std::string msg = "Global Search Aborted: YOLO predicted '" + targetZoneId + "', but this map is NOT loaded in 'zones'.";
        LogInfo << msg;
        return computation;
    }

    if (!constraint.yolo_validated) {
        LogInfo << "Global Search Aborted: no validated YOLO constraint." << VAR(targetZoneId);
        return computation;
    }

    const cv::Mat& bigMap = zones.at(targetZoneId);
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
        searchFeature = getGlobalSearchFeature(targetZoneId, constrainedRect, bigMap(constrainedRect), strategyPtr);
    }
    else {
        computation.constrainedRect = mapBounds;
        searchFeature = strategyPtr->extractSearchFeature(bigMap(mapBounds));
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
    computation.batch->start(*scaleExecutor);
    return computation;
}

// 在粗扫峰位周围开一个小窗做连续求极大；相关面的格点不再是精度上限，代价只是一个小窗
GlobalSearchComputation MapLocator::Impl::startRefineSearch(const GlobalSearchComputation& coarse, const cv::Point2d& center)
{
    const auto zoneIt = zones.find(coarse.targetZoneId);
    if (zoneIt == zones.end()) {
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
    MatchFeature searchFeature = coarse.strategy->extractSearchFeature(bigMap(refineRect));
    refined.batch = std::make_shared<GlobalSearchBatch>(
        tmplFeat,
        PrepareSearchFeature(searchFeature.image),
        searchFeature.image.size(),
        ZoneTemplateScale(coarse.targetZoneId),
        ScaleTaskPriority::GlobalPrimary,
        PeakRefineMode::Continuous);
    refined.batch->start(*scaleExecutor);
    return refined;
}

YoloCoarseResult MapLocator::Impl::predictCoarse(const cv::Mat& minimap) const
{
    if (!zoneClassifier || !zoneClassifier->isLoaded()) {
        return {};
    }
    return zoneClassifier->predictCoarseByYOLO(minimap);
}

std::optional<AsyncYoloHandle> MapLocator::Impl::refreshAsyncYoloState(const cv::Mat& minimap, TimePoint now)
{
    if (!zoneClassifier || !zoneClassifier->isLoaded()) {
        return std::nullopt;
    }

    std::unique_lock<std::mutex> lock(taskMutex, std::try_to_lock);
    if (!lock.owns_lock()) {
        return std::nullopt;
    }

    if (asyncYoloState.has_value() && asyncYoloState->future.valid()) {
        const bool isReady = asyncYoloState->future.wait_for(std::chrono::seconds(0)) == std::future_status::ready;
        if (isReady && !asyncYoloState->zoneGuardApplied) {
            const YoloCoarseResult predicted = asyncYoloState->future.get();
            if (predicted.valid && !predicted.is_none && !predicted.zone_id.empty() && !currentZoneId.empty()
                && predicted.zone_id != currentZoneId) {
                LogInfo << "Async YOLO detected zone change: " << currentZoneId << " -> " << predicted.zone_id;
                motionTracker->forceLost();
                stablePosition.reset();
            }
            asyncYoloState->zoneGuardApplied = true;
        }

        if (asyncYoloState->frameId == activeFrameId) {
            return AsyncYoloHandle { .frameId = asyncYoloState->frameId, .future = asyncYoloState->future };
        }
        if (!isReady) {
            return std::nullopt;
        }
    }

    const auto elapsed = std::chrono::duration_cast<std::chrono::seconds>(now - lastYoloCheckTime).count();
    if (elapsed < 3) {
        return std::nullopt;
    }

    // 限制频次：YOLO CPU 推理存在开销，区域大范围切换并非瞬发，降低频率足以应对漂移容错并显著降低资源负担
    lastYoloCheckTime = now;
    cv::Mat yoloInput = minimap.clone();
    auto future = std::async(std::launch::async, [this, yoloInput]() { return zoneClassifier->predictCoarseByYOLO(yoloInput); }).share();
    asyncYoloState = AsyncYoloState { .frameId = activeFrameId, .future = future };
    return AsyncYoloHandle { .frameId = activeFrameId, .future = std::move(future) };
}

std::optional<LocateResult> MapLocator::Impl::tryTrackingLocate(
    const cv::Mat& minimap,
    const LocateOptions& options,
    const std::string& expectedZoneId,
    TimePoint now,
    FrameTemplateFeatureCache& featureCache,
    std::optional<AsyncYoloHandle>* sameFrameYolo,
    bool yoloRefreshAlreadyPerformed,
    const std::function<void()>& slowPathSignal)
{
    if (options.force_global_search) {
        return std::nullopt;
    }
    if (!motionTracker) {
        return std::nullopt;
    }

    if (!yoloRefreshAlreadyPerformed) {
        const std::optional<AsyncYoloHandle> refreshedYolo = refreshAsyncYoloState(minimap, now);
        if (sameFrameYolo && !sameFrameYolo->has_value() && refreshedYolo.has_value()) {
            *sameFrameYolo = refreshedYolo;
        }
    }

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
    const MatchFeature& trackingTmpl = featureCache.get(minimap, primaryStrategy.get());
    auto trackingResult = tryTracking(trackingTmpl, primaryStrategy.get(), now, options, slowPathSignal, &rawPrimaryPos);
    const bool trackingHeld = trackingResult.has_value() && trackingResult->isHeld;

    if (trackingResult && !trackingHeld) {
        arbiterRejectedPrimaryStreak = 0;
        arbiterRejectedPrimary.reset();
        return LocateResult {
            .status = LocateStatus::Success,
            .position = trackingResult,
            .debugMessage = "Tracking Success",
        };
    }

    const bool shouldTryDualTracking = !isPathHeatmapZone && rawPrimaryPos.score > 0.1 && (!trackingResult || trackingHeld);
    if (shouldTryDualTracking) {
        auto fallbackStrategy =
            MatchStrategyFactory::create(currentZoneId, trackingCfg, matchCfg, baseImgCfg, tierImgCfg, MatchMode::ForcePathHeatmap);
        const MatchFeature& fallbackTmpl = featureCache.get(minimap, fallbackStrategy.get());

        MapPosition rawFallbackPos {};
        // fallback 只作为 dual 互证的第二路信号；试算后立即恢复状态，避免两策略不一致时单独推进或 hold tracker
        auto savedMotionTracker = *motionTracker;
        const auto savedStablePosition = stablePosition;
        tryTracking(fallbackTmpl, fallbackStrategy.get(), now, options, {}, &rawFallbackPos);
        *motionTracker = savedMotionTracker;
        stablePosition = savedStablePosition;
        const double dist = std::hypot(rawPrimaryPos.x - rawFallbackPos.x, rawPrimaryPos.y - rawFallbackPos.y);

        // 双策略互证：两者分数均需满足最低要求，且坐标差在容差内，才视为结果可信
        if (rawPrimaryPos.score >= kDualVerifyMinScore && rawFallbackPos.score >= kDualVerifyMinScore && dist <= kDualVerifyMaxDistance) {
            LogInfo << "Dual-Mode Tracking Verified! Coords matched. Dist: " << dist;

            MapPosition verifiedPos = rawPrimaryPos;
            verifiedPos.score = std::max(rawPrimaryPos.score, rawFallbackPos.score);
            verifiedPos = acceptPosition(verifiedPos, now);
            arbiterRejectedPrimaryStreak = 0;
            arbiterRejectedPrimary.reset();

            return LocateResult {
                .status = LocateStatus::Success,
                .position = verifiedPos,
                .debugMessage = "Dual-Mode Tracking Success",
            };
        }
        if (motionTracker->getLastPos().has_value()) {
            const double predX = motionTracker->getPredictedX(now);
            const double predY = motionTracker->getPredictedY(now);
            const double distPrimaryToPred = std::hypot(rawPrimaryPos.x - predX, rawPrimaryPos.y - predY);
            const double distFallbackToPred = std::hypot(rawFallbackPos.x - predX, rawFallbackPos.y - predY);
            const bool primaryCloser = distPrimaryToPred <= distFallbackToPred;
            MapPosition arbitrated = primaryCloser ? rawPrimaryPos : rawFallbackPos;
            const double arbitratedDistToPred = primaryCloser ? distPrimaryToPred : distFallbackToPred;
            if (arbitratedDistToPred <= kTrackingOutlierDistance) {
                // 主策略被判"离预测远"却一直指着同一处、分数也不比兜底低：预测已被喂到错的位置，
                // 再按预测选下去永远出不来。兜底分数更高时不算证据，那种局面本就该信兜底
                if (!primaryCloser) {
                    const bool primaryEvidenced = rawPrimaryPos.score >= rawFallbackPos.score;
                    const bool selfConsistent =
                        primaryEvidenced && arbiterRejectedPrimary.has_value()
                        && std::hypot(rawPrimaryPos.x - arbiterRejectedPrimary->x, rawPrimaryPos.y - arbiterRejectedPrimary->y)
                               <= kArbiterReclaimDriftDistance;
                    arbiterRejectedPrimaryStreak = selfConsistent ? arbiterRejectedPrimaryStreak + 1 : (primaryEvidenced ? 1 : 0);
                    arbiterRejectedPrimary = primaryEvidenced ? std::optional<MapPosition>(rawPrimaryPos) : std::nullopt;
                    if (arbiterRejectedPrimaryStreak >= kArbiterReclaimStreak) {
                        LogWarn << "Dual-Mode arbiter reclaimed by primary" << VAR(rawPrimaryPos.x) << VAR(rawPrimaryPos.y)
                                << VAR(rawPrimaryPos.score) << VAR(rawFallbackPos.score) << VAR(distPrimaryToPred)
                                << VAR(distFallbackToPred);
                        arbiterRejectedPrimaryStreak = 0;
                        arbiterRejectedPrimary.reset();
                        // 先 markLost 让 update 跳过速度 EMA，否则这次几十像素的修正会被当成一次高速位移
                        motionTracker->markLost(1);
                        MapPosition reclaimed = rawPrimaryPos;
                        reclaimed.isHeld = false;
                        reclaimed = acceptPosition(reclaimed, now);
                        motionTracker->clearVelocity();
                        return LocateResult {
                            .status = LocateStatus::Success,
                            .position = reclaimed,
                            .debugMessage = "Dual-Mode Arbiter Reclaimed",
                        };
                    }
                }
                else {
                    arbiterRejectedPrimaryStreak = 0;
                    arbiterRejectedPrimary.reset();
                }
                arbitrated.isHeld = false;
                LogInfo << "Dual-Mode arbitrated by motion continuity" << VAR(distPrimaryToPred) << VAR(distFallbackToPred)
                        << VAR(arbitrated.x) << VAR(arbitrated.y) << VAR(arbitrated.score) << VAR(dist);
                MapPosition accepted = acceptPosition(arbitrated, now);
                return LocateResult {
                    .status = LocateStatus::Success,
                    .position = accepted,
                    .debugMessage = "Dual-Mode Motion Arbitrated",
                };
            }
        }
        LogInfo << "Dual-Mode Tracking rejected" << VAR(rawPrimaryPos.score) << VAR(rawPrimaryPos.x) << VAR(rawPrimaryPos.y)
                << VAR(rawFallbackPos.score) << VAR(rawFallbackPos.x) << VAR(rawFallbackPos.y) << VAR(dist);
    }

    if (!trackingHeld) {
        return std::nullopt;
    }

    return LocateResult { .status = LocateStatus::Success, .position = trackingResult, .debugMessage = "Tracking Hold" };
}

SearchConstraint MapLocator::Impl::buildSearchConstraint(
    const std::string& expectedZoneSelector,
    const std::string& targetZoneId,
    const YoloCoarseResult& coarse,
    bool emitLog) const
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
        if (emitLog) {
            LogInfo << "YOLO validated path-heatmap zone; using full-map direct fine search." << VAR(expectedZoneSelector)
                    << VAR(coarse.raw_class) << VAR(targetZoneId);
        }
        return constraint;
    }

    if (!coarse.has_roi) {
        constraint.mode = GlobalSearchMode::FullMapFine;
        if (emitLog) {
            LogInfo << "YOLO validated zone without ROI mapping; using full-map direct fine search." << VAR(expectedZoneSelector)
                    << VAR(coarse.raw_class) << VAR(targetZoneId);
        }
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
    if (emitLog) {
        LogInfo << "YOLO constrained global search to ROI" << VAR(expectedZoneSelector) << VAR(coarse.raw_class) << VAR(targetZoneId)
                << VAR(constraint.roi.x) << VAR(constraint.roi.y) << VAR(constraint.roi.width) << VAR(constraint.roi.height);
    }
    return constraint;
}

GlobalSearchCandidates MapLocator::Impl::startGlobalSearchCandidates(
    const cv::Mat& minimap,
    const std::string& targetZoneId,
    const SearchConstraint& constraint,
    FrameTemplateFeatureCache featureCache,
    std::uint64_t searchFrameId,
    const TrackingConfig& trackingConfig,
    const MatchConfig& matchConfig,
    const ImageProcessingConfig& baseImageConfig,
    const ImageProcessingConfig& tierImageConfig,
    bool reserveTrackingCapacity)
{
    GlobalSearchCandidates candidates {
        .frameId = searchFrameId,
        .targetZoneId = targetZoneId,
        .constraint = constraint,
    };
    const bool isPathHeatmapZone = IsPathHeatmapZone(targetZoneId);
    const unsigned hardwareThreads = std::max(1U, std::thread::hardware_concurrency());
    const bool canSpeculateDualMode = !isPathHeatmapZone && constraint.yolo_validated && hardwareThreads >= 8;

    auto startSearch = [this,
                        &minimap,
                        &constraint,
                        &targetZoneId,
                        &featureCache,
                        &trackingConfig,
                        &matchConfig,
                        &baseImageConfig,
                        &tierImageConfig,
                        reserveTrackingCapacity](MatchMode mode, ScaleTaskPriority priority) -> GlobalSearchComputation {
        auto uniqueStrategy =
            MatchStrategyFactory::create(targetZoneId, trackingConfig, matchConfig, baseImageConfig, tierImageConfig, mode);
        if (!uniqueStrategy) {
            return {};
        }

        std::shared_ptr<IMatchStrategy> strategy = std::move(uniqueStrategy);
        const MatchFeature& globalTmpl = featureCache.get(minimap, strategy.get());
        return startGlobalSearch(globalTmpl, std::move(strategy), targetZoneId, constraint, priority, reserveTrackingCapacity);
    };

    candidates.primary = startSearch(MatchMode::Auto, ScaleTaskPriority::GlobalPrimary);
    if (!isPathHeatmapZone) {
        auto uniqueFallbackStrategy = MatchStrategyFactory::create(
            targetZoneId,
            trackingConfig,
            matchConfig,
            baseImageConfig,
            tierImageConfig,
            MatchMode::ForcePathHeatmap);
        if (uniqueFallbackStrategy) {
            std::shared_ptr<IMatchStrategy> fallbackStrategy = std::move(uniqueFallbackStrategy);
            MatchFeature fallbackTemplate = featureCache.get(minimap, fallbackStrategy.get());
            candidates.deferredFallback = [this,
                                           fallbackTemplate = std::move(fallbackTemplate),
                                           fallbackStrategy = std::move(fallbackStrategy),
                                           targetZoneId,
                                           constraint,
                                           reserveTrackingCapacity]() mutable {
                return startGlobalSearch(
                    fallbackTemplate,
                    fallbackStrategy,
                    targetZoneId,
                    constraint,
                    ScaleTaskPriority::GlobalFallback,
                    reserveTrackingCapacity);
            };
            if (canSpeculateDualMode) {
                candidates.fallback = candidates.deferredFallback();
                candidates.deferredFallback = {};
            }
        }
    }
    return candidates;
}

std::optional<MapPosition> MapLocator::Impl::finishGlobalSearchCandidates(GlobalSearchCandidates candidates, MapPosition* outBestRaw)
{
    const bool isPathHeatmapZone = IsPathHeatmapZone(candidates.targetZoneId);

    // Speculative global work stays deliberately narrow while Tracking may still
    // win the frame. Once the original control flow reaches Global, let the
    // batch use the full executor because there is no foreground Tracking work
    // left to protect.
    if (candidates.primary.batch) {
        candidates.primary.batch->releaseTrackingCapacity(*scaleExecutor);
    }
    if (candidates.fallback.has_value() && candidates.fallback->batch) {
        candidates.fallback->batch->releaseTrackingCapacity(*scaleExecutor);
    }

    GlobalSearchAttempt primaryAttempt = finishGlobalSearch(candidates.primary);
    auto globalResult = primaryAttempt.result;
    const MapPosition& rawGlobalPrimaryPos = primaryAttempt.rawPos;

    if (outBestRaw) {
        *outBestRaw = rawGlobalPrimaryPos;
    }

    const bool shouldTryDualMode =
        !globalResult && !isPathHeatmapZone && (candidates.constraint.yolo_validated || rawGlobalPrimaryPos.score > 0.1);
    if (!shouldTryDualMode) {
        if (candidates.fallback.has_value() && candidates.fallback->batch) {
            candidates.fallback->batch->cancel(*scaleExecutor);
        }
        return globalResult;
    }

    GlobalSearchAttempt fallbackAttempt;
    if (candidates.fallback.has_value() && candidates.fallback->batch) {
        candidates.fallback->batch->promote(*scaleExecutor);
        fallbackAttempt = finishGlobalSearch(*candidates.fallback);
    }
    else {
        GlobalSearchComputation synchronousFallback;
        if (candidates.deferredFallback) {
            synchronousFallback = candidates.deferredFallback();
            if (synchronousFallback.batch) {
                synchronousFallback.batch->releaseTrackingCapacity(*scaleExecutor);
                synchronousFallback.batch->promote(*scaleExecutor);
            }
        }
        fallbackAttempt = finishGlobalSearch(synchronousFallback);
    }

    auto fallbackResult = fallbackAttempt.result;
    const MapPosition& rawGlobalFallbackPos = fallbackAttempt.rawPos;
    const double dist = std::hypot(rawGlobalPrimaryPos.x - rawGlobalFallbackPos.x, rawGlobalPrimaryPos.y - rawGlobalFallbackPos.y);

    if (outBestRaw && rawGlobalFallbackPos.score > rawGlobalPrimaryPos.score) {
        *outBestRaw = rawGlobalFallbackPos;
    }

    // 双策略验证：正常图传和梯度图传独立得出的坐标若极度相近，且双方分数都过线，才视为互证。
    if (rawGlobalPrimaryPos.score >= kDualGlobalVerifyMinScore && rawGlobalFallbackPos.score >= kDualGlobalVerifyMinScore
        && dist <= kDualVerifyMaxDistance) {
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

std::optional<MapPosition> MapLocator::Impl::tryGlobalSearchWithFallback(
    const cv::Mat& minimap,
    const std::string& targetZoneId,
    const SearchConstraint& constraint,
    FrameTemplateFeatureCache& featureCache,
    MapPosition* outBestRaw)
{
    GlobalSearchCandidates candidates = startGlobalSearchCandidates(
        minimap,
        targetZoneId,
        constraint,
        featureCache,
        activeFrameId,
        trackingCfg,
        matchCfg,
        baseImgCfg,
        tierImgCfg,
        false);
    return finishGlobalSearchCandidates(std::move(candidates), outBestRaw);
}

LocateResult MapLocator::Impl::locate(const cv::Mat& minimap, const LocateOptions& options)
{
    const auto now = std::chrono::steady_clock::now();
    activeFrameId = ++frameId;

    if (!isInitialized) {
        return LocateResult { .status = LocateStatus::NotInitialized, .debugMessage = "MapLocator not initialized." };
    }

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
    FrameTemplateFeatureCache featureCache;
    std::optional<AsyncYoloHandle> sameFrameYolo;
    const std::string expectedZoneSelector = options.expected_zone_id;
    const std::string expectedZoneId = NormalizeExpectedZoneId(expectedZoneSelector, zoneClassifier.get());
    std::shared_ptr<FrameSearchCoordinator> frameSearchCoordinator;
    FrameSearchCancelGuard frameSearchCancelGuard(&frameSearchCoordinator);
    bool periodicYoloRefreshPerformed = false;
    auto ensureFrameSearch = [&](bool allowPeriodicRefresh, bool reserveTrackingCapacity) {
        if (frameSearchCoordinator || !scaleExecutor || scaleExecutor->workerCount() < 6) {
            return;
        }

        if ((!sameFrameYolo.has_value() || sameFrameYolo->frameId != activeFrameId) && allowPeriodicRefresh) {
            sameFrameYolo = refreshAsyncYoloState(minimap, now);
            periodicYoloRefreshPerformed = true;
        }

        const std::uint64_t searchFrameId = activeFrameId;
        cv::Mat frameMinimap = minimap.clone();
        FrameSearchCoordinator::CoarseFunction predictForFrame;
        if (sameFrameYolo.has_value() && sameFrameYolo->frameId == searchFrameId && sameFrameYolo->future.valid()) {
            const std::shared_future<YoloCoarseResult> future = sameFrameYolo->future;
            predictForFrame = [future]() {
                return future.get();
            };
        }
        else {
            cv::Mat yoloInput = frameMinimap;
            predictForFrame = [this, yoloInput = std::move(yoloInput)]() {
                return predictCoarse(yoloInput);
            };
        }
        FrameTemplateFeatureCache frameFeatureCache = featureCache;
        const TrackingConfig trackingConfig = trackingCfg;
        const MatchConfig matchConfig = matchCfg;
        const ImageProcessingConfig baseImageConfig = baseImgCfg;
        const ImageProcessingConfig tierImageConfig = tierImgCfg;
        auto compute = [this,
                        frameMinimap = std::move(frameMinimap),
                        frameFeatureCache = std::move(frameFeatureCache),
                        expectedZoneSelector,
                        expectedZoneId,
                        searchFrameId,
                        trackingConfig,
                        matchConfig,
                        baseImageConfig,
                        tierImageConfig,
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

            const SearchConstraint constraint = buildSearchConstraint(expectedZoneSelector, targetZoneId, coarse, false);
            if (coarse.valid && !coarse.is_none && !constraint.yolo_validated) {
                return std::nullopt;
            }
            const bool isPathHeatmapZone = IsPathHeatmapZone(targetZoneId);
            if (coarse.valid && !coarse.is_none && coarse.has_roi && !isPathHeatmapZone && constraint.mode != GlobalSearchMode::RoiFine) {
                return std::nullopt;
            }

            return startGlobalSearchCandidates(
                frameMinimap,
                targetZoneId,
                constraint,
                frameFeatureCache,
                searchFrameId,
                trackingConfig,
                matchConfig,
                baseImageConfig,
                tierImageConfig,
                reserveTrackingCapacity);
        };

        frameSearchCoordinator = std::make_shared<FrameSearchCoordinator>(std::move(predictForFrame), *scaleExecutor, std::move(compute));
        frameSearchCoordinator->start();
    };

    const int trackingLostLimit = IsPathHeatmapZone(currentZoneId) ? 10 : options.max_lost_frames;
    const bool trackerUnavailable = !motionTracker || currentZoneId.empty() || !motionTracker->isTracking(trackingLostLimit);
    const bool expectedZoneMismatch = !expectedZoneId.empty() && currentZoneId != expectedZoneId;
    if (options.force_global_search || trackerUnavailable || expectedZoneMismatch) {
        ensureFrameSearch(!options.force_global_search && motionTracker != nullptr, false);
    }

    auto slowPathSignal = [&]() {
        ensureFrameSearch(false, true);
    };
    if (auto trackingResult = tryTrackingLocate(
            minimap,
            options,
            expectedZoneId,
            now,
            featureCache,
            &sameFrameYolo,
            periodicYoloRefreshPerformed,
            slowPathSignal)) {
        if (trackingResult->position.has_value()) {
            trackingResult->position->angle = resolveAngle();
        }
        return *trackingResult;
    }

    const double inferredAngle = resolveAngle();
    auto predictCurrentFrameCoarse = [&]() {
        if (frameSearchCoordinator) {
            return frameSearchCoordinator->getCoarse();
        }
        if (sameFrameYolo.has_value() && sameFrameYolo->frameId == activeFrameId && sameFrameYolo->future.valid()) {
            return sameFrameYolo->future.get();
        }
        return predictCoarse(minimap);
    };
    if (inferredAngle < 0.0) {
        angleGuardCoarse = predictCurrentFrameCoarse();
        LogInfo << "Angle inference failed; forcing synchronous YOLO refresh." << VAR(angleGuardCoarse->valid)
                << VAR(angleGuardCoarse->is_none) << VAR(angleGuardCoarse->zone_id);
    }

    std::string targetZoneId = expectedZoneId;
    const YoloCoarseResult coarse = angleGuardCoarse.has_value() ? *angleGuardCoarse : predictCurrentFrameCoarse();
    if (coarse.valid && coarse.is_none) {
        return LocateResult {
            .status = LocateStatus::TrackingLost,
            .debugMessage = kColdStartCollectingMessage,
        };
    }

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
    MapPosition bestRawGlobal {};
    std::optional<MapPosition> globalResult;
    bool prefetchedCandidatesConsumed = false;
    if (frameSearchCoordinator) {
        auto prefetchedCandidates = frameSearchCoordinator->takeCandidates(activeFrameId, targetZoneId, constraint);
        if (prefetchedCandidates.has_value()) {
            prefetchedCandidatesConsumed = true;
            globalResult = finishGlobalSearchCandidates(std::move(*prefetchedCandidates), &bestRawGlobal);
        }
    }
    if (!prefetchedCandidatesConsumed) {
        globalResult = tryGlobalSearchWithFallback(minimap, targetZoneId, constraint, featureCache, &bestRawGlobal);
    }
    if (!globalResult) {
        if (bestRawGlobal.score > kSeamFallbackMinPeakScore) {
            bestRawGlobal.isHeld = true;
            globalResult = bestRawGlobal;
            LogInfo << "Global gate low-confidence: releasing best raw peak (held) to avoid cold-start deadlock." << VAR(bestRawGlobal.x)
                    << VAR(bestRawGlobal.y) << VAR(bestRawGlobal.score);
        }
        else {
            motionTracker->markLost();
            if (motionTracker->getLostCount() > maxAllowedLost) {
                motionTracker->forceLost();
                stablePosition.reset();
            }
            return LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = "Global search failed." };
        }
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
            stablePosition.reset();
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
            return LocateResult { .status = LocateStatus::TrackingLost, .debugMessage = kColdStartCollectingMessage };
        }
        LogInfo << "Cold-start: consensus." << VAR(globalResult->x) << VAR(globalResult->y) << VAR(globalResult->score);
    }
    coldStartBuffer.clear();

    if (farJump || zoneChanged) {
        stablePosition.reset();
        motionTracker->clearVelocity();
        if (farJump) {
            LogInfo << "Global Search: high-conf reseed." << VAR(jumpDist) << VAR(globalResult->score);
        }
    }

    currentZoneId = globalResult->zoneId;
    globalResult->angle = inferredAngle;
    MapPosition accepted = acceptPosition(*globalResult, now);
    return LocateResult { .status = LocateStatus::Success, .position = accepted, .debugMessage = "Global Search Success" };
}

void MapLocator::Impl::resetTrackingState()
{
    if (motionTracker) {
        motionTracker->forceLost();
        motionTracker->clearVelocity();
    }
    currentZoneId = "";
    coldStartBuffer.clear();
    stablePosition.reset();
    arbiterRejectedPrimary.reset();
    arbiterRejectedPrimaryStreak = 0;
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
    const long long latencyMs = std::chrono::duration_cast<std::chrono::milliseconds>(end - start).count();
    if (res.position.has_value()) {
        res.position->latencyMs = latencyMs;
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
