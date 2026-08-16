#include "manual_runner_config.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <charconv>
#include <filesystem>
#include <stdexcept>
#include <string_view>

#include <meojson/json.hpp>

namespace iconrecognition::test
{

namespace
{

bool IsHelpOption(std::string_view argument)
{
    return argument == "-h" || argument == "--help" || argument == "-?";
}

bool IsDualGridType(GridType grid_type)
{
    return grid_type == GridType::Transfer || grid_type == GridType::PortStorager;
}

bool IsPngFile(const std::filesystem::path& path)
{
    std::string extension = path.extension().string();
    std::ranges::transform(extension, extension.begin(), [](unsigned char value) { return static_cast<char>(std::tolower(value)); });
    return extension == ".png";
}

std::vector<std::filesystem::path> ListPngFiles(const std::filesystem::path& directory)
{
    std::vector<std::filesystem::path> paths;
    if (!std::filesystem::is_directory(directory)) {
        return paths;
    }
    for (const auto& entry : std::filesystem::directory_iterator(directory)) {
        if (entry.is_regular_file() && IsPngFile(entry.path())) {
            paths.push_back(entry.path());
        }
    }
    std::ranges::sort(paths);
    return paths;
}

cv::Rect ReadRoi(const json::value& value, std::string_view context)
{
    if (!value.is_array() || value.as_array().size() != 4) {
        throw std::runtime_error(std::string(context) + " must use Maa [x,y,width,height] format");
    }
    const auto& values = value.as_array();
    const cv::Rect roi(values.at(0).as_integer(), values.at(1).as_integer(), values.at(2).as_integer(), values.at(3).as_integer());
    if (roi.width <= 0 || roi.height <= 0) {
        throw std::runtime_error(std::string(context) + " must have positive width and height");
    }
    return roi;
}

const json::object& ReadRoiSet(const json::object& rois, GridType grid_type)
{
    const std::string grid_name(GridTypeName(grid_type));
    if (!rois.contains(grid_name) || !rois.at(grid_name).is_object()) {
        throw std::runtime_error("rois.json does not define grid type: " + grid_name);
    }
    return rois.at(grid_name).as_object();
}

std::vector<std::string_view> RoiNames(GridType grid_type, DualGridMode mode)
{
    if (!IsDualGridType(grid_type)) {
        return { "full" };
    }
    switch (mode) {
    case DualGridMode::Full:
        return { "full" };
    case DualGridMode::Left:
        return { "left" };
    case DualGridMode::Right:
        return { "right" };
    case DualGridMode::Split:
        return { "left", "right" };
    case DualGridMode::All:
        return { "full", "left", "right" };
    }
    throw std::runtime_error("unknown dual-grid mode");
}

int ParsePositiveInt(std::string_view value, std::string_view context)
{
    int parsed = 0;
    const auto [position, error] = std::from_chars(value.data(), value.data() + value.size(), parsed);
    if (error != std::errc {} || position != value.data() + value.size() || parsed <= 0) {
        throw std::runtime_error(std::string(context) + " must contain positive integers");
    }
    return parsed;
}

int ParseNonNegativeInt(std::string_view value, std::string_view context)
{
    int parsed = 0;
    const auto [position, error] = std::from_chars(value.data(), value.data() + value.size(), parsed);
    if (error != std::errc {} || position != value.data() + value.size() || parsed < 0) {
        throw std::runtime_error(std::string(context) + " must contain non-negative x and y coordinates");
    }
    return parsed;
}

cv::Rect ParseSingleRoiDirectory(const std::filesystem::path& directory)
{
    const std::string name = directory.filename().string();
    const std::size_t first = name.find('-');
    const std::size_t second = first == std::string::npos ? std::string::npos : name.find('-', first + 1);
    if (first == std::string::npos || second == std::string::npos || name.find('-', second + 1) != std::string::npos) {
        throw std::runtime_error("single_roi directory must use <x>-<y>-<size>: " + name);
    }
    const std::string_view view(name);
    const int x = ParseNonNegativeInt(view.substr(0, first), name);
    const int y = ParseNonNegativeInt(view.substr(first + 1, second - first - 1), name);
    const int size = ParsePositiveInt(view.substr(second + 1), name);
    return cv::Rect(x, y, size, size);
}

bool IsSelectedImage(const std::filesystem::path& image_path, const ManualRunnerOptions& options)
{
    return !options.image_name || image_path.filename().string() == *options.image_name;
}

CandidateFilter ReadImageCandidates(const std::filesystem::path& image_path)
{
    auto config_path = image_path;
    config_path.replace_extension(".json");
    if (!std::filesystem::is_regular_file(config_path)) {
        return {};
    }
    const auto parsed = json::open(config_path.string());
    if (!parsed || !parsed->is_object()) {
        throw std::runtime_error("image sidecar must be a JSON object: " + config_path.string());
    }
    CandidateFilter candidates;
    const auto& object = parsed->as_object();
    if (!object.contains("item_filters")) {
        return candidates;
    }
    if (!object.at("item_filters").is_array()) {
        throw std::runtime_error("image sidecar item_filters must be an array: " + config_path.string());
    }
    for (const auto& filter : object.at("item_filters").as_array()) {
        if (!filter.is_string()) {
            throw std::runtime_error("image sidecar item_filters must contain strings: " + config_path.string());
        }
        candidates.item_filters.push_back(filter.as_string());
    }
    return candidates;
}

void AppendGridCases(
    std::vector<ManualRunnerCase>& output,
    const std::filesystem::path& input_root,
    const json::object& rois,
    GridType grid_type,
    const ManualRunnerOptions& options)
{
    const auto images = ListPngFiles(input_root / std::string(GridTypeName(grid_type)));
    if (images.empty()) {
        return;
    }
    const auto& roi_set = ReadRoiSet(rois, grid_type);
    for (const auto& image_path : images) {
        if (!IsSelectedImage(image_path, options)) {
            continue;
        }
        for (const std::string_view roi_name : RoiNames(grid_type, options.dual_grid_mode)) {
            if (!roi_set.contains(std::string(roi_name))) {
                throw std::runtime_error(
                    "rois.json does not define " + std::string(roi_name) + " ROI for " + std::string(GridTypeName(grid_type)));
            }
            output.push_back(ManualRunnerCase {
                .grid_type = grid_type,
                .image_path = image_path,
                .roi = ReadRoi(roi_set.at(std::string(roi_name)), GridTypeName(grid_type)),
                .roi_name = std::string(roi_name),
                .candidates = ReadImageCandidates(image_path),
            });
        }
    }
}

void AppendSingleRoiCases(
    std::vector<ManualRunnerCase>& output,
    const std::filesystem::path& input_root,
    const ManualRunnerOptions& options)
{
    const auto root = input_root / std::string(GridTypeName(GridType::SingleRoi));
    if (!std::filesystem::is_directory(root)) {
        return;
    }
    std::vector<std::filesystem::path> directories;
    for (const auto& entry : std::filesystem::directory_iterator(root)) {
        if (entry.is_directory()) {
            directories.push_back(entry.path());
        }
    }
    std::ranges::sort(directories);
    for (const auto& directory : directories) {
        const cv::Rect roi = ParseSingleRoiDirectory(directory);
        for (const auto& image_path : ListPngFiles(directory)) {
            if (IsSelectedImage(image_path, options)) {
                output.push_back(ManualRunnerCase {
                    .grid_type = GridType::SingleRoi,
                    .image_path = image_path,
                    .roi = roi,
                    .roi_name = directory.filename().string(),
                    .candidates = ReadImageCandidates(image_path),
                });
            }
        }
    }
}

DualGridMode ParseDualGridMode(std::string_view value)
{
    if (value == "full") {
        return DualGridMode::Full;
    }
    if (value == "left") {
        return DualGridMode::Left;
    }
    if (value == "right") {
        return DualGridMode::Right;
    }
    if (value == "split") {
        return DualGridMode::Split;
    }
    if (value == "all") {
        return DualGridMode::All;
    }
    throw std::invalid_argument("unknown side mode: " + std::string(value));
}

const std::string& RequireValue(const std::vector<std::string>& arguments, std::size_t& index)
{
    ++index;
    if (index >= arguments.size() || arguments[index].starts_with('-')) {
        throw std::invalid_argument("missing value for " + arguments[index - 1]);
    }
    return arguments[index];
}

} // namespace

std::string ManualRunnerUsage()
{
    return R"(Usage:
  icon-recognition-manual-runner --all [--dataset win32|adb] [--side full|left|right|split|all] [--jobs <N|auto>] [--debug] [--expected <path>] [--rois <path>]
  icon-recognition-manual-runner --grid-type <type> [--image <basename>] [--dataset win32|adb] [--side full|left|right|split|all] [--jobs <N|auto>] [--debug] [--expected <path>] [--rois <path>]
  icon-recognition-manual-runner --image <basename> [--dataset win32|adb] [--jobs <N|auto>] [--debug] [--expected <path>] [--rois <path>]
  icon-recognition-manual-runner -h|--help|-?

Grid types:
  trade, transfer, port_storager, valuables, shipment, credit_trade, rewards, single_roi

Side modes apply only to transfer and port_storager. The default is full.
Worker selection defaults to 1. auto uses physical cores and is capped at 16.
Without --rois, the selected dataset uses its bundled ROI file. An unspecified dataset defaults to win32.
)";
}

ManualRunnerOptions ParseManualRunnerOptions(const std::vector<std::string>& arguments)
{
    ManualRunnerOptions options;
    if (arguments.empty()) {
        options.show_help = true;
        return options;
    }

    bool side_specified = false;
    bool jobs_specified = false;
    bool debug_specified = false;
    bool dataset_specified = false;
    bool expected_specified = false;
    bool rois_specified = false;
    for (std::size_t index = 0; index < arguments.size(); ++index) {
        const std::string& argument = arguments[index];
        if (IsHelpOption(argument)) {
            options.show_help = true;
            continue;
        }
        if (argument == "--all") {
            if (options.all_images) {
                throw std::invalid_argument("duplicate option: --all");
            }
            options.all_images = true;
            continue;
        }
        if (argument == "--grid-type") {
            if (options.grid_type) {
                throw std::invalid_argument("duplicate option: --grid-type");
            }
            const std::string& value = RequireValue(arguments, index);
            options.grid_type = ParseGridType(value);
            if (!options.grid_type) {
                throw std::invalid_argument("unknown grid type: " + value);
            }
            continue;
        }
        if (argument == "--image") {
            if (options.image_name) {
                throw std::invalid_argument("duplicate option: --image");
            }
            options.image_name = RequireValue(arguments, index);
            continue;
        }
        if (argument == "--side") {
            if (side_specified) {
                throw std::invalid_argument("duplicate option: --side");
            }
            options.dual_grid_mode = ParseDualGridMode(RequireValue(arguments, index));
            side_specified = true;
            continue;
        }
        if (argument == "--jobs") {
            if (jobs_specified) {
                throw std::invalid_argument("duplicate option: --jobs");
            }
            const std::string& value = RequireValue(arguments, index);
            if (value == "auto") {
                options.automatic_jobs = true;
            }
            else {
                try {
                    options.jobs = static_cast<std::size_t>(ParsePositiveInt(value, "--jobs"));
                }
                catch (const std::runtime_error& error) {
                    throw std::invalid_argument(error.what());
                }
            }
            jobs_specified = true;
            continue;
        }
        if (argument == "--debug") {
            if (debug_specified) {
                throw std::invalid_argument("duplicate option: --debug");
            }
            options.debug = true;
            debug_specified = true;
            continue;
        }
        if (argument == "--dataset") {
            if (dataset_specified) {
                throw std::invalid_argument("duplicate option: --dataset");
            }
            const std::string& value = RequireValue(arguments, index);
            if (value == "win32") {
                options.dataset = TestDataset::Win32;
            }
            else if (value == "adb") {
                options.dataset = TestDataset::Adb;
            }
            else {
                throw std::invalid_argument("unknown dataset: " + value);
            }
            dataset_specified = true;
            continue;
        }
        if (argument == "--expected") {
            if (expected_specified) {
                throw std::invalid_argument("duplicate option: --expected");
            }
            options.expected_path = RequireValue(arguments, index);
            expected_specified = true;
            continue;
        }
        if (argument == "--rois") {
            if (rois_specified) {
                throw std::invalid_argument("duplicate option: --rois");
            }
            options.rois_path = RequireValue(arguments, index);
            rois_specified = true;
            continue;
        }
        throw std::invalid_argument("unknown option: " + argument);
    }

    if (options.show_help) {
        if (arguments.size() != 1) {
            throw std::invalid_argument("help cannot be combined with other options");
        }
        return options;
    }
    if (options.all_images && (options.grid_type || options.image_name)) {
        throw std::invalid_argument("--all cannot be combined with --grid-type or --image");
    }
    if (!options.all_images && !options.grid_type && !options.image_name) {
        throw std::invalid_argument("select --all, --grid-type, or --image");
    }
    if (side_specified && !options.all_images && (!options.grid_type || !IsDualGridType(*options.grid_type))) {
        throw std::invalid_argument("--side requires --grid-type transfer or port_storager");
    }
    return options;
}

std::filesystem::path ResolveManualRunnerRoisPath(
    const ManualRunnerOptions& options,
    const std::filesystem::path& win32_default,
    const std::filesystem::path& adb_default)
{
    if (!options.rois_path.empty()) {
        return options.rois_path;
    }
    return options.dataset == TestDataset::Adb ? adb_default : win32_default;
}

std::size_t ResolveManualRunnerJobs(const ManualRunnerOptions& options, std::size_t physical_core_count, std::size_t case_count)
{
    if (case_count == 0) {
        return 0;
    }
    constexpr std::size_t kAutomaticMaximumJobs = 16;
    const std::size_t requested =
        options.automatic_jobs ? std::min(std::max<std::size_t>(physical_core_count, 1), kAutomaticMaximumJobs) : options.jobs;
    return std::min(std::max<std::size_t>(requested, 1), case_count);
}

std::vector<ManualRunnerCase> DiscoverManualRunnerCases(
    const std::filesystem::path& input_root,
    const std::filesystem::path& rois_path,
    const ManualRunnerOptions& options)
{
    if (options.show_help) {
        return {};
    }
    if (!std::filesystem::is_directory(input_root)) {
        throw std::runtime_error("input directory does not exist: " + input_root.string());
    }
    const auto parsed_rois = json::open(rois_path.string());
    if (!parsed_rois || !parsed_rois->is_object()) {
        throw std::runtime_error("unable to read rois.json: " + rois_path.string());
    }
    const auto& rois = parsed_rois->as_object();

    constexpr std::array kGridTypes {
        GridType::Trade,    GridType::Transfer,    GridType::PortStorager, GridType::Valuables,
        GridType::Shipment, GridType::CreditTrade, GridType::Rewards,
    };
    std::vector<ManualRunnerCase> cases;
    if (options.grid_type) {
        if (*options.grid_type == GridType::SingleRoi) {
            AppendSingleRoiCases(cases, input_root, options);
        }
        else {
            AppendGridCases(cases, input_root, rois, *options.grid_type, options);
        }
    }
    else {
        for (const GridType grid_type : kGridTypes) {
            AppendGridCases(cases, input_root, rois, grid_type, options);
        }
        AppendSingleRoiCases(cases, input_root, options);
    }
    if (cases.empty()) {
        throw std::runtime_error("no input images match the selected test scope");
    }
    return cases;
}

} // namespace iconrecognition::test
