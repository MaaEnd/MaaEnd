#ifdef ICON_RECOGNITION_TEST_MAIN

#include "../IconRecognizer.h"
#include "../detail/CandidateSelector.h"
#include "../detail/EdgeOcclusion.h"
#include "../detail/GridAnchors.h"
#include "../detail/GridDetector.h"
#include "../detail/GridGeometry.h"
#include "../detail/GridProfiles.h"
#include "../detail/MaskPolicy.h"
#include "../detail/RarityCandidates.h"
#include "../detail/RegularLattice.h"
#include "../detail/SubpixelMatcher.h"
#include "../detail/TemplateCatalog.h"
#include "../detail/TemplateTypes.h"

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
    Check(selected.front().record.aliases.front().name_key == "iconRecognition.name.alias", "shared composite icon alias name mismatch");

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
        alias_requested.front().record.aliases.size() == 2 && alias_requested.front().record.aliases.front().item_id == "additional_alias"
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

void TestTransferBottomVisibilityIsGridSpecific()
{
    using namespace iconrecognition::detail;
    Check(TransferProfileFor(TransferGridVariant::TransferLeft).minimum_bottom_visibility == 0.65, "transfer left bottom visibility");
    Check(TransferProfileFor(TransferGridVariant::TransferRight).minimum_bottom_visibility == 0.65, "transfer right bottom visibility");
    Check(TransferProfileFor(TransferGridVariant::PortStoragerLeft).minimum_bottom_visibility == 0.70, "port left visibility unchanged");
    Check(TransferProfileFor(TransferGridVariant::PortStoragerRight).minimum_bottom_visibility == 0.80, "port right visibility unchanged");
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

void TestValuablesGridKeepsSixColumnsAtAdbDensity()
{
    const auto profile = iconrecognition::detail::ProfileFor(iconrecognition::GridType::Valuables);
    Check(profile.min_columns == 6, "valuables profile must allow the six-column ADB layout");
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
        TestShipmentTopBarMaskScalesWithCellHeight();
        TestValuablesPortraitMaskScalesWithCellSize();
        TestMaskDiagnosticsDescribeComposedPolicies();
        TestTransferPanelIntersections();
        TestGridGeometryModuleContract();
        TestTransferBottomVisibilityIsGridSpecific();
        TestControllerTypeSelectsKnownGridScale();
        TestValuablesCardExtentUsesScaledProfileOcclusionPolicy();
        TestPortOcclusionPolicyDropsOnlyWeakSevenColumnFirstRow();
        TestRewardsRowCompletesInternalMissingCards();
        TestRewardsDefaultFiltersIncludeAllRewardStorageKinds();
        TestShipmentProfileAcceptsTwoCompleteRows();
        TestTransferRegionPartitionKeepsUndetectedOuterColumns();
        TestPortStoragerWideRoiUsesStablePanelPartitions();
        TestValuablesGridKeepsSixColumnsAtAdbDensity();
        TestRegularLatticeUsesOneGlobalFloatingPitch();
        TestRegularLatticeUsesObservedPitchTolerance();
        TestRegularLatticeRejectsAccumulatingResiduals();
        TestRarityCandidatePassesAreDisjointAndComplete();
        TestSubpixelPhasesAreStable();
        TestEdgeOcclusionSkipsRewardsAndSingleRoi();
        TestEdgeOcclusionRecoveryPolicyIsConservative();
        TestCatalogBuildsFinalSizeDirectlyFromSourceAssets();
        TestCatalogUsesGameSortOrderBeforeItemId();
        TestCatalogRejectsNonBooleanRegionRestricted();
        TestCatalogConcurrentLoadIsStable();
        std::cout << "IconRecognition small algorithm tests passed\n";
        return 0;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}

#endif
