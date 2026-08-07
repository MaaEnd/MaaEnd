#include "RecognitionDiagnostics.h"

#include "../IconRecognitionTypes.h"

namespace iconrecognition::detail
{

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
    json::array cells_json;
    for (const auto& cell : cells) {
        cells_json.emplace_back(cell);
    }
    return json::object { { "cells", std::move(cells_json) } };
}

} // namespace iconrecognition::detail
