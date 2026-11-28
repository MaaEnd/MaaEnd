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

#include "../Backend/backend.h"
#include "../control/controller_type_utils.h"
#include "../control/motion_controller.h"
#include "../navi_log.h"
#include "../navi_time.h"
#include "../planning/navigation_session.h"
#include "../sensing/maa_task.h"
#include "../sensing/prompt_scan_profile.h"
#include "../sensing/roi_template_scanner.h"
#include "../state_machine/navigation_state_machine.h"

#include "../../utils.h"

namespace mapnavigator
{

namespace
{

// override 会合并进节点, 所以只写调用方拥有的字段, roi/key 留在 pipeline 里
std::string build_run_override(const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected, PromptRunMode mode)
{
    json::object exit_node;
    exit_node["next"] = json::array {};

    json::object root;
    root[spec.exit_node] = std::move(exit_node);

    const bool has_expected = expected != nullptr && !expected->empty();
    if (has_expected || mode == PromptRunMode::RecognizeOnly) {
        json::object recognition_node = has_expected ? ExpectedTextOverride(*expected) : json::object {};
        if (mode == PromptRunMode::RecognizeOnly) {
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
    bool loaded = false;
    if (spec.default_scan_node != nullptr) {
        loaded = TryLoadPromptScanProfile(context, spec.default_scan_node, controller_type, &profile);
    }
    if (loaded) {
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

// 扫描节点变了才重建, 重建会一并丢掉旧闩
void ensure_scanner_for(AsyncPromptAction* action, MaaContext* context, const Waypoint* waypoint)
{
    if (waypoint == nullptr || waypoint->interact_scan == action->scan_node) {
        return;
    }
    RestartPromptScanner(action, context, waypoint->interact_scan);
}

// 只认当前要走到的那个点, nullptr 表示这一拍不该停车
const Waypoint* current_waypoint_of_this_kind(const AsyncPromptActionSpec& spec, const Session* session)
{
    if (session == nullptr || !HasCurrentWaypoint(*session)) {
        return nullptr;
    }

    const Waypoint& waypoint = CurrentWaypoint(*session);
    return SpecMatches(spec, waypoint) ? &waypoint : nullptr;
}

const Waypoint* tail_waypoint_of_this_kind(const AsyncPromptActionSpec& spec, const Session* session)
{
    if (session == nullptr) {
        return nullptr;
    }

    const std::vector<Waypoint>& path = session->original_path;
    for (auto it = path.rbegin(); it != path.rend(); ++it) {
        if (!it->has_position) {
            continue;
        }
        return SpecMatches(spec, *it) ? &*it : nullptr;
    }
    return nullptr;
}

struct PromptBand
{
    const std::vector<std::string>* expected = nullptr;
    PromptRunMode mode = PromptRunMode::Act;
    bool in_band = false;
};

PromptBand band_on_route_text(const Waypoint* waypoint, double waypoint_distance)
{
    PromptBand band;
    band.in_band = waypoint != nullptr && waypoint_distance <= kPromptTriggerBandWu;
    if (band.in_band) {
        band.expected = &waypoint->interact_text;
        band.mode = PromptRunModeOf(*waypoint);
    }
    return band;
}

PromptBand band_on_nearest_point(const AsyncPromptAction& action, const Session* session, const NaviPosition* position)
{
    PromptBand band;
    const double nearest_sq = NearestPromptDistanceSq(action, session, position);
    band.in_band = nearest_sq >= 0.0 && nearest_sq <= kPromptTriggerBandWu * kPromptTriggerBandWu;
    return band;
}

bool run_prompt_subtask_holding(
    Motion* motion_controller,
    MaaContext* context,
    const AsyncPromptActionSpec& spec,
    const std::vector<std::string>* expected,
    PromptRunMode mode)
{
    HoldPosition(motion_controller);
    SleepFor(kStopWaitMs);
    return RunPromptSubtask(context, spec, expected, mode);
}

} // namespace

PromptRunMode PromptRunModeOf(const Waypoint& waypoint)
{
    return waypoint.interact_rec ? PromptRunMode::RecognizeOnly : PromptRunMode::Act;
}

bool RunPromptSubtask(MaaContext* context, const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected, PromptRunMode mode)
{
    if (context == nullptr) {
        return false;
    }

    const std::string pipeline_override = build_run_override(spec, expected, mode);
    const std::optional<bool> recognized = RunTaskAndCheckNode(context, spec.entry_node, spec.recognition_node, pipeline_override);
    if (!recognized) {
        LogWarn << "Prompt subtask produced no result." << VAR(spec.tag) << VAR(spec.entry_node);
        return false;
    }
    SleepFor(kPromptPostSleepMs);
    return *recognized;
}

AsyncPromptAction MakeAsyncPromptAction(const AsyncPromptActionSpec& spec)
{
    AsyncPromptAction action;
    action.spec = spec;
    return action;
}

bool PromptRouteHasPoint(const AsyncPromptActionSpec& spec, const Session* session)
{
    if (session == nullptr) {
        return false;
    }
    const std::vector<Waypoint>& path = session->original_path;
    return std::any_of(path.begin(), path.end(), [&spec](const Waypoint& waypoint) { return SpecMatches(spec, waypoint); });
}

bool PromptRouteEndsWithPoint(const AsyncPromptActionSpec& spec, const Session* session)
{
    return tail_waypoint_of_this_kind(spec, session) != nullptr;
}

void StartPromptAction(Navigator* nav, AsyncPromptAction* action, const cv::Rect& fallback_roi)
{
    if (action->started || !PromptRouteHasPoint(action->spec, nav->session)) {
        return;
    }
    action->fallback_roi = fallback_roi;
    action->controller_type = nav->input_backend->controller_type;
    action->started = true;

    for (const Waypoint& waypoint : nav->session->original_path) {
        if (SpecMatches(action->spec, waypoint)) {
            RestartPromptScanner(action, nav->maa_context, waypoint.interact_scan);
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

// 空串表示用默认预筛, 节点坏了也记下名字, 免得每拍重读
void RestartPromptScanner(AsyncPromptAction* action, MaaContext* context, const std::string& scan_node)
{
    action->scanner.reset();
    action->scan_node = scan_node;

    PromptScanProfile profile;
    if (scan_node.empty()) {
        profile = default_scan_profile(context, action->spec, action->fallback_roi, action->controller_type);
    }
    else if (!TryLoadPromptScanProfile(context, scan_node, action->controller_type, &profile)) {
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

bool ConsumePromptDetection(AsyncPromptAction* action)
{
    return action->scanner != nullptr && action->scanner->consumeDetection();
}

void PreWarmPromptRecognition(const Navigator& nav, const AsyncPromptAction& action)
{
    if (nav.maa_context == nullptr || !PromptRouteHasPoint(action.spec, nav.session)) {
        return;
    }

    LogInfo << "Pre-warming the prompt recognition model while the agent is stopped." << VAR(action.spec.tag);
    const std::string pipeline_override = build_pre_warm_override(action.spec);
    MaaContextRunTask(nav.maa_context, action.spec.entry_node, pipeline_override.c_str());
}

double NearestPromptDistanceSq(const AsyncPromptAction& action, const Session* session, const NaviPosition* position)
{
    if (!action.started || session == nullptr || position == nullptr || !position->valid) {
        return -1.0;
    }

    double nearest_sq = -1.0;
    for (const Waypoint& waypoint : session->current_path) {
        if (!SpecMatches(action.spec, waypoint)) {
            continue;
        }
        const double dx = waypoint.x - position->x;
        const double dy = waypoint.y - position->y;
        const double distance_sq = dx * dx + dy * dy;
        if (nearest_sq < 0.0 || distance_sq < nearest_sq) {
            nearest_sq = distance_sq;
        }
    }
    return nearest_sq;
}

PromptTriggerOutcome TryTriggerPromptWhileWalking(Navigator* nav, AsyncPromptAction* action, double waypoint_distance)
{
    if (nav->maa_context == nullptr || !action->started || nav->motion_controller == nullptr) {
        return PromptTriggerOutcome::NotTriggered;
    }

    const size_t node_idx = nav->session->current_node_idx;
    const auto now = std::chrono::steady_clock::now();
    if (!IsUnset(action->last_trigger_at) && now - action->last_trigger_at < std::chrono::milliseconds(kPromptScanIntervalMs)) {
        return PromptTriggerOutcome::NotTriggered;
    }

    // 提示图标是全局的, 只在自家语义点附近才停车
    PromptBand band;
    if (action->spec.text_from_route) {
        // 认中就算本点走完, 所以只认正在走向的那个点
        const Waypoint* waypoint = current_waypoint_of_this_kind(action->spec, nav->session);
        ensure_scanner_for(action, nav->maa_context, waypoint);
        band = band_on_route_text(waypoint, waypoint_distance);
    }
    else {
        // 共用表那类按最近同类点算距离, 已走过的也算, 提示常在走过头后才弹出
        band = band_on_nearest_point(*action, nav->session, nav->position);
    }

    // 带外也要读闩把旧观测作废, 否则进带第一拍会拿老观测开火
    const bool detected = ConsumePromptDetection(action);
    if (!detected || !band.in_band) {
        return PromptTriggerOutcome::NotTriggered;
    }

    action->last_trigger_at = now;
    LogInfo << "Async prompt flagged — stopping for authoritative recognition." << VAR(action->spec.tag) << VAR(waypoint_distance)
            << VAR(node_idx) << VAR_AS(rec, band.mode == PromptRunMode::RecognizeOnly);
    if (!run_prompt_subtask_holding(nav->motion_controller, nav->maa_context, action->spec, band.expected, band.mode)) {
        // 间隔从识别结束算起, 否则常驻的别家提示会挤掉到点判定
        action->last_trigger_at = std::chrono::steady_clock::now();
        LogInfo << "Async prompt not confirmed — resuming the walk." << VAR(action->spec.tag) << VAR(waypoint_distance) << VAR(node_idx);
        return PromptTriggerOutcome::Missed;
    }
    return PromptTriggerOutcome::Recognized;
}

bool TryTriggerPromptAtRouteTail(Navigator* nav, AsyncPromptAction* action)
{
    if (nav->maa_context == nullptr || !action->started) {
        return false;
    }

    const Waypoint* waypoint = tail_waypoint_of_this_kind(action->spec, nav->session);
    if (action->spec.text_from_route && waypoint == nullptr) {
        return false;
    }
    ensure_scanner_for(action, nav->maa_context, waypoint);
    if (!ConsumePromptDetection(action)) {
        return false;
    }

    const PromptRunMode mode = waypoint != nullptr ? PromptRunModeOf(*waypoint) : PromptRunMode::Act;
    LogInfo << "Route tail prompt flagged — running the authoritative recognition before finishing." << VAR(action->spec.tag)
            << VAR_AS(rec, mode == PromptRunMode::RecognizeOnly);
    RunPromptSubtask(nav->maa_context, action->spec, waypoint != nullptr ? &waypoint->interact_text : nullptr, mode);
    return true;
}

void ForgetPromptDetection(AsyncPromptAction* action)
{
    ConsumePromptDetection(action);
}

} // namespace mapnavigator
