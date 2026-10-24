#pragma once

#include <chrono>
#include <memory>
#include <string>
#include <string_view>
#include <vector>

#include <opencv2/core.hpp>

#include "../navi_config.h"
#include "../navi_domain_types.h"

struct MaaContext;

namespace mapnavigator
{

class MotionController;
class RoiTemplateScanner;
struct NavigationSession;

struct AsyncPromptActionSpec
{
    ActionType action;
    const char* tag;               // 日志里的扫描器标签
    const char* entry_node;
    const char* recognition_node;  // 提供兜底 ROI, 也是预热与注入路线文本的目标
    const char* exit_node;         // 运行时清空其 next, 让子任务到此返回
    const char* default_scan_node; // 本类默认预筛的 TemplateMatch 节点, nullptr 表示用内置预筛
    bool text_from_route;          // 为假时节点自带共用文本表

    bool Matches(const Waypoint& waypoint) const
    {
        return waypoint.action == action && waypoint.HasPosition() && (!text_from_route || !waypoint.interact_text.empty());
    }

    bool CompletesWaypointOnTrigger() const { return text_from_route; }
};

inline constexpr AsyncPromptActionSpec kCollectPromptSpec {
    .action = ActionType::COLLECT,
    .tag = "collect",
    .entry_node = kCollectEntryNode,
    .recognition_node = kCollectRecognitionNode,
    .exit_node = kCollectExitNode,
    .default_scan_node = nullptr,
    .text_from_route = false,
};

inline constexpr AsyncPromptActionSpec kInteractPromptSpec {
    .action = ActionType::INTERACT,
    .tag = "interact",
    .entry_node = kInteractEntryNode,
    .recognition_node = kInteractRecognitionNode,
    .exit_node = kInteractExitNode,
    .default_scan_node = kInteractScanNode,
    .text_from_route = true,
};

// expected 为空时沿用节点自带文本表, rec 为真时把节点动作换成 DoNothing 只做识别
bool RunPromptSubtask(MaaContext* context, const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected, bool rec);

enum class PromptTriggerOutcome
{
    NotTriggered,
    Missed,
    Recognized,
};

class AsyncPromptAction
{
public:
    AsyncPromptAction(const AsyncPromptActionSpec& spec, MaaContext* context, NavigationSession* session, const NaviPosition* position);
    ~AsyncPromptAction();

    AsyncPromptAction(const AsyncPromptAction&) = delete;
    AsyncPromptAction& operator=(const AsyncPromptAction&) = delete;

    const AsyncPromptActionSpec& spec() const { return spec_; }

    bool armed() const { return started_; }

    bool RouteHasPoint() const;
    bool RouteEndsWithPoint() const;

    // fallback_roi 是识别节点自己的 ROI, 只有内置预筛用它
    void Start(const cv::Rect& fallback_roi, std::string_view controller_type);
    void Stop();
    void SubmitFrame(const cv::Mat& frame);

    // 只在角色停下时调用, 付掉识别模型的冷启动
    void PreWarmRecognition();

    // 未启动、未定位或没有剩余同类点时返回 -1
    double NearestDistanceSq() const;

    // 非 NotTriggered 表示本拍已用掉, 只有 Recognized 才让路线命名的点算走完
    PromptTriggerOutcome TryTriggerWhileWalking(MotionController* motion_controller, double waypoint_distance, size_t node_idx);
    // 不节流, 调用方须在路线末尾站定后调用
    bool TryTriggerAtRouteTail();

    void ForgetDetection();

private:
    const Waypoint* CurrentWaypointOfThisKind() const;
    const Waypoint* LastWaypointOfThisKind() const;

    // 扫描节点变了才重建, 重建会一并丢掉旧闩
    void EnsureScannerFor(const Waypoint* waypoint);
    void BuildScanner(const std::string& scan_node);
    bool ConsumeDetection();

    AsyncPromptActionSpec spec_;
    MaaContext* context_;
    NavigationSession* session_;
    const NaviPosition* position_;
    cv::Rect fallback_roi_ {};
    std::string controller_type_;
    std::string scan_node_;
    std::unique_ptr<RoiTemplateScanner> scanner_;
    bool started_ = false;
    std::chrono::steady_clock::time_point last_trigger_at_ {};
};

} // namespace mapnavigator
