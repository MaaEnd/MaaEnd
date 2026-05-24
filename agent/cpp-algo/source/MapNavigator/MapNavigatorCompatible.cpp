#include "MapNavigatorCompatible.h"

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <cstring>
#include <filesystem>
#include <limits>
#include <optional>
#include <string>
#include <string_view>
#include <system_error>
#include <unordered_map>
#include <utility>
#include <vector>

#include <meojson/json.hpp>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

#include "../MapLocator/MapLocateAction.h"
#include "action_wrapper.h"
#include "navi_controller.h"
#include "navi_math.h"
#include "navi_param_parser.h"

namespace mapnavigator
{

namespace
{

// MapTrackerAssertLocation legacy default; MapLocateAssertLocation defaults are stricter.
constexpr double kMapTrackerAssertDefaultThreshold = 0.4;
// Old MapTracker tier images darken unrelated floors; only bright pixels represent the active tier.
constexpr double kMapTrackerEffectiveLuminanceThreshold = 80.0;
// Avoid jitter from tiny post-navigation heading differences.
constexpr double kFinalOrientationToleranceDegrees = 1.0;
// Give the backend a short frame to release stale movement keys before compatible navigation.
constexpr int kInitialMovementResetDelayMillis = 120;
constexpr MaaBool kMaaTrue = 1;
constexpr MaaBool kMaaFalse = 0;

struct MapTrackerCoordinateTransform
{
    std::string map_name_;
    std::string zone_id_;
    double offset_x_ = 0.0;
    double offset_y_ = 0.0;
    double scale_x_ = 1.0;
    double scale_y_ = 1.0;
    std::string parent_map_name_;
    std::vector<double> source_bbox_;

    MEO_JSONIZATION(
        MEO_KEY("map_name") map_name_,
        MEO_KEY("zone_id") zone_id_,
        MEO_KEY("offset_x") offset_x_,
        MEO_KEY("offset_y") offset_y_,
        MEO_KEY("scale_x") scale_x_,
        MEO_KEY("scale_y") scale_y_,
        MEO_OPT MEO_KEY("parent_map_name") parent_map_name_,
        MEO_OPT MEO_KEY("source_bbox") source_bbox_)

    bool hasParentMap() const { return !parent_map_name_.empty(); }

    bool containsSourcePoint(double x, double y) const
    {
        return source_bbox_.size() == 4 && source_bbox_[0] <= x && x <= source_bbox_[2] && source_bbox_[1] <= y
               && y <= source_bbox_[3];
    }

    double sourceArea() const
    {
        if (source_bbox_.size() != 4) {
            return 0.0;
        }
        return std::max(0.0, source_bbox_[2] - source_bbox_[0]) * std::max(0.0, source_bbox_[3] - source_bbox_[1]);
    }
};

struct MapTrackerCoordinateTransformConfig
{
    std::vector<MapTrackerCoordinateTransform> transforms_;

    MEO_JSONIZATION(MEO_KEY("transforms") transforms_)
};

struct MapTrackerCompatibleConditionInput
{
    std::optional<std::string> zone_id_;
    std::optional<std::string> zoneId_;
    std::optional<std::string> zone_;
    std::optional<std::string> map_name_;
    std::optional<std::string> mapName_;
    std::vector<double> target_;

    MEO_JSONIZATION(
        MEO_OPT MEO_KEY("zone_id") zone_id_,
        MEO_OPT MEO_KEY("zoneId") zoneId_,
        MEO_OPT MEO_KEY("zone") zone_,
        MEO_OPT MEO_KEY("map_name") map_name_,
        MEO_OPT MEO_KEY("mapName") mapName_,
        MEO_OPT MEO_KEY("target") target_)
};

struct MapTrackerCompatibleCondition
{
    std::string map_name_;
    std::vector<double> target_;
};

struct MapTrackerMoveOptions
{
    bool path_trim_ = false;
    bool no_ensure_final_orientation_ = false;
    bool no_ensure_initial_movement_state_ = false;
    std::string fine_approach_ = "FinalTarget";

    MEO_JSONIZATION(
        MEO_OPT MEO_KEY("path_trim") path_trim_,
        MEO_OPT MEO_KEY("no_ensure_final_orientation") no_ensure_final_orientation_,
        MEO_OPT MEO_KEY("no_ensure_initial_movement_state") no_ensure_initial_movement_state_,
        MEO_OPT MEO_KEY("fine_approach") fine_approach_)
};

struct MapNavigatorAssertLocationCompatibleParam
{
    std::vector<MapTrackerCompatibleConditionInput> expected_;
    double threshold_ = kMapTrackerAssertDefaultThreshold;

    MEO_JSONIZATION(MEO_KEY("expected") expected_, MEO_OPT MEO_KEY("threshold") threshold_)
};

struct MapNavigatorCompatibleLocateRequest
{
    std::string expected_zone_id_;
    bool force_global_search_ = true;

    MEO_JSONIZATION(MEO_OPT MEO_KEY("expected_zone_id") expected_zone_id_, MEO_OPT MEO_KEY("force_global_search") force_global_search_)
};

struct MapLocatorAssertLocationCompatibleRequest
{
    std::string zone_id_;
    std::vector<double> target_;
    double loc_threshold_ = kMapTrackerAssertDefaultThreshold;

    MEO_JSONIZATION(MEO_KEY("zone_id") zone_id_, MEO_KEY("target") target_, MEO_KEY("loc_threshold") loc_threshold_)
};

struct MapNavigatorCompatibleLocateDetail
{
    std::string mapName_;
    double x_ = 0.0;
    double y_ = 0.0;
    double rot_ = 0.0;

    MEO_JSONIZATION(MEO_KEY("mapName") mapName_, MEO_KEY("x") x_, MEO_KEY("y") y_, MEO_KEY("rot") rot_)
};

std::string resolve_condition_map_name(const MapTrackerCompatibleConditionInput& input)
{
    if (input.zone_id_.has_value() && !input.zone_id_->empty()) {
        return *input.zone_id_;
    }
    if (input.zoneId_.has_value() && !input.zoneId_->empty()) {
        return *input.zoneId_;
    }
    if (input.zone_.has_value() && !input.zone_->empty()) {
        return *input.zone_;
    }
    if (input.map_name_.has_value() && !input.map_name_->empty()) {
        return *input.map_name_;
    }
    if (input.mapName_.has_value() && !input.mapName_->empty()) {
        return *input.mapName_;
    }
    return {};
}

std::optional<MapTrackerCompatibleCondition> try_make_condition_from_input(const MapTrackerCompatibleConditionInput& input)
{
    const std::string map_name = resolve_condition_map_name(input);
    if (map_name.empty() || input.target_.size() != 4 || input.target_[2] <= 0.0 || input.target_[3] <= 0.0) {
        return std::nullopt;
    }

    return MapTrackerCompatibleCondition { .map_name_ = map_name, .target_ = input.target_ };
}

std::optional<json::value> parse_detail_value(const MaaStringBuffer* buffer)
{
    if (buffer == nullptr || MaaStringBufferIsEmpty(buffer)) {
        return std::nullopt;
    }

    const char* raw_detail = MaaStringBufferGet(buffer);
    if (raw_detail == nullptr) {
        return std::nullopt;
    }

    return json::parse(raw_detail);
}

std::optional<MapNavigatorCompatibleLocateDetail> try_parse_locate_detail(const MaaStringBuffer* buffer)
{
    const auto detail_opt = parse_detail_value(buffer);
    if (!detail_opt) {
        return std::nullopt;
    }

    MapNavigatorCompatibleLocateDetail detail;
    if (!detail.from_json(*detail_opt)) {
        return std::nullopt;
    }
    return detail;
}

class ScopedImageBuffer
{
public:
    ScopedImageBuffer()
        : buffer_(MaaImageBufferCreate())
    {
    }

    ~ScopedImageBuffer()
    {
        if (buffer_ != nullptr) {
            MaaImageBufferDestroy(buffer_);
        }
    }

    ScopedImageBuffer(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer& operator=(const ScopedImageBuffer&) = delete;

    MaaImageBuffer* Get() const { return buffer_; }

private:
    MaaImageBuffer* buffer_ = nullptr;
};

class ScopedStringBuffer
{
public:
    ScopedStringBuffer()
        : buffer_(MaaStringBufferCreate())
    {
    }

    ~ScopedStringBuffer()
    {
        if (buffer_ != nullptr) {
            MaaStringBufferDestroy(buffer_);
        }
    }

    ScopedStringBuffer(const ScopedStringBuffer&) = delete;
    ScopedStringBuffer& operator=(const ScopedStringBuffer&) = delete;

    MaaStringBuffer* Get() const { return buffer_; }

private:
    MaaStringBuffer* buffer_ = nullptr;
};

struct MapTrackerImageCacheEntry
{
    cv::Mat image_;
    bool loaded_ = false;
};

constexpr const char* kMapTrackerCoordinateTransformsFile = "maptracker_coordinate_transforms.json";

std::optional<std::filesystem::path> find_existing_from_parents(const std::filesystem::path& relative_path);

std::vector<MapTrackerCoordinateTransform> load_maptracker_coordinate_transforms()
{
    const std::filesystem::path source_tree_path =
        std::filesystem::path("assets") / "resource" / "image" / "MapLocator" / kMapTrackerCoordinateTransformsFile;
    const std::filesystem::path packaged_path =
        std::filesystem::path("resource") / "image" / "MapLocator" / kMapTrackerCoordinateTransformsFile;

    for (const auto& relative_path : { source_tree_path, packaged_path }) {
        const auto config_path = find_existing_from_parents(relative_path);
        if (!config_path.has_value()) {
            continue;
        }

        const auto config_json = json::open(*config_path);
        const std::string config_path_text = config_path->string();
        if (!config_json) {
            LogError << "Failed to parse MapTracker coordinate transform mapping." << VAR(config_path_text);
            return {};
        }

        MapTrackerCoordinateTransformConfig config;
        if (!config.from_json(*config_json)) {
            LogError << "Invalid MapTracker coordinate transform mapping." << VAR(config_path_text);
            return {};
        }

        LogInfo << "Loaded MapTracker coordinate transform mapping." << VAR(config_path_text) << VAR(config.transforms_.size());
        return std::move(config.transforms_);
    }

    LogError << "MapTracker coordinate transform mapping not found." << VAR(kMapTrackerCoordinateTransformsFile);
    return {};
}

const std::vector<MapTrackerCoordinateTransform>& maptracker_coordinate_transforms()
{
    static const std::vector<MapTrackerCoordinateTransform> transforms = load_maptracker_coordinate_transforms();
    return transforms;
}

const MapTrackerCoordinateTransform* find_maptracker_coordinate_transform(std::string_view map_name)
{
    for (const auto& transform : maptracker_coordinate_transforms()) {
        if (std::string_view(transform.map_name_) == map_name) {
            return &transform;
        }
    }
    return nullptr;
}

std::optional<std::filesystem::path> find_existing_from_parents(const std::filesystem::path& relative_path)
{
    std::error_code ec;
    std::filesystem::path current = std::filesystem::current_path(ec);
    if (ec) {
        return std::nullopt;
    }

    while (!current.empty()) {
        const std::filesystem::path candidate = current / relative_path;
        if (std::filesystem::exists(candidate, ec) && !ec) {
            return candidate;
        }
        const std::filesystem::path parent = current.parent_path();
        if (parent == current) {
            break;
        }
        current = parent;
    }
    return std::nullopt;
}

cv::Mat load_maptracker_image(std::string_view map_name)
{
    static std::unordered_map<std::string, MapTrackerImageCacheEntry> cache;

    const std::string map_name_text(map_name);
    auto cache_iter = cache.find(map_name_text);
    if (cache_iter != cache.end()) {
        return cache_iter->second.image_;
    }

    MapTrackerImageCacheEntry entry;
    const std::filesystem::path source_tree_path =
        std::filesystem::path("assets") / "resource" / "image" / "MapTracker" / "map" / (map_name_text + ".png");
    const std::filesystem::path packaged_path =
        std::filesystem::path("resource") / "image" / "MapTracker" / "map" / (map_name_text + ".png");
    for (const auto& relative_path : { source_tree_path, packaged_path }) {
        if (auto image_path = find_existing_from_parents(relative_path); image_path.has_value()) {
            entry.image_ = MAA_NS::imread(image_path->string(), cv::IMREAD_UNCHANGED);
            entry.loaded_ = !entry.image_.empty();
            if (entry.loaded_) {
                break;
            }
        }
    }
    if (!entry.loaded_) {
        LogWarn << "Failed to load MapTracker tier image for compatibility mask." << VAR(map_name_text);
    }
    return cache.emplace(map_name_text, std::move(entry)).first->second.image_;
}

std::optional<double> maptracker_luminance_at(std::string_view map_name, double x, double y)
{
    const cv::Mat image = load_maptracker_image(map_name);
    if (image.empty()) {
        return std::nullopt;
    }

    const int pixel_x = static_cast<int>(std::lround(x));
    const int pixel_y = static_cast<int>(std::lround(y));
    if (pixel_x < 0 || pixel_x >= image.cols || pixel_y < 0 || pixel_y >= image.rows) {
        return std::nullopt;
    }

    if (image.channels() == 1) {
        return static_cast<double>(image.at<uchar>(pixel_y, pixel_x));
    }
    if (image.channels() == 3) {
        const cv::Vec3b color = image.at<cv::Vec3b>(pixel_y, pixel_x);
        return (static_cast<double>(color[0]) + static_cast<double>(color[1]) + static_cast<double>(color[2])) / 3.0;
    }
    if (image.channels() == 4) {
        const cv::Vec4b color = image.at<cv::Vec4b>(pixel_y, pixel_x);
        return (static_cast<double>(color[0]) + static_cast<double>(color[1]) + static_cast<double>(color[2])) / 3.0;
    }
    return std::nullopt;
}

bool is_effective_tier_point(const MapTrackerCoordinateTransform& transform, double x, double y)
{
    const auto luminance = maptracker_luminance_at(transform.map_name_, x, y);
    return luminance.has_value() && *luminance >= kMapTrackerEffectiveLuminanceThreshold;
}

const MapTrackerCoordinateTransform* find_maptracker_point_transform(std::string_view map_name, double x, double y)
{
    const auto* transform = find_maptracker_coordinate_transform(map_name);
    if (transform == nullptr || transform->hasParentMap()) {
        return transform;
    }

    const MapTrackerCoordinateTransform* best_child = nullptr;
    double best_area = std::numeric_limits<double>::infinity();
    for (const auto& candidate : maptracker_coordinate_transforms()) {
        if (candidate.parent_map_name_ != transform->map_name_ || !candidate.containsSourcePoint(x, y)
            || !is_effective_tier_point(candidate, x, y)) {
            continue;
        }

        const double area = candidate.sourceArea();
        if (area < best_area) {
            best_child = &candidate;
            best_area = area;
        }
    }
    return best_child == nullptr ? transform : best_child;
}

std::string root_maptracker_map_name(const MapTrackerCoordinateTransform& transform)
{
    return std::string(transform.parent_map_name_.empty() ? transform.map_name_ : transform.parent_map_name_);
}

std::string infer_route_maptracker_map_name(const NaviParam& param)
{
    const auto* param_transform = find_maptracker_coordinate_transform(param.map_name);
    if (param_transform != nullptr) {
        return root_maptracker_map_name(*param_transform);
    }

    std::unordered_map<std::string, int> map_name_counts;
    for (const auto& waypoint : param.path) {
        const auto* transform = find_maptracker_coordinate_transform(waypoint.zone_id);
        if (transform == nullptr) {
            continue;
        }

        const std::string root_map_name = root_maptracker_map_name(*transform);
        ++map_name_counts[root_map_name];
    }

    if (map_name_counts.empty()) {
        return {};
    }

    const auto best_count = std::max_element(map_name_counts.begin(), map_name_counts.end(), [](const auto& lhs, const auto& rhs) {
        return lhs.second < rhs.second;
    });
    return best_count->first;
}

const MapTrackerCoordinateTransform*
    resolve_waypoint_transform(const Waypoint& waypoint, const std::string& source_map_name, const std::string& route_map_name)
{
    const bool has_point = waypoint.HasPosition() || waypoint.heading_uses_target;
    const auto* transform = has_point ? find_maptracker_point_transform(source_map_name, waypoint.x, waypoint.y)
                                      : find_maptracker_coordinate_transform(source_map_name);
    if (transform != nullptr && has_point && !route_map_name.empty() && transform->map_name_ == route_map_name) {
        const auto* refined_transform = find_maptracker_point_transform(route_map_name, waypoint.x, waypoint.y);
        if (refined_transform != nullptr) {
            transform = refined_transform;
        }
    }
    return transform;
}

double transform_maptracker_x(double x, const MapTrackerCoordinateTransform& transform)
{
    return transform.offset_x_ + x * transform.scale_x_;
}

double transform_maptracker_y(double y, const MapTrackerCoordinateTransform& transform)
{
    return transform.offset_y_ + y * transform.scale_y_;
}

void transform_maptracker_rect(std::vector<double>& target, const MapTrackerCoordinateTransform& transform)
{
    target[0] = transform_maptracker_x(target[0], transform);
    target[1] = transform_maptracker_y(target[1], transform);
    target[2] *= transform.scale_x_;
    target[3] *= transform.scale_y_;
}

void apply_maptracker_coordinate_transform(Waypoint& waypoint, const MapTrackerCoordinateTransform& transform)
{
    if (waypoint.HasPosition() || waypoint.heading_uses_target) {
        waypoint.x = transform_maptracker_x(waypoint.x, transform);
        waypoint.y = transform_maptracker_y(waypoint.y, transform);
    }

    if (waypoint.zone_id.empty() || waypoint.zone_id == transform.map_name_ || waypoint.zone_id == transform.parent_map_name_) {
        waypoint.zone_id = std::string(transform.zone_id_);
    }
}

void apply_boundary_portal_semantics(std::vector<Waypoint>& path)
{
    size_t previous_position_index = std::numeric_limits<size_t>::max();
    for (size_t index = 0; index < path.size(); ++index) {
        if (!path[index].HasPosition()) {
            continue;
        }

        if (previous_position_index != std::numeric_limits<size_t>::max()) {
            const std::string& previous_zone = path[previous_position_index].zone_id;
            const std::string& current_zone = path[index].zone_id;
            if (!previous_zone.empty() && !current_zone.empty() && previous_zone != current_zone) {
                path[previous_position_index].action = ActionType::PORTAL;
                path[index].action = ActionType::PORTAL;
            }
        }
        previous_position_index = index;
    }
}

bool has_any_position(const std::vector<Waypoint>& path)
{
    return std::any_of(path.begin(), path.end(), [](const Waypoint& waypoint) { return waypoint.HasPosition(); });
}

void insert_zone_declarations(std::vector<Waypoint>& path)
{
    std::vector<Waypoint> result;
    result.reserve(path.size() * 2);

    std::string current_zone;
    for (const auto& waypoint : path) {
        if (waypoint.IsZoneDeclaration()) {
            if (!waypoint.zone_id.empty() && waypoint.zone_id != current_zone) {
                result.push_back(waypoint);
                current_zone = waypoint.zone_id;
            }
            continue;
        }

        if (!waypoint.zone_id.empty() && waypoint.zone_id != current_zone) {
            result.push_back(Waypoint::Zone(waypoint.zone_id));
            current_zone = waypoint.zone_id;
        }
        result.push_back(waypoint);
    }

    path = std::move(result);
}

void update_param_map_name_from_path(NaviParam& param)
{
    for (const auto& waypoint : param.path) {
        if (!waypoint.zone_id.empty()) {
            param.map_name = waypoint.zone_id;
            return;
        }
    }
}

bool convert_maptracker_waypoint(Waypoint& waypoint, const std::string& default_map_name, const std::string& route_map_name)
{
    const std::string source_map_name = waypoint.zone_id.empty() ? default_map_name : waypoint.zone_id;
    if (source_map_name.empty()) {
        LogError << "MapNavigatorCompatible waypoint is missing MapTracker map name.";
        return false;
    }

    const MapTrackerCoordinateTransform* transform = resolve_waypoint_transform(waypoint, source_map_name, route_map_name);
    if (transform == nullptr) {
        LogError << "MapNavigatorCompatible unsupported waypoint map name." << VAR(source_map_name);
        return false;
    }

    apply_maptracker_coordinate_transform(waypoint, *transform);
    return true;
}

bool convert_maptracker_coordinates(NaviParam& param)
{
    if (param.map_name.empty()) {
        LogError << "MapNavigatorCompatible requires map_name for MapTracker coordinate conversion.";
        return false;
    }

    const std::string route_map_name = infer_route_maptracker_map_name(param);
    for (auto& waypoint : param.path) {
        if (!convert_maptracker_waypoint(waypoint, param.map_name, route_map_name)) {
            return false;
        }
    }

    update_param_map_name_from_path(param);
    LogInfo << "MapTracker coordinates converted for MapNavigator." << VAR(param.map_name) << VAR(param.path.size());
    return true;
}

bool try_parse_maptracker_expected_conditions(
    const MapNavigatorAssertLocationCompatibleParam& param,
    std::vector<MapTrackerCompatibleCondition>& out_conditions)
{
    if (param.expected_.empty()) {
        return false;
    }

    for (const auto& item : param.expected_) {
        const auto condition = try_make_condition_from_input(item);
        if (!condition) {
            return false;
        }

        out_conditions.push_back(*condition);
    }

    return true;
}

bool convert_maptracker_condition(MapTrackerCompatibleCondition& condition)
{
    if (condition.target_.size() != 4) {
        LogError << "MapNavigatorAssertLocationCompatible invalid target." << VAR(condition.map_name_);
        return false;
    }

    const double center_x = condition.target_[0] + condition.target_[2] / 2.0;
    const double center_y = condition.target_[1] + condition.target_[3] / 2.0;
    const auto* transform = find_maptracker_point_transform(condition.map_name_, center_x, center_y);
    if (transform == nullptr) {
        LogError << "MapNavigatorAssertLocationCompatible unsupported condition." << VAR(condition.map_name_);
        return false;
    }

    transform_maptracker_rect(condition.target_, *transform);
    condition.map_name_ = std::string(transform->zone_id_);
    return true;
}

MapTrackerMoveOptions read_maptracker_move_options(const json::value& options)
{
    MapTrackerMoveOptions result;
    if (!result.from_json(options)) {
        return result;
    }

    if (result.fine_approach_.empty()) {
        result.fine_approach_ = "FinalTarget";
    }
    return result;
}

void apply_maptracker_move_options(const MapTrackerMoveOptions& options, NaviParam& param)
{
    if (options.fine_approach_ == "AllTargets") {
        for (auto& waypoint : param.path) {
            if (waypoint.HasPosition()) {
                waypoint.strict_arrival = true;
            }
        }
    }
    else if (options.fine_approach_ == "FinalTarget") {
        for (auto iter = param.path.rbegin(); iter != param.path.rend(); ++iter) {
            if (iter->HasPosition()) {
                iter->strict_arrival = true;
                break;
            }
        }
    }
}

bool try_make_locate_assert_param(const MapTrackerCompatibleCondition& condition, double threshold, std::string& out_param)
{
    if (condition.target_.size() != 4) {
        return false;
    }

    MapLocatorAssertLocationCompatibleRequest request;
    request.zone_id_ = condition.map_name_;
    request.target_ = condition.target_;
    request.loc_threshold_ = threshold;
    out_param = json::value(request).dumps();
    return true;
}

bool try_make_assert_box(const std::vector<double>& target, MaaRect* out_box)
{
    if (out_box == nullptr || target.size() != 4 || target[2] <= 0.0 || target[3] <= 0.0) {
        return false;
    }

    out_box->x = static_cast<int>(std::lround(target[0]));
    out_box->y = static_cast<int>(std::lround(target[1]));
    out_box->width = std::max(1, static_cast<int>(std::lround(target[2])));
    out_box->height = std::max(1, static_cast<int>(std::lround(target[3])));
    return true;
}

void trim_path_to_current_position(NaviParam& param, MaaContext* context, const MapTrackerMoveOptions& options)
{
    if (!options.path_trim_ || param.path.size() <= 1 || context == nullptr) {
        return;
    }

    ScopedImageBuffer image;
    if (image.Get() == nullptr) {
        return;
    }

    MaaRect out_box {};
    ScopedStringBuffer detail;
    if (detail.Get() == nullptr) {
        return;
    }

    MapNavigatorCompatibleLocateRequest locate_request;
    locate_request.expected_zone_id_ = param.map_name;
    locate_request.force_global_search_ = true;
    const std::string locate_param_text = json::value(locate_request).dumps();
    const MaaBool located = maplocator::MapLocateRecognitionRun(
        context,
        0,
        "MapNavigatorCompatiblePathTrim",
        "MapLocateRecognition",
        locate_param_text.c_str(),
        image.Get(),
        nullptr,
        nullptr,
        &out_box,
        detail.Get());

    if (!located || MaaStringBufferIsEmpty(detail.Get())) {
        LogWarn << "MapNavigatorCompatible path_trim failed to locate current position." << VAR(param.map_name);
        return;
    }

    const auto detail_opt = try_parse_locate_detail(detail.Get());
    if (!detail_opt) {
        LogWarn << "MapNavigatorCompatible path_trim failed to parse locate detail.";
        return;
    }

    if (detail_opt->mapName_ != param.map_name) {
        LogWarn << "MapNavigatorCompatible path_trim located unexpected map." << VAR(param.map_name) << VAR(detail_opt->mapName_);
        return;
    }

    const double current_x = detail_opt->x_;
    const double current_y = detail_opt->y_;
    size_t closest_index = 0;
    double closest_distance = std::numeric_limits<double>::infinity();
    for (size_t index = 0; index < param.path.size(); ++index) {
        const Waypoint& waypoint = param.path[index];
        if (!waypoint.HasPosition()) {
            continue;
        }
        const double distance = std::hypot(current_x - waypoint.x, current_y - waypoint.y);
        if (distance < closest_distance) {
            closest_distance = distance;
            closest_index = index;
        }
    }

    if (closest_index > 0) {
        param.path.erase(param.path.begin(), param.path.begin() + static_cast<std::ptrdiff_t>(closest_index));
        LogInfo << "MapNavigatorCompatible path_trim applied." << VAR(closest_index) << VAR(closest_distance);
    }
}

std::optional<double> try_locate_heading(MaaContext* context, const std::string& zone_id)
{
    if (context == nullptr) {
        return std::nullopt;
    }

    ScopedImageBuffer image;
    if (image.Get() == nullptr) {
        return std::nullopt;
    }

    MaaRect out_box {};
    ScopedStringBuffer detail;
    if (detail.Get() == nullptr) {
        return std::nullopt;
    }

    MapNavigatorCompatibleLocateRequest locate_request;
    locate_request.expected_zone_id_ = zone_id;
    locate_request.force_global_search_ = true;
    const std::string locate_param_text = json::value(locate_request).dumps();
    const MaaBool located = maplocator::MapLocateRecognitionRun(
        context,
        0,
        "MapNavigatorCompatibleFinalOrientation",
        "MapLocateRecognition",
        locate_param_text.c_str(),
        image.Get(),
        nullptr,
        nullptr,
        &out_box,
        detail.Get());

    std::optional<double> heading;
    if (located && !MaaStringBufferIsEmpty(detail.Get())) {
        const auto detail_opt = try_parse_locate_detail(detail.Get());
        if (detail_opt && detail_opt->mapName_ == zone_id) {
            heading = detail_opt->rot_;
        }
    }

    return heading;
}

void ensure_final_orientation(MaaContext* context, const NaviParam& param, const MapTrackerMoveOptions& options)
{
    if (options.no_ensure_final_orientation_ || param.path.size() < 2) {
        return;
    }

    const Waypoint& final_waypoint = param.path.back();
    const Waypoint& previous_waypoint = param.path[param.path.size() - 2];
    if (!final_waypoint.HasPosition() || !previous_waypoint.HasPosition()) {
        return;
    }

    const auto current_heading = try_locate_heading(context, param.map_name);
    if (!current_heading) {
        LogWarn << "MapNavigatorCompatible final orientation skipped: locate failed." << VAR(param.map_name);
        return;
    }

    ActionWrapper action_wrapper(context);
    if (!action_wrapper.is_supported()) {
        LogWarn << "MapNavigatorCompatible final orientation skipped: unsupported backend." << VAR(action_wrapper.controller_type());
        return;
    }

    const double target_heading =
        NaviMath::CalcTargetRotation(previous_waypoint.x, previous_waypoint.y, final_waypoint.x, final_waypoint.y);
    const double heading_delta = NaviMath::NormalizeAngle(target_heading - NaviMath::NormalizeAngle(*current_heading));
    if (std::abs(heading_delta) <= kFinalOrientationToleranceDegrees) {
        return;
    }

    int units = static_cast<int>(std::lround(heading_delta * action_wrapper.DefaultTurnUnitsPerDegree()));
    if (units == 0) {
        units = heading_delta > 0.0 ? 1 : -1;
    }
    if (!action_wrapper.SendViewDeltaSync(units, 0)) {
        LogWarn << "MapNavigatorCompatible final orientation adjustment failed." << VAR(units);
        return;
    }
    LogInfo << "MapNavigatorCompatible final orientation adjusted." << VAR(target_heading) << VAR(*current_heading) << VAR(heading_delta)
            << VAR(units);
}

} // namespace

MaaBool MAA_CALL MapNavigatorCompatibleRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    if (custom_action_param == nullptr || std::strlen(custom_action_param) == 0) {
        return kMaaTrue;
    }

    const auto options_opt = json::parse(custom_action_param);
    if (!options_opt) {
        LogError << "Failed to parse MapNavigatorCompatible param (invalid JSON)" << VAR(custom_action_param);
        return kMaaFalse;
    }

    NaviParam param;
    if (!TryParseNaviParam(*options_opt, param, "MapNavigatorCompatible")) {
        return kMaaFalse;
    }

    const MapTrackerMoveOptions move_options = read_maptracker_move_options(*options_opt);
    if (!convert_maptracker_coordinates(param)) {
        return kMaaFalse;
    }
    apply_boundary_portal_semantics(param.path);
    insert_zone_declarations(param.path);
    trim_path_to_current_position(param, context, move_options);
    apply_maptracker_move_options(move_options, param);

    if (!has_any_position(param.path)) {
        return kMaaTrue;
    }

    if (!move_options.no_ensure_initial_movement_state_) {
        ActionWrapper action_wrapper(context);
        if (action_wrapper.is_supported()) {
            action_wrapper.SetMovementStateSync(false, false, false, false, kInitialMovementResetDelayMillis);
        }
    }

    NaviController controller(context);
    if (!controller.Navigate(param)) {
        return kMaaFalse;
    }
    ensure_final_orientation(context, param, move_options);
    return kMaaTrue;
}

MaaBool MAA_CALL MapNavigatorAssertLocationCompatibleRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi,
    [[maybe_unused]] void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    if (custom_recognition_param == nullptr || std::strlen(custom_recognition_param) == 0) {
        LogError << "MapNavigatorAssertLocationCompatible requires custom_recognition_param.";
        return kMaaFalse;
    }

    const auto options_opt = json::parse(custom_recognition_param);
    if (!options_opt) {
        LogError << "Failed to parse MapNavigatorAssertLocationCompatible param." << VAR(custom_recognition_param);
        return kMaaFalse;
    }

    MapNavigatorAssertLocationCompatibleParam param;
    if (!param.from_json(*options_opt)) {
        LogError << "Failed to deserialize MapNavigatorAssertLocationCompatible param." << VAR(custom_recognition_param);
        return kMaaFalse;
    }

    std::vector<MapTrackerCompatibleCondition> conditions;
    if (!try_parse_maptracker_expected_conditions(param, conditions)) {
        LogError << "MapNavigatorAssertLocationCompatible invalid expected conditions.";
        return kMaaFalse;
    }

    for (auto& condition : conditions) {
        if (!convert_maptracker_condition(condition)) {
            return kMaaFalse;
        }

        std::string locate_param;
        if (!try_make_locate_assert_param(condition, param.threshold_, locate_param)) {
            return kMaaFalse;
        }

        MaaRect condition_box {};
        ScopedStringBuffer condition_detail;
        if (condition_detail.Get() == nullptr) {
            return kMaaFalse;
        }
        const MaaBool matched = maplocator::MapLocateAssertLocationRun(
            context,
            task_id,
            node_name,
            "MapLocateAssertLocation",
            locate_param.c_str(),
            image,
            roi,
            nullptr,
            &condition_box,
            condition_detail.Get());

        if (matched) {
            if (out_box != nullptr) {
                if (!try_make_assert_box(condition.target_, out_box)) {
                    *out_box = condition_box;
                }
            }
            if (out_detail != nullptr && !MaaStringBufferIsEmpty(condition_detail.Get())) {
                MaaStringBufferSet(out_detail, MaaStringBufferGet(condition_detail.Get()));
            }
            LogInfo << "MapNavigatorAssertLocationCompatible matched." << VAR(condition.map_name_);
            return kMaaTrue;
        }
    }

    LogInfo << "MapNavigatorAssertLocationCompatible miss." << VAR(conditions.size());
    return kMaaFalse;
}

} // namespace mapnavigator
