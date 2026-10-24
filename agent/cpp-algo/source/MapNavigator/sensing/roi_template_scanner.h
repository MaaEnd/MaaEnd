#pragma once

#include <atomic>
#include <condition_variable>
#include <mutex>
#include <string>
#include <string_view>
#include <thread>
#include <vector>

#include <opencv2/core.hpp>

namespace mapnavigator
{

// One icon a pre-filter looks for. templ: grayscale, base scale, never empty. mask: CV_8UC1 the size of templ,
// empty to match the whole template.
struct ScanIcon
{
    cv::Mat templ;
    cv::Mat mask;
    double threshold = 0.0;
};

// Off-thread ROI pre-filter. The single nav thread can't run a nested framework recognition mid-tick (it
// wedges the next controller IPC), so detection runs on a worker that touches ONLY OpenCV; the nav loop just
// reacts to the flag and leaves the authoritative recognition to the pipeline subtask it then runs.
class RoiTemplateScanner
{
public:
    // base_roi: scan region in the pipeline's authored base resolution (not live-frame). Frames are cropped to
    // the rescaled region and normalized back to base_roi's size, so detection runs in one resolution-
    // independent pixel space. Any one of icons matching counts as a detection.
    RoiTemplateScanner(std::string tag, const cv::Rect& base_roi, std::vector<ScanIcon> icons);
    ~RoiTemplateScanner();

    RoiTemplateScanner(const RoiTemplateScanner&) = delete;
    RoiTemplateScanner& operator=(const RoiTemplateScanner&) = delete;

    // Capture-chokepoint side (nav thread). Hands the ROI to the worker without blocking; a frame arriving
    // while the worker is busy overwrites the pending one (latest-wins).
    void SubmitFrame(const cv::Mat& frame);

    // Nav-loop side. Returns true once per detection (consume-on-read latch), so one flagged hit = one
    // attempt; the latch holds across the caller's cooldown so a detection isn't lost.
    bool ConsumeDetection();

private:
    void WorkerLoop();

    std::string tag_;
    cv::Rect base_roi_;
    std::vector<ScanIcon> icons_;
    std::thread worker_;
    std::mutex mutex_;
    std::condition_variable cv_;
    cv::Mat pending_roi_;
    bool has_pending_ = false;
    std::atomic<bool> stop_ { false };
    std::atomic<bool> detected_ { false };
};

// One-shot match for callers that pace their own probing on frames they already hold (FIND's stop pre-filter):
// crops the authored ROI, normalizes it back to the authored size and runs the same matcher as the worker, with
// no thread and no latch. Returns the hit directly; the caller owns pacing and reaction.
bool MatchTemplateOnFrame(const cv::Mat& frame, const cv::Rect& base_roi, const std::vector<ScanIcon>& icons, std::string_view tag);

} // namespace mapnavigator
