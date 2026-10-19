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

    // 整窗解不出来时用几乘几的分块投票再解一次。置 1 关掉这条回退路径：
    // 单个点位要是被它坑了可以就地关掉，不必等下一版
    int vote_grid = ViewportConfig {}.voteGrid;

    // 认到之后再放大几档，交出去的是放大后重新认到的那个图标。0 不放大
    int zoom_in = 0;

    MEO_JSONIZATION(
        zone,
        MEO_OPT at,
        MEO_OPT candidates,
        MEO_OPT icon,
        MEO_OPT state,
        MEO_OPT max_attempts,
        MEO_OPT vote_grid,
        MEO_OPT zoom_in);
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

// 放大一档同样交给 pipeline：「+」按钮坐标跟着资源层走。已到最大档时它什么都不点
constexpr const char* kZoomInNode = "SceneMapZoomInStepWithoutReco";

// 「+」放大时屏幕上不动的那一点，按屏幕尺寸的比例。PlayCover 实测八档都落在 (640.6, 359.8) 附近 3 像素内
constexpr double kZoomCenterX = 0.5;
constexpr double kZoomCenterY = 0.5;
// 「+」是按住持续放大，点一下放大多少随按压时长变：PlayCover 同样单击实测 1.07 到 1.60 倍。
// 所以每档都拿前后两帧现量倍率，不按档数推。上限只框住搜索范围
constexpr double kZoomStepFactorMax = 2.0;
// 量出来不到这个倍率就是这一档没放大。PlayCover 实测最大档约为最小档的 4.75 倍
constexpr double kZoomStepMinFactor = 1.03;
// 量倍率那一下的相关分下限。相邻两档实测 0.73 到 0.99，图标与地名密的地方偏低；
// 画面换成了别的东西时实测 -0.15 到 0.28
constexpr double kZoomMatchMinScore = 0.50;
// 放大前把图标拖到离缩放中心这么近，单位屏幕像素。它也是对中时最短的一拖：拖得太短、起手又压在图标上，
// 会被当成点击把图标详情点开
constexpr double kZoomCenterTolerance = 16.0;
// 每档放大前对中最多拖几次。放大后的地图拖多少走多少（Win32 实测多走不到一成），再拖不到就是顶到了边界
constexpr int kZoomRecenterTries = 3;
// 放大后在推算位置开的确认窗半径。窗口得比图标宽出一个身位，才看得见旁边有没有同类图标
constexpr int kZoomConfirmRadius = 60;
// 放大后命中离推算位置的偏差上限，单位屏幕像素：放大后的尺度是推出来的，折回底图像素没有依据
constexpr double kZoomGateScreen = 12.0;
// 窗口里次高分至少比命中低这么多，才认窗口里只有这一个图标。传送锚点实测命中 0.94、次高 0.32 到 0.34
constexpr double kZoomRivalMargin = 0.10;

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
    if (value.zoom_in < 0) {
        LogError << "WorldMap: 'zoom_in' must not be negative" << VAR(value.zoom_in);
        return false;
    }
    // 放大后交出去的位置只来自重新认到的图标，不认图标就没有东西可交
    if (value.zoom_in > 0 && value.icon.empty()) {
        LogError << "WorldMap: 'zoom_in' needs an 'icon' to confirm after zooming" << VAR(value.zoom_in);
        return false;
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

cv::Point2d ZoomCenter(const cv::Size& screenSize)
{
    return { screenSize.width * kZoomCenterX, screenSize.height * kZoomCenterY };
}

// 把 aim 挪到缩放中心要拖的位移，已在容差圈里就不拖。两根轴都要挪，和只救出界那根轴的 PanDelta 不同
cv::Point2d CenterDelta(const cv::Point2d& center, const cv::Point2d& aim)
{
    if (std::hypot(aim.x - center.x, aim.y - center.y) <= kZoomCenterTolerance) {
        return { 0.0, 0.0 };
    }
    return center - aim;
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

std::vector<Target> BuildTargets(const FindParam& param)
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
bool HandBackNext(MaaContext* context, const char* node_name, const std::string& next)
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
bool CandidateEnabled(MaaContext* context, const std::string& node_name)
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

// 一个候选的三种收场：认出来了、这次没认出来、不该再往下跑的硬错
enum class Probe
{
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
    int stallX = 0;
    int stallY = 0;
};

// 一次调用里所有候选共用的东西
struct FindSession
{
    MaaContext* context = nullptr;
    MaaController* controller = nullptr;
    WorldMapSolver* solver = nullptr;
    ViewportConfig viewportCfg {};
    const FindParam* param = nullptr;
    std::optional<IconSpec> spec;
    MapView view;
};

struct ZoomedSpot
{
    SpotHit hit;
    Viewport viewport;
    cv::Point2d predicted { 0.0, 0.0 };
    int steps = 0;
    double factor = 1.0;
};

// 放大一档会把图标离中心的距离按倍率拉长。按倍率上限外推仍落在安全区里，这一档才放得下它
bool ZoomKeepsInView(const cv::Rect& safe, const cv::Point2d& center, const cv::Point2d& at)
{
    const cv::Point2d reach = center + (at - center) * kZoomStepFactorMax;
    return cv::Rect2d(safe).contains(reach);
}

// 把底图点 base 拖回缩放中心，按两帧实测的平移更新视口。顶到边界拖不动就留在原处，由放大前的出界判断收场。
// 两帧对不上是画面被换掉了（拖动被当成点击点开了别的东西），不给
bool RecenterBeforeZoom(
    FindSession& session,
    const cv::Rect& safe,
    const cv::Point2d& center,
    const cv::Point2d& base,
    cv::Mat* previous,
    Viewport* viewport)
{
    MapView& view = session.view;
    for (int i = 0; i < kZoomRecenterTries; ++i) {
        const cv::Point2d need = CenterDelta(center, viewport->toScreen(base));
        if (std::hypot(need.x, need.y) < 1.0) {
            return true;
        }
        // 不乘最小档记下的增益：那份兑现率可能是顶着边界量出来的，放大后照乘只会拖过头
        const cv::Point2d issued = DragMap(session.controller, safe, need);
        if (!CaptureScreen(session.controller, &view.buffer, &view.screen)) {
            return false;
        }
        const ShiftMeasure moved = WorldMapSolver::MeasureShift(*previous, view.screen, center, issued).value_or(ShiftMeasure {});
        if (moved.score < kZoomMatchMinScore) {
            LogWarn << "WorldMap: the map was replaced while centering" << VAR(i) << VAR(moved.score);
            return false;
        }
        *viewport = viewport->shiftedBy(moved.shift);
        *previous = view.screen.clone();
        LogInfo << "WorldMap: centered again before zooming" << VAR(need.x) << VAR(need.y) << VAR(issued.x) << VAR(issued.y)
                << VAR(moved.shift.x) << VAR(moved.shift.y) << VAR(moved.score);
        if (std::hypot(moved.shift.x, moved.shift.y) < kPinMoved) {
            return true;
        }
    }
    return true;
}

// 图标认到之后按档放大，到推算位置重新认一次。交出去的位置只来自这次重认，推算出来的视口只决定去哪开窗。
// 最小档整张区域图铺满屏幕，几乎没有平移余量，图标常常拖不到中心；放大后余量才出来，所以每档放大前都再对一次中。
// 顶在地图边界上、再放一档图标就出画面的，停在这一档。任何一步对不上就不给，地图停在放大后的样子，由调用方缩回去
std::optional<ZoomedSpot> ZoomOntoSpot(FindSession& session, const Viewport& before, const SpotHit& spot, bool wantUnlocked)
{
    MapView& view = session.view;
    const int steps = session.param->zoom_in;
    const cv::Point2d center = ZoomCenter(view.screen.size());
    const cv::Rect safe = WorldMapSolver::SafeArea(view.screen.size(), kIconArea, kIconMargin);
    const cv::Point2d base = before.toBase(spot.center);

    ZoomedSpot out;
    out.viewport = before;
    // view.screen 是截图缓冲的视图，下一次截图就覆盖掉，要比对得先拷一份
    cv::Mat previous = view.screen.clone();
    for (int step = 0; step < steps; ++step) {
        // 第一档之前调用方已经在最小档对过中
        if (step > 0 && !RecenterBeforeZoom(session, safe, center, base, &previous, &out.viewport)) {
            return std::nullopt;
        }
        if (!ZoomKeepsInView(safe, center, out.viewport.toScreen(base))) {
            LogInfo << "WorldMap: one more step would push the icon out of view, zooming stops here" << VAR(step) << VAR(steps)
                    << VAR(out.factor);
            break;
        }
        if (MaaContextRunTask(session.context, kZoomInNode, "{}") == MaaInvalidId) {
            LogWarn << "WorldMap: zoom-in subtask failed to dispatch" << VAR(kZoomInNode) << VAR(step);
            return std::nullopt;
        }
        if (!CaptureScreen(session.controller, &view.buffer, &view.screen)) {
            return std::nullopt;
        }
        const ZoomMeasure measured =
            WorldMapSolver::MeasureZoom(previous, view.screen, center, kZoomStepFactorMax).value_or(ZoomMeasure {});
        if (measured.score < kZoomMatchMinScore) {
            LogWarn << "WorldMap: the map was replaced while zooming" << VAR(step) << VAR(measured.factor) << VAR(measured.score);
            return std::nullopt;
        }
        // 到了最大档「+」变灰不再点，点击被吞也是这副样子，两种都按放大到此为止，接着在这一档重认
        if (measured.factor < kZoomStepMinFactor) {
            LogInfo << "WorldMap: the map stopped zooming in" << VAR(step) << VAR(steps) << VAR(out.factor);
            break;
        }
        ++out.steps;
        out.factor *= measured.factor;
        out.viewport = out.viewport.zoomedAbout(center, measured.factor);
        LogInfo << "WorldMap: zoomed one step" << VAR(step) << VAR(measured.factor) << VAR(measured.score) << VAR(out.factor);
        previous = view.screen.clone();
    }

    out.predicted = out.viewport.toScreen(base);

    // 浮动图标在最小档已经认到了实际落点，放大后同样只在推算位置附近开定点小窗
    SpotConfig cfg = session.spec->spot;
    cfg.radiusBase = 0.0;
    cfg.radiusScreen = kZoomConfirmRadius;
    cfg.gateBase = kZoomGateScreen;
    // 尺度传 1：偏差闸与收窄重扫都按屏幕像素算
    const auto hit = session.solver->ConfirmSpot(view.screen, out.predicted, 1.0, cfg);
    if (!hit) {
        LogWarn << "WorldMap: icon not confirmed after zooming" << VAR(out.predicted.x) << VAR(out.predicted.y) << VAR(out.factor);
        return std::nullopt;
    }
    // 放大把同类图标彼此推开，窗口里还剩第二个过线的，就说不清认到的是哪一个
    if (hit->rivalScore >= cfg.minScore || hit->score - hit->rivalScore < kZoomRivalMargin) {
        LogWarn << "WorldMap: another icon of the same kind shares the zoomed window" << VAR(hit->score) << VAR(hit->rivalScore)
                << VAR(cfg.minScore) << VAR(kZoomRivalMargin);
        return std::nullopt;
    }
    if (hit->unlocked != wantUnlocked) {
        LogWarn << "WorldMap: the icon reads in another state after zooming" << VAR(hit->unlocked) << VAR(hit->goldRatio)
                << VAR(wantUnlocked);
        return std::nullopt;
    }

    out.hit = *hit;
    LogInfo << "WorldMap: icon confirmed after zooming" << VAR(out.steps) << VAR(out.factor) << VAR(out.predicted.x) << VAR(out.predicted.y)
            << VAR(out.hit.center.x) << VAR(out.hit.center.y) << VAR(out.hit.score) << VAR(out.hit.rivalScore) << VAR(out.viewport.scale);
    return out;
}

// 视口求解与钉尺度都以最小档为前提，放大没成就缩回去，画面与视口全部作废重来
void RestoreMinimumZoom(FindSession& session)
{
    ZoomMapOut(session.context);
    session.view.screen.release();
    session.view.viewport.reset();
    session.view.previous.reset();
    session.view.issued = { 0.0, 0.0 };
}

// 把一个候选认到底。重试、平移、放弃都只影响这一个候选，画面与视口留给下一个接着用
Probe ProbeTarget(FindSession& session, const Target& target, std::size_t index, MaaRect* out_box, MaaStringBuffer* out_detail)
{
    const FindParam& param = *session.param;
    MapView& view = session.view;
    const bool wantUnlocked = param.state != "locked";
    const bool wantZoom = param.zoom_in > 0;

    int attempt = 0;
    int pans = 0;
    int nudges = 0;

    // 最小档拖到边界还对不到缩放中心，就不在这一档硬拖了，认到图标后边放大边对中
    bool centerPinned = false;
    // 放大一档图标就出画面，只能交最小档的结果
    bool zoomSkipped = false;
    // 图标实际落点减去视口推算位置。对中要对的是图标本身，推算位置能差出一个判定圈
    cv::Point2d iconBias { 0.0, 0.0 };

    while (attempt < param.max_attempts) {
        if (view.screen.empty()) {
            if (!CaptureScreen(session.controller, &view.buffer, &view.screen)) {
                ++attempt;
                std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
                continue;
            }
            // 换了画面，上一次解出来的视口跟着作废
            view.viewport.reset();
        }

        const cv::Rect safe = WorldMapSolver::SafeArea(view.screen.size(), kIconArea, kIconMargin);
        if (safe.empty()) {
            LogError << "WorldMap: safe area degenerated" << VAR(view.screen.cols) << VAR(view.screen.rows);
            return Probe::Fatal;
        }

        if (!view.viewport) {
            // 拖动只挪地图，缩放进图时压到底就不再动，上一趟解出来的尺度照旧成立。
            // 钉住它只搜位置，整条尺度阶梯都省了；缩放真被动过则这一趟解不出来，下一趟按整条阶梯解
            if (view.previous) {
                ViewportConfig pinned = session.viewportCfg;
                pinned.scaleHint = view.previous->scale;
                view.viewport = session.solver->SolveViewport(view.screen, param.zone, pinned);
                if (!view.viewport) {
                    LogInfo << "WorldMap: pinned scale rejected, rescanning full ladder" << VAR(view.previous->scale);
                }
            }
            if (!view.viewport) {
                view.viewport = session.solver->SolveViewport(view.screen, param.zone, session.viewportCfg);
            }
            if (!view.viewport) {
                view.previous.reset();
                view.screen.release();
                if (nudges >= kMaxNudges) {
                    ++attempt;
                    LogWarn << "WorldMap: viewport still unsolved after nudging the map" << VAR(attempt) << VAR(nudges);
                    std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
                    continue;
                }
                LogInfo << "WorldMap: viewport unsolved, nudging the map" << VAR(nudges);
                const cv::Point2d moved = DragMap(session.controller, safe, NudgeDelta(safe, view.issued, nudges));
                ++nudges;
                view.issued = moved;
                if (std::hypot(moved.x, moved.y) < 1.0) {
                    ++attempt;
                }
                continue;
            }
            // 上一拍发出的位移兑现了多少：不动的那根轴是顶到了地图边界，兑现不足的比例现补回去
            if (view.previous && std::abs(view.previous->scale - view.viewport->scale) < 1e-6
                && std::hypot(view.issued.x, view.issued.y) >= 1.0) {
                const cv::Point2d moved(
                    (view.previous->baseOrigin.x - view.viewport->baseOrigin.x) / view.viewport->scale,
                    (view.previous->baseOrigin.y - view.viewport->baseOrigin.y) / view.viewport->scale);
                view.stallX = std::abs(view.issued.x) >= kPinCommand && std::abs(moved.x) < kPinMoved ? view.stallX + 1 : 0;
                view.stallY = std::abs(view.issued.y) >= kPinCommand && std::abs(moved.y) < kPinMoved ? view.stallY + 1 : 0;

                const double want = std::hypot(view.issued.x, view.issued.y);
                const double got = std::hypot(moved.x, moved.y);
                if (want >= kGainMinSpan && got >= want * kGainMinRatio) {
                    view.gain = std::clamp(want / got, 1.0, kGainMax);
                }
                LogInfo << "WorldMap: drag delivered" << VAR(view.issued.x) << VAR(view.issued.y) << VAR(moved.x) << VAR(moved.y)
                        << VAR(view.gain) << VAR(view.stallX) << VAR(view.stallY);
            }
            view.previous = view.viewport;
            view.issued = { 0.0, 0.0 };
        }

        const Viewport& viewport = *view.viewport;
        const cv::Point2d expected = viewport.toScreen(target.at);
        const bool centering = wantZoom && !zoomSkipped && !centerPinned;
        const cv::Point2d need = centering ? CenterDelta(ZoomCenter(view.screen.size()), expected + iconBias) : PanDelta(safe, expected);
        if (std::hypot(need.x, need.y) >= 1.0) {
            // 换了几条路径还是纹丝不动，才认这根轴真到边了；只零一拍多半是触控被压在起手点上的控件吃了
            const bool pinnedX = view.stallX >= kPinStreak;
            const bool pinnedY = view.stallY >= kPinStreak;
            // 对中要两根轴都到位：钉死的那根轴上还差出容差，另一根轴怎么拖都对不上
            const bool stuck =
                centering ? ((pinnedX && std::abs(need.x) > kZoomCenterTolerance) || (pinnedY && std::abs(need.y) > kZoomCenterTolerance))
                          : ((std::abs(need.x) < 1.0 || pinnedX) && (std::abs(need.y) < 1.0 || pinnedY));
            if (centering && (pans >= kMaxPans || stuck)) {
                // 这一拍的画面与视口仍然有效，下一圈按不对中的那套接着认
                LogInfo << "WorldMap: the map pans no further at minimum zoom, centering again while zooming in" << VAR(index) << VAR(pans)
                        << VAR(need.x) << VAR(need.y) << VAR(view.stallX) << VAR(view.stallY);
                centerPinned = true;
                continue;
            }
            if (pans >= kMaxPans || stuck) {
                // 挪不动了也别空手回去。出了安全区不等于出了能点的地方——那圈边距是留给识别的余量，
                // 目标只要还落在可用区里就照常认、照常点
                const cv::Rect usable = WorldMapSolver::SafeArea(view.screen.size(), kIconArea, 0);
                const cv::Point at(static_cast<int>(std::lround(expected.x)), static_cast<int>(std::lround(expected.y)));
                if (!usable.contains(at)) {
                    LogWarn << "WorldMap: the map will not pan any further and the target is out of reach" << VAR(param.zone) << VAR(index)
                            << VAR(pans) << VAR(expected.x) << VAR(expected.y) << VAR(view.stallX) << VAR(view.stallY);
                    return Probe::Miss;
                }
                LogWarn << "WorldMap: the map will not pan any further, taking the target where it stands" << VAR(param.zone) << VAR(pans)
                        << VAR(expected.x) << VAR(expected.y) << VAR(view.stallX) << VAR(view.stallY);
            }
            else {
                ++pans;
                LogInfo << (centering ? "WorldMap: bringing the target to the zoom center" : "WorldMap: target outside safe area, panning")
                        << VAR(expected.x) << VAR(expected.y) << VAR(need.x) << VAR(need.y) << VAR(view.gain);
                view.screen.release();
                view.issued = DragMap(session.controller, safe, need * view.gain);
                if (std::hypot(view.issued.x, view.issued.y) < 1.0) {
                    LogWarn << "WorldMap: target outside safe area but pan distance is degenerate" << VAR(index);
                    return Probe::Miss;
                }
                continue;
            }
        }

        ++attempt;

        json::object detail {
            { "zone", param.zone },
            { "index", static_cast<int>(index) },
            { "at", json::array { target.at.x, target.at.y } },
            { "screen", json::array { expected.x, expected.y } },
            { "viewport_scale", viewport.scale },
            { "viewport_vote", viewport.voteGrid },
        };
        if (!target.next.empty()) {
            detail.emplace("next", target.next);
        }
        if (wantZoom) {
            detail.emplace("zoom_steps", 0);
        }

        // 图标名没给就只把坐标解出来，认不认得出图标由调用方自己接着判
        if (!session.spec) {
            WriteDetail(out_detail, detail);
            if (out_box != nullptr) {
                *out_box = PointBox(expected);
            }
            LogInfo << "WorldMap: located" << VAR(param.zone) << VAR(expected.x) << VAR(expected.y);
            return Probe::Hit;
        }

        const auto icon = session.solver->ConfirmSpot(view.screen, expected, viewport.scale, session.spec->spot);
        if (icon && icon->unlocked != wantUnlocked) {
            // 解锁与否是规则不是识别失败，重试多少次都一样，立刻收场
            LogWarn << "WorldMap: the icon is here but not in the requested state" << VAR(param.zone) << VAR(param.icon)
                    << VAR(icon->unlocked) << VAR(wantUnlocked) << VAR(icon->goldRatio);
            return Probe::Miss;
        }
        if (icon && wantZoom && !zoomSkipped) {
            // 认到的图标还在容差圈外：按它的实际落点再对一次中，这一趟不算识别失败
            const cv::Point2d center = ZoomCenter(view.screen.size());
            if (!centerPinned && std::hypot(icon->center.x - center.x, icon->center.y - center.y) > kZoomCenterTolerance) {
                iconBias = icon->center - expected;
                --attempt;
                continue;
            }
            zoomSkipped = !ZoomKeepsInView(safe, center, icon->center);
            if (zoomSkipped) {
                LogInfo << "WorldMap: cannot bring the target to the zoom center, keeping minimum zoom" << VAR(index) << VAR(icon->center.x)
                        << VAR(icon->center.y);
            }
        }
        if (icon && wantZoom && !zoomSkipped) {
            const auto zoomed = ZoomOntoSpot(session, viewport, *icon, wantUnlocked);
            if (!zoomed) {
                LogWarn << "WorldMap: zoomed confirmation failed, back to minimum zoom" << VAR(attempt) << VAR(index);
                RestoreMinimumZoom(session);
                continue;
            }

            const cv::Point2d landed = zoomed->viewport.toScreen(target.at);
            detail["screen"] = json::array { landed.x, landed.y };
            detail["viewport_scale"] = zoomed->viewport.scale;
            detail["zoom_steps"] = zoomed->steps;
            detail.emplace("zoom_factor", zoomed->factor);
            detail.emplace("icon", param.icon);
            detail.emplace("template", zoomed->hit.templateName);
            detail.emplace("score", zoomed->hit.score);
            detail.emplace("rival", zoomed->hit.rivalScore);
            detail.emplace("unlocked", zoomed->hit.unlocked);
            detail.emplace("click", json::array { zoomed->hit.hotspot.x, zoomed->hit.hotspot.y });
            WriteDetail(out_detail, detail);
            if (out_box != nullptr) {
                *out_box = SpotBox(zoomed->hit);
            }
            return Probe::Hit;
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
            return Probe::Hit;
        }

        // 角色标记画在图标之上，它落在期望位置就是图标认不出来的原因：人已经站在这了。
        // 认不出图标的其他原因不会命中这一支
        if (session.spec->occludedByPlayer && wantUnlocked) {
            PlayerMarkerConfig markerCfg {};
            if (session.spec->spot.radiusBase > 0.0) {
                // 图标浮动多远，压在它上面的角色标记就离目标多远，窗口得跟着放到浮动区那么宽
                markerCfg.searchRadius = static_cast<int>(std::lround(session.spec->spot.radiusBase / viewport.scale));
            }
            const auto marker = WorldMapSolver::DetectPlayerMarker(view.screen, expected, markerCfg);
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
                return Probe::Hit;
            }
        }

        LogWarn << "WorldMap: icon not confirmed at expected position" << VAR(attempt) << VAR(index) << VAR(expected.x) << VAR(expected.y)
                << VAR(viewport.voteGrid);
        view.screen.release();
        std::this_thread::sleep_for(std::chrono::milliseconds(kRetryMillis));
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

    if (!param.state.empty() && spec && spec->spot.minGoldRatio <= 0.0) {
        LogError << "WorldMap: this icon has no unlock threshold, 'state' cannot be judged" << VAR(param.icon) << VAR(param.state);
        return false;
    }

    const std::vector<Target> targets = BuildTargets(param);
    LogInfo << "WorldMap: find" << VAR(param.zone) << VAR(targets.size()) << VAR(param.icon) << VAR(param.state) << VAR(param.max_attempts)
            << VAR(param.zoom_in);

    ZoomMapOut(context);

    FindSession session;
    session.context = context;
    session.controller = controller;
    session.solver = &solver;
    session.viewportCfg = viewportCfg;
    session.param = &param;
    session.spec = spec;

    // 一组候选共用这一次缩放，也共用它留下的画面：缩放那几趟自己会截屏，框架给进来的那一帧已过期。
    // 认下来一个就收场，剩下的候选连地图都不用再挪
    for (std::size_t index = 0; index < targets.size(); ++index) {
        const Target& target = targets[index];
        if (!target.next.empty() && !CandidateEnabled(context, target.next)) {
            LogInfo << "WorldMap: candidate turned off, skipping it" << VAR(index) << VAR(target.next);
            continue;
        }
        const Probe probe = ProbeTarget(session, target, index, out_box, out_detail);
        if (probe == Probe::Fatal) {
            return false;
        }
        if (probe == Probe::Miss) {
            continue;
        }
        // 交不回去就不算认出来。命中即停，后面的候选已经没机会了，此时报成功等于让
        // 上层拿着这个位置去走节点自己那份 next——点的是这个候选，走的是别处
        if (!target.next.empty() && !HandBackNext(context, node_name, target.next)) {
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
