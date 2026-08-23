#pragma once

#include "../RecoGrid/GridRecognizer.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <cstddef>
#include <map>
#include <string>
#include <utility>
#include <vector>

namespace gridtracker
{

enum class Status
{
    Initial,
    Advanced,
    ConfirmingEnd,
    Repeated,
    Rejected,
};

struct Options
{
    int match_distance_threshold = 12;
    double min_match_ratio = 0.5;
    double repeat_match_ratio = 0.95;
    int min_overlap_rows = 2;
};

struct Alignment
{
    int row_offset = 0;
    int support_rows = 0;
    int compared_cells = 0;
    int matched_cells = 0;
    int total_distance = 0;
    double average_distance = 0.0;
    double score = 0.0;
    double match_ratio = 0.0;
    bool reliable = false;
};

struct Cell
{
    int row = 0;
    int col = 0;
    std::size_t frame_index = 0;
    cv::Rect screen_rect;
};

struct Result
{
    Status status = Status::Rejected;
    std::string message;
    std::string reason;
    int frame_rows = 0;
    int frame_cols = 0;
    int tracked_rows = 0;
    int tracked_cells = 0;
    int previous_viewport_start_row = 0;
    int viewport_start_row = 0;
    Alignment alignment;
    std::vector<Cell> visible_cells;
    std::vector<Cell> new_cells;

    [[nodiscard]] bool accepted() const { return status != Status::Rejected; }
};

class GridTracker
{
public:
    Result observe(const recogrid::GridFrame& frame, const Options& options = {});
    void reset();

    [[nodiscard]] int rowHeight() const { return row_height_; }
    [[nodiscard]] int colWidth() const { return col_width_; }

private:
    recogrid::GridFrame previous_frame_;
    int viewport_start_row_ = 0;
    int row_height_ = 0;
    int col_width_ = 0;
    int consecutive_complete_repeats_ = 0;
    std::map<std::pair<int, int>, Cell> cells_;
};

const char* ToString(Status status);

} // namespace gridtracker
