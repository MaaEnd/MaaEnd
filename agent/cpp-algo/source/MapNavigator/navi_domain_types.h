#pragma once

#include <algorithm>
#include <chrono>
#include <optional>
#include <string>
#include <vector>

#include <meojson/json.hpp>

#include "navi_config.h"

namespace mapnavigator
{

// RUN      - 纯推算目标点，到达该点时不执行任何特殊操作
// SPRINT   - 到达该点时触发一次右键冲刺
// JUMP     - 到达该点时按下空格
// FIGHT    - 到达该点时刹车，左键攻击一次
// INTERACT - 到达该点时刹车交互一次。路线给了 interact_text 则升级为异步交互：行进中检测到交互提示就停车
//            跑一次子任务，到点时再兜底一次；没给文本就保持原语义——到点狂按F键。interact_scan 只换预筛看
//            什么，得配着 interact_text 用。动作恒为交互键，不可换。
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
    X(DIG)

enum class ActionType
{
#define NAVI_X_(name) name,
    NAVI_ACTION_TYPES(NAVI_X_)
#undef NAVI_X_
    MEOJSON_ENUM_RANGE(RUN, DIG)
};

struct Waypoint
{
    double x;
    double y;
    ActionType action;
    bool has_position;
    bool strict_arrival;
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
    // INTERACT 专用: 该点的提示文字, 停车后当 OCR expected 用。留空则不做这次确认, 该点也就不算异步交互
    std::vector<std::string> interact_text;
    // INTERACT 专用: 行进预筛读 roi/template/threshold 的 TemplateMatch 节点, 留给提示长得不一样的业务;
    // 留空用出厂那份
    std::string interact_scan;

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

    bool RequiresStrictArrival() const
    {
        if (!has_position) {
            return false;
        }
        return strict_arrival || action == ActionType::SPRINT || action == ActionType::JUMP || action == ActionType::INTERACT
               || action == ActionType::FIGHT || action == ActionType::TRANSFER || action == ActionType::PORTAL
               || action == ActionType::NAVMESH || action == ActionType::DIG;
    }

    // 路线说了停下后认什么才走异步交互。只换预筛不给文本的点走不通: 共用识别节点里的占位文本没被顶掉, 停下来
    // 也认不出东西, 所以那种点退回原语义而不是白停一次。
    bool IsAsyncInteract() const { return action == ActionType::INTERACT && !interact_text.empty(); }

    // 走到跟前才算数的点: 交互提示得在屏幕上待得住, 所以判定圈、疾跑抑制、切走路都按同一套来
    bool StopsOnPromptDetection() const { return action == ActionType::COLLECT || IsAsyncInteract(); }

    bool HasPosition() const { return has_position; }

    bool IsHeadingOnly() const { return action == ActionType::HEADING; }

    bool IsZoneDeclaration() const { return action == ActionType::ZONE; }

    bool IsControlNode() const { return !has_position; }

    Waypoint()
        : x(0.0)
        , y(0.0)
        , action(ActionType::RUN)
        , has_position(true)
        , strict_arrival(false)
        , corridor_clearance(0.0)
        , heading_uses_target(false)
        , heading_angle(0.0)
        , zone_id()
    {
    }

    Waypoint(double waypoint_x, double waypoint_y, ActionType waypoint_action = ActionType::RUN)
        : x(waypoint_x)
        , y(waypoint_y)
        , action(waypoint_action)
        , has_position(true)
        , strict_arrival(false)
        , corridor_clearance(0.0)
        , heading_uses_target(false)
        , heading_angle(0.0)
        , zone_id()
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

struct NaviPosition
{
    double x = 0.0;
    double y = 0.0;
    double angle = 0.0;
    bool valid = false;
    std::string zone_id;
    std::chrono::steady_clock::time_point timestamp;
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
