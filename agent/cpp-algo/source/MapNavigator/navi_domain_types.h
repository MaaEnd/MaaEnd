#pragma once

#include <chrono>
#include <string>
#include <string_view>
#include <utility>

#include "navi_config.h"

namespace mapnavigator
{

// RUN      - 纯推算目标点，到达该点时不执行任何特殊操作
// SPRINT   - 到达该点时触发一次右键冲刺
// JUMP     - 到达该点时按下空格
// FIGHT    - 到达该点时刹车，左键攻击一次
// INTERACT - 到达该点时刹车，狂按F键
// TRANSFER - 精确抵达该点后停住，等待机关/跳板/回传等把角色转移到下一段可达路径
// PORTAL   - 跨区过渡节点，触发后进入盲走等待区域切换
// HEADING  - 无坐标朝向节点，执行时只调整镜头到指定角度，再按下W继续前进
// ZONE     - 无坐标区域声明节点，要求后续定位稳定落在指定 zone 后再继续
#define NAVI_ACTION_TYPES(X) \
    X(RUN)                   \
    X(SPRINT)                \
    X(JUMP)                  \
    X(FIGHT)                 \
    X(INTERACT)              \
    X(TRANSFER)              \
    X(PORTAL)                \
    X(HEADING)               \
    X(ZONE)

enum class ActionType
{
#define NAVI_X_(name) name,
    NAVI_ACTION_TYPES(NAVI_X_)
#undef NAVI_X_
};

inline bool TryActionTypeFromString(std::string_view str, ActionType* out_action)
{
#define NAVI_X_(name) { #name, ActionType::name },
    static constexpr std::pair<std::string_view, ActionType> kMap[] = { NAVI_ACTION_TYPES(NAVI_X_) };
#undef NAVI_X_

    for (auto [name, type] : kMap) {
        if (name == str) {
            if (out_action != nullptr) {
                *out_action = type;
            }
            return true;
        }
    }
    return false;
}

inline ActionType ActionTypeFromString(std::string_view str)
{
    ActionType action = ActionType::RUN;
    TryActionTypeFromString(str, &action);
    return action;
}

struct Waypoint
{
    double x;
    double y;
    ActionType action;
    bool has_position;
    bool strict_arrival;
    double heading_angle;
    std::string zone_id;

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

    bool RequiresStrictArrival() const
    {
        if (!has_position) {
            return false;
        }
        return strict_arrival || action == ActionType::SPRINT || action == ActionType::JUMP || action == ActionType::INTERACT
               || action == ActionType::FIGHT || action == ActionType::TRANSFER || action == ActionType::PORTAL;
    }

    bool HasPosition() const { return has_position; }

    bool IsHeadingOnly() const { return action == ActionType::HEADING; }

    bool IsZoneDeclaration() const { return action == ActionType::ZONE; }

    bool WaitsForRelocation() const { return action == ActionType::TRANSFER; }

    bool IsControlNode() const { return !has_position; }

    Waypoint()
        : x(0.0)
        , y(0.0)
        , action(ActionType::RUN)
        , has_position(true)
        , strict_arrival(false)
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
        waypoint.heading_angle = angle;
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

struct TurnActuationResult
{
    int units_sent = 0;
};

class ITurnActuator
{
public:
    virtual ~ITurnActuator() = default;
    virtual TurnActuationResult TurnByUnits(int units, int duration_millis) = 0;
};

constexpr double kPi = 3.14159265358979323846;

} // namespace mapnavigator
