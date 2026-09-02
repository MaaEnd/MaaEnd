#include "WorldMapFind.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstring>
#include <optional>
#include <random>
#include <string>
#include <thread>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "WorldMapSolver.h"
#include "utils.h"

namespace worldmap
{

namespace
{

// 区域底图上的一个坐标，加上认它的那个图标名。图标名不给就只解坐标不认图标，
// 阈值一概不在这里露面：它们跟着图标走，写在图标表里
struct FindParam
{
    std::string zone;
    std::vector<double> at;

    std::string icon;
    std::string state;
    int max_attempts = 4;

    // 整窗解不出来时用几乘几的分块投票再解一次。置 1 关掉这条回退路径：
    // 单个点位要是被它坑了可以就地关掉，不必等下一版
    int vote_grid = ViewportConfig {}.voteGrid;

    MEO_JSONIZATION(zone, at, MEO_OPT icon, MEO_OPT state, MEO_OPT max_attempts, MEO_OPT vote_grid);
};

// 大地图铺满全屏，UI 只是浮在四角的几块。图标要认要点，只需躲开这几块，
// 与视口求解那块窄 ROI 不是一回事：那块是为了别让浮层污染相关性才收得那么紧。
// 拿它当「图标必须落进来」的判据会把大片能点的地方判成点不到，
// 而地图拖到边界就不动了，判进不来又拖不动，只能空转到放弃
constexpr ScreenMapRoi kIconArea { 0.02, 0.13, 0.80, 0.85 };

// 目标离可用区边界不足这些像素就先平移，免得图标被 UI 压住或裁掉一半
constexpr int kIconMargin = 30;

// 拖动按恒定速度发，别按恒定时长：同样 420ms，244px 的拖动兑现了六成、688px 只兑现四成，
// 拉长时长把速度压回来才是对症的。上下限只防极短拖动抖成点击、极长拖动等太久。
// 鼠标后端不吃这一套打折——实测 389px 兑现了 106%，所以速度按它来定；
// 真有端上兑现不足，下面那套实测位移＋增益会把差的补回来
constexpr double kSwipeSpeed = 2.0; // 屏幕像素每毫秒
constexpr int kSwipeDurationMin = 100;
constexpr int kSwipeDurationMax = 600;

// 单次拖动不超过安全区尺寸的这个比例。发出的位移就是目标到画面中心的实际差值，
// 拖过头在几何上不成立，所以这个上限只决定一次能挪多远、跑几个来回，留窄纯属白等
constexpr double kSwipeSpanRatio = 0.85;
// 拖动后等地图停稳再截屏。实测松手后还会多滑约半成，留一点余量把余滑等完，
// 截在滑动中会把这一拖的位移量错、连带把增益也带偏
constexpr int kSettleMillis = 250;
constexpr int kRetryMillis = 350;

// 平移单独记账：把目标挪进画面是这一步该干的事，不该算作识别失败
constexpr int kMaxPans = 16;

// 各端把发出的滑动兑现成多少地图位移并不一样，实测能差三分之一。
// 拿相邻两拍量到的比值现补；上限只防测偏时越补越远。
// 兑现比低到 kGainMinRatio 以下的那一拍不是端上打折，是这次拖动压根没生效，
// 拿它算比值只会把后面每一拖都放大到上限
constexpr double kGainMax = 2.0;
constexpr double kGainMinSpan = 40.0;
constexpr double kGainMinRatio = 0.25;

// 这根轴发出的位移够大却纹丝不动，可能是地图顶到了边界，也可能是起手正压在图标或连线上、
// 触控被那个控件吃掉了。两者靠「换条路径再拖」区分：被吃掉的换个起点就动了，
// 真到边界的换几条路径还是零，连着 kPinStreak 拍都零才判死
constexpr double kPinCommand = 20.0;
constexpr double kPinMoved = 2.0;
constexpr int kPinStreak = 3;

// 视口解不出来时同一张静止画面重截多少次结果都逐位一样，得先把地图挪开换个画面
constexpr int kMaxNudges = 6;
constexpr double kNudgeRatio = 0.35;

// 缩放档位是视口求解的未知量，进来先压到最小钉死。按钮坐标各端不同，
// 交给 pipeline 的 SceneMapZoomOut 处理，cpp 只触发一次子任务
constexpr const char* kZoomOutNode = "SceneMapZoomOut";

bool ParseParam(const char* raw, FindParam* out)
{
    if (raw == nullptr || std::strlen(raw) == 0) {
        LogError << "WorldMap: empty custom_recognition_param";
        return false;
    }

    const auto parsed = json::parse(raw);
    if (!parsed) {
        LogError << "WorldMap: custom_recognition_param is not valid JSON" << VAR(raw);
        return false;
    }

    FindParam value {};
    if (!value.from_json(*parsed)) {
        LogError << "WorldMap: custom_recognition_param missing required fields" << VAR(raw);
        return false;
    }
    if (value.zone.empty() || value.at.size() != 2) {
        LogError << "WorldMap: 'zone' must be non-empty and 'at' must hold exactly two numbers" << VAR(raw);
        return false;
    }
    if (!value.state.empty() && value.state != "locked" && value.state != "unlocked") {
        LogError << "WorldMap: 'state' must be either 'unlocked' or 'locked'" << VAR(value.state);
        return false;
    }
    if (!value.state.empty() && value.icon.empty()) {
        LogError << "WorldMap: 'state' needs an 'icon' to judge" << VAR(value.state);
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
    // 取不到就只剩基础资源层可用, 端上那层图会被静默跳过, 所以每条空路径都得留下痕迹
    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaControllerGetInfo(controller, buffer.Get()) || MaaStringBufferIsEmpty(buffer.Get())) {
        LogError << "WorldMap: controller info unavailable";
        return {};
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || raw[0] == '\0') {
        LogError << "WorldMap: controller info is empty";
        return {};
    }

    const auto info = json::parse(raw);
    if (!info) {
        LogError << "WorldMap: controller info is not valid json" << VAR(raw);
        return {};
    }
    if (!info->contains("type") || !info->at("type").is_string()) {
        LogError << "WorldMap: controller info carries no 'type' string" << VAR(raw);
        return {};
    }
    return info->at("type").as_string();
}

bool CaptureScreen(MaaController* controller, ScopedImageBuffer* buffer, cv::Mat* out)
{
    const MaaCtrlId screencap_id = MaaControllerPostScreencap(controller);
    if (MaaControllerWait(controller, screencap_id) != MaaStatus_Succeeded) {
        LogWarn << "WorldMap: screencap did not succeed";
        return false;
    }
    if (!MaaControllerCachedImage(controller, buffer->Get()) || MaaImageBufferIsEmpty(buffer->Get())) {
        LogWarn << "WorldMap: screencap returned an empty image";
        return false;
    }

    *out = to_mat(buffer->Get());
    return !out->empty();
}

void ZoomMapOut(MaaContext* context)
{
    if (MaaContextRunTask(context, kZoomOutNode, "{}") == MaaInvalidId) {
        LogWarn << "WorldMap: zoom-out subtask failed to dispatch" << VAR(kZoomOutNode);
    }
}

double RandomIn(double lo, double hi)
{
    if (!(hi > lo)) {
        return (lo + hi) / 2.0;
    }
    static thread_local std::mt19937 rng(std::random_device {}());
    return std::uniform_real_distribution<double>(lo, hi)(rng);
}

// 把 delta 钳到单次可拖的范围内，返回实际发出的位移。
// 线段在安全区里的落位每次随机取：起手压在图标或连线上时触控会被那个控件吃掉，
// 而钉死在正中心的话重试逐位重复同一条路径，吃掉一次就永远吃
cv::Point2d DragMap(MaaController* controller, const cv::Rect& safe, const cv::Point2d& delta)
{
    const double maxX = safe.width * kSwipeSpanRatio;
    const double maxY = safe.height * kSwipeSpanRatio;
    const double dx = std::clamp(delta.x, -maxX, maxX);
    const double dy = std::clamp(delta.y, -maxY, maxY);
    if (std::hypot(dx, dy) < 1.0) {
        return { 0.0, 0.0 };
    }

    // 中点能落的范围＝安全区往里缩掉半个线段，这样两端都还在安全区内
    const cv::Point2d center(
        RandomIn(safe.x + std::abs(dx) / 2.0, safe.x + safe.width - std::abs(dx) / 2.0),
        RandomIn(safe.y + std::abs(dy) / 2.0, safe.y + safe.height - std::abs(dy) / 2.0));
    const cv::Point from(static_cast<int>(std::lround(center.x - dx / 2.0)), static_cast<int>(std::lround(center.y - dy / 2.0)));
    const cv::Point to(static_cast<int>(std::lround(center.x + dx / 2.0)), static_cast<int>(std::lround(center.y + dy / 2.0)));

    const int duration = static_cast<int>(
        std::clamp(std::hypot(dx, dy) / kSwipeSpeed, static_cast<double>(kSwipeDurationMin), static_cast<double>(kSwipeDurationMax)));

    LogInfo << "WorldMap: dragging map" << VAR(from.x) << VAR(from.y) << VAR(to.x) << VAR(to.y) << VAR(duration);
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

MaaRect PointBox(const cv::Point2d& point)
{
    return MaaRect {
        .x = static_cast<int32_t>(std::lround(point.x)),
        .y = static_cast<int32_t>(std::lround(point.y)),
        .width = 1,
        .height = 1,
    };
}

// 交图标本体上的那一点，不交整个模板框：框架会在给它的框里随机取点落指，
// 而定居点核心的图标只占模板框的一成多，交整框就是在图标旁边的地面上掷骰子
MaaRect SpotBox(const SpotHit& hit)
{
    return PointBox(hit.hotspot);
}

void WriteDetail(MaaStringBuffer* out_detail, const json::object& payload)
{
    if (out_detail == nullptr) {
        return;
    }
    const std::string text = payload.to_string();
    MaaStringBufferSetEx(out_detail, text.c_str(), static_cast<MaaSize>(text.size()));
}

} // namespace

MaaBool MAA_CALL MapFindRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    [[maybe_unused]] const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi_param,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    if (context == nullptr) {
        LogError << "WorldMap: null context";
        return false;
    }

    FindParam param;
    if (!ParseParam(custom_recognition_param, &param)) {
        return false;
    }

    MaaController* controller = MaaTaskerGetController(MaaContextGetTasker(context));
    if (controller == nullptr) {
        LogError << "WorldMap: no controller bound to context";
        return false;
    }

    WorldMapSolver& solver = GetSolver(ControllerType(controller));
    ViewportConfig viewportCfg {};
    viewportCfg.voteGrid = param.vote_grid;

    // 图标名给了就必须在表里查得到：查不到照样跑下去等于把认图标这一步悄悄跳过
    std::optional<IconSpec> spec;
    if (!param.icon.empty()) {
        spec = solver.ResolveIcon(param.icon);
        if (!spec) {
            return false;
        }
    }

    const bool wantUnlocked = param.state != "locked";
    if (!param.state.empty() && spec && spec->spot.minGoldRatio <= 0.0) {
        LogError << "WorldMap: this icon has no unlock threshold, 'state' cannot be judged" << VAR(param.icon) << VAR(param.state);
        return false;
    }

    const cv::Point2d target(param.at[0], param.at[1]);
    LogInfo << "WorldMap: find" << VAR(param.zone) << VAR(target.x) << VAR(target.y) << VAR(param.icon) << VAR(param.state)
            << VAR(param.max_attempts);

    ZoomMapOut(context);

    // 缩放那几趟自己会截屏，框架给进来的那一帧已过期
    ScopedImageBuffer buffer;
    cv::Mat screen;

    int attempt = 0;
    int pans = 0;
    int nudges = 0;
    double gain = 1.0;
    int stallX = 0;
    int stallY = 0;
    cv::Point2d issued { 0.0, 0.0 };
    std::optional<Viewport> previous;

    while (attempt < param.max_attempts) {
        if (screen.empty() && !CaptureScreen(controller, &buffer, &screen)) {
            ++attempt;
            std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
            continue;
        }

        const cv::Rect safe = WorldMapSolver::SafeArea(screen.size(), kIconArea, kIconMargin);
        if (safe.empty()) {
            LogError << "WorldMap: safe area degenerated" << VAR(screen.cols) << VAR(screen.rows);
            return false;
        }

        const auto viewport = solver.SolveViewport(screen, param.zone, viewportCfg);
        if (!viewport) {
            previous.reset();
            screen.release();
            if (nudges >= kMaxNudges) {
                ++attempt;
                LogWarn << "WorldMap: viewport still unsolved after nudging the map" << VAR(attempt) << VAR(nudges);
                std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
                continue;
            }
            LogInfo << "WorldMap: viewport unsolved, nudging the map" << VAR(nudges);
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
            stallX = std::abs(issued.x) >= kPinCommand && std::abs(moved.x) < kPinMoved ? stallX + 1 : 0;
            stallY = std::abs(issued.y) >= kPinCommand && std::abs(moved.y) < kPinMoved ? stallY + 1 : 0;

            const double want = std::hypot(issued.x, issued.y);
            const double got = std::hypot(moved.x, moved.y);
            if (want >= kGainMinSpan && got >= want * kGainMinRatio) {
                gain = std::clamp(want / got, 1.0, kGainMax);
            }
            LogInfo << "WorldMap: drag delivered" << VAR(issued.x) << VAR(issued.y) << VAR(moved.x) << VAR(moved.y) << VAR(gain)
                    << VAR(stallX) << VAR(stallY);
        }
        previous = viewport;
        issued = { 0.0, 0.0 };

        const cv::Point2d expected = viewport->toScreen(target);
        const cv::Point2d need = PanDelta(safe, expected);
        if (std::hypot(need.x, need.y) >= 1.0) {
            // 换了几条路径还是纹丝不动，才认这根轴真到边了；只零一拍多半是触控被压在起手点上的控件吃了
            const bool pinnedX = stallX >= kPinStreak;
            const bool pinnedY = stallY >= kPinStreak;
            const bool stuck = (std::abs(need.x) < 1.0 || pinnedX) && (std::abs(need.y) < 1.0 || pinnedY);
            if (pans >= kMaxPans || stuck) {
                // 挪不动了也别空手回去。出了安全区不等于出了能点的地方——那圈边距是留给识别的余量，
                // 目标只要还落在可用区里就照常认、照常点
                const cv::Rect usable = WorldMapSolver::SafeArea(screen.size(), kIconArea, 0);
                const cv::Point at(static_cast<int>(std::lround(expected.x)), static_cast<int>(std::lround(expected.y)));
                if (!usable.contains(at)) {
                    LogError << "WorldMap: the map will not pan any further and the target is out of reach" << VAR(param.zone) << VAR(pans)
                             << VAR(expected.x) << VAR(expected.y) << VAR(stallX) << VAR(stallY);
                    return false;
                }
                LogWarn << "WorldMap: the map will not pan any further, taking the target where it stands" << VAR(param.zone) << VAR(pans)
                        << VAR(expected.x) << VAR(expected.y) << VAR(stallX) << VAR(stallY);
            }
            else {
                ++pans;
                LogInfo << "WorldMap: target outside safe area, panning" << VAR(expected.x) << VAR(expected.y) << VAR(need.x) << VAR(need.y)
                        << VAR(gain);
                screen.release();
                issued = DragMap(controller, safe, need * gain);
                if (std::hypot(issued.x, issued.y) < 1.0) {
                    LogError << "WorldMap: target outside safe area but pan distance is degenerate";
                    return false;
                }
                continue;
            }
        }

        ++attempt;

        json::object detail {
            { "zone", param.zone },
            { "at", json::array { target.x, target.y } },
            { "screen", json::array { expected.x, expected.y } },
            { "viewport_scale", viewport->scale },
            { "viewport_vote", viewport->voteGrid },
        };

        // 图标名没给就只把坐标解出来，认不认得出图标由调用方自己接着判
        if (!spec) {
            WriteDetail(out_detail, detail);
            if (out_box != nullptr) {
                *out_box = PointBox(expected);
            }
            LogInfo << "WorldMap: located" << VAR(param.zone) << VAR(expected.x) << VAR(expected.y);
            return true;
        }

        const auto icon = solver.ConfirmSpot(screen, expected, viewport->scale, spec->spot);
        if (icon && icon->unlocked != wantUnlocked) {
            // 解锁与否是规则不是识别失败，重试多少次都一样，立刻收场
            LogWarn << "WorldMap: the icon is here but not in the requested state" << VAR(param.zone) << VAR(param.icon)
                    << VAR(icon->unlocked) << VAR(wantUnlocked) << VAR(icon->goldRatio);
            return false;
        }
        if (icon) {
            detail.emplace("icon", param.icon);
            detail.emplace("template", icon->templateName);
            detail.emplace("score", icon->score);
            detail.emplace("unlocked", icon->unlocked);
            detail.emplace("click", json::array { icon->hotspot.x, icon->hotspot.y });
            WriteDetail(out_detail, detail);
            if (out_box != nullptr) {
                *out_box = SpotBox(*icon);
            }
            return true;
        }

        // 角色标记画在图标之上，它落在期望位置就是图标认不出来的原因：人已经站在这了。
        // 认不出图标的其他原因不会命中这一支
        if (spec->occludedByPlayer && wantUnlocked) {
            PlayerMarkerConfig markerCfg {};
            if (spec->spot.radiusBase > 0.0) {
                // 图标浮动多远，压在它上面的角色标记就离目标多远，窗口得跟着放到浮动区那么宽
                markerCfg.searchRadius = static_cast<int>(std::lround(spec->spot.radiusBase / viewport->scale));
            }
            const auto marker = WorldMapSolver::DetectPlayerMarker(screen, expected, markerCfg);
            if (marker) {
                // 图标被标记盖住认不出，但标记落在这里就佐证了视口没解错，可以照期望位置交坐标
                LogInfo << "WorldMap: player marker covers the icon, taking the expected position" << VAR(param.zone)
                        << VAR(marker->center.x) << VAR(marker->center.y) << VAR(marker->area) << VAR(marker->solidity);
                detail.emplace("icon", param.icon);
                detail.emplace("player_marker", true);
                WriteDetail(out_detail, detail);
                if (out_box != nullptr) {
                    *out_box = PointBox(expected);
                }
                return true;
            }
        }

        LogWarn << "WorldMap: icon not confirmed at expected position" << VAR(attempt) << VAR(expected.x) << VAR(expected.y)
                << VAR(viewport->voteGrid);
        screen.release();
        std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
    }

    // 认不出图标又没有角色标记佐证时就不给坐标：宁可让上层走失败分支，也不交一个算出来的空位置
    LogError << "WorldMap: gave up without a confirmed icon" << VAR(param.zone) << VAR(target.x) << VAR(target.y) << VAR(param.icon)
             << VAR(param.max_attempts);
    return false;
}

} // namespace worldmap
