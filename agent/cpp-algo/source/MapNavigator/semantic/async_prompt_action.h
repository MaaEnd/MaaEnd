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

struct Motion;
class RoiTemplateScanner;
struct Session;

struct AsyncPromptActionSpec
{
    ActionType action;
    const char* tag;               // 日志里的扫描器标签
    const char* entry_node;
    const char* recognition_node;  // 提供兜底 ROI, 也是预热与注入路线文本的目标
    const char* exit_node;         // 运行时清空其 next, 让子任务到此返回
    const char* default_scan_node; // 本类默认预筛的 TemplateMatch 节点, nullptr 表示用内置预筛
    bool text_from_route;          // 为假时节点自带共用文本表
};

inline bool SpecMatches(const AsyncPromptActionSpec& spec, const Waypoint& waypoint)
{
    return waypoint.action == spec.action && waypoint.has_position && (!spec.text_from_route || !waypoint.interact_text.empty());
}

inline bool CompletesWaypointOnTrigger(const AsyncPromptActionSpec& spec)
{
    return spec.text_from_route;
}

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

struct AsyncPromptAction
{
    AsyncPromptActionSpec spec;
    MaaContext* context;
    Session* session;
    const NaviPosition* position;
    cv::Rect fallback_roi {};
    std::string controller_type;
    std::string scan_node;
    std::unique_ptr<RoiTemplateScanner> scanner;
    bool started = false;
    std::chrono::steady_clock::time_point last_trigger_at {};
};

AsyncPromptAction
    MakeAsyncPromptAction(const AsyncPromptActionSpec& spec, MaaContext* context, Session* session, const NaviPosition* position);

bool PromptRouteHasPoint(const AsyncPromptAction& action);
bool PromptRouteEndsWithPoint(const AsyncPromptAction& action);

// fallback_roi 是识别节点自己的 ROI, 只有内置预筛用它
void StartPromptAction(AsyncPromptAction* action, const cv::Rect& fallback_roi, std::string_view controller_type);
void StopPromptAction(AsyncPromptAction* action);
void SubmitPromptFrame(AsyncPromptAction* action, const cv::Mat& frame);

// 只在角色停下时调用, 付掉识别模型的冷启动
void PreWarmPromptRecognition(const AsyncPromptAction& action);

// 未启动、未定位或没有剩余同类点时返回 -1
double NearestPromptDistanceSq(const AsyncPromptAction& action);

// 非 NotTriggered 表示本拍已用掉, 只有 Recognized 才让路线命名的点算走完
PromptTriggerOutcome
    TryTriggerPromptWhileWalking(AsyncPromptAction* action, Motion* motion_controller, double waypoint_distance, size_t node_idx);
// 不节流, 调用方须在路线末尾站定后调用
bool TryTriggerPromptAtRouteTail(AsyncPromptAction* action);

void ForgetPromptDetection(AsyncPromptAction* action);

} // namespace mapnavigator
