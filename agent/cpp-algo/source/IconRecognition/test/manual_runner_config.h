#pragma once

#include <cstddef>
#include <filesystem>
#include <optional>
#include <string>
#include <vector>

#include "../IconRecognitionTypes.h"

namespace iconrecognition::test
{

enum class DualGridMode
{
    Full,
    Left,
    Right,
    Split,
    All,
};

struct ManualRunnerOptions
{
    bool show_help = false;
    bool all_images = false;
    std::optional<GridType> grid_type;
    std::optional<std::string> image_name;
    DualGridMode dual_grid_mode = DualGridMode::Full;
    std::size_t jobs = 1;
    bool automatic_jobs = false;
    bool debug = false;
    std::filesystem::path expected_path;
};

struct ManualRunnerCase
{
    GridType grid_type;
    std::filesystem::path image_path;
    cv::Rect roi;
    std::string roi_name;
    CandidateFilter candidates;
};

std::string ManualRunnerUsage();
ManualRunnerOptions ParseManualRunnerOptions(const std::vector<std::string>& arguments);
std::vector<ManualRunnerCase> DiscoverManualRunnerCases(
    const std::filesystem::path& input_root,
    const std::filesystem::path& rois_path,
    const ManualRunnerOptions& options);
std::size_t ResolveManualRunnerJobs(const ManualRunnerOptions& options, std::size_t physical_core_count, std::size_t case_count);

} // namespace iconrecognition::test
