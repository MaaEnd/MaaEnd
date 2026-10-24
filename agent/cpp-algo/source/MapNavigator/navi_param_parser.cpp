#include "navi_param_parser.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <cstddef>
#include <cstring>
#include <initializer_list>
#include <optional>
#include <string>
#include <string_view>
#include <utility>
#include <variant>
#include <vector>

#include <meojson/json.hpp>

#include <MaaUtils/Logger.h>

#include "navi_config.h"
#include "navi_log.h"
#include "sensing/pipeline_node_reader.h"

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

bool has_key(const json::value& input, const char* key)
{
    return input.find_value(key) != nullptr;
}

bool has_any_key(const json::value& input, std::initializer_list<const char*> keys)
{
    return std::any_of(keys.begin(), keys.end(), [&input](const char* key) { return has_key(input, key); });
}

// 都为空时返回最后一个
template <typename T, typename... Rest>
const T& first_non_empty(const T& first, const Rest&... rest)
{
    if constexpr (sizeof...(rest) == 0) {
        return first;
    }
    else {
        return first.empty() ? first_non_empty(rest...) : first;
    }
}

// 参数按 (键, 字段) 成对给出, 取第一个写了键的字段, 调用前须已过 MEO 解析以排除 null
template <typename T, typename... Rest>
std::optional<T> first_present(const json::value& input, const char* key, const T& value, const Rest&... rest)
{
    if (has_key(input, key)) {
        return value;
    }
    if constexpr (sizeof...(rest) == 0) {
        return std::nullopt;
    }
    else {
        return first_present(input, rest...);
    }
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

// 接受字符串、字符串数组或 { "node": ... }, 空文本会匹配任何提示, 一律拒绝
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

// 每个别名都要通过类型校验, 取哪个由调用方按优先级决定
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
    bool required_ = false;
    double x_ = 0.0;
    double y_ = 0.0;
    double angle_ = 0.0;
    double heading_ = 0.0;
    double yaw_ = 0.0;
    std::array<double, 2> target_ {};

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
        MEO_OPT MEO_KEY("required") required_,
        MEO_OPT MEO_KEY("x") x_,
        MEO_OPT MEO_KEY("y") y_,
        MEO_OPT MEO_KEY("angle") angle_,
        MEO_OPT MEO_KEY("heading") heading_,
        MEO_OPT MEO_KEY("yaw") yaw_,
        MEO_OPT MEO_KEY("target") target_)
};

// 单独一趟解析, MEO_OPT MEO_KEY 每个键占 3 个宏参数, 一块最多 21 个键
struct NaviInteractInput
{
    NaviTextListInput interact_text_;
    NaviTextListInput interactText_;
    std::string interact_scan_;
    std::string interactScan_;
    bool interact_rec_ = false;
    bool interactRec_ = false;

    MEO_FROMJSON(
        MEO_OPT MEO_KEY("interact_text") interact_text_,
        MEO_OPT MEO_KEY("interactText") interactText_,
        MEO_OPT MEO_KEY("interact_scan") interact_scan_,
        MEO_OPT MEO_KEY("interactScan") interactScan_,
        MEO_OPT MEO_KEY("interact_rec") interact_rec_,
        MEO_OPT MEO_KEY("interactRec") interactRec_)
};

struct NaviInteractSpec
{
    std::vector<std::string> texts;
    std::string text_node;
    std::string scan;
    bool rec = false;
};

bool read_interact_spec(const json::value& input, NaviInteractSpec& out_spec)
{
    NaviInteractInput flat;
    if (!flat.from_json(input)) {
        return false;
    }

    const NaviTextListInput& text = first_non_empty(flat.interact_text_, flat.interactText_);
    out_spec.texts = text.texts_;
    out_spec.text_node = text.node_;
    out_spec.scan = first_non_empty(flat.interact_scan_, flat.interactScan_);
    out_spec.rec = flat.interact_rec_ || flat.interactRec_;
    return true;
}

// 点上与路线顶层共用这份解析, 点上优先
struct NaviFindInput
{
    std::string find_target_;
    std::string findTarget_;
    NaviTextListInput find_text_;
    NaviTextListInput findText_;
    std::string find_stop_;
    std::string findStop_;

    MEO_FROMJSON(
        MEO_OPT MEO_KEY("find_target") find_target_,
        MEO_OPT MEO_KEY("findTarget") findTarget_,
        MEO_OPT MEO_KEY("find_text") find_text_,
        MEO_OPT MEO_KEY("findText") findText_,
        MEO_OPT MEO_KEY("find_stop") find_stop_,
        MEO_OPT MEO_KEY("findStop") findStop_)
};

struct NaviFindSpec
{
    // find_text 写 { "node": ... } 时与 find_target 同义
    std::string target_node;
    std::vector<std::string> texts;
    // 命中即算到位, 与 arrive 至少给一个
    std::string stop_node;
    std::optional<std::array<double, 2>> arrive;
    // find_target 与内联文字同时给出
    bool conflicting_source = false;

    bool hasTargetSource() const { return !target_node.empty() || !texts.empty(); }
};

// find_arrive 只认 [x, y] 数字对, 形状不对是硬错误
bool read_arrive_point(const json::value& input, std::optional<std::array<double, 2>>& out_point)
{
    if (!input.is<std::array<double, 2>>()) {
        return false;
    }
    out_point = input.as<std::array<double, 2>>();
    return true;
}

bool read_find_spec(const json::value& input, NaviFindSpec& out_spec)
{
    NaviFindInput flat;
    if (!flat.from_json(input)) {
        return false;
    }

    const std::string& target = first_non_empty(flat.find_target_, flat.findTarget_);
    const NaviTextListInput& text = first_non_empty(flat.find_text_, flat.findText_);
    out_spec.target_node = target.empty() ? text.node_ : target;
    out_spec.texts = text.texts_;
    out_spec.stop_node = first_non_empty(flat.find_stop_, flat.findStop_);
    out_spec.conflicting_source = !target.empty() && (!text.texts_.empty() || !text.node_.empty());

    const json::value* arrive = input.find_value("find_arrive");
    if (arrive == nullptr) {
        arrive = input.find_value("findArrive");
    }
    return arrive == nullptr || read_arrive_point(*arrive, out_spec.arrive);
}

struct NaviTriggerInput
{
    std::string trigger_node_;
    std::string triggerNode_;

    MEO_FROMJSON(MEO_OPT MEO_KEY("trigger_node") trigger_node_, MEO_OPT MEO_KEY("triggerNode") triggerNode_)
};

struct NaviWaypointInput
{
    std::vector<ActionType> actions_;
    std::string zone_id_;
    std::string target_tier_;
    std::optional<double> x_;
    std::optional<double> y_;
    std::optional<double> angle_;
    std::optional<std::array<double, 2>> target_;
    std::optional<double> target_deck_y_;
    bool strict_arrival_ = false;
    bool required_ = false;
    NaviInteractSpec interact_;
    NaviFindSpec find_;
    std::string trigger_node_;

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

        NaviWaypointObjectInput object;
        NaviTriggerInput trigger;
        if (!object.from_json(input) || !read_interact_spec(input, interact_) || !read_find_spec(input, find_)
            || !trigger.from_json(input)) {
            return false;
        }
        fromObject(input, object, trigger);
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

        for (size_t index = 2; index < items.size(); ++index) {
            if (const auto* strict_arrival = std::get_if<bool>(&items[index])) {
                strict_arrival_ = *strict_arrival;
                continue;
            }

            if (const auto* action_input = std::get_if<NaviActionListInput>(&items[index])) {
                appendActions(*action_input);
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

    void fromObject(const json::value& input, const NaviWaypointObjectInput& object, const NaviTriggerInput& trigger)
    {
        appendActions(object.action_);
        appendActions(object.actions_);

        zone_id_ = first_non_empty(object.zone_id_, object.zoneId_, object.zone_, object.map_name_, object.mapName_);
        target_tier_ = first_non_empty(object.target_tier_, object.targetTier_);
        x_ = first_present(input, "x", object.x_);
        y_ = first_present(input, "y", object.y_);
        angle_ = first_present(input, "angle", object.angle_, "heading", object.heading_, "yaw", object.yaw_);
        target_ = first_present(input, "target", object.target_);
        target_deck_y_ = first_present(input, "target_deck_y", object.target_deck_y_, "targetDeckY", object.targetDeckY_);
        strict_arrival_ =
            first_present(input, "strict", object.strict_, "strict_arrival", object.strict_arrival_, "strictArrival", object.strictArrival_)
                .value_or(false);
        required_ = object.required_;
        trigger_node_ = first_non_empty(trigger.trigger_node_, trigger.triggerNode_);
    }

    void appendActions(const NaviActionListInput& action_input)
    {
        actions_.insert(actions_.end(), action_input.actions_.begin(), action_input.actions_.end());
    }
};

// 单点写法的键在这里只做类型校验, 由 NaviWaypointInput 另行解析
struct NaviParamInput
{
    std::string map_name_;
    std::string heading_source_ = "character";
    std::vector<NaviWaypointInput> path_;
    int64_t arrival_timeout_ = 60000;
    double sprint_threshold_ = 16.0;
    bool enable_local_driver_ = true;
    bool enable_bootstrap_navmesh_ = true;
    std::string navmesh_file_;
    std::string nav_file_;
    double navmesh_snap_radius_ = 5.0;
    double snap_radius_ = 5.0;
    bool zip_ = false;
    NaviActionListInput action_;
    NaviActionListInput actions_;
    double x_ = 0.0;
    double y_ = 0.0;
    double angle_ = 0.0;
    double heading_ = 0.0;
    double yaw_ = 0.0;
    std::array<double, 2> target_ {};

    MEO_FROMJSON(
        MEO_OPT MEO_KEY("map_name") map_name_,
        MEO_OPT MEO_KEY("path") path_,
        MEO_OPT MEO_KEY("arrival_timeout") arrival_timeout_,
        MEO_OPT MEO_KEY("heading_source") heading_source_,
        MEO_OPT MEO_KEY("sprint_threshold") sprint_threshold_,
        MEO_OPT MEO_KEY("enable_local_driver") enable_local_driver_,
        MEO_OPT MEO_KEY("zip") zip_,
        MEO_OPT MEO_KEY("enable_bootstrap_navmesh") enable_bootstrap_navmesh_,
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

NaviParam build_navi_param(const json::value& custom_action_param, const NaviParamInput& input)
{
    NaviParam param;
    param.map_name = input.map_name_;
    param.heading_source = input.heading_source_ == "camera" ? HeadingSource::Camera : HeadingSource::Character;
    param.arrival_timeout = input.arrival_timeout_;
    param.sprint_threshold = input.sprint_threshold_;
    param.enable_local_driver = input.enable_local_driver_;
    param.zipline_enabled = input.zip_;
    param.enable_bootstrap_navmesh = input.enable_bootstrap_navmesh_;
    param.navmesh_file =
        first_present(custom_action_param, "nav_file", input.nav_file_, "navmesh_file", input.navmesh_file_).value_or(param.navmesh_file);
    param.navmesh_snap_radius =
        first_present(custom_action_param, "snap_radius", input.snap_radius_, "navmesh_snap_radius", input.navmesh_snap_radius_)
            .value_or(param.navmesh_snap_radius);
    return param;
}

bool has_action(const NaviWaypointInput& input, ActionType action)
{
    return std::find(input.actions_.begin(), input.actions_.end(), action) != input.actions_.end();
}

std::optional<std::array<double, 2>> authored_position(const NaviWaypointInput& input)
{
    if (input.target_) {
        return input.target_;
    }
    if (input.x_ && input.y_) {
        return std::array<double, 2> { *input.x_, *input.y_ };
    }
    return std::nullopt;
}

// 同时写了 RUN 和别的动作时 RUN 不单独展开
void append_expanded_waypoints(
    const NaviWaypointInput& input,
    const std::array<double, 2>& position,
    const std::string& zone_id,
    std::vector<Waypoint>& out_waypoints)
{
    const auto append = [&](ActionType action) {
        Waypoint waypoint(position.at(0), position.at(1), action);
        waypoint.zone_id = zone_id;
        waypoint.target_tier = input.target_tier_;
        waypoint.strict_arrival = input.strict_arrival_;
        waypoint.authored_strict_arrival = input.strict_arrival_;
        waypoint.route_required = input.required_;
        out_waypoints.push_back(std::move(waypoint));
    };

    if (input.actions_.empty()) {
        append(ActionType::RUN);
        return;
    }
    const bool has_non_run_action =
        std::any_of(input.actions_.begin(), input.actions_.end(), [](ActionType action) { return action != ActionType::RUN; });
    for (ActionType action : input.actions_) {
        if (has_non_run_action && action == ActionType::RUN) {
            continue;
        }
        append(action);
    }
}

// interact_rec 分不出写了 false 和没写, 所以只开不关
void apply_interact_defaults(const NaviInteractSpec& spec, std::vector<Waypoint>& waypoints, size_t from_index)
{
    for (size_t index = from_index; index < waypoints.size(); ++index) {
        Waypoint& waypoint = waypoints[index];
        if (waypoint.action != ActionType::INTERACT) {
            continue;
        }
        if (waypoint.interact_text.empty() && waypoint.interact_text_node.empty()) {
            waypoint.interact_text = spec.texts;
            waypoint.interact_text_node = spec.text_node;
        }
        if (waypoint.interact_scan.empty()) {
            waypoint.interact_scan = spec.scan;
        }
        if (spec.rec) {
            waypoint.interact_rec = true;
        }
    }
}

void apply_find_defaults(const NaviFindSpec& spec, std::vector<Waypoint>& waypoints, size_t from_index)
{
    for (size_t index = from_index; index < waypoints.size(); ++index) {
        Waypoint& waypoint = waypoints[index];
        if (waypoint.action != ActionType::FIND) {
            continue;
        }
        if (waypoint.find_target.empty() && waypoint.find_text.empty()) {
            waypoint.find_target = spec.target_node;
            waypoint.find_text = spec.texts;
        }
        if (waypoint.find_stop.empty()) {
            waypoint.find_stop = spec.stop_node;
        }
        if (!waypoint.find_arrive.has_value()) {
            waypoint.find_arrive = spec.arrive;
        }
    }
}

// 须在路线级默认值落位之后运行
bool validate_find_points(const std::vector<Waypoint>& waypoints)
{
    bool ok = true;
    for (size_t index = 0; index < waypoints.size(); ++index) {
        const Waypoint& waypoint = waypoints[index];
        if (waypoint.action != ActionType::FIND) {
            continue;
        }
        if (waypoint.find_target.empty() && waypoint.find_text.empty()) {
            LogError << "FIND waypoint has neither find_target nor find_text; nothing to look for." << VAR(index);
            ok = false;
        }
        if (waypoint.find_stop.empty() && !waypoint.find_arrive.has_value()) {
            LogError << "FIND waypoint has neither find_stop nor find_arrive; there is no way to tell it arrived." << VAR(index);
            ok = false;
        }
    }
    return ok;
}

void apply_trigger_node(const std::string& trigger_node, std::vector<Waypoint>& waypoints, size_t from_index)
{
    if (trigger_node.empty()) {
        return;
    }
    for (size_t index = from_index; index < waypoints.size(); ++index) {
        if (waypoints[index].action == ActionType::TRIGGER) {
            waypoints[index].trigger_node = trigger_node;
        }
    }
}

bool validate_trigger_points(const std::vector<Waypoint>& waypoints)
{
    bool ok = true;
    for (size_t index = 0; index < waypoints.size(); ++index) {
        const Waypoint& waypoint = waypoints[index];
        if (waypoint.action != ActionType::TRIGGER) {
            continue;
        }
        if (waypoint.trigger_node.empty()) {
            LogError << "TRIGGER waypoint has no trigger_node; nothing tells it the navigation is done." << VAR(index);
            ok = false;
        }
        if (index + 1 != waypoints.size()) {
            LogError << "TRIGGER waypoint must be the last point; a hit ends the navigation and the rest would never run." << VAR(index)
                     << VAR(waypoints.size());
            ok = false;
        }
    }
    return ok;
}

// 须在路线级默认值落位之后运行
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

void warn_unusable_interact_fields(const NaviWaypointInput& input)
{
    const NaviInteractSpec& interact = input.interact_;
    if (interact.texts.empty() && interact.text_node.empty() && interact.scan.empty() && !interact.rec) {
        return;
    }
    if (has_action(input, ActionType::INTERACT)) {
        return;
    }
    LogWarn << "Waypoint carries interact fields without an INTERACT action; they do nothing here."
            << VAR_AS(input.interact_text_.size(), interact.texts.size()) << VAR_AS(input.interact_text_node_, interact.text_node)
            << VAR_AS(input.interact_scan_, interact.scan) << VAR_AS(input.interact_rec_, interact.rec);
}

void warn_unusable_find_fields(const NaviWaypointInput& input)
{
    const NaviFindSpec& find = input.find_;
    if (!find.hasTargetSource() && find.stop_node.empty()) {
        return;
    }
    if (has_action(input, ActionType::FIND)) {
        return;
    }
    LogWarn << "Waypoint carries find fields without a FIND action; they do nothing here." << VAR_AS(input.find_target_, find.target_node)
            << VAR_AS(input.find_text_.size(), find.texts.size()) << VAR_AS(input.find_stop_, find.stop_node);
}

void warn_unusable_trigger_fields(const NaviWaypointInput& input)
{
    if (input.trigger_node_.empty()) {
        return;
    }
    if (has_action(input, ActionType::TRIGGER)) {
        return;
    }
    LogWarn << "Waypoint carries trigger_node without a TRIGGER action; it does nothing here." << VAR(input.trigger_node_);
}

bool reject_conflicting_find_fields(const NaviWaypointInput& input)
{
    const NaviFindSpec& find = input.find_;
    if (!find.conflicting_source) {
        return true;
    }
    if (!has_action(input, ActionType::FIND)) {
        LogWarn << "Waypoint carries both find_target and find_text without a FIND action; they do nothing here."
                << VAR_AS(input.find_target_, find.target_node) << VAR_AS(input.find_text_.size(), find.texts.size());
        return true;
    }
    LogError << "FIND waypoint sets both find_target and find_text; they are two ways to say what to look for."
             << VAR_AS(input.find_target_, find.target_node) << VAR_AS(input.find_text_.size(), find.texts.size());
    return false;
}

bool has_find_points(const std::vector<Waypoint>& waypoints)
{
    return std::any_of(waypoints.begin(), waypoints.end(), [](const Waypoint& waypoint) { return waypoint.action == ActionType::FIND; });
}

bool append_zone(const std::string& zone_id, std::vector<Waypoint>& out_waypoints, std::string& zone_context)
{
    if (zone_id.empty()) {
        return false;
    }
    out_waypoints.push_back(Waypoint::Zone(zone_id));
    zone_context = zone_id;
    return true;
}

bool append_heading(const NaviWaypointInput& input, const std::string& zone_id, std::vector<Waypoint>& out_waypoints)
{
    if (input.target_) {
        Waypoint heading_waypoint = Waypoint::HeadingToTarget(input.target_->at(0), input.target_->at(1));
        heading_waypoint.zone_id = zone_id;
        heading_waypoint.target_tier = input.target_tier_;
        heading_waypoint.route_required = input.required_;
        out_waypoints.push_back(std::move(heading_waypoint));
        return true;
    }
    if (!input.angle_) {
        return false;
    }
    Waypoint heading_waypoint = Waypoint::Heading(*input.angle_);
    heading_waypoint.zone_id = zone_id;
    heading_waypoint.route_required = input.required_;
    out_waypoints.push_back(std::move(heading_waypoint));
    return true;
}

bool append_navmesh(const NaviWaypointInput& input, const std::string& zone_id, std::vector<Waypoint>& out_waypoints)
{
    if (!input.target_) {
        return false;
    }
    Waypoint navmesh_waypoint(input.target_->at(0), input.target_->at(1), ActionType::NAVMESH);
    // 规划腿一律按严判收尾, 与作者是否写 strict_arrival 无关
    navmesh_waypoint.strict_arrival = true;
    navmesh_waypoint.zone_id = zone_id;
    // target 所在的 tier 坐标系, 与 zone_id 无关, 空表示基础像素坐标
    navmesh_waypoint.target_tier = input.target_tier_;
    navmesh_waypoint.target_deck_y = input.target_deck_y_;
    navmesh_waypoint.route_required = input.required_;
    out_waypoints.push_back(std::move(navmesh_waypoint));
    return true;
}

// 不带坐标的 FIND 不更新后续点沿用的区
bool append_find(
    const NaviWaypointInput& input,
    const std::string& zone_id,
    std::vector<Waypoint>& out_waypoints,
    std::string& zone_context)
{
    Waypoint find_waypoint;
    if (const std::optional<std::array<double, 2>> position = authored_position(input)) {
        find_waypoint = Waypoint(position->at(0), position->at(1), ActionType::FIND);
        find_waypoint.target_tier = input.target_tier_;
        find_waypoint.target_deck_y = input.target_deck_y_;
        find_waypoint.strict_arrival = input.strict_arrival_;
        find_waypoint.authored_strict_arrival = input.strict_arrival_;
        zone_context = zone_id;
    }
    else {
        find_waypoint.action = ActionType::FIND;
        find_waypoint.has_position = false;
        find_waypoint.strict_arrival = false;
    }
    find_waypoint.zone_id = zone_id;
    find_waypoint.route_required = input.required_;
    find_waypoint.find_target = input.find_.target_node;
    find_waypoint.find_text = input.find_.texts;
    find_waypoint.find_stop = input.find_.stop_node;
    find_waypoint.find_arrive = input.find_.arrive;
    out_waypoints.push_back(std::move(find_waypoint));
    return true;
}

// 没坐标但写了朝向时退化成只转向的点
bool append_positioned(
    const NaviWaypointInput& input,
    const std::string& zone_id,
    std::vector<Waypoint>& out_waypoints,
    std::string& zone_context)
{
    if (const std::optional<std::array<double, 2>> position = authored_position(input)) {
        const size_t first_expanded = out_waypoints.size();
        append_expanded_waypoints(input, *position, zone_id, out_waypoints);
        apply_interact_defaults(input.interact_, out_waypoints, first_expanded);
        apply_find_defaults(input.find_, out_waypoints, first_expanded);
        apply_trigger_node(input.trigger_node_, out_waypoints, first_expanded);
        zone_context = zone_id;
        return true;
    }
    if (!input.angle_) {
        return false;
    }
    Waypoint heading_waypoint = Waypoint::Heading(*input.angle_);
    heading_waypoint.zone_id = zone_id;
    out_waypoints.push_back(std::move(heading_waypoint));
    return true;
}

bool append_parsed_waypoint(const NaviWaypointInput& input, std::vector<Waypoint>& out_waypoints, std::string& zone_context)
{
    warn_unusable_interact_fields(input);
    warn_unusable_find_fields(input);
    warn_unusable_trigger_fields(input);
    if (!reject_conflicting_find_fields(input)) {
        return false;
    }
    const std::string zone_id = resolve_waypoint_zone_id(input, zone_context);
    const ActionType primary_action = input.actions_.empty() ? ActionType::RUN : input.actions_.front();

    if (primary_action == ActionType::ZONE) {
        return append_zone(zone_id, out_waypoints, zone_context);
    }
    if (primary_action == ActionType::HEADING) {
        return append_heading(input, zone_id, out_waypoints);
    }
    if (primary_action == ActionType::NAVMESH) {
        return append_navmesh(input, zone_id, out_waypoints);
    }
    if (primary_action == ActionType::FIND && input.actions_.size() <= 1) {
        return append_find(input, zone_id, out_waypoints, zone_context);
    }
    return append_positioned(input, zone_id, out_waypoints, zone_context);
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

bool read_route_header(
    const json::value& custom_action_param,
    const std::string& caller_name_text,
    NaviParamInput& input,
    NaviInteractSpec& route_interact,
    NaviFindSpec& route_find)
{
    std::string error_key;
    if (!input.from_json(custom_action_param, error_key)) {
        LogError << "Failed to deserialize " << caller_name_text << " param." << VAR(error_key) << VAR(custom_action_param);
        return false;
    }
    if (!read_interact_spec(custom_action_param, route_interact)) {
        LogError << "Failed to deserialize " << caller_name_text << " interact defaults." << VAR(custom_action_param);
        return false;
    }
    if (!read_find_spec(custom_action_param, route_find)) {
        LogError << "Failed to deserialize " << caller_name_text << " find defaults." << VAR(custom_action_param);
        return false;
    }
    if (input.heading_source_ != "character" && input.heading_source_ != "camera") {
        LogError << "Invalid navigation heading_source." << VAR(input.heading_source_);
        return false;
    }
    return true;
}

// 写了 path 按多点解析, 否则顶层对象本身就是一个点
bool append_route_waypoints(
    const json::value& custom_action_param,
    const NaviParamInput& input,
    const std::string& caller_name_text,
    NaviParam& param)
{
    std::string zone_context = param.map_name;
    if (has_key(custom_action_param, "path")) {
        return append_parsed_waypoints(input.path_, param.path, zone_context, caller_name_text);
    }
    if (!has_any_key(custom_action_param, { "action", "actions", "x", "y", "angle", "heading", "yaw", "target" })) {
        return true;
    }
    NaviWaypointInput waypoint;
    if (!waypoint.from_json(custom_action_param) || !append_parsed_waypoint(waypoint, param.path, zone_context)) {
        LogError << "Failed to parse " << caller_name_text << " waypoint from custom_action_param object.";
        return false;
    }
    return true;
}

// 路线级默认值, 点上自己写的优先
bool apply_route_defaults(
    const NaviInteractSpec& route_interact,
    const NaviFindSpec& route_find,
    const std::string& caller_name_text,
    std::vector<Waypoint>& waypoints)
{
    apply_interact_defaults(route_interact, waypoints, 0);
    warn_scan_without_text(waypoints);
    apply_find_defaults(route_find, waypoints, 0);
    if (!route_find.conflicting_source) {
        return true;
    }
    if (has_find_points(waypoints)) {
        LogError << "Route sets both find_target and find_text; they are two ways to say what to look for." << VAR(caller_name_text)
                 << VAR(route_find.target_node) << VAR(route_find.texts.size());
        return false;
    }
    LogWarn << "Route sets both find_target and find_text but carries no FIND point; they do nothing here." << VAR(caller_name_text)
            << VAR(route_find.target_node) << VAR(route_find.texts.size());
    return true;
}

} // namespace

bool TryParseNaviParam(const json::value& custom_action_param, NaviParam& out_param, std::string_view caller_name)
{
    const std::string caller_name_text(caller_name);
    NaviParamInput input;
    NaviInteractSpec route_interact;
    NaviFindSpec route_find;
    if (!read_route_header(custom_action_param, caller_name_text, input, route_interact, route_find)) {
        return false;
    }

    NaviParam param = build_navi_param(custom_action_param, input);
    if (!append_route_waypoints(custom_action_param, input, caller_name_text, param)
        || !apply_route_defaults(route_interact, route_find, caller_name_text, param.path)) {
        return false;
    }
    if (!validate_find_points(param.path) || !validate_trigger_points(param.path)) {
        return false;
    }

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
        if (!TryReadNodeInteractTexts(context, waypoint.interact_text_node, &texts)) {
            continue;
        }
        waypoint.interact_text = std::move(texts);
        LogInfo << "Interact text resolved from node." << VAR(waypoint.interact_text_node) << VAR(waypoint.interact_text.size());
    }
}

} // namespace mapnavigator
