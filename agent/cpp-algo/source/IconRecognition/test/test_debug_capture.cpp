#include <algorithm>
#include <cstdint>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <set>
#include <stdexcept>
#include <string>

#include <MaaUtils/NoWarningCV.hpp>
#include <meojson/json.hpp>

#include "../detail/DebugCapture.h"
#include "../detail/RecognitionDiagnostics.h"

namespace
{

void Require(bool condition, const std::string& message)
{
    if (!condition) {
        throw std::runtime_error(message);
    }
}

std::set<std::string> Stems(const std::filesystem::path& directory)
{
    std::set<std::string> stems;
    for (const auto& entry : std::filesystem::directory_iterator(directory)) {
        if (entry.is_regular_file()) {
            stems.insert(entry.path().stem().string());
        }
    }
    return stems;
}

void TestDebugCaptureKeepsSynchronizedGroups()
{
    const std::filesystem::path root = std::filesystem::current_path() / "icon-recognition-debug-fixture";
    std::filesystem::remove_all(root);

    iconrecognition::RecognitionResult result;
    result.matched = true;
    result.roi = cv::Rect(2, 3, 32, 32);
    result.diagnostics = std::make_shared<iconrecognition::detail::RecognitionDiagnostics>();
    result.diagnostics->performance = iconrecognition::detail::RecognitionPerformanceDiagnostics {
        .total_ms = 12.5,
        .grid_detection_ms = 1.25,
        .template_selection_ms = 0.5,
        .active_templates_ms = 0.25,
        .ranking =
            {
                .total_ms = 9.0,
                .baseline_scoring_ms = 7.0,
                .baseline_sort_ms = 1.0,
                .refinement_scoring_ms = 0.75,
                .refinement_sort_ms = 0.25,
                .baseline_candidates = 445,
                .refined_candidates = 5,
                .rarity_prefiltered_cells = 1,
                .rarity_fallback_cells = 1,
                .rarity_preferred_candidates = 48,
                .rarity_remaining_candidates = 397,
            },
        .matcher =
            {
                .score_calls = 450,
                .canvas_prepare_ms = 0.5,
                .template_shift_ms = 0.25,
                .template_match_ms = 4.5,
                .response_reduce_ms = 0.5,
                .lab_conversion_ms = 2.0,
                .color_distance_ms = 1.0,
            },
        .foreground_texture_ms = 0.25,
        .rarity_classification_ms = 0.25,
        .result_assembly_ms = 0.5,
        .result_sort_ms = 0.25,
        .cell_count = 1,
    };
    result.diagnostics->cells.push_back(iconrecognition::detail::CellRecognitionDiagnostics {
        .cell_box = cv::Rect(4, 5, 16, 16),
        .candidate_box = cv::Rect(6, 7, 12, 12),
        .best_candidate_id = "fixture_item",
        .baseline_score = 0.71,
        .score = 0.83,
        .top2_margin = 0.12,
        .candidate_count = 3,
        .fallback_used = true,
        .best_phase = cv::Point2d(0.25, -0.5),
        .rejected_reason = std::nullopt,
        .foreground_texture = 18.0,
        .rarity = 3,
        .rarity_coverage = 1.0,
        .rarity_row_offset = 0,
        .mask_kind = "lower_extended",
        .edge_occlusion_side = "top",
        .edge_occlusion_cutoff = 4,
        .edge_occlusion_residual_ratio = 6.5,
        .row = 0,
        .column = 1,
    });
    const cv::Mat image(32, 32, CV_8UC3, cv::Scalar(10, 20, 30));
    for (std::uint64_t reco_id = 1; reco_id <= 21; ++reco_id) {
        Require(iconrecognition::detail::SaveDebugCapture(root, image, result, reco_id), "debug capture must report successful writes");
    }

    const auto raw_stems = Stems(root / "raw");
    const auto annotated_stems = Stems(root / "annotated");
    const auto detail_stems = Stems(root / "detail");
    Require(raw_stems.size() == 20, "raw captures must retain at most 20 groups");
    Require(annotated_stems == raw_stems, "annotated captures must be trimmed by raw group stem");
    Require(detail_stems == raw_stems, "detail captures must be trimmed by raw group stem");

    const std::string latest_stem = *raw_stems.rbegin();
    const cv::Mat raw = cv::imread((root / "raw" / (latest_stem + ".png")).string(), cv::IMREAD_COLOR);
    const cv::Mat annotated = cv::imread((root / "annotated" / (latest_stem + ".png")).string(), cv::IMREAD_COLOR);
    Require(!raw.empty() && !annotated.empty(), "debug PNGs must be readable");
    Require(cv::norm(raw, image, cv::NORM_INF) == 0.0, "raw PNG must remain unannotated");
    Require(cv::norm(annotated, image, cv::NORM_INF) > 0.0, "annotated PNG must contain overlays");

    const auto detail = json::open((root / "detail" / (latest_stem + ".json")).string());
    Require(detail && detail->is_object(), "debug detail must be a JSON object");
    const auto& object = detail->as_object();
    Require(object.contains("diagnostics"), "debug detail must include internal diagnostics");
    const auto& diagnostics = object.at("diagnostics").as_object();
    const auto& performance = diagnostics.at("performance").as_object();
    Require(performance.at("total_ms").as_double() == 12.5, "debug detail must preserve total recognition time");
    Require(
        performance.at("ranking").as_object().at("baseline_candidates").as_integer() == 445,
        "debug detail must preserve baseline candidate count");
    Require(
        performance.at("ranking").as_object().at("rarity_preferred_candidates").as_integer() == 48,
        "debug detail must preserve preferred rarity candidate count");
    Require(
        performance.at("ranking").as_object().at("rarity_remaining_candidates").as_integer() == 397,
        "debug detail must preserve non-duplicated fallback candidate count");
    Require(performance.at("matcher").as_object().at("template_match_ms").as_double() == 4.5, "debug detail must preserve matcher timing");
    const auto& cell = diagnostics.at("cells").as_array().at(0).as_object();
    Require(cell.at("baseline_score").as_double() == 0.71, "debug detail must preserve baseline score");
    Require(cell.at("fallback_used").as_boolean(), "debug detail must preserve fallback state");
    Require(cell.at("mask_kind").as_string() == "lower_extended", "debug detail must preserve mask kind");
    const auto& edge_occlusion = cell.at("edge_occlusion").as_object();
    Require(edge_occlusion.at("side").as_string() == "top", "debug detail must preserve the edge-obstruction side");
    Require(edge_occlusion.at("cutoff").as_integer() == 4, "debug detail must preserve the dynamic mask cutoff");
    Require(edge_occlusion.at("residual_ratio").as_double() == 6.5, "debug detail must preserve edge residual evidence");
    Require(cell.at("rarity").as_object().at("row_offset").as_integer() == 0, "debug detail must preserve rarity row offset");

    std::filesystem::remove_all(root);
}

void TestDebugCaptureFailureIsBestEffort()
{
    const std::filesystem::path root = std::filesystem::current_path() / "icon-recognition-debug-blocked";
    std::filesystem::remove_all(root);
    {
        std::ofstream blocker(root, std::ios::binary | std::ios::trunc);
        Require(blocker.good(), "unable to create blocked debug fixture");
    }

    const cv::Mat image(8, 8, CV_8UC3, cv::Scalar(10, 20, 30));
    const iconrecognition::RecognitionResult result;
    bool saved = true;
    try {
        saved = iconrecognition::detail::SaveDebugCapture(root, image, result, 1);
    }
    catch (...) {
        std::filesystem::remove(root);
        throw std::runtime_error("debug capture failure must not escape the caller");
    }
    Require(!saved, "unwritable debug capture must report failure");
    std::filesystem::remove(root);
}

} // namespace

int main()
{
    try {
        TestDebugCaptureKeepsSynchronizedGroups();
        TestDebugCaptureFailureIsBestEffort();
        std::cout << "IconRecognition debug capture tests passed\n";
        return 0;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
