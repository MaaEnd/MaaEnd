#include "GridTracker.h"

#include <algorithm>

namespace gridtracker
{
namespace
{

constexpr double kDoubleEpsilon = 1e-9;
constexpr int kRequiredConsecutiveCompleteRepeats = 2;

std::size_t cell_index(int row, int col, int cols)
{
    return static_cast<std::size_t>(row * cols + col);
}

int cell_distance(
    const recogrid::GridFrame& previous,
    std::size_t previousIndex,
    const recogrid::GridFrame& current,
    std::size_t currentIndex)
{
    if (previousIndex < previous.cells.size() && currentIndex < current.cells.size()) {
        const recogrid::CellFeature& lhs = previous.cells[previousIndex].feature;
        const recogrid::CellFeature& rhs = current.cells[currentIndex].feature;
        if (!lhs.data.empty() && !rhs.data.empty()) {
            return recogrid::FeatureDistance(lhs, rhs);
        }
    }
    if (previousIndex >= previous.cells.size() || currentIndex >= current.cells.size()) {
        return 64;
    }
    return recogrid::HammingDistance(previous.cells[previousIndex].hash, current.cells[currentIndex].hash);
}

bool is_better(const Alignment& candidate, const Alignment& best)
{
    if (best.compared_cells == 0) {
        return true;
    }
    if (candidate.match_ratio > best.match_ratio + kDoubleEpsilon) {
        return true;
    }
    if (candidate.match_ratio + kDoubleEpsilon < best.match_ratio) {
        return false;
    }
    if (candidate.average_distance + kDoubleEpsilon < best.average_distance) {
        return true;
    }
    if (candidate.average_distance > best.average_distance + kDoubleEpsilon) {
        return false;
    }
    return candidate.compared_cells > best.compared_cells;
}

Alignment align(const recogrid::GridFrame& previous, const recogrid::GridFrame& current, const Options& options)
{
    Alignment best;
    const int compared_cols = std::min(previous.cols, current.cols);
    const int max_offset = std::max(0, previous.rows - std::max(1, options.min_overlap_rows));

    for (int offset = 0; offset <= max_offset; ++offset) {
        Alignment candidate;
        candidate.row_offset = offset;
        for (int current_row = 0; current_row < current.rows; ++current_row) {
            const int previous_row = current_row + offset;
            if (previous_row >= previous.rows) {
                break;
            }

            bool row_compared = false;
            for (int col = 0; col < compared_cols; ++col) {
                const std::size_t previous_index = cell_index(previous_row, col, previous.cols);
                const std::size_t current_index = cell_index(current_row, col, current.cols);
                const int distance = cell_distance(previous, previous_index, current, current_index);
                candidate.total_distance += distance;
                ++candidate.compared_cells;
                row_compared = true;
                if (distance <= std::max(0, options.match_distance_threshold)) {
                    ++candidate.matched_cells;
                }
            }
            candidate.support_rows += row_compared ? 1 : 0;
        }

        if (candidate.compared_cells == 0) {
            continue;
        }
        candidate.average_distance = static_cast<double>(candidate.total_distance) / static_cast<double>(candidate.compared_cells);
        candidate.match_ratio = static_cast<double>(candidate.matched_cells) / static_cast<double>(candidate.compared_cells);
        candidate.score = candidate.match_ratio * 1000.0 - candidate.average_distance;
        if (is_better(candidate, best)) {
            best = candidate;
        }
    }

    best.reliable =
        best.support_rows >= std::max(1, options.min_overlap_rows) && best.match_ratio >= std::clamp(options.min_match_ratio, 0.0, 1.0);
    return best;
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

int count_rows(const std::map<std::pair<int, int>, Cell>& cells)
{
    int rows = 0;
    for (const auto& [key, _] : cells) {
        rows = std::max(rows, key.first + 1);
    }
    return rows;
}

bool covers_entire_frame(const recogrid::GridFrame& previous, const recogrid::GridFrame& current, const Alignment& alignment)
{
    if (previous.rows != current.rows || previous.cols != current.cols) {
        return false;
    }

    const std::size_t expected_cells = static_cast<std::size_t>(current.rows) * static_cast<std::size_t>(current.cols);
    return previous.cells.size() == expected_cells && current.cells.size() == expected_cells
           && alignment.compared_cells == static_cast<int>(expected_cells);
}

} // namespace

Result GridTracker::observe(const recogrid::GridFrame& frame, const Options& options)
{
    Result result;
    result.frame_rows = frame.rows;
    result.frame_cols = frame.cols;
    result.previous_viewport_start_row = viewport_start_row_;
    result.viewport_start_row = viewport_start_row_;

    if (frame.empty()) {
        consecutive_complete_repeats_ = 0;
        result.message = frame.message.empty() ? "Grid frame is empty" : frame.message;
        result.reason = "grid_missing";
    }
    else if (previous_frame_.cols > 0 && frame.cols != previous_frame_.cols) {
        consecutive_complete_repeats_ = 0;
        result.message = "Grid column count changed";
        result.reason = "shape_changed";
    }
    else if (previous_frame_.cols == 0) {
        consecutive_complete_repeats_ = 0;
        previous_frame_ = frame;
        row_height_ = frame.row_height;
        col_width_ = frame.col_width;
        result.status = Status::Initial;
        result.message = "Grid tracking started";
        result.visible_cells = place_cells(frame, 0);
        for (const Cell& cell : result.visible_cells) {
            cells_[{ cell.row, cell.col }] = cell;
            result.new_cells.push_back(cell);
        }
    }
    else {
        result.alignment = align(previous_frame_, frame, options);
        if (!result.alignment.reliable) {
            consecutive_complete_repeats_ = 0;
            result.message = "Grid frames could not be aligned";
            result.reason = "low_confidence";
        }
        else if (result.alignment.row_offset == 0 && covers_entire_frame(previous_frame_, frame, result.alignment)) {
            if (result.alignment.match_ratio < std::clamp(options.repeat_match_ratio, 0.0, 1.0)) {
                consecutive_complete_repeats_ = 0;
                result.message = "Repeated grid was below the required match ratio";
                result.reason = "repeat_below_threshold";
            }
            else {
                previous_frame_ = frame;
                ++consecutive_complete_repeats_;
                if (consecutive_complete_repeats_ < kRequiredConsecutiveCompleteRepeats) {
                    result.status = Status::ConfirmingEnd;
                    result.message = "Grid end awaiting confirmation";
                }
                else {
                    result.status = Status::Repeated;
                    result.message = "Grid frame repeatedly confirmed";
                }
            }
        }
        else {
            consecutive_complete_repeats_ = 0;
            result.status = Status::Advanced;
            result.message = result.alignment.row_offset == 0 ? "Grid tracking refreshed" : "Grid tracking advanced";
            viewport_start_row_ += result.alignment.row_offset;
            result.viewport_start_row = viewport_start_row_;
            result.visible_cells = place_cells(frame, viewport_start_row_);
            for (const Cell& cell : result.visible_cells) {
                const auto [iter, inserted] = cells_.insert_or_assign({ cell.row, cell.col }, cell);
                if (inserted) {
                    result.new_cells.push_back(iter->second);
                }
            }
            previous_frame_ = frame;
        }
    }

    result.tracked_cells = static_cast<int>(cells_.size());
    result.tracked_rows = count_rows(cells_);
    return result;
}

void GridTracker::reset()
{
    previous_frame_ = {};
    viewport_start_row_ = 0;
    row_height_ = 0;
    col_width_ = 0;
    consecutive_complete_repeats_ = 0;
    cells_.clear();
}

const char* ToString(Status status)
{
    switch (status) {
    case Status::Initial:
        return "initial";
    case Status::Advanced:
        return "advanced";
    case Status::ConfirmingEnd:
        return "confirming_end";
    case Status::Repeated:
        return "repeated";
    case Status::Rejected:
        return "rejected";
    }
    return "rejected";
}

} // namespace gridtracker
