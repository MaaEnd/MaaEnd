#pragma once

#include <atomic>
#include <condition_variable>
#include <mutex>
#include <thread>

#include <opencv2/core.hpp>

namespace mapnavigator
{

// Off-thread collectible pre-filter. The single nav thread can't run a nested framework recognition mid-tick
// (it wedges the next controller IPC), so detection runs on a worker that touches ONLY OpenCV; the nav loop
// does the authoritative stop + collect RunTask once a label is flagged. Primary detector is template-matching
// the interact ICON (white ⟳ glyph); the bright-text-blob heuristic is the fallback when the icon can't load.
class CollectibleScanner
{
public:
    // base_roi: scan region in the pipeline's authored base resolution (not live-frame). Frames are cropped to
    // the rescaled region and normalized back to base_roi's size, so detection runs in one resolution-
    // independent pixel space. icon_template: grayscale, base scale; empty -> fall back to the heuristic.
    CollectibleScanner(const cv::Rect& base_roi, const cv::Mat& icon_template);
    ~CollectibleScanner();

    CollectibleScanner(const CollectibleScanner&) = delete;
    CollectibleScanner& operator=(const CollectibleScanner&) = delete;

    // Capture-chokepoint side (nav thread). Hands the collect ROI to the worker without blocking; a frame
    // arriving while the worker is busy overwrites the pending one (latest-wins).
    void SubmitFrame(const cv::Mat& frame);

    // Nav-loop side. Returns true once per detection (consume-on-read latch), so one flagged label = one
    // collect attempt; the latch holds across the collect cooldown so a detection isn't lost.
    bool ConsumeDetection();

private:
    void WorkerLoop();

    cv::Rect base_roi_;
    cv::Mat icon_template_; // grayscale; empty → fall back to the bright-text-blob heuristic
    std::thread worker_;
    std::mutex mutex_;
    std::condition_variable cv_;
    cv::Mat pending_roi_;
    bool has_pending_ = false;
    std::atomic<bool> stop_ { false };
    std::atomic<bool> detected_ { false };
};

} // namespace mapnavigator
