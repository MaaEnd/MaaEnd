#pragma once

#include "../RecoGrid/GridRecognizer.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <cstddef>
#include <set>
#include <string>
#include <utility>
#include <vector>

namespace gridtracker
{

enum class Status
{
    Initial,
    Advanced,
    Stalled,
    Complete,
    Rejected,
};

struct Options
{
    int expected_cells = -1;
    bool at_top = false;
    bool at_bottom = false;
    double estimated_start_row = 0;
    double position_tolerance_rows = 0;
};

struct Alignment
{
    int row_offset = 0;
    int support_rows = 0;
    int compared_cells = 0;
    int matched_cells = 0;
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
    Result observe(const recogrid::GridFrame& frame, const Options& options);
    [[nodiscard]] bool matches_current_viewport(const recogrid::GridFrame& frame, int expected_cells) const;

    [[nodiscard]] bool has_viewport() const { return !previous_frame_.empty(); }

    void reset();

private:
    recogrid::GridFrame previous_frame_;
    int viewport_start_row_ = 0;
    std::set<std::pair<int, int>> seen_positions_;
};

const char* ToString(Status status);

} // namespace gridtracker
