#pragma once

#include <algorithm>
#include <array>
#include <chrono>
#include <limits>
#include <optional>
#include <string>
#include <vector>

#include <meojson/json.hpp>

#include "navi_config.h"
#include "navi_position.h"
#include "zipline_types.h"

namespace mapnavigator
{

// RUN      - 纯推算目标点，到达该点时不执行任何特殊操作
// SPRINT   - 到达该点时触发一次右键冲刺
// JUMP     - 到达该点时按下空格
// FIGHT    - 到达该点时刹车，左键攻击一次
// INTERACT - 到达该点时刹车交互一次。路线给了 interact_text 则升级为异步交互：行进中检测到交互提示就停车
//            跑一次子任务，到点时再兜底一次；没给文本就保持原语义——到点狂按F键。文字可以直接写，也可以写
//            { "node": ... } 指一个 OCR 节点，多条路线共用一张表。interact_scan 只换预筛看什么，得配着
//            interact_text 用。动作恒为交互键，不可换；写 interact_rec 走 rec 模式，只认提示不按键。
// TRANSFER - 精确抵达该点后停住，等待机关/跳板/回传等把角色转移到下一段可达路径
// PORTAL   - 跨区过渡节点，触发后进入盲走等待区域切换
// HEADING  - 无坐标朝向节点，执行时只调整镜头到指定角度，再按下W继续前进
// NAVMESH  - 语义寻路节点，读取 .nav 并从当前定位位置自动规划到 target
// ZONE     - 无坐标区域声明节点，要求后续定位稳定落在指定 zone 后再继续
// COLLECT  - 仅作为"开启采集扫描"的路径点：经过时按普通路点直接推进，不再到点停车。
//            采集完全由行进中的异步图标检测驱动——检测到采集物才立即停车并触发
//            AutoCollectClickStart 子任务（OCR + AutoAltClickAction），没有采集物时不空停。
//            （检测只受冷却限速，误报由 OCR 名称白名单挡下，最多白停一次）
// DIG      - 触发 AutoCollectDigStart pipeline 子任务（无条件 Click target=true 两次），用于挖掘点。
//            与 COLLECT 不同，DIG 仍是精确抵达后停车触发（挖掘是定点动作，非行进检测）
// FIND     - 寻找并接近一个只有识别框、没有坐标的目标：拿点名节点给出的框直接对着世界走
//            （转视角搜索 / 对准了前进 / 走过头退一步），全程不读小地图。命中 find_stop 即算到位，
//            位置走到 find_arrive 附近同样算到位（两者都给就先到先算）。带 target 先走到锚点再找，
//            不带就是就地开找的控制节点
// ZIPLINE  - 滑索上索点：精确抵达后停车，把镜头转向下索点再交互上索，然后等滑行自己结束。
//            只由滑索规划生成，不手写：能不能滑取决于两端的落差与跨度，那是规划器算出来的
#define NAVI_ACTION_TYPES(X) \
    X(RUN)                   \
    X(SPRINT)                \
    X(JUMP)                  \
    X(FIGHT)                 \
    X(INTERACT)              \
    X(TRANSFER)              \
    X(PORTAL)                \
    X(HEADING)               \
    X(NAVMESH)               \
    X(ZONE)                  \
    X(COLLECT)               \
    X(DIG)                   \
    X(FIND)                  \
    X(ZIPLINE)

enum class ActionType
{
#define NAVI_X_(name) name,
    NAVI_ACTION_TYPES(NAVI_X_)
#undef NAVI_X_
    MEOJSON_ENUM_RANGE(RUN, ZIPLINE)
};

// 每种动作的静态策略, 一种动作一行; 到点后做什么见 semantic_nodes 的到点分发
struct ActionTraits
{
    bool strict_arrival = false;     // 到点必须精确停住, 线路上的 strict_arrival 只能再加严
    bool settles_at_arrival = false; // 进圈后末端纠正到位才验收, 只对线路明写 strict_arrival 的点
    bool walk_approach = false;      // 接近段允许切走路
    bool walk_at_startup = false;    // 起步位移还没确认也允许走路
    bool settle_walking = false;     // 末端纠正前先切走路
    bool route_boundary = false;     // 全局规划分段的固有边界
    double commit_distance = 0.0;    // 判定圈下限 px, 0 = 不放宽
};

constexpr ActionTraits TraitsOf(ActionType action)
{
    switch (action) {
    case ActionType::RUN:
        return { .settles_at_arrival = true, .walk_approach = true };
    case ActionType::SPRINT:
        return { .strict_arrival = true, .settles_at_arrival = true };
    case ActionType::JUMP:
        return { .strict_arrival = true, .settles_at_arrival = true };
    case ActionType::FIGHT:
        return { .strict_arrival = true, .settles_at_arrival = true };
    case ActionType::INTERACT:
        return { .strict_arrival = true, .walk_approach = true };
    case ActionType::TRANSFER:
        return { .strict_arrival = true, .settles_at_arrival = true };
    case ActionType::PORTAL:
        return { .strict_arrival = true, .commit_distance = kPortalCommitDistance };
    case ActionType::HEADING:
        return { .route_boundary = true };
    case ActionType::NAVMESH:
        return { .strict_arrival = true, .settles_at_arrival = true, .walk_approach = true };
    case ActionType::ZONE:
        return {};
    case ActionType::COLLECT:
        return { .walk_approach = true, .route_boundary = true };
    case ActionType::DIG:
        return {
            .strict_arrival = true,
            .settles_at_arrival = true,
            .walk_approach = true,
            .walk_at_startup = true,
            .settle_walking = true,
            .route_boundary = true,
        };
    // 不设 strict_arrival：锚点只是「站这儿开始找」，收尾由视觉伺服完成
    case ActionType::FIND:
        return { .walk_approach = true, .route_boundary = true };
    case ActionType::ZIPLINE:
        return { .strict_arrival = true, .walk_approach = true };
    }
    return {};
}

// 运行期在卡死点前方生成的圆形禁区, 用于占位网格中未记录的障碍, 此后每次规划都绕开它。
// 圆心按生成时所在定位区的坐标记录, 仅对同区规划生效。push_through 表示该禁区封闭了唯一通路,
// 规划时不再计入, 只作为恢复流程判定此处需要物理脱困的依据。
struct VirtualNoGoDisc
{
    std::string zone_id;
    double x = 0.0;
    double y = 0.0;
    double radius = 0.0;
    bool push_through = false;
};

struct Waypoint
{
    double x;
    double y;
    ActionType action;
    bool has_position;
    bool strict_arrival;
    // 线路里明写的那个 strict_arrival, 原样留一份。strict_arrival 本身还被解析器、展开器、bootstrap、
    // 滑索当精度标志置位, 读它分不出是作者写的还是引擎自己加的
    bool authored_strict_arrival;
    // 该点处的通道半宽 px, 0 = 未知(非 navmesh 规划的点)
    double corridor_clearance;
    bool heading_uses_target;
    double heading_angle;
    std::string zone_id;
    // Optional authored coordinate frame for x/y. Native MapNavigator projects an explicitly tagged
    // NAVMESH target, regular positioned waypoint, or target-based HEADING through this tier's baked affine
    // onto the base-pixel execution frame. Empty keeps the legacy coordinates unchanged.
    std::string target_tier;
    // NAVMESH only: height of the overlapping deck this waypoint sits on. Pins the goal span for the leg
    // ending here and the start span for the leg leaving it. Unset -> full span set, unchanged.
    std::optional<double> target_deck_y;
    // Authored path only: make this node a hard boundary between globally planned legs. Coordinate-bearing
    // movement nodes are optional route/action hints by default. HEADING is an explicit control command;
    // COLLECT and DIG are task-producing markers. Those three are intrinsic boundaries even without this flag.
    bool route_required;
    // INTERACT 专用: 该点的提示文字, 停车后当 OCR expected 用。留空则不做这次确认, 该点也就不算异步交互
    std::vector<std::string> interact_text;
    // INTERACT 专用: 作者写的是 { "node": ... } 时先落在这里, 开跑前从那个 OCR 节点读出 expected 填进
    // interact_text; 读不出来该点退回原语义
    std::string interact_text_node;
    // INTERACT 专用: 行进预筛读 roi/template/threshold 的 TemplateMatch 节点, 留给提示长得不一样的业务;
    // 留空用出厂那份
    std::string interact_scan;
    // INTERACT 专用: rec 模式, 只认提示不按键。判定圈、行进中提示停车都照旧, 按不按、按哪个留给业务侧决定。
    // 写在路线顶层是整条路线的默认, 点上只能开不能关
    bool interact_rec;
    // FIND 专用：目标识别节点名，与 find_text 二选一
    std::string find_target;
    // FIND 专用：内联 OCR 文本表，执行时注入内置节点
    std::vector<std::string> find_text;
    // FIND 专用：命中它就算到位，与 find_arrive 至少给一个
    std::string find_stop;
    // FIND 专用：走到这个坐标附近也算到位。给了它才读定位, 判定只看 x/y 距离, 不参与转向
    std::optional<std::array<double, 2>> find_arrive;
    // ZIPLINE only: 这一跳的计划(两端架子、落点仰角、备用站位)。只由滑索规划写入; 缺这个字段的
    // ZIPLINE 点是配置写错了, 执行侧拒绝
    std::optional<ZiplineHopPlan> zipline_hop;
    // 展开路径专用: 这个点由原始作者 path 的哪一组(组首下标)展开而来。滑索链半路失败时按它把
    // 进度折回作者路线重新展开; 作者原始点和运行时生成的点不带(= max)。
    size_t authored_group_begin = std::numeric_limits<size_t>::max();

    double GetLookahead() const
    {
        if (!has_position) {
            return 0.0;
        }
        if (RequiresStrictArrival()) {
            return kStrictArrivalLookaheadRadius;
        }
        return kLookaheadRadius;
    }

    // 到点判定圈半径, 通道比判定圈还窄时按通道收紧, 否则角色会提前弃点直奔下一点, 抄出撞墙的弦
    // 提示驱动的点另按 kCollectArrivalBandWu 收紧(点距比常规判定圈还小), relax_tight_band 是够不着时的退让
    double ArrivalBand(double position_quantum, bool relax_tight_band = false) const
    {
        const bool strict = RequiresStrictArrival();
        double band = strict ? GetLookahead() + position_quantum : GetLookahead() + kWaypointArrivalSlack + position_quantum;
        if (StopsOnPromptDetection() && !relax_tight_band) {
            band = std::min(band, kCollectArrivalBandWu);
        }
        if (strict || corridor_clearance <= 0.0) {
            return band;
        }
        return std::min(band, std::max(corridor_clearance, kMinArrivalBand));
    }

    ActionTraits Traits() const { return TraitsOf(action); }

    bool RequiresStrictArrival() const { return has_position && (strict_arrival || Traits().strict_arrival); }

    // 末端纠正到位才验收的点, 只认线路明写的 strict_arrival
    bool SettlesAtArrival() const { return has_position && authored_strict_arrival && Traits().settles_at_arrival; }

    // 顺着走过去就算数的点: 走廊跟随、经过即推进都只对这种点生效
    bool IsContinuousRun() const { return has_position && action == ActionType::RUN && !RequiresStrictArrival(); }

    // 路线说了停下后认什么才走异步交互。只换预筛不给文本的点走不通: 共用识别节点里的占位文本没被顶掉, 停下来
    // 也认不出东西, 所以那种点退回原语义而不是白停一次。
    bool IsAsyncInteract() const { return action == ActionType::INTERACT && !interact_text.empty(); }

    // 不看有没有文本: 文本没解析出来的点会退回原语义(到点狂按F), 那正是 rec 模式要避开的
    bool IsRecInteract() const { return action == ActionType::INTERACT && interact_rec; }

    // 走到跟前才算数的点: 交互提示得在屏幕上待得住, 所以判定圈、疾跑抑制、切走路都按同一套来
    // 上索点也认提示, 但只有链首那一次要认 —— 收不收得看运行时上没上索, 所以那道收紧在状态机里
    bool StopsOnPromptDetection() const { return action == ActionType::COLLECT || IsAsyncInteract(); }

    bool HasPosition() const { return has_position; }

    bool IsHeadingOnly() const { return action == ActionType::HEADING; }

    bool IsFindPoint() const { return action == ActionType::FIND; }

    bool IsIntrinsicRouteBoundary() const { return Traits().route_boundary; }

    bool ClosesGlobalRouteGroup() const { return route_required || IsIntrinsicRouteBoundary(); }

    bool IsZoneDeclaration() const { return action == ActionType::ZONE; }

    bool IsControlNode() const { return !has_position; }

    Waypoint()
        : x(0.0)
        , y(0.0)
        , action(ActionType::RUN)
        , has_position(true)
        , strict_arrival(false)
        , authored_strict_arrival(false)
        , corridor_clearance(0.0)
        , heading_uses_target(false)
        , heading_angle(0.0)
        , zone_id()
        , route_required(false)
        , interact_rec(false)
    {
    }

    Waypoint(double waypoint_x, double waypoint_y, ActionType waypoint_action = ActionType::RUN)
        : x(waypoint_x)
        , y(waypoint_y)
        , action(waypoint_action)
        , has_position(true)
        , strict_arrival(false)
        , authored_strict_arrival(false)
        , corridor_clearance(0.0)
        , heading_uses_target(false)
        , heading_angle(0.0)
        , zone_id()
        , route_required(false)
        , interact_rec(false)
    {
    }

    static Waypoint Heading(double angle)
    {
        Waypoint waypoint;
        waypoint.action = ActionType::HEADING;
        waypoint.has_position = false;
        waypoint.strict_arrival = false;
        waypoint.heading_uses_target = false;
        waypoint.heading_angle = angle;
        return waypoint;
    }

    static Waypoint HeadingToTarget(double target_x, double target_y)
    {
        Waypoint waypoint;
        waypoint.x = target_x;
        waypoint.y = target_y;
        waypoint.action = ActionType::HEADING;
        waypoint.has_position = false;
        waypoint.strict_arrival = false;
        waypoint.heading_uses_target = true;
        waypoint.heading_angle = 0.0;
        return waypoint;
    }

    static Waypoint Zone(std::string zone)
    {
        Waypoint waypoint;
        waypoint.action = ActionType::ZONE;
        waypoint.has_position = false;
        waypoint.strict_arrival = false;
        waypoint.zone_id = std::move(zone);
        return waypoint;
    }
};

struct TurnCommandResult
{
    bool issued = false;
    double issued_delta_degrees = 0.0;
    int64_t send_ms = 0;
};

enum class MotionPredictMode
{
    Idle,
    Walk,
    Sprint,
    Corrective,
};

enum class LocalDriverAction
{
    Forward,
    JumpForward,
    BackwardJump,
};

constexpr double kPi = 3.14159265358979323846;

} // namespace mapnavigator
