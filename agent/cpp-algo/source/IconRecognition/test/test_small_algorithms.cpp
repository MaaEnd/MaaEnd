#ifdef ICON_RECOGNITION_TEST_MAIN

#include "../IconRecognizer.h"
#include "../detail/ForegroundTexture.h"
#include "../detail/GridDetector.h"
#include "../detail/GridFeatures.h"
#include "../detail/GridGeometry.h"
#include "../detail/GridProfiles.h"
#include "../detail/IconMatcher.h"
#include "../detail/MaskPolicy.h"
#include "../detail/RarityClassifier.h"
#include "../detail/SubpixelMatcher.h"
#include "../detail/TemplateCatalog.h"
#include "../detail/TemplateTypes.h"

#include <array>
#include <barrier>
#include <cmath>
#include <cstdlib>
#include <exception>
#include <filesystem>
#include <iostream>
#include <stdexcept>
#include <string>
#include <thread>

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

void TestTransferVerticalPhaseUsesDirectedCellBoundaries()
{
    constexpr int kCellSize = 64;
    constexpr int kPitch = 69;
    constexpr int kTruePhase = 37;
    constexpr int kFalsePhase = 4;
    std::vector<float> signed_boundary(291, 0.0F);

    for (int row = 0; row < 3; ++row) {
        const int start = kTruePhase + row * kPitch;
        signed_boundary[start] = 0.65F;
        signed_boundary[start + kCellSize] = -0.65F;

        const int false_start = kFalsePhase + row * kPitch;
        signed_boundary[false_start] = -1.0F;
        signed_boundary[false_start + kCellSize] = 1.0F;
    }
    signed_boundary[kTruePhase + 3 * kPitch] = 0.65F;

    const auto phase = iconrecognition::detail::FitDirectedCellPhase(signed_boundary, kCellSize, kPitch);
    Check(phase && *phase == kTruePhase, "transfer vertical phase must follow directed cell boundaries");
}

void TestTransferVerticalPhaseRejectsMissingEvidence()
{
    const auto phase = iconrecognition::detail::FitDirectedCellPhase(std::vector<float>(291, 0.0F), 64, 69);
    Check(!phase, "transfer vertical phase must preserve the coarse fallback when directed evidence is missing");
}

void TestCellPhaseDistanceWrapsWithinOnePitch()
{
    Check(iconrecognition::detail::CellPhaseDistance(2, 67, 69) == 4, "cell phase distance must wrap across the pitch boundary");
    Check(iconrecognition::detail::CellPhaseDistance(10, 40, 69) == 30, "cell phase distance must retain a distinct phase");
}

void TestDirectedCellPhasePolicyRejectsDistantWideGridTexture()
{
    Check(
        iconrecognition::detail::ShouldUseDirectedCellPhase(true, 5, 8, 36, 69),
        "narrow transfer grids must allow directed recovery beyond local refinement");
    Check(
        !iconrecognition::detail::ShouldUseDirectedCellPhase(true, 6, 8, 36, 69),
        "wide transfer grids must reject a distant phase caused by internal texture");
    Check(
        iconrecognition::detail::ShouldUseDirectedCellPhase(true, 8, 2, 67, 69),
        "wide transfer grids must accept directed boundaries inside local refinement");
    Check(
        iconrecognition::detail::ShouldUseDirectedCellPhase(false, 7, 8, 36, 69),
        "port grids must use directed recovery when the legacy local refinement cannot reach it");
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
    Check(
        regions[0].width >= detected_left.x + 4 * 69,
        "left transfer search region must retain room for a weak outer column");
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
    const cv::Mat image = cv::imread("agent/cpp-algo/source/IconRecognition/test/input/62.png");
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

    const cv::Mat port_image = cv::imread("agent/cpp-algo/source/IconRecognition/test/input/70.png");
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
    const auto found = std::ranges::find_if(templates, [](const auto& templ) { return templ.record.item_id == "item_drop_lbroshan_1"; });
    Check(found != templates.end(), "representative matcher template is missing");
    const cv::Mat image = cv::imread("agent/cpp-algo/source/IconRecognition/test/input/11.png");
    const cv::Rect slot(506, 354, 64, 64);
    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, cv::Rect(154, 202, 983, 291));
    const auto actual_cell = std::ranges::find_if(grid.cells, [](const auto& cell) {
        return std::abs(cell.cell_box.x - 506) <= 1 && std::abs(cell.cell_box.y - 354) <= 1 && cell.cell_box.width == 64
               && cell.cell_box.height == 64;
    });
    Check(actual_cell != grid.cells.end(), "representative matcher grid cell is missing");
    Check(
        actual_cell->cell_box == slot,
        "representative matcher grid cell mismatch: " + std::to_string(actual_cell->cell_box.x) + ","
            + std::to_string(actual_cell->cell_box.y));
    const auto baseline = iconrecognition::detail::ScoreTemplateAt(image, slot, *found, 2, {});
    const auto phase = iconrecognition::detail::ScoreTemplateAt(image, slot, *found, 2, { 0.5, -0.5 });
    Check(std::abs(baseline.tm_score - 0.7952435613) <= 0.005, "representative tm score mismatch: " + std::to_string(baseline.tm_score));
    Check(
        std::abs(baseline.color_score - 0.9079354529) <= 0.005,
        "representative color score mismatch: " + std::to_string(baseline.color_score));
    Check(
        baseline.position == cv::Point(505, 355),
        "representative position mismatch: " + std::to_string(baseline.position.x) + "," + std::to_string(baseline.position.y));
    Check(std::abs(phase.tm_score - 0.9105755687) <= 0.005, "phase tm score mismatch: " + std::to_string(phase.tm_score));
    Check(std::abs(phase.color_score - 0.9345279469) <= 0.005, "phase color score mismatch: " + std::to_string(phase.color_score));
    Check(
        phase.position == cv::Point(505, 355),
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
        ranked.front().second == "item_drop_lbroshan_1",
        "representative top item mismatch: " + ranked.front().second + " score=" + std::to_string(ranked.front().first));
    Check(
        std::abs(ranked.front().first - 0.8121473450) <= 0.005,
        "representative top score mismatch: " + std::to_string(ranked.front().first));
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
        TestTransferVerticalPhaseUsesDirectedCellBoundaries();
        TestTransferVerticalPhaseRejectsMissingEvidence();
        TestCellPhaseDistanceWrapsWithinOnePitch();
        TestDirectedCellPhasePolicyRejectsDistantWideGridTexture();
        TestTransferRegionPartitionKeepsUndetectedOuterColumns();
        TestCreditTradeGridUsesDimCardStructures();
        TestTransferProfileModuleContract();
        TestRarityUsesBottomEdgeRows();
        TestMatcherSearchRadiusIsExplicit();
        TestSubpixelPhasesAreStable();
        TestTemplatePreparationUsesExpectedMasks();
        TestCatalogBuildsFinalSizeDirectlyFromSourceAssets();
        TestIconPathResolutionDoesNotAssumeCatalogRarity();
        TestCatalogConcurrentLoadIsStable();
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
