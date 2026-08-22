#include "MapTeleportAction.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstring>
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

// 一个点位就是「底图上的一个坐标 + 认它的那张图标」。下面这些留空就按通用传送点走，
// 图标不一样的点位（基建核心之类）把模板名和它自己那套阈值写进节点参数即可
struct SelectParam
{
    std::string zone;
    std::vector<double> target;
    int max_attempts = 4;
    double gate_base = 10.0;

    std::vector<std::string> icon;

    // 大于零表示图标在这个半径内浮动（底图像素），此时 gate_base 不参与
    double radius = 0.0;

    double scale_max = 0.0;
    double min_score = 0.0;
    double min_gold_ratio = 0.0;

    MEO_JSONIZATION(
        zone,
        target,
        MEO_OPT max_attempts,
        MEO_OPT gate_base,
        MEO_OPT icon,
        MEO_OPT radius,
        MEO_OPT scale_max,
        MEO_OPT min_score,
        MEO_OPT min_gold_ratio);
};

// 大地图铺满全屏，UI 只是浮在四角的几块。图标要认要点，只需躲开这几块，
// 与视口求解那块窄 ROI 不是一回事：那块是为了别让浮层污染相关性才收得那么紧。
// 拿它当「图标必须落进来」的判据会把大片能点的地方判成点不到，
// 而地图拖到边界就不动了，判进不来又拖不动，只能空转到放弃
constexpr ScreenMapRoi kIconArea { 0.02, 0.13, 0.80, 0.85 };

// 目标离可用区边界不足这些像素就先平移，免得图标被 UI 压住或裁掉一半
constexpr int kIconMargin = 30;

// 拖动按恒定速度发，别按恒定时长：同样 420ms，244px 的拖动兑现了六成、688px 只兑现四成，
// 拉长时长把速度压回来才是对症的。上下限只防极短拖动抖成点击、极长拖动等太久
constexpr double kSwipeSpeed = 0.5;      // 屏幕像素每毫秒
constexpr int kSwipeDurationMin = 300;
constexpr int kSwipeDurationMax = 1600;

// 单次拖动不超过安全区尺寸的这个比例。发出的位移就是目标到画面中心的实际差值，
// 拖过头在几何上不成立，所以这个上限只决定一次能挪多远、跑几个来回，留窄纯属白等
constexpr double kSwipeSpanRatio = 0.85;
constexpr int kSettleMillis = 700;       // 拖动后等地图停稳再截屏
constexpr int kRetryMillis = 350;

// 平移单独记账：把目标挪进画面是这一步该干的事，不该算作识别失败
constexpr int kMaxPans = 16;

// 各端把发出的滑动兑现成多少地图位移并不一样，实测能差三分之一。
// 拿相邻两拍量到的比值现补；上限只防测偏时越补越远
constexpr double kGainMax = 2.0;
constexpr double kGainMinSpan = 40.0;

// 地图拖到边界就不动了。这根轴发出的位移够大却纹丝不动，就是顶到边了
constexpr double kPinCommand = 20.0;
constexpr double kPinMoved = 2.0;

// 视口解不出来时同一张静止画面重截多少次结果都逐位一样，得先把地图挪开换个画面
constexpr int kMaxNudges = 6;
constexpr double kNudgeRatio = 0.35;

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
cv::Point2d DragMap(MaaController* controller, const cv::Rect& safe, const cv::Point2d& delta)
{
    const double maxX = safe.width * kSwipeSpanRatio;
    const double maxY = safe.height * kSwipeSpanRatio;
    const double dx = std::clamp(delta.x, -maxX, maxX);
    const double dy = std::clamp(delta.y, -maxY, maxY);
    if (std::hypot(dx, dy) < 1.0) {
        return { 0.0, 0.0 };
    }

    const cv::Point2d center(safe.x + safe.width / 2.0, safe.y + safe.height / 2.0);
    const cv::Point from(
        static_cast<int>(std::lround(center.x - dx / 2.0)),
        static_cast<int>(std::lround(center.y - dy / 2.0)));
    const cv::Point to(
        static_cast<int>(std::lround(center.x + dx / 2.0)),
        static_cast<int>(std::lround(center.y + dy / 2.0)));

    const int duration = static_cast<int>(
        std::clamp(std::hypot(dx, dy) / kSwipeSpeed, static_cast<double>(kSwipeDurationMin), static_cast<double>(kSwipeDurationMax)));

    LogInfo << "MapTeleport: dragging map" << VAR(from.x) << VAR(from.y) << VAR(to.x) << VAR(to.y) << VAR(duration);
    const MaaCtrlId swipe_id = MaaControllerPostSwipe(controller, from.x, from.y, to.x, to.y, duration);
    MaaControllerWait(controller, swipe_id);
    std::this_thread::sleep_for(std::chrono::milliseconds(kSettleMillis));
    return { static_cast<double>(to.x - from.x), static_cast<double>(to.y - from.y) };
}

// 只把出了可用区的那根轴挪回中心。另一根轴本来就在画面里，跟着动一下纯属白挪，
// 还会把画面推到地图边界或没渲染的地方去
cv::Point2d PanDelta(const cv::Rect& safe, const cv::Point2d& expected)
{
    const cv::Point2d center(safe.x + safe.width / 2.0, safe.y + safe.height / 2.0);
    cv::Point2d need { 0.0, 0.0 };
    if (expected.x < safe.x || expected.x > safe.x + safe.width) {
        need.x = center.x - expected.x;
    }
    if (expected.y < safe.y || expected.y > safe.y + safe.height) {
        need.y = center.y - expected.y;
    }
    return need;
}

// 解不出来时先把上一拍拖过去的挪回一半——那边刚解出来过；没拖过就绕四个方向轮着试
cv::Point2d NudgeDelta(const cv::Rect& safe, const cv::Point2d& last, int index)
{
    if (index == 0 && std::hypot(last.x, last.y) >= 1.0) {
        return { -last.x / 2.0, -last.y / 2.0 };
    }

    const double sx = safe.width * kNudgeRatio;
    const double sy = safe.height * kNudgeRatio;
    switch (index % 4) {
    case 0:
        return { sx, 0.0 };
    case 1:
        return { 0.0, sy };
    case 2:
        return { -sx, 0.0 };
    default:
        return { 0.0, -sy };
    }
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

    // 缺省是通用传送点那组标定，节点给了就按节点的来
    SpotConfig spotCfg {};
    spotCfg.templates = param.icon;
    spotCfg.radiusBase = param.radius;
    spotCfg.gateBase = param.gate_base;
    if (param.scale_max > 0.0) {
        spotCfg.scaleMax = param.scale_max;
    }
    if (param.min_score > 0.0) {
        spotCfg.minScore = param.min_score;
    }
    spotCfg.minGoldRatio = param.min_gold_ratio;

    const cv::Point2d target(param.target[0], param.target[1]);
    LogInfo << "MapTeleport: start" << VAR(param.zone) << VAR(target.x) << VAR(target.y) << VAR(param.max_attempts);

    ScopedImageBuffer buffer;
    int attempt = 0;
    int pans = 0;
    int nudges = 0;
    double gain = 1.0;
    bool pinnedX = false;
    bool pinnedY = false;
    cv::Point2d issued { 0.0, 0.0 };
    std::optional<Viewport> previous;

    while (attempt < param.max_attempts) {
        cv::Mat screen;
        if (!CaptureScreen(controller, &buffer, &screen)) {
            ++attempt;
            std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
            continue;
        }

        const cv::Rect safe = MapTeleportSolver::SafeArea(screen.size(), kIconArea, kIconMargin);
        if (safe.empty()) {
            LogError << "MapTeleport: safe area degenerated" << VAR(screen.cols) << VAR(screen.rows);
            return false;
        }

        const auto viewport = solver.SolveViewport(screen, param.zone, viewportCfg);
        if (!viewport) {
            previous.reset();
            if (nudges >= kMaxNudges) {
                ++attempt;
                LogWarn << "MapTeleport: viewport still unsolved after nudging the map" << VAR(attempt) << VAR(nudges);
                std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
                continue;
            }
            LogInfo << "MapTeleport: viewport unsolved, nudging the map" << VAR(nudges);
            const cv::Point2d moved = DragMap(controller, safe, NudgeDelta(safe, issued, nudges));
            ++nudges;
            issued = moved;
            if (std::hypot(moved.x, moved.y) < 1.0) {
                ++attempt;
            }
            continue;
        }
        // 上一拍发出的位移兑现了多少：不动的那根轴是顶到了地图边界，兑现不足的比例现补回去
        if (previous && std::abs(previous->scale - viewport->scale) < 1e-6 && std::hypot(issued.x, issued.y) >= 1.0) {
            const cv::Point2d moved(
                (previous->baseOrigin.x - viewport->baseOrigin.x) / viewport->scale,
                (previous->baseOrigin.y - viewport->baseOrigin.y) / viewport->scale);
            pinnedX = std::abs(issued.x) >= kPinCommand && std::abs(moved.x) < kPinMoved;
            pinnedY = std::abs(issued.y) >= kPinCommand && std::abs(moved.y) < kPinMoved;

            const double want = std::hypot(issued.x, issued.y);
            const double got = std::hypot(moved.x, moved.y);
            if (want >= kGainMinSpan && got >= 1.0) {
                gain = std::clamp(want / got, 1.0, kGainMax);
            }
            LogInfo << "MapTeleport: drag delivered" << VAR(issued.x) << VAR(issued.y) << VAR(moved.x) << VAR(moved.y)
                    << VAR(gain) << VAR(pinnedX) << VAR(pinnedY);
        }
        previous = viewport;
        issued = { 0.0, 0.0 };

        const cv::Point2d expected = viewport->toScreen(target);
        const cv::Point2d need = PanDelta(safe, expected);
        if (std::hypot(need.x, need.y) >= 1.0) {
            const bool stuck = (std::abs(need.x) < 1.0 || pinnedX) && (std::abs(need.y) < 1.0 || pinnedY);
            if (pans >= kMaxPans || stuck) {
                LogError << "MapTeleport: the map will not pan any further toward the target" << VAR(param.zone) << VAR(pans)
                         << VAR(expected.x) << VAR(expected.y) << VAR(pinnedX) << VAR(pinnedY);
                return false;
            }
            ++pans;
            LogInfo << "MapTeleport: target outside safe area, panning" << VAR(expected.x) << VAR(expected.y) << VAR(need.x)
                    << VAR(need.y) << VAR(gain);
            issued = DragMap(controller, safe, need * gain);
            if (std::hypot(issued.x, issued.y) < 1.0) {
                LogError << "MapTeleport: target outside safe area but pan distance is degenerate";
                return false;
            }
            continue;
        }

        ++attempt;
        const auto icon = solver.ConfirmSpot(screen, expected, viewport->scale, spotCfg);
        if (icon && !icon->unlocked) {
            // 没解锁的点位不接受传送，这是规则不是识别失败，重试多少次都一样，立刻收场。
            // TODO: 这一支缺未解锁的实拍，从没真跑到过，判据见 SpotConfig 里 minGoldRatio 的说明
            LogWarn << "MapTeleport: this spot is still locked, teleport is not available" << VAR(param.zone)
                    << VAR(icon->goldRatio) << VAR(spotCfg.minGoldRatio);
            if (MaaContextRunTask(context, kEnterWorldEntry, "{}") == MaaInvalidId) {
                LogError << "MapTeleport: could not leave the map after finding the spot locked" << VAR(param.zone);
            }
            return false;
        }

        if (!icon) {
            // 角色标记画在图标之上，它落在期望位置就是图标认不出来的原因：人已经站在这了。
            // 认不出图标的其他原因不会命中这一支
            PlayerMarkerConfig rescueCfg = markerCfg;
            if (spotCfg.radiusBase > 0.0) {
                // 图标浮动多远，压在它上面的角色标记就离 target 多远，窗口得跟着放到浮动区那么宽
                rescueCfg.searchRadius = static_cast<int>(std::lround(spotCfg.radiusBase / viewport->scale));
            }
            const auto marker = MapTeleportSolver::DetectPlayerMarker(screen, expected, rescueCfg);
            if (!marker) {
                LogWarn << "MapTeleport: icon not confirmed at expected position" << VAR(attempt) << VAR(expected.x)
                        << VAR(expected.y);
                std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
                continue;
            }

            // 图标被标记盖住点不了，但标记落在这里就佐证了视口没解错，可以照期望位置点。
            // 已经在点位上也照样传一次：判定圈比点位本身大得多，在圈里不等于站在它的落点上
            LogInfo << "MapTeleport: player marker covers the icon, clicking the expected position" << VAR(param.zone)
                    << VAR(marker->center.x) << VAR(marker->center.y) << VAR(marker->area) << VAR(marker->solidity);
        }

        const cv::Point2d spot = icon ? icon->center : expected;
        const int cx = static_cast<int>(std::lround(spot.x));
        const int cy = static_cast<int>(std::lround(spot.y));
        LogInfo << "MapTeleport: clicking" << VAR(param.zone) << VAR(cx) << VAR(cy);
        const MaaCtrlId click_id = MaaControllerPostClick(controller, cx, cy);
        return MaaControllerWait(controller, click_id) == MaaStatus_Succeeded ? true : false;
    }

    // 点不到就不点：既认不出图标、又没有角色标记佐证时，宁可让上层走失败分支，也不按算出来的坐标空点
    LogError << "MapTeleport: gave up without a confirmed icon" << VAR(param.zone) << VAR(target.x) << VAR(target.y)
             << VAR(param.max_attempts);
    return false;
}

} // namespace mapteleport
