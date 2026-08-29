#pragma once

#include <optional>
#include <string>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>
#include <meojson/json.hpp>

#include "GridTypes.h"
#include "PerformanceDiagnostics.h"

namespace iconrecognition::detail
{

struct CellRecognitionDiagnostics
{
    cv::Rect cell_box;
    cv::Rect candidate_box;
    std::string best_candidate_id;
    double baseline_score = 0.0;
    double score = 0.0;
    std::optional<double> top2_margin;
    std::size_t candidate_count = 0;
    bool fallback_used = false;
    bool region_unavailable_fallback_used = false;
    cv::Point2d best_phase;
    std::optional<std::string> rejected_reason;
    std::optional<double> foreground_texture;
    std::optional<int> rarity;
    double rarity_coverage = 0.0;
    std::optional<int> rarity_row_offset;
    std::string mask_kind;
    std::optional<std::string> edge_occlusion_side;
    std::optional<int> edge_occlusion_cutoff;
    std::optional<double> edge_occlusion_residual_ratio;
    std::optional<int> row;
    std::optional<int> column;

    json::value to_json() const;
};

struct RecognitionDiagnostics
{
    std::vector<GridSelectionDiagnostics> grids;
    std::vector<CellRecognitionDiagnostics> cells;
    // 性能数据只在 debug 请求中采集，避免正常识别承担计时开销。
    std::optional<RecognitionPerformanceDiagnostics> performance;

    json::value to_json() const;
};

} // namespace iconrecognition::detail
