#include "GridTracker.h"

#include <algorithm>
#include <cmath>

namespace gridtracker
{
namespace
{

constexpr int kTile = recogrid::kAlignmentTileSize;
constexpr int kRegistrationRadius = 3;
constexpr double kMinIconCorrelation = 0.85;
constexpr double kSeparationRatio = 0.75;

cv::Mat icon_feature(const cv::Mat& tiles, int row, int col, cv::Point shift)
{
    // Center of the icon, clear of selection brackets, rarity strip and marks.
    const cv::Rect core(col * kTile + 11 + shift.x, row * kTile + 11 + shift.y, 22, 22);
    cv::Mat small;
    cv::resize(tiles(core), small, { 16, 16 }, 0, 0, cv::INTER_AREA);
    cv::Mat feature;
    small.reshape(1, 1).convertTo(feature, CV_32F);
    feature -= cv::mean(feature)[0];
    cv::normalize(feature, feature);
    return feature;
}

struct Candidate
{
    Alignment alignment;
    double error = 2.0;
    double shifted_column_error = 0;
};

Candidate compare_overlap(const recogrid::GridFrame& previous, const recogrid::GridFrame& current, int offset)
{
    Candidate candidate;
    auto& alignment = candidate.alignment;
    alignment.row_offset = offset;
    const int old_start = std::max(0, offset);
    const int new_start = std::max(0, -offset);
    const int rows = std::min(previous.rows - old_start, current.rows - new_start);
    const int cols = current.cols;
    const auto old_tiles = previous.alignment_tiles.rowRange(old_start * kTile, (old_start + rows) * kTile);
    const auto new_tiles = current.alignment_tiles.rowRange(new_start * kTile, (new_start + rows) * kTile);
    cv::Mat old_gray, new_gray, scores;
    cv::cvtColor(old_tiles, old_gray, cv::COLOR_BGR2GRAY);
    cv::cvtColor(new_tiles, new_gray, cv::COLOR_BGR2GRAY);
    const cv::Rect interior(
        kRegistrationRadius,
        kRegistrationRadius,
        new_gray.cols - 2 * kRegistrationRadius,
        kTile - 2 * kRegistrationRadius);

    double correlation = 0, shifted_correlation = 0;
    for (int row = 0; row < rows; ++row) {
        // Row positions come from separate card contours. Register each row at
        // its own position, including the row touching the viewport boundary.
        const cv::Range band(row * kTile, (row + 1) * kTile);
        const cv::Mat current_row = new_gray.rowRange(band);
        cv::matchTemplate(old_gray.rowRange(band), current_row(interior), scores, cv::TM_CCOEFF_NORMED);
        cv::Point peak;
        cv::minMaxLoc(scores, nullptr, nullptr, nullptr, &peak);
        const cv::Point shift = peak - interior.tl();
        std::vector<cv::Mat> old_icons;
        old_icons.reserve(cols);
        for (int col = 0; col < cols; ++col) {
            old_icons.push_back(icon_feature(old_tiles, row, col, shift));
        }
        const int before = alignment.compared_cells;
        for (int col = 0; col < cols; ++col) {
            const bool occupied = current.cells[(new_start + row) * cols + col].occupied;
            if (previous.cells[(old_start + row) * cols + col].occupied != occupied) {
                return {};
            }
            if (!occupied) {
                continue;
            }
            const auto icon = icon_feature(new_tiles, row, col, {});
            const double similarity = old_icons[col].dot(icon);
            correlation += similarity;
            shifted_correlation += old_icons[(col + cols - 1) % cols].dot(icon);
            ++alignment.compared_cells;
            if (similarity >= kMinIconCorrelation) {
                ++alignment.matched_cells;
            }
        }
        if (before == alignment.compared_cells) {
            return {};
        }
        ++alignment.support_rows;
    }
    alignment.match_ratio = static_cast<double>(alignment.matched_cells) / alignment.compared_cells;
    alignment.score = correlation / alignment.compared_cells;
    candidate.error = std::max(0.0, 1.0 - alignment.score);
    candidate.shifted_column_error = std::max(0.0, 1.0 - shifted_correlation / alignment.compared_cells);
    alignment.average_distance = candidate.error;
    return candidate;
}

std::vector<Cell> place_cells(const recogrid::GridFrame& frame, int viewport_start_row)
{
    std::vector<Cell> cells;
    for (const recogrid::GridCell& cell : frame.cells) {
        if (!cell.occupied) {
            continue;
        }
        cells.push_back({ viewport_start_row + cell.row, cell.col, cell.index, cell.screen_rect });
    }
    return cells;
}

int count_rows(const std::set<std::pair<int, int>>& positions)
{
    return positions.empty() ? 0 : positions.rbegin()->first + 1;
}

bool matches_inventory(const recogrid::GridFrame& frame, int start_row, int count)
{
    if (static_cast<int>(frame.cells.size()) != frame.rows * frame.cols) {
        return false;
    }
    for (const auto& cell : frame.cells) {
        const int index = (start_row + cell.row) * frame.cols + cell.col;
        if (cell.occupied != (index < count)) {
            return false;
        }
    }
    return true;
}

Alignment align_rows(const recogrid::GridFrame& previous, const recogrid::GridFrame& current, int min_offset, int max_offset)
{
    if (previous.cols != current.cols || min_offset > max_offset || min_offset <= -current.rows || max_offset >= previous.rows) {
        return {}; // The measured interval includes a completely disjoint page.
    }
    for (const auto* frame : { &previous, &current }) {
        if (frame->empty() || frame->cells.size() != static_cast<std::size_t>(frame->rows * frame->cols)
            || frame->alignment_tiles.size() != cv::Size(frame->cols * kTile, frame->rows * kTile)) {
            return {};
        }
    }
    Candidate best;
    double alternative_error = 2.0;
    for (int offset = min_offset; offset <= max_offset; ++offset) {
        auto candidate = compare_overlap(previous, current, offset);
        if (!candidate.alignment.compared_cells) {
            continue;
        }
        if (candidate.error < best.error) {
            alternative_error = best.error;
            best = candidate;
        }
        else {
            alternative_error = std::min(alternative_error, candidate.error);
        }
    }
    auto& alignment = best.alignment;
    // A unique row offset measured from the scrollbar and counter can be
    // corroborated by one complete row. Ambiguous geometry needs two rows.
    const bool fixed_offset = min_offset == max_offset;
    const bool full_row = alignment.support_rows == 1 && alignment.compared_cells == current.cols;
    const bool supported = alignment.support_rows >= 2 || (fixed_offset && full_row);
    const double reference = std::min(alternative_error, best.shifted_column_error);
    alignment.reliable = alignment.row_offset >= 0 && supported && alignment.matched_cells == alignment.compared_cells
                         && (fixed_offset || best.error + 1e-6 < kSeparationRatio * reference);
    return alignment;
}

} // namespace

Result GridTracker::observe(const recogrid::GridFrame& frame, const Options& options)
{
    Result result;
    result.frame_rows = frame.rows;
    result.frame_cols = frame.cols;
    result.previous_viewport_start_row = viewport_start_row_;
    result.viewport_start_row = viewport_start_row_;
    result.tracked_cells = static_cast<int>(seen_positions_.size());
    result.tracked_rows = count_rows(seen_positions_);
    const bool initial = previous_frame_.empty();
    if (frame.empty() || options.expected_cells < 0) {
        result.reason = "grid_missing";
        result.message = frame.message;
        return result;
    }
    if (initial && (!options.at_top || std::abs(options.estimated_start_row) > options.position_tolerance_rows)) {
        result.reason = "top_unverified";
        return result;
    }
    if (!initial) {
        if (frame.cols != previous_frame_.cols) {
            result.reason = "shape_changed";
            return result;
        }
        const int total_rows = (options.expected_cells + frame.cols - 1) / frame.cols;
        const int first = std::max(0, static_cast<int>(std::ceil(options.estimated_start_row - options.position_tolerance_rows)));
        const int last =
            std::min(total_rows - frame.rows, static_cast<int>(std::floor(options.estimated_start_row + options.position_tolerance_rows)));
        result.alignment = align_rows(previous_frame_, frame, first - viewport_start_row_, last - viewport_start_row_);
        if (!result.alignment.reliable) {
            result.reason = "alignment_unverified";
            return result;
        }
        result.viewport_start_row += result.alignment.row_offset;
    }
    if (!matches_inventory(frame, result.viewport_start_row, options.expected_cells)) {
        result.reason = "occupancy_mismatch";
        return result;
    }

    result.visible_cells = place_cells(frame, result.viewport_start_row);
    for (const Cell& cell : result.visible_cells) {
        if (seen_positions_.emplace(cell.row, cell.col).second) {
            result.new_cells.push_back(cell);
        }
    }
    viewport_start_row_ = result.viewport_start_row;
    result.tracked_cells = static_cast<int>(seen_positions_.size());
    result.tracked_rows = count_rows(seen_positions_);
    if (options.at_bottom && result.tracked_cells == options.expected_cells) {
        result.status = Status::Complete;
        result.message = "All inventory positions accounted for at bottom";
    }
    else if (initial) {
        result.status = Status::Initial;
        result.message = "Inventory top confirmed";
    }
    else if (result.new_cells.empty()) {
        result.status = Status::Stalled;
        result.message = "No new inventory positions after swipe";
    }
    else {
        result.status = Status::Advanced;
        result.message = "Grid tracking advanced";
    }
    previous_frame_ = frame;
    return result;
}

bool GridTracker::matches_current_viewport(const recogrid::GridFrame& frame, int expected_cells) const
{
    return !frame.empty() && frame.rows == previous_frame_.rows && frame.cols == previous_frame_.cols
           && matches_inventory(frame, viewport_start_row_, expected_cells);
}

void GridTracker::reset()
{
    previous_frame_ = {};
    viewport_start_row_ = 0;
    seen_positions_.clear();
}

const char* ToString(Status status)
{
    switch (status) {
    case Status::Initial:
        return "initial";
    case Status::Advanced:
        return "advanced";
    case Status::Stalled:
        return "stalled";
    case Status::Complete:
        return "complete";
    case Status::Rejected:
        return "rejected";
    }
    return "rejected";
}

} // namespace gridtracker
