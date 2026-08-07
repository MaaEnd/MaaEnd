#pragma once

#include <vector>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

struct GridCell
{
    int grid_index = 0;
    int row = 0;
    int column = 0;
    cv::Rect cell_box;
};

struct GridLayout
{
    int grid_index = 0;
    cv::Rect bounds;
    int cell_size = 64;
    double pitch_x = 64.0;
    double pitch_y = 64.0;
    int rows = 0;
    int columns = 0;
    std::vector<GridCell> cells;
};

struct GridDetection
{
    GridType type = GridType::Transfer;
    cv::Rect roi;
    std::vector<GridLayout> grids;
    std::vector<GridCell> cells;
};

} // namespace iconrecognition::detail
