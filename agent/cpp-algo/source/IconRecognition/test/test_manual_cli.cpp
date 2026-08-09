#include "manual_runner_config.h"

#include <filesystem>
#include <fstream>
#include <functional>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

namespace
{

void Check(bool condition, const std::string& message)
{
    if (!condition) {
        throw std::runtime_error(message);
    }
}

void CheckRejected(const std::vector<std::string>& arguments, const std::string& context)
{
    try {
        static_cast<void>(iconrecognition::test::ParseManualRunnerOptions(arguments));
    }
    catch (const std::invalid_argument&) {
        return;
    }
    throw std::runtime_error(context + " must be rejected");
}

void TestHelpModes()
{
    Check(iconrecognition::test::ParseManualRunnerOptions({}).show_help, "no arguments must show usage");
    for (const std::string option : { "-h", "--help", "-?" }) {
        Check(iconrecognition::test::ParseManualRunnerOptions({ option }).show_help, option + " must show usage");
    }
    const std::string usage = iconrecognition::test::ManualRunnerUsage();
    Check(usage.find("--all") != std::string::npos, "usage must document full test selection");
    Check(usage.find("--grid-type") != std::string::npos, "usage must document grid type selection");
    Check(usage.find("--image") != std::string::npos, "usage must document image selection");
    Check(
        usage.find("full|left|right|split|all") != std::string::npos,
        "usage must document dual-grid modes");
}

void TestSelectors()
{
    const auto all = iconrecognition::test::ParseManualRunnerOptions({ "--all" });
    Check(all.all_images && !all.grid_type && !all.image_name, "--all must select every classified image");

    const auto type = iconrecognition::test::ParseManualRunnerOptions({ "--grid-type", "transfer" });
    Check(type.grid_type == iconrecognition::GridType::Transfer, "--grid-type must parse public grid type names");

    const auto image = iconrecognition::test::ParseManualRunnerOptions({ "--image", "43.png" });
    Check(image.image_name == "43.png", "--image must preserve the exact basename");

    const auto combined =
        iconrecognition::test::ParseManualRunnerOptions({ "--grid-type", "transfer", "--image", "43.png", "--side", "all" });
    Check(combined.grid_type == iconrecognition::GridType::Transfer, "combined selector must retain grid type");
    Check(combined.image_name == "43.png", "combined selector must retain image name");
    Check(combined.dual_grid_mode == iconrecognition::test::DualGridMode::All, "combined selector must retain dual-grid mode");

    const auto full_audit = iconrecognition::test::ParseManualRunnerOptions({ "--all", "--side", "all" });
    Check(full_audit.all_images, "--all with a dual-grid mode must remain a full audit");
    Check(full_audit.dual_grid_mode == iconrecognition::test::DualGridMode::All, "--all must retain the dual-grid mode");
}

void TestDualGridModes()
{
    const std::vector<std::pair<std::string, iconrecognition::test::DualGridMode>> modes {
        { "full", iconrecognition::test::DualGridMode::Full },
        { "left", iconrecognition::test::DualGridMode::Left },
        { "right", iconrecognition::test::DualGridMode::Right },
        { "split", iconrecognition::test::DualGridMode::Split },
        { "all", iconrecognition::test::DualGridMode::All },
    };
    for (const auto& [name, expected] : modes) {
        const auto options = iconrecognition::test::ParseManualRunnerOptions({ "--grid-type", "port_storager", "--side", name });
        Check(options.dual_grid_mode == expected, "dual-grid mode mismatch: " + name);
    }
}

void TestInvalidArguments()
{
    CheckRejected({ "--all", "--image", "43.png" }, "conflicting all/image selectors");
    CheckRejected({ "--grid-type", "unknown" }, "unknown grid type");
    CheckRejected({ "--image" }, "missing image value");
    CheckRejected({ "--grid-type", "trade", "--side", "left" }, "side mode on a single grid type");
    CheckRejected({ "--side", "left", "--image", "43.png" }, "side mode without a dual grid type");
    CheckRejected({ "--unknown" }, "unknown option");
}

void WriteTextFile(const std::filesystem::path& path, const std::string& content)
{
    std::filesystem::create_directories(path.parent_path());
    std::ofstream stream(path, std::ios::binary | std::ios::trunc);
    stream << content;
}

void TestCaseDiscovery()
{
    const std::filesystem::path fixture = std::filesystem::path(ICON_RECOGNITION_TEST_BUILD_DIR) / "manual-runner-config-fixture";
    const auto input_root = fixture / "input";
    const auto rois_path = fixture / "rois.json";
    WriteTextFile(
        rois_path,
        R"({
    "trade": { "full": [170, 165, 935, 385] },
    "transfer": {
        "full": [154, 202, 983, 291],
        "left": [154, 202, 585, 291],
        "right": [739, 202, 398, 291]
    },
    "port_storager": {
        "full": [190, 250, 880, 350],
        "left": [190, 250, 318, 350],
        "right": [570, 250, 500, 350]
    }
})");
    WriteTextFile(input_root / "trade" / "11.png", {});
    WriteTextFile(input_root / "transfer" / "43.png", {});
    WriteTextFile(input_root / "port_storager" / "43.png", {});
    WriteTextFile(input_root / "single_roi" / "1177-450-54" / "90.png", {});

    const auto all = iconrecognition::test::DiscoverManualRunnerCases(
        input_root,
        rois_path,
        iconrecognition::test::ParseManualRunnerOptions({ "--all" }));
    Check(all.size() == 4, "--all must use one default ROI for every classified input image");

    const auto split = iconrecognition::test::DiscoverManualRunnerCases(
        input_root,
        rois_path,
        iconrecognition::test::ParseManualRunnerOptions({ "--grid-type", "transfer", "--side", "split" }));
    Check(split.size() == 2, "split must run both single-side ROIs for every transfer image");
    Check(split[0].roi_name == "left" && split[1].roi_name == "right", "split must keep left/right ROI ordering");

    const auto triple = iconrecognition::test::DiscoverManualRunnerCases(
        input_root,
        rois_path,
        iconrecognition::test::ParseManualRunnerOptions({ "--grid-type", "port_storager", "--side", "all" }));
    Check(triple.size() == 3, "all must run full, left, and right ROIs for a dual grid");
    Check(triple[0].roi_name == "full" && triple[1].roi_name == "left" && triple[2].roi_name == "right", "all must keep stable ROI ordering");

    const auto named = iconrecognition::test::DiscoverManualRunnerCases(
        input_root,
        rois_path,
        iconrecognition::test::ParseManualRunnerOptions({ "--image", "43.png" }));
    Check(named.size() == 2, "a basename selector must include every classified image with that exact name");

    const auto single = iconrecognition::test::DiscoverManualRunnerCases(
        input_root,
        rois_path,
        iconrecognition::test::ParseManualRunnerOptions({ "--grid-type", "single_roi" }));
    Check(single.size() == 1, "single_roi directories must produce their contained image cases");
    Check(single[0].roi == cv::Rect(1177, 450, 54, 54), "single_roi must parse x-y-size directories as square ROIs");
}

void TestSingleRoiAtScreenOrigin()
{
    const std::filesystem::path fixture = std::filesystem::path(ICON_RECOGNITION_TEST_BUILD_DIR) / "manual-runner-origin-fixture";
    const auto input_root = fixture / "input";
    const auto rois_path = fixture / "rois.json";
    WriteTextFile(rois_path, "{}");
    WriteTextFile(input_root / "single_roi" / "0-0-54" / "edge.png", {});

    const auto cases = iconrecognition::test::DiscoverManualRunnerCases(
        input_root,
        rois_path,
        iconrecognition::test::ParseManualRunnerOptions({ "--grid-type", "single_roi" }));
    Check(cases.size() == 1, "single_roi at the screen origin must produce one case");
    Check(cases[0].roi == cv::Rect(0, 0, 54, 54), "single_roi must allow zero x and y coordinates");
}

} // namespace

int main()
{
    try {
        TestHelpModes();
        TestSelectors();
        TestDualGridModes();
        TestInvalidArguments();
        TestCaseDiscovery();
        TestSingleRoiAtScreenOrigin();
        std::cout << "IconRecognition manual CLI tests passed\n";
        return 0;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
