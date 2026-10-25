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

// templ 为基础尺度灰度图且非空, mask 为同尺寸 CV_8UC1, 空表示整张模板参与匹配
struct ScanIcon
{
    cv::Mat templ;
    cv::Mat mask;
    double threshold = 0.0;
};

// 导航线程中途嵌套框架识别会卡住下一次控制器 IPC, 所以检测放在只碰 OpenCV 的后台线程
class RoiTemplateScanner
{
public:
    // base_roi 是 pipeline 基础分辨率下的坐标, 任一图标命中即算检测到
    RoiTemplateScanner(std::string tag, const cv::Rect& base_roi, std::vector<ScanIcon> icons);
    ~RoiTemplateScanner();

    RoiTemplateScanner(const RoiTemplateScanner&) = delete;
    RoiTemplateScanner& operator=(const RoiTemplateScanner&) = delete;

    // 不阻塞, 后台线程忙时新帧覆盖待处理帧
    void submitFrame(const cv::Mat& frame);

    // 每次检测只返回一次 true, 读即清除
    bool consumeDetection();

private:
    void workerLoop();

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

// 同步一次性匹配, 判据与后台线程相同, 不锁存
bool MatchTemplateOnFrame(const cv::Mat& frame, const cv::Rect& base_roi, const std::vector<ScanIcon>& icons, std::string_view tag);

} // namespace mapnavigator
