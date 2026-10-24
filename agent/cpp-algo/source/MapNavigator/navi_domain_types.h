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
#include "zipline/zipline_types.h"

namespace mapnavigator
{

// 路线约定: JUMP_PAD 坐标写板心, ZIPLINE 只由滑索规划生成, TRIGGER 只能做路线终点
#define NAVI_ACTION_TYPES(X) \
    X(RUN)                   \
    X(SPRINT)                \
    X(JUMP)                  \
    X(FIGHT)                 \
    X(INTERACT)              \
    X(TRANSFER)              \
    X(JUMP_PAD)              \
    X(PORTAL)                \
    X(HEADING)               \
    X(NAVMESH)               \
    X(ZONE)                  \
    X(COLLECT)               \
    X(DIG)                   \
    X(FIND)                  \
    X(ZIPLINE)               \
    X(TRIGGER)

enum class ActionType
{
#define NAVI_X_(name) name,
    NAVI_ACTION_TYPES(NAVI_X_)
#undef NAVI_X_
    MEOJSON_ENUM_RANGE(RUN, TRIGGER)
};

struct ActionTraits
{
    bool strict_arrival = false;     // 线路上的 strict_arrival 只能再加严
    bool settles_at_arrival = false; // 进圈后末端纠正到位才验收, 除 INTERACT 外只对线路明写 strict_arrival 的点
    bool walk_approach = false;
    bool walk_at_startup = false;    // 起步位移还没确认也允许走路
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
        return { .strict_arrival = true, .settles_at_arrival = true, .walk_approach = true };
    case ActionType::TRANSFER:
        return { .strict_arrival = true, .settles_at_arrival = true };
    // 不做末端纠正, 纠正途中就可能被跳板弹走
    case ActionType::JUMP_PAD:
        return { .strict_arrival = true, .walk_approach = true };
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
            .route_boundary = true,
        };
    case ActionType::FIND:
        return { .walk_approach = true, .route_boundary = true };
    case ActionType::ZIPLINE:
        return { .strict_arrival = true, .walk_approach = true };
    // 须设为边界, 否则滑索规划会把它换成不带 trigger_node 的点
    case ActionType::TRIGGER:
        return { .strict_arrival = true, .settles_at_arrival = true, .route_boundary = true };
    }
    return {};
}

// 从绳底交互坐上去, ride_s 秒后站在 top_height 那层
struct RopeRideInfo
{
    uint32_t uid = 0;
    double ride_s = 0.0;
    double top_height = 0.0;
};

// 圆心是 zone_id 区内坐标, 只对同区规划生效
struct VirtualNoGoDisc
{
    std::string zone_id;
    double x = 0.0;
    double y = 0.0;
    double radius = 0.0;
    bool push_through = false; // 封住了唯一通路, 不参与规划, 只标记此处需物理脱困
};

struct Waypoint
{
    double x;
    double y;
    ActionType action;
    bool has_position;
    bool strict_arrival;
    // 线路明写的 strict_arrival 原样副本, strict_arrival 本身还会被引擎置位
    bool authored_strict_arrival;
    // 该点处的通道半宽 px, 0 = 未知(非 navmesh 规划的点)
    double corridor_clearance;
    bool heading_uses_target;
    double heading_angle;
    std::string zone_id;
    // x/y 所在的 tier 坐标系, 执行前投影到基础像素坐标, 空表示 x/y 已是基础坐标
    std::string target_tier;
    // 仅 NAVMESH: 本点所在重叠层的高度, 钉住进出两段的层, 未设则不限层
    std::optional<double> target_deck_y;
    // 仅 navmesh 规划点: 规划线在此点所在面的高度
    std::optional<double> route_floor_y;
    // 台沿下落落点专用的起跳点, 沿起跳到落点方向越过本点才算到
    std::optional<std::array<double, 2>> drop_from;
    // 仅作者路线: 强制作为全局规划分段边界
    bool route_required;
    // INTERACT 专用: 停车后用作 OCR expected, 留空则不算异步交互
    std::vector<std::string> interact_text;
    // 作者写 { "node": ... } 时的节点名, 开跑前读出其 expected 填进 interact_text
    std::string interact_text_node;
    // 行进预筛用的 TemplateMatch 节点, 留空用默认节点
    std::string interact_scan;
    // 只认提示不按键, 路线顶层设的是默认值, 点上只能开不能关
    bool interact_rec;
    // 目标识别节点名, 与 find_text 二选一
    std::string find_target;
    // 内联 OCR 文本, 执行时注入内置节点
    std::vector<std::string> find_text;
    // 命中即到位, 与 find_arrive 至少给一个
    std::string find_stop;
    // 走到此坐标附近也算到位, 只看 x/y 距离, 不参与转向
    std::optional<std::array<double, 2>> find_arrive;
    // 命中即算导航成功, 只跑识别不执行动作
    std::string trigger_node;
    // 只由滑索规划写入, 缺它的 ZIPLINE 点执行侧拒绝
    std::optional<ZiplineHopPlan> zipline_hop;
    std::optional<RopeRideInfo> rope_ride;
    // 绳顶落脚点, 坐完升降索即算到达
    bool rope_top = false;
    // 展开来源在作者 path 中的组首下标, 作者原点和运行时生成的点为 max
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

    bool SettlesAtArrival() const
    {
        if (!has_position || !Traits().settles_at_arrival) {
            return false;
        }
        if (action == ActionType::INTERACT) {
            return !IsAsyncInteract();
        }
        return authored_strict_arrival;
    }

    bool IsContinuousRun() const { return has_position && action == ActionType::RUN && !RequiresStrictArrival(); }

    // 只给 interact_scan 不给文本的点不算异步交互, 共用节点里的占位文本认不出东西
    bool IsAsyncInteract() const { return action == ActionType::INTERACT && !interact_text.empty(); }

    // 不看文本, 否则文本解析失败的点会退回到点连按交互键
    bool IsRecInteract() const { return action == ActionType::INTERACT && interact_rec; }

    // 上索点也认提示, 但只有链首要认, 那道收紧在状态机里
    bool StopsOnPromptDetection() const { return action == ActionType::COLLECT || IsAsyncInteract(); }

    bool HasPosition() const { return has_position; }

    bool IsHeadingOnly() const { return action == ActionType::HEADING; }

    bool IsFindPoint() const { return action == ActionType::FIND; }

    bool IsTriggerPoint() const { return action == ActionType::TRIGGER; }

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
