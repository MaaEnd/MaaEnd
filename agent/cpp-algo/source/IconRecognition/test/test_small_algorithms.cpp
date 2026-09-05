#ifdef ICON_RECOGNITION_TEST_MAIN

#include "../IconRecognizer.h"
#include "../detail/CandidateSelector.h"
#include "../detail/DisabledIcon.h"
#include "../detail/EdgeOcclusion.h"
#include "../detail/ForegroundTexture.h"
#include "../detail/GridAnchors.h"
#include "../detail/GridDetector.h"
#include "../detail/GridFeatures.h"
#include "../detail/GridGeometry.h"
#include "../detail/GridProfiles.h"
#include "../detail/IconMatcher.h"
#include "../detail/MaskPolicy.h"
#include "../detail/RarityCandidates.h"
#include "../detail/RarityClassifier.h"
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

template <typename Request>
concept HasPublicGridScale = requires(Request request) { request.grid_scale; };

static_assert(
    !HasPublicGridScale<iconrecognition::RecognitionRequest>,
    "RecognitionRequest must not expose a caller-controlled grid_scale");

void Check(bool condition, const std::string& message)
{
    if (!condition) {
        throw std::runtime_error(message);
    }
}

cv::Scalar RarityBgr(int rarity);

iconrecognition::detail::PreparedTemplate CandidateTemplate(
    std::string item_id,
    std::string storage_kind,
    std::string category_type,
    std::string icon_id = {},
    std::string fluid_icon_id = {})
{
    if (icon_id.empty()) {
        icon_id = item_id;
    }
    const std::string name_key = "iconRecognition.name." + item_id;
    return iconrecognition::detail::PreparedTemplate {
        .record =
            iconrecognition::detail::TemplateRecord {
                .item_id = std::move(item_id),
                .name_key = name_key,
                .storage_kind = std::move(storage_kind),
                .category_type = std::move(category_type),
                .icon_id = std::move(icon_id),
                .fluid_icon_id = std::move(fluid_icon_id),
            },
    };
}

std::vector<std::string> CandidateIDs(const std::vector<iconrecognition::detail::PreparedTemplate>& templates)
{
    std::vector<std::string> result;
    result.reserve(templates.size());
    std::ranges::transform(templates, std::back_inserter(result), [](const auto& templ) { return templ.record.item_id; });
    return result;
}

void TestCandidateSelectionUsesDocumentedSetOrder()
{
    const std::vector all {
        CandidateTemplate("ore", "Normal", "Ore"),
        CandidateTemplate("product", "Normal", "Product"),
        CandidateTemplate("special", "Isolate", "SpecialItem"),
    };
    iconrecognition::CandidateFilter candidates;
    candidates.item_ids = { "ore", "special" };
    candidates.item_filters = { "Normal:*" };
    candidates.additional_item_filters = { "Isolate:*" };
    candidates.excluded_item_ids = { "ore" };

    Check(
        CandidateIDs(iconrecognition::detail::SelectCandidateTemplates(all, candidates, { "Normal:*" }))
            == std::vector<std::string> { "special" },
        "candidate selection must intersect ids, append filters, then apply exclusions");
}

void TestCandidateSelectionWithoutIdsSkipsIntersection()
{
    const std::vector all {
        CandidateTemplate("ore", "Normal", "Ore"),
        CandidateTemplate("product", "Normal", "Product"),
        CandidateTemplate("special", "Isolate", "SpecialItem"),
    };
    iconrecognition::CandidateFilter candidates;
    candidates.item_filters = { "Normal:*" };
    candidates.additional_item_filters = { "Isolate:*" };
    candidates.excluded_item_ids = { "product" };

    Check(
        CandidateIDs(iconrecognition::detail::SelectCandidateTemplates(all, candidates, { "ValuableDepot:*" }))
            == std::vector<std::string>({ "ore", "special" }),
        "candidate selection without ids must retain every base-filter match before append and exclusion");
}

void TestCandidateSelectionTreatsDuplicateValuesAsOne()
{
    const std::vector all {
        CandidateTemplate("ore", "Normal", "Ore"),
        CandidateTemplate("special", "Isolate", "SpecialItem"),
    };
    iconrecognition::CandidateFilter candidates;
    candidates.item_ids = { "ore", "ore" };
    candidates.item_filters = { "Normal:*", "Normal:*" };
    candidates.additional_item_filters = { "Isolate:*", "Isolate:*" };
    candidates.excluded_item_ids = { "ore", "ore" };

    Check(
        CandidateIDs(iconrecognition::detail::SelectCandidateTemplates(all, candidates, { "Normal:*" }))
            == std::vector<std::string> { "special" },
        "duplicate candidate values must behave as if each value was provided once");

    iconrecognition::detail::ValidateCandidateFilterList({ "Isolate:*", "Isolate:*" }, "item_recheck_filters");
}

void TestCandidateSelectionDeduplicatesCompositeIconIdentity()
{
    const std::vector all {
        CandidateTemplate("representative", "Normal", "Product", "shared", "fluid_a"),
        CandidateTemplate("alias", "Normal", "Product", "shared", "fluid_a"),
        CandidateTemplate("other_fluid", "Normal", "Product", "shared", "fluid_b"),
    };
    const iconrecognition::CandidateFilter candidates;

    const auto selected = iconrecognition::detail::SelectCandidateTemplates(all, candidates, { "Normal:*" });
    Check(
        CandidateIDs(selected) == std::vector<std::string>({ "representative", "other_fluid" }),
        "candidate selection must deduplicate by iconId and fluidIconId after filtering");
    Check(selected.front().record.aliases.size() == 1, "shared composite icon must retain one alias");
    Check(selected.front().record.aliases.front().item_id == "alias", "shared composite icon alias id mismatch");
    Check(
        selected.front().record.aliases.front().name_key == "iconRecognition.name.alias",
        "shared composite icon alias name mismatch");

    const auto recheck = iconrecognition::detail::SelectCandidateTemplates(all, candidates, { "Normal:*" }, false);
    Check(recheck.size() == 2, "recheck candidate selection must use the same icon identity deduplication");
    Check(recheck.front().record.aliases.empty(), "recheck candidate selection must not retain aliases");
}

void TestCandidateSelectionExactIdRetainsFilteredAliases()
{
    const std::vector all {
        CandidateTemplate("base_alias", "Normal", "Product", "shared"),
        CandidateTemplate("additional_alias", "Isolate", "SpecialItem", "shared"),
        CandidateTemplate("outside_filters", "ValuableDepot", "CommercialItem", "shared"),
        CandidateTemplate("excluded_alias", "Normal", "Product", "shared"),
        CandidateTemplate("requested", "Normal", "Product", "shared"),
    };
    iconrecognition::CandidateFilter candidates;
    candidates.item_ids = { "requested" };
    candidates.item_filters = { "Normal:*" };
    candidates.additional_item_filters = { "Isolate:*" };
    candidates.excluded_item_ids = { "excluded_alias" };

    const auto requested = iconrecognition::detail::SelectCandidateTemplates(all, candidates, { "ValuableDepot:*" });
    Check(requested.size() == 1, "exact item selection must keep one shared-icon representative");
    Check(requested.front().record.item_id == "requested", "exact item selection must return the requested item id");
    const auto& requested_aliases = requested.front().record.aliases;
    Check(
        requested_aliases.size() == 2 && requested_aliases.front().item_id == "base_alias"
            && requested_aliases.front().name_key == "iconRecognition.name.base_alias"
            && requested_aliases.back().item_id == "additional_alias"
            && requested_aliases.back().name_key == "iconRecognition.name.additional_alias",
        "exact item aliases must come from item_filters and additional_item_filters after exclusions");

    candidates.item_ids = { "base_alias" };
    const auto alias_requested = iconrecognition::detail::SelectCandidateTemplates(all, candidates, { "ValuableDepot:*" });
    Check(alias_requested.front().record.item_id == "base_alias", "requesting the alias id must make it the representative");
    Check(
        alias_requested.front().record.aliases.size() == 2
            && alias_requested.front().record.aliases.front().item_id == "additional_alias"
            && alias_requested.front().record.aliases.back().item_id == "requested",
        "requesting either shared-icon id must return the other filtered ids as aliases");
}

void TestCandidateSelectionRejectsInvalidRequests()
{
    const std::vector all {
        CandidateTemplate("ore", "Normal", "Ore"),
        CandidateTemplate("special", "Isolate", "SpecialItem"),
    };
    const auto require_invalid = [&](iconrecognition::CandidateFilter candidates, std::string_view expected) {
        try {
            static_cast<void>(iconrecognition::detail::SelectCandidateTemplates(all, candidates, { "Normal:*" }));
        }
        catch (const std::invalid_argument& error) {
            Check(
                std::string_view(error.what()).find(expected) != std::string_view::npos,
                "candidate validation error must identify the invalid field or value");
            return;
        }
        throw std::runtime_error("invalid candidate request must be rejected");
    };

    iconrecognition::CandidateFilter unknown_id;
    unknown_id.item_ids = { "missing" };
    require_invalid(std::move(unknown_id), "missing");

    iconrecognition::CandidateFilter unknown_excluded;
    unknown_excluded.excluded_item_ids = { "missing" };
    require_invalid(std::move(unknown_excluded), "missing");

    iconrecognition::CandidateFilter malformed_filter;
    malformed_filter.additional_item_filters = { "invalid" };
    require_invalid(std::move(malformed_filter), "additional_item_filters");

    iconrecognition::CandidateFilter empty_result;
    empty_result.item_ids = { "special" };
    empty_result.item_filters = { "Normal:*" };
    require_invalid(std::move(empty_result), "no candidate templates");
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

void TestShipmentQuantityBarThresholdScalesWithCellArea()
{
    cv::Mat slot = cv::Mat::zeros(80, 80, CV_8UC3);
    slot(cv::Rect(0, 0, 24, 20)).setTo(cv::Scalar(0, 220, 220));
    slot(cv::Rect(0, 20, 64, 5)).setTo(cv::Scalar(0, 220, 220));
    Check(iconrecognition::detail::HasShipmentTopBar(slot), "80px shipment cells must inspect the full proportional top band");

    slot.setTo(cv::Scalar(0, 0, 0));
    slot(cv::Rect(0, 0, 30, 20)).setTo(cv::Scalar(0, 220, 220));
    Check(
        !iconrecognition::detail::HasShipmentTopBar(slot),
        "80px shipment cells must scale the minimum yellow-pixel evidence by top-band area");
}

void TestShipmentTopBarMaskScalesWithCellHeight()
{
    for (const auto& [cell_size, expected_height] : std::array<std::pair<int, int>, 2> {
             std::pair { 64, 20 },
             std::pair { 80, 25 },
         }) {
        cv::Mat mask(cell_size, cell_size, CV_8UC1, cv::Scalar(255));
        iconrecognition::detail::ApplyShipmentTopBarMask(mask);
        Check(
            cv::countNonZero(mask.rowRange(0, expected_height)) == 0,
            "shipment top-bar mask must clear the calibrated proportional band at cell size " + std::to_string(cell_size));
        Check(
            cv::countNonZero(mask.row(expected_height)) == cell_size,
            "shipment top-bar mask must retain icon pixels immediately below the calibrated band at cell size "
                + std::to_string(cell_size));
    }
}

void TestValuablesPortraitMaskScalesWithCellSize()
{
    cv::Mat mask(120, 120, CV_8UC1, cv::Scalar(255));
    iconrecognition::detail::ApplyValuablesWeaponPortraitMask(mask);
    Check(mask.at<unsigned char>(19, 101) == 0, "scaled valuables portrait center must be excluded");
    Check(mask.at<unsigned char>(19, 118) == 0, "scaled valuables portrait radius must exclude the right edge");
}

void TestMaskDiagnosticsDescribeComposedPolicies()
{
    using iconrecognition::detail::DescribeMaskKind;
    using iconrecognition::detail::MaskKind;
    Check(DescribeMaskKind(MaskKind::LowerExtended, false) == "lower_extended", "standard mask diagnostic mismatch");
    Check(DescribeMaskKind(MaskKind::LowerExtended, true) == "composite_union", "composite mask diagnostic mismatch");
    Check(
        DescribeMaskKind(MaskKind::ShipmentTopBar, true) == "composite_union+shipment_top_bar",
        "composite shipment diagnostic must retain both applied masks");
    Check(
        DescribeMaskKind(MaskKind::ValuablesWeapon, true) == "composite_union+valuables_weapon",
        "composite valuables diagnostic must retain both applied masks");
}

void TestValuablesPortraitDetectionDoesNotDependOnTemplateMask()
{
    cv::Mat slot = cv::Mat::zeros(96, 96, CV_8UC3);
    cv::circle(slot, cv::Point(81, 15), 18, cv::Scalar(255, 255, 255), 2, cv::LINE_AA);
    Check(
        iconrecognition::detail::HasValuablesWeaponPortrait(slot),
        "valuables portrait detection must depend only on the slot image");

    slot.setTo(cv::Scalar(0, 0, 0));
    Check(
        !iconrecognition::detail::HasValuablesWeaponPortrait(slot),
        "valuables portrait detection must reject slots without a portrait circle");

    const cv::Mat tiny_slot = cv::Mat::zeros(1, 1, CV_8UC3);
    Check(
        !iconrecognition::detail::HasValuablesWeaponPortrait(tiny_slot),
        "valuables portrait detection must reject a slot whose scaled detection rectangle is empty");
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

void TestForegroundTextureUsesNativeLargerCell()
{
    cv::Mat image = cv::Mat::zeros(80, 80, CV_8UC3);
    for (int y = 6; y < 72; ++y) {
        for (int x = 6; x < 24; ++x) {
            const unsigned char value = ((x + y) % 2 == 0) ? 0 : 255;
            image.at<cv::Vec3b>(y, x) = cv::Vec3b(value, value, value);
        }
    }
    const auto score = iconrecognition::detail::ForegroundTextureScore(image, cv::Rect(0, 0, 80, 80), iconrecognition::GridType::Transfer);
    Check(score.has_value(), "native larger cells must use the existing texture calculation");
    Check(*score > 10.0, "native larger cell texture fixture must remain above the low-texture threshold");
}

void TestForegroundTextureInsetsStayGridSpecific()
{
    // 非均匀纹理让裁剪边界的任一变化都影响分数，同时覆盖 64px 和 80px 原生格子。
    for (const int size : { 64, 80 }) {
        cv::Mat image(size, size, CV_8UC3);
        cv::RNG random(0);
        random.fill(image, cv::RNG::UNIFORM, 0, 256);
        const cv::Rect cell(0, 0, size, size);
        const auto transfer = iconrecognition::detail::ForegroundTextureScore(image, cell, iconrecognition::GridType::Transfer);
        const auto port = iconrecognition::detail::ForegroundTextureScore(image, cell, iconrecognition::GridType::PortStorager);
        Check(
            transfer == iconrecognition::detail::LaplacianVariance(image, cv::Rect(4, 4, size - 8, size - 8)),
            "transfer texture must inset all four sides by four pixels");
        Check(
            port == iconrecognition::detail::LaplacianVariance(image, cv::Rect(6, 6, size - 12, size - 14)),
            "port storager texture must retain its existing insets");
    }
    Check(iconrecognition::detail::kDefaultLowTextureThreshold == 10.0, "default low-texture threshold must remain ten");
}

void TestTransferPanelIntersections()
{
    using namespace iconrecognition::detail;
    const cv::Rect full(0, 0, 1280, 720);
    const auto win32 = TransferPanelRegionsFor(kWin32ControllerGridScale, full);
    Check(win32[0].search_roi == cv::Rect(160, 205, 547, 286), "Win32 left outer bounds must match the reviewed panel");
    Check(win32[1].texture_roi == cv::Rect(770, 215, 341, 266), "Win32 right trusted bounds must exclude the fade");
    const auto adb = TransferPanelRegionsFor(kAdbControllerGridScale, full);
    Check(adb[0].texture_roi == cv::Rect(41, 182, 683, 325), "ADB left bounds must use the native controller profile");
    Check(adb[1].search_roi == cv::Rect(803, 171, 426, 346), "ADB right outer bounds must retain the bottom partial row");
    const cv::Rect restricted(820, 230, 300, 200);
    const auto single = TransferPanelRegionsFor(kAdbControllerGridScale, restricted);
    Check(single[0].search_roi.empty() && single[0].texture_roi.empty(), "right-only ROI must not create a left panel");
    Check(single[1].search_roi == restricted && single[1].texture_roi == restricted, "profile must never expand the caller ROI");
    const auto gap = TransferPanelRegionsFor(kWin32ControllerGridScale, cv::Rect(710, 220, 50, 200));
    Check(gap[0].search_roi.empty() && gap[1].search_roi.empty(), "the gap between panels must not become a search region");
}

void TestTransferTextureClipsUiWithoutRecroppingCell()
{
    using namespace iconrecognition::detail;
    const cv::Rect cell(1149, 453, 80, 80);
    const auto panels = TransferPanelRegionsFor(kAdbControllerGridScale, cv::Rect(0, 0, 1280, 720));
    const cv::Rect trusted = panels[1].texture_roi;
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(30, 30, 30));
    cv::RNG random(0);
    random.fill(image(cv::Rect(1149, 517, 80, 16)), cv::RNG::UNIFORM, 0, 256);
    Check(!IsLowTexture(image, cell, iconrecognition::GridType::Transfer), "the fixture must expose the old bottom UI contamination");
    Check(
        IsLowTexture(image, cell, iconrecognition::GridType::Transfer, kDefaultLowTextureThreshold, trusted),
        "UI outside the trusted panel must not prevent empty detection");
    // 紧贴可信下界的物品纹理必须保留，不能在求交后的新边界再内缩一次。
    random.fill(image(cv::Rect(1153, 507, 72, 4)), cv::RNG::UNIFORM, 0, 256);
    const auto score = ForegroundTextureScore(image, cell, iconrecognition::GridType::Transfer, trusted);
    Check(score == LaplacianVariance(image, cv::Rect(1153, 457, 72, 54)), "native ADB texture must intersect after four-pixel cell insets");
    Check(score && *score > kDefaultLowTextureThreshold, "visible item texture in the partial row must not be rejected");
    Check(
        !ForegroundTextureScore(image, cell, iconrecognition::GridType::Transfer, cv::Rect()).has_value(),
        "invisible content must remain unknown");
    Check(
        !IsLowTexture(image, cell, iconrecognition::GridType::Transfer, kDefaultLowTextureThreshold, cv::Rect(1153, 457, 72, 4)),
        "a narrow visible fragment must not prove the cell empty");
    Check(
        ForegroundTextureScore(image, cell, iconrecognition::GridType::PortStorager, trusted)
            == ForegroundTextureScore(image, cell, iconrecognition::GridType::PortStorager),
        "Transfer bounds must not affect PortStorager texture");
    for (const auto type : { iconrecognition::GridType::Trade,
                             iconrecognition::GridType::Valuables,
                             iconrecognition::GridType::Shipment,
                             iconrecognition::GridType::CreditTrade,
                             iconrecognition::GridType::Rewards,
                             iconrecognition::GridType::SingleRoi }) {
        Check(
            !ForegroundTextureScore(image, cell, type, trusted),
            "Transfer bounds must not enable texture rejection for other grid types");
    }
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

    const std::vector<iconrecognition::detail::GridCell> visible_cells {
        { .grid_index = 0, .row = 0, .column = 0, .cell_box = cv::Rect(10, 10, 64, 64) },
        { .grid_index = 0, .row = 0, .column = 1, .cell_box = cv::Rect(79, 10, 64, 64) },
        { .grid_index = 0, .row = 1, .column = 0, .cell_box = cv::Rect(10, 79, 64, 64) },
        { .grid_index = 0, .row = 2, .column = 1, .cell_box = cv::Rect(79, 148, 64, 64) },
    };
    Check(
        iconrecognition::detail::VisibleGridShape(visible_cells) == cv::Size(2, 3),
        "visible grid shape must exclude filtered axes that produced no cell");
}

cv::Mat BuildSyntheticGrid(int pitch, int cell_size = 0)
{
    const cv::Rect roi(0, 0, 420, 320);
    cv::Mat image(roi.size(), CV_8UC3, cv::Scalar(18, 18, 18));
    for (int x = 0; x < roi.width; x += pitch) {
        image.colRange(x, std::min(x + 2, roi.width)).setTo(cv::Scalar(245, 245, 245));
        if (cell_size > 0 && x + cell_size < roi.width) {
            image.colRange(x + cell_size, std::min(x + cell_size + 2, roi.width)).setTo(cv::Scalar(245, 245, 245));
        }
    }
    for (int y = 0; y < roi.height; y += pitch) {
        image.rowRange(y, std::min(y + 2, roi.height)).setTo(cv::Scalar(245, 245, 245));
        if (cell_size > 0 && y + cell_size < roi.height) {
            image.rowRange(y + cell_size, std::min(y + cell_size + 2, roi.height)).setTo(cv::Scalar(245, 245, 245));
        }
    }
    return image;
}

void TestGridScaleEstimateSelectsCalibratedProfiles()
{
    const cv::Rect roi(0, 0, 420, 320);
    cv::Mat standard = BuildSyntheticGrid(69);
    const auto standard_scale = iconrecognition::detail::EstimateGridScale(standard, iconrecognition::GridType::Transfer, roi);
    Check(standard_scale && std::abs(*standard_scale - 1.0) <= 1e-6, "standard grid structure must keep scale 1.0");

    cv::Mat enlarged = BuildSyntheticGrid(86);
    const auto scale = iconrecognition::detail::EstimateGridScale(enlarged, iconrecognition::GridType::Transfer, roi);
    Check(scale && std::abs(*scale - 1.25) <= 1e-6, "enlarged grid structure must select calibrated scale 1.25");

    cv::Mat empty(roi.size(), CV_8UC3, cv::Scalar(18, 18, 18));
    const auto ambiguous = iconrecognition::detail::EstimateGridScale(empty, iconrecognition::GridType::Transfer, roi);
    Check(!ambiguous, "automatic grid scale must reject an ROI without periodic structure");
}

void TestGridDetectorMapsNormalizedCellsBackToSourceImage()
{
    constexpr double kGridScale = 1.25;
    // 保留原测试的左侧 profile；整数归一化平移不改变规则边框的采样相位。
    const cv::Rect roi(60, 190, 420, 320);
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(18, 18, 18));
    BuildSyntheticGrid(86, 80).copyTo(image(roi));
    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, roi);

    Check(std::abs(grid.grid_scale - kGridScale) <= 1e-6, "grid detection must expose the resolved source scale");
    Check(!grid.cells.empty(), "scaled synthetic grid must contain detected cells");
    Check(grid.cells.front().texture_roi == cv::Rect(60, 190, 420, 317), "ADB cell texture bounds must remain in source coordinates");
    Check(
        std::ranges::all_of(grid.cells, [](const auto& cell) { return cell.cell_box.size() == cv::Size(80, 80); }),
        "normalized 64px cells must be mapped back to 80px source-image cells");

    const auto& layout = grid.grids.front();
    const auto first_cell = std::ranges::find_if(layout.cells, [](const auto& cell) { return cell.row == 0; });
    Check(first_cell != layout.cells.end(), "scaled regular grid must expose a first row");
    const int first_x = first_cell->cell_box.x - first_cell->column * 86;
    for (const auto& cell : layout.cells) {
        if (cell.row != 0) {
            continue;
        }
        Check(
            cell.cell_box.x == first_x + cell.column * 86,
            "scaled regular grid must preserve the source-image pitch without cumulative rounding drift: column="
                + std::to_string(cell.column) + " expected=" + std::to_string(first_x + cell.column * 86)
                + " actual=" + std::to_string(cell.cell_box.x) + " pitch=" + std::to_string(layout.pitch_x));
    }
}

void TestRewardsGridScaleSelectsCardProfileInsideCallerRoi()
{
    const cv::Scalar kRarityBand = RarityBgr(4);
    cv::Mat standard(96, 96, CV_8UC3, cv::Scalar(245, 245, 245));
    standard.rowRange(91, 96).setTo(kRarityBand);
    const auto standard_scale =
        iconrecognition::detail::EstimateGridScale(standard, iconrecognition::GridType::Rewards, cv::Rect(0, 0, 96, 96));
    Check(standard_scale && std::abs(*standard_scale - 1.0) <= 1e-6, "96px reward card ROI must keep scale 1.0");

    cv::Mat ambiguous(108, 108, CV_8UC3, cv::Scalar(245, 245, 245));
    const auto ambiguous_scale =
        iconrecognition::detail::EstimateGridScale(ambiguous, iconrecognition::GridType::Rewards, cv::Rect(0, 0, 108, 108));
    Check(!ambiguous_scale, "a reward card equidistant from both controller profiles must be rejected");

    cv::Mat vertically_connected(120, 120, CV_8UC3, cv::Scalar(24, 24, 24));
    vertically_connected(cv::Rect(12, 0, 96, 120)).setTo(cv::Scalar(245, 245, 245));
    vertically_connected(cv::Rect(12, 91, 96, 5)).setTo(kRarityBand);
    vertically_connected(cv::Rect(12, 0, 1, 120)).setTo(cv::Scalar(245, 245, 245));
    const auto connected_scale =
        iconrecognition::detail::EstimateGridScale(vertically_connected, iconrecognition::GridType::Rewards, cv::Rect(0, 0, 120, 120));
    Check(
        connected_scale && std::abs(*connected_scale - 1.0) <= 1e-6,
        "a 96px reward card connected to vertical highlights must remain scale 1.0");

    cv::Mat enlarged(120, 120, CV_8UC3, cv::Scalar(245, 245, 245));
    enlarged.rowRange(115, 120).setTo(kRarityBand);
    const auto scale = iconrecognition::detail::EstimateGridScale(enlarged, iconrecognition::GridType::Rewards, cv::Rect(0, 0, 120, 120));
    Check(scale && std::abs(*scale - 1.25) <= 1e-6, "larger rewards cards must estimate 1.25 UI scale");
}

void TestRewardsGridScaleIgnoresBrightBackgroundWithoutRarityBand()
{
    constexpr int kCellSize = 96;
    const cv::Rect roi(0, 0, 520, 300);
    cv::Mat image(roi.size(), CV_8UC3, cv::Scalar(24, 24, 24));

    // 该背景块尺寸复现 Issue #5066 中把真实 91px 卡片中位数拉到 81.5px 的干扰分量。
    image(cv::Rect(30, 30, 90, 72)).setTo(cv::Scalar(235, 235, 235));
    const int card_x = (image.cols - kCellSize) / 2;
    const int card_y = (image.rows - kCellSize) / 2;
    image(cv::Rect(card_x, card_y, kCellSize, 91)).setTo(cv::Scalar(245, 245, 245));
    image(cv::Rect(card_x, card_y + 91, kCellSize, 5)).setTo(RarityBgr(4));

    const auto scale = iconrecognition::detail::EstimateGridScale(image, iconrecognition::GridType::Rewards, roi);
    Check(scale && std::abs(*scale - 1.0) <= 1e-6, "rarity-backed 91px reward card must win over a 72px bright background");

    image(cv::Rect(card_x, card_y, kCellSize, kCellSize)).setTo(cv::Scalar(24, 24, 24));
    Check(
        !iconrecognition::detail::EstimateGridScale(image, iconrecognition::GridType::Rewards, roi),
        "a bright background without a reward rarity band must not select a controller profile");
}

void TestControllerTypeSelectsKnownGridScale()
{
    const auto win32 = iconrecognition::detail::GridScaleForControllerType("Win32");
    Check(win32 && std::abs(*win32 - 1.0) <= 1e-6, "Win32 controller must use the standard grid scale");

    const auto adb = iconrecognition::detail::GridScaleForControllerType("aDb");
    Check(adb && std::abs(*adb - 1.25) <= 1e-6, "Adb controller matching must be case-insensitive");

    const auto playcover = iconrecognition::detail::GridScaleForControllerType("PlayCover");
    Check(playcover && std::abs(*playcover - 1.25) <= 1e-6, "PlayCover controller must use the ADB grid scale");

    const auto linux_scale = iconrecognition::detail::GridScaleForControllerType("linux");
    Check(linux_scale && std::abs(*linux_scale - 1.0) <= 1e-6, "Linux controller must use the standard grid scale");

    const auto macos = iconrecognition::detail::GridScaleForControllerType("MacOS");
    Check(macos && std::abs(*macos - 1.0) <= 1e-6, "MacOS controller must use the standard grid scale");
    Check(!iconrecognition::detail::GridScaleForControllerType("Unknown"), "unknown controllers must keep image-based fallback");
}

void TestExplicitGridScaleHintBypassesImageEstimate()
{
    cv::Mat ambiguous(108, 108, CV_8UC3, cv::Scalar(245, 245, 245));
    const cv::Rect roi(0, 0, ambiguous.cols, ambiguous.rows);
    Check(
        !iconrecognition::detail::EstimateGridScale(ambiguous, iconrecognition::GridType::Rewards, roi),
        "fixture must remain ambiguous without controller context");

    const auto grid = iconrecognition::detail::DetectGrid(ambiguous, iconrecognition::GridType::Rewards, roi, 1.0);
    Check(grid.grid_scale == 1.0, "explicit Win32 profile hint must be preserved");
    Check(grid.cells.size() == 1, "explicit Win32 profile hint must bypass ambiguous image scale estimation");
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

void TestValuablesCardExtentUsesScaledProfileOcclusionPolicy()
{
    const cv::Rect win32_roi(0, 0, 96, 66);
    const cv::Rect win32_cell(0, 0, 96, 96);
    Check(
        !iconrecognition::detail::HasFormalCardExtent(win32_cell, win32_roi, iconrecognition::GridType::Valuables, 1.0),
        "Win32 valuables must reject a 96px row with only 66px visible above the bottom toolbar");

    Check(
        iconrecognition::detail::HasFormalCardExtent(win32_cell, win32_roi, iconrecognition::GridType::Valuables, 1.25),
        "normalized ADB valuables must retain the same 96px row when its source profile is scaled");
    Check(
        !iconrecognition::detail::HasFormalCardExtent(win32_cell, win32_roi, iconrecognition::GridType::Trade, 1.25),
        "other card grids must keep the default 70% bottom visibility rule");
}

void TestPortOcclusionPolicyDropsOnlyWeakSevenColumnFirstRow()
{
    Check(
        iconrecognition::detail::ShouldDropPortFirstRow(7, 0.08, 0.30, 61, 80),
        "seven-column port grid must drop a first row that is much weaker than the complete second row");
    Check(
        !iconrecognition::detail::ShouldDropPortFirstRow(7, 0.20, 0.30, 61, 80),
        "seven-column port grid must retain a first row with comparable structure");
    Check(
        !iconrecognition::detail::ShouldDropPortFirstRow(4, 0.08, 0.30, 61, 80),
        "left four-column panel must not reuse the right toolbar-occlusion rule");
    Check(
        !iconrecognition::detail::ShouldDropPortFirstRow(7, 0.08, 0.30, 72, 64),
        "complete Win32 first row below the toolbar must not be dropped even when its structure support is weak");
}

void TestRewardsRowCompletesInternalMissingCards()
{
    const std::vector<int> observed { 216, 362, 508, 944 };
    Check(
        iconrecognition::detail::CompleteRewardsRowStarts(observed, 146.0) == std::vector<int>({ 216, 362, 508, 654, 800, 944 }),
        "rewards row must fill internal card gaps without extending beyond observed endpoints");
    Check(
        iconrecognition::detail::CompleteRewardsRowStarts({ 216, 362 }, 146.0) == std::vector<int>({ 216, 362 }),
        "rewards row must not extrapolate beyond reliable endpoints");
}

void TestRewardsDefaultFiltersIncludeAllRewardStorageKinds()
{
    const auto& filters = iconrecognition::detail::DefaultItemFilters(iconrecognition::GridType::Rewards);
    Check(
        std::ranges::find(filters, "Isolate:*") != filters.end(),
        "rewards defaults must retain isolate resources such as gold and premium currency");
    Check(
        std::ranges::find(filters, "ValuableDepot:*") != filters.end(),
        "rewards defaults must include progression items and consumables stored in ValuableDepot");
}

void TestShipmentProfileAcceptsTwoCompleteRows()
{
    const auto profile = iconrecognition::detail::ProfileFor(iconrecognition::GridType::Shipment);
    const cv::Rect roi(0, 0, 390, 310);
    constexpr int kPhaseX = 12;
    constexpr int kPhaseY = 72;
    int complete_rows = 0;
    for (int row = 0; row < 3; ++row) {
        const int y = cvRound(kPhaseY + row * profile.pitch_y);
        if (iconrecognition::detail::HasFormalCardExtent(
                cv::Rect(kPhaseX, y, profile.cell_size, profile.cell_size),
                roi,
                iconrecognition::GridType::Shipment,
                1.0)) {
            ++complete_rows;
        }
    }
    Check(complete_rows == 2, "shipment fixture must leave exactly two complete rows above the bottom toolbar");
    Check(
        profile.min_rows <= complete_rows,
        "shipment profile must allow a strong card phase with two complete rows: min_rows=" + std::to_string(profile.min_rows));
}

void TestRewardsGridKeepsBottomRarityBandInsideCell()
{
    constexpr int kCellSize = 96;
    constexpr int kBrightBodyHeight = 92;
    constexpr int kRarityBandHeight = kCellSize - kBrightBodyHeight;
    constexpr int kPhaseX = 40;
    constexpr int kPhaseY = 35;
    constexpr int kPitchX = 117;
    constexpr int kColumns = 3;
    const cv::Rect roi(0, 0, 420, 180);
    cv::Mat image(roi.size(), CV_8UC3, cv::Scalar(24, 24, 24));

    for (int column = 0; column < kColumns; ++column) {
        const int x = kPhaseX + column * kPitchX;
        image(cv::Rect(x, kPhaseY, kCellSize, kBrightBodyHeight)).setTo(cv::Scalar(240, 240, 240));
        // 饱和彩色色条不会进入白色底板连通域，但仍属于需要识别的完整 96px cell。
        image(cv::Rect(x, kPhaseY + kBrightBodyHeight, kCellSize, kRarityBandHeight)).setTo(cv::Scalar(0, 220, 220));
    }

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Rewards, roi, 1.0);
    Check(grid.cells.size() == kColumns, "synthetic rewards row must retain every card");
    for (const auto& cell : grid.cells) {
        Check(
            cell.cell_box.y == kPhaseY,
            "rewards cell must start at the bright card top so its bottom rarity band stays inside: actual_y="
                + std::to_string(cell.cell_box.y));
        Check(cell.cell_box.height == kCellSize, "rewards cell height must keep the 96px template contract");
    }
}

void TestRewardsSingleCardRoiClampsSmallBodyPhaseOffset()
{
    constexpr int kCellSize = 96;
    constexpr int kBodyTop = 2;
    constexpr int kBrightBodyHeight = 91;
    const cv::Rect roi(0, 0, kCellSize, kCellSize + 1);
    cv::Mat image(roi.size(), CV_8UC3, cv::Scalar(24, 24, 24));
    image(cv::Rect(0, kBodyTop, kCellSize, kBrightBodyHeight)).setTo(cv::Scalar(240, 240, 240));

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Rewards, roi, 1.0);
    Check(grid.cells.size() == 1, "single reward ROI must retain a card whose bright body starts two pixels below the ROI");
    Check(grid.cells.front().cell_box == cv::Rect(0, 1, kCellSize, kCellSize), "reward cell must stay inside the caller ROI");
}

void TestRewardsGridUsesCenteredSharedOriginWithoutFixedColumnCount()
{
    constexpr int kCellSize = 96;
    constexpr int kBrightBodyHeight = 92;
    constexpr int kRarityBandHeight = kCellSize - kBrightBodyHeight;
    constexpr int kPitchX = 117;
    constexpr int kPitchY = 117;
    constexpr int kFullColumns = 7;
    constexpr int kFirstRowX = (1280 - ((kFullColumns - 1) * kPitchX + kCellSize)) / 2;
    constexpr int kFirstRowY = (720 - (kPitchY + kCellSize)) / 2;
    constexpr int kSecondRowY = kFirstRowY + kPitchY;
    const cv::Rect roi(39, 82, 1205, 511);
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(24, 24, 24));

    const auto paint_row = [&](int origin_x, int origin_y, int columns) {
        for (int column = 0; column < columns; ++column) {
            const int x = origin_x + column * kPitchX;
            image(cv::Rect(x, origin_y, kCellSize, kBrightBodyHeight)).setTo(cv::Scalar(240, 240, 240));
            image(cv::Rect(x, origin_y + kBrightBodyHeight, kCellSize, kRarityBandHeight)).setTo(RarityBgr(4));
        }
    };
    paint_row(kFirstRowX, kFirstRowY, kFullColumns);
    paint_row(kFirstRowX, kSecondRowY, 2);
    paint_row(70, 480, 1);

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Rewards, roi, 1.0);
    Check(grid.grids.size() == 1, "wrapped rewards rows must form one shared grid");
    Check(
        grid.grids.front().columns == kFullColumns && grid.grids.front().rows == 2,
        "wrapped rewards grid shape must follow the observed first-row width");
    Check(
        grid.grids.front().cells.size() == kFullColumns + 2,
        "wrapped rewards grid must keep the observed first row and two second-row cells");
    const auto& cells = grid.grids.front().cells;
    Check(
        std::ranges::count_if(cells, [](const auto& cell) { return cell.row == 0; }) == kFullColumns,
        "first rewards row must contain the observed full width");
    Check(
        std::ranges::count_if(cells, [](const auto& cell) { return cell.row == 1; }) == 2,
        "wrapped rewards row must contain only its two observed cells");
    const auto second_row = std::ranges::find_if(cells, [](const auto& cell) { return cell.row == 1 && cell.column == 0; });
    Check(second_row != cells.end() && second_row->cell_box.x == kFirstRowX, "wrapped row must reuse the full row left boundary");
}

void TestRewardsGridRejectsOffCenterFalseCard()
{
    constexpr int kCellSize = 96;
    constexpr int kBodyHeight = 92;
    const cv::Rect roi(39, 82, 1205, 511);
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(24, 24, 24));
    const auto paint_card = [&](int x, int y) {
        image(cv::Rect(x, y, kCellSize, kBodyHeight)).setTo(cv::Scalar(240, 240, 240));
        image(cv::Rect(x, y + kBodyHeight, kCellSize, kCellSize - kBodyHeight)).setTo(RarityBgr(4));
    };
    paint_card(70, 105);
    paint_card((image.cols - kCellSize) / 2, (image.rows - kCellSize) / 2);

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Rewards, roi, 1.0);
    Check(grid.cells.size() == 1, "off-center upper-left bright card must not become a rewards grid");
    Check(
        grid.cells.front().cell_box == cv::Rect((image.cols - kCellSize) / 2, (image.rows - kCellSize) / 2, kCellSize, kCellSize),
        "centered reward card must survive off-center false-card filtering");
}

void TestRewardsAdbGridUsesSixColumnSharedOrigin()
{
    constexpr int kCellSize = 120;
    constexpr int kBodyHeight = 115;
    constexpr int kFullColumns = 6;
    constexpr int kPitchX = 146;
    constexpr int kOriginX = 216;
    constexpr int kFirstRowY = 209;
    constexpr int kSecondRowY = 375;
    const cv::Rect roi(178, 140, 935, 440);
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(24, 24, 24));

    const auto paint_row = [&](int y, int columns) {
        for (int column = 0; column < columns; ++column) {
            const int x = kOriginX + column * kPitchX;
            image(cv::Rect(x, y, kCellSize, kBodyHeight)).setTo(cv::Scalar(240, 240, 240));
            image(cv::Rect(x, y + kBodyHeight, kCellSize, kCellSize - kBodyHeight)).setTo(RarityBgr(4));
        }
    };
    paint_row(kFirstRowY, kFullColumns);
    paint_row(kSecondRowY, 2);

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Rewards, roi, 1.25);
    Check(grid.grids.size() == 1, "ADB wrapped rewards rows must form one shared grid");
    Check(grid.grids.front().columns == kFullColumns && grid.grids.front().rows == 2, "ADB wrapped grid shape must be 6x2");
    Check(grid.grids.front().cells.size() == 8, "ADB wrapped grid must keep six first-row and two second-row cells");
    const auto second_row =
        std::ranges::find_if(grid.grids.front().cells, [](const auto& cell) { return cell.row == 1 && cell.column == 0; });
    Check(second_row != grid.grids.front().cells.end(), "ADB wrapped row must expose its first cell");
    Check(std::abs(second_row->cell_box.x - kOriginX) <= 1, "ADB wrapped row must reuse the six-column left boundary");
}

void TestRewardsGridRenumbersColumnsAfterRoiFiltering()
{
    constexpr int kCellSize = 96;
    constexpr int kClippedCardSize = 80;
    constexpr int kPhaseY = 20;
    constexpr int kKeptCardX = 117;
    const cv::Rect roi(0, 0, 300, 150);
    cv::Mat image(roi.size(), CV_8UC3, cv::Scalar(24, 24, 24));
    image(cv::Rect(0, kPhaseY, kClippedCardSize, kClippedCardSize)).setTo(cv::Scalar(240, 240, 240));
    image(cv::Rect(kKeptCardX, kPhaseY, kCellSize, kCellSize)).setTo(cv::Scalar(240, 240, 240));

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Rewards, roi, 1.0);
    Check(grid.grids.size() == 1, "filtered rewards candidates must retain one row layout");
    Check(grid.grids.front().cells.size() == 1, "out-of-ROI rewards cells must be filtered");
    Check(grid.grids.front().columns == 1, "rewards columns must count only retained cells");
    Check(grid.grids.front().cells.front().column == 0, "retained rewards columns must be renumbered from zero");
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

void TestTransferGridDetectsSparseVisiblePhase()
{
    constexpr int kCellSize = 64;
    constexpr int kPitch = 69;
    constexpr int kColumns = 4;
    constexpr int kVisiblePhaseX = 7;
    constexpr int kBackgroundPhaseX = 36;
    constexpr int kPhaseY = 15;
    constexpr int kTargetColumn = 1;
    const cv::Rect roi(770, 209, 330, 291);
    const cv::Rect target_box(roi.x + kVisiblePhaseX + kTargetColumn * kPitch, roi.y + kPhaseY, kCellSize, kCellSize);
    cv::Mat image(291, 330, CV_8UC3, cv::Scalar(24, 24, 24));

    const auto draw_cell = [&](int x, int y, const cv::Scalar& border) {
        image.colRange(x, x + 2).rowRange(y, y + kCellSize + 1).setTo(border);
        image.colRange(x + kCellSize, x + kCellSize + 2).rowRange(y, y + kCellSize + 1).setTo(border);
        image.rowRange(y, y + 2).colRange(x, x + kCellSize + 1).setTo(border);
        image.rowRange(y + kCellSize, y + kCellSize + 2).colRange(x, x + kCellSize + 1).setTo(border);
    };
    // 模拟物品行下方的重复背景纹理，使结构检测稳定落在错误的半格相位。
    for (int row = 0; row < 4; ++row) {
        for (int column = 0; column < kColumns; ++column) {
            draw_cell(kBackgroundPhaseX + column * kPitch, kPhaseY + row * kPitch, cv::Scalar(130, 130, 130));
        }
    }
    // 可见物品行包含高纹理内容，避免测试只依赖单个模板图标。
    for (int column = 0; column < kColumns; ++column) {
        const int x = kVisiblePhaseX + column * kPitch;
        draw_cell(x, kPhaseY, cv::Scalar(90, 90, 90));
        for (int y = kPhaseY + 6; y < kPhaseY + kCellSize - 8; ++y) {
            for (int local_x = 6; local_x < kCellSize - 6; ++local_x) {
                const unsigned char value = static_cast<unsigned char>(40 + ((local_x * 7 + y * 11 + column * 13) % 180));
                image.at<cv::Vec3b>(y, x + local_x) = cv::Vec3b(value, value, value);
            }
        }
    }

    cv::Mat source(720, 1280, CV_8UC3, cv::Scalar(24, 24, 24));
    image.copyTo(source(roi));
    image = source;
    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, roi, 1.0);
    const auto target_cell = std::ranges::find_if(grid.cells, [&](const auto& cell) {
        return std::abs(cell.cell_box.x - target_box.x) <= 1 && std::abs(cell.cell_box.y - target_box.y) <= 2;
    });
    std::string grid_summary = "sparse transfer grid must preserve the visible item phase";
    if (!grid.grids.empty() && grid.grids.front().selection_diagnostics) {
        const auto& layout = grid.grids.front();
        const auto& diagnostics = *layout.selection_diagnostics;
        grid_summary += "; origin=" + std::to_string(static_cast<int>(diagnostics.origin.x)) + ","
                        + std::to_string(static_cast<int>(diagnostics.origin.y))
                        + "; pitch=" + std::to_string(static_cast<int>(diagnostics.pitch.x)) + ","
                        + std::to_string(static_cast<int>(diagnostics.pitch.y));
        if (!layout.cells.empty()) {
            grid_summary +=
                "; first=" + std::to_string(layout.cells.front().cell_box.x) + "," + std::to_string(layout.cells.front().cell_box.y);
        }
    }
    Check(target_cell != grid.cells.end(), grid_summary);

    iconrecognition::detail::TemplateCatalog catalog("assets/data/IconRecognition", "assets/resource/image/IconRecognition");
    Check(catalog.initialize(), "transfer recovery fixture catalog must initialize");
    const auto& templates = catalog.load(kCellSize);
    const auto target = std::ranges::find_if(templates, [](const auto& templ) { return templ.record.item_id == "item_iron_ore"; });
    Check(target != templates.end(), "sparse transfer fixture must contain item_iron_ore");
    target->image.copyTo(image(target_box));

    iconrecognition::IconRecognizer recognizer("assets/data/IconRecognition");
    Check(recognizer.initialize(), "sparse transfer recognizer must initialize");
    iconrecognition::RecognitionRequest request;
    request.grid_type = iconrecognition::GridType::Transfer;
    request.roi = roi;
    request.candidates.item_ids = { "item_iron_ore" };
    request.candidates.item_filters = { "Normal:Ore" };
    request.candidates.item_recheck_filters = { "Normal:Ore" };
    request.grid_scale_hint = 1.0;
    request.deduplicate = true;
    const auto result = recognizer.recognize(image, request);

    Check(result.matched && result.matches.size() == 1, "sparse transfer target must be found from the detected grid");
    Check(result.matches.front().item.item_id == "item_iron_ore", "sparse transfer detection must preserve the target id");
    Check(
        std::abs(result.matches.front().cell_box.x - target_box.x) <= 1 && std::abs(result.matches.front().cell_box.y - target_box.y) <= 2,
        "transfer recognition must keep the target at its detected cell position");
}

void TestTransferGridRejectsBroadOvercapacityPhase()
{
    constexpr int kCellSize = 64;
    constexpr int kFormalPitch = 69;
    constexpr int kFormalColumns = 5;
    constexpr int kBroadPitch = 66;
    constexpr int kBroadColumns = 6;
    constexpr int kRows = 4;
    constexpr int kFormalPhaseX = 33;
    constexpr int kBroadPhaseX = 2;
    constexpr int kPhaseY = 7;
    cv::Mat image(291, 398, CV_8UC3, cv::Scalar(24, 24, 24));

    const auto draw_cell = [&](int x, int y, const cv::Scalar& border) {
        image.colRange(x, x + 2).rowRange(y, y + kCellSize + 1).setTo(border);
        image.colRange(x + kCellSize, x + kCellSize + 2).rowRange(y, y + kCellSize + 1).setTo(border);
        image.rowRange(y, y + 2).colRange(x, x + kCellSize + 1).setTo(border);
        image.rowRange(y + kCellSize, y + kCellSize + 2).colRange(x, x + kCellSize + 1).setTo(border);
    };
    // 真实五列格子提供稳定结构和前景纹理；错相位格会跨过这些纹理，因此单看纹理覆盖仍无法消歧。
    for (int row = 0; row < kRows; ++row) {
        for (int column = 0; column < kFormalColumns; ++column) {
            const int x = kFormalPhaseX + column * kFormalPitch;
            const int y = kPhaseY + row * kFormalPitch;
            draw_cell(x, y, cv::Scalar(90, 90, 90));
            for (int local_y = 6; local_y < kCellSize - 8; ++local_y) {
                for (int local_x = 6; local_x < kCellSize - 6; ++local_x) {
                    const unsigned char value =
                        static_cast<unsigned char>(40 + ((local_x * 7 + local_y * 11 + row * 17 + column * 13) % 180));
                    image.at<cv::Vec3b>(y + local_y, x + local_x) = cv::Vec3b(value, value, value);
                }
            }
        }
    }
    // 粗搜索允许 66px pitch；该错相位能在 398px ROI 内塞入六列，但无法形成正式 69px 晶格。
    for (int row = 0; row < kRows; ++row) {
        for (int column = 0; column < kBroadColumns; ++column) {
            draw_cell(kBroadPhaseX + column * kBroadPitch, kPhaseY + row * kFormalPitch, cv::Scalar(160, 160, 160));
        }
    }

    const auto hints = iconrecognition::detail::DiscoverTransferGridHints(image, true);
    Check(hints.size() == 1, "single transfer panel must produce one grid hint");
    Check(hints.front().x_starts.size() == kFormalColumns, "broad search must not add a column that cannot fit the formal pitch");
    Check(std::abs(hints.front().x_starts.front() - kFormalPhaseX) <= 1, "transfer hint must preserve the five-column formal phase");
}

void TestTransferRarityGridKeepsVisibleTopRow()
{
    constexpr int kCellSize = 64;
    constexpr int kPitch = 69;
    constexpr int kColumns = 4;
    constexpr int kRows = 4;
    const cv::Rect roi(770, 209, 300, 277);
    const cv::Point origin(780, 195);
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(30, 30, 30));
    for (int row = 0; row < kRows; ++row) {
        for (int column = 0; column < kColumns; ++column) {
            const cv::Rect cell(origin.x + column * kPitch, origin.y + row * kPitch, kCellSize, kCellSize);
            image(cell).setTo(cv::Scalar(60, 60, 60));
            cv::rectangle(image, cell, cv::Scalar(100, 100, 100), 1);
            // 内容区必须非空，确保本测试进入稀有度拟合而非前置全空路径。
            for (int y = 8; y < kCellSize - 8; y += 4) {
                image(cv::Rect(cell.x + 8, cell.y + y, kCellSize - 16, 1)).setTo(cv::Scalar(180, 180, 180));
            }
            image(cv::Rect(cell.x, cell.y + kCellSize - 3, kCellSize, 3)).setTo(RarityBgr(3));
        }
    }
    // 顶行只有 50/64 可见，但色带完整；从上方恢复的行必须生成在上方。
    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, roi, 1.0);
    Check(grid.grids.size() == 1, "rarity transfer must form one panel");
    const auto& layout = grid.grids.front();
    Check(
        layout.rows == kRows && layout.columns == kColumns,
        "rarity transfer must retain the visible top row; rows=" + std::to_string(layout.rows)
            + " columns=" + std::to_string(layout.columns) + " first=" + std::to_string(layout.cells.front().cell_box.x) + ","
            + std::to_string(layout.cells.front().cell_box.y));
    Check(layout.cells.front().cell_box.y == origin.y, "rarity transfer must generate the top row at the rarity anchor");
}

void TestTransferBottomVisibilityIsGridSpecific()
{
    using namespace iconrecognition::detail;
    Check(TransferProfileFor(TransferGridVariant::TransferLeft).minimum_bottom_visibility == 0.65, "transfer left bottom visibility");
    Check(TransferProfileFor(TransferGridVariant::TransferRight).minimum_bottom_visibility == 0.65, "transfer right bottom visibility");
    Check(TransferProfileFor(TransferGridVariant::PortStoragerLeft).minimum_bottom_visibility == 0.70, "port left visibility unchanged");
    Check(TransferProfileFor(TransferGridVariant::PortStoragerRight).minimum_bottom_visibility == 0.80, "port right visibility unchanged");

    constexpr int kCellSize = 64;
    constexpr int kPitch = 69;
    constexpr int kColumns = 4;
    constexpr int kRows = 4;
    const cv::Point origin(780, 237);
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(30, 30, 30));
    for (int row = 0; row < kRows; ++row) {
        for (int column = 0; column < kColumns; ++column) {
            const cv::Rect cell(origin.x + column * kPitch, origin.y + row * kPitch, kCellSize, kCellSize);
            image(cell).setTo(cv::Scalar(60, 60, 60));
            cv::rectangle(image, cell, cv::Scalar(100, 100, 100), 1);
            for (int y = 8; y < kCellSize - 8; y += 4) {
                image(cv::Rect(cell.x + 8, cell.y + y, kCellSize - 16, 1)).setTo(cv::Scalar(180, 180, 180));
            }
            image(cv::Rect(cell.x, cell.y + kCellSize - 3, kCellSize, 3)).setTo(RarityBgr(3));
        }
    }
    // 底行从 y=444 开始：42/64 超过 65%，41/64 则不足；两次只改变调用方 ROI 下界。
    for (int visible_height : { 42, 41 }) {
        const cv::Rect roi(770, 209, 300, 444 + visible_height - 209);
        const auto grid = DetectGrid(image, iconrecognition::GridType::Transfer, roi, 1.0);
        Check(grid.grids.size() == 1, "bottom visibility fixture must form one panel");
        Check(grid.grids.front().rows == (visible_height == 42 ? 4 : 3), "transfer bottom row must obey the 65% boundary");
    }
}

void CheckTransferEmptyGridLattice(int pitch, cv::Point origin, const cv::Rect& roi = cv::Rect(160, 205, 398, 286))
{
    constexpr int kCellSize = 64;
    constexpr int kColumns = 5;
    constexpr int kRows = 4;
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(30, 30, 30));
    for (int row = 0; row < kRows; ++row) {
        for (int column = 0; column < kColumns; ++column) {
            const cv::Rect cell(origin.x + column * pitch, origin.y + row * pitch, kCellSize, kCellSize);
            image(cell).setTo(cv::Scalar(38, 38, 38));
            cv::rectangle(image, cell, cv::Scalar(105, 105, 105), 2, cv::LINE_8);
        }
    }

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, roi, 1.0);
    Check(grid.grids.size() == 1, "empty transfer panel must produce one grid");
    Check(
        grid.grids.front().columns == kColumns && grid.grids.front().rows == kRows,
        "empty transfer cells must form one 5x4 lattice: origin=" + std::to_string(origin.x) + "," + std::to_string(origin.y)
            + " rows=" + std::to_string(grid.grids.front().rows) + " columns=" + std::to_string(grid.grids.front().columns));
    const auto& cells = grid.grids.front().cells;
    Check(cells.size() == kColumns * kRows, "empty transfer lattice must retain every cell");
    for (int row = 0; row < kRows; ++row) {
        for (int column = 0; column < kColumns; ++column) {
            const auto& box = cells[row * kColumns + column].cell_box;
            const std::string location = " row=" + std::to_string(row) + " column=" + std::to_string(column)
                                         + " origin=" + std::to_string(origin.x) + "," + std::to_string(origin.y)
                                         + " actual=" + std::to_string(box.x) + "," + std::to_string(box.y);
            // LINE_8 的 2px 描边向矩形外扩 1px，按实际外沿而非描边中心检查定位误差。
            Check(
                std::abs(box.x - (origin.x - 1 + column * pitch)) <= 1 && std::abs(box.y - (origin.y - 1 + row * pitch)) <= 1,
                "empty transfer lattice must use measured borders for every cell" + location);
            const auto texture = iconrecognition::detail::ForegroundTextureScore(image, box, iconrecognition::GridType::Transfer);
            Check(
                texture && *texture < iconrecognition::detail::kDefaultLowTextureThreshold,
                "empty cell must not sample its border" + location);
        }
    }
}

void TestTransferEmptyGridFitsOneCompleteLattice()
{
    CheckTransferEmptyGridLattice(69, cv::Point(193, 212));
    CheckTransferEmptyGridLattice(68, cv::Point(195, 215));
    CheckTransferEmptyGridLattice(70, cv::Point(191, 211));
    // 首边两侧不可完整测量、末格恰好贴住 ROI，以及顶行少量遮挡时仍须保留全部格子。
    CheckTransferEmptyGridLattice(69, cv::Point(771, 216), cv::Rect(770, 209, 341, 277));
    CheckTransferEmptyGridLattice(69, cv::Point(771, 206), cv::Rect(770, 209, 341, 277));
}

void TestTransferEmptyGridSkipsCroppedTopRowAndKeepsWeakColumn()
{
    constexpr int kCellSize = 64;
    constexpr int kPitch = 69;
    constexpr int kColumns = 5;
    constexpr int kPhaseX = 193;
    constexpr int kPhaseY = 183;
    const cv::Rect roi(160, 205, 398, 286);
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(30, 30, 30));
    for (int row = 0; row < 5; ++row) {
        for (int column = 0; column < kColumns; ++column) {
            const cv::Rect cell(kPhaseX + column * kPitch, kPhaseY + row * kPitch, kCellSize, kCellSize);
            const cv::Rect visible = cell & roi;
            if (visible.empty()) {
                continue;
            }
            image(visible).setTo(cv::Scalar(38, 38, 38));
            const cv::Scalar border = column + 1 == kColumns ? cv::Scalar(75, 75, 75) : cv::Scalar(105, 105, 105);
            cv::rectangle(image, cell, border, 2, cv::LINE_8);
        }
    }

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, roi);
    Check(grid.grids.size() == 1, "cropped empty transfer panel must produce one grid");
    Check(
        grid.grids.front().columns == kColumns && grid.grids.front().rows == 3,
        "one lattice fit must retain five columns and three full rows");
    Check(
        std::abs(grid.grids.front().cells.front().cell_box.x - kPhaseX) <= 1
            && std::abs(grid.grids.front().cells.front().cell_box.y - (kPhaseY + kPitch)) <= 1,
        "cropped residual row must not become the first formal transfer row");
    for (const auto& cell : grid.grids.front().cells) {
        Check(
            std::abs(cell.cell_box.x - (kPhaseX + cell.column * kPitch)) <= 1
                && std::abs(cell.cell_box.y - (kPhaseY + (cell.row + 1) * kPitch)) <= 1,
            "weak-column fixture must use measured borders for every cell");
        const auto texture = iconrecognition::detail::ForegroundTextureScore(image, cell.cell_box, iconrecognition::GridType::Transfer);
        Check(texture && *texture < iconrecognition::detail::kDefaultLowTextureThreshold, "weak-column empty cells must stay low-texture");
    }
}

void TestTransferEmptyGridDiagnosticsSkipUnexecutedRarity()
{
    // 首条边框两侧均可见，覆盖全空结构门控成功而跳过稀有度扫描的路径。
    cv::Mat image(720, 1280, CV_8UC3, cv::Scalar(30, 30, 30));
    for (int row = 0; row < 3; ++row) {
        for (int column = 0; column < 4; ++column) {
            const cv::Rect cell(780 + column * 69, 220 + row * 69, 64, 64);
            image(cell).setTo(cv::Scalar(38, 38, 38));
            cv::rectangle(image, cell, cv::Scalar(105, 105, 105), 2, cv::LINE_8);
        }
    }
    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::Transfer, cv::Rect(770, 209, 341, 277), 1.0);
    Check(grid.grids.size() == 1 && grid.grids.front().selection_diagnostics, "empty grid must retain selection diagnostics");
    const auto& diagnostics = *grid.grids.front().selection_diagnostics;
    Check(
        diagnostics.fallback_reason == "empty-grid-structure",
        "empty grid must identify its structural path: " + diagnostics.fallback_reason);
    Check(diagnostics.rejected_reasons.empty(), "skipped rarity scans must not report rejected rarity evidence");
}

void TestPortStoragerWideRoiUsesStablePanelPartitions()
{
    const auto win32 = iconrecognition::detail::PartitionPortStoragerRegions(cv::Size(880, 350));
    Check(win32.size() == 2, "Win32 port full ROI must produce two panel regions");
    Check(win32[0].x == 0 && win32[0].width >= 318, "Win32 left panel region must retain all four columns");
    Check(win32[1].x >= 365 && win32[1].x <= 380, "Win32 right panel region must begin before the first storage column");
    Check((win32[1].width - 64) / 68 + 1 == 7, "Win32 right panel region must not admit an eighth column");

    const auto adb = iconrecognition::detail::PartitionPortStoragerRegions(cv::Size(920, 328));
    Check(adb.size() == 2, "ADB port full ROI must produce two panel regions");
    Check(adb[0].x == 0 && adb[0].width >= 295, "ADB left panel region must retain all four columns");
    Check(adb[1].x >= 380 && adb[1].x <= 388, "ADB right panel region must begin before the first storage column");
    Check((adb[1].width - 64) / 68 + 1 == 7, "ADB right panel region must not admit an eighth column");

    const auto right_profile = iconrecognition::detail::TransferProfileFor(iconrecognition::detail::TransferGridVariant::PortStoragerRight);
    Check(right_profile.minimum_bottom_visibility >= 0.80, "port right grid must reject a row with only 75% bottom visibility");
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

void TestCreditTradeGridUsesSixColumnsWhenRoiCannotContainSeven()
{
    constexpr int kCellSize = 128;
    constexpr int kPitchX = 161;
    constexpr int kPitchY = 205;
    constexpr int kColumns = 6;
    constexpr int kRows = 2;
    constexpr int kPhaseX = 20;
    constexpr int kPhaseY = 14;
    const cv::Rect roi(0, 0, 1010, 410);
    cv::Mat image(roi.size(), CV_8UC3, cv::Scalar(28, 28, 28));
    for (int row = 0; row < kRows; ++row) {
        for (int column = 0; column < kColumns; ++column) {
            const int x = kPhaseX + column * kPitchX;
            const int y = kPhaseY + row * kPitchY;
            image(cv::Rect(x - 10, y - 6, 150, 180)).setTo(cv::Scalar(240, 240, 240));
            image(cv::Rect(x, y, kCellSize, kCellSize)).setTo(cv::Scalar(245, 245, 245));
        }
    }

    const auto grid = iconrecognition::detail::DetectGrid(image, iconrecognition::GridType::CreditTrade, roi);
    Check(grid.grids.size() == 1, "six-column credit cards must form one grid");
    Check(grid.grids.front().columns == kColumns, "credit trade must not extend beyond the columns that fit the caller ROI");
    Check(grid.grids.front().rows == kRows, "six-column credit trade must retain both rows");
}

void TestValuablesGridKeepsSixColumnsAtAdbDensity()
{
    const auto profile = iconrecognition::detail::ProfileFor(iconrecognition::GridType::Valuables);
    Check(profile.min_columns == 6, "valuables profile must allow the six-column ADB layout");
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

void TestRarityFitPreservesSupportedRowRange()
{
    using namespace iconrecognition::detail;
    const auto profile = TransferProfileFor(TransferGridVariant::TransferRight);
    const std::vector<int> x_starts { 10, 79, 148 };
    const std::vector<int> actual_y { -14, 55, 124, 193 };
    for (bool missing_middle : { false, true }) {
        cv::Mat image(270, 230, CV_8UC3, cv::Scalar(25, 25, 25));
        for (int row = 0; row < static_cast<int>(actual_y.size()); ++row) {
            if (missing_middle && row == 1) {
                continue;
            }
            for (int x : x_starts) {
                image(cv::Rect(x, actual_y[row] + 61, 64, 3)).setTo(RarityBgr(3));
            }
        }
        for (bool coarse_has_top : { false, true }) {
            const std::vector<int> coarse_y = coarse_has_top ? actual_y : std::vector<int> { 55, 124, 193 };
            const auto fit = FitRarityGrid(image, x_starts, coarse_y, profile);
            Check(fit.has_value(), "visible rarity rows must form a fit");
            Check(fit->origin == coarse_y.front(), "rarity origin must retain the coarse reference for existing callers");
            Check(fit->first_supported_row == (coarse_has_top ? 0 : -1), "top recovery must retain a negative row index");
            Check(fit->last_supported_row == (coarse_has_top ? 3 : 2), "the last supported row must not move when recovering the top");
            Check(fit->supporting_rows == (missing_middle ? 3 : 4), "support count must remain distinct from row span");
            Check(fit->last_supported_row - fit->first_supported_row + 1 == 4, "a missing middle band must not shrink the fitted span");
        }
    }
}

void TestRarityFitRejectsClippedBandEndpoint()
{
    using namespace iconrecognition::detail;
    const auto profile = TransferProfileFor(TransferGridVariant::TransferLeft);
    const std::vector<int> x_starts { 0, 69, 138, 207, 276, 345, 414, 483 };
    const std::vector<int> coarse_y { 12, 81, 150, 219 };
    for (int rarity = 1; rarity <= 6; ++rarity) {
        cv::Mat image(287, 547, CV_8UC3, cv::Scalar(25, 25, 25));
        for (int row = 0; row < 3; ++row) {
            for (int x : x_starts) {
                image(cv::Rect(x, coarse_y[row] + 61, 64, 3)).setTo(RarityBgr(3));
            }
        }
        // 底部色块延伸到裁剪线，不能把 ROI 高度 286 当作真实色带下边缘，拉偏 69px 周期。
        image(cv::Rect(0, 280, 547, 7)).setTo(RarityBgr(rarity));
        const auto clipped = FitRarityGrid(image.rowRange(0, 286), x_starts, coarse_y, profile);
        Check(clipped && clipped->origin == 12 && clipped->pitch == 69, "clipped rarity endpoint must not move the fitted lattice");
        Check(clipped->supporting_rows == 3, "a clipped band must not count as an observed fourth row");

        // 下方背景可见时，下边缘才是已知位置；完整的灰色和有色色带都应保留。
        image.rowRange(283, 287).setTo(cv::Scalar(25, 25, 25));
        const auto complete = FitRarityGrid(image.rowRange(0, 284), x_starts, coarse_y, profile);
        Check(complete && complete->origin == 12 && complete->pitch == 69, "complete bottom band must preserve the lattice");
        Check(complete->supporting_rows == 4, "a complete bottom band must remain valid evidence");
    }
}

void TestRarityUsesBottomEdgeRows()
{
    cv::Mat image = cv::Mat::zeros(100, 100, CV_8UC3);
    cv::Mat lab(1, 1, CV_8UC3, cv::Scalar(198, 98, 191));
    cv::Mat bgr;
    cv::cvtColor(lab, bgr, cv::COLOR_Lab2BGR);
    image(cv::Rect(10, 74, 64, 1)).setTo(bgr.at<cv::Vec3b>(0, 0));

    const auto rarity = iconrecognition::detail::ClassifyRarity(image, cv::Rect(10, 10, 64, 64), 1.0);
    Check(rarity.rarity == 2, "rarity must use rows around the slot bottom edge");
    Check(std::abs(rarity.coverage - 1.0) <= 1e-6, "rarity coverage must preserve the selected row evidence");
    Check(rarity.row_offset == 0, "rarity row offset must be relative to the slot bottom edge");

    const auto absent = iconrecognition::detail::ClassifyRarity(cv::Mat::zeros(100, 100, CV_8UC3), cv::Rect(10, 10, 64, 64), 1.0);
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

void TestTrustedRarityIgnoresConnectedSpecks()
{
    cv::Mat image(120, 140, CV_8UC3, cv::Scalar(35, 40, 46));
    const cv::Scalar rarity = RarityBgr(5);
    image(cv::Rect(20, 70, 64, 2)).setTo(rarity);
    // 模拟数量文字等动态同色杂点：它们会与色带连成超过固定高度的组件，
    // 但自身宽度不足一个色带，不能成为稀有度条的核心证据。
    image(cv::Rect(20, 60, 20, 11)).setTo(rarity);
    image(cv::Rect(20, 72, 8, 1)).setTo(rarity);

    const auto strips = iconrecognition::detail::DetectTrustedRarityStrips(image, 64);
    Check(strips.size() == 1, "connected narrow specks must not create extra rarity strips");
    Check(strips.front().box == cv::Rect(20, 70, 64, 2), "trusted rarity must keep the full-width strip core");
    Check(strips.front().color_coverage >= 0.95, "connected specks must not dilute strip color coverage");
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
    Check(fit->endpoint_drift <= 1.0, "selected pitch must keep endpoint drift bounded");
    const auto starts = iconrecognition::detail::ProjectRegularAxis(*fit);
    for (std::size_t index = 0; index < starts.size(); ++index) {
        Check(
            starts[index] == cvRound(fit->origin + static_cast<double>(index + fit->minimum_index) * fit->pitch),
            "every integer start must project directly from one global model");
    }
}

void TestRegularLatticeUsesObservedPitchTolerance()
{
    const std::vector<iconrecognition::detail::LatticeObservation> quantized {
        { 618.0, 1.0, true }, { 687.0, 1.0, true }, { 755.0, 1.0, true }, { 824.0, 1.0, true }, { 893.0, 1.0, true },
    };
    Check(
        !iconrecognition::detail::FitRegularAxis(quantized, 5, { 69.0, 69.0 }, 69.0),
        "fixed pitch must reject quantized observations when no tolerance is supplied");
    const auto fit = iconrecognition::detail::FitRegularAxis(quantized, 5, { 69.0, 69.0 }, 69.0, 1.0);
    Check(fit.has_value(), "fixed pitch must accept one-pixel quantization with observed tolerance");
    Check(std::abs(fit->pitch - 69.0) <= 1e-9, "observed tolerance must not change the formal output pitch");
    Check(
        iconrecognition::detail::ProjectRegularAxis(*fit) == std::vector<int> { 617, 686, 755, 824, 893 },
        "fixed pitch projection must remain regular");
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

iconrecognition::detail::PreparedTemplate BuildEdgeOcclusionFixture()
{
    iconrecognition::detail::PreparedTemplate fixture;
    fixture.record.item_id = "edge-occlusion-fixture";
    fixture.image = cv::Mat::zeros(80, 80, CV_8UC3);
    for (int y = 0; y < fixture.image.rows; ++y) {
        for (int x = 0; x < fixture.image.cols; ++x) {
            fixture.image.at<cv::Vec3b>(y, x) = cv::Vec3b(
                static_cast<unsigned char>((x * 17 + y * 3) % 256),
                static_cast<unsigned char>((x * 5 + y * 19) % 256),
                static_cast<unsigned char>((x * 11 + y * 7) % 256));
        }
    }
    fixture.mask = cv::Mat(80, 80, CV_8UC1, cv::Scalar(255));
    return fixture;
}

void TestEdgeOcclusionDetectsContinuousTopAndBottomBands()
{
    const auto fixture = BuildEdgeOcclusionFixture();
    const cv::Rect slot(8, 8, 80, 80);
    for (const auto& [side, occluded] : std::array {
             std::pair { iconrecognition::detail::EdgeOcclusionSide::Top, cv::Rect(0, 0, 80, 20) },
             std::pair { iconrecognition::detail::EdgeOcclusionSide::Bottom, cv::Rect(0, 48, 80, 32) },
         }) {
        cv::Mat image = cv::Mat::zeros(96, 96, CV_8UC3);
        fixture.image.copyTo(image(slot));
        image(slot)(occluded).setTo(cv::Scalar(250, 8, 245));

        const auto detected = iconrecognition::detail::DetectEdgeOcclusion(image, slot, fixture, {});
        Check(detected.has_value(), "a continuous edge obstruction must produce a dynamic mask");
        Check(detected->side == side, "dynamic edge mask must preserve the obstructed side");
        if (side == iconrecognition::detail::EdgeOcclusionSide::Top) {
            Check(detected->cutoff >= 18 && detected->cutoff <= 22, "top obstruction cutoff must follow its measured boundary");
        }
        else {
            Check(detected->cutoff >= 46 && detected->cutoff <= 50, "bottom obstruction cutoff must follow its measured boundary");
        }

        cv::Mat mask = fixture.mask.clone();
        iconrecognition::detail::ApplyEdgeOcclusionMask(mask, *detected);
        const int excluded_row = side == iconrecognition::detail::EdgeOcclusionSide::Top ? 0 : 79;
        const int retained_row = side == iconrecognition::detail::EdgeOcclusionSide::Top ? 79 : 0;
        Check(cv::countNonZero(mask.row(excluded_row)) == 0, "detected edge band must be excluded from template matching");
        Check(cv::countNonZero(mask.row(retained_row)) == 80, "the opposite unoccluded edge must remain active");
    }
}

void TestEdgeOcclusionRejectsUniformResiduals()
{
    const auto fixture = BuildEdgeOcclusionFixture();
    const cv::Rect slot(8, 8, 80, 80);
    cv::Mat image = cv::Mat::zeros(96, 96, CV_8UC3);
    cv::add(fixture.image, cv::Scalar(12, 12, 12), image(slot));

    Check(
        !iconrecognition::detail::DetectEdgeOcclusion(image, slot, fixture, {}),
        "a whole-icon color difference must not be misclassified as an edge obstruction");
}

void TestEdgeOcclusionRejectsSubpixelBoundaryFill()
{
    auto fixture = BuildEdgeOcclusionFixture();
    fixture.image.setTo(cv::Scalar(80, 120, 160));
    const cv::Rect slot(8, 8, 80, 80);
    cv::Mat image = cv::Mat::zeros(96, 96, CV_8UC3);
    fixture.image.copyTo(image(slot));

    for (const auto phase : std::array {
             iconrecognition::detail::Phase { 0.0, 0.25 },
             iconrecognition::detail::Phase { 0.0, -0.25 },
             iconrecognition::detail::Phase { 0.0, 1.0 },
             iconrecognition::detail::Phase { 0.0, -1.0 },
         }) {
        Check(
            !iconrecognition::detail::DetectEdgeOcclusion(image, slot, fixture, phase),
            "subpixel transform boundary fill must not be misclassified as an edge obstruction");
    }
}

void TestEdgeOcclusionSkipsRewardsAndSingleRoi()
{
    for (const auto type : std::array {
             iconrecognition::GridType::Trade,
             iconrecognition::GridType::Transfer,
             iconrecognition::GridType::PortStorager,
             iconrecognition::GridType::Valuables,
             iconrecognition::GridType::Shipment,
             iconrecognition::GridType::CreditTrade,
         }) {
        Check(iconrecognition::detail::SupportsEdgeOcclusion(type), "regular grids must support edge-obstruction recovery");
    }
    Check(
        !iconrecognition::detail::SupportsEdgeOcclusion(iconrecognition::GridType::Rewards),
        "reward cards must skip edge-obstruction recovery");
    Check(
        !iconrecognition::detail::SupportsEdgeOcclusion(iconrecognition::GridType::SingleRoi),
        "fixed single ROI must skip edge-obstruction recovery");
}

void TestEdgeOcclusionRecoveryPolicyIsConservative()
{
    Check(
        iconrecognition::detail::ShouldAttemptEdgeOcclusionRecovery(iconrecognition::GridType::Trade, 0.82, 0.85, 0.60, false),
        "a regular-grid candidate rejected after subpixel refinement must enter edge recovery");
    Check(
        !iconrecognition::detail::ShouldAttemptEdgeOcclusionRecovery(iconrecognition::GridType::Trade, 0.91, 0.90, 0.60, false),
        "an already accepted candidate must not pay for edge recovery");
    Check(
        !iconrecognition::detail::ShouldAttemptEdgeOcclusionRecovery(iconrecognition::GridType::Transfer, 0.82, 0.85, 0.60, true),
        "a low-texture transfer cell must remain rejected before edge recovery");

    Check(
        iconrecognition::detail::ShouldAcceptEdgeOcclusionRecovery(3, 3, 0.88, 0.32, 0.85),
        "recovery must accept the same candidate above the caller threshold with a strong margin");
    Check(
        !iconrecognition::detail::ShouldAcceptEdgeOcclusionRecovery(3, 4, 0.94, 0.40, 0.85),
        "recovery must not replace the original top candidate after hiding an edge");
    Check(
        !iconrecognition::detail::ShouldAcceptEdgeOcclusionRecovery(3, 3, 0.88, 0.12, 0.85),
        "recovery must reject an ambiguous masked ranking");
    Check(
        !iconrecognition::detail::ShouldAcceptEdgeOcclusionRecovery(3, 3, 0.89, 0.40, 0.90),
        "recovery must honor a caller-supplied threshold instead of the default threshold");
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

void TestDisabledTemplateScalesCenteredOverlaysFrom128PixelReference()
{
    const cv::Mat dark_band(28, 120, CV_8UC4, cv::Scalar(0, 0, 0, 255));
    const cv::Mat white_mark(24, 24, CV_8UC4, cv::Scalar(255, 255, 255, 255));
    const std::array cases {
        std::tuple { 64, cv::Rect(2, 25, 30, 14), cv::Rect(26, 26, 12, 12) },
        std::tuple { 80, cv::Rect(2, 31, 38, 18), cv::Rect(32, 32, 15, 15) },
        std::tuple { 96, cv::Rect(3, 37, 45, 21), cv::Rect(39, 39, 18, 18) },
        std::tuple { 128, cv::Rect(4, 50, 60, 28), cv::Rect(52, 52, 24, 24) },
    };
    const auto WithinOnePixel = [](const cv::Rect actual, const cv::Rect expected) {
        return std::abs(actual.x - expected.x) <= 1 && std::abs(actual.y - expected.y) <= 1 && std::abs(actual.width - expected.width) <= 1
               && std::abs(actual.height - expected.height) <= 1;
    };

    for (const auto& [target_size, expected_band, expected_mark] : cases) {
        iconrecognition::detail::PreparedTemplate base {
            .record = iconrecognition::detail::TemplateRecord { .item_id = "restricted" },
            .image = cv::Mat(target_size, target_size, CV_8UC3, cv::Scalar(100, 120, 140)),
            .mask = cv::Mat::zeros(target_size, target_size, CV_8UC1),
            .composite = true,
        };
        base.mask.colRange(0, target_size / 2).setTo(cv::Scalar(255));

        const auto disabled = iconrecognition::detail::BuildRegionUnavailableTemplate(base, dark_band, white_mark, 230);
        Check(disabled.region_unavailable, "region-unavailable template must retain its variant state");
        Check(disabled.composite, "disabled template must preserve the base composite state");
        Check(disabled.record.item_id == "restricted", "disabled template must preserve item metadata");
        Check(disabled.image.size() == cv::Size(target_size, target_size), "disabled template must preserve target size");

        const cv::Point band_center {
            expected_band.x + expected_band.width / 2,
            expected_band.y + expected_band.height / 2,
        };
        Check(
            disabled.mask.at<unsigned char>(band_center.y, 0) == 0,
            "the disabled band row must be excluded across the full template width");

        cv::Mat white_pixels;
        cv::inRange(disabled.image, cv::Scalar(255, 255, 255), cv::Scalar(255, 255, 255), white_pixels);
        Check(
            WithinOnePixel(cv::boundingRect(white_pixels), expected_mark),
            "disabled white mark must use S/128 scaling and full-canvas centering");

        const cv::Vec3b band_pixel = disabled.image.at<cv::Vec3b>(band_center);
        Check(
            band_pixel[0] < 20 && band_pixel[1] < 20 && band_pixel[2] < 20,
            "disabled dark band must use S/128 scaling and full-canvas centering");
        Check(
            disabled.image.at<cv::Vec3b>(target_size / 2, target_size - 1) == cv::Vec3b(100, 120, 140),
            "dark band must not alter pixels outside the base template mask");
        Check(
            disabled.mask.at<unsigned char>(expected_mark.y, expected_mark.x) == 0,
            "disabled white mark must be excluded from the template mask");
        Check(disabled.mask.at<unsigned char>(band_center) == 0, "disabled dark band must be excluded from the template mask");
        Check(
            disabled.mask.at<unsigned char>(target_size / 2, target_size - 1) == 0,
            "dark band must not extend the disabled template mask");
    }
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

void TestCatalogUsesGameSortOrderBeforeItemId()
{
    const std::filesystem::path fixture = "agent/cpp-algo/source/IconRecognition/test/build/generated-sorted-catalog";
    std::filesystem::remove_all(fixture);
    const auto data_root = fixture / "data";
    std::filesystem::create_directories(data_root);
    std::ofstream(data_root / "recognition_items.json", std::ios::binary | std::ios::trunc)
        << R"({"unsorted":{"name":"无排序","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"unsorted","fluidIconId":""},"lower":{"name":"低排序","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"lower","fluidIconId":"","sortId1":-100,"sortId2":5},"same_a":{"name":"同序甲","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"same_a","fluidIconId":"","sortId1":-80,"sortId2":4},"same_b":{"name":"同序乙","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"same_b","fluidIconId":"","sortId1":-80,"sortId2":4},"higher":{"name":"高排序","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"higher","fluidIconId":"","sortId1":-80,"sortId2":6}})";

    iconrecognition::detail::TemplateCatalog catalog(data_root, fixture / "images");
    Check(catalog.initialize(), "sorted catalog fixture must initialize");
    std::vector<std::string> item_ids;
    std::ranges::transform(catalog.records(), std::back_inserter(item_ids), [](const auto& record) { return record.item_id; });
    Check(
        item_ids == std::vector<std::string>({ "higher", "same_b", "same_a", "lower", "unsorted" }),
        "catalog must order sortId1, sortId2 and item_id descending before unsorted records");
}

void TestCatalogLoadsOnlyRegionRestrictedDisabledVariantsOnDemand()
{
    const std::filesystem::path fixture = "agent/cpp-algo/source/IconRecognition/test/build/generated-disabled-catalog";
    std::filesystem::remove_all(fixture);
    const auto data_root = fixture / "data";
    const auto image_root = fixture / "images";
    std::filesystem::create_directories(data_root);
    std::filesystem::create_directories(image_root / "1");
    std::ofstream(data_root / "recognition_items.json", std::ios::binary | std::ios::trunc)
        << R"({"restricted":{"name":"受限物品","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"restricted","fluidIconId":"","regionRestricted":true},"normal":{"name":"普通物品","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"normal","fluidIconId":""},"explicit_false":{"name":"普通物品二","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"explicit_false","fluidIconId":"","regionRestricted":false}})";
    for (const std::string_view item_id : { "restricted", "normal", "explicit_false" }) {
        Check(
            cv::imwrite(
                (image_root / "1" / (std::string(item_id) + ".png")).string(),
                cv::Mat(32, 32, CV_8UC4, cv::Scalar(10, 20, 30, 255))),
            "unable to write disabled catalog icon fixture");
    }

    iconrecognition::detail::TemplateCatalog catalog(data_root, image_root);
    Check(catalog.initialize(), "disabled catalog fixture must initialize");
    const auto restricted = std::ranges::find_if(catalog.records(), [](const auto& record) { return record.item_id == "restricted"; });
    Check(restricted != catalog.records().end() && restricted->region_restricted, "catalog must retain regionRestricted=true");
    Check(
        std::ranges::count_if(catalog.records(), [](const auto& record) { return record.region_restricted; }) == 1,
        "missing and false regionRestricted values must remain ordinary records");

    const auto& normal_templates = catalog.load(64);
    Check(normal_templates.size() == 3, "normal catalog load must not depend on disabled overlays");
    Check(
        std::ranges::none_of(normal_templates, [](const auto& templ) { return templ.region_unavailable; }),
        "normal catalog load must not generate disabled variants");

    bool missing_overlay_rejected = false;
    try {
        static_cast<void>(catalog.loadRegionUnavailable(64));
    }
    catch (const std::runtime_error&) {
        missing_overlay_rejected = true;
    }
    Check(missing_overlay_rejected, "disabled catalog load must read overlays lazily");

    std::filesystem::create_directories(image_root / "Overlay");
    Check(
        cv::imwrite(
            (image_root / "Overlay" / "icon_placement_disabled_bg.png").string(),
            cv::Mat(28, 120, CV_8UC4, cv::Scalar(0, 0, 0, 255))),
        "unable to write disabled background fixture");
    Check(
        cv::imwrite(
            (image_root / "Overlay" / "icon_placement_disabled.png").string(),
            cv::Mat(24, 24, CV_8UC4, cv::Scalar(255, 255, 255, 255))),
        "unable to write disabled mark fixture");
    const auto& disabled_templates = catalog.loadRegionUnavailable(64);
    Check(disabled_templates.size() == 1, "disabled catalog must contain only region-restricted items");
    Check(
        disabled_templates.front().record.item_id == "restricted" && disabled_templates.front().region_unavailable,
        "disabled catalog must preserve the original item id and mark the variant");
}

void TestCatalogRejectsNonBooleanRegionRestricted()
{
    const std::filesystem::path fixture = "agent/cpp-algo/source/IconRecognition/test/build/generated-invalid-region-restricted";
    std::filesystem::remove_all(fixture);
    const auto data_root = fixture / "data";
    std::filesystem::create_directories(data_root);
    std::ofstream(data_root / "recognition_items.json", std::ios::binary | std::ios::trunc)
        << R"({"invalid":{"name":"非法物品","category":"test","storageKind":"Normal","categoryType":"Product","rarity":1,"iconId":"invalid","fluidIconId":"","regionRestricted":1}})";

    bool rejected = false;
    try {
        iconrecognition::detail::TemplateCatalog catalog(data_root, fixture / "images");
        static_cast<void>(catalog.initialize());
    }
    catch (const std::runtime_error& error) {
        rejected = std::string_view(error.what()).find("regionRestricted") != std::string_view::npos;
    }
    Check(rejected, "catalog must reject non-boolean regionRestricted with a field-specific error");
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

} // namespace

int main()
{
    try {
        TestCandidateSelectionUsesDocumentedSetOrder();
        TestCandidateSelectionWithoutIdsSkipsIntersection();
        TestCandidateSelectionTreatsDuplicateValuesAsOne();
        TestCandidateSelectionDeduplicatesCompositeIconIdentity();
        TestCandidateSelectionExactIdRetainsFilteredAliases();
        TestCandidateSelectionRejectsInvalidRequests();
        TestLowerExtendedMaskSnapshots();
        TestShipmentQuantityBarThreshold();
        TestShipmentQuantityBarThresholdScalesWithCellArea();
        TestShipmentTopBarMaskScalesWithCellHeight();
        TestValuablesPortraitMaskScalesWithCellSize();
        TestMaskDiagnosticsDescribeComposedPolicies();
        TestValuablesPortraitDetectionDoesNotDependOnTemplateMask();
        TestForegroundTextureUsesContentInsets();
        TestForegroundTextureUsesNativeLargerCell();
        TestForegroundTextureInsetsStayGridSpecific();
        TestTransferPanelIntersections();
        TestTransferTextureClipsUiWithoutRecroppingCell();
        TestStructureFeatureModuleContract();
        TestGridGeometryModuleContract();
        TestGridScaleEstimateSelectsCalibratedProfiles();
        TestTransferRarityGridKeepsVisibleTopRow();
        TestTransferBottomVisibilityIsGridSpecific();
        TestGridDetectorMapsNormalizedCellsBackToSourceImage();
        TestRewardsGridScaleSelectsCardProfileInsideCallerRoi();
        TestRewardsGridScaleIgnoresBrightBackgroundWithoutRarityBand();
        TestControllerTypeSelectsKnownGridScale();
        TestExplicitGridScaleHintBypassesImageEstimate();
        TestTradeGridUsesCardBoundariesForVerticalPhase();
        TestValuablesCardExtentUsesScaledProfileOcclusionPolicy();
        TestPortOcclusionPolicyDropsOnlyWeakSevenColumnFirstRow();
        TestRewardsRowCompletesInternalMissingCards();
        TestRewardsDefaultFiltersIncludeAllRewardStorageKinds();
        TestShipmentProfileAcceptsTwoCompleteRows();
        TestRewardsGridKeepsBottomRarityBandInsideCell();
        TestRewardsSingleCardRoiClampsSmallBodyPhaseOffset();
        TestRewardsGridUsesCenteredSharedOriginWithoutFixedColumnCount();
        TestRewardsGridRejectsOffCenterFalseCard();
        TestRewardsAdbGridUsesSixColumnSharedOrigin();
        TestRewardsGridRenumbersColumnsAfterRoiFiltering();
        TestTransferRegionPartitionKeepsUndetectedOuterColumns();
        TestTransferGridDetectsSparseVisiblePhase();
        TestTransferGridRejectsBroadOvercapacityPhase();
        TestTransferEmptyGridFitsOneCompleteLattice();
        TestTransferEmptyGridSkipsCroppedTopRowAndKeepsWeakColumn();
        TestTransferEmptyGridDiagnosticsSkipUnexecutedRarity();
        TestPortStoragerWideRoiUsesStablePanelPartitions();
        TestCreditTradeGridUsesDimCardStructures();
        TestCreditTradeGridUsesSixColumnsWhenRoiCannotContainSeven();
        TestValuablesGridKeepsSixColumnsAtAdbDensity();
        TestRarityRowEvidenceKeepsAllSixChannels();
        TestTrustedRarityRejectsSameColorBackground();
        TestTrustedRarityIgnoresConnectedSpecks();
        TestGrayRarityCannotSeedLattice();
        TestRegularLatticeUsesOneGlobalFloatingPitch();
        TestRegularLatticeUsesObservedPitchTolerance();
        TestRegularLatticeRejectsAccumulatingResiduals();
        TestRarityBandsRecoverGridFromGlobalEvidence();
        TestRarityFitPreservesSupportedRowRange();
        TestRarityFitRejectsClippedBandEndpoint();
        TestRarityUsesBottomEdgeRows();
        TestRarityCandidatePassesAreDisjointAndComplete();
        TestMatcherSearchRadiusIsExplicit();
        TestSubpixelPhasesAreStable();
        TestEdgeOcclusionDetectsContinuousTopAndBottomBands();
        TestEdgeOcclusionRejectsUniformResiduals();
        TestEdgeOcclusionRejectsSubpixelBoundaryFill();
        TestEdgeOcclusionSkipsRewardsAndSingleRoi();
        TestEdgeOcclusionRecoveryPolicyIsConservative();
        TestTemplatePreparationUsesExpectedMasks();
        TestDisabledTemplateScalesCenteredOverlaysFrom128PixelReference();
        TestCatalogBuildsFinalSizeDirectlyFromSourceAssets();
        TestCatalogUsesGameSortOrderBeforeItemId();
        TestCatalogLoadsOnlyRegionRestrictedDisabledVariantsOnDemand();
        TestCatalogRejectsNonBooleanRegionRestricted();
        TestIconPathResolutionDoesNotAssumeCatalogRarity();
        TestCatalogConcurrentLoadIsStable();
        TestCatalogFailedLoadDoesNotPoisonCache();
        TestDecodeBgraRejectsNonStandardSourceSizes();
        TestArbitrarySquareRoiUsesItsFinalSize();
        std::cout << "IconRecognition small algorithm tests passed\n";
        return 0;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}

#endif
