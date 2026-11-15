#pragma once

#include <array>
#include <chrono>
#include <limits>
#include <optional>
#include <string>
#include <vector>

#include <meojson/json.hpp>

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
    double commit_distance = 0.0;    // 判定圈下限 px, 0 = 不放宽
};

ActionTraits TraitsOf(ActionType action);

// 执行后可能留下挡住小地图的界面
bool MayOpenUi(ActionType action);

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
    double x = 0.0;
    double y = 0.0;
    ActionType action = ActionType::RUN;
    bool has_position = true;
    bool strict_arrival = false;
    // 线路明写的 strict_arrival 原样副本, strict_arrival 本身还会被引擎置位
    bool authored_strict_arrival = false;
    // 该点处的通道半宽 px, 0 = 未知(非 navmesh 规划的点)
    double corridor_clearance = 0.0;
    bool heading_uses_target = false;
    double heading_angle = 0.0;
    std::string zone_id;
    // 区名压扁前所指层的主楼面高
    std::optional<double> zone_floor_y;
    // x/y 所在的 tier 坐标系, 执行前投影到基础像素坐标, 空表示 x/y 已是基础坐标
    std::string target_tier;
    // 仅 NAVMESH: 本点所在重叠层的高度, 钉住进出两段的层, 未设则不限层
    std::optional<double> target_deck_y;
    // 仅 navmesh 规划点: 规划线在此点所在面的高度
    std::optional<double> route_floor_y;
    // 台沿下落落点专用的起跳点, 沿起跳到落点方向越过本点才算到
    std::optional<std::array<double, 2>> drop_from;
    // 仅作者路线: 滑索不许越过这个点
    bool route_required = false;
    // INTERACT 专用: 停车后用作 OCR expected, 留空则不算异步交互
    std::vector<std::string> interact_text;
    // 作者写 { "node": ... } 时的节点名, 开跑前读出其 expected 填进 interact_text
    std::string interact_text_node;
    // 行进预筛用的 TemplateMatch 节点, 留空用默认节点
    std::string interact_scan;
    // 只认提示不按键, 路线顶层设的是默认值, 点上只能开不能关
    bool interact_rec = false;
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
    // 展开来源在作者 path 中的段首下标, 作者原点和运行时生成的点为 max
    size_t authored_group_begin = std::numeric_limits<size_t>::max();
};

bool RequiresStrictArrival(const Waypoint& waypoint);

double GetLookahead(const Waypoint& waypoint);

// 只给 interact_scan 不给文本的点不算异步交互, 共用节点里的占位文本认不出东西
bool IsAsyncInteract(const Waypoint& waypoint);

// 不看文本, 否则文本解析失败的点会退回到点连按交互键
bool IsRecInteract(const Waypoint& waypoint);

// 上索点也认提示, 但只有链首要认, 那道收紧在状态机里
bool StopsOnPromptDetection(const Waypoint& waypoint);

double BaseArrivalBand(const Waypoint& waypoint, double position_quantum);

double ClampArrivalBandToCorridor(const Waypoint& waypoint, double band);

double ArrivalBand(const Waypoint& waypoint, double position_quantum);

double UntightenedArrivalBand(const Waypoint& waypoint, double position_quantum);

bool SettlesAtArrival(const Waypoint& waypoint);

bool IsContinuousRun(const Waypoint& waypoint);

bool IsHeadingOnly(const Waypoint& waypoint);

bool IsFindPoint(const Waypoint& waypoint);

bool IsTriggerPoint(const Waypoint& waypoint);

bool IsMovementPoint(const Waypoint& waypoint);

// 滑索只顶替两个边界之间的走路, 动作点和 required 点都是边界
bool EndsReplaceableSpan(const Waypoint& waypoint);

bool IsZoneDeclaration(const Waypoint& waypoint);

Waypoint HeadingWaypoint(double angle);

Waypoint HeadingToTargetWaypoint(double target_x, double target_y);

Waypoint ZoneWaypoint(std::string zone);

void OffsetAuthoredGroups(std::vector<Waypoint>* path, size_t offset);

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
