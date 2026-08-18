#include "async_prompt_action.h"

#include <algorithm>
#include <filesystem>
#include <string>
#include <string_view>
#include <utility>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "motion_controller.h"
#include "navi_math.h"
#include "navigation_session.h"
#include "prompt_scan_profile.h"
#include "roi_template_scanner.h"

#include "../utils.h"

namespace mapnavigator
{

namespace
{

// override 是合并进节点而非替换, 所以只写调用方拥有的两件事: 截断共用出口的 next(子任务到此返回), 以及
// 路线给的文本。roi/action/key 一律留在 pipeline 里。
std::string BuildRunOverride(const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected)
{
    json::object exit_node;
    exit_node["next"] = json::array {};

    json::object root;
    root[spec.exit_node] = std::move(exit_node);

    if (expected != nullptr && !expected->empty()) {
        json::array expected_list;
        for (const std::string& text : *expected) {
            expected_list.emplace_back(text);
        }
        json::object param;
        param["expected"] = std::move(expected_list);
        json::object recognition;
        recognition["param"] = std::move(param);
        json::object recognition_node;
        recognition_node["recognition"] = std::move(recognition);
        root[spec.recognition_node] = std::move(recognition_node);
    }

    return json::value(std::move(root)).dumps();
}

// 同一个入口, 但识别节点什么都不做: 这趟只为把模型的冷启动付掉
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

// 出厂预筛: 先读本类自己的 scan 节点, 老资源里没有这个节点时回落到内置图标与识别节点自己的 ROI。
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
    profile.threshold = kPromptIconMatchThreshold;
    const std::filesystem::path icon_path = std::filesystem::absolute(get_exe_dir() / ".." / kPromptIconRelativePath);
    profile.templ = MAA_NS::imread(icon_path, cv::IMREAD_GRAYSCALE);
    if (profile.templ.empty()) {
        LogError << "Prompt icon template not loaded." << VAR(spec.tag) << VAR(MAA_NS::path_to_utf8_string(icon_path));
    }
    return profile;
}

} // namespace

void RunPromptSubtask(MaaContext* context, const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected)
{
    if (context == nullptr) {
        return;
    }

    const std::string pipeline_override = BuildRunOverride(spec, expected);
    const MaaTaskId sub_id = MaaContextRunTask(context, spec.entry_node, pipeline_override.c_str());
    if (sub_id == MaaInvalidId) {
        LogWarn << "Prompt subtask failed to dispatch." << VAR(spec.tag) << VAR(spec.entry_node);
        return;
    }
    utils::SleepFor(kPromptPostSleepMs);
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
    return LastWaypointOfThisKind() != nullptr;
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
    // 疾跑抑制按 NearestDistanceSq 逐拍算, 不整条线压掉: 两个点之间照样跑
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

bool AsyncPromptAction::TryTriggerWhileWalking(MotionController* motion_controller, double waypoint_distance, size_t node_idx)
{
    if (context_ == nullptr || !started_ || motion_controller == nullptr) {
        return false;
    }

    const auto now = std::chrono::steady_clock::now();
    if (last_trigger_at_.time_since_epoch().count() != 0 && now - last_trigger_at_ < std::chrono::milliseconds(kPromptScanIntervalMs)) {
        return false;
    }

    // 提示图标是全局的, 路过任何可交互物都会闪, 所以只在自家语义点附近才动手, 带宽同走路模式的激活带。
    const std::vector<std::string>* expected = nullptr;
    bool in_band = false;
    if (spec_.text_from_route) {
        // 这一下命中就把点算走完, 所以还要求正走向的就是它: 蹭到别家的提示会静默消掉本点
        const Waypoint* waypoint = CurrentWaypointOfThisKind();
        EnsureScannerFor(waypoint);
        in_band = waypoint != nullptr && waypoint_distance <= kPromptTriggerBandWu;
        if (in_band) {
            expected = &waypoint->interact_text;
        }
    }
    else {
        // 共用表那类不消费路点, 所以按离最近的同类点多远来算, 已走过的点也算: 提示常常是走过头才弹出来
        const double nearest_sq = NearestDistanceSq();
        in_band = nearest_sq >= 0.0 && nearest_sq <= kPromptTriggerBandWu * kPromptTriggerBandWu;
    }

    // 无论带内带外都读闩: 带外看到的提示当场作废, 否则跨进带内的第一拍就拿老观测开火, 而那时本点的提示还没弹。
    // 松预筛停不死整条线: 点名目标那类每次误停花掉自己一个点, 共用表那类两次停车之间照样往前挪。
    if (!ConsumeDetection() || !in_band) {
        return false; // 后台没报, 这一拍零成本
    }

    last_trigger_at_ = now;
    LogInfo << "Async prompt flagged — stopping for authoritative recognition." << VAR(spec_.tag) << VAR(waypoint_distance)
            << VAR(node_idx);
    motion_controller->SetForwardState(false);
    utils::SleepFor(kStopWaitMs);
    RunPromptSubtask(context_, spec_, expected);
    return true;
}

bool AsyncPromptAction::TryTriggerAtRouteTail()
{
    if (context_ == nullptr || !started_) {
        return false;
    }

    const Waypoint* waypoint = LastWaypointOfThisKind();
    if (spec_.text_from_route && waypoint == nullptr) {
        return false;
    }
    EnsureScannerFor(waypoint);
    if (!ConsumeDetection()) {
        return false;
    }

    LogInfo << "Route tail prompt flagged — running the authoritative recognition before finishing." << VAR(spec_.tag);
    RunPromptSubtask(context_, spec_, waypoint != nullptr ? &waypoint->interact_text : nullptr);
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

// 空串 = 用出厂预筛。节点坏了也照样记下名字: 否则每一拍都会重读一遍同一个坏节点。
void AsyncPromptAction::BuildScanner(const std::string& scan_node)
{
    scanner_.reset();
    scan_node_ = scan_node;

    PromptScanProfile profile;
    if (scan_node.empty()) {
        profile = DefaultScanProfile(context_, spec_, fallback_roi_, controller_type_);
    }
    else if (!TryLoadPromptScanProfile(context_, scan_node, controller_type_, &profile)) {
        LogError << "Points naming this scan node get no pre-filter; they stop on arrival instead." << VAR(spec_.tag)
                 << VAR(scan_node);
        return;
    }
    if (profile.templ.empty()) {
        LogError << "Prompt scan template is empty; these points stop on arrival instead." << VAR(spec_.tag) << VAR(scan_node);
        return;
    }

    scanner_ = std::make_unique<RoiTemplateScanner>(spec_.tag, profile.base_roi, profile.templ, profile.mask, profile.threshold);
    LogInfo << "Async prompt scanner started." << VAR(spec_.tag) << VAR(scan_node) << VAR(profile.base_roi.x)
            << VAR(profile.base_roi.y) << VAR(profile.base_roi.width) << VAR(profile.base_roi.height) << VAR(profile.templ.cols)
            << VAR(profile.templ.rows) << VAR(profile.threshold);
}

bool AsyncPromptAction::ConsumeDetection()
{
    return scanner_ != nullptr && scanner_->ConsumeDetection();
}

// 一条线可带多个业务的交互点, 只认当前要走到的那个; nullptr = 这一拍不该停车
const Waypoint* AsyncPromptAction::CurrentWaypointOfThisKind() const
{
    if (session_ == nullptr || !session_->HasCurrentWaypoint()) {
        return nullptr;
    }

    const Waypoint& waypoint = session_->CurrentWaypoint();
    return spec_.Matches(waypoint) ? &waypoint : nullptr;
}

const Waypoint* AsyncPromptAction::LastWaypointOfThisKind() const
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
