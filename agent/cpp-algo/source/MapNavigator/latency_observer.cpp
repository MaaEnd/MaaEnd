#include "latency_observer.h"

#include <mutex>
#include <optional>

#include <MaaUtils/Logger.h>

#include "notice.h"

namespace mapnavigator
{

namespace latency
{

namespace
{

std::mutex g_mutex;
Detector g_detector;

int64_t StageMs(const Verdict& verdict, Stage stage)
{
    return verdict.stage_median_ms[static_cast<std::size_t>(stage)];
}

void Emit(MaaContext* context, const Verdict& verdict)
{
    const int64_t tick_median_ms = verdict.tick_median_ms;
    const int64_t screencap_ms = StageMs(verdict, Stage::Screencap);
    const int64_t image_prep_ms = StageMs(verdict, Stage::ImagePrep);
    const int64_t locate_ms = StageMs(verdict, Stage::Locate);
    const int64_t steer_ms = StageMs(verdict, Stage::Steer);
    const int64_t other_ms = StageMs(verdict, Stage::Other);
    const int slow_percent = verdict.slow_percent;
    const int64_t sample_count = verdict.sample_count;
    LogInfo << "Navigation tick latency looks abnormal." << VAR(tick_median_ms) << VAR(screencap_ms) << VAR(image_prep_ms)
            << VAR(locate_ms) << VAR(steer_ms) << VAR(other_ms) << VAR(slow_percent) << VAR(sample_count);

    notice::Publish(
        context,
        notice::Text("navigation.latency_slow", { tick_median_ms, screencap_ms, image_prep_ms, locate_ms, steer_ms, other_ms }));
}

} // namespace

void BeginRun()
{
    const std::lock_guard<std::mutex> guard(g_mutex);
    g_detector.BeginRun();
}

void RecordStage(Stage stage, int64_t elapsed_ms)
{
    const std::lock_guard<std::mutex> guard(g_mutex);
    g_detector.RecordStage(stage, elapsed_ms);
}

void RecordTick(MaaContext* context, uint64_t tick_seq, int64_t tick_gap_ms)
{
    std::optional<Verdict> verdict;
    {
        const std::lock_guard<std::mutex> guard(g_mutex);
        verdict = g_detector.RecordTick(tick_seq, tick_gap_ms);
    }
    if (verdict) {
        Emit(context, *verdict);
    }
}

} // namespace latency

} // namespace mapnavigator
