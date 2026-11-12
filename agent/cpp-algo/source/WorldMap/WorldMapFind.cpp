#include "WorldMapFind.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstring>
#include <mutex>
#include <optional>
#include <random>
#include <string>
#include <thread>
#include <vector>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "WorldMapAssets.h"
#include "WorldMapIcon.h"
#include "WorldMapViewport.h"
#include "utils.h"

namespace worldmap
{

namespace
{

// 一组候选里的一项：底图坐标，加上认出它之后交回给框架的那个节点
struct Candidate
{
    std::vector<double> at;
    std::string next;

    MEO_JSONIZATION(at, next);
};

// 区域底图上的坐标，加上认它的那个图标名。图标名不给就只解坐标不认图标，
// 阈值一概不在这里露面：它们跟着图标走，写在图标表里
struct FindParam
{
    std::string zone;

    // 认一个点写 at，认一组点写 candidates，二选一。一组点共用同一次缩放与视口求解，
    // 各自只多付一次开窗确认，比一个点占一个节点省掉几乎全部重复开销
    std::vector<double> at;
    std::vector<Candidate> candidates;

    std::string icon;
    std::string state;
    int max_attempts = 4;

    MEO_JSONIZATION(zone, MEO_OPT at, MEO_OPT candidates, MEO_OPT icon, MEO_OPT state, MEO_OPT max_attempts);
};

// 解析后的统一形态：单点写法归一成一个不带 next 的候选，往下只认这一种
struct Target
{
    cv::Point2d at { 0.0, 0.0 };
    std::string next;
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
// 交给 pipeline 的 SceneMapZoomOutWithoutReco 处理，cpp 只触发一次子任务
constexpr const char* kZoomOutNode = "SceneMapZoomOutWithoutReco";

// 一个候选的三种收场：认出来了、这次没认出来、不该再往下跑的硬错
enum class Probe
{
    Hit,
    Miss,
    Fatal,
};

// Next 接着走本拍的下一步，Again 回到循环开头再来一拍
enum class ProbeStep
{
    Next,
    Again,
    Hit,
    Miss,
    Fatal,
};

// 跨候选留着的画面状态。画面没动就不重截屏、不重解视口，拖动兑现率也接着上一次记账。
// screen 是 buffer 那块裸数据的视图，两者必须同生共死
struct MapView
{
    ScopedImageBuffer buffer;
    cv::Mat screen;
    std::optional<Viewport> viewport;
    std::optional<Viewport> previous;
    cv::Point2d issued { 0.0, 0.0 };
    double gain = 1.0;
    int stall_x = 0;
    int stall_y = 0;
};

// 一次调用里所有候选共用的东西
struct FindSession
{
    MaaController* controller = nullptr;
    WorldMapAssets* assets = nullptr;
    std::mt19937* rng = nullptr;
    const FindParam* param = nullptr;
    std::optional<IconSpec> spec;
    MapView view;
};

struct TargetProbe
{
    Target target;
    std::size_t index = 0;
    int attempt = 0;
    int pans = 0;
    int nudges = 0;
};

struct FindHit
{
    json::object detail;
    MaaRect box {};
};

bool parse_param(const char* raw, FindParam* out)
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
    if (value.zone.empty()) {
        LogError << "WorldMap: 'zone' must be non-empty" << VAR(raw);
        return false;
    }
    if (value.at.empty() == value.candidates.empty()) {
        LogError << "WorldMap: give exactly one of 'at' and 'candidates'" << VAR(raw);
        return false;
    }
    if (!value.at.empty() && value.at.size() != 2) {
        LogError << "WorldMap: 'at' must hold exactly two numbers" << VAR(raw);
        return false;
    }
    for (const Candidate& candidate : value.candidates) {
        if (candidate.at.size() != 2 || candidate.next.empty()) {
            LogError << "WorldMap: every candidate needs two numbers in 'at' and a non-empty 'next'" << VAR(raw);
            return false;
        }
    }
    // 一组候选彼此只靠图标区分，不认图标的话每个候选都会"成功"，返回的永远是第一个
    if (!value.candidates.empty() && value.icon.empty()) {
        LogError << "WorldMap: 'candidates' needs an 'icon' to tell them apart" << VAR(raw);
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
std::string controller_type(MaaController* controller)
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

bool capture_screen(MaaController* controller, ScopedImageBuffer* buffer, cv::Mat* out)
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

void zoom_map_out(MaaContext* context)
{
    if (MaaContextRunTask(context, kZoomOutNode, "{}") == MaaInvalidId) {
        LogWarn << "WorldMap: zoom-out subtask failed to dispatch" << VAR(kZoomOutNode);
    }
}

double random_in(std::mt19937* rng, double lo, double hi)
{
    if (!(hi > lo)) {
        return (lo + hi) / 2.0;
    }
    return std::uniform_real_distribution<double>(lo, hi)(*rng);
}

// 把 delta 钳到单次可拖的范围内，返回实际发出的位移。
// 线段在安全区里的落位每次随机取：起手压在图标或连线上时触控会被那个控件吃掉，
// 而钉死在正中心的话重试逐位重复同一条路径，吃掉一次就永远吃
cv::Point2d drag_map(MaaController* controller, std::mt19937* rng, const cv::Rect& safe, const cv::Point2d& delta)
{
    const double max_x = safe.width * kSwipeSpanRatio;
    const double max_y = safe.height * kSwipeSpanRatio;
    const double dx = std::clamp(delta.x, -max_x, max_x);
    const double dy = std::clamp(delta.y, -max_y, max_y);
    if (std::hypot(dx, dy) < 1.0) {
        return { 0.0, 0.0 };
    }

    // 中点能落的范围＝安全区往里缩掉半个线段，这样两端都还在安全区内
    const cv::Point2d center(
        random_in(rng, safe.x + std::abs(dx) / 2.0, safe.x + safe.width - std::abs(dx) / 2.0),
        random_in(rng, safe.y + std::abs(dy) / 2.0, safe.y + safe.height - std::abs(dy) / 2.0));
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
cv::Point2d pan_delta(const cv::Rect& safe, const cv::Point2d& expected)
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
cv::Point2d nudge_delta(const cv::Rect& safe, const cv::Point2d& last, int index)
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

MaaRect point_box(const cv::Point2d& point)
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
MaaRect spot_box(const SpotHit& hit)
{
    return point_box(hit.hotspot);
}

void write_detail(MaaStringBuffer* out_detail, const json::object& payload)
{
    if (out_detail == nullptr) {
        return;
    }
    const std::string text = payload.to_string();
    MaaStringBufferSetEx(out_detail, text.c_str(), static_cast<MaaSize>(text.size()));
}

std::vector<Target> build_targets(const FindParam& param)
{
    if (param.candidates.empty()) {
        return { Target { cv::Point2d(param.at[0], param.at[1]), {} } };
    }

    std::vector<Target> targets;
    targets.reserve(param.candidates.size());
    for (const Candidate& candidate : param.candidates) {
        targets.push_back(Target { cv::Point2d(candidate.at[0], candidate.at[1]), candidate.next });
    }
    return targets;
}

// 把命中项交回框架当 next，顶掉节点自己写的那份
bool hand_back_next(MaaContext* context, const char* node_name, const std::string& next)
{
    ScopedStringBuffer item;
    ScopedStringListBuffer list;
    if (node_name == nullptr || item.Get() == nullptr || list.Get() == nullptr) {
        return false;
    }
    return MaaStringBufferSet(item.Get(), next.c_str()) && MaaStringListBufferAppend(list.Get(), item.Get())
           && MaaContextOverrideNext(context, node_name, list.Get());
}

// 候选归它的 next 节点管开关：那个节点被 enabled:false 关掉，这个候选就整个跳过。
// 认了再交回一个不会跑的节点是不行的——命中即停，那样排在后面的候选一并被废掉。
// 读不出来一律当开着：这一步是替使用者省事的，不该反过来把候选判没
bool candidate_enabled(MaaContext* context, const std::string& node_name)
{
    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, node_name.c_str(), buffer.Get())) {
        return true;
    }
    const char* text = MaaStringBufferGet(buffer.Get());
    if (text == nullptr) {
        return true;
    }
    const auto data = json::parse(text);
    if (!data) {
        LogWarn << "WorldMap: cannot read the candidate node's data" << VAR(node_name);
        return true;
    }
    return data->get("enabled", true);
}

std::optional<Viewport>
    solve_on_tiers(WorldMapAssets* assets, const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg, const cv::Point2d& at)
{
    std::lock_guard guard(assets->mutex);
    const cv::Mat* base = LoadZoneBase(assets, zone);
    if (base == nullptr) {
        return std::nullopt;
    }
    return SolveViewportOnTiers(LoadZoneTiers(assets, zone, *base), screen, zone, cfg, at);
}

std::optional<Viewport> solve_on_base(WorldMapAssets* assets, const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg)
{
    if (screen.empty()) {
        LogError << "WorldMap: empty screen image";
        return std::nullopt;
    }

    std::lock_guard guard(assets->mutex);
    const cv::Mat* base = LoadZoneBase(assets, zone);
    if (base == nullptr) {
        return std::nullopt;
    }
    return SolveViewport(*base, screen, zone, cfg);
}

std::optional<Viewport> solve_for_target(FindSession* session, const ViewportConfig& cfg, const cv::Point2d& at)
{
    const std::string& zone = session->param->zone;
    std::optional<Viewport> viewport = solve_on_tiers(session->assets, session->view.screen, zone, cfg, at);
    if (viewport) {
        return viewport;
    }
    return solve_on_base(session->assets, session->view.screen, zone, cfg);
}

// 拖动只挪地图，缩放进图时压到底就不再动，上一趟解出来的尺度照旧成立。
// 钉住它只搜位置，整条尺度阶梯都省了；缩放真被动过则这一趟解不出来，下一趟按整条阶梯解
std::optional<Viewport> solve_view(FindSession* session, const Target& target)
{
    const MapView& view = session->view;
    if (view.previous) {
        const ViewportConfig pinned { .scale_hint = view.previous->scale };
        std::optional<Viewport> viewport = solve_for_target(session, pinned, target.at);
        if (viewport) {
            return viewport;
        }
        LogInfo << "WorldMap: pinned scale rejected, rescanning full ladder" << VAR(view.previous->scale);
    }
    return solve_for_target(session, ViewportConfig {}, target.at);
}

ProbeStep capture_if_released(FindSession* session, TargetProbe* probe)
{
    MapView& view = session->view;
    if (!view.screen.empty()) {
        return ProbeStep::Next;
    }
    if (!capture_screen(session->controller, &view.buffer, &view.screen)) {
        ++probe->attempt;
        std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
        return ProbeStep::Again;
    }
    // 换了画面，上一次解出来的视口跟着作废
    view.viewport.reset();
    return ProbeStep::Next;
}

ProbeStep nudge_or_retry(FindSession* session, TargetProbe* probe, const cv::Rect& safe)
{
    if (probe->nudges >= kMaxNudges) {
        ++probe->attempt;
        LogWarn << "WorldMap: viewport still unsolved after nudging the map" << VAR(probe->attempt) << VAR(probe->nudges);
        std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
        return ProbeStep::Again;
    }
    LogInfo << "WorldMap: viewport unsolved, nudging the map" << VAR(probe->nudges);
    const cv::Point2d moved = drag_map(session->controller, session->rng, safe, nudge_delta(safe, session->view.issued, probe->nudges));
    ++probe->nudges;
    session->view.issued = moved;
    if (std::hypot(moved.x, moved.y) < 1.0) {
        ++probe->attempt;
    }
    return ProbeStep::Again;
}

// 上一拍发出的位移兑现了多少：不动的那根轴是顶到了地图边界，兑现不足的比例现补回去
void record_drag_delivered(MapView* view)
{
    if (view->previous && std::abs(view->previous->scale - view->viewport->scale) < 1e-6
        && std::hypot(view->issued.x, view->issued.y) >= 1.0) {
        const cv::Point2d moved(
            (view->previous->base_origin.x - view->viewport->base_origin.x) / view->viewport->scale,
            (view->previous->base_origin.y - view->viewport->base_origin.y) / view->viewport->scale);
        view->stall_x = std::abs(view->issued.x) >= kPinCommand && std::abs(moved.x) < kPinMoved ? view->stall_x + 1 : 0;
        view->stall_y = std::abs(view->issued.y) >= kPinCommand && std::abs(moved.y) < kPinMoved ? view->stall_y + 1 : 0;

        const double want = std::hypot(view->issued.x, view->issued.y);
        const double got = std::hypot(moved.x, moved.y);
        if (want >= kGainMinSpan && got >= want * kGainMinRatio) {
            view->gain = std::clamp(want / got, 1.0, kGainMax);
        }
        LogInfo << "WorldMap: drag delivered" << VAR(view->issued.x) << VAR(view->issued.y) << VAR(moved.x) << VAR(moved.y)
                << VAR(view->gain) << VAR(view->stall_x) << VAR(view->stall_y);
    }
}

ProbeStep pan_toward(FindSession* session, TargetProbe* probe, const cv::Rect& safe, const cv::Point2d& expected)
{
    const FindParam& param = *session->param;
    MapView& view = session->view;
    const cv::Point2d need = pan_delta(safe, expected);
    if (std::hypot(need.x, need.y) < 1.0) {
        return ProbeStep::Next;
    }

    // 换了几条路径还是纹丝不动，才认这根轴真到边了；只零一拍多半是触控被压在起手点上的控件吃了
    const bool pinned_x = view.stall_x >= kPinStreak;
    const bool pinned_y = view.stall_y >= kPinStreak;
    const bool stuck = (std::abs(need.x) < 1.0 || pinned_x) && (std::abs(need.y) < 1.0 || pinned_y);
    if (probe->pans < kMaxPans && !stuck) {
        ++probe->pans;
        LogInfo << "WorldMap: target outside safe area, panning" << VAR(expected.x) << VAR(expected.y) << VAR(need.x) << VAR(need.y)
                << VAR(view.gain);
        view.screen.release();
        view.issued = drag_map(session->controller, session->rng, safe, need * view.gain);
        if (std::hypot(view.issued.x, view.issued.y) < 1.0) {
            LogWarn << "WorldMap: target outside safe area but pan distance is degenerate" << VAR(probe->index);
            return ProbeStep::Miss;
        }
        return ProbeStep::Again;
    }

    // 挪不动了也别空手回去。出了安全区不等于出了能点的地方——那圈边距是留给识别的余量，
    // 目标只要还落在可用区里就照常认、照常点
    const cv::Rect usable = SafeArea(view.screen.size(), kIconArea, 0);
    const cv::Point at(static_cast<int>(std::lround(expected.x)), static_cast<int>(std::lround(expected.y)));
    if (!usable.contains(at)) {
        LogWarn << "WorldMap: the map will not pan any further and the target is out of reach" << VAR(param.zone) << VAR(probe->index)
                << VAR(probe->pans) << VAR(expected.x) << VAR(expected.y) << VAR(view.stall_x) << VAR(view.stall_y);
        return ProbeStep::Miss;
    }
    LogWarn << "WorldMap: the map will not pan any further, taking the target where it stands" << VAR(param.zone) << VAR(probe->pans)
            << VAR(expected.x) << VAR(expected.y) << VAR(view.stall_x) << VAR(view.stall_y);
    return ProbeStep::Next;
}

ProbeStep confirm_target(FindSession* session, TargetProbe* probe, const cv::Point2d& expected, FindHit* hit)
{
    const FindParam& param = *session->param;
    MapView& view = session->view;
    const Viewport& viewport = *view.viewport;
    const bool want_unlocked = param.state != "locked";

    ++probe->attempt;

    json::object detail {
        { "zone", param.zone },
        { "index", static_cast<int>(probe->index) },
        { "at", json::array { probe->target.at.x, probe->target.at.y } },
        { "screen", json::array { expected.x, expected.y } },
        { "viewport_scale", viewport.scale },
        { "viewport_tier", viewport.tier },
    };
    if (!probe->target.next.empty()) {
        detail.emplace("next", probe->target.next);
    }

    // 图标名没给就只把坐标解出来，认不认得出图标由调用方自己接着判
    if (!session->spec) {
        *hit = FindHit { .detail = detail, .box = point_box(expected) };
        LogInfo << "WorldMap: located" << VAR(param.zone) << VAR(expected.x) << VAR(expected.y);
        return ProbeStep::Hit;
    }

    const auto icon = ConfirmSpot(session->assets, view.screen, expected, viewport.scale, session->spec->spot);
    if (icon && icon->unlocked != want_unlocked) {
        // 解锁与否是规则不是识别失败，重试多少次都一样，立刻收场
        LogWarn << "WorldMap: the icon is here but not in the requested state" << VAR(param.zone) << VAR(param.icon) << VAR(icon->unlocked)
                << VAR(want_unlocked) << VAR(icon->gold_ratio);
        return ProbeStep::Miss;
    }
    if (icon) {
        detail.emplace("icon", param.icon);
        detail.emplace("template", icon->template_name);
        detail.emplace("score", icon->score);
        detail.emplace("unlocked", icon->unlocked);
        detail.emplace("click", json::array { icon->hotspot.x, icon->hotspot.y });
        *hit = FindHit { .detail = detail, .box = spot_box(*icon) };
        return ProbeStep::Hit;
    }

    // 角色标记画在图标之上，它落在期望位置就是图标认不出来的原因：人已经站在这了。
    // 认不出图标的其他原因不会命中这一支
    if (session->spec->occluded_by_player && want_unlocked) {
        PlayerMarkerConfig marker_cfg {};
        if (session->spec->spot.radius_base > 0.0) {
            // 图标浮动多远，压在它上面的角色标记就离目标多远，窗口得跟着放到浮动区那么宽
            marker_cfg.search_radius = static_cast<int>(std::lround(session->spec->spot.radius_base / viewport.scale));
        }
        const auto marker = DetectPlayerMarker(view.screen, expected, marker_cfg);
        if (marker) {
            // 图标被标记盖住认不出，但标记落在这里就佐证了视口没解错，可以照期望位置交坐标
            LogInfo << "WorldMap: player marker covers the icon, taking the expected position" << VAR(param.zone) << VAR(marker->center.x)
                    << VAR(marker->center.y) << VAR(marker->area) << VAR(marker->solidity);
            detail.emplace("icon", param.icon);
            detail.emplace("player_marker", true);
            *hit = FindHit { .detail = detail, .box = point_box(expected) };
            return ProbeStep::Hit;
        }
    }

    LogWarn << "WorldMap: icon not confirmed at expected position" << VAR(probe->attempt) << VAR(probe->index) << VAR(expected.x)
            << VAR(expected.y) << VAR(viewport.tier);
    view.screen.release();
    std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
    return ProbeStep::Again;
}

ProbeStep probe_once(FindSession* session, TargetProbe* probe, FindHit* hit)
{
    MapView& view = session->view;
    const ProbeStep captured = capture_if_released(session, probe);
    if (captured != ProbeStep::Next) {
        return captured;
    }

    const cv::Rect safe = SafeArea(view.screen.size(), kIconArea, kIconMargin);
    if (safe.empty()) {
        LogError << "WorldMap: safe area degenerated" << VAR(view.screen.cols) << VAR(view.screen.rows);
        return ProbeStep::Fatal;
    }

    if (!view.viewport) {
        view.viewport = solve_view(session, probe->target);
        if (!view.viewport) {
            view.previous.reset();
            view.screen.release();
            return nudge_or_retry(session, probe, safe);
        }
        record_drag_delivered(&view);
        view.previous = view.viewport;
        view.issued = { 0.0, 0.0 };
    }

    const cv::Point2d expected = ViewportToScreen(*view.viewport, probe->target.at);
    const ProbeStep panned = pan_toward(session, probe, safe, expected);
    if (panned != ProbeStep::Next) {
        return panned;
    }
    return confirm_target(session, probe, expected, hit);
}

// 把一个候选认到底。重试、平移、放弃都只影响这一个候选，画面与视口留给下一个接着用
Probe probe_target(FindSession* session, const Target& target, std::size_t index, FindHit* hit)
{
    TargetProbe probe { .target = target, .index = index };
    while (probe.attempt < session->param->max_attempts) {
        switch (probe_once(session, &probe, hit)) {
        case ProbeStep::Next:
        case ProbeStep::Again:
            break;
        case ProbeStep::Hit:
            return Probe::Hit;
        case ProbeStep::Miss:
            return Probe::Miss;
        case ProbeStep::Fatal:
            return Probe::Fatal;
        }
    }
    return Probe::Miss;
}

} // namespace

MaaBool MAA_CALL MapFindRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    const char* node_name,
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
    if (!parse_param(custom_recognition_param, &param)) {
        return false;
    }

    MaaController* controller = MaaTaskerGetController(MaaContextGetTasker(context));
    if (controller == nullptr) {
        LogError << "WorldMap: no controller bound to context";
        return false;
    }

    WorldMapAssets* assets = SharedAssets(controller_type(controller));

    // 图标名给了就必须在表里查得到：查不到照样跑下去等于把认图标这一步悄悄跳过
    std::optional<IconSpec> spec;
    if (!param.icon.empty()) {
        spec = ResolveIcon(assets, param.icon);
        if (!spec) {
            return false;
        }
    }

    if (!param.state.empty() && spec && spec->spot.min_gold_ratio <= 0.0) {
        LogError << "WorldMap: this icon has no unlock threshold, 'state' cannot be judged" << VAR(param.icon) << VAR(param.state);
        return false;
    }

    const std::vector<Target> targets = build_targets(param);
    LogInfo << "WorldMap: find" << VAR(param.zone) << VAR(targets.size()) << VAR(param.icon) << VAR(param.state) << VAR(param.max_attempts);

    zoom_map_out(context);

    static thread_local std::mt19937 rng(std::random_device {}());
    FindSession session { .controller = controller, .assets = assets, .rng = &rng, .param = &param, .spec = spec };

    // 一组候选共用这一次缩放，也共用它留下的画面：缩放那几趟自己会截屏，框架给进来的那一帧已过期。
    // 认下来一个就收场，剩下的候选连地图都不用再挪
    for (std::size_t index = 0; index < targets.size(); ++index) {
        const Target& target = targets[index];
        if (!target.next.empty() && !candidate_enabled(context, target.next)) {
            LogInfo << "WorldMap: candidate turned off, skipping it" << VAR(index) << VAR(target.next);
            continue;
        }

        FindHit hit;
        switch (probe_target(&session, target, index, &hit)) {
        case Probe::Fatal:
            return false;
        case Probe::Miss:
            continue;
        case Probe::Hit:
            break;
        }

        write_detail(out_detail, hit.detail);
        if (out_box != nullptr) {
            *out_box = hit.box;
        }
        if (target.next.empty()) {
            return true;
        }
        // 交不回去就不算认出来。命中即停，后面的候选已经没机会了，此时报成功等于让
        // 上层拿着这个位置去走节点自己那份 next——点的是这个候选，走的是别处
        if (!hand_back_next(context, node_name, target.next)) {
            LogError << "WorldMap: failed to hand the hit back to the pipeline" << VAR(target.next);
            return false;
        }
        return true;
    }

    // 认不出图标又没有角色标记佐证时就不给坐标：宁可让上层走失败分支，也不交一个算出来的空位置。
    // 认不到是上层预期的分支（候选筛选逐个试过来，全不中是常态），故为 WARN——ERR 会直接刷到用户界面
    LogWarn << "WorldMap: gave up without a confirmed icon" << VAR(param.zone) << VAR(targets.size()) << VAR(param.icon)
            << VAR(param.max_attempts);
    return false;
}

} // namespace worldmap
