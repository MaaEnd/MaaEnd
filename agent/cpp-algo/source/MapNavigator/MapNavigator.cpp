#include <algorithm>
#include <cctype>
#include <cstring>
#include <string>
#include <utility>
#include <vector>

#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "MapNavigator.h"
#include "navi_controller.h"

#ifndef MAA_TRUE
#define MAA_TRUE 1
#endif
#ifndef MAA_FALSE
#define MAA_FALSE 0
#endif

namespace mapnavigator
{

namespace
{

bool TryReadNumber(const json::value& value, double& out)
{
    if (!value.is_number()) {
        return false;
    }
    out = value.as_double();
    return true;
}

bool TryReadBool(const json::value& value, bool& out)
{
    if (!value.is_boolean()) {
        return false;
    }
    out = value.as_boolean();
    return true;
}

bool LooksLikeActionToken(const std::string& text)
{
    if (text.empty()) {
        return false;
    }
    return std::all_of(text.begin(), text.end(), [](char ch) {
        return std::isupper(static_cast<unsigned char>(ch)) || ch == '_';
    });
}

bool IsActionKeywordCaseInsensitive(const std::string& text)
{
    std::string normalized;
    normalized.reserve(text.size());
    for (char ch : text) {
        normalized.push_back(static_cast<char>(std::toupper(static_cast<unsigned char>(ch))));
    }

#define NAVI_X_(name)      \
    if (normalized == #name) { \
        return true;       \
    }
    NAVI_ACTION_TYPES(NAVI_X_)
#undef NAVI_X_
    return false;
}

bool TryParseActionType(const json::value& value, ActionType& out_action)
{
    if (!value.is_string()) {
        return false;
    }

    const std::string action_text = value.as_string();
#define NAVI_X_(name)                  \
    if (action_text == #name) {        \
        out_action = ActionType::name; \
        return true;                   \
    }
    NAVI_ACTION_TYPES(NAVI_X_)
#undef NAVI_X_
    return false;
}

bool TryAppendActionList(const json::value& value, std::vector<ActionType>& out_actions)
{
    ActionType parsed_action = ActionType::RUN;
    if (TryParseActionType(value, parsed_action)) {
        out_actions.push_back(parsed_action);
        return true;
    }

    if (!value.is_array()) {
        return false;
    }
    if (value.as_array().empty()) {
        return true;
    }

    for (const auto& item : value.as_array()) {
        if (!TryParseActionType(item, parsed_action)) {
            return false;
        }
        out_actions.push_back(parsed_action);
    }
    return true;
}

bool TryReadZoneId(const json::object& obj, std::string& out_zone_id)
{
    static constexpr const char* kZoneKeys[] = { "zone_id", "zoneId", "zone", "map_name", "mapName" };
    for (const char* key : kZoneKeys) {
        if (!obj.contains(key) || !obj.at(key).is_string()) {
            continue;
        }

        out_zone_id = obj.at(key).as_string();
        if (!out_zone_id.empty()) {
            return true;
        }
    }
    return false;
}

void TryReadStrictArrival(const json::object& obj, Waypoint& waypoint)
{
    static constexpr const char* kStrictKeys[] = { "strict", "strict_arrival", "strictArrival" };
    for (const char* key : kStrictKeys) {
        if (!obj.contains(key)) {
            continue;
        }

        bool strict_arrival = false;
        if (TryReadBool(obj.at(key), strict_arrival)) {
            waypoint.strict_arrival = strict_arrival;
            return;
        }
    }
}

void AppendExpandedWaypoints(
    double tx,
    double ty,
    const std::vector<ActionType>& actions,
    const std::string& zone_id,
    bool strict_arrival,
    std::vector<Waypoint>& out_waypoints)
{
    const bool has_non_run_action =
        std::any_of(actions.begin(), actions.end(), [](ActionType action) { return action != ActionType::RUN; });
    if (actions.empty()) {
        Waypoint waypoint(tx, ty, ActionType::RUN);
        waypoint.zone_id = zone_id;
        waypoint.strict_arrival = strict_arrival;
        out_waypoints.push_back(std::move(waypoint));
        return;
    }

    for (ActionType action : actions) {
        if (has_non_run_action && action == ActionType::RUN) {
            continue;
        }
        Waypoint waypoint(tx, ty, action);
        waypoint.zone_id = zone_id;
        waypoint.strict_arrival = strict_arrival;
        out_waypoints.push_back(std::move(waypoint));
    }
}

bool TryReadAngleValue(const json::object& obj, double& angle)
{
    if (obj.contains("angle")) {
        return TryReadNumber(obj.at("angle"), angle);
    }
    if (obj.contains("heading")) {
        return TryReadNumber(obj.at("heading"), angle);
    }
    if (obj.contains("yaw")) {
        return TryReadNumber(obj.at("yaw"), angle);
    }
    return false;
}

bool AppendArrayWaypoint(const json::array& p, std::vector<Waypoint>& out_waypoints, std::string& zone_context)
{
    if (p.size() < 2) {
        return false;
    }

    double tx = 0.0;
    double ty = 0.0;
    if (!TryReadNumber(p[0], tx) || !TryReadNumber(p[1], ty)) {
        return false;
    }

    std::vector<ActionType> actions;
    std::string zone_id = zone_context;
    bool strict_arrival = false;
    for (size_t i = 2; i < p.size(); ++i) {
        if (TryReadBool(p[i], strict_arrival)) {
            continue;
        }
        if (TryAppendActionList(p[i], actions)) {
            continue;
        }
        if (!p[i].is_string()) {
            return false;
        }

        const std::string extra_text = p[i].as_string();
        if (LooksLikeActionToken(extra_text) || IsActionKeywordCaseInsensitive(extra_text)) {
            return false;
        }
        zone_id = extra_text;
    }

    AppendExpandedWaypoints(tx, ty, actions, zone_id, strict_arrival, out_waypoints);
    if (!zone_id.empty()) {
        zone_context = zone_id;
    }
    return true;
}

bool AppendObjectWaypoint(const json::object& obj, std::vector<Waypoint>& out_waypoints, std::string& zone_context)
{
    std::vector<ActionType> actions;
    if (obj.contains("action")) {
        const auto& act = obj.at("action");
        if (act.is_array()) {
            if (!TryAppendActionList(act, actions)) {
                return false;
            }
        }
        else if (act.is_string() && LooksLikeActionToken(act.as_string())) {
            ActionType parsed_action = ActionType::RUN;
            if (!TryParseActionType(act, parsed_action)) {
                return false;
            }
            actions.push_back(parsed_action);
        }
        else {
            ActionType parsed_action = ActionType::RUN;
            if (!TryParseActionType(act, parsed_action)) {
                return false;
            }
            actions.push_back(parsed_action);
        }
    }
    if (obj.contains("actions")) {
        if (!TryAppendActionList(obj.at("actions"), actions)) {
            return false;
        }
    }

    std::string zone_id = zone_context;
    TryReadZoneId(obj, zone_id);

    const ActionType primary_action = actions.empty() ? ActionType::RUN : actions.front();

    if (primary_action == ActionType::ZONE) {
        if (zone_id.empty()) {
            return false;
        }
        out_waypoints.push_back(Waypoint::Zone(zone_id));
        zone_context = zone_id;
        return true;
    }

    double angle = 0.0;
    const bool has_angle = TryReadAngleValue(obj, angle);

    if (primary_action == ActionType::HEADING) {
        if (!has_angle) {
            return false;
        }
        Waypoint heading_waypoint = Waypoint::Heading(angle);
        heading_waypoint.zone_id = zone_id;
        out_waypoints.push_back(std::move(heading_waypoint));
        return true;
    }

    if (obj.contains("x") && obj.contains("y")) {
        double tx = 0.0;
        double ty = 0.0;
        if (!TryReadNumber(obj.at("x"), tx) || !TryReadNumber(obj.at("y"), ty)) {
            return false;
        }

        Waypoint strict_probe(tx, ty, primary_action);
        TryReadStrictArrival(obj, strict_probe);
        AppendExpandedWaypoints(tx, ty, actions, zone_id, strict_probe.strict_arrival, out_waypoints);
        if (!zone_id.empty()) {
            zone_context = zone_id;
        }
        return true;
    }

    if (has_angle) {
        Waypoint heading_waypoint = Waypoint::Heading(angle);
        heading_waypoint.zone_id = zone_id;
        out_waypoints.push_back(std::move(heading_waypoint));
        return true;
    }

    return false;
}

bool AppendParsedWaypoints(const json::value& point, std::vector<Waypoint>& out_waypoints, std::string& zone_context)
{
    if (point.is_array()) {
        return AppendArrayWaypoint(point.as_array(), out_waypoints, zone_context);
    }

    if (point.is_object()) {
        return AppendObjectWaypoint(point.as_object(), out_waypoints, zone_context);
    }

    return false;
}

} // namespace

MaaBool MAA_CALL MapNavigateActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    if (custom_action_param && std::strlen(custom_action_param) > 0) {
        LogInfo << "MapNavigateActionRun param string: " << custom_action_param;
        auto options_opt = json::parse(custom_action_param);
        if (!options_opt || !options_opt->is_object()) {
            LogError << "Failed to parse MapNavigateAction param (invalid JSON object)" << VAR(custom_action_param);
            return MAA_FALSE;
        }

        const auto& options = options_opt->as_object();
        NaviParam param;

        param.map_name = options.get("map_name", param.map_name);
        param.arrival_timeout = options.get("arrival_timeout", param.arrival_timeout);
        param.sprint_threshold = options.get("sprint_threshold", param.sprint_threshold);
        param.enable_local_driver = options.get("enable_local_driver", param.enable_local_driver);

        std::string zone_context = param.map_name;
        if (options.contains("path") && options.at("path").is_array()) {
            const auto& path = options.at("path").as_array();
            for (size_t index = 0; index < path.size(); ++index) {
                if (AppendParsedWaypoints(path[index], param.path, zone_context)) {
                    continue;
                }
                LogError << "Failed to parse MapNavigator waypoint in path array." << VAR(index);
                return MAA_FALSE;
            }
        }
        else {
            const bool looks_like_single_waypoint = options.contains("x") || options.contains("y") || options.contains("action")
                                                    || options.contains("actions") || options.contains("angle")
                                                    || options.contains("heading") || options.contains("yaw");
            if (looks_like_single_waypoint && !AppendParsedWaypoints(*options_opt, param.path, zone_context)) {
                LogError << "Failed to parse MapNavigator waypoint from custom_action_param object.";
                return MAA_FALSE;
            }
        }

        if (!param.path.empty()) {
            NaviController controller(context);
            if (!controller.Navigate(param)) {
                return MAA_FALSE;
            }
        }
    }

    return MAA_TRUE;
}

} // namespace mapnavigator
