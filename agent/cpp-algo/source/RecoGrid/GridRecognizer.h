#pragma once

#include "GridGeometry.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <cstddef>
#include <string>
#include <vector>

namespace recogrid
{

inline constexpr int kAlignmentTileSize = 48;

struct GridRecognitionOptions
{
    GridRegion region_;
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
    bool occupied = false;
};

struct GridFrame
{
    int rows = 0;
    int cols = 0;
    std::vector<GridCell> cells;
    std::string message;
    cv::Mat alignment_tiles; // BGR tiles in grid order, including the gap after each card

    [[nodiscard]] bool empty() const { return rows <= 0 || cols <= 0 || cells.empty(); }
};

GridFrame RecognizeGrid(const cv::Mat& image, const GridRecognitionOptions& options, const GridLayout& detected);

} // namespace recogrid
