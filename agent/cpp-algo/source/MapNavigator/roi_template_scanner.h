#pragma once

#include <atomic>
#include <condition_variable>
#include <mutex>
#include <string>
#include <thread>

#include <opencv2/core.hpp>

namespace mapnavigator
{

// Off-thread ROI pre-filter. The single nav thread can't run a nested framework recognition mid-tick (it
// wedges the next controller IPC), so detection runs on a worker that touches ONLY OpenCV; the nav loop just
// reacts to the flag and leaves the authoritative recognition to the pipeline subtask it then runs.
class RoiTemplateScanner
{
public:
    // base_roi: scan region in the pipeline's authored base resolution (not live-frame). Frames are cropped to
    // the rescaled region and normalized back to base_roi's size, so detection runs in one resolution-
    // independent pixel space. templ: grayscale, base scale, never empty. mask: CV_8UC1 the size of templ,
    // empty to match the whole template.
    RoiTemplateScanner(std::string tag, const cv::Rect& base_roi, const cv::Mat& templ, const cv::Mat& mask, double match_threshold);
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
    cv::Mat template_; // grayscale, base scale
    cv::Mat mask_;     // empty = whole template
    double match_threshold_;
    std::thread worker_;
    std::mutex mutex_;
    std::condition_variable cv_;
    cv::Mat pending_roi_;
    bool has_pending_ = false;
    std::atomic<bool> stop_ { false };
    std::atomic<bool> detected_ { false };
};

} // namespace mapnavigator
