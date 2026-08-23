#pragma once

#include "CellMask.h"
#include "GridDetector.h"
#include "PHashFilter.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <cstddef>
#include <string>
#include <vector>

namespace recogrid
{

struct GridRecognitionOptions
{
    GridDetectOptions detect;
    CellMaskRatios mask;
    int occupied_bright_threshold = 70;
    double min_occupied_mean = 55.0;
    double min_occupied_bright_ratio = 0.20;
};

struct GridCell
{
    int row = 0;
    int col = 0;
    std::size_t index = 0;
    cv::Rect screen_rect;
    Hash hash = 0;
    CellFeature feature;
    bool occupied = false;
};

struct GridFrame
{
    int rows = 0;
    int cols = 0;
    int row_height = 0;
    int col_width = 0;
    std::vector<GridCell> cells;
    std::string message;

    [[nodiscard]] bool empty() const { return rows <= 0 || cols <= 0 || cells.empty(); }
};

GridFrame RecognizeGrid(const cv::Mat& image, const GridRecognitionOptions& options = {});

} // namespace recogrid
