#ifdef ICON_RECOGNITION_TEST_MAIN

#include "../IconRecognizer.h"
#include "../detail/ForegroundTexture.h"
#include "../detail/GridAnchors.h"
#include "../detail/GridDetector.h"
#include "../detail/GridFeatures.h"
#include "../detail/GridGeometry.h"
#include "../detail/GridProfiles.h"
#include "../detail/IconMatcher.h"
#include "../detail/MaskPolicy.h"
#include "../detail/RarityClassifier.h"
#include "../detail/RarityCandidates.h"
#include "../detail/RegularLattice.h"
#include "../detail/SubpixelMatcher.h"
#include "../detail/TemplateCatalog.h"
#include "../detail/TemplateTypes.h"
#include "../detail/TrustedRarity.h"

#include <algorithm>
#include <array>
#include <barrier>
#include <cmath>
#include <cstdlib>
#include <exception>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <thread>
#include <tuple>
#include <vector>

namespace
{

void Check(bool condition, const std::string& message)
{
    if (!condition) {
        throw std::runtime_error(message);
    }
}

void TestLowerExtendedMaskSnapshots()
{
    const std::array<std::pair<int, int>, 3> snapshots {
        std::pair { 64, 1841 },
        std::pair { 96, 4104 },
        std::pair { 128, 7393 },
    };
    for (const auto& [size, active_pixels] : snapshots) {
        const cv::Mat mask = iconrecognition::detail::BuildLowerExtendedMask(size);
        const int actual_pixels = cv::countNonZero(mask);
        if (std::abs(actual_pixels - active_pixels) > 1) {
            std::cerr << "row counts:";
            for (int row = 0; row < mask.rows; ++row) {
                std::cerr << ' ' << cv::countNonZero(mask.row(row));
            }
            std::cerr << '\n';
        }
        Check(
            std::abs(actual_pixels - active_pixels) <= 1,
            "lower mask active pixel snapshot drift exceeds OpenCV rasterization tolerance: size=" + std::to_string(size)
                + " expected=" + std::to_string(active_pixels) + " actual=" + std::to_string(actual_pixels));
        Check(mask.at<unsigned char>(0, size / 2) == 255, "lower mask top center must be active");
        Check(mask.at<unsigned char>(size - 1, 0) == 0, "lower mask bottom left must be clear");
        Check(mask.at<unsigned char>(size - 1, size / 2) == 0, "lower mask bottom center must be clear");
    }
}

void TestShipmentQuantityBarThreshold()
{
    cv::Mat slot = cv::Mat::zeros(64, 64, CV_8UC3);
    slot(cv::Rect(0, 8, 25, 12)).setTo(cv::Scalar(0, 220, 220));
    slot(cv::Rect(25, 8, 25, 8)).setTo(cv::Scalar(0, 220, 220));
    Check(cv::countNonZero(slot.reshape(1)) > 0, "shipment fixture must contain color");
    Check(iconrecognition::detail::HasShipmentTopBar(slot), "500 yellow pixels in top 20 rows must be accepted");

    slot.setTo(cv::Scalar(0, 0, 0));
    slot(cv::Rect(0, 0, 20, 20)).setTo(cv::Scalar(0, 220, 220));
    Check(!iconrecognition::detail::HasShipmentTopBar(slot), "400 yellow pixels must be rejected");
}

void TestForegroundTextureUsesContentInsets()
{
    cv::Mat image = cv::Mat::zeros(64, 64, CV_8UC3);
    for (int y = 6; y < 56; ++y) {
        for (int x = 6; x < 16; ++x) {
            const unsigned char value = ((x + y) % 2 == 0) ? 0 : 255;
            image.at<cv::Vec3b>(y, x) = cv::Vec3b(value, value, value);
        }
    }
    Check(
        !iconrecognition::detail::IsLowTexture(image, cv::Rect(0, 0, 64, 64), iconrecognition::GridType::Transfer, 10.0),
        "texture inside the content inset must be retained");
}

void TestStructureFeatureModuleContract()
{
    cv::Mat image = cv::Mat::zeros(96, 96, CV_8UC3);
    image.colRange(47, 49).setTo(cv::Scalar(255, 255, 255));

    const auto maps = iconrecognition::detail::BuildStructureMaps(image, 64);
    Check(maps.vertical.size() == image.size(), "vertical structure map size mismatch");
    Check(maps.horizontal.size() == image.size(), "horizontal structure map size mismatch");
    const auto projection = iconrecognition::detail::RobustProjection(maps.vertical, true);
    Check(projection.size() == 96, "vertical structure projection size mismatch");
}

void TestGridGeometryModuleContract()
{
    const std::vector<float> empty_signal(220, 0.0F);
    const auto axis =
        iconrecognition::detail::FitSubpixelAxis(empty_signal, empty_signal, empty_signal, empty_signal, 64, 69, { 67, 71 }, 3);
    Check(axis.integer_starts == std::vector<int>({ 0, 69, 138 }), "empty evidence must use the fallback axis sequence");
}

void TestTradeGridUsesCardBoundariesForVerticalPhase()
{
    constexpr int kCellSize = 96;
    constexpr int kPitchX = 310;
    constexpr int kPitchY = 109;
    constexpr int kCardWidth = 300;
    constexpr int kPhaseY = 70;
    const cv::Rect roi(0, 0, 935, 385);
    cv::Mat image(roi.size(), CV_8UC3, cv::Scalar(24, 24, 24));

    for (int row = 0; row < 3; ++row) {
        const int y = kPhaseY + row * kPitchY;
        for (int column = 0; column < 3; ++column) {
            const int x = 10 + column * kPitchX;
            image(cv::Rect(x, y, kCardWidth, kCellSize)).setTo(cv::Scalar(132, 132, 132));
            image(cv::Rect(x, y, kCellSize, kCellSize)).setTo(cv::Scalar(224, 224, 224));
        }
    }

    // 反向强边界模拟卡片内部纹理：结构投影会响应，但卡片边界应保持“外暗内亮”。
    constexpr int kTextureOffset = 25;
    constexpr int kTextureBand = 6;
    for (int row = 0; row < 3; ++row) {
        const int false_y = kPhaseY - kTextureOffset + row * kPitchY;
        image.rowRange(false_y - kTextureBand, false_y).setTo(cv::Scalar(245, 245, 245));
        image.rowRange(false_y, false_y + kTextureBand).setTo(cv::Scalar(12, 12, 12));
        image.rowRange(false_y + kCellSize - kTextureBand, false_y + kCellSize).setTo(cv::Scalar(12, 12, 12));
        image.rowRange(false_y + kCellSize, false_y + kCellSize + kTextureBand).setTo(cv::Scalar(245, 245, 245));
    }

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Trade, roi);
    const auto first_row = std::ranges::find_if(grid.cells, [](const auto& cell) { return cell.row == 0 && cell.column == 0; });
    Check(first_row != grid.cells.end(), "synthetic trade grid must contain its first cell");
    Check(
        std::abs(first_row->cell_box.y - kPhaseY) <= 1,
        "trade grid must follow card boundaries instead of internal texture: actual_y=" + std::to_string(first_row->cell_box.y));
}

void TestTransferRegionPartitionKeepsUndetectedOuterColumns()
{
    const cv::Rect detected_left(8, 20, 203, 271);
    const cv::Rect detected_right(394, 18, 479, 271);
    const auto regions = iconrecognition::detail::PartitionTransferRegions(cv::Size(880, 350), detected_left, detected_right);

    Check(regions.size() == 2, "two detected grids must produce two search regions");
    Check(regions[0].x == 0, "left transfer search region must begin at the ROI edge");
    Check(regions[1].x > regions[0].width, "transfer search regions may preserve unstructured space between grids");
    Check(regions[1].x < detected_right.x, "right transfer search region must retain structural context before the grid");
    Check(regions[0].width >= detected_left.x + 4 * 69, "left transfer search region must retain room for a weak outer column");
}

void TestCreditTradeGridUsesDimCardStructures()
{
    constexpr int kCellSize = 128;
    constexpr int kPitchX = 161;
    constexpr int kPitchY = 205;
    constexpr int kColumns = 7;
    constexpr int kRows = 2;
    constexpr int kPhaseX = 20;
    constexpr int kPhaseY = 14;
    const cv::Rect roi(0, 0, 1130, 410);
    cv::Mat image(roi.size(), CV_8UC3, cv::Scalar(28, 28, 28));

    for (int row = 0; row < kRows; ++row) {
        for (int column = 0; column < kColumns; ++column) {
            const int x = kPhaseX + column * kPitchX;
            const int y = kPhaseY + row * kPitchY;
            const bool bright_anchor = row == 0 && column < 3;
            const unsigned char card_value = bright_anchor ? 240 : 72;
            image(cv::Rect(x - 10, y - 6, 150, 180)).setTo(cv::Scalar(card_value, card_value, card_value));
            const unsigned char value = bright_anchor ? 245 : static_cast<unsigned char>(120 + 12 * ((row + column) % 3));
            image(cv::Rect(x, y, kCellSize, kCellSize)).setTo(cv::Scalar(value, value, value));
        }
    }

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::CreditTrade, roi);
    Check(grid.grids.size() == 1, "dim credit cards must form one grid");
    Check(grid.grids.front().columns == kColumns, "dim credit card grid must retain every column");
    Check(grid.grids.front().rows == kRows, "dim credit card grid must retain every row");
}

void TestTransferProfileModuleContract()
{
    const cv::Mat image = cv::imread("agent/cpp-algo/source/IconRecognition/test/input/transfer/62.png");
    Check(!image.empty(), "transfer profile fixture is missing");
    const cv::Rect roi(155, 205, 970, 280);
    const auto hints = iconrecognition::detail::DiscoverTransferGridHints(image(roi), true);
    Check(hints.size() == 2, "transfer profile must discover two representative grid regions");

    const cv::Rect left_roi(roi.x, roi.y, roi.width / 2, roi.height);
    const cv::Rect right_roi(roi.x + roi.width / 2, roi.y, roi.width - roi.width / 2, roi.height);
    Check(
        iconrecognition::detail::DiscoverTransferGridHints(image(left_roi), true).size() == 1,
        "transfer profile must accept the left grid ROI independently");
    Check(
        iconrecognition::detail::DiscoverTransferGridHints(image(right_roi), true).size() == 1,
        "transfer profile must accept the right grid ROI independently");

    const cv::Mat port_image = cv::imread("agent/cpp-algo/source/IconRecognition/test/input/port_storager/1.png");
    Check(!port_image.empty(), "port storager profile fixture is missing");
    const cv::Rect port_roi(190, 250, 880, 350);
    const cv::Rect port_left_roi(port_roi.x, port_roi.y, port_roi.width / 2, port_roi.height);
    const cv::Rect port_right_roi(port_roi.x + port_roi.width / 2, port_roi.y, port_roi.width - port_roi.width / 2, port_roi.height);
    Check(
        iconrecognition::detail::DiscoverTransferGridHints(port_image(port_roi), false).size() == 2,
        "port storager profile must discover two representative grid regions");
    Check(
        iconrecognition::detail::DiscoverTransferGridHints(port_image(port_left_roi), false).size() == 1,
        "port storager profile must accept the left grid ROI independently");
    Check(
        iconrecognition::detail::DiscoverTransferGridHints(port_image(port_right_roi), false).size() == 1,
        "port storager profile must accept the right grid ROI independently");
}

std::vector<int> GridRowStarts(const iconrecognition::detail::GridLayout& grid)
{
    std::vector<int> starts;
    for (const auto& cell : grid.cells) {
        starts.push_back(cell.cell_box.y);
    }
    std::ranges::sort(starts);
    starts.erase(std::unique(starts.begin(), starts.end()), starts.end());
    return starts;
}

std::vector<int> GridColumnStarts(const iconrecognition::detail::GridLayout& grid)
{
    std::vector<int> starts;
    for (const auto& cell : grid.cells) {
        starts.push_back(cell.cell_box.x);
    }
    std::ranges::sort(starts);
    starts.erase(std::unique(starts.begin(), starts.end()), starts.end());
    return starts;
}

std::vector<cv::Rect> GridCellBoxes(const iconrecognition::detail::GridDetection& detection)
{
    std::vector<cv::Rect> boxes;
    for (const auto& cell : detection.cells) {
        boxes.push_back(cell.cell_box);
    }
    std::ranges::sort(boxes, [](const auto& left, const auto& right) {
        return std::tie(left.x, left.y, left.width, left.height) < std::tie(right.x, right.y, right.width, right.height);
    });
    return boxes;
}

void CheckGridRows(
    const iconrecognition::detail::GridDetection& detection,
    std::size_t grid_index,
    int expected_first,
    int tolerance,
    int expected_rows,
    const std::string& context)
{
    Check(detection.grids.size() > grid_index, context + " grid is missing");
    const auto starts = GridRowStarts(detection.grids[grid_index]);
    Check(!starts.empty(), context + " has no rows");
    Check(
        std::abs(starts.front() - expected_first) <= tolerance,
        context + " first row mismatch: expected=" + std::to_string(expected_first) + " actual=" + std::to_string(starts.front()));
    Check(
        static_cast<int>(starts.size()) == expected_rows,
        context + " row count mismatch: expected=" + std::to_string(expected_rows) + " actual=" + std::to_string(starts.size()));
}

void CheckGridOrigin(
    const iconrecognition::detail::GridDetection& detection,
    std::size_t grid_index,
    cv::Point expected,
    int tolerance,
    cv::Size expected_shape,
    const std::string& context)
{
    Check(detection.grids.size() > grid_index, context + " grid is missing");
    const auto columns = GridColumnStarts(detection.grids[grid_index]);
    const auto rows = GridRowStarts(detection.grids[grid_index]);
    Check(!columns.empty() && !rows.empty(), context + " has no cells");
    Check(
        std::abs(columns.front() - expected.x) <= tolerance,
        context + " first column mismatch: expected=" + std::to_string(expected.x) + " actual=" + std::to_string(columns.front()));
    Check(
        std::abs(rows.front() - expected.y) <= tolerance,
        context + " first row mismatch: expected=" + std::to_string(expected.y) + " actual=" + std::to_string(rows.front())
            + " actual_x=" + std::to_string(columns.front()) + " pitch_x=" + std::to_string(detection.grids[grid_index].pitch_x)
            + " pitch_y=" + std::to_string(detection.grids[grid_index].pitch_y));
    Check(
        static_cast<int>(columns.size()) == expected_shape.width,
        context + " column count mismatch: expected=" + std::to_string(expected_shape.width) + " actual=" + std::to_string(columns.size()));
    Check(
        static_cast<int>(rows.size()) == expected_shape.height,
        context + " row count mismatch: expected=" + std::to_string(expected_shape.height) + " actual=" + std::to_string(rows.size()));
}

void CheckGridColumns(
    const iconrecognition::detail::GridDetection& detection,
    std::size_t grid_index,
    int expected_first,
    int tolerance,
    int expected_columns,
    const std::string& context)
{
    Check(detection.grids.size() > grid_index, context + " grid is missing");
    const auto columns = GridColumnStarts(detection.grids[grid_index]);
    Check(!columns.empty(), context + " has no columns");
    Check(
        std::abs(columns.front() - expected_first) <= tolerance,
        context + " first column mismatch: expected=" + std::to_string(expected_first) + " actual=" + std::to_string(columns.front()));
    Check(
        static_cast<int>(columns.size()) == expected_columns,
        context + " column count mismatch: expected=" + std::to_string(expected_columns) + " actual=" + std::to_string(columns.size()));
}

void TestTransferRarityBarsAnchorGridOrigins()
{
    const auto profile = iconrecognition::detail::TransferProfileFor(iconrecognition::detail::TransferGridVariant::TransferLeft);
    Check(profile.cell_size == 64, "transfer left profile cell size mismatch");
    Check(profile.pitch_min == 68 && profile.pitch_max == 70 && profile.preferred_pitch == 69, "transfer left profile pitch mismatch");
    Check(profile.rarity_anchor_offset == 64, "transfer left rarity anchor offset mismatch");
    Check(
        std::abs(profile.minimum_rarity_coverage - 0.80) <= 1e-6,
        "transfer left rarity coverage mismatch: actual=" + std::to_string(profile.minimum_rarity_coverage));

    const cv::Rect roi(154, 202, 983, 291);
    const auto detect = [&](const char* name) {
        const std::filesystem::path path = std::filesystem::path("agent/cpp-algo/source/IconRecognition/test/input/transfer") / name;
        const cv::Mat image = cv::imread(path.string(), cv::IMREAD_COLOR);
        Check(!image.empty(), "transfer grid fixture is missing: " + path.string());
        return iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, roi);
    };

    // 完整四行应保持标准色带 origin，不能被界面内横纹移动到相邻相位。
    CheckGridRows(detect("12.png"), 0, 217, 1, 4, "transfer 12 left");
    // 完整左侧网格应由规则色带晶格保留全部四行。
    CheckGridRows(detect("28.png"), 0, 217, 1, 4, "transfer 28 left");
    // 右侧五列的内部横条不能再通过 directed phase 覆盖正确粗晶格。
    CheckGridRows(detect("43.png"), 1, 208, 1, 4, "transfer 43 right");
    CheckGridRows(detect("56.png"), 1, 217, 1, 4, "transfer 56 right");
    CheckGridRows(detect("57.png"), 1, 217, 1, 4, "transfer 57 right");
    CheckGridRows(detect("58.png"), 1, 217, 1, 4, "transfer 58 right");
    // 左右面板的标准色带 origin 应一致，不能被格内弱横纹移相。
    CheckGridRows(detect("57.png"), 0, 217, 1, 4, "transfer 57 left");
}

void TestTransferRarityBandsDefineRightGridBounds()
{
    const cv::Rect roi(739, 202, 398, 291);
    const auto detect = [&](const char* name) {
        const std::filesystem::path path = std::filesystem::path("agent/cpp-algo/source/IconRecognition/test/input/transfer") / name;
        const cv::Mat image = cv::imread(path.string(), cv::IMREAD_COLOR);
        Check(!image.empty(), "transfer right grid fixture is missing: " + path.string());
        return iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, roi);
    };

    // 色带连续区域的下边界属于 64px cell；不能用单条色带行作为开区间下边界。
    CheckGridOrigin(detect("1.png"), 0, cv::Point(771, 217), 1, cv::Size(5, 4), "transfer 1 right");
    CheckGridOrigin(detect("100.png"), 0, cv::Point(771, 217), 1, cv::Size(5, 4), "transfer 100 right");
    CheckGridOrigin(detect("26.png"), 0, cv::Point(771, 217), 1, cv::Size(5, 4), "transfer 26 right");
    CheckGridOrigin(detect("28.png"), 0, cv::Point(771, 217), 1, cv::Size(5, 4), "transfer 28 right");
    // 工具栏附近的同色像素不能被解释成网格上一行。
    CheckGridOrigin(detect("106.png"), 0, cv::Point(771, 217), 1, cv::Size(5, 4), "transfer 106 right");
    // 重复物品形成的结构假相位不能压过完整的五列色带证据。
    CheckGridOrigin(detect("108.png"), 0, cv::Point(771, 217), 1, cv::Size(5, 4), "transfer 108 right");
}

void TestTransferRarityBandsPreserveCompleteLeftGrid()
{
    const cv::Rect roi(154, 202, 585, 291);
    const auto detect = [&](const char* name) {
        const std::filesystem::path path = std::filesystem::path("agent/cpp-algo/source/IconRecognition/test/input/transfer") / name;
        const cv::Mat image = cv::imread(path.string(), cv::IMREAD_COLOR);
        Check(!image.empty(), "transfer left grid fixture is missing: " + path.string());
        return iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, roi);
    };

    // 少量额外弱色带不能把完整的八列证据整体移动到相邻格之间。
    CheckGridColumns(detect("1.png"), 0, 162, 1, 8, "transfer 1 left");
    CheckGridColumns(detect("10.png"), 0, 161, 1, 8, "transfer 10 left");
    const auto detection_15 = detect("15.png");
    CheckGridColumns(detection_15, 0, 161, 1, 8, "transfer 15 left");
    Check(std::abs(detection_15.grids.front().pitch_x - 69.0) <= 1e-6, "transfer 15 left pitch must preserve structural evidence");
    CheckGridColumns(detect("100.png"), 0, 162, 1, 8, "transfer 100 left");
    const auto detection_103 = detect("103.png");
    CheckGridColumns(detection_103, 0, 162, 1, 8, "transfer 103 left");
    Check(std::abs(detection_103.grids.front().pitch_x - 69.0) <= 1e-6, "transfer 103 left pitch must preserve structural evidence");
}

void TestTransferSparseLeftGridUsesVisibleCardEvidence()
{
    const cv::Mat image = cv::imread("agent/cpp-algo/source/IconRecognition/test/input/transfer/25.png", cv::IMREAD_COLOR);
    Check(!image.empty(), "transfer sparse left fixture is missing");
    const auto detection = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, cv::Rect(154, 202, 585, 291));

    CheckGridOrigin(detection, 0, cv::Point(161, 217), 1, cv::Size(1, 2), "transfer 25 sparse left");
    const auto& layout = detection.grids.front();
    Check(layout.selection_diagnostics.has_value(), "transfer 25 must expose grid selection diagnostics");
    Check(!layout.selection_diagnostics->fallback_used, "transfer 25 must select the trusted rarity candidate");
    Check(layout.pitch_x >= 68.0 && layout.pitch_x <= 70.0, "transfer 25 x pitch must use the formal range");
    Check(layout.pitch_y >= 68.0 && layout.pitch_y <= 70.0, "transfer 25 y pitch must use the formal range");
    Check(layout.selection_diagnostics->maximum_residual <= 2.25, "transfer 25 must not accumulate lattice residual");
}

void TestTransferFullRoiMatchesIndependentSides()
{
    const cv::Rect full_roi(154, 202, 983, 291);
    const cv::Rect left_roi(154, 202, 585, 291);
    const cv::Rect right_roi(739, 202, 398, 291);
    for (const char* name : { "4.png", "41.png", "53.png" }) {
        const std::filesystem::path path = std::filesystem::path("agent/cpp-algo/source/IconRecognition/test/input/transfer") / name;
        const cv::Mat image = cv::imread(path.string(), cv::IMREAD_COLOR);
        Check(!image.empty(), "transfer dual-grid fixture is missing: " + path.string());
        const auto full = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, full_roi);
        const auto left = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, left_roi);
        const auto right = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, right_roi);
        auto split_boxes = GridCellBoxes(left);
        const auto right_boxes = GridCellBoxes(right);
        split_boxes.insert(split_boxes.end(), right_boxes.begin(), right_boxes.end());
        std::ranges::sort(split_boxes, [](const auto& lhs, const auto& rhs) {
            return std::tie(lhs.x, lhs.y, lhs.width, lhs.height) < std::tie(rhs.x, rhs.y, rhs.width, rhs.height);
        });
        Check(GridCellBoxes(full) == split_boxes, "transfer full ROI must equal independent sides: " + std::string(name));
    }
}

void TestRarityBandsRecoverGridFromGlobalEvidence()
{
    cv::Mat image = cv::Mat::zeros(291, 398, CV_8UC3);
    cv::Mat lab(1, 1, CV_8UC3, cv::Scalar(198, 98, 191));
    cv::Mat bgr;
    cv::cvtColor(lab, bgr, cv::COLOR_Lab2BGR);
    const cv::Scalar rarity = bgr.at<cv::Vec3b>(0, 0);
    const std::vector<int> expected_x { 32, 101, 170, 239, 308 };
    const auto paint_band = [&](int bottom, int columns) {
        for (int column = 0; column < columns; ++column) {
            image(cv::Rect(expected_x[column], bottom - 3, 64, 3)).setTo(rarity);
        }
    };

    // 顶部伪色只覆盖部分列，真实第三行只剩部分物品，末行色带被完全遮挡。
    paint_band(11, 3);
    paint_band(79, 5);
    paint_band(148, 3);
    paint_band(217, 5);
    const std::vector<int> coarse_x { 7, 76, 145, 214, 283 };
    const std::vector<int> coarse_y { 15, 84, 153, 222 };
    const auto profile = iconrecognition::detail::TransferProfileFor(iconrecognition::detail::TransferGridVariant::TransferRight);
    const auto fit = iconrecognition::detail::FitRarityGrid(image, coarse_x, coarse_y, profile);

    Check(fit.has_value(), "global rarity evidence must produce a grid fit");
    Check(
        fit->x_starts == expected_x,
        "global rarity evidence must recover the correct x phase: actual=" + std::to_string(fit->x_starts.front())
            + " support=" + std::to_string(fit->supporting_cells) + " strong=" + std::to_string(fit->supporting_strong_cells)
            + " chromatic=" + std::to_string(fit->supporting_chromatic_cells) + " pitch_x=" + std::to_string(fit->pitch_x)
            + " mean=" + std::to_string(fit->mean_coverage));
    Check(fit->origin == 15, "global rarity evidence must recover the band-bottom y origin");
    Check(fit->pitch_x == 69 && fit->pitch == 69, "global rarity evidence must preserve the regular pitch");
    Check(fit->supporting_rows == 3, "obscured final row must be completed from the regular lattice");
    Check(fit->supporting_cells == 13, "partially empty rows must preserve their available cell evidence");
}

void TestPortStoragerRarityBarsAnchorGridOrigins()
{
    const std::filesystem::path path = "agent/cpp-algo/source/IconRecognition/test/input/port_storager/1.png";
    const cv::Mat image = cv::imread(path.string(), cv::IMREAD_COLOR);
    Check(!image.empty(), "port storager grid fixture is missing: " + path.string());
    const auto detection =
        iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::PortStorager, cv::Rect(190, 250, 880, 350));

    CheckGridRows(detection, 0, 261, 2, 5, "port storager 1 left");
    CheckGridRows(detection, 1, 322, 2, 4, "port storager 1 right");
}

void TestRarityUsesBottomEdgeRows()
{
    cv::Mat image = cv::Mat::zeros(100, 100, CV_8UC3);
    cv::Mat lab(1, 1, CV_8UC3, cv::Scalar(198, 98, 191));
    cv::Mat bgr;
    cv::cvtColor(lab, bgr, cv::COLOR_Lab2BGR);
    image(cv::Rect(10, 74, 64, 1)).setTo(bgr.at<cv::Vec3b>(0, 0));

    const auto rarity = iconrecognition::detail::ClassifyRarity(image, cv::Rect(10, 10, 64, 64));
    Check(rarity.rarity == 2, "rarity must use rows around the slot bottom edge");
    Check(std::abs(rarity.coverage - 1.0) <= 1e-6, "rarity coverage must preserve the selected row evidence");
    Check(rarity.row_offset == 0, "rarity row offset must be relative to the slot bottom edge");

    const auto absent = iconrecognition::detail::ClassifyRarity(cv::Mat::zeros(100, 100, CV_8UC3), cv::Rect(10, 10, 64, 64));
    Check(!absent.rarity, "unreliable rarity evidence must not report a rarity");
    Check(std::abs(absent.coverage) <= 1e-6, "unreliable rarity coverage must remain available for diagnostics");
    Check(absent.row_offset == -8, "rarity ties must keep the first row like numpy.argmax");
}

void TestRarityCandidatePassesAreDisjointAndComplete()
{
    std::vector<iconrecognition::detail::PreparedTemplate> templates(5);
    const std::array<int, 5> rarities { 1, 2, 2, 3, 2 };
    for (std::size_t index = 0; index < templates.size(); ++index) {
        templates[index].record.rarity = rarities[index];
    }

    const auto filtered = iconrecognition::detail::BuildRarityCandidatePasses(templates, 2);
    Check(filtered.prefiltered, "available rarity must enable candidate prefiltering");
    Check(filtered.preferred_indices == std::vector<std::size_t> { 1, 2, 4 }, "preferred pass must contain only matching rarity");
    Check(filtered.remaining_indices == std::vector<std::size_t> { 0, 3 }, "fallback pass must exclude preferred candidates");

    std::vector<std::size_t> combined = filtered.preferred_indices;
    combined.insert(combined.end(), filtered.remaining_indices.begin(), filtered.remaining_indices.end());
    std::ranges::sort(combined);
    Check(combined == std::vector<std::size_t> { 0, 1, 2, 3, 4 }, "candidate passes must form one complete partition");

    const auto unavailable = iconrecognition::detail::BuildRarityCandidatePasses(templates, 5);
    Check(!unavailable.prefiltered, "rarity without templates must not enable prefiltering");
    Check(
        unavailable.preferred_indices == std::vector<std::size_t> { 0, 1, 2, 3, 4 } && unavailable.remaining_indices.empty(),
        "unavailable rarity must use one full candidate pass");

    const auto unknown = iconrecognition::detail::BuildRarityCandidatePasses(templates, std::nullopt);
    Check(!unknown.prefiltered, "unknown rarity must not enable prefiltering");
    Check(
        unknown.preferred_indices == std::vector<std::size_t> { 0, 1, 2, 3, 4 } && unknown.remaining_indices.empty(),
        "unknown rarity must use one full candidate pass");
}

void TestRarityRowEvidenceKeepsAllSixChannels()
{
    const std::array<cv::Vec3f, 6> prototypes {
        cv::Vec3f(163.0F, 128.0F, 128.0F), cv::Vec3f(198.0F, 98.0F, 191.0F),  cv::Vec3f(182.0F, 113.0F, 86.0F),
        cv::Vec3f(129.0F, 189.0F, 55.0F),  cv::Vec3f(204.0F, 136.0F, 202.0F), cv::Vec3f(163.0F, 167.0F, 191.0F),
    };
    cv::Mat row(1, 60, CV_32FC3);
    for (int rarity = 0; rarity < 6; ++rarity) {
        for (int x = rarity * 10; x < (rarity + 1) * 10; ++x) {
            row.at<cv::Vec3f>(0, x) = prototypes[rarity];
        }
    }

    const auto evidence = iconrecognition::detail::MeasureRarityRow(row);
    for (std::size_t rarity = 0; rarity < evidence.coverages.size(); ++rarity) {
        Check(
            std::abs(evidence.coverages[rarity] - 1.0 / 6.0) <= 1e-6,
            "rarity row evidence must retain channel " + std::to_string(rarity + 1));
    }
    Check(std::abs(evidence.maximumCoverage() - 1.0 / 6.0) <= 1e-6, "maximum coverage must derive from six channels");
    Check(std::abs(evidence.maximumChromaticCoverage() - 1.0 / 6.0) <= 1e-6, "chromatic maximum must exclude only rarity one");
}

cv::Scalar RarityBgr(int rarity)
{
    const cv::Vec3f prototype = iconrecognition::detail::RarityLabPrototypes().at(static_cast<std::size_t>(rarity - 1));
    cv::Mat lab(1, 1, CV_8UC3, cv::Scalar(prototype[0], prototype[1], prototype[2]));
    cv::Mat bgr;
    cv::cvtColor(lab, bgr, cv::COLOR_Lab2BGR);
    return bgr.at<cv::Vec3b>(0, 0);
}

void TestTrustedRarityRejectsSameColorBackground()
{
    cv::Mat image(120, 220, CV_8UC3, RarityBgr(6));
    const auto background = iconrecognition::detail::DetectTrustedRarityStrips(image, 64);
    Check(background.empty(), "large same-color background must not become a rarity strip");

    image.setTo(cv::Scalar(35, 40, 46));
    image(cv::Rect(20, 70, 64, 3)).setTo(RarityBgr(6));
    image(cv::Rect(120, 70, 64, 3)).setTo(RarityBgr(2));
    const auto trusted = iconrecognition::detail::DetectTrustedRarityStrips(image, 64);
    Check(trusted.size() == 2, "two differently colored cells on one row must both remain available");
    Check(trusted[0].rarity != trusted[1].rarity, "mixed rarity evidence must stay cell-local");
    Check(trusted[0].trusted && trusted[1].trusted, "real narrow bars must pass local contrast and shape constraints");
}

void TestGrayRarityCannotSeedLattice()
{
    cv::Mat image(100, 100, CV_8UC3, cv::Scalar(25, 30, 35));
    image(cv::Rect(18, 60, 64, 3)).setTo(RarityBgr(1));
    const auto strips = iconrecognition::detail::DetectTrustedRarityStrips(image, 64);
    Check(strips.size() == 1 && strips.front().trusted, "gray strip must remain as evidence");
    Check(!strips.front().can_seed_lattice, "gray evidence must require an existing structural candidate");
}

void TestRegularLatticeUsesOneGlobalFloatingPitch()
{
    const std::vector<iconrecognition::detail::LatticeObservation> observations {
        { 12.0, 1.0, true }, { 81.0, 1.0, true }, { 150.0, 1.0, true }, { 220.0, 1.0, true }, { 289.0, 1.0, true },
    };
    const auto fit = iconrecognition::detail::FitRegularAxis(observations, 8, { 68.0, 70.0 }, 69.0);
    Check(fit.has_value(), "regular observations must produce a global axis");
    Check(fit->pitch >= 68.0 && fit->pitch <= 70.0, "fitted pitch must stay inside the formal prior");
    const auto starts = iconrecognition::detail::ProjectRegularAxis(*fit);
    for (std::size_t index = 0; index < starts.size(); ++index) {
        Check(
            starts[index] == cvRound(fit->origin + static_cast<double>(index + fit->minimum_index) * fit->pitch),
            "every integer start must project directly from one global model");
    }
}

void TestRegularLatticeRejectsAccumulatingResiduals()
{
    const std::vector<iconrecognition::detail::LatticeObservation> drifting {
        { 10.0, 1.0, true }, { 78.0, 1.0, true }, { 147.0, 1.0, true }, { 218.0, 1.0, true }, { 291.0, 1.0, true },
    };
    Check(
        !iconrecognition::detail::FitRegularAxis(drifting, 8, { 68.0, 70.0 }, 69.0),
        "a sequence requiring increasing per-cell pitch must be rejected");

    const auto sparse = iconrecognition::detail::FitRegularAxis({ { 31.0, 1.0, true } }, 8, { 68.0, 70.0 }, 69.0);
    Check(sparse && sparse->low_geometry_confidence, "one observation may retain only its direct cell");
    Check(iconrecognition::detail::ProjectRegularAxis(*sparse) == std::vector<int> { 31 }, "one observation must not expand a remote grid");
}

void TestTransfer25ExposesIndependentTrustedRaritySeed()
{
    const cv::Mat image = cv::imread("agent/cpp-algo/source/IconRecognition/test/input/transfer/25.png", cv::IMREAD_COLOR);
    Check(!image.empty(), "transfer sparse rarity fixture is missing");
    const cv::Rect roi(154, 202, 585, 291);
    const auto strips = iconrecognition::detail::DetectTrustedRarityStrips(image(roi), 64);
    Check(
        std::ranges::any_of(
            strips,
            [](const auto& strip) {
                const int cell_top = strip.box.y + strip.box.height - 64;
                return strip.can_seed_lattice && std::abs(strip.box.x - 7) <= 2 && std::abs(cell_top - 15) <= 2;
            }),
        "transfer 25 must expose the left-top chromatic strip independently of the structural winner");

    const auto profile = iconrecognition::detail::TransferProfileFor(iconrecognition::detail::TransferGridVariant::TransferLeft);
    const auto fit = iconrecognition::detail::FitTrustedRarityGrid(image(roi), cv::Rect(0, 0, roi.width, roi.height), profile);
    Check(fit.has_value(), "transfer 25 trusted strips must produce an independent grid candidate");
    Check(
        std::abs(fit->x_starts.front() - 7) <= 2 && std::abs(fit->y_starts.front() - 15) <= 2,
        "transfer 25 trusted candidate must retain the real left-top phase");
}

iconrecognition::detail::PreparedTemplate BuildMatcherFixture()
{
    iconrecognition::detail::PreparedTemplate fixture;
    fixture.record.item_id = "fixture";
    fixture.image = cv::Mat::zeros(8, 8, CV_8UC3);
    for (int y = 0; y < fixture.image.rows; ++y) {
        for (int x = 0; x < fixture.image.cols; ++x) {
            fixture.image.at<cv::Vec3b>(y, x) = cv::Vec3b(
                static_cast<unsigned char>(x * 23 + y),
                static_cast<unsigned char>(y * 29 + x * 2),
                static_cast<unsigned char>((x + y) * 13));
        }
    }
    fixture.mask = cv::Mat(8, 8, CV_8UC1, cv::Scalar(255));
    return fixture;
}

void TestMatcherSearchRadiusIsExplicit()
{
    const auto fixture = BuildMatcherFixture();
    cv::Mat image = cv::Mat::zeros(14, 14, CV_8UC3);
    fixture.image.copyTo(image(cv::Rect(3, 2, 8, 8)));
    const cv::Rect slot(2, 2, 8, 8);

    const auto fixed = iconrecognition::detail::ScoreTemplateAt(image, slot, fixture, 0, {});
    const auto grid = iconrecognition::detail::ScoreTemplateAt(image, slot, fixture, 2, {});

    Check(grid.position == cv::Point(3, 2), "grid search must find the one-pixel offset");
    Check(grid.score > fixed.score, "fixed ROI must not inspect pixels outside the supplied ROI");
}

void TestSubpixelPhasesAreStable()
{
    const auto phases = iconrecognition::detail::PhaseGrid();
    Check(phases.size() == 49, "phase grid must contain 7x7 phases");
    const auto extensions = iconrecognition::detail::BoundaryExtensionPhases({ 0.75, 0.75 });
    Check(extensions.size() == 15, "corner boundary extension must contain 15 unique phases");
    for (std::size_t index = 1; index < extensions.size(); ++index) {
        const auto& left = extensions[index - 1];
        const auto& right = extensions[index];
        Check(left.x < right.x || (left.x == right.x && left.y < right.y), "boundary extension phases must be lexicographically sorted");
    }
}

void TestTemplatePreparationUsesExpectedMasks()
{
    iconrecognition::detail::TemplateRecord record;
    record.item_id = "opaque";
    cv::Mat opaque(32, 32, CV_8UC4, cv::Scalar(10, 20, 30, 255));
    const auto standard = iconrecognition::detail::PrepareStandardTemplate(record, opaque, 64, 230);
    Check(
        std::abs(cv::countNonZero(standard.mask) - 1841) <= 1,
        "opaque standard template must retain the lower mask within rasterization tolerance");

    cv::Mat content(32, 32, CV_8UC4, cv::Scalar(110, 120, 130, 128));
    const auto composite = iconrecognition::detail::PrepareCompositeTemplate(record, opaque, content, 64, 100);
    const cv::Vec3b center = composite.image.at<cv::Vec3b>(32, 32);
    Check(center == cv::Vec3b(60, 70, 80), "composite alpha blending must truncate like NumPy uint8 conversion");
    Check(composite.mask.at<unsigned char>(45, 32) == 255, "overlay alpha must extend beyond the base polygon mask");
}

void TestCatalogBuildsFinalSizeDirectlyFromSourceAssets()
{
    iconrecognition::detail::TemplateCatalog catalog("assets/data/IconRecognition", "assets/resource/image/IconRecognition");
    Check(catalog.initialize(), "template catalog must initialize from public assets");

    const std::array cases {
        std::tuple { "item_copper_ore", 1, 88 },
        std::tuple { "item_weekraid_ore_5_3", 5, 140 },
    };
    for (const auto& [item_id, rarity, target_size] : cases) {
        const auto& templates = catalog.load(target_size);
        const auto prepared = std::ranges::find_if(templates, [&](const auto& templ) { return templ.record.item_id == item_id; });
        Check(prepared != templates.end(), "final-size catalog must contain " + std::string(item_id));

        const auto record = std::ranges::find_if(catalog.records(), [&](const auto& item) { return item.item_id == item_id; });
        Check(record != catalog.records().end(), "catalog record must contain " + std::string(item_id));
        const cv::Mat source = iconrecognition::detail::DecodeBgra(
            std::filesystem::path("assets/resource/image/IconRecognition") / std::to_string(rarity) / (std::string(item_id) + ".png"));
        const auto expected = iconrecognition::detail::PrepareStandardTemplate(*record, source, target_size, 230);
        Check(cv::norm(prepared->image, expected.image, cv::NORM_INF) == 0.0, "template image must be generated directly at final size");
        Check(cv::norm(prepared->mask, expected.mask, cv::NORM_INF) == 0.0, "template mask must be generated directly at final size");
    }
}

void TestIconPathResolutionDoesNotAssumeCatalogRarity()
{
    const std::filesystem::path image_root = "agent/cpp-algo/source/IconRecognition/test/build/generated-icon-resolution-generic";
    const std::filesystem::path expected = image_root / "future-rarity" / "synthetic-fluid.png";
    std::filesystem::create_directories(expected.parent_path());
    Check(cv::imwrite(expected.string(), cv::Mat(8, 8, CV_8UC4, cv::Scalar(10, 20, 30, 255))), "unable to write synthetic icon");

    Check(
        iconrecognition::detail::ResolveIconPath(image_root, "synthetic-fluid") == expected,
        "icon path resolution must search resource folders independently of item rarity");
}

void TestCatalogConcurrentLoadIsStable()
{
    iconrecognition::detail::TemplateCatalog catalog("assets/data/IconRecognition", "assets/resource/image/IconRecognition");
    Check(catalog.initialize(), "concurrent catalog must initialize from public assets");

    std::barrier start(3);
    std::array<std::size_t, 2> counts {};
    std::array<std::exception_ptr, 2> errors {};
    std::array<std::thread, 2> workers;
    for (std::size_t index = 0; index < workers.size(); ++index) {
        workers[index] = std::thread([&, index] {
            start.arrive_and_wait();
            try {
                counts[index] = catalog.load(72).size();
            }
            catch (...) {
                errors[index] = std::current_exception();
            }
        });
    }
    start.arrive_and_wait();
    for (auto& worker : workers) {
        worker.join();
    }
    Check(errors[0] == nullptr && errors[1] == nullptr, "concurrent catalog load must not throw");
    Check(counts[0] == catalog.records().size() && counts[1] == catalog.records().size(), "concurrent catalog load must be complete");
}

void TestCatalogFailedLoadDoesNotPoisonCache()
{
    const std::filesystem::path fixture = "agent/cpp-algo/source/IconRecognition/test/build/generated-catalog-failure";
    std::filesystem::remove_all(fixture);
    const auto data_root = fixture / "data";
    const auto image_root = fixture / "images";
    std::filesystem::create_directories(data_root);
    std::filesystem::create_directories(image_root / "1");
    std::ofstream(data_root / "recognition_items.json", std::ios::binary | std::ios::trunc)
        << R"({"missing_item":{"name":"missing","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"missing_item","fluidIconId":""}})";
    Check(
        cv::imwrite((image_root / "1" / "missing_item.png").string(), cv::Mat(127, 127, CV_8UC4, cv::Scalar(10, 20, 30, 255))),
        "unable to write invalid template fixture");

    iconrecognition::detail::TemplateCatalog catalog(data_root, image_root);
    Check(catalog.initialize(), "failing catalog fixture must initialize");
    for (int attempt = 0; attempt < 2; ++attempt) {
        bool rejected = false;
        try {
            static_cast<void>(catalog.load(64));
        }
        catch (const std::runtime_error&) {
            rejected = true;
        }
        Check(rejected, "failed template loads must not leave a reusable partial cache");
    }
}

void TestDecodeBgraRejectsNonStandardSourceSizes()
{
    const std::filesystem::path output_root = "agent/cpp-algo/source/IconRecognition/test/build/generated-icon-validation";
    std::filesystem::create_directories(output_root);

    const auto write_icon = [&](const std::string& name, int width, int height) {
        const auto path = output_root / (name + ".png");
        const cv::Mat image(height, width, CV_8UC4, cv::Scalar(10, 20, 30, 255));
        Check(cv::imwrite(path.string(), image), "unable to write generated icon fixture: " + name);
        return path;
    };
    Check(
        iconrecognition::detail::DecodeBgra(write_icon("valid-128", 128, 128)).size() == cv::Size(128, 128),
        "128px icon must be accepted");
    Check(
        iconrecognition::detail::DecodeBgra(write_icon("valid-256", 256, 256)).size() == cv::Size(256, 256),
        "256px icon must be accepted");

    const auto check_rejected = [&](const std::filesystem::path& path, const std::string& message) {
        try {
            static_cast<void>(iconrecognition::detail::DecodeBgra(path));
        }
        catch (const std::runtime_error&) {
            return;
        }
        throw std::runtime_error(message);
    };
    check_rejected(write_icon("invalid-rectangle", 128, 256), "non-square source icon must be rejected");
    check_rejected(write_icon("invalid-power", 127, 127), "non-power-of-two source icon must be rejected");
}

void TestArbitrarySquareRoiUsesItsFinalSize()
{
    constexpr int kRoiSize = 72;
    const cv::Rect roi(40, 30, kRoiSize, kRoiSize);
    cv::Mat image = cv::Mat::zeros(160, 180, CV_8UC3);
    const cv::Mat source = iconrecognition::detail::DecodeBgra("assets/resource/image/IconRecognition/1/item_copper_ore.png");
    iconrecognition::detail::ResizeAndCenter(source, kRoiSize).copyTo(image(roi));

    iconrecognition::IconRecognizer recognizer("assets/data/IconRecognition");
    Check(recognizer.initialize(), "arbitrary ROI recognizer must initialize from public assets");
    iconrecognition::RecognitionRequest request;
    request.grid_type = iconrecognition::GridType::SingleRoi;
    request.roi = roi;
    request.candidates.item_ids = { "item_copper_ore" };
    const auto result = recognizer.recognize(image, request);
    Check(result.matched && result.matches.size() == 1, "72px square ROI must recognize one item");
    Check(result.matches.front().item.item_id == "item_copper_ore", "72px square ROI must preserve the requested item id");
    Check(result.matches.front().cell_box == roi, "arbitrary square ROI must be returned as the temporary cell box");
    Check(result.matches.front().item_box.size() == cv::Size(kRoiSize, kRoiSize), "arbitrary ROI template must use the final ROI size");
}

void TestRepresentativeMatcherBreakdown()
{
    iconrecognition::detail::TemplateCatalog catalog("assets/data/IconRecognition", "assets/resource/image/IconRecognition");
    Check(catalog.initialize(), "template catalog initialization failed");
    const auto& templates = catalog.load(64);
    const auto found = std::ranges::find_if(templates, [](const auto& templ) { return templ.record.item_id == "item_drop_klbuds_1"; });
    Check(found != templates.end(), "representative matcher template is missing");
    const cv::Mat image = cv::imread("agent/cpp-algo/source/IconRecognition/test/input/transfer/11.png");
    const cv::Rect slot(771, 319, 64, 64);
    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, cv::Rect(154, 202, 983, 291));
    const auto actual_cell = std::ranges::find_if(grid.cells, [&](const auto& cell) {
        return std::abs(cell.cell_box.x - slot.x) <= 1 && std::abs(cell.cell_box.y - slot.y) <= 1 && cell.cell_box.width == 64
               && cell.cell_box.height == 64;
    });
    Check(actual_cell != grid.cells.end(), "representative matcher grid cell is missing");
    Check(
        std::abs(actual_cell->cell_box.x - slot.x) <= 1 && std::abs(actual_cell->cell_box.y - slot.y) <= 1
            && actual_cell->cell_box.size() == slot.size(),
        "representative matcher grid cell mismatch: " + std::to_string(actual_cell->cell_box.x) + ","
            + std::to_string(actual_cell->cell_box.y));
    const auto baseline = iconrecognition::detail::ScoreTemplateAt(image, slot, *found, 2, {});
    const auto phase = iconrecognition::detail::ScoreTemplateAt(image, slot, *found, 2, { 0.5, -0.5 });
    Check(baseline.tm_score >= 0.75, "representative tm score is unexpectedly low: " + std::to_string(baseline.tm_score));
    Check(baseline.color_score >= 0.85, "representative color score is unexpectedly low: " + std::to_string(baseline.color_score));
    Check(
        cv::norm(baseline.position - slot.tl()) <= 2.0,
        "representative position mismatch: " + std::to_string(baseline.position.x) + "," + std::to_string(baseline.position.y));
    Check(phase.tm_score > baseline.tm_score, "subpixel phase must improve representative tm score");
    Check(phase.color_score >= baseline.color_score, "subpixel phase must preserve representative color score");
    Check(
        cv::norm(phase.position - slot.tl()) <= 2.0,
        "phase position mismatch: " + std::to_string(phase.position.x) + "," + std::to_string(phase.position.y));

    std::vector<std::pair<double, std::string>> ranked;
    for (const auto& templ : templates) {
        if (templ.record.storage_kind != "Normal") {
            continue;
        }
        const auto score = iconrecognition::detail::ScoreTemplateAt(image, slot, templ, 2, {});
        ranked.emplace_back(score.score, templ.record.item_id);
    }
    std::ranges::sort(ranked, [](const auto& left, const auto& right) {
        return left.first > right.first || (left.first == right.first && left.second < right.second);
    });
    Check(
        ranked.front().second == "item_drop_klbuds_1",
        "representative top item mismatch: " + ranked.front().second + " score=" + std::to_string(ranked.front().first));
    Check(ranked.front().first >= 0.75, "representative top score is unexpectedly low: " + std::to_string(ranked.front().first));
}

} // namespace

int main()
{
    try {
        TestLowerExtendedMaskSnapshots();
        TestShipmentQuantityBarThreshold();
        TestForegroundTextureUsesContentInsets();
        TestStructureFeatureModuleContract();
        TestGridGeometryModuleContract();
        TestTradeGridUsesCardBoundariesForVerticalPhase();
        TestTransferRegionPartitionKeepsUndetectedOuterColumns();
        TestCreditTradeGridUsesDimCardStructures();
        TestTransferProfileModuleContract();
        TestRarityRowEvidenceKeepsAllSixChannels();
        TestTrustedRarityRejectsSameColorBackground();
        TestGrayRarityCannotSeedLattice();
        TestRegularLatticeUsesOneGlobalFloatingPitch();
        TestRegularLatticeRejectsAccumulatingResiduals();
        TestTransfer25ExposesIndependentTrustedRaritySeed();
        TestRarityBandsRecoverGridFromGlobalEvidence();
        TestTransferFullRoiMatchesIndependentSides();
        TestTransferRarityBandsPreserveCompleteLeftGrid();
        TestTransferSparseLeftGridUsesVisibleCardEvidence();
        TestTransferRarityBandsDefineRightGridBounds();
        TestTransferRarityBarsAnchorGridOrigins();
        TestPortStoragerRarityBarsAnchorGridOrigins();
        TestRarityUsesBottomEdgeRows();
        TestRarityCandidatePassesAreDisjointAndComplete();
        TestMatcherSearchRadiusIsExplicit();
        TestSubpixelPhasesAreStable();
        TestTemplatePreparationUsesExpectedMasks();
        TestCatalogBuildsFinalSizeDirectlyFromSourceAssets();
        TestIconPathResolutionDoesNotAssumeCatalogRarity();
        TestCatalogConcurrentLoadIsStable();
        TestCatalogFailedLoadDoesNotPoisonCache();
        TestDecodeBgraRejectsNonStandardSourceSizes();
        TestArbitrarySquareRoiUsesItsFinalSize();
        TestRepresentativeMatcherBreakdown();
        std::cout << "IconRecognition small algorithm tests passed\n";
        return 0;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}

#endif
