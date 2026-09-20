#include "find_action.h"

#include <chrono>
#include <cmath>
#include <cstdlib>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "action_wrapper.h"
#include "controller_type_utils.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "position_provider.h"
#include "prompt_scan_profile.h"
#include "roi_template_scanner.h"
#include "semantic_helpers.h"

#include "../utils.h"

namespace mapnavigator
{

namespace semantic_nodes
{

namespace
{

// 调用成功与命中分开: 节点不存在或框架报错要当场判失败, 不能当成"没看见"
struct FindSighting
{
    bool hit = false;
    MaaRect box {};
};

bool RunFindNode(
    MaaContext* context,
    const std::string& node,
    const std::string& pipeline_override,
    const MaaImageBuffer* image,
    FindSighting* out_sighting)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    if (tasker == nullptr) {
        LogError << "FIND: tasker is unavailable." << VAR(node);
        return false;
    }

    const MaaRecoId reco_id = MaaContextRunRecognition(context, node.c_str(), pipeline_override.c_str(), image);
    if (reco_id == MaaInvalidId) {
        LogError << "FIND: recognition failed to dispatch; check the node name and its params." << VAR(node);
        return false;
    }

    MaaBool hit = 0;
    MaaRect box {};
    if (!MaaTaskerGetRecognitionDetail(tasker, reco_id, nullptr, nullptr, &hit, &box, nullptr, nullptr, nullptr)) {
        LogError << "FIND: recognition detail is unavailable." << VAR(node) << VAR(reco_id);
        return false;
    }

    out_sighting->hit = hit != 0;
    out_sighting->box = box;
    return true;
}

// 内联文本注入内置节点: roi 与阈值留给 pipeline, 只换 expected
std::string BuildInlineTextOverride(const std::vector<std::string>& texts)
{
    json::array expected;
    for (const std::string& text : texts) {
        expected.emplace_back(text);
    }

    json::object param;
    param["expected"] = std::move(expected);
    json::object recognition;
    recognition["param"] = std::move(param);
    json::object node;
    node["recognition"] = std::move(recognition);
    json::object root;
    root[kFindInlineOcrNode] = std::move(node);
    return json::value(std::move(root)).dumps();
}

// 截图发不出去或没等到结果就直接空手而归: 读缓存会拿到旧帧, FIND 会照着过期画面走
bool CaptureFindFrame(MaaController* controller, ScopedImageBuffer* buffer)
{
    const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller);
    if (screencap_id == MaaInvalidId) {
        LogWarn << "FIND: screencap request was not posted.";
        return false;
    }
    if (MaaControllerWait(controller, screencap_id) != MaaStatus_Succeeded) {
        LogWarn << "FIND: screencap did not succeed." << VAR(screencap_id);
        return false;
    }
    return MaaControllerCachedImage(controller, buffer->Get()) && !MaaImageBufferIsEmpty(buffer->Get());
}

// 只在调用内用, 不做拷贝: MaaImageBuffer 的像素在下一帧截图前都有效
cv::Mat FrameAsMat(const MaaImageBuffer* buffer)
{
    return { MaaImageBufferHeight(buffer), MaaImageBufferWidth(buffer), MaaImageBufferType(buffer), MaaImageBufferGetRawData(buffer) };
}

int64_t ElapsedMs(std::chrono::steady_clock::time_point from, std::chrono::steady_clock::time_point now)
{
    return std::chrono::duration_cast<std::chrono::milliseconds>(now - from).count();
}

// 到位判据之二: 走到 find_arrive 附近 (半径沿用 strict 到达的判定圈)。只有写了它才读定位, 读数只用于算距离
bool ReachedArrivePoint(const Context& ctx, const Waypoint& waypoint)
{
    if (!waypoint.find_arrive.has_value()) {
        return false;
    }
    if (!ctx.position_provider->Capture(ctx.position, false, ctx.session->current_zone_id()) || ctx.position_provider->LastCaptureWasHeld()
        || ctx.position_provider->LastCaptureWasBlackScreen()) {
        return false; // 读不到就这一拍不算数, 下一步再读
    }

    const double dx = ctx.position->x - waypoint.find_arrive->at(0);
    const double dy = ctx.position->y - waypoint.find_arrive->at(1);
    const double distance = std::hypot(dx, dy);
    LogDebug << "FIND: arrive point distance." << VAR(distance) << VAR(kStrictArrivalLookaheadRadius);
    return distance <= kStrictArrivalLookaheadRadius;
}

// 退出时作废操舵记账、走廊、速度样本与路点进度: 这一段没读地图, 转视角是开环发的,
// 留下的账会让下一段拿旧朝向去操舵
void LeaveFindPhase(const Context& ctx)
{
    ctx.motion_controller->SetForwardState(false);
    ctx.runtime_state->ResetNavigationAssistState();
    ctx.runtime_state->flow.motionless_hold_ticks = 0;
    ctx.runtime_state->flow.futile_forward_reasserts = 0;
    ctx.position_provider->ResetTracking();
    ctx.session->ResetProgress();
    ctx.runtime_state->find.Reset();
}

Result FailFind(const Context& ctx, const char* reason, const char* message)
{
    LogError << message << VAR(reason) << VAR(ctx.runtime_state->find.steps);
    LeaveFindPhase(ctx);

    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;
    result.request_failure = true;
    result.failure_reason = reason;
    result.failure_log_message = message;
    return result;
}

// 停车判据能读成模板就备好预筛: 走路时用它密集盯提示, 命中才跑权威识别.
// 读不成模板 (OCR 等) 就安静退回探测时的权威识别, 只是每次探测更贵
void ArmStopProbe(const Context& ctx, const Waypoint& waypoint, FindState& find)
{
    find.stop_probe.reset();
    if (waypoint.find_stop.empty() || ctx.maa_context == nullptr) {
        return;
    }
    const std::string stop_type = ReadNodeRecognitionType(ctx.maa_context, waypoint.find_stop);
    if (!EqualsIgnoreCase(stop_type, "TemplateMatch")) {
        LogInfo << "FIND stop node has no template pre-filter; probes run it directly." << VAR(waypoint.find_stop) << VAR(stop_type);
        return;
    }
    PromptScanProfile profile;
    if (!TryLoadPromptScanProfile(ctx.maa_context, waypoint.find_stop, ctx.action_wrapper->controller_type(), &profile)) {
        return; // 读不出来的原因里面已经报过
    }
    find.stop_probe = std::move(profile);
}

// 接手: 停车、切相位、预算从这一拍开始算
Result BeginFind(const Context& ctx, const char* reason)
{
    StopMotionAndCommitment(ctx);
    utils::SleepFor(kStopWaitMs);

    FindState& find = ctx.runtime_state->find;
    find.Reset();
    find.started_at = std::chrono::steady_clock::now();
    ctx.session->UpdatePhase(NaviPhase::WaitFind, reason);

    const Waypoint& waypoint = ctx.session->CurrentWaypoint();
    ArmStopProbe(ctx, waypoint, find);
    LogInfo << "Action: FIND started." << VAR(reason) << VAR(waypoint.find_target) << VAR(waypoint.find_text.size())
            << VAR(waypoint.find_stop);

    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;
    return result;
}

// 原地转一步接着找: 第一步朝目标上次出现的那侧转, 之后保持同向, 否则正后方的目标会让镜头来回摆
bool SearchOneStep(const Context& ctx, const FindState& find)
{
    const int32_t sign = find.last_seen_side != 0 ? find.last_seen_side : find.search_sign;
    const double step_deg = kFindSearchStepDeg * static_cast<double>(sign);
    LogDebug << "FIND: target is not on screen, searching." << VAR(find.steps) << VAR(step_deg) << VAR(find.miss_streak);
    return TurnToHeadingOnce(ctx, step_deg);
}

// 框到手: 偏了就转视角, 走过了就退一步, 否则保持前进。移动连续, 修正跟着每拍的框走
bool ApproachSighting(const Context& ctx, FindState& find, const MaaRect& box, int32_t frame_width, int32_t frame_height)
{
    const int32_t center_x = box.x + box.width / 2;
    const int32_t center_y = box.y + box.height / 2;
    // 记下偏在哪一侧: 目标这拍之后消失时, 搜索要先朝这边转
    find.last_seen_side = center_x >= frame_width / 2 ? 1 : -1;
    const int32_t offset_px = center_x - frame_width / 2;
    // 判定用 1280 基准帧的像素, 角度换算用真实帧宽 (灵敏度按帧宽定义)
    const int32_t offset_base =
        static_cast<int32_t>(std::lround(static_cast<double>(offset_px) * kPipelineRoiBaseWidth / static_cast<double>(frame_width)));

    if (std::abs(offset_base) > kFindAlignTolerancePx) {
        // 小偏角边走边转; 偏太大先站定转正, 免得带着旧方向越走越偏
        const bool walk_through = std::abs(offset_base) <= kFindWalkWhileTurningPx;
        if (!walk_through) {
            ctx.motion_controller->SetForwardState(false);
            // 停下也算移动指令, 紧随其后的转向会被后端的静默期吞掉, 等过去再转
            utils::SleepFor(ctx.action_wrapper->SteeringProfile().action_quiet_period_ms);
        }
        const double degrees_per_px = kTurnDegreesPerCircle / static_cast<double>(ComputeTurn360Units(frame_width));
        const double yaw_deg = static_cast<double>(offset_px) * degrees_per_px * kFindSteerGain;
        LogDebug << "FIND: turning toward the target." << VAR(find.steps) << VAR(offset_px) << VAR(yaw_deg) << VAR(walk_through);
        const bool turned = TurnToHeadingOnce(ctx, yaw_deg);
        if (walk_through) {
            ctx.motion_controller->SetForwardState(true);
        }
        return turned;
    }

    if (static_cast<double>(center_y) > static_cast<double>(frame_height) * kFindPassedCenterYRatio) {
        LogDebug << "FIND: target is behind, stepping back." << VAR(find.steps) << VAR(center_y);
        ctx.motion_controller->SetForwardState(false);
        ctx.action_wrapper->SetMovementStateSync(false, false, true, false, kFindBackwardPulseMs);
        ctx.action_wrapper->SetMovementStateSync(false, false, false, false, 0);
        return true;
    }

    // 对准了保持前进: 移动不停, 下一拍再按新框修正
    LogDebug << "FIND: walking toward the target." << VAR(find.steps) << VAR(offset_px);
    ctx.motion_controller->SetForwardState(true);
    return true;
}

// 走路时的停车探测: 提示窗口可能就夹在两拍之间, 只靠每拍一查容易直接走过头.
// 有预筛先跑毫秒级的模板匹配, 命中才跑权威识别; 没有预筛就每次探测都跑权威识别
Result ProbeStopWhileWalking(const Context& ctx, const Waypoint& waypoint, MaaController* controller)
{
    Result result;
    FindState& find = ctx.runtime_state->find;
    if (waypoint.find_stop.empty() || controller == nullptr || !ctx.motion_controller->IsMovingForward()) {
        return result;
    }

    LogDebug << "FIND: probing the stop node while walking." << VAR(find.steps) << VAR(waypoint.find_stop)
             << VAR(find.stop_probe.has_value());
    const auto deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(kFindStopProbeWindowMs);
    while (ctx.motion_controller->IsMovingForward() && std::chrono::steady_clock::now() < deadline) {
        ScopedImageBuffer probe;
        if (!CaptureFindFrame(controller, &probe)) {
            break; // 抓不到就收手, 下一拍再盯
        }
        bool flagged = true;
        if (find.stop_probe.has_value()) {
            const PromptScanProfile& profile = *find.stop_probe;
            flagged = MatchTemplateOnFrame(
                FrameAsMat(probe.Get()),
                profile.base_roi,
                profile.templ,
                profile.mask,
                profile.threshold,
                "find-stop");
        }
        if (!flagged) {
            utils::SleepFor(kFindStopProbeIntervalMs);
            continue;
        }
        // 命中先站住: 权威确认也要时间, 走着确认会多滑出去一段
        ctx.motion_controller->SetForwardState(false);
        utils::SleepFor(kStopWaitMs);
        FindSighting sighting {};
        if (!RunFindNode(ctx.maa_context, waypoint.find_stop, "{}", probe.Get(), &sighting)) {
            return FailFind(ctx, "find_recognition_failed", "FIND stop node failed to recognize.");
        }
        if (sighting.hit) {
            LogInfo << "FIND: stop node hit while walking, target reached." << VAR(waypoint.find_stop) << VAR(find.steps);
            LeaveFindPhase(ctx);
            return CompleteArrival(ctx, waypoint, ctx.session->CurrentAbsoluteNodeIndex(), "find_target_reached");
        }
        // 预筛误报: 重新走起来, 探测继续
        LogDebug << "FIND: stop probe did not confirm; resuming the walk." << VAR(find.steps);
        ctx.motion_controller->SetForwardState(true);
        utils::SleepFor(kFindStopProbeIntervalMs);
    }
    return result;
}

} // namespace

Result ArriveFind(const Context& ctx, const Waypoint& waypoint, double actual_distance)
{
    LogInfo << "Action: FIND reached its anchor." << VAR(actual_distance) << VAR(waypoint.x) << VAR(waypoint.y);
    return BeginFind(ctx, "find_anchor_reached");
}

Result ConsumeFindNodes(const Context& ctx)
{
    if (!ctx.session->HasCurrentWaypoint()) {
        return {};
    }

    const Waypoint& waypoint = ctx.session->CurrentWaypoint();
    // 带坐标的点走到锚点后走 ArriveFind, 这里只接控制节点, 否则它们会被提前吃掉
    if (!waypoint.IsFindPoint() || waypoint.HasPosition()) {
        return {};
    }
    return BeginFind(ctx, "find_control_node_reached");
}

Result TickFindTarget(const Context& ctx)
{
    Result result;
    result.consumed = true;
    result.stay_in_current_tick = true;

    if (ctx.maa_context == nullptr || ctx.session == nullptr || !ctx.session->HasCurrentWaypoint()) {
        // 统一清理会解引用 session, 上下文不全时只能直接交还失败, 不走 FailFind
        LogError << "FIND is running without a waypoint or a MaaContext." << VAR(ctx.maa_context == nullptr) << VAR(ctx.session == nullptr);
        result.request_failure = true;
        result.failure_reason = "find_context_missing";
        result.failure_log_message = "FIND is running without a waypoint or a MaaContext.";
        return result;
    }

    const Waypoint& waypoint = ctx.session->CurrentWaypoint();
    if (!waypoint.IsFindPoint()) {
        // 相位与当前点对不上: 交还控制权, 别占着别人点位的拍
        LeaveFindPhase(ctx);
        SelectPhaseForCurrentWaypoint(ctx, "find_phase_mismatch");
        return result;
    }

    FindState& find = ctx.runtime_state->find;
    const auto now = std::chrono::steady_clock::now();
    if (find.started_at.time_since_epoch().count() == 0) {
        find.started_at = now;
    }
    if (find.steps >= kFindMaxSteps || ElapsedMs(find.started_at, now) >= kFindBudgetMs) {
        return FailFind(ctx, "find_budget_exhausted", "FIND ran out of budget before the stop node hit or the arrive point was reached.");
    }
    ++find.steps;

    if (waypoint.find_stop.empty() && !waypoint.find_arrive.has_value()) {
        return FailFind(ctx, "find_criteria_missing", "FIND point has neither find_stop nor find_arrive.");
    }

    MaaController* controller = ctx.action_wrapper->GetCtrl();
    ScopedImageBuffer image;
    if (controller == nullptr || !CaptureFindFrame(controller, &image)) {
        // 看不清就先站住空转这一拍, 别照着上一帧继续走; 步数预算兜住连续失败
        ctx.motion_controller->SetForwardState(false);
        LogWarn << "FIND: screencap failed, holding this step empty." << VAR(find.steps);
        utils::SleepFor(kFindStepSleepMs);
        return result;
    }

    if (!waypoint.find_stop.empty()) {
        FindSighting stop_sighting {};
        if (!RunFindNode(ctx.maa_context, waypoint.find_stop, "{}", image.Get(), &stop_sighting)) {
            return FailFind(ctx, "find_recognition_failed", "FIND stop node failed to recognize.");
        }
        if (stop_sighting.hit) {
            LogInfo << "FIND: stop node hit, target reached." << VAR(waypoint.find_stop) << VAR(find.steps) << VAR(waypoint.x)
                    << VAR(waypoint.y);
            LeaveFindPhase(ctx);
            return CompleteArrival(ctx, waypoint, ctx.session->CurrentAbsoluteNodeIndex(), "find_target_reached");
        }
    }

    if (ReachedArrivePoint(ctx, waypoint)) {
        LogInfo << "FIND: arrive point reached." << VAR(find.steps) << VAR(waypoint.find_arrive->at(0)) << VAR(waypoint.find_arrive->at(1));
        LeaveFindPhase(ctx);
        return CompleteArrival(ctx, waypoint, ctx.session->CurrentAbsoluteNodeIndex(), "find_arrive_reached");
    }

    const bool inline_text = waypoint.find_target.empty();
    if (inline_text && waypoint.find_text.empty()) {
        return FailFind(ctx, "find_target_missing", "FIND point names neither a target node nor a text list.");
    }
    const std::string target_node = inline_text ? std::string(kFindInlineOcrNode) : waypoint.find_target;
    const std::string target_override = inline_text ? BuildInlineTextOverride(waypoint.find_text) : std::string("{}");

    FindSighting target_sighting {};
    if (!RunFindNode(ctx.maa_context, target_node, target_override, image.Get(), &target_sighting)) {
        return FailFind(ctx, "find_recognition_failed", "FIND target node failed to recognize.");
    }
    if (!target_sighting.hit) {
        // 目标看不到了先站住, 别带着上一拍的朝向继续往前冲
        ctx.motion_controller->SetForwardState(false);
        ++find.miss_streak;
        // 刚还看着的目标漏认一两拍就原地等: 遮挡与抖动不该把镜头甩走; 从没见过目标则不给宽限, 直接搜
        if (find.last_seen_side != 0 && find.miss_streak < kFindMissGraceTicks) {
            LogDebug << "FIND: target lost for a beat, holding." << VAR(find.steps) << VAR(find.miss_streak);
            utils::SleepFor(kFindStepSleepMs);
            return result;
        }
        if (!SearchOneStep(ctx, find)) {
            return FailFind(ctx, "find_turn_failed", "FIND failed to issue the search turn.");
        }
        utils::SleepFor(kFindStepSleepMs);
        return result;
    }
    find.miss_streak = 0;

    // 命中却没有框 (DirectHit 之类): 框宽为零只能推出"一直往左转", 拿它当方向就错了
    if (target_sighting.box.width <= 0 || target_sighting.box.height <= 0) {
        return FailFind(ctx, "find_empty_box", "FIND target node hit without a usable box.");
    }

    const int32_t frame_width = MaaImageBufferWidth(image.Get());
    const int32_t frame_height = MaaImageBufferHeight(image.Get());
    if (frame_width <= 0 || frame_height <= 0) {
        LogWarn << "FIND: the captured frame has no size." << VAR(frame_width) << VAR(frame_height);
        ctx.motion_controller->SetForwardState(false);
        utils::SleepFor(kFindStepSleepMs);
        return result;
    }

    if (!ApproachSighting(ctx, find, target_sighting.box, frame_width, frame_height)) {
        return FailFind(ctx, "find_turn_failed", "FIND failed to issue a view turn.");
    }

    // 走路时把停车判据盯密一点: 提示窗口可能就夹在两拍之间, 一拍一查容易直接走过去
    Result probe_result = ProbeStopWhileWalking(ctx, waypoint, controller);
    if (probe_result.consumed) {
        return probe_result;
    }
    utils::SleepFor(kFindStepSleepMs);
    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
