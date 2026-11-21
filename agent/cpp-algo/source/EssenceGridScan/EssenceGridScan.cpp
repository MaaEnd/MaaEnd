#include "EssenceGridScan.h"

#include "InventoryLayout.h"

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <exception>
#include <filesystem>
#include <optional>
#include <regex>
#include <set>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/ImageIo.h>
#include <MaaUtils/JsonExt.hpp>
#include <MaaUtils/Logger.h>

#include "../Common/paths.h"
#include "../GridTracker/GridTracker.h"
#include "../RecoGrid/CellFeature.h"
#include "../RecoGrid/GridGeometry.h"
#include "../RecoGrid/GridRecognizer.h"
#include "../utils.h"

namespace essencegridscan
{
namespace
{

constexpr const char* kVerifyNode = "EssenceGridVerifyComplete";
constexpr const char* kVerifyPrefixNode = "EssenceGridVerifyLockedPrefix";

struct EssenceTemplateConfig
{
    std::string thumb_discard_template_path_;
    std::vector<std::string> thumb_lock_template_paths_;

    bool operator==(const EssenceTemplateConfig&) const = default;
};

struct EssenceGridConfig
{
    std::string thumb_discard_template_path_;
    std::vector<std::string> thumb_lock_template_paths_;
    cv::Rect roi_;
    cv::Size normalized_size_;
    cv::Rect first_cell_;
    cv::Rect scrollbar_track_;
    cv::Rect preview_core_;
    cv::Rect preview_quality_;
    double row_pitch_ = 0.0;
    bool flawless_essence_ = false;
    bool pure_essence_ = false;
    bool collect_inventory_ = false;
    bool skip_thumb_lock_ = false;
    bool skip_thumb_discard_ = false;

    MEO_JSONIZATION(
        MEO_KEY("thumb_discard_template_path") thumb_discard_template_path_,
        MEO_KEY("thumb_lock_template_paths") thumb_lock_template_paths_,
        MEO_KEY("roi") roi_,
        MEO_KEY("normalized_size") normalized_size_,
        MEO_KEY("first_cell") first_cell_,
        MEO_KEY("scrollbar_track") scrollbar_track_,
        MEO_KEY("preview_core") preview_core_,
        MEO_OPT MEO_KEY("preview_quality") preview_quality_,
        MEO_KEY("row_pitch") row_pitch_,
        MEO_KEY("flawless_essence") flawless_essence_,
        MEO_KEY("pure_essence") pure_essence_,
        MEO_OPT MEO_KEY("collect_inventory") collect_inventory_,
        MEO_KEY("skip_thumb_lock") skip_thumb_lock_,
        MEO_KEY("skip_thumb_discard") skip_thumb_discard_)
};

struct EssenceCell
{
    gridtracker::Cell grid_;
    std::string quality_ = "unknown";
    std::string thumb_state_ = "none";
    bool boundary_candidate_ = false;
};

struct HsvRange
{
    int hue_min_ = 0;
    int hue_max_ = 0;
    int saturation_min_ = 0;
    int value_min_ = 0;
};

constexpr HsvRange kFlawlessGoldHsvRange { 16, 29, 71, 89 };
constexpr HsvRange kHighPurityPurpleHsvRange { 128, 158, 61, 71 };
constexpr int kQualitySampleHeightDivisor = 10;
constexpr int kMinQualityPixels = 80;
constexpr int kQualityDominanceRatio = 2;
constexpr int kThumbSearchWidthPercent = 20;
constexpr int kThumbSearchHeightPercent = 20;
constexpr int kMaxUnchangedDetailCoreDistance = 4; // 0..64 color distance between the same area of two frames
constexpr double kThumbLockMatchThreshold = 0.7;
constexpr double kThumbDiscardMatchThreshold = 0.75;

} // namespace

struct EssenceGridState
{
    gridtracker::GridTracker tracker_;
    MaaTaskId task_id_ = MaaInvalidId;
    std::optional<EssenceGridConfig> config_;
    std::size_t issued_cells_ = 0;
    std::optional<int> inventory_count_;
    std::set<std::pair<int, int>> resolved_;
    int recovery_at_count_ = -1;
    std::optional<int> rewind_thumb_offset_;
    std::string panel_quality_;
    recogrid::CellFeature before_core_;
    bool core_changed_ = false;
    bool failed_ = false;
    std::optional<EssenceCell> pending_cell_;
    std::optional<gridtracker::Result> last_scan_result_;
    std::vector<EssenceCell> current_page_cells_;
    std::vector<EssenceCell> current_page_queue_;
    std::size_t current_page_queue_index_ = 0;
    bool scan_required_ = true;
    cv::Mat thumb_discard_template_;
    cv::Mat thumb_equipped_template_;
    std::vector<cv::Mat> thumb_lock_templates_;
    std::optional<EssenceTemplateConfig> loaded_template_config_;
};

namespace
{

std::optional<std::filesystem::path> resolve_essence_image_path(const std::string& configured_path)
{
    if (configured_path.empty()) {
        return std::nullopt;
    }

    const std::filesystem::path configured = MAA_NS::path(configured_path);
    const std::filesystem::path resolved = common::InstallPath(configured);
    std::error_code ec;
    if (std::filesystem::exists(resolved, ec) && std::filesystem::is_regular_file(resolved, ec)) {
        return resolved;
    }
    return std::nullopt;
}

bool ensure_loaded(EssenceGridState& state, const EssenceTemplateConfig& config)
{
    if (state.loaded_template_config_ && *state.loaded_template_config_ == config) {
        return true;
    }

    const auto discard_path = resolve_essence_image_path(config.thumb_discard_template_path_);
    if (!discard_path) {
        LogError << "EssenceGrid template not found" << VAR(config.thumb_discard_template_path_);
        return false;
    }
    cv::Mat thumb_discard_template = MAA_NS::imread(*discard_path, cv::IMREAD_UNCHANGED);
    auto thumb_equipped_template = MAA_NS::imread(discard_path->parent_path() / "ThumbEquipped.png", cv::IMREAD_UNCHANGED);
    std::vector<cv::Mat> thumb_lock_templates;
    thumb_lock_templates.reserve(config.thumb_lock_template_paths_.size());
    for (const std::string& template_path : config.thumb_lock_template_paths_) {
        const auto path = resolve_essence_image_path(template_path);
        if (!path) {
            LogError << "EssenceGrid template not found" << VAR(template_path);
            return false;
        }
        thumb_lock_templates.emplace_back(MAA_NS::imread(*path, cv::IMREAD_UNCHANGED));
    }
    const bool lock_template_missing =
        thumb_lock_templates.empty()
        || std::any_of(thumb_lock_templates.begin(), thumb_lock_templates.end(), [](const cv::Mat& templ) { return templ.empty(); });
    if (thumb_discard_template.empty() || thumb_equipped_template.empty() || lock_template_missing) {
        LogError << "EssenceGrid templates could not be decoded";
        return false;
    }

    state.thumb_discard_template_ = std::move(thumb_discard_template);
    state.thumb_equipped_template_ = std::move(thumb_equipped_template);
    state.thumb_lock_templates_ = std::move(thumb_lock_templates);
    state.loaded_template_config_ = config;
    state.task_id_ = MaaInvalidId;
    LogInfo << "EssenceGridScan templates loaded" << VAR(config.thumb_discard_template_path_) << VAR(config.thumb_lock_template_paths_);
    return true;
}

std::optional<EssenceGridConfig> read_essence_grid_config(MaaContext* context, const char* node_name)
{
    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, node_name, buffer.Get())) {
        LogError << "EssenceGrid node data unavailable" << VAR(node_name);
        return std::nullopt;
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || *raw == '\0') {
        LogError << "EssenceGrid node data is empty" << VAR(node_name);
        return std::nullopt;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogError << "EssenceGrid node data is invalid" << VAR(node_name);
        return std::nullopt;
    }

    const json::object& node = parsed->as_object();
    if (!node.contains("attach") || !node.at("attach").is_object()) {
        LogError << "EssenceGrid node attach is missing" << VAR(node_name);
        return std::nullopt;
    }

    EssenceGridConfig config;
    if (!config.from_json(node.at("attach"))) {
        LogError << "EssenceGrid node attach is invalid" << VAR(node_name);
        return std::nullopt;
    }
    return config;
}

recogrid::GridRecognitionOptions make_recognition_options(const EssenceGridConfig& config)
{
    recogrid::GridRecognitionOptions options;
    options.region_.roi_ = config.roi_;
    options.region_.normalized_size_ = config.normalized_size_;
    return options;
}

EssenceTemplateConfig make_template_config(const EssenceGridConfig& config)
{
    return {
        .thumb_discard_template_path_ = config.thumb_discard_template_path_,
        .thumb_lock_template_paths_ = config.thumb_lock_template_paths_,
    };
}

void reset_tracking(EssenceGridState& state)
{
    state.issued_cells_ = 0;
    state.inventory_count_.reset();
    state.resolved_.clear();
    state.recovery_at_count_ = -1;
    state.rewind_thumb_offset_.reset();
    state.before_core_ = {};
    state.failed_ = false;
    state.pending_cell_.reset();
    state.last_scan_result_.reset();
    state.current_page_cells_.clear();
    state.current_page_queue_.clear();
    state.current_page_queue_index_ = 0;
    state.scan_required_ = true;
    state.tracker_.reset();
}

void reset_session_for_new_task(EssenceGridState& state, MaaTaskId task_id)
{
    if (task_id == state.task_id_) {
        return;
    }
    reset_tracking(state);
    state.task_id_ = task_id;
    LogInfo << "EssenceGridScan reset session" << VAR(task_id);
}

bool configure_for_task(EssenceGridState& state, MaaContext* context, MaaTaskId task_id, const char* node_name)
{
    if (state.task_id_ == task_id && state.config_) {
        return true;
    }

    auto config = read_essence_grid_config(context, node_name);
    if (!config || !ensure_loaded(state, make_template_config(*config))) {
        return false;
    }

    reset_session_for_new_task(state, task_id);
    state.config_ = std::move(*config);
    return true;
}

json::object to_json_rect(const cv::Rect& rect)
{
    json::object output;
    output["x"] = rect.x;
    output["y"] = rect.y;
    output["width"] = rect.width;
    output["height"] = rect.height;
    return output;
}

struct QualityStats
{
    std::string quality_ = "unknown";
    int gold_pixels_ = 0;
    int purple_pixels_ = 0;
};

bool is_in_hsv_range(const cv::Vec3b& hsv, const HsvRange& range)
{
    return hsv[0] >= range.hue_min_ && hsv[0] <= range.hue_max_ && hsv[1] >= range.saturation_min_ && hsv[2] >= range.value_min_;
}

bool is_gold_pixel(const cv::Vec3b& hsv)
{
    return is_in_hsv_range(hsv, kFlawlessGoldHsvRange);
}

bool is_purple_pixel(const cv::Vec3b& hsv)
{
    return is_in_hsv_range(hsv, kHighPurityPurpleHsvRange);
}

QualityStats classify_quality(const cv::Mat& sample)
{
    QualityStats stats;
    cv::Mat bgr;
    if (sample.channels() == 4) {
        cv::cvtColor(sample, bgr, cv::COLOR_BGRA2BGR);
    }
    else if (sample.channels() == 3) {
        bgr = sample;
    }
    else if (sample.channels() == 1) {
        cv::cvtColor(sample, bgr, cv::COLOR_GRAY2BGR);
    }
    else {
        return stats;
    }

    cv::Mat hsv;
    cv::cvtColor(bgr, hsv, cv::COLOR_BGR2HSV);
    for (int row = 0; row < hsv.rows; ++row) {
        for (int col = 0; col < hsv.cols; ++col) {
            const cv::Vec3b pixel = hsv.at<cv::Vec3b>(row, col);
            if (is_gold_pixel(pixel)) {
                stats.gold_pixels_++;
            }
            if (is_purple_pixel(pixel)) {
                stats.purple_pixels_++;
            }
        }
    }

    if (stats.gold_pixels_ >= kMinQualityPixels && stats.gold_pixels_ >= stats.purple_pixels_ * kQualityDominanceRatio) {
        stats.quality_ = "flawless_gold";
    }
    else if (stats.purple_pixels_ >= kMinQualityPixels && stats.purple_pixels_ >= stats.gold_pixels_ * kQualityDominanceRatio) {
        stats.quality_ = "high_purity_purple";
    }
    return stats;
}

QualityStats classify_cell_quality(const cv::Mat& image, const cv::Rect& screen_cell, int half_row_gap)
{
    const cv::Rect image_bounds(0, 0, image.cols, image.rows);
    const cv::Rect cell = screen_cell & image_bounds;
    if (cell.empty()) {
        return {};
    }

    const int sample_height = std::max(1, cell.height / kQualitySampleHeightDivisor);
    // A selected shell can draw its rarity stripe below the nominal cell.
    // Stop halfway across the row gap to keep the next card out of the sample.
    const cv::Rect sample_rect = cv::Rect(cell.x, cell.br().y - sample_height, cell.width, sample_height + half_row_gap) & image_bounds;
    return classify_quality(image(sample_rect));
}

cv::Mat to_gray_for_template(const cv::Mat& image)
{
    if (image.empty()) {
        return {};
    }
    if (image.channels() == 1) {
        return image;
    }

    cv::Mat gray;
    if (image.channels() == 4) {
        cv::cvtColor(image, gray, cv::COLOR_BGRA2GRAY);
    }
    else if (image.channels() == 3) {
        cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
    }
    return gray;
}

double match_template_score(const cv::Mat& search, const cv::Mat& templ)
{
    if (search.empty() || templ.empty() || search.cols < templ.cols || search.rows < templ.rows) {
        return {};
    }

    const cv::Mat search_gray = to_gray_for_template(search);
    const cv::Mat template_gray = to_gray_for_template(templ);
    if (search_gray.empty() || template_gray.empty()) {
        return {};
    }

    cv::Mat result;
    cv::matchTemplate(search_gray, template_gray, result, cv::TM_CCOEFF_NORMED);

    double score = 0.0;
    cv::minMaxLoc(result, nullptr, &score);
    return std::isfinite(score) ? score : 0.0;
}

std::string detect_cell_thumb_state(const EssenceGridState& state, const cv::Mat& image, const cv::Rect& screen_cell)
{
    const cv::Rect image_bounds(0, 0, image.cols, image.rows);
    const cv::Rect cell = screen_cell & image_bounds;
    if (cell.empty()) {
        return "none";
    }

    const int search_width = std::max(1, cell.width * kThumbSearchWidthPercent / 100);
    const int search_height = std::max(1, cell.height * kThumbSearchHeightPercent / 100);
    const cv::Rect search_rect(cell.x, cell.y + cell.height - search_height, search_width, search_height);
    const cv::Mat search = image(search_rect & image_bounds);

    double lock_score = 0.0;
    for (const cv::Mat& lock_template : state.thumb_lock_templates_) {
        lock_score = std::max(lock_score, match_template_score(search, lock_template));
    }
    if (match_template_score(search, state.thumb_discard_template_) >= kThumbDiscardMatchThreshold) {
        return "discard";
    }
    if (lock_score >= kThumbLockMatchThreshold) {
        return "lock";
    }
    return "none";
}

bool should_dispatch_quality(const EssenceCell& cell, const EssenceGridConfig& config)
{
    if (cell.quality_ == "flawless_gold") {
        return config.flawless_essence_;
    }
    if (cell.quality_ == "high_purity_purple") {
        return config.pure_essence_;
    }
    return false;
}

bool should_dispatch_thumb_state(const EssenceCell& cell, const EssenceGridConfig& config)
{
    if (cell.thumb_state_ == "lock") {
        return !config.skip_thumb_lock_;
    }
    if (cell.thumb_state_ == "discard") {
        return !config.skip_thumb_discard_;
    }
    return true;
}

void write_error(MaaStringBuffer* out_detail, const char* message)
{
    if (out_detail == nullptr) {
        return;
    }

    json::object detail;
    detail["success"] = false;
    detail["message"] = message == nullptr ? "" : message;
    const std::string text = json::value(std::move(detail)).dumps();
    MaaStringBufferSet(out_detail, text.c_str());
}

void write_advance_detail(
    MaaStringBuffer* out_detail,
    const EssenceGridState& state,
    const gridtracker::Result& result,
    const std::optional<EssenceCell>& selected,
    const EssenceGridConfig& config,
    bool verified_end = false)
{
    if (out_detail == nullptr) {
        return;
    }

    const int remaining_queue_cells = static_cast<int>(state.current_page_queue_.size() - state.current_page_queue_index_);
    const int visible_candidates = remaining_queue_cells + (selected ? 1 : 0);

    json::object detail;
    const bool has_progress = result.status == gridtracker::Status::Initial || result.status == gridtracker::Status::Advanced;
    const int committed_row_offset = result.status == gridtracker::Status::Advanced ? result.alignment.row_offset : 0;

    detail["success"] = result.accepted();
    detail["message"] = result.message;
    detail["page_grid"] = static_cast<int>(result.visible_cells.size());
    detail["cumulative_grid"] = result.tracked_cells;
    detail["rows"] = result.tracked_rows;
    detail["cols"] = result.frame_cols;
    detail["detected_rows"] = result.frame_rows;
    detail["detected_cols"] = result.frame_cols;
    detail["detected_grid"] = result.frame_rows * result.frame_cols;
    detail["visible_candidates"] = visible_candidates;
    detail["expected_cells"] = state.inventory_count_.value_or(-1);
    detail["resolved_cells"] = static_cast<int>(state.resolved_.size());
    detail["issued_cells"] = static_cast<int>(state.issued_cells_);
    detail["queue_remaining"] = remaining_queue_cells;
    detail["scan_required"] = state.scan_required_;
    detail["scan_failed"] = state.failed_;
    detail["filter_flawless_essence"] = config.flawless_essence_;
    detail["filter_pure_essence"] = config.pure_essence_;
    detail["skip_thumb_lock"] = config.skip_thumb_lock_;
    detail["skip_thumb_discard"] = config.skip_thumb_discard_;
    detail["filter_explicit"] = true;
    detail["selected_cell_index"] = selected ? static_cast<int>(selected->grid_.frame_index) : -1;
    detail["selected_row"] = selected ? selected->grid_.row : -1;
    detail["selected_col"] = selected ? selected->grid_.col : -1;
    detail["selected_for_filter"] =
        selected && should_dispatch_quality(*selected, config) && should_dispatch_thumb_state(*selected, config);
    detail["selected_quality"] = selected ? selected->quality_ : "unknown";
    detail["selected_thumb_state"] = selected ? selected->thumb_state_ : "none";
    detail["boundary_candidate"] = selected && selected->boundary_candidate_;
    detail["selected_box"] = selected ? to_json_rect(selected->grid_.screen_rect) : json::object {};
    detail["reached_end"] = verified_end;
    detail["has_progress"] = has_progress;
    detail["row_offset"] = committed_row_offset;
    detail["raw_alignment_offset"] = result.alignment.row_offset;
    detail["support_rows"] = result.alignment.support_rows;
    detail["tracking_status"] = gridtracker::ToString(result.status);
    detail["delta_reliable"] = result.alignment.reliable;
    detail["matched_cells"] = result.alignment.matched_cells;
    detail["compared_cells"] = result.alignment.compared_cells;
    detail["average_distance"] = result.alignment.average_distance;
    detail["delta_score"] = result.alignment.score;
    detail["match_ratio"] = result.alignment.match_ratio;
    detail["previous_viewport_start_row"] = result.previous_viewport_start_row;
    detail["current_viewport_start_row"] = result.viewport_start_row;
    detail["unresolved_reason"] = result.reason;

    json::object quality_counts;
    quality_counts["flawless_gold"] = 0;
    quality_counts["high_purity_purple"] = 0;
    quality_counts["unknown"] = 0;
    for (const EssenceCell& cell : state.current_page_queue_) {
        const std::string& quality = cell.quality_;
        if (quality_counts.contains(quality) && quality_counts[quality].is_number()) {
            quality_counts[quality] = quality_counts[quality].as_integer() + 1;
        }
        else {
            quality_counts["unknown"] = quality_counts["unknown"].as_integer() + 1;
        }
    }
    detail["quality_counts"] = std::move(quality_counts);

    json::object thumb_counts;
    thumb_counts["lock"] = 0;
    thumb_counts["discard"] = 0;
    thumb_counts["none"] = 0;
    for (const EssenceCell& cell : state.current_page_cells_) {
        const std::string& thumb_state = cell.thumb_state_;
        if (thumb_counts.contains(thumb_state) && thumb_counts[thumb_state].is_number()) {
            thumb_counts[thumb_state] = thumb_counts[thumb_state].as_integer() + 1;
        }
        else {
            thumb_counts["none"] = thumb_counts["none"].as_integer() + 1;
        }
    }
    detail["thumb_counts"] = std::move(thumb_counts);

    const std::string text = json::value(std::move(detail)).dumps();
    MaaStringBufferSet(out_detail, text.c_str());
}

void write_pending_detail(MaaStringBuffer* out_detail, const EssenceGridState& state, bool success, const char* message)
{
    if (out_detail == nullptr) {
        return;
    }

    json::object detail;
    detail["success"] = success;
    detail["message"] = message == nullptr ? "" : message;
    detail["core_changed"] = state.core_changed_;
    detail["panel_quality"] = state.panel_quality_;
    detail["selected_cell_index"] = state.pending_cell_ ? static_cast<int>(state.pending_cell_->grid_.frame_index) : -1;
    detail["selected_quality"] = state.pending_cell_ ? state.pending_cell_->quality_ : "unknown";
    detail["selected_box"] = state.pending_cell_ ? to_json_rect(state.pending_cell_->grid_.screen_rect) : json::object {};
    const std::string text = json::value(std::move(detail)).dumps();
    MaaStringBufferSet(out_detail, text.c_str());
}

std::optional<int> read_inventory_count(MaaContext* context, const MaaImageBuffer* image)
{
    // Weapon and quest pages also show a valid quantity/capacity counter.
    auto* tasker = MaaContextGetTasker(context);
    const auto tab = MaaContextRunRecognition(context, "EssenceFilterInitNormalize", "{}", image);
    MaaBool hit = false;
    if (tab == MaaInvalidId || !MaaTaskerGetRecognitionDetail(tasker, tab, nullptr, nullptr, &hit, nullptr, nullptr, nullptr, nullptr)
        || !hit) {
        return std::nullopt;
    }
    const auto id = MaaContextRunRecognition(context, "EssenceGridInventoryCount", "{}", image);
    ScopedStringBuffer detail;
    if (id == MaaInvalidId || !detail.Get()
        || !MaaTaskerGetRecognitionDetail(tasker, id, nullptr, nullptr, &hit, nullptr, detail.Get(), nullptr, nullptr) || !hit) {
        return std::nullopt;
    }
    const auto parsed = json::parse(MaaStringBufferGet(detail.Get()));
    if (!parsed) {
        return std::nullopt;
    }
    const auto text = parsed->get("best", "text", std::string {});
    static const std::regex counter(R"((?:^|[^0-9])([0-9]{1,5})\s*/\s*([0-9]{1,5})\s*$)");
    std::smatch match;
    if (!std::regex_search(text, match, counter)) {
        return std::nullopt;
    }
    const int count = std::stoi(match[1]);
    const int capacity = std::stoi(match[2]);
    return capacity > 0 && count <= capacity ? std::optional<int>(count) : std::nullopt;
}

gridtracker::Result scan_grid(EssenceGridState& state, const cv::Mat& image, const EssenceGridConfig& config)
{
    const InventoryLayout layout { config.first_cell_, config.scrollbar_track_, config.row_pitch_ };
    const auto page = ReadInventoryPage(image, make_recognition_options(config), layout, *state.inventory_count_);
    gridtracker::Result result;
    if (*state.inventory_count_ == 0 && page.empty_inventory) {
        result.status = gridtracker::Status::Complete;
        result.message = "Empty inventory confirmed";
        return result;
    }
    if (!page.has_scrollbar && !(page.at_top && page.at_bottom)) {
        result.reason = "scrollbar_missing";
        return result;
    }
    if (!page.at_top && page.has_scrollbar && !page.frame.empty() && !state.tracker_.has_viewport()) {
        // The inventory can reopen at the previous scroll position. Rewind while
        // the thumb actually moves upwards, before assigning any global indices.
        if (!state.rewind_thumb_offset_ || page.thumb_offset < *state.rewind_thumb_offset_) {
            state.rewind_thumb_offset_ = page.thumb_offset;
            result.reason = "rewind_to_top";
        }
        else {
            result.reason = "top_unverified";
        }
        return result;
    }
    gridtracker::Options tracking;
    tracking.expected_cells = *state.inventory_count_;
    tracking.at_top = page.at_top;
    tracking.at_bottom = page.at_bottom;
    tracking.estimated_start_row = page.estimated_start_row;
    tracking.position_tolerance_rows = page.position_tolerance_rows;
    return state.tracker_.observe(page.frame, tracking);
}

// Compare the large detail icon at the same screen position across clicks.
recogrid::CellFeature panel_core(const EssenceGridConfig& config, const cv::Mat& image)
{
    const auto core = recogrid::ScaleRect(config.preview_core_, config.normalized_size_, image.size());
    return recogrid::ComputeCellFeature(image(core));
}

// The panel stripe can distinguish successive reads. A missing stripe is
// simply unavailable as a comparison signal.
std::string panel_quality(const EssenceGridConfig& config, const cv::Mat& image)
{
    const auto stripe = recogrid::ScaleRect(config.preview_quality_, config.normalized_size_, image.size()) & cv::Rect({}, image.size());
    return stripe.empty() ? "unknown" : classify_quality(image(stripe)).quality_;
}

void rebuild_current_page_queue(
    EssenceGridState& state,
    const cv::Mat& image,
    const gridtracker::Result& result,
    const EssenceGridConfig& config)
{
    state.current_page_cells_.clear();
    state.current_page_queue_.clear();
    state.current_page_queue_index_ = 0;
    const double row_gap = (config.row_pitch_ - config.first_cell_.height) * image.rows / config.normalized_size_.height;
    const int half_row_gap = std::max(0, cvRound(row_gap / 2));
    std::size_t last_protected = 0;
    bool saw_protected = false;
    for (const gridtracker::Cell& cell : result.new_cells) {
        EssenceCell essence_cell {
            .grid_ = cell,
            .quality_ = classify_cell_quality(image, cell.screen_rect, half_row_gap).quality_,
            .thumb_state_ = detect_cell_thumb_state(state, image, cell.screen_rect),
        };
        state.current_page_cells_.push_back(essence_cell);
        if (config.collect_inventory_) {
            if (essence_cell.thumb_state_ != "lock") {
                const auto& box = cell.screen_rect;
                const cv::Rect badge(box.x + box.width * 2 / 3, box.y, box.width - box.width * 2 / 3, box.height / 3);
                const bool equipped = match_template_score(image(badge), state.thumb_equipped_template_) >= kThumbLockMatchThreshold;
                if (equipped) {
                    last_protected = cell.frame_index;
                    saw_protected = true;
                }
                if (!equipped) {
                    essence_cell.boundary_candidate_ = true;
                    state.current_page_queue_.push_back(std::move(essence_cell));
                }
                else {
                    state.resolved_.emplace(cell.row, cell.col);
                }
                continue;
            }
            last_protected = cell.frame_index;
            saw_protected = true;
            if (essence_cell.quality_ == "high_purity_purple") {
                state.resolved_.emplace(cell.row, cell.col);
                continue;
            }
        }
        if (!config.collect_inventory_ && !should_dispatch_thumb_state(essence_cell, config)) {
            state.resolved_.emplace(cell.row, cell.col);
            continue;
        }
        if (essence_cell.quality_ == "unknown") {
            LogWarn << "Unresolved inventory quality" << VAR(cell.row) << VAR(cell.col);
            state.resolved_.emplace(cell.row, cell.col);
            continue;
        }
        if (config.collect_inventory_ || should_dispatch_quality(essence_cell, config)) {
            state.current_page_queue_.push_back(std::move(essence_cell));
        }
        else {
            state.resolved_.emplace(cell.row, cell.col);
        }
    }
    // A later visible lock or sword disproves an earlier boundary on this page.
    if (saw_protected) {
        for (auto& cell : state.current_page_queue_) {
            if (cell.grid_.frame_index < last_protected) {
                cell.boundary_candidate_ = false;
            }
        }
    }
}

std::optional<EssenceCell> select_next_queued_cell(EssenceGridState& state)
{
    if (state.current_page_queue_index_ < state.current_page_queue_.size()) {
        return state.current_page_queue_[state.current_page_queue_index_++];
    }
    return std::nullopt;
}

} // namespace

EssenceGrid::EssenceGrid()
    : state_(std::make_unique<EssenceGridState>())
{
}

EssenceGrid::~EssenceGrid() = default;

MaaBool MAA_CALL EssenceGrid::advanceRecognitionRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    [[maybe_unused]] const char* custom_recognition_param,
    const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi,
    void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    return static_cast<EssenceGrid*>(trans_arg)->advance(context, task_id, node_name, image, out_box, out_detail);
}

MaaBool EssenceGrid::advance(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const MaaImageBuffer* image,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    if (image == nullptr || MaaImageBufferIsEmpty(image)) {
        write_error(out_detail, "Image buffer is empty");
        return 0;
    }

    try {
        EssenceGridState& state = *state_;
        if (!configure_for_task(state, context, task_id, node_name)) {
            write_error(out_detail, "EssenceGrid configuration is unavailable");
            return 0;
        }
        if (state.failed_) {
            write_error(out_detail, "Inventory scan has failed");
            return 0;
        }
        const EssenceGridConfig& config = *state.config_;
        const cv::Mat scanned_image = to_mat(image);
        if (std::string_view(node_name) == kVerifyPrefixNode) {
            const auto count = read_inventory_count(context, image);
            if (!config.collect_inventory_ || !state.pending_cell_ || !state.pending_cell_->boundary_candidate_ || !count
                || count != state.inventory_count_) {
                write_error(out_detail, "Locked inventory boundary changed");
                return 0;
            }
            const auto& boundary = state.pending_cell_->grid_;
            const int cols = state.last_scan_result_->frame_cols;
            for (int i = 0; i < boundary.row * cols + boundary.col; ++i) {
                if (!state.resolved_.contains({ i / cols, i % cols })) {
                    write_error(out_detail, "Unvisited cell before locked inventory boundary");
                    return 0;
                }
            }
            write_advance_detail(out_detail, state, *state.last_scan_result_, std::nullopt, config, true);
            return 1;
        }
        if (std::string_view(node_name) == kVerifyNode) {
            const auto count = read_inventory_count(context, image);
            if (!count || count != state.inventory_count_ || static_cast<int>(state.resolved_.size()) != *count || !state.last_scan_result_
                || state.last_scan_result_->status != gridtracker::Status::Complete) {
                write_error(out_detail, "Final inventory count or coverage changed");
                return 0;
            }
            const auto page = ReadInventoryPage(
                scanned_image,
                make_recognition_options(config),
                { config.first_cell_, config.scrollbar_track_, config.row_pitch_ },
                *count);
            if (!(*count == 0 ? page.empty_inventory : page.at_bottom && state.tracker_.matches_current_viewport(page.frame, *count))) {
                write_error(out_detail, "Final inventory viewport unverified");
                return 0;
            }
            write_advance_detail(out_detail, state, *state.last_scan_result_, std::nullopt, config, true);
            return 1;
        }
        gridtracker::Result result = state.last_scan_result_.value_or(gridtracker::Result {});
        if (state.pending_cell_) {
            const auto& cell = state.pending_cell_->grid_;
            state.resolved_.emplace(cell.row, cell.col);
            state.pending_cell_.reset();
        }
        if (state.scan_required_) {
            const auto count = read_inventory_count(context, image);
            if (!count || (state.inventory_count_ && *count != *state.inventory_count_)) {
                state.failed_ = count.has_value();
                write_error(out_detail, "Inventory counter missing or changed");
                return 0;
            }
            state.inventory_count_ = count;
            result = scan_grid(state, scanned_image, config);
            state.last_scan_result_ = result;
            if (!result.accepted()) {
                if (result.reason == "alignment_unverified" && state.recovery_at_count_ != result.tracked_cells) {
                    state.recovery_at_count_ = result.tracked_cells;
                }
                else if (result.reason != "rewind_to_top") {
                    LogWarn << "Inventory coverage unverified" << VAR(result.reason) << VAR(result.tracked_cells) << VAR(*count);
                    write_advance_detail(out_detail, state, result, std::nullopt, config);
                    return 0;
                }
            }
            else {
                rebuild_current_page_queue(state, scanned_image, result, config);
                state.scan_required_ = false;
            }
        }
        std::optional<EssenceCell> selected;
        if (result.accepted()) {
            selected = select_next_queued_cell(state);
        }
        if (result.accepted() && !selected) {
            state.scan_required_ = true;
            if (result.status == gridtracker::Status::Complete) {
                state.failed_ = static_cast<int>(state.resolved_.size()) != *state.inventory_count_;
            }
            else if (result.status == gridtracker::Status::Stalled) {
                state.failed_ = state.recovery_at_count_ == result.tracked_cells;
                state.recovery_at_count_ = result.tracked_cells;
                result.reason = "no_new_cells";
            }
        }

        if (selected) {
            state.pending_cell_ = selected;
            state.before_core_ = {};
            state.core_changed_ = false;
            state.panel_quality_.clear();
            ++state.issued_cells_;
            if (out_box != nullptr) {
                const cv::Rect& box = selected->grid_.screen_rect;
                *out_box = {
                    box.x,
                    box.y,
                    box.width,
                    box.height,
                };
            }
        }

        LogInfo << "EssenceGridScan advance" << VAR(state.failed_) << VAR(result.tracked_cells) << VAR(state.issued_cells_)
                << VAR(state.current_page_queue_.size()) << VAR(state.current_page_queue_index_) << VAR(state.scan_required_)
                << gridtracker::ToString(result.status) << VAR(result.alignment.row_offset) << VAR(result.alignment.support_rows)
                << VAR(result.alignment.match_ratio);
        write_advance_detail(out_detail, state, result, selected, config);
        return 1;
    }
    catch (const std::exception& e) {
        state_->failed_ = true;
        write_error(out_detail, e.what());
        LogError << "EssenceGridAdvanceRecognition failed" << VAR(e.what());
        return 0;
    }
}

MaaBool MAA_CALL EssenceGrid::resetActionRun(
    [[maybe_unused]] MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    [[maybe_unused]] const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    void* trans_arg)
{
    return static_cast<EssenceGrid*>(trans_arg)->resetTracking(task_id, node_name);
}

MaaBool MAA_CALL EssenceGrid::pendingRecognitionRun(
    [[maybe_unused]] MaaContext* context,
    MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    [[maybe_unused]] const char* custom_recognition_param,
    const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi,
    void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    return static_cast<EssenceGrid*>(trans_arg)->readPending(task_id, image, out_box, out_detail, false);
}

MaaBool MAA_CALL EssenceGrid::detailRecognitionRun(
    [[maybe_unused]] MaaContext* context,
    MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    [[maybe_unused]] const char* custom_recognition_param,
    const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi,
    void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    return static_cast<EssenceGrid*>(trans_arg)->readPending(task_id, image, out_box, out_detail, true);
}

MaaBool EssenceGrid::readPending(
    MaaTaskId task_id,
    const MaaImageBuffer* image,
    MaaRect* out_box,
    MaaStringBuffer* out_detail,
    bool compare_detail)
{
    auto& state = *state_;
    if (state.failed_ || state.task_id_ != task_id || !state.pending_cell_ || !image || MaaImageBufferIsEmpty(image)) {
        write_error(out_detail, "No pending inventory cell for this task");
        return 0;
    }
    try {
        const auto frame = to_mat(image);
        if (compare_detail) {
            state.panel_quality_ = panel_quality(*state.config_, frame);
            state.core_changed_ =
                recogrid::FeatureDistance(state.before_core_, panel_core(*state.config_, frame)) > kMaxUnchangedDetailCoreDistance;
        }
        else if (state.before_core_.data.empty()) {
            state.before_core_ = panel_core(*state.config_, frame);
        }
        const auto& box = state.pending_cell_->grid_.screen_rect;
        if (out_box) {
            *out_box = compare_detail ? MaaRect { box.x, box.y, box.width, box.height }
                                      : MaaRect { box.x + box.width / 2, box.y + box.height / 2, 1, 1 };
        }
        write_pending_detail(out_detail, state, true, "Pending Essence grid cell");
        LogInfo << "EssenceGridScan pending" << VAR(compare_detail) << VAR(state.pending_cell_->grid_.frame_index) << VAR(box.x)
                << VAR(box.y) << VAR(box.width) << VAR(box.height);
        return 1;
    }
    catch (const std::exception& e) {
        state.failed_ = true;
        write_error(out_detail, e.what());
        LogError << "EssenceGrid pending recognition failed" << VAR(compare_detail) << VAR(e.what());
        return 0;
    }
}

MaaBool EssenceGrid::resetTracking(MaaTaskId task_id, const char* node_name)
{
    EssenceGridState& state = *state_;
    reset_tracking(state);
    state.task_id_ = MaaInvalidId;
    LogInfo << "EssenceGridScan tracking reset" << VAR(task_id) << VAR(node_name);
    return 1;
}

} // namespace essencegridscan
