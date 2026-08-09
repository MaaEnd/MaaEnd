#pragma once

#include <cstddef>

#include <meojson/json.hpp>

namespace iconrecognition::detail
{

struct RankingPerformanceDiagnostics
{
    double total_ms = 0.0;
    double baseline_scoring_ms = 0.0;
    double baseline_sort_ms = 0.0;
    double refinement_scoring_ms = 0.0;
    double refinement_sort_ms = 0.0;
    std::size_t baseline_candidates = 0;
    std::size_t refined_candidates = 0;
    std::size_t rarity_prefiltered_cells = 0;
    std::size_t rarity_fallback_cells = 0;
    std::size_t rarity_preferred_candidates = 0;
    std::size_t rarity_remaining_candidates = 0;

    json::value to_json() const;
};

struct MatcherPerformanceDiagnostics
{
    std::size_t score_calls = 0;
    double canvas_prepare_ms = 0.0;
    double template_shift_ms = 0.0;
    double template_match_ms = 0.0;
    double response_reduce_ms = 0.0;
    double lab_conversion_ms = 0.0;
    double color_distance_ms = 0.0;

    json::value to_json() const;
};

struct RecognitionPerformanceDiagnostics
{
    double total_ms = 0.0;
    double grid_detection_ms = 0.0;
    double template_selection_ms = 0.0;
    double active_templates_ms = 0.0;
    RankingPerformanceDiagnostics ranking;
    MatcherPerformanceDiagnostics matcher;
    double foreground_texture_ms = 0.0;
    double rarity_classification_ms = 0.0;
    double result_assembly_ms = 0.0;
    double result_sort_ms = 0.0;
    std::size_t cell_count = 0;

    json::value to_json() const;
};

} // namespace iconrecognition::detail
