#include "EssenceGridScan.h"

#include "InventoryLayout.h"
#include "InventorySelection.h"

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
#include "../RecoGrid/GridGeometry.h"
#include "../RecoGrid/GridRecognizer.h"
#include "../utils.h"

namespace essencegridscan
{
namespace
{

constexpr const char* kClickNextNode = "EssenceGridClickPending";
constexpr const char* kSwipeNextNode = "EssenceGridSwipeNext";
constexpr const char* kVerifyNode = "EssenceGridVerifyComplete";

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
    std::vector<std::pair<cv::Rect, recogrid::CellFeature>> page_icons_;
    cv::Size page_size_;
    cv::Range page_thumb_;
    bool selection_confirmed_ = false;
    int pending_clicks_ = 0;
    cv::Mat before_click_;
    std::optional<std::pair<int, int>> last_selected_;
    bool failed_ = false;
    std::optional<EssenceCell> pending_cell_;
    std::optional<gridtracker::Result> last_scan_result_;
    std::vector<EssenceCell> current_page_cells_;
    std::vector<EssenceCell> current_page_queue_;
    std::size_t current_page_queue_index_ = 0;
    bool scan_required_ = true;
    cv::Mat thumb_discard_template_;
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
    if (thumb_discard_template.empty() || lock_template_missing) {
        LogError << "EssenceGrid templates could not be decoded";
        return false;
    }

    state.thumb_discard_template_ = std::move(thumb_discard_template);
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
    options.detect.roi = config.roi_;
    options.detect.normalizedSize = config.normalized_size_;
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
    state.page_icons_.clear();
    state.page_size_ = {};
    state.page_thumb_ = {};
    state.selection_confirmed_ = false;
    state.before_click_.release();
    state.last_selected_.reset();
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

QualityStats classify_cell_quality(const cv::Mat& image, const cv::Rect& screen_cell, int half_row_gap)
{
    QualityStats stats;
    const cv::Rect image_bounds(0, 0, image.cols, image.rows);
    const cv::Rect cell = screen_cell & image_bounds;
    if (cell.empty()) {
        return stats;
    }

    const int sample_height = std::max(1, cell.height / kQualitySampleHeightDivisor);
    // A selected shell can draw its rarity stripe below the nominal cell.
    // Stop halfway across the row gap to keep the next card out of the sample.
    const cv::Rect sample_rect = cv::Rect(cell.x, cell.br().y - sample_height, cell.width, sample_height + half_row_gap) & image_bounds;
    cv::Mat sample = image(sample_rect);
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

struct ThumbMatchScore
{
    double score = 0.0;
};

struct ThumbDetection
{
    std::string state = "none";
    cv::Rect search;
    ThumbMatchScore lock;
    ThumbMatchScore discard;
};

ThumbMatchScore match_template_score(const cv::Mat& search, const cv::Mat& templ)
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

    ThumbMatchScore score;
    cv::minMaxLoc(result, nullptr, &score.score);
    if (!std::isfinite(score.score)) {
        score.score = 0.0;
    }
    return score;
}

ThumbDetection detect_cell_thumb_state(const EssenceGridState& state, const cv::Mat& image, const cv::Rect& screen_cell)
{
    ThumbDetection detection;
    const cv::Rect image_bounds(0, 0, image.cols, image.rows);
    const cv::Rect cell = screen_cell & image_bounds;
    if (cell.empty()) {
        return detection;
    }

    const int search_width = std::max(1, cell.width * kThumbSearchWidthPercent / 100);
    const int search_height = std::max(1, cell.height * kThumbSearchHeightPercent / 100);
    const cv::Rect search_rect(cell.x, cell.y + cell.height - search_height, search_width, search_height);
    detection.search = search_rect & image_bounds;
    const cv::Mat search = image(detection.search);

    for (const cv::Mat& lock_template : state.thumb_lock_templates_) {
        const ThumbMatchScore lock_score = match_template_score(search, lock_template);
        if (lock_score.score > detection.lock.score) {
            detection.lock = lock_score;
        }
    }
    detection.discard = match_template_score(search, state.thumb_discard_template_);
    if (detection.discard.score >= kThumbDiscardMatchThreshold) {
        detection.state = "discard";
        return detection;
    }
    if (detection.lock.score >= kThumbLockMatchThreshold) {
        detection.state = "lock";
        return detection;
    }
    return detection;
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
    detail["filter_flawless_essence"] = config.flawless_essence_;
    detail["filter_pure_essence"] = config.pure_essence_;
    detail["skip_thumb_lock"] = config.skip_thumb_lock_;
    detail["skip_thumb_discard"] = config.skip_thumb_discard_;
    detail["filter_explicit"] = true;
    detail["selected_cell_index"] = selected ? static_cast<int>(selected->grid_.frame_index) : -1;
    detail["selected_row"] = selected ? selected->grid_.row : -1;
    detail["selected_col"] = selected ? selected->grid_.col : -1;
    detail["selected_for_filter"] = selected && should_dispatch_quality(*selected, config) && should_dispatch_thumb_state(*selected, config);
    detail["selected_quality"] = selected ? selected->quality_ : "unknown";
    detail["selected_thumb_state"] = selected ? selected->thumb_state_ : "none";
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
    detail["selected_cell_index"] = state.pending_cell_ ? static_cast<int>(state.pending_cell_->grid_.frame_index) : -1;
    detail["selected_quality"] = state.pending_cell_ ? state.pending_cell_->quality_ : "unknown";
    detail["selected_box"] = state.pending_cell_ ? to_json_rect(state.pending_cell_->grid_.screen_rect) : json::object {};
    const std::string text = json::value(std::move(detail)).dumps();
    MaaStringBufferSet(out_detail, text.c_str());
}

bool override_next(MaaContext* context, const char* node_name, const char* next_node)
{
    if (context == nullptr || node_name == nullptr || next_node == nullptr) {
        return false;
    }

    MaaStringBuffer* item = MaaStringBufferCreate();
    MaaStringListBuffer* list = MaaStringListBufferCreate();
    if (item == nullptr || list == nullptr) {
        if (item != nullptr) {
            MaaStringBufferDestroy(item);
        }
        if (list != nullptr) {
            MaaStringListBufferDestroy(list);
        }
        return false;
    }

    const bool ok =
        MaaStringBufferSet(item, next_node) && MaaStringListBufferAppend(list, item) && MaaContextOverrideNext(context, node_name, list);
    MaaStringListBufferDestroy(list);
    MaaStringBufferDestroy(item);
    return ok;
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
    if (!page.at_top && page.has_scrollbar && !page.frame.empty() && state.page_size_.empty()) {
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

// Marks and the selection outline can change while a card is being processed.
void remember_page_icons(EssenceGridState& state, const cv::Mat& image, const gridtracker::Result& result)
{
    state.page_size_ = image.size();
    state.page_thumb_ = ReadScrollThumb(image, state.config_->normalized_size_, state.config_->scrollbar_track_);
    state.page_icons_.clear();
    for (const auto& cell : result.visible_cells) {
        const auto& box = cell.screen_rect;
        const cv::Rect center(box.x + box.width / 4, box.y + box.height / 4, box.width / 2, box.height / 2);
        state.page_icons_.emplace_back(center, recogrid::ComputeCellFeature(image(center)));
    }
}

bool same_page_items(const EssenceGridState& state, const cv::Mat& image)
{
    constexpr int kMaxStationaryIconDistance = 4; // 0..64 color distance, comparing the same screen position
    if (state.page_size_ != image.size()
        || ReadScrollThumb(image, state.config_->normalized_size_, state.config_->scrollbar_track_) != state.page_thumb_) {
        return false;
    }
    return std::all_of(state.page_icons_.begin(), state.page_icons_.end(), [&](const auto& icon) {
        return recogrid::FeatureDistance(icon.second, recogrid::ComputeCellFeature(image(icon.first))) <= kMaxStationaryIconDistance;
    });
}

bool pending_selection_matches(const EssenceGridState& state, const cv::Mat& image)
{
    const auto& cells = state.last_scan_result_->visible_cells;
    std::optional<cv::Rect> previous;
    for (const auto& cell : cells) {
        if (state.last_selected_ == std::pair(cell.row, cell.col)) {
            previous = cell.screen_rect;
            break;
        }
    }
    const auto& config = *state.config_;
    const auto preview = recogrid::ScaleRect(config.preview_core_, config.normalized_size_, image.size());
    return ConfirmInventorySelection(
        state.before_click_,
        image,
        cells,
        state.pending_cell_->grid_.screen_rect,
        preview,
        previous,
        state.inventory_count_ == 1);
}

bool rebuild_current_page_queue(
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
    for (const gridtracker::Cell& cell : result.new_cells) {
        EssenceCell essence_cell {
            .grid_ = cell,
            .quality_ = classify_cell_quality(image, cell.screen_rect, half_row_gap).quality_,
            .thumb_state_ = detect_cell_thumb_state(state, image, cell.screen_rect).state,
        };
        state.current_page_cells_.push_back(essence_cell);
        if (!config.collect_inventory_ && !should_dispatch_thumb_state(essence_cell, config)) {
            state.resolved_.emplace(cell.row, cell.col);
            continue;
        }
        if (essence_cell.quality_ == "unknown") {
            LogWarn << "Unresolved inventory quality" << VAR(cell.row) << VAR(cell.col);
            return false;
        }
        if (config.collect_inventory_ || should_dispatch_quality(essence_cell, config)) {
            state.current_page_queue_.push_back(std::move(essence_cell));
        }
        else {
            state.resolved_.emplace(cell.row, cell.col);
        }
    }
    return true;
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
        if (std::string_view(node_name) == kVerifyNode) {
            const auto count = read_inventory_count(context, image);
            if (!count || count != state.inventory_count_ || static_cast<int>(state.resolved_.size()) != *count || !state.last_scan_result_
                || state.last_scan_result_->status != gridtracker::Status::Complete || !same_page_items(state, scanned_image)) {
                write_error(out_detail, "Final inventory count or page changed");
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
        const char* next_node = nullptr;
        if (state.pending_cell_) {
            if (!state.selection_confirmed_ || !same_page_items(state, scanned_image) || !pending_selection_matches(state, scanned_image)) {
                write_error(out_detail, "Inventory moved or reordered during item processing");
                return 0;
            }
            const auto& cell = state.pending_cell_->grid_;
            state.resolved_.emplace(cell.row, cell.col);
            state.last_selected_.emplace(cell.row, cell.col);
            state.before_click_.release();
            state.pending_cell_.reset();
            state.selection_confirmed_ = false;
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
                if (result.reason == "rewind_to_top") {
                    next_node = "EssenceGridSwipeToTop";
                }
                else if (result.reason == "alignment_unverified" && state.recovery_at_count_ != result.tracked_cells) {
                    state.recovery_at_count_ = result.tracked_cells;
                    next_node = "EssenceGridSwipeBack";
                }
                else {
                    LogWarn << "Inventory coverage unverified" << VAR(result.reason) << VAR(result.tracked_cells) << VAR(*count);
                    write_advance_detail(out_detail, state, result, std::nullopt, config);
                    return 0;
                }
            }
            else {
                if (!rebuild_current_page_queue(state, scanned_image, result, config)) {
                    result.reason = "unknown_cell_quality";
                    next_node = "FailedNode";
                }
                remember_page_icons(state, scanned_image, result);
                state.scan_required_ = false;
            }
        }
        std::optional<EssenceCell> selected;
        if (!next_node) {
            selected = select_next_queued_cell(state);
        }
        if (!next_node && !selected) {
            state.scan_required_ = true;
            if (result.status == gridtracker::Status::Complete) {
                next_node = static_cast<int>(state.resolved_.size()) == *state.inventory_count_ ? kVerifyNode : "FailedNode";
            }
            else if (result.status == gridtracker::Status::Stalled) {
                next_node = state.recovery_at_count_ == result.tracked_cells ? "FailedNode" : "EssenceGridSwipeShort";
                state.recovery_at_count_ = result.tracked_cells;
                result.reason = "no_new_cells";
            }
            else {
                next_node = kSwipeNextNode;
            }
        }

        if (selected) {
            state.pending_cell_ = selected;
            state.pending_clicks_ = 0;
            ++state.issued_cells_;
            next_node = kClickNextNode;
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

        LogInfo << "EssenceGridScan advance" << VAR(next_node) << VAR(result.tracked_cells) << VAR(state.issued_cells_)
                << VAR(state.current_page_queue_.size()) << VAR(state.current_page_queue_index_) << VAR(state.scan_required_)
                << gridtracker::ToString(result.status) << VAR(result.alignment.row_offset) << VAR(result.alignment.support_rows)
                << VAR(result.alignment.match_ratio);
        state.failed_ = std::string_view(next_node) == "FailedNode";
        if (!override_next(context, node_name, next_node)) {
            state.failed_ = true;
            LogWarn << "EssenceGridScan override next failed" << VAR(next_node);
            return 0;
        }
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
    const char* node_name,
    [[maybe_unused]] const char* custom_recognition_name,
    [[maybe_unused]] const char* custom_recognition_param,
    const MaaImageBuffer* image,
    [[maybe_unused]] const MaaRect* roi,
    void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    auto* grid = static_cast<EssenceGrid*>(trans_arg);
    auto& state = *grid->state_;
    if (state.failed_ || state.task_id_ != task_id || !state.pending_cell_ || !image || MaaImageBufferIsEmpty(image)) {
        write_error(out_detail, "No pending inventory cell for this task");
        return 0;
    }
    try {
        const auto frame = to_mat(image);
        if (!same_page_items(state, frame)) {
            write_error(out_detail, "Inventory page changed before selection");
            return 0;
        }
        if (std::string_view(node_name) == "EssenceGridCheckSelection") {
            if (!pending_selection_matches(state, frame)) {
                write_error(out_detail, "Pending inventory cell not selected");
                return 0;
            }
            state.selection_confirmed_ = true;
        }
        else if (std::string_view(node_name) == kClickNextNode) {
            if (state.pending_clicks_ >= 2) {
                write_error(out_detail, "Pending inventory cell still unconfirmed after reclick");
                return 0;
            }
            if (state.pending_clicks_ == 0) {
                state.before_click_ = frame.clone();
            }
            ++state.pending_clicks_;
        }
        const auto hit = grid->pending(out_box, out_detail);
        if (hit && out_box && std::string_view(node_name) == kClickNextNode) {
            const auto& box = state.pending_cell_->grid_.screen_rect;
            *out_box = { box.x + box.width / 2, box.y + box.height / 2, 1, 1 };
        }
        return hit;
    }
    catch (const std::exception& e) {
        state.failed_ = true;
        write_error(out_detail, e.what());
        LogError << "EssenceGridPendingRecognition failed" << VAR(e.what());
        return 0;
    }
}

MaaBool EssenceGrid::pending(MaaRect* out_box, MaaStringBuffer* out_detail)
{
    const EssenceGridState& state = *state_;
    if (!state.pending_cell_) {
        write_pending_detail(out_detail, state, false, "No pending Essence grid cell");
        LogWarn << "EssenceGridPendingRecognition missing pending cell";
        return 0;
    }

    const cv::Rect& box = state.pending_cell_->grid_.screen_rect;
    if (out_box != nullptr) {
        *out_box = { box.x, box.y, box.width, box.height };
    }
    write_pending_detail(out_detail, state, true, "Pending Essence grid cell");
    LogInfo << "EssenceGridScan pending" << VAR(state.pending_cell_->grid_.frame_index) << VAR(box.x) << VAR(box.y) << VAR(box.width)
            << VAR(box.height);
    return 1;
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
