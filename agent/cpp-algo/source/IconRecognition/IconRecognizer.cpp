#include "IconRecognizer.h"

#include <algorithm>
#include <filesystem>
#include <memory>
#include <set>
#include <stdexcept>
#include <string_view>
#include <tuple>
#include <unordered_set>

#include <MaaUtils/Logger.h>

#include "detail/ForegroundTexture.h"
#include "detail/GridDetector.h"
#include "detail/GridProfiles.h"
#include "detail/IconMatcher.h"
#include "detail/MaskPolicy.h"
#include "detail/RarityClassifier.h"
#include "detail/RecognitionDiagnostics.h"
#include "detail/TemplateCatalog.h"

namespace iconrecognition
{
namespace
{

constexpr int kGridSearchRadius = 2;
constexpr int kTradeTemplateSize = 88;
constexpr int kCreditTradeTemplateSize = 140;
constexpr int kCreditTradeOffsetX = -6;
constexpr int kCreditTradeOffsetY = 4;
constexpr int kShortlistCount = 5;
constexpr double kShortlistScoreWindow = 0.08;

const std::vector<std::string>& DefaultFilters(GridType type)
{
    static const std::vector<std::string> normal { "Normal:*" };
    static const std::vector<std::string> trade { "Normal:Product", "Normal:Usable" };
    static const std::vector<std::string> valuables { "ValuableDepot:*" };
    static const std::vector<std::string> credit { "ValuableDepot:SpecialItem", "Isolate:*" };
    switch (type) {
    case GridType::Trade:
        return trade;
    case GridType::Valuables:
        return valuables;
    case GridType::CreditTrade:
        return credit;
    case GridType::Transfer:
    case GridType::PortStorager:
    case GridType::Shipment:
    case GridType::SingleRoi:
        return normal;
    }
    return normal;
}

bool MatchesFilter(const detail::TemplateRecord& record, std::string_view filter)
{
    const auto separator = filter.find(':');
    if (separator == std::string_view::npos || filter.find(':', separator + 1) != std::string_view::npos) {
        throw std::invalid_argument("item_filters must use storageKind:categoryType");
    }
    const std::string_view storage = filter.substr(0, separator);
    const std::string_view category = filter.substr(separator + 1);
    if (storage.empty() || category.empty()) {
        throw std::invalid_argument("item_filters must use non-empty storageKind:categoryType");
    }
    return storage == record.storage_kind && (category == "*" || category == record.category_type);
}

std::vector<detail::PreparedTemplate> SelectTemplates(
    const std::vector<detail::PreparedTemplate>& all,
    const CandidateFilter& candidates,
    const std::vector<std::string>& defaults)
{
    const auto& filters = candidates.item_filters.empty() ? defaults : candidates.item_filters;
    std::vector<detail::PreparedTemplate> filtered;
    for (const auto& templ : all) {
        if (std::ranges::any_of(filters, [&](const auto& filter) { return MatchesFilter(templ.record, filter); })) {
            filtered.push_back(templ);
        }
    }
    if (filtered.empty()) {
        throw std::invalid_argument("item_filters selected no candidate templates");
    }
    if (candidates.item_ids.empty()) {
        return filtered;
    }

    const std::set<std::string> unique_ids(candidates.item_ids.begin(), candidates.item_ids.end());
    if (unique_ids.size() != candidates.item_ids.size()) {
        throw std::invalid_argument("item_ids must not contain duplicates");
    }
    const auto find_by_id = [](const auto& templates, const std::string& item_id) {
        return std::ranges::find_if(templates, [&](const auto& templ) { return templ.record.item_id == item_id; });
    };
    std::vector<detail::PreparedTemplate> result;
    for (const auto& item_id : candidates.item_ids) {
        if (find_by_id(all, item_id) == all.end()) {
            throw std::invalid_argument("recognition catalog does not contain item_id: " + item_id);
        }
        const auto selected = find_by_id(filtered, item_id);
        if (selected == filtered.end()) {
            throw std::invalid_argument("item_id is excluded by item_filters: " + item_id);
        }
        result.push_back(*selected);
    }
    return result;
}

void ValidateThresholds(double accept, double subpixel)
{
    if (!(0.0 <= subpixel && subpixel < accept && accept <= 1.0)) {
        throw std::invalid_argument("thresholds must satisfy 0 <= subpixel_threshold < threshold <= 1");
    }
}

cv::Rect SlotFor(GridType type, const detail::GridCell& cell)
{
    if (type == GridType::Trade) {
        const int inset = (cell.cell_box.width - kTradeTemplateSize) / 2;
        return cv::Rect(cell.cell_box.x + inset, cell.cell_box.y + inset, kTradeTemplateSize, kTradeTemplateSize);
    }
    if (type == GridType::CreditTrade) {
        return cv::Rect(
            cell.cell_box.x + kCreditTradeOffsetX,
            cell.cell_box.y + kCreditTradeOffsetY,
            kCreditTradeTemplateSize,
            kCreditTradeTemplateSize);
    }
    return cell.cell_box;
}

std::vector<detail::PreparedTemplate>
    ActiveTemplates(const cv::Mat& image, GridType type, const cv::Rect& slot, const std::vector<detail::PreparedTemplate>& templates)
{
    if (type != GridType::Shipment && type != GridType::Valuables) {
        return templates;
    }
    const cv::Rect bounds(0, 0, image.cols, image.rows);
    if ((slot & bounds) != slot) {
        return templates;
    }
    const cv::Mat slot_image = image(slot);
    std::vector<detail::PreparedTemplate> active = templates;
    if (type == GridType::Shipment) {
        if (!detail::HasShipmentTopBar(slot_image)) {
            return templates;
        }
        for (auto& templ : active) {
            templ.mask = templ.mask.clone();
            templ.mask.rowRange(0, std::min(20, templ.mask.rows)).setTo(cv::Scalar(0));
        }
        return active;
    }
    cv::Mat probe = active.front().mask.clone();
    const int before = cv::countNonZero(probe);
    detail::ClearValuablesWeaponPortrait(probe, slot_image);
    if (cv::countNonZero(probe) == before) {
        return templates;
    }
    for (auto& templ : active) {
        templ.mask = templ.mask.clone();
        cv::circle(templ.mask, cv::Point(81, 15), 20, cv::Scalar(0), cv::FILLED);
    }
    return active;
}

struct RankedCandidate
{
    std::size_t template_index = 0;
    detail::MatchDiagnostics diagnostics;
    detail::MatchDiagnostics baseline;
    detail::Phase phase;
};

struct SlotRanking
{
    RankedCandidate best;
    std::vector<RankedCandidate> ranked;
    double baseline_score = 0.0;
    bool fallback_used = false;
};

bool CandidateLess(const RankedCandidate& left, const RankedCandidate& right, const std::vector<detail::PreparedTemplate>& templates)
{
    if (left.diagnostics.score != right.diagnostics.score) {
        return left.diagnostics.score > right.diagnostics.score;
    }
    return templates[left.template_index].record.item_id < templates[right.template_index].record.item_id;
}

bool PhaseScoreBetter(const detail::MatchDiagnostics& candidate, const detail::Phase& phase, const RankedCandidate& best)
{
    return std::tuple { candidate.score, phase.x, phase.y } > std::tuple { best.diagnostics.score, best.phase.x, best.phase.y };
}

RankedCandidate RefineCandidate(
    const cv::Mat& image,
    const cv::Rect& slot,
    const std::vector<detail::PreparedTemplate>& templates,
    RankedCandidate candidate,
    int search_radius)
{
    for (const detail::Phase phase : detail::PhaseGrid()) {
        if (phase.x == 0.0 && phase.y == 0.0) {
            continue;
        }
        const auto diagnostics = detail::ScoreTemplateAt(image, slot, templates[candidate.template_index], search_radius, phase);
        if (PhaseScoreBetter(diagnostics, phase, candidate)) {
            candidate.diagnostics = diagnostics, candidate.phase = phase;
        }
    }
    for (const detail::Phase phase : detail::BoundaryExtensionPhases(candidate.phase)) {
        const auto diagnostics = detail::ScoreTemplateAt(image, slot, templates[candidate.template_index], search_radius, phase);
        if (PhaseScoreBetter(diagnostics, phase, candidate)) {
            candidate.diagnostics = diagnostics, candidate.phase = phase;
        }
    }
    return candidate;
}

SlotRanking RankSlot(
    const cv::Mat& image,
    const cv::Rect& slot,
    const std::vector<detail::PreparedTemplate>& templates,
    double accept,
    double subpixel,
    int search_radius)
{
    std::vector<RankedCandidate> ranked;
    ranked.reserve(templates.size());
    for (std::size_t index = 0; index < templates.size(); ++index) {
        auto diagnostics = detail::ScoreTemplateAt(image, slot, templates[index], search_radius, {});
        ranked.push_back({ index, diagnostics, diagnostics, {} });
    }
    std::ranges::sort(ranked, [&](const auto& left, const auto& right) { return CandidateLess(left, right, templates); });
    const double baseline_score = ranked.front().diagnostics.score;
    if (!(subpixel <= baseline_score && baseline_score < accept)) {
        return { ranked.front(), std::move(ranked), baseline_score, false };
    }

    std::vector<RankedCandidate> refined;
    for (std::size_t index = 0; index < ranked.size(); ++index) {
        if (index < kShortlistCount || ranked[index].diagnostics.score >= baseline_score - kShortlistScoreWindow) {
            refined.push_back(RefineCandidate(image, slot, templates, ranked[index], search_radius));
        }
    }
    std::ranges::sort(refined, [&](const auto& left, const auto& right) { return CandidateLess(left, right, templates); });
    return { refined.front(), std::move(refined), baseline_score, true };
}

std::string ActiveMaskKind(
    GridType type,
    const std::vector<detail::PreparedTemplate>& selected,
    const std::vector<detail::PreparedTemplate>& active)
{
    if (!active.empty() && active.front().composite) {
        return "composite_union";
    }
    if (selected.empty() || active.empty()) {
        return "lower_extended";
    }
    if (cv::norm(selected.front().mask, active.front().mask, cv::NORM_INF) == 0.0) {
        return "lower_extended";
    }
    if (type == GridType::Shipment) {
        return "shipment_top_bar";
    }
    if (type == GridType::Valuables) {
        return "valuables_weapon";
    }
    return "lower_extended";
}

ItemInfo ItemFromTemplate(const detail::PreparedTemplate& templ)
{
    return {
        templ.record.item_id,      templ.record.name_key,      templ.record.category,
        templ.record.storage_kind, templ.record.category_type, templ.record.rarity,
    };
}

} // namespace

class IconRecognizer::Impl
{
public:
    explicit Impl(std::filesystem::path data_root)
        : data_root_(std::move(data_root))
        , image_root_(data_root_.parent_path().parent_path() / "resource" / "image" / "IconRecognition")
        , catalog_(data_root_, image_root_)
    {
    }

    bool initialize() { return catalog_.initialize(); }

    RecognitionResult Error(cv::Rect roi, std::optional<GridType> type, std::string code, std::string message) const
    {
        RecognitionResult result;
        if (type) {
            result.grid_type = *type;
        }
        else {
            result.has_grid_type = false;
        }
        result.roi = roi;
        result.error_code = std::move(code);
        result.message = std::move(message);
        return result;
    }

    const std::vector<detail::PreparedTemplate>& TemplatesFor(GridType type) const
    {
        int target_size = detail::ProfileFor(type).cell_size;
        if (type == GridType::Trade) {
            target_size = kTradeTemplateSize;
        }
        if (type == GridType::CreditTrade) {
            target_size = kCreditTradeTemplateSize;
        }
        return catalog_.load(target_size);
    }

    const std::vector<detail::PreparedTemplate>& RoiTemplates(int target_size) const { return catalog_.load(target_size); }

    RecognitionResult recognize(const cv::Mat& image, const RecognitionRequest& request) const
    {
        try {
            if (image.empty()) {
                return Error(request.roi, request.grid_type, "invalid_image", "Input image is empty");
            }
            ValidateThresholds(request.threshold, request.subpixel_threshold);
            const bool single_roi = request.grid_type == GridType::SingleRoi;
            std::vector<detail::GridCell> cells;
            std::vector<detail::PreparedTemplate> selected;
            if (single_roi) {
                if (request.roi.width <= 0 || request.roi.width != request.roi.height) {
                    throw std::invalid_argument("single_roi must be a positive square");
                }
                const cv::Rect bounds(0, 0, image.cols, image.rows);
                if ((request.roi & bounds) != request.roi) {
                    throw std::invalid_argument("single_roi must be fully inside the image");
                }
                cells.push_back(detail::GridCell { .cell_box = request.roi });
                selected = SelectTemplates(RoiTemplates(request.roi.width), request.candidates, DefaultFilters(request.grid_type));
            }
            else {
                cells = detail::DetectGrid(image, request.grid_type, request.roi).cells;
                selected = SelectTemplates(TemplatesFor(request.grid_type), request.candidates, DefaultFilters(request.grid_type));
            }
            RecognitionResult result;
            result.grid_type = request.grid_type;
            result.roi = request.roi;
            result.diagnostics = std::make_shared<detail::RecognitionDiagnostics>();
            for (const auto& cell : cells) {
                const cv::Rect slot = single_roi ? cell.cell_box : SlotFor(request.grid_type, cell);
                const auto active = single_roi ? selected : ActiveTemplates(image, request.grid_type, slot, selected);
                const SlotRanking ranking =
                    RankSlot(image, slot, active, request.threshold, request.subpixel_threshold, single_roi ? 0 : kGridSearchRadius);
                const auto& best = ranking.best;
                const auto& templ = active[best.template_index];
                const auto foreground_texture =
                    single_roi ? std::optional<double> {} : detail::ForegroundTextureScore(image, cell.cell_box, request.grid_type);
                const auto rarity = single_roi ? detail::RarityResult {} : detail::ClassifyRarity(image, slot);
                const bool texture_rejected = !single_roi && best.diagnostics.score >= request.threshold
                                              && detail::IsLowTexture(image, cell.cell_box, request.grid_type);
                const bool accepted = best.diagnostics.score >= request.threshold && !texture_rejected;
                std::optional<std::string> rejected_reason;
                if (!accepted) {
                    rejected_reason = texture_rejected
                                          ? "low-foreground-texture"
                                          : (best.diagnostics.score < request.subpixel_threshold ? "below-subpixel-threshold"
                                                                                                 : "below-accept-threshold-after-fallback");
                }
                result.diagnostics->cells.push_back(detail::CellRecognitionDiagnostics {
                    .cell_box = cell.cell_box,
                    .candidate_box = cv::Rect(best.diagnostics.position, templ.image.size()),
                    .best_candidate_id = templ.record.item_id,
                    .baseline_score = ranking.baseline_score,
                    .score = best.diagnostics.score,
                    .top2_margin = ranking.ranked.size() > 1
                                       ? std::optional<double>(best.diagnostics.score - ranking.ranked[1].diagnostics.score)
                                       : std::nullopt,
                    .candidate_count = ranking.ranked.size(),
                    .fallback_used = ranking.fallback_used,
                    .best_phase = cv::Point2d(best.phase.x, best.phase.y),
                    .rejected_reason = rejected_reason,
                    .foreground_texture = foreground_texture,
                    .rarity = single_roi && accepted ? std::optional<int>(templ.record.rarity) : rarity.rarity,
                    .rarity_coverage = single_roi ? 0.0 : rarity.coverage,
                    .rarity_row_offset = single_roi ? std::optional<int> {} : rarity.row_offset,
                    .mask_kind = single_roi ? (templ.composite ? "composite_union" : "lower_extended")
                                            : ActiveMaskKind(request.grid_type, selected, active),
                    .row = single_roi ? std::optional<int> {} : std::optional<int>(cell.row),
                    .column = single_roi ? std::optional<int> {} : std::optional<int>(cell.column),
                });
                if (!accepted) {
                    continue;
                }
                result.matches.push_back(ItemMatch {
                    .item = ItemFromTemplate(templ),
                    .cell_box = cell.cell_box,
                    .item_box = cv::Rect(best.diagnostics.position, templ.image.size()),
                    .score = best.diagnostics.score,
                    .row = single_roi ? std::optional<int> {} : std::optional<int>(cell.row),
                    .column = single_roi ? std::optional<int> {} : std::optional<int>(cell.column),
                });
            }
            std::ranges::stable_sort(result.matches, ItemMatchLess {});
            if (request.deduplicate) {
                DeduplicateMatches(result.matches);
            }
            result.matched = !result.matches.empty();
            if (!result.matched) {
                result.error_code = "no_match", result.message = "No item reached the configured threshold";
            }
            return result;
        }
        catch (const std::exception& error) {
            LogError << "IconRecognizer recognition failed" << VAR(error.what());
            return Error(request.roi, request.grid_type, "exception", error.what());
        }
    }

    std::filesystem::path data_root_;
    std::filesystem::path image_root_;
    mutable detail::TemplateCatalog catalog_;
};

IconRecognizer::IconRecognizer(std::filesystem::path data_root)
    : impl_(std::make_unique<Impl>(std::move(data_root)))
{
}

IconRecognizer::~IconRecognizer() = default;
IconRecognizer::IconRecognizer(IconRecognizer&&) noexcept = default;
IconRecognizer& IconRecognizer::operator=(IconRecognizer&&) noexcept = default;

bool IconRecognizer::initialize()
{
    return impl_->initialize();
}

RecognitionResult IconRecognizer::recognize(const cv::Mat& image, const RecognitionRequest& request) const
{
    return impl_->recognize(image, request);
}

} // namespace iconrecognition
