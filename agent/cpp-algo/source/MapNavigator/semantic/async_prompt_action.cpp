#include "async_prompt_action.h"

#include <algorithm>
#include <filesystem>
#include <optional>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../control/controller_type_utils.h"
#include "../control/motion_controller.h"
#include "../navi_time.h"
#include "../planning/navigation_session.h"
#include "../sensing/maa_task.h"
#include "../sensing/prompt_scan_profile.h"
#include "../sensing/roi_template_scanner.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace
{

// override 会合并进节点, 所以只写调用方拥有的字段, roi/key 留在 pipeline 里
std::string build_run_override(const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected, bool rec)
{
    json::object exit_node;
    exit_node["next"] = json::array {};

    json::object root;
    root[spec.exit_node] = std::move(exit_node);

    const bool has_expected = expected != nullptr && !expected->empty();
    if (has_expected || rec) {
        json::object recognition_node = has_expected ? ExpectedTextOverride(*expected) : json::object {};
        if (rec) {
            json::object do_nothing;
            do_nothing["type"] = "DoNothing";
            recognition_node["action"] = std::move(do_nothing);
        }
        root[spec.recognition_node] = std::move(recognition_node);
    }

    return json::value(std::move(root)).dumps();
}

std::string build_pre_warm_override(const AsyncPromptActionSpec& spec)
{
    json::object do_nothing;
    do_nothing["type"] = "DoNothing";

    json::object recognition_node;
    recognition_node["action"] = std::move(do_nothing);
    recognition_node["next"] = json::array {};

    json::object exit_node;
    exit_node["next"] = json::array {};

    json::object root;
    root[spec.recognition_node] = std::move(recognition_node);
    root[spec.exit_node] = std::move(exit_node);
    return json::value(std::move(root)).dumps();
}

PromptScanProfile default_scan_profile(
    MaaContext* context,
    const AsyncPromptActionSpec& spec,
    const cv::Rect& fallback_roi,
    std::string_view controller_type)
{
    PromptScanProfile profile;
    if (spec.default_scan_node != nullptr && TryLoadPromptScanProfile(context, spec.default_scan_node, controller_type, &profile)) {
        return profile;
    }

    profile = PromptScanProfile {};
    profile.base_roi = fallback_roi;
    const std::vector<std::filesystem::path> roots = ResourceImageRoots(controller_type);
    cv::Mat templ;
    if (const std::optional<std::filesystem::path> icon_path = ResolveResourceImage(roots, kPromptIconRelativePath)) {
        templ = MAA_NS::imread(*icon_path, cv::IMREAD_GRAYSCALE);
    }
    if (templ.empty()) {
        LogError << "Prompt icon template not loaded." << VAR(spec.tag) << VAR(kPromptIconRelativePath) << VAR(DescribeRoots(roots));
        return profile;
    }
    profile.icons.push_back(ScanIcon { .templ = std::move(templ), .threshold = kPromptIconMatchThreshold });
    return profile;
}

// 空串表示用默认预筛, 节点坏了也记下名字, 免得每拍重读
void build_scanner(AsyncPromptAction* action, const std::string& scan_node)
{
    action->scanner.reset();
    action->scan_node = scan_node;

    PromptScanProfile profile;
    if (scan_node.empty()) {
        profile = default_scan_profile(action->context, action->spec, action->fallback_roi, action->controller_type);
    }
    else if (!TryLoadPromptScanProfile(action->context, scan_node, action->controller_type, &profile)) {
        LogError << "Points naming this scan node get no pre-filter; they stop on arrival instead." << VAR(action->spec.tag)
                 << VAR(scan_node);
        return;
    }
    if (profile.icons.empty()) {
        LogError << "Prompt scan template is empty; these points stop on arrival instead." << VAR(action->spec.tag) << VAR(scan_node);
        return;
    }

    LogInfo << "Async prompt scanner started." << VAR(action->spec.tag) << VAR(scan_node) << VAR(profile.base_roi.x)
            << VAR(profile.base_roi.y) << VAR(profile.base_roi.width) << VAR(profile.base_roi.height) << VAR(profile.icons.size());
    action->scanner = std::make_unique<RoiTemplateScanner>(action->spec.tag, profile.base_roi, std::move(profile.icons));
}

// 扫描节点变了才重建, 重建会一并丢掉旧闩
void ensure_scanner_for(AsyncPromptAction* action, const Waypoint* waypoint)
{
    if (waypoint == nullptr || waypoint->interact_scan == action->scan_node) {
        return;
    }
    build_scanner(action, waypoint->interact_scan);
}

bool consume_detection(AsyncPromptAction* action)
{
    return action->scanner != nullptr && action->scanner->consumeDetection();
}

// 只认当前要走到的那个点, nullptr 表示这一拍不该停车
const Waypoint* current_waypoint_of_this_kind(const AsyncPromptAction& action)
{
    if (action.session == nullptr || !action.session->hasCurrentWaypoint()) {
        return nullptr;
    }

    const Waypoint& waypoint = action.session->currentWaypoint();
    return SpecMatches(action.spec, waypoint) ? &waypoint : nullptr;
}

const Waypoint* tail_waypoint_of_this_kind(const AsyncPromptAction& action)
{
    if (action.session == nullptr) {
        return nullptr;
    }

    const std::vector<Waypoint>& path = action.session->originalPath();
    for (auto it = path.rbegin(); it != path.rend(); ++it) {
        if (!it->has_position) {
            continue;
        }
        return SpecMatches(action.spec, *it) ? &*it : nullptr;
    }
    return nullptr;
}

} // namespace

bool RunPromptSubtask(MaaContext* context, const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected, bool rec)
{
    if (context == nullptr) {
        return false;
    }

    const std::string pipeline_override = build_run_override(spec, expected, rec);
    const std::optional<bool> recognized = RunTaskAndCheckNode(context, spec.entry_node, spec.recognition_node, pipeline_override);
    if (!recognized) {
        LogWarn << "Prompt subtask produced no result." << VAR(spec.tag) << VAR(spec.entry_node);
        return false;
    }
    SleepFor(kPromptPostSleepMs);
    return *recognized;
}

AsyncPromptAction
    MakeAsyncPromptAction(const AsyncPromptActionSpec& spec, MaaContext* context, NavigationSession* session, const NaviPosition* position)
{
    AsyncPromptAction action;
    action.spec = spec;
    action.context = context;
    action.session = session;
    action.position = position;
    return action;
}

bool PromptRouteHasPoint(const AsyncPromptAction& action)
{
    if (action.session == nullptr) {
        return false;
    }
    const std::vector<Waypoint>& path = action.session->originalPath();
    return std::any_of(path.begin(), path.end(), [&action](const Waypoint& waypoint) { return SpecMatches(action.spec, waypoint); });
}

bool PromptRouteEndsWithPoint(const AsyncPromptAction& action)
{
    return tail_waypoint_of_this_kind(action) != nullptr;
}

void StartPromptAction(AsyncPromptAction* action, const cv::Rect& fallback_roi, std::string_view controller_type)
{
    if (action->started || !PromptRouteHasPoint(*action)) {
        return;
    }
    action->fallback_roi = fallback_roi;
    action->controller_type = controller_type;
    action->started = true;

    for (const Waypoint& waypoint : action->session->originalPath()) {
        if (SpecMatches(action->spec, waypoint)) {
            build_scanner(action, waypoint.interact_scan);
            break;
        }
    }
}

void StopPromptAction(AsyncPromptAction* action)
{
    action->scanner.reset();
    action->scan_node.clear();
    action->started = false;
}

void SubmitPromptFrame(AsyncPromptAction* action, const cv::Mat& frame)
{
    if (action->scanner != nullptr) {
        action->scanner->submitFrame(frame);
    }
}

void PreWarmPromptRecognition(const AsyncPromptAction& action)
{
    if (action.context == nullptr || !PromptRouteHasPoint(action)) {
        return;
    }

    LogInfo << "Pre-warming the prompt recognition model while the agent is stopped." << VAR(action.spec.tag);
    const std::string pipeline_override = build_pre_warm_override(action.spec);
    MaaContextRunTask(action.context, action.spec.entry_node, pipeline_override.c_str());
}

double NearestPromptDistanceSq(const AsyncPromptAction& action)
{
    if (!action.started || action.session == nullptr || action.position == nullptr || !action.position->valid) {
        return -1.0;
    }

    double nearest_sq = -1.0;
    for (const Waypoint& waypoint : action.session->currentPath()) {
        if (!SpecMatches(action.spec, waypoint)) {
            continue;
        }
        const double dx = waypoint.x - action.position->x;
        const double dy = waypoint.y - action.position->y;
        const double distance_sq = dx * dx + dy * dy;
        if (nearest_sq < 0.0 || distance_sq < nearest_sq) {
            nearest_sq = distance_sq;
        }
    }
    return nearest_sq;
}

PromptTriggerOutcome
    TryTriggerPromptWhileWalking(AsyncPromptAction* action, Motion* motion_controller, double waypoint_distance, size_t node_idx)
{
    if (action->context == nullptr || !action->started || motion_controller == nullptr) {
        return PromptTriggerOutcome::NotTriggered;
    }

    const auto now = std::chrono::steady_clock::now();
    if (!IsUnset(action->last_trigger_at) && now - action->last_trigger_at < std::chrono::milliseconds(kPromptScanIntervalMs)) {
        return PromptTriggerOutcome::NotTriggered;
    }

    // 提示图标是全局的, 只在自家语义点附近才停车
    const std::vector<std::string>* expected = nullptr;
    bool rec = false;
    bool in_band = false;
    if (action->spec.text_from_route) {
        // 认中就算本点走完, 所以只认正在走向的那个点
        const Waypoint* waypoint = current_waypoint_of_this_kind(*action);
        ensure_scanner_for(action, waypoint);
        in_band = waypoint != nullptr && waypoint_distance <= kPromptTriggerBandWu;
        if (in_band) {
            expected = &waypoint->interact_text;
            rec = waypoint->interact_rec;
        }
    }
    else {
        // 共用表那类按最近同类点算距离, 已走过的也算, 提示常在走过头后才弹出
        const double nearest_sq = NearestPromptDistanceSq(*action);
        in_band = nearest_sq >= 0.0 && nearest_sq <= kPromptTriggerBandWu * kPromptTriggerBandWu;
    }

    // 带外也要读闩把旧观测作废, 否则进带第一拍会拿老观测开火
    if (!consume_detection(action) || !in_band) {
        return PromptTriggerOutcome::NotTriggered;
    }

    action->last_trigger_at = now;
    LogInfo << "Async prompt flagged — stopping for authoritative recognition." << VAR(action->spec.tag) << VAR(waypoint_distance)
            << VAR(node_idx) << VAR(rec);
    HoldPosition(motion_controller);
    SleepFor(kStopWaitMs);
    if (!RunPromptSubtask(action->context, action->spec, expected, rec)) {
        // 间隔从识别结束算起, 否则常驻的别家提示会挤掉到点判定
        action->last_trigger_at = std::chrono::steady_clock::now();
        LogInfo << "Async prompt not confirmed — resuming the walk." << VAR(action->spec.tag) << VAR(waypoint_distance) << VAR(node_idx);
        return PromptTriggerOutcome::Missed;
    }
    return PromptTriggerOutcome::Recognized;
}

bool TryTriggerPromptAtRouteTail(AsyncPromptAction* action)
{
    if (action->context == nullptr || !action->started) {
        return false;
    }

    const Waypoint* waypoint = tail_waypoint_of_this_kind(*action);
    if (action->spec.text_from_route && waypoint == nullptr) {
        return false;
    }
    ensure_scanner_for(action, waypoint);
    if (!consume_detection(action)) {
        return false;
    }

    const bool rec = waypoint != nullptr && waypoint->interact_rec;
    LogInfo << "Route tail prompt flagged — running the authoritative recognition before finishing." << VAR(action->spec.tag) << VAR(rec);
    RunPromptSubtask(action->context, action->spec, waypoint != nullptr ? &waypoint->interact_text : nullptr, rec);
    return true;
}

void ForgetPromptDetection(AsyncPromptAction* action)
{
    consume_detection(action);
}

} // namespace mapnavigator
