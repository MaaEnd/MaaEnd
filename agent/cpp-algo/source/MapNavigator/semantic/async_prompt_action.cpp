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
std::string BuildRunOverride(const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected, bool rec)
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

std::string BuildPreWarmOverride(const AsyncPromptActionSpec& spec)
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

PromptScanProfile DefaultScanProfile(
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

} // namespace

bool RunPromptSubtask(MaaContext* context, const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected, bool rec)
{
    if (context == nullptr) {
        return false;
    }

    const std::string pipeline_override = BuildRunOverride(spec, expected, rec);
    const std::optional<bool> recognized = RunTaskAndCheckNode(context, spec.entry_node, spec.recognition_node, pipeline_override);
    if (!recognized) {
        LogWarn << "Prompt subtask produced no result." << VAR(spec.tag) << VAR(spec.entry_node);
        return false;
    }
    SleepFor(kPromptPostSleepMs);
    return *recognized;
}

AsyncPromptAction::AsyncPromptAction(
    const AsyncPromptActionSpec& spec,
    MaaContext* context,
    NavigationSession* session,
    const NaviPosition* position)
    : spec_(spec)
    , context_(context)
    , session_(session)
    , position_(position)
{
}

AsyncPromptAction::~AsyncPromptAction() = default;

bool AsyncPromptAction::RouteHasPoint() const
{
    if (session_ == nullptr) {
        return false;
    }
    const std::vector<Waypoint>& path = session_->original_path();
    return std::any_of(path.begin(), path.end(), [this](const Waypoint& waypoint) { return spec_.Matches(waypoint); });
}

bool AsyncPromptAction::RouteEndsWithPoint() const
{
    return TailWaypointOfThisKind() != nullptr;
}

void AsyncPromptAction::Start(const cv::Rect& fallback_roi, std::string_view controller_type)
{
    if (started_ || !RouteHasPoint()) {
        return;
    }
    fallback_roi_ = fallback_roi;
    controller_type_ = controller_type;
    started_ = true;

    for (const Waypoint& waypoint : session_->original_path()) {
        if (spec_.Matches(waypoint)) {
            BuildScanner(waypoint.interact_scan);
            break;
        }
    }
}

void AsyncPromptAction::Stop()
{
    scanner_.reset();
    scan_node_.clear();
    started_ = false;
}

void AsyncPromptAction::SubmitFrame(const cv::Mat& frame)
{
    if (scanner_ != nullptr) {
        scanner_->SubmitFrame(frame);
    }
}

void AsyncPromptAction::PreWarmRecognition()
{
    if (context_ == nullptr || !RouteHasPoint()) {
        return;
    }

    LogInfo << "Pre-warming the prompt recognition model while the agent is stopped." << VAR(spec_.tag);
    const std::string pipeline_override = BuildPreWarmOverride(spec_);
    MaaContextRunTask(context_, spec_.entry_node, pipeline_override.c_str());
}

double AsyncPromptAction::NearestDistanceSq() const
{
    if (!started_ || session_ == nullptr || position_ == nullptr || !position_->valid) {
        return -1.0;
    }

    double nearest_sq = -1.0;
    for (const Waypoint& waypoint : session_->current_path()) {
        if (!spec_.Matches(waypoint)) {
            continue;
        }
        const double dx = waypoint.x - position_->x;
        const double dy = waypoint.y - position_->y;
        const double distance_sq = dx * dx + dy * dy;
        if (nearest_sq < 0.0 || distance_sq < nearest_sq) {
            nearest_sq = distance_sq;
        }
    }
    return nearest_sq;
}

PromptTriggerOutcome
    AsyncPromptAction::TryTriggerWhileWalking(MotionController* motion_controller, double waypoint_distance, size_t node_idx)
{
    if (context_ == nullptr || !started_ || motion_controller == nullptr) {
        return PromptTriggerOutcome::NotTriggered;
    }

    const auto now = std::chrono::steady_clock::now();
    if (!IsUnset(last_trigger_at_) && now - last_trigger_at_ < std::chrono::milliseconds(kPromptScanIntervalMs)) {
        return PromptTriggerOutcome::NotTriggered;
    }

    // 提示图标是全局的, 只在自家语义点附近才停车
    const std::vector<std::string>* expected = nullptr;
    bool rec = false;
    bool in_band = false;
    if (spec_.text_from_route) {
        // 认中就算本点走完, 所以只认正在走向的那个点
        const Waypoint* waypoint = CurrentWaypointOfThisKind();
        EnsureScannerFor(waypoint);
        in_band = waypoint != nullptr && waypoint_distance <= kPromptTriggerBandWu;
        if (in_band) {
            expected = &waypoint->interact_text;
            rec = waypoint->interact_rec;
        }
    }
    else {
        // 共用表那类按最近同类点算距离, 已走过的也算, 提示常在走过头后才弹出
        const double nearest_sq = NearestDistanceSq();
        in_band = nearest_sq >= 0.0 && nearest_sq <= kPromptTriggerBandWu * kPromptTriggerBandWu;
    }

    // 带外也要读闩把旧观测作废, 否则进带第一拍会拿老观测开火
    if (!ConsumeDetection() || !in_band) {
        return PromptTriggerOutcome::NotTriggered;
    }

    last_trigger_at_ = now;
    LogInfo << "Async prompt flagged — stopping for authoritative recognition." << VAR(spec_.tag) << VAR(waypoint_distance) << VAR(node_idx)
            << VAR(rec);
    motion_controller->SetForwardState(false);
    SleepFor(kStopWaitMs);
    if (!RunPromptSubtask(context_, spec_, expected, rec)) {
        // 间隔从识别结束算起, 否则常驻的别家提示会挤掉到点判定
        last_trigger_at_ = std::chrono::steady_clock::now();
        LogInfo << "Async prompt not confirmed — resuming the walk." << VAR(spec_.tag) << VAR(waypoint_distance) << VAR(node_idx);
        return PromptTriggerOutcome::Missed;
    }
    return PromptTriggerOutcome::Recognized;
}

bool AsyncPromptAction::TryTriggerAtRouteTail()
{
    if (context_ == nullptr || !started_) {
        return false;
    }

    const Waypoint* waypoint = TailWaypointOfThisKind();
    if (spec_.text_from_route && waypoint == nullptr) {
        return false;
    }
    EnsureScannerFor(waypoint);
    if (!ConsumeDetection()) {
        return false;
    }

    const bool rec = waypoint != nullptr && waypoint->interact_rec;
    LogInfo << "Route tail prompt flagged — running the authoritative recognition before finishing." << VAR(spec_.tag) << VAR(rec);
    RunPromptSubtask(context_, spec_, waypoint != nullptr ? &waypoint->interact_text : nullptr, rec);
    return true;
}

void AsyncPromptAction::ForgetDetection()
{
    ConsumeDetection();
}

void AsyncPromptAction::EnsureScannerFor(const Waypoint* waypoint)
{
    if (waypoint == nullptr || waypoint->interact_scan == scan_node_) {
        return;
    }
    BuildScanner(waypoint->interact_scan);
}

// 空串表示用默认预筛, 节点坏了也记下名字, 免得每拍重读
void AsyncPromptAction::BuildScanner(const std::string& scan_node)
{
    scanner_.reset();
    scan_node_ = scan_node;

    PromptScanProfile profile;
    if (scan_node.empty()) {
        profile = DefaultScanProfile(context_, spec_, fallback_roi_, controller_type_);
    }
    else if (!TryLoadPromptScanProfile(context_, scan_node, controller_type_, &profile)) {
        LogError << "Points naming this scan node get no pre-filter; they stop on arrival instead." << VAR(spec_.tag) << VAR(scan_node);
        return;
    }
    if (profile.icons.empty()) {
        LogError << "Prompt scan template is empty; these points stop on arrival instead." << VAR(spec_.tag) << VAR(scan_node);
        return;
    }

    LogInfo << "Async prompt scanner started." << VAR(spec_.tag) << VAR(scan_node) << VAR(profile.base_roi.x) << VAR(profile.base_roi.y)
            << VAR(profile.base_roi.width) << VAR(profile.base_roi.height) << VAR(profile.icons.size());
    scanner_ = std::make_unique<RoiTemplateScanner>(spec_.tag, profile.base_roi, std::move(profile.icons));
}

bool AsyncPromptAction::ConsumeDetection()
{
    return scanner_ != nullptr && scanner_->ConsumeDetection();
}

// 只认当前要走到的那个点, nullptr 表示这一拍不该停车
const Waypoint* AsyncPromptAction::CurrentWaypointOfThisKind() const
{
    if (session_ == nullptr || !session_->HasCurrentWaypoint()) {
        return nullptr;
    }

    const Waypoint& waypoint = session_->CurrentWaypoint();
    return spec_.Matches(waypoint) ? &waypoint : nullptr;
}

const Waypoint* AsyncPromptAction::TailWaypointOfThisKind() const
{
    if (session_ == nullptr) {
        return nullptr;
    }

    const std::vector<Waypoint>& path = session_->original_path();
    for (auto it = path.rbegin(); it != path.rend(); ++it) {
        if (!it->HasPosition()) {
            continue;
        }
        return spec_.Matches(*it) ? &*it : nullptr;
    }
    return nullptr;
}

} // namespace mapnavigator
