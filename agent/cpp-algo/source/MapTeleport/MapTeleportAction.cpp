#include "MapTeleportAction.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstring>
#include <limits>
#include <optional>
#include <string>
#include <thread>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "MapTeleportSolver.h"
#include "utils.h"

namespace mapteleport
{

namespace
{

struct SelectParam
{
    std::string zone;
    std::vector<double> target;
    int max_attempts = 4;
    double gate_base = 10.0;

    // 基建核心区的传送点：图标随玩家摆放浮动，target 只圈得住范围，换颜色判据确认
    bool core = false;

    MEO_JSONIZATION(zone, target, MEO_OPT max_attempts, MEO_OPT gate_base, MEO_OPT core);
};

// 目标离安全区边界不足这些像素就先平移，免得图标被 UI 压住或裁掉一半
constexpr int kIconMargin = 30;
constexpr int kSwipeDuration = 420;
constexpr double kSwipeSpanRatio = 0.55; // 单次拖动不超过安全区尺寸的这个比例
constexpr int kSettleMillis = 700;       // 拖动后等地图停稳再截屏
constexpr int kRetryMillis = 350;

// 平移单独记账：把目标挪进画面是这一步该干的事，不该算作识别失败。
// 拖到地图边界后再拖也不动，所以每次必须实打实缩短距离，否则立刻收手
constexpr int kMaxPans = 16;
constexpr double kPanProgress = 20.0;

// 已经站在目标上时用它退回大世界。地图可能是从菜单里打开的，
// 逐个界面怎么关由这个公开接口负责，这里不自己按键
constexpr const char* kEnterWorldEntry = "SceneAnyEnterWorld";

bool ParseParam(const char* raw, SelectParam* out)
{
    if (raw == nullptr || std::strlen(raw) == 0) {
        LogError << "MapTeleport: empty custom_action_param";
        return false;
    }

    const auto parsed = json::parse(raw);
    if (!parsed) {
        LogError << "MapTeleport: custom_action_param is not valid JSON" << VAR(raw);
        return false;
    }

    SelectParam value {};
    if (!value.from_json(*parsed)) {
        LogError << "MapTeleport: custom_action_param missing required fields" << VAR(raw);
        return false;
    }
    if (value.zone.empty() || value.target.size() != 2) {
        LogError << "MapTeleport: 'zone' must be non-empty and 'target' must hold exactly two numbers" << VAR(raw);
        return false;
    }
    if (value.max_attempts < 1) {
        value.max_attempts = 1;
    }

    *out = std::move(value);
    return true;
}

// 控制器自带的资源层要靠它选出来。句柄由调用方传进来：为了拿这一个字符串再去取一次控制器,
// 会把上一个取回来的控制器就地析构掉, 长期持有它的人当场悬垂
std::string ControllerType(MaaController* controller)
{
    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaControllerGetInfo(controller, buffer.Get()) || MaaStringBufferIsEmpty(buffer.Get())) {
        return {};
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || raw[0] == '\0') {
        return {};
    }

    const auto info = json::parse(raw).value_or(json::object {});
    if (!info.contains("type") || !info.at("type").is_string()) {
        return {};
    }
    return info.at("type").as_string();
}

bool CaptureScreen(MaaController* controller, ScopedImageBuffer* buffer, cv::Mat* out)
{
    const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller);
    if (MaaControllerWait(controller, screencap_id) != MaaStatus_Succeeded) {
        LogWarn << "MapTeleport: screencap did not succeed";
        return false;
    }
    if (!MaaControllerCachedImage(controller, buffer->Get()) || MaaImageBufferIsEmpty(buffer->Get())) {
        LogWarn << "MapTeleport: screencap returned an empty image";
        return false;
    }

    *out = to_mat(buffer->Get());
    return !out->empty();
}

// 把 delta 钳到单次可拖的范围内，返回实际发出的位移
bool DragMap(MaaController* controller, const cv::Rect& safe, const cv::Point2d& delta)
{
    const double maxX = safe.width * kSwipeSpanRatio;
    const double maxY = safe.height * kSwipeSpanRatio;
    const double dx = std::clamp(delta.x, -maxX, maxX);
    const double dy = std::clamp(delta.y, -maxY, maxY);
    if (std::hypot(dx, dy) < 1.0) {
        return false;
    }

    const cv::Point2d center(safe.x + safe.width / 2.0, safe.y + safe.height / 2.0);
    const cv::Point from(
        static_cast<int>(std::lround(center.x - dx / 2.0)),
        static_cast<int>(std::lround(center.y - dy / 2.0)));
    const cv::Point to(
        static_cast<int>(std::lround(center.x + dx / 2.0)),
        static_cast<int>(std::lround(center.y + dy / 2.0)));

    LogInfo << "MapTeleport: dragging map" << VAR(from.x) << VAR(from.y) << VAR(to.x) << VAR(to.y);
    const MaaCtrlId swipe_id = MaaControllerPostSwipe(controller, from.x, from.y, to.x, to.y, kSwipeDuration);
    MaaControllerWait(controller, swipe_id);
    std::this_thread::sleep_for(std::chrono::milliseconds(kSettleMillis));
    return true;
}

} // namespace

MaaBool MAA_CALL MapTeleportSelectRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    if (context == nullptr) {
        LogError << "MapTeleport: null context";
        return false;
    }

    SelectParam param;
    if (!ParseParam(custom_action_param, &param)) {
        return false;
    }

    MaaController* controller = MaaTaskerGetController(MaaContextGetTasker(context));
    if (controller == nullptr) {
        LogError << "MapTeleport: no controller bound to context";
        return false;
    }

    const std::string controller_type = ControllerType(controller);
    MapTeleportSolver& solver = GetSolver(controller_type);
    const ViewportConfig viewportCfg {};
    const PlayerMarkerConfig markerCfg {};
    const CoreIconConfig coreCfg {};
    AnchorConfig anchorCfg {};
    anchorCfg.gateBase = param.gate_base;

    const cv::Point2d target(param.target[0], param.target[1]);
    LogInfo << "MapTeleport: start" << VAR(param.zone) << VAR(target.x) << VAR(target.y) << VAR(param.max_attempts);

    ScopedImageBuffer buffer;
    int attempt = 0;
    int pans = 0;
    double lastGap = std::numeric_limits<double>::max();

    while (attempt < param.max_attempts) {
        cv::Mat screen;
        if (!CaptureScreen(controller, &buffer, &screen)) {
            ++attempt;
            std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
            continue;
        }

        const auto viewport = solver.SolveViewport(screen, param.zone, viewportCfg);
        if (!viewport) {
            ++attempt;
            LogWarn << "MapTeleport: viewport unsolved, retrying" << VAR(attempt);
            std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
            continue;
        }

        const cv::Rect safe = MapTeleportSolver::SafeArea(screen.size(), viewportCfg.roi, kIconMargin);
        if (safe.empty()) {
            LogError << "MapTeleport: safe area degenerated" << VAR(screen.cols) << VAR(screen.rows);
            return false;
        }

        const cv::Point2d expected = viewport->toScreen(target);
        const cv::Point2d center(safe.x + safe.width / 2.0, safe.y + safe.height / 2.0);
        if (!safe.contains(cv::Point(static_cast<int>(std::lround(expected.x)), static_cast<int>(std::lround(expected.y))))) {
            const double gap = std::hypot(center.x - expected.x, center.y - expected.y);
            if (pans >= kMaxPans || gap > lastGap - kPanProgress) {
                LogError << "MapTeleport: panning no longer closes in on the target" << VAR(param.zone) << VAR(pans) << VAR(gap)
                         << VAR(lastGap);
                return false;
            }
            lastGap = gap;
            ++pans;
            LogInfo << "MapTeleport: target outside safe area, panning" << VAR(expected.x) << VAR(expected.y) << VAR(gap);
            if (!DragMap(controller, safe, center - expected)) {
                LogError << "MapTeleport: target outside safe area but pan distance is degenerate";
                return false;
            }
            continue;
        }

        // 目标已在画面里，下次再被推出去算新的一轮，别拿上一轮的距离卡它
        lastGap = std::numeric_limits<double>::max();

        ++attempt;
        std::optional<cv::Point2d> spot;
        if (param.core) {
            const auto icon = solver.ConfirmCoreIcon(screen, expected, viewport->scale, coreCfg);
            if (icon && !icon->unlocked) {
                // 没解锁的基建点不接受传送，这是规则不是识别失败，重试多少次都一样，立刻收场。
                // TODO: 这一支缺未解锁的实拍，从没真跑到过，判据见 CoreIconConfig 的同名说明
                LogWarn << "MapTeleport: this base is still locked, teleport is not available" << VAR(param.zone)
                        << VAR(icon->goldRatio) << VAR(coreCfg.minGoldRatio);
                if (MaaContextRunTask(context, kEnterWorldEntry, "{}") == MaaInvalidId) {
                    LogError << "MapTeleport: could not leave the map after finding the base locked" << VAR(param.zone);
                }
                return false;
            }
            if (icon) {
                spot = icon->center;
                LogInfo << "MapTeleport: core icon confirmed" << VAR(param.zone) << VAR(icon->score) << VAR(icon->goldRatio);
            }
        }
        else {
            const auto anchor = solver.ConfirmAnchor(screen, expected, viewport->scale, anchorCfg);
            if (anchor) {
                spot = anchor->center;
                LogInfo << "MapTeleport: anchor confirmed" << VAR(param.zone) << VAR(anchor->score) << VAR(anchor->offsetBase);
            }
        }

        if (!spot) {
            // 角色标记画在图标之上，它落在期望位置就是图标认不出来的原因：人已经站在这了。
            // 剩下的距离交给寻路，退回大世界当作到达。认不出图标的其他原因不会命中这一支
            PlayerMarkerConfig rescueCfg = markerCfg;
            if (param.core) {
                // 图标浮动多远，压在它上面的角色标记就离 target 多远，窗口得跟着放到浮动区那么宽
                rescueCfg.searchRadius = static_cast<int>(std::lround(coreCfg.searchBase / viewport->scale));
            }
            const auto marker = MapTeleportSolver::DetectPlayerMarker(screen, expected, rescueCfg);
            if (marker) {
                LogInfo << "MapTeleport: already standing on the target, skipping the teleport" << VAR(param.zone)
                        << VAR(marker->center.x) << VAR(marker->center.y) << VAR(marker->area) << VAR(marker->solidity);
                if (MaaContextRunTask(context, kEnterWorldEntry, "{}") == MaaInvalidId) {
                    LogError << "MapTeleport: could not leave the map after skipping the teleport" << VAR(param.zone);
                    return false;
                }
                return true;
            }

            LogWarn << "MapTeleport: icon not confirmed at expected position" << VAR(attempt) << VAR(expected.x) << VAR(expected.y);
            std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
            continue;
        }

        const int cx = static_cast<int>(std::lround(spot->x));
        const int cy = static_cast<int>(std::lround(spot->y));
        LogInfo << "MapTeleport: clicking" << VAR(param.zone) << VAR(cx) << VAR(cy);
        const MaaCtrlId click_id = MaaControllerPostClick(controller, cx, cy);
        return MaaControllerWait(controller, click_id) == MaaStatus_Succeeded ? true : false;
    }

    // 点不到就不点：认不出图标时宁可让上层走失败分支，也不按算出来的坐标空点
    LogError << "MapTeleport: gave up without a confirmed icon" << VAR(param.zone) << VAR(target.x) << VAR(target.y)
             << VAR(param.max_attempts);
    return false;
}

} // namespace mapteleport
