#pragma once

#include <array>
#include <optional>
#include <string>
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

struct GridSelectionDiagnostics
{
    cv::Point2d origin;
    cv::Point2d pitch;
    int rows = 0;
    int columns = 0;
    double best_score = 0.0;
    double second_score = 0.0;
    double score_margin = 0.0;
    double structure_score = 0.0;
    double rarity_score = 0.0;
    double consistency_score = 0.0;
    double maximum_residual = 0.0;
    double residual_trend = 0.0;
    std::array<int, 6> trusted_rarity_cells {};
    bool fallback_used = false;
    std::string fallback_reason;
    std::vector<std::string> rejected_reasons;
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
    std::optional<GridSelectionDiagnostics> selection_diagnostics;
};

struct GridDetection
{
    GridType type = GridType::Transfer;
    cv::Rect roi;
    std::vector<GridLayout> grids;
    std::vector<GridCell> cells;
};

} // namespace iconrecognition::detail
