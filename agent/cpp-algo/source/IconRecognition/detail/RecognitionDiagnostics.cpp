#include "RecognitionDiagnostics.h"

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

json::value RankingPerformanceDiagnostics::to_json() const
{
    return json::object {
        { "total_ms", total_ms },
        { "baseline_scoring_ms", baseline_scoring_ms },
        { "baseline_sort_ms", baseline_sort_ms },
        { "refinement_scoring_ms", refinement_scoring_ms },
        { "refinement_sort_ms", refinement_sort_ms },
        { "baseline_candidates", static_cast<unsigned long long>(baseline_candidates) },
        { "refined_candidates", static_cast<unsigned long long>(refined_candidates) },
        { "rarity_prefiltered_cells", static_cast<unsigned long long>(rarity_prefiltered_cells) },
        { "rarity_fallback_cells", static_cast<unsigned long long>(rarity_fallback_cells) },
        { "rarity_preferred_candidates", static_cast<unsigned long long>(rarity_preferred_candidates) },
        { "rarity_remaining_candidates", static_cast<unsigned long long>(rarity_remaining_candidates) },
    };
}

json::value MatcherPerformanceDiagnostics::to_json() const
{
    return json::object {
        { "score_calls", static_cast<unsigned long long>(score_calls) },
        { "canvas_prepare_ms", canvas_prepare_ms },
        { "template_shift_ms", template_shift_ms },
        { "template_match_ms", template_match_ms },
        { "response_reduce_ms", response_reduce_ms },
        { "lab_conversion_ms", lab_conversion_ms },
        { "color_distance_ms", color_distance_ms },
    };
}

json::value RecognitionPerformanceDiagnostics::to_json() const
{
    return json::object {
        { "total_ms", total_ms },
        { "grid_detection_ms", grid_detection_ms },
        { "template_selection_ms", template_selection_ms },
        { "active_templates_ms", active_templates_ms },
        { "ranking", ranking },
        { "matcher", matcher },
        { "foreground_texture_ms", foreground_texture_ms },
        { "rarity_classification_ms", rarity_classification_ms },
        { "result_assembly_ms", result_assembly_ms },
        { "result_sort_ms", result_sort_ms },
        { "cell_count", static_cast<unsigned long long>(cell_count) },
    };
}

json::value CellRecognitionDiagnostics::to_json() const
{
    json::object object {
        { "cell_box", RectToJson(cell_box) },
        { "candidate_box", RectToJson(candidate_box) },
        { "best_candidate_id", best_candidate_id },
        { "baseline_score", baseline_score },
        { "score", score },
        { "candidate_count", static_cast<unsigned long long>(candidate_count) },
        { "fallback_used", fallback_used },
        { "best_phase", json::object { { "x", best_phase.x }, { "y", best_phase.y } } },
        { "rarity", json::object { { "coverage", rarity_coverage } } },
        { "mask_kind", mask_kind },
    };
    if (top2_margin) {
        object["top2_margin"] = *top2_margin;
    }
    if (rejected_reason) {
        object["rejected_reason"] = *rejected_reason;
    }
    if (foreground_texture) {
        object["foreground_texture"] = *foreground_texture;
    }
    if (rarity) {
        object["rarity"]["rarity"] = *rarity;
    }
    if (rarity_row_offset) {
        object["rarity"]["row_offset"] = *rarity_row_offset;
    }
    if (edge_occlusion_side && edge_occlusion_cutoff && edge_occlusion_residual_ratio) {
        object["edge_occlusion"] = json::object {
            { "side", *edge_occlusion_side },
            { "cutoff", *edge_occlusion_cutoff },
            { "residual_ratio", *edge_occlusion_residual_ratio },
        };
    }
    if (row) {
        object["row"] = *row;
    }
    if (column) {
        object["column"] = *column;
    }
    return object;
}

json::value RecognitionDiagnostics::to_json() const
{
    json::array grids_json;
    for (const auto& grid : grids) {
        json::array rarity_counts;
        for (int count : grid.trusted_rarity_cells) {
            rarity_counts.emplace_back(count);
        }
        json::array rejected_reasons;
        for (const auto& reason : grid.rejected_reasons) {
            rejected_reasons.emplace_back(reason);
        }
        grids_json.emplace_back(json::object {
            { "origin", json::object { { "x", grid.origin.x }, { "y", grid.origin.y } } },
            { "pitch", json::object { { "x", grid.pitch.x }, { "y", grid.pitch.y } } },
            { "rows", grid.rows },
            { "columns", grid.columns },
            { "best_score", grid.best_score },
            { "second_score", grid.second_score },
            { "score_margin", grid.score_margin },
            { "structure_score", grid.structure_score },
            { "rarity_score", grid.rarity_score },
            { "consistency_score", grid.consistency_score },
            { "maximum_residual", grid.maximum_residual },
            { "residual_trend", grid.residual_trend },
            { "trusted_rarity_cells", std::move(rarity_counts) },
            { "fallback_used", grid.fallback_used },
            { "fallback_reason", grid.fallback_reason },
            { "rejected_reasons", std::move(rejected_reasons) },
        });
    }
    json::array cells_json;
    for (const auto& cell : cells) {
        cells_json.emplace_back(cell);
    }
    json::object object { { "grids", std::move(grids_json) }, { "cells", std::move(cells_json) } };
    if (performance) {
        object["performance"] = *performance;
    }
    return object;
}

} // namespace iconrecognition::detail
