#include "navi_param_parser.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <cstddef>
#include <cstring>
#include <optional>
#include <string>
#include <string_view>
#include <utility>
#include <variant>
#include <vector>

#include <meojson/json.hpp>

#include <MaaUtils/Logger.h>

#include "navi_config.h"
#include "prompt_scan_profile.h"

namespace mapnavigator
{

namespace
{

bool is_valid_action_type(ActionType action)
{
    switch (action) {
#define NAVI_X_(name)      \
    case ActionType::name: \
        return true;
        NAVI_ACTION_TYPES(NAVI_X_)
#undef NAVI_X_
    default:
        return false;
    }
}

bool looks_like_action_token(const std::string& text)
{
    if (text.empty()) {
        return false;
    }
    return std::all_of(text.begin(), text.end(), [](char ch) { return std::isupper(static_cast<unsigned char>(ch)) || ch == '_'; });
}

bool is_action_keyword_case_insensitive(const std::string& text)
{
    std::string normalized;
    normalized.reserve(text.size());
    for (char ch : text) {
        normalized.push_back(static_cast<char>(std::toupper(static_cast<unsigned char>(ch))));
    }

#define NAVI_X_(name)          \
    if (normalized == #name) { \
        return true;           \
    }
    NAVI_ACTION_TYPES(NAVI_X_)
#undef NAVI_X_
    return false;
}

struct NaviActionListInput
{
    std::vector<ActionType> actions_;

    bool check_json(const json::value& input) const
    {
        NaviActionListInput parsed;
        return parsed.from_json(input);
    }

    bool from_json(const json::value& input)
    {
        actions_.clear();

        if (input.is<ActionType>()) {
            const ActionType action = input.as<ActionType>();
            if (!is_valid_action_type(action)) {
                return false;
            }
            actions_.push_back(action);
            return true;
        }

        if (!input.is<std::vector<ActionType>>()) {
            return false;
        }

        actions_ = input.as<std::vector<ActionType>>();
        return std::all_of(actions_.begin(), actions_.end(), is_valid_action_type);
    }
};

// A string or an array of strings, or { "node": "SomeOcrNode" } to read the list off that node instead. Empty is
// rejected rather than ignored: empty text matches anything on the recognition side, which is exactly the
// press-on-any-prompt behaviour these fields exist to avoid.
struct NaviTextListInput
{
    std::vector<std::string> texts_;
    std::string node_;

    bool empty() const { return texts_.empty() && node_.empty(); }

    bool check_json(const json::value& input) const
    {
        NaviTextListInput parsed;
        return parsed.from_json(input);
    }

    bool from_json(const json::value& input)
    {
        texts_.clear();
        node_.clear();

        // 字符串和数组都被字面文字占着, 所以引用一个节点只剩对象这一种不含糊的写法。
        if (input.is_object()) {
            const json::object& object = input.as_object();
            if (!object.contains("node") || !object.at("node").is_string()) {
                return false;
            }
            node_ = object.at("node").as_string();
            return !node_.empty();
        }

        if (input.is_string()) {
            if (input.as_string().empty()) {
                return false;
            }
            texts_.push_back(input.as_string());
            return true;
        }

        if (!input.is<std::vector<std::string>>()) {
            return false;
        }

        texts_ = input.as<std::vector<std::string>>();
        return !texts_.empty() && std::all_of(texts_.begin(), texts_.end(), [](const std::string& text) { return !text.empty(); });
    }
};

using NaviWaypointArrayItem = std::variant<double, NaviActionListInput, bool, std::string>;

struct NaviWaypointObjectInput
{
    NaviActionListInput action_;
    NaviActionListInput actions_;
    std::string zone_id_;
    std::string zoneId_;
    std::string zone_;
    std::string map_name_;
    std::string mapName_;
    std::string target_tier_;
    std::string targetTier_;
    double target_deck_y_ = 0.0;
    double targetDeckY_ = 0.0;
    bool strict_ = false;
    bool strict_arrival_ = false;
    bool strictArrival_ = false;
    double x_ = 0.0;
    double y_ = 0.0;
    double angle_ = 0.0;
    double heading_ = 0.0;
    double yaw_ = 0.0;
    std::array<double, 2> target_ {};
    bool has_action_ = false;
    bool has_actions_ = false;
    bool has_zone_id_ = false;
    bool has_zoneId_ = false;
    bool has_zone_ = false;
    bool has_map_name_ = false;
    bool has_mapName_ = false;
    bool has_target_deck_y_ = false;
    bool has_targetDeckY_ = false;
    bool has_strict_ = false;
    bool has_strict_arrival_ = false;
    bool has_strictArrival_ = false;
    bool has_x_ = false;
    bool has_y_ = false;
    bool has_angle_ = false;
    bool has_heading_ = false;
    bool has_yaw_ = false;
    bool has_target_ = false;

    MEO_FROMJSON(
        MEO_OPT MEO_KEY("action") action_,
        MEO_OPT MEO_KEY("actions") actions_,
        MEO_OPT MEO_KEY("zone_id") zone_id_,
        MEO_OPT MEO_KEY("zoneId") zoneId_,
        MEO_OPT MEO_KEY("zone") zone_,
        MEO_OPT MEO_KEY("map_name") map_name_,
        MEO_OPT MEO_KEY("mapName") mapName_,
        MEO_OPT MEO_KEY("target_tier") target_tier_,
        MEO_OPT MEO_KEY("targetTier") targetTier_,
        MEO_OPT MEO_KEY("target_deck_y") target_deck_y_,
        MEO_OPT MEO_KEY("targetDeckY") targetDeckY_,
        MEO_OPT MEO_KEY("strict") strict_,
        MEO_OPT MEO_KEY("strict_arrival") strict_arrival_,
        MEO_OPT MEO_KEY("strictArrival") strictArrival_,
        MEO_OPT MEO_KEY("x") x_,
        MEO_OPT MEO_KEY("y") y_,
        MEO_OPT MEO_KEY("angle") angle_,
        MEO_OPT MEO_KEY("heading") heading_,
        MEO_OPT MEO_KEY("yaw") yaw_,
        MEO_OPT MEO_KEY("target") target_)

    bool hasWaypointInput() const
    {
        return has_action_ || has_actions_ || has_x_ || has_y_ || has_angle_ || has_heading_ || has_yaw_ || has_target_;
    }
};

struct NaviWaypointObjectPresenceInput
{
    std::optional<json::value> action_;
    std::optional<json::value> actions_;
    std::optional<json::value> zone_id_;
    std::optional<json::value> zoneId_;
    std::optional<json::value> zone_;
    std::optional<json::value> map_name_;
    std::optional<json::value> mapName_;
    std::optional<json::value> target_deck_y_;
    std::optional<json::value> targetDeckY_;
    std::optional<json::value> strict_;
    std::optional<json::value> strict_arrival_;
    std::optional<json::value> strictArrival_;
    std::optional<json::value> x_;
    std::optional<json::value> y_;
    std::optional<json::value> angle_;
    std::optional<json::value> heading_;
    std::optional<json::value> yaw_;
    std::optional<json::value> target_;

    MEO_FROMJSON(
        MEO_OPT MEO_KEY("action") action_,
        MEO_OPT MEO_KEY("actions") actions_,
        MEO_OPT MEO_KEY("zone_id") zone_id_,
        MEO_OPT MEO_KEY("zoneId") zoneId_,
        MEO_OPT MEO_KEY("zone") zone_,
        MEO_OPT MEO_KEY("map_name") map_name_,
        MEO_OPT MEO_KEY("mapName") mapName_,
        MEO_OPT MEO_KEY("target_deck_y") target_deck_y_,
        MEO_OPT MEO_KEY("targetDeckY") targetDeckY_,
        MEO_OPT MEO_KEY("strict") strict_,
        MEO_OPT MEO_KEY("strict_arrival") strict_arrival_,
        MEO_OPT MEO_KEY("strictArrival") strictArrival_,
        MEO_OPT MEO_KEY("x") x_,
        MEO_OPT MEO_KEY("y") y_,
        MEO_OPT MEO_KEY("angle") angle_,
        MEO_OPT MEO_KEY("heading") heading_,
        MEO_OPT MEO_KEY("yaw") yaw_,
        MEO_OPT MEO_KEY("target") target_)
};

// Read from a point and from the route root alike, as its own pass over the same object: MEO_OPT MEO_KEY spends three
// macro arguments per key and the macro tops out at 64, so a single block holds at most 21 keys.
struct NaviInteractInput
{
    NaviTextListInput interact_text_;
    NaviTextListInput interactText_;
    std::string interact_scan_;
    std::string interactScan_;

    MEO_FROMJSON(
        MEO_OPT MEO_KEY("interact_text") interact_text_,
        MEO_OPT MEO_KEY("interactText") interactText_,
        MEO_OPT MEO_KEY("interact_scan") interact_scan_,
        MEO_OPT MEO_KEY("interactScan") interactScan_)

    const NaviTextListInput& text() const { return interact_text_.empty() ? interactText_ : interact_text_; }

    const std::vector<std::string>& texts() const { return text().texts_; }

    const std::string& textNode() const { return text().node_; }

    const std::string& scan() const { return interact_scan_.empty() ? interactScan_ : interact_scan_; }
};

struct NaviInteractSpec
{
    std::vector<std::string> texts;
    std::string text_node;
    std::string scan;
};

bool read_interact_spec(const json::value& input, NaviInteractSpec& out_spec)
{
    NaviInteractInput flat;
    if (!flat.from_json(input)) {
        return false;
    }

    out_spec.texts = flat.texts();
    out_spec.text_node = flat.textNode();
    out_spec.scan = flat.scan();
    return true;
}

struct NaviWaypointInput
{
    double x_ = 0.0;
    double y_ = 0.0;
    std::vector<ActionType> actions_;
    std::string zone_id_;
    std::string target_tier_;
    std::vector<std::string> interact_text_;
    std::string interact_text_node_;
    std::string interact_scan_;
    std::optional<double> target_deck_y_;
    bool strict_arrival_ = false;
    double angle_ = 0.0;
    std::array<double, 2> target_ {};
    bool has_x_ = false;
    bool has_y_ = false;
    bool has_angle_ = false;
    bool has_target_ = false;

    bool check_json(const json::value& input) const
    {
        NaviWaypointInput parsed;
        return parsed.from_json(input);
    }

    bool from_json(const json::value& input)
    {
        *this = NaviWaypointInput {};

        if (input.is_array()) {
            if (!input.is<std::vector<NaviWaypointArrayItem>>()) {
                return false;
            }
            return fromArray(input.as<std::vector<NaviWaypointArrayItem>>());
        }

        if (!input.is_object()) {
            return false;
        }

        NaviWaypointObjectInput object_input;
        if (!object_input.from_json(input)) {
            return false;
        }

        NaviWaypointObjectPresenceInput presence;
        if (!presence.from_json(input)) {
            return false;
        }
        applyPresence(presence, object_input);

        NaviInteractSpec interact_spec;
        if (!read_interact_spec(input, interact_spec)) {
            return false;
        }

        fromObject(object_input, interact_spec);
        return true;
    }

private:
    bool fromArray(const std::vector<NaviWaypointArrayItem>& items)
    {
        if (items.size() < 2 || !std::holds_alternative<double>(items[0]) || !std::holds_alternative<double>(items[1])) {
            return false;
        }

        x_ = std::get<double>(items[0]);
        y_ = std::get<double>(items[1]);
        has_x_ = true;
        has_y_ = true;

        for (size_t index = 2; index < items.size(); ++index) {
            if (const auto* strict_arrival = std::get_if<bool>(&items[index])) {
                strict_arrival_ = *strict_arrival;
                continue;
            }

            if (const auto* action_input = std::get_if<NaviActionListInput>(&items[index])) {
                actions_.insert(actions_.end(), action_input->actions_.begin(), action_input->actions_.end());
                continue;
            }

            if (const auto* zone_id = std::get_if<std::string>(&items[index])) {
                if (looks_like_action_token(*zone_id) || is_action_keyword_case_insensitive(*zone_id)) {
                    return false;
                }
                zone_id_ = *zone_id;
                continue;
            }

            return false;
        }

        return true;
    }

    void fromObject(const NaviWaypointObjectInput& object_input, const NaviInteractSpec& interact_spec)
    {
        appendActions(object_input.action_);
        appendActions(object_input.actions_);

        zone_id_ = resolveZoneId(object_input);
        target_tier_ = resolveTargetTier(object_input);
        interact_text_ = interact_spec.texts;
        interact_text_node_ = interact_spec.text_node;
        interact_scan_ = interact_spec.scan;
        target_deck_y_ = resolveTargetDeckY(object_input);
        strict_arrival_ = resolveStrictArrival(object_input);
        x_ = object_input.x_;
        y_ = object_input.y_;
        angle_ = resolveAngle(object_input);
        target_ = object_input.target_;
        has_x_ = object_input.has_x_;
        has_y_ = object_input.has_y_;
        has_angle_ = object_input.has_angle_ || object_input.has_heading_ || object_input.has_yaw_;
        has_target_ = object_input.has_target_;
    }

    static void applyPresence(const NaviWaypointObjectPresenceInput& presence, NaviWaypointObjectInput& object_input)
    {
        object_input.has_action_ = presence.action_.has_value();
        object_input.has_actions_ = presence.actions_.has_value();
        object_input.has_zone_id_ = presence.zone_id_.has_value();
        object_input.has_zoneId_ = presence.zoneId_.has_value();
        object_input.has_zone_ = presence.zone_.has_value();
        object_input.has_map_name_ = presence.map_name_.has_value();
        object_input.has_mapName_ = presence.mapName_.has_value();
        object_input.has_target_deck_y_ = presence.target_deck_y_.has_value();
        object_input.has_targetDeckY_ = presence.targetDeckY_.has_value();
        object_input.has_strict_ = presence.strict_.has_value();
        object_input.has_strict_arrival_ = presence.strict_arrival_.has_value();
        object_input.has_strictArrival_ = presence.strictArrival_.has_value();
        object_input.has_x_ = presence.x_.has_value();
        object_input.has_y_ = presence.y_.has_value();
        object_input.has_angle_ = presence.angle_.has_value();
        object_input.has_heading_ = presence.heading_.has_value();
        object_input.has_yaw_ = presence.yaw_.has_value();
        object_input.has_target_ = presence.target_.has_value();
    }

    void appendActions(const NaviActionListInput& action_input)
    {
        actions_.insert(actions_.end(), action_input.actions_.begin(), action_input.actions_.end());
    }

    static std::string firstNonEmpty(
        const std::string& first,
        const std::string& second,
        const std::string& third,
        const std::string& fourth,
        const std::string& fifth)
    {
        for (const auto* value : { &first, &second, &third, &fourth, &fifth }) {
            if (!value->empty()) {
                return *value;
            }
        }
        return {};
    }

    static std::string resolveZoneId(const NaviWaypointObjectInput& input)
    {
        return firstNonEmpty(input.zone_id_, input.zoneId_, input.zone_, input.map_name_, input.mapName_);
    }

    static std::string resolveTargetTier(const NaviWaypointObjectInput& input)
    {
        for (const std::string* value : { &input.target_tier_, &input.targetTier_ }) {
            if (!value->empty()) {
                return *value;
            }
        }
        return {};
    }

    static std::optional<double> resolveTargetDeckY(const NaviWaypointObjectInput& input)
    {
        if (input.has_target_deck_y_) {
            return input.target_deck_y_;
        }
        if (input.has_targetDeckY_) {
            return input.targetDeckY_;
        }
        return std::nullopt;
    }

    static bool resolveStrictArrival(const NaviWaypointObjectInput& input)
    {
        if (input.has_strict_) {
            return input.strict_;
        }
        if (input.has_strict_arrival_) {
            return input.strict_arrival_;
        }
        return input.strictArrival_;
    }

    static double resolveAngle(const NaviWaypointObjectInput& input)
    {
        if (input.has_angle_) {
            return input.angle_;
        }
        if (input.has_heading_) {
            return input.heading_;
        }
        return input.yaw_;
    }
};

struct NaviParamInput
{
    std::string map_name_;
    std::vector<NaviWaypointInput> path_;
    int64_t arrival_timeout_ = 60000;
    double sprint_threshold_ = 16.0;
    bool enable_local_driver_ = true;
    std::string navmesh_file_;
    std::string nav_file_;
    double navmesh_snap_radius_ = 5.0;
    double snap_radius_ = 5.0;
    NaviActionListInput action_;
    NaviActionListInput actions_;
    double x_ = 0.0;
    double y_ = 0.0;
    double angle_ = 0.0;
    double heading_ = 0.0;
    double yaw_ = 0.0;
    std::array<double, 2> target_ {};
    bool has_path_ = false;
    bool has_navmesh_file_ = false;
    bool has_nav_file_ = false;
    bool has_navmesh_snap_radius_ = false;
    bool has_snap_radius_ = false;
    bool has_action_ = false;
    bool has_actions_ = false;
    bool has_x_ = false;
    bool has_y_ = false;
    bool has_angle_ = false;
    bool has_heading_ = false;
    bool has_yaw_ = false;
    bool has_target_ = false;

    MEO_FROMJSON(
        MEO_OPT MEO_KEY("map_name") map_name_,
        MEO_OPT MEO_KEY("path") path_,
        MEO_OPT MEO_KEY("arrival_timeout") arrival_timeout_,
        MEO_OPT MEO_KEY("sprint_threshold") sprint_threshold_,
        MEO_OPT MEO_KEY("enable_local_driver") enable_local_driver_,
        MEO_OPT MEO_KEY("navmesh_file") navmesh_file_,
        MEO_OPT MEO_KEY("nav_file") nav_file_,
        MEO_OPT MEO_KEY("navmesh_snap_radius") navmesh_snap_radius_,
        MEO_OPT MEO_KEY("snap_radius") snap_radius_,
        MEO_OPT MEO_KEY("action") action_,
        MEO_OPT MEO_KEY("actions") actions_,
        MEO_OPT MEO_KEY("x") x_,
        MEO_OPT MEO_KEY("y") y_,
        MEO_OPT MEO_KEY("angle") angle_,
        MEO_OPT MEO_KEY("heading") heading_,
        MEO_OPT MEO_KEY("yaw") yaw_,
        MEO_OPT MEO_KEY("target") target_)

    bool hasSingleWaypointInput() const
    {
        return has_action_ || has_actions_ || has_x_ || has_y_ || has_angle_ || has_heading_ || has_yaw_ || has_target_;
    }
};

struct NaviParamPresenceInput
{
    std::optional<json::value> path_;
    std::optional<json::value> navmesh_file_;
    std::optional<json::value> nav_file_;
    std::optional<json::value> navmesh_snap_radius_;
    std::optional<json::value> snap_radius_;
    std::optional<json::value> action_;
    std::optional<json::value> actions_;
    std::optional<json::value> x_;
    std::optional<json::value> y_;
    std::optional<json::value> angle_;
    std::optional<json::value> heading_;
    std::optional<json::value> yaw_;
    std::optional<json::value> target_;

    MEO_FROMJSON(
        MEO_OPT MEO_KEY("path") path_,
        MEO_OPT MEO_KEY("navmesh_file") navmesh_file_,
        MEO_OPT MEO_KEY("nav_file") nav_file_,
        MEO_OPT MEO_KEY("navmesh_snap_radius") navmesh_snap_radius_,
        MEO_OPT MEO_KEY("snap_radius") snap_radius_,
        MEO_OPT MEO_KEY("action") action_,
        MEO_OPT MEO_KEY("actions") actions_,
        MEO_OPT MEO_KEY("x") x_,
        MEO_OPT MEO_KEY("y") y_,
        MEO_OPT MEO_KEY("angle") angle_,
        MEO_OPT MEO_KEY("heading") heading_,
        MEO_OPT MEO_KEY("yaw") yaw_,
        MEO_OPT MEO_KEY("target") target_)
};

bool apply_navi_param_presence(const json::value& input, NaviParamInput& param)
{
    NaviParamPresenceInput presence;
    if (!presence.from_json(input)) {
        return false;
    }

    param.has_path_ = presence.path_.has_value();
    param.has_navmesh_file_ = presence.navmesh_file_.has_value();
    param.has_nav_file_ = presence.nav_file_.has_value();
    param.has_navmesh_snap_radius_ = presence.navmesh_snap_radius_.has_value();
    param.has_snap_radius_ = presence.snap_radius_.has_value();
    param.has_action_ = presence.action_.has_value();
    param.has_actions_ = presence.actions_.has_value();
    param.has_x_ = presence.x_.has_value();
    param.has_y_ = presence.y_.has_value();
    param.has_angle_ = presence.angle_.has_value();
    param.has_heading_ = presence.heading_.has_value();
    param.has_yaw_ = presence.yaw_.has_value();
    param.has_target_ = presence.target_.has_value();
    return true;
}

NaviParam build_navi_param(const NaviParamInput& input)
{
    NaviParam param;
    param.map_name = input.map_name_;
    param.arrival_timeout = input.arrival_timeout_;
    param.sprint_threshold = input.sprint_threshold_;
    param.enable_local_driver = input.enable_local_driver_;

    if (input.has_navmesh_file_) {
        param.navmesh_file = input.navmesh_file_;
    }
    if (input.has_nav_file_) {
        param.navmesh_file = input.nav_file_;
    }
    if (input.has_navmesh_snap_radius_) {
        param.navmesh_snap_radius = input.navmesh_snap_radius_;
    }
    if (input.has_snap_radius_) {
        param.navmesh_snap_radius = input.snap_radius_;
    }

    return param;
}

void append_expanded_waypoints(
    double tx,
    double ty,
    const std::vector<ActionType>& actions,
    const std::string& zone_id,
    const std::string& target_tier,
    bool strict_arrival,
    std::vector<Waypoint>& out_waypoints)
{
    const bool has_non_run_action =
        std::any_of(actions.begin(), actions.end(), [](ActionType action) { return action != ActionType::RUN; });
    if (actions.empty()) {
        Waypoint waypoint(tx, ty, ActionType::RUN);
        waypoint.zone_id = zone_id;
        waypoint.target_tier = target_tier;
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
        waypoint.target_tier = target_tier;
        waypoint.strict_arrival = strict_arrival;
        out_waypoints.push_back(std::move(waypoint));
    }
}

// Fill in the interact text on the INTERACT points of [from_index, end); empty leaves them plain INTERACT. Written
// out or named by node are the same field, so a point that carries either one is already spoken for.
void apply_interact_text(
    const std::vector<std::string>& interact_text,
    const std::string& interact_text_node,
    std::vector<Waypoint>& waypoints,
    size_t from_index)
{
    if (interact_text.empty() && interact_text_node.empty()) {
        return;
    }
    for (size_t index = from_index; index < waypoints.size(); ++index) {
        Waypoint& waypoint = waypoints[index];
        if (waypoint.action != ActionType::INTERACT || !waypoint.interact_text.empty() || !waypoint.interact_text_node.empty()) {
            continue;
        }
        waypoint.interact_text = interact_text;
        waypoint.interact_text_node = interact_text_node;
    }
}

// Same, for the node the walking pre-filter reads its roi/template/threshold from.
void apply_interact_scan(const std::string& interact_scan, std::vector<Waypoint>& waypoints, size_t from_index)
{
    if (interact_scan.empty()) {
        return;
    }
    for (size_t index = from_index; index < waypoints.size(); ++index) {
        if (waypoints[index].action == ActionType::INTERACT && waypoints[index].interact_scan.empty()) {
            waypoints[index].interact_scan = interact_scan;
        }
    }
}

// Runs after the route-wide defaults land, so a point holding only the scan node is not flagged. The pre-filter
// only decides when to stop; with no text there is nothing to confirm, so the point falls back to plain INTERACT.
void warn_scan_without_text(const std::vector<Waypoint>& waypoints)
{
    for (size_t index = 0; index < waypoints.size(); ++index) {
        const Waypoint& waypoint = waypoints[index];
        if (!waypoint.interact_scan.empty() && waypoint.interact_text.empty() && waypoint.interact_text_node.empty()) {
            LogWarn << "Waypoint names a prompt scan node without any interact text; it stays a plain INTERACT." << VAR(index)
                    << VAR(waypoint.interact_scan);
        }
    }
}

std::string resolve_waypoint_zone_id(const NaviWaypointInput& input, const std::string& zone_context)
{
    return input.zone_id_.empty() ? zone_context : input.zone_id_;
}

// Only INTERACT points read these fields; elsewhere they are dropped. Not worth rejecting the route over, but the
// missing piece is the action rather than anything the author can see in the interact fields themselves.
void warn_unusable_interact_fields(const NaviWaypointInput& input)
{
    if (input.interact_text_.empty() && input.interact_text_node_.empty() && input.interact_scan_.empty()) {
        return;
    }
    if (std::find(input.actions_.begin(), input.actions_.end(), ActionType::INTERACT) != input.actions_.end()) {
        return;
    }
    LogWarn << "Waypoint carries interact fields without an INTERACT action; they do nothing here." << VAR(input.interact_text_.size())
            << VAR(input.interact_text_node_) << VAR(input.interact_scan_);
}

bool append_parsed_waypoint(const NaviWaypointInput& input, std::vector<Waypoint>& out_waypoints, std::string& zone_context)
{
    warn_unusable_interact_fields(input);
    const std::string zone_id = resolve_waypoint_zone_id(input, zone_context);
    const ActionType primary_action = input.actions_.empty() ? ActionType::RUN : input.actions_.front();

    if (primary_action == ActionType::ZONE) {
        if (zone_id.empty()) {
            return false;
        }
        out_waypoints.push_back(Waypoint::Zone(zone_id));
        zone_context = zone_id;
        return true;
    }

    if (primary_action == ActionType::HEADING) {
        if (input.has_target_) {
            Waypoint heading_waypoint = Waypoint::HeadingToTarget(input.target_.at(0), input.target_.at(1));
            heading_waypoint.zone_id = zone_id;
            heading_waypoint.target_tier = input.target_tier_;
            out_waypoints.push_back(std::move(heading_waypoint));
            return true;
        }
        if (!input.has_angle_) {
            return false;
        }
        Waypoint heading_waypoint = Waypoint::Heading(input.angle_);
        heading_waypoint.zone_id = zone_id;
        out_waypoints.push_back(std::move(heading_waypoint));
        return true;
    }

    if (primary_action == ActionType::NAVMESH) {
        if (!input.has_target_) {
            return false;
        }
        Waypoint navmesh_waypoint(input.target_.at(0), input.target_.at(1), ActionType::NAVMESH);
        navmesh_waypoint.strict_arrival = true;
        navmesh_waypoint.zone_id = zone_id;
        // The tier whose coordinate frame `target` was authored in. Distinct from zone_id, which is the
        // start/floor context and is inheritable from a preceding ZONE declaration — reusing it would
        // double-convert legacy base-pixel targets. Empty -> target stays base-pixel (legacy), see expander.
        navmesh_waypoint.target_tier = input.target_tier_;
        // 该坐标压着好几张可走面时, 声明落在哪一张的高度; 不填保持原样
        navmesh_waypoint.target_deck_y = input.target_deck_y_;
        out_waypoints.push_back(std::move(navmesh_waypoint));
        return true;
    }

    const bool has_legacy_position = input.has_x_ && input.has_y_;
    // No target_tier -> the target stays base-pixel (legacy), same as the bare-array form; see expander.
    if (has_legacy_position || input.has_target_) {
        const double target_x = input.has_target_ ? input.target_.at(0) : input.x_;
        const double target_y = input.has_target_ ? input.target_.at(1) : input.y_;
        const size_t first_expanded = out_waypoints.size();
        append_expanded_waypoints(target_x, target_y, input.actions_, zone_id, input.target_tier_, input.strict_arrival_, out_waypoints);
        // One coordinate may carry several actions and expand into several waypoints; only the INTERACT ones take these.
        apply_interact_text(input.interact_text_, input.interact_text_node_, out_waypoints, first_expanded);
        apply_interact_scan(input.interact_scan_, out_waypoints, first_expanded);
        if (!zone_id.empty()) {
            zone_context = zone_id;
        }
        return true;
    }

    if (input.has_angle_) {
        Waypoint heading_waypoint = Waypoint::Heading(input.angle_);
        heading_waypoint.zone_id = zone_id;
        out_waypoints.push_back(std::move(heading_waypoint));
        return true;
    }

    return false;
}

bool append_parsed_waypoints(
    const std::vector<NaviWaypointInput>& inputs,
    std::vector<Waypoint>& out_waypoints,
    std::string& zone_context,
    std::string_view caller_name)
{
    for (size_t index = 0; index < inputs.size(); ++index) {
        if (append_parsed_waypoint(inputs[index], out_waypoints, zone_context)) {
            continue;
        }
        LogError << "Failed to parse " << std::string(caller_name) << " waypoint in path array." << VAR(index);
        return false;
    }
    return true;
}

} // namespace

bool TryParseNaviParam(const json::value& custom_action_param, NaviParam& out_param, std::string_view caller_name)
{
    const std::string caller_name_text(caller_name);
    NaviParamInput input;
    std::string error_key;
    if (!input.from_json(custom_action_param, error_key)) {
        LogError << "Failed to deserialize " << caller_name_text << " param." << VAR(error_key) << VAR(custom_action_param);
        return false;
    }
    if (!apply_navi_param_presence(custom_action_param, input)) {
        LogError << "Failed to deserialize " << caller_name_text << " param presence." << VAR(custom_action_param);
        return false;
    }

    // Route-wide default, written once at the top level; a point's own value wins.
    NaviInteractSpec route_interact;
    if (!read_interact_spec(custom_action_param, route_interact)) {
        LogError << "Failed to deserialize " << caller_name_text << " interact defaults." << VAR(custom_action_param);
        return false;
    }

    NaviParam param = build_navi_param(input);
    std::string zone_context = param.map_name;

    if (input.has_path_) {
        if (!append_parsed_waypoints(input.path_, param.path, zone_context, caller_name_text)) {
            return false;
        }
    }
    else if (input.hasSingleWaypointInput()) {
        NaviWaypointInput waypoint;
        if (!waypoint.from_json(custom_action_param) || !append_parsed_waypoint(waypoint, param.path, zone_context)) {
            LogError << "Failed to parse " << caller_name_text << " waypoint from custom_action_param object.";
            return false;
        }
    }

    apply_interact_text(route_interact.texts, route_interact.text_node, param.path, 0);
    apply_interact_scan(route_interact.scan, param.path, 0);
    warn_scan_without_text(param.path);

    out_param = std::move(param);
    return true;
}

bool TryParseNaviParam(const char* custom_action_param, NaviParam& out_param, std::string_view caller_name)
{
    if (custom_action_param == nullptr || std::strlen(custom_action_param) == 0) {
        out_param = NaviParam {};
        return true;
    }

    auto options_opt = json::parse(custom_action_param);
    if (!options_opt) {
        LogError << "Failed to parse " << std::string(caller_name) << " param (invalid JSON)" << VAR(custom_action_param);
        return false;
    }

    return TryParseNaviParam(*options_opt, out_param, caller_name);
}

void ResolveInteractTextNodes(MaaContext* context, NaviParam& param)
{
    for (Waypoint& waypoint : param.path) {
        if (waypoint.interact_text_node.empty() || !waypoint.interact_text.empty()) {
            continue;
        }
        std::vector<std::string> texts;
        // 读不到就留空: 错误行由被调用方写, 该点退回原语义, 而不是拖垮整条路线。
        if (!TryReadNodeInteractTexts(context, waypoint.interact_text_node, &texts)) {
            continue;
        }
        waypoint.interact_text = std::move(texts);
        LogInfo << "Interact text resolved from node." << VAR(waypoint.interact_text_node) << VAR(waypoint.interact_text.size());
    }
}

} // namespace mapnavigator
