#include "find_action.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdlib>
#include <string>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "action_wrapper.h"
#include "motion_controller.h"
#include "navi_config.h"
#include "navi_math.h"
#include "position_provider.h"
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

// 框到手: 偏了就转视角, 走过了就退一步, 否则朝它走一步。脉冲长短按框的高低估远近
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
        const double degrees_per_px = kTurnDegreesPerCircle / static_cast<double>(ComputeTurn360Units(frame_width));
        const double yaw_deg = static_cast<double>(offset_px) * degrees_per_px * kFindSteerGain;
        LogDebug << "FIND: turning toward the target." << VAR(find.steps) << VAR(offset_px) << VAR(yaw_deg);
        return TurnToHeadingOnce(ctx, yaw_deg);
    }

    if (static_cast<double>(center_y) > static_cast<double>(frame_height) * kFindPassedCenterYRatio) {
        LogDebug << "FIND: target is behind, stepping back." << VAR(find.steps) << VAR(center_y);
        ctx.action_wrapper->SetMovementStateSync(false, false, true, false, kFindBackwardPulseMs);
        ctx.action_wrapper->SetMovementStateSync(false, false, false, false, 0);
        return true;
    }

    const double half_height = static_cast<double>(frame_height) / 2.0;
    const double far_factor = std::clamp((half_height - static_cast<double>(center_y)) / half_height, 0.0, 1.0);
    const int32_t hold_ms = static_cast<int32_t>(std::lround(kFindForwardPulseMs * (1.0 + (kFindFarPulseScaleMax - 1.0) * far_factor)));
    LogDebug << "FIND: stepping forward." << VAR(find.steps) << VAR(far_factor) << VAR(hold_ms);
    ctx.action_wrapper->PulseForwardSync(hold_ms);
    return true;
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
        // 截图抖动不判死整条路线, 空转这一拍, 步数预算兜住连续失败
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
        utils::SleepFor(kFindStepSleepMs);
        return result;
    }

    if (!ApproachSighting(ctx, find, target_sighting.box, frame_width, frame_height)) {
        return FailFind(ctx, "find_turn_failed", "FIND failed to issue a view turn.");
    }
    utils::SleepFor(kFindStepSleepMs);
    return result;
}

} // namespace semantic_nodes

} // namespace mapnavigator
