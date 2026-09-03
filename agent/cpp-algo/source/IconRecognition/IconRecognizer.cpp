#include "IconRecognizer.h"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <filesystem>
#include <memory>
#include <stdexcept>
#include <string_view>
#include <tuple>
#include <unordered_map>
#include <unordered_set>

#include <MaaUtils/Logger.h>

#include "detail/CandidateSelector.h"
#include "detail/EdgeOcclusion.h"
#include "detail/ForegroundTexture.h"
#include "detail/GridDetector.h"
#include "detail/GridProfiles.h"
#include "detail/IconMatcher.h"
#include "detail/MaskPolicy.h"
#include "detail/RarityCandidates.h"
#include "detail/RarityClassifier.h"
#include "detail/RecognitionDiagnostics.h"
#include "detail/TemplateCatalog.h"

namespace iconrecognition
{
namespace
{

// 常规网格允许模板相对 cell 中心平移的像素半径；调大提高错位容忍度，但增加匹配耗时和串格风险。
constexpr int kGridSearchRadius = 2;
// 据点交易图标在 96px cell 内的实际模板边长；调大会纳入更多卡片背景，调小会裁掉图标边缘。
constexpr int kTradeTemplateSize = 88;
// 信用交易卡片内图标区域的模板边长，按 720p 卡片截图标定。
constexpr int kCreditTradeTemplateSize = 140;
// 信用交易模板相对检测 cell 左上角的横向偏移；数值增大时采样区域向右移动。
constexpr int kCreditTradeOffsetX = -6;
// 信用交易模板相对检测 cell 左上角的纵向偏移；数值增大时采样区域向下移动。
constexpr int kCreditTradeOffsetY = 4;
// 亚像素细化至少保留的候选数量；调大提高次优模板翻盘机会，但增加相位匹配次数。
constexpr int kShortlistCount = 5;
// 除固定数量外允许进入细化的分数窗口；调大提高召回但增加耗时，调小更偏向首轮排名。
constexpr double kShortlistScoreWindow = 0.08;

using PerformanceClock = std::chrono::steady_clock;

double ElapsedMilliseconds(PerformanceClock::time_point started_at)
{
    return std::chrono::duration<double, std::milli>(PerformanceClock::now() - started_at).count();
}

const std::vector<std::string>& DefaultItemFiltersImpl(GridType type)
{
    // 背包、存取站、送货和 single ROI 默认覆盖普通仓库的全部分类。
    static const std::vector<std::string> normal { "Normal:*" };
    // 据点交易只展示产品和可使用物品，缩小候选集可降低单格识别耗时。
    static const std::vector<std::string> trade { "Normal:Product", "Normal:Usable" };
    // 贵重品库默认覆盖 ValuableDepot 下的全部分类。
    static const std::vector<std::string> valuables { "ValuableDepot:*" };
    // 信用交易所只展示特殊商品和独立存储的资源。
    static const std::vector<std::string> credit { "ValuableDepot:SpecialItem", "Isolate:*" };
    // 多个游戏功能共用奖励界面；默认同时覆盖独立资源、培养素材和珍贵消耗品。
    static const std::vector<std::string> rewards { "Isolate:*", "ValuableDepot:*" };
    switch (type) {
    case GridType::Trade:
        return trade;
    case GridType::Valuables:
        return valuables;
    case GridType::CreditTrade:
        return credit;
    case GridType::Rewards:
        return rewards;
    case GridType::Transfer:
    case GridType::PortStorager:
    case GridType::Shipment:
    case GridType::SingleRoi:
        return normal;
    }
    return normal;
}

void ValidateThresholds(double accept, double subpixel)
{
    if (!(0.0 <= subpixel && subpixel < accept && accept <= 1.0)) {
        throw std::invalid_argument("thresholds must satisfy 0 <= subpixel_threshold < threshold <= 1");
    }
}

int TemplateSizeFor(GridType type, double grid_scale)
{
    int baseline_size = detail::ProfileFor(type).cell_size;
    if (type == GridType::Trade) {
        baseline_size = kTradeTemplateSize;
    }
    else if (type == GridType::CreditTrade) {
        baseline_size = kCreditTradeTemplateSize;
    }
    return std::max(1, cvRound(baseline_size * grid_scale));
}

cv::Rect SlotFor(GridType type, const detail::GridCell& cell, double grid_scale)
{
    const int template_size = TemplateSizeFor(type, grid_scale);
    if (type == GridType::Trade) {
        const int inset = (cell.cell_box.width - template_size) / 2;
        return cv::Rect(cell.cell_box.x + inset, cell.cell_box.y + inset, template_size, template_size);
    }
    if (type == GridType::CreditTrade) {
        return cv::Rect(
            cell.cell_box.x + cvRound(kCreditTradeOffsetX * grid_scale),
            cell.cell_box.y + cvRound(kCreditTradeOffsetY * grid_scale),
            template_size,
            template_size);
    }
    return cv::Rect(cell.cell_box.x, cell.cell_box.y, template_size, template_size);
}

struct ActiveTemplateSelection
{
    std::vector<detail::PreparedTemplate> templates;
    detail::MaskKind mask_kind = detail::MaskKind::LowerExtended;
};

ActiveTemplateSelection
    ActiveTemplates(const cv::Mat& image, GridType type, const cv::Rect& slot, const std::vector<detail::PreparedTemplate>& templates)
{
    if (type != GridType::Shipment && type != GridType::Valuables) {
        return { .templates = templates };
    }
    const cv::Rect bounds(0, 0, image.cols, image.rows);
    if ((slot & bounds) != slot) {
        return { .templates = templates };
    }
    const cv::Mat slot_image = image(slot);
    std::vector<detail::PreparedTemplate> active = templates;
    if (type == GridType::Shipment) {
        if (!detail::HasShipmentTopBar(slot_image)) {
            return { .templates = templates };
        }
        for (auto& templ : active) {
            templ.mask = templ.mask.clone();
            detail::ApplyShipmentTopBarMask(templ.mask);
        }
        return { .templates = std::move(active), .mask_kind = detail::MaskKind::ShipmentTopBar };
    }
    if (!detail::HasValuablesWeaponPortrait(slot_image)) {
        return { .templates = templates };
    }
    for (auto& templ : active) {
        templ.mask = templ.mask.clone();
        detail::ApplyValuablesWeaponPortraitMask(templ.mask);
    }
    return { .templates = std::move(active), .mask_kind = detail::MaskKind::ValuablesWeapon };
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
    bool rarity_prefiltered = false;
    bool rarity_fallback_used = false;
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
    int search_radius,
    detail::RecognitionPerformanceDiagnostics* performance)
{
    for (const detail::Phase phase : detail::PhaseGrid()) {
        if (phase.x == 0.0 && phase.y == 0.0) {
            continue;
        }
        const auto diagnostics = detail::ScoreTemplateAt(
            image,
            slot,
            templates[candidate.template_index],
            search_radius,
            phase,
            performance ? &performance->matcher : nullptr);
        if (PhaseScoreBetter(diagnostics, phase, candidate)) {
            candidate.diagnostics = diagnostics, candidate.phase = phase;
        }
    }
    for (const detail::Phase phase : detail::BoundaryExtensionPhases(candidate.phase)) {
        const auto diagnostics = detail::ScoreTemplateAt(
            image,
            slot,
            templates[candidate.template_index],
            search_radius,
            phase,
            performance ? &performance->matcher : nullptr);
        if (PhaseScoreBetter(diagnostics, phase, candidate)) {
            candidate.diagnostics = diagnostics, candidate.phase = phase;
        }
    }
    return candidate;
}

void ScoreBaselineCandidates(
    const cv::Mat& image,
    const cv::Rect& slot,
    const std::vector<detail::PreparedTemplate>& templates,
    const std::vector<std::size_t>& indices,
    int search_radius,
    std::vector<RankedCandidate>& ranked,
    detail::RecognitionPerformanceDiagnostics* performance)
{
    const auto baseline_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    for (const std::size_t index : indices) {
        auto diagnostics =
            detail::ScoreTemplateAt(image, slot, templates[index], search_radius, {}, performance ? &performance->matcher : nullptr);
        ranked.push_back({ index, diagnostics, diagnostics, {} });
    }
    if (performance) {
        performance->ranking.baseline_candidates += indices.size();
        performance->ranking.baseline_scoring_ms += ElapsedMilliseconds(baseline_started);
    }
}

SlotRanking EvaluateRankedCandidates(
    const cv::Mat& image,
    const cv::Rect& slot,
    const std::vector<detail::PreparedTemplate>& templates,
    const std::vector<RankedCandidate>& baseline_candidates,
    double accept,
    double subpixel,
    int search_radius,
    std::vector<std::optional<RankedCandidate>>& refinement_cache,
    detail::RecognitionPerformanceDiagnostics* performance)
{
    std::vector<RankedCandidate> ranked = baseline_candidates;
    const auto baseline_sort_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    std::ranges::sort(ranked, [&](const auto& left, const auto& right) { return CandidateLess(left, right, templates); });
    if (performance) {
        performance->ranking.baseline_sort_ms += ElapsedMilliseconds(baseline_sort_started);
    }
    const double baseline_score = ranked.front().diagnostics.score;
    if (!(subpixel <= baseline_score && baseline_score < accept)) {
        return { ranked.front(), std::move(ranked), baseline_score, false };
    }

    const auto refinement_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    std::vector<RankedCandidate> refined;
    for (std::size_t index = 0; index < ranked.size(); ++index) {
        if (index < kShortlistCount || ranked[index].diagnostics.score >= baseline_score - kShortlistScoreWindow) {
            const std::size_t template_index = ranked[index].template_index;
            if (!refinement_cache[template_index]) {
                refinement_cache[template_index] = RefineCandidate(image, slot, templates, ranked[index], search_radius, performance);
                if (performance) {
                    ++performance->ranking.refined_candidates;
                }
            }
            refined.push_back(*refinement_cache[template_index]);
        }
    }
    if (performance) {
        performance->ranking.refinement_scoring_ms += ElapsedMilliseconds(refinement_started);
    }

    const auto refinement_sort_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    std::ranges::sort(refined, [&](const auto& left, const auto& right) { return CandidateLess(left, right, templates); });
    if (performance) {
        performance->ranking.refinement_sort_ms += ElapsedMilliseconds(refinement_sort_started);
    }
    return { refined.front(), std::move(refined), baseline_score, true };
}

SlotRanking RankSlot(
    const cv::Mat& image,
    const cv::Rect& slot,
    const std::vector<detail::PreparedTemplate>& templates,
    std::optional<int> detected_rarity,
    double accept,
    double subpixel,
    int search_radius,
    detail::RecognitionPerformanceDiagnostics* performance)
{
    const auto ranking_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    const auto passes = detail::BuildRarityCandidatePasses(templates, detected_rarity);
    std::vector<RankedCandidate> baseline_candidates;
    baseline_candidates.reserve(templates.size());
    std::vector<std::optional<RankedCandidate>> refinement_cache(templates.size());

    ScoreBaselineCandidates(image, slot, templates, passes.preferred_indices, search_radius, baseline_candidates, performance);
    if (performance && passes.prefiltered) {
        ++performance->ranking.rarity_prefiltered_cells;
        performance->ranking.rarity_preferred_candidates += passes.preferred_indices.size();
    }

    SlotRanking ranking = EvaluateRankedCandidates(
        image,
        slot,
        templates,
        baseline_candidates,
        accept,
        subpixel,
        search_radius,
        refinement_cache,
        performance);
    ranking.rarity_prefiltered = passes.prefiltered;
    if (ranking.best.diagnostics.score < accept && !passes.remaining_indices.empty()) {
        ScoreBaselineCandidates(image, slot, templates, passes.remaining_indices, search_radius, baseline_candidates, performance);
        if (performance) {
            ++performance->ranking.rarity_fallback_cells;
            performance->ranking.rarity_remaining_candidates += passes.remaining_indices.size();
        }
        ranking = EvaluateRankedCandidates(
            image,
            slot,
            templates,
            baseline_candidates,
            accept,
            subpixel,
            search_radius,
            refinement_cache,
            performance);
        ranking.rarity_prefiltered = true;
        ranking.rarity_fallback_used = true;
    }
    if (performance) {
        performance->ranking.total_ms += ElapsedMilliseconds(ranking_started);
    }
    return ranking;
}

bool ValidateCandidateCell(
    const cv::Mat& image,
    const cv::Rect& cell_box,
    const detail::TemplateRecord& expected,
    const std::vector<detail::PreparedTemplate>& templates,
    double threshold,
    double subpixel_threshold)
{
    const SlotRanking ranking =
        RankSlot(image, cell_box, templates, std::nullopt, threshold, subpixel_threshold, kGridSearchRadius, nullptr);
    const auto& actual = templates[ranking.best.template_index].record;
    return ranking.best.diagnostics.score >= threshold && actual.icon_id == expected.icon_id
           && actual.fluid_icon_id == expected.fluid_icon_id;
}

struct CellEvaluation
{
    std::vector<detail::PreparedTemplate> active;
    std::vector<detail::PreparedTemplate> edge_active;
    SlotRanking ranking;
    std::optional<double> foreground_texture;
    std::optional<detail::EdgeOcclusion> edge_occlusion;
    std::optional<double> top2_margin;
    std::optional<std::string> rejected_reason;
    std::string mask_kind;
    bool edge_recovery_used = false;
    bool accepted = false;

    const std::vector<detail::PreparedTemplate>& effectiveTemplates() const { return edge_recovery_used ? edge_active : active; }

    const detail::PreparedTemplate& bestTemplate() const { return effectiveTemplates().at(ranking.best.template_index); }
};

CellEvaluation EvaluateCellTemplates(
    const cv::Mat& image,
    GridType grid_type,
    const cv::Rect& cell_box,
    const cv::Rect& slot,
    const std::vector<detail::PreparedTemplate>& selected,
    std::optional<int> rarity,
    bool single_roi,
    double grid_scale,
    double threshold,
    double subpixel_threshold,
    detail::RecognitionPerformanceDiagnostics* performance)
{
    CellEvaluation result;
    const auto active_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    detail::MaskKind active_mask_kind = detail::MaskKind::LowerExtended;
    if (single_roi) {
        result.active = selected;
    }
    else {
        auto active_selection = ActiveTemplates(image, grid_type, slot, selected);
        result.active = std::move(active_selection.templates);
        active_mask_kind = active_selection.mask_kind;
    }
    if (performance) {
        performance->active_templates_ms += ElapsedMilliseconds(active_started);
    }
    result.ranking = RankSlot(
        image,
        slot,
        result.active,
        rarity,
        threshold,
        subpixel_threshold,
        std::max(1, cvRound(kGridSearchRadius * grid_scale)),
        performance);

    const auto texture_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
    result.foreground_texture = single_roi ? std::optional<double> {} : detail::ForegroundTextureScore(image, cell_box, grid_type);
    const bool low_texture = !single_roi && detail::IsLowTexture(image, cell_box, grid_type);
    if (performance) {
        performance->foreground_texture_ms += ElapsedMilliseconds(texture_started);
    }

    if (detail::ShouldAttemptEdgeOcclusionRecovery(
            grid_type,
            result.ranking.best.diagnostics.score,
            threshold,
            subpixel_threshold,
            low_texture)) {
        const auto& original_template = result.active[result.ranking.best.template_index];
        result.edge_occlusion = detail::DetectEdgeOcclusion(
            image,
            cv::Rect(result.ranking.best.diagnostics.position, original_template.image.size()),
            original_template,
            result.ranking.best.phase);
        if (result.edge_occlusion) {
            result.edge_active = result.active;
            for (auto& templ : result.edge_active) {
                templ.mask = templ.mask.clone();
                detail::ApplyEdgeOcclusionMask(templ.mask, *result.edge_occlusion);
            }
            SlotRanking recovered = RankSlot(
                image,
                slot,
                result.edge_active,
                rarity,
                threshold,
                subpixel_threshold,
                std::max(1, cvRound(kGridSearchRadius * grid_scale)),
                performance);
            const std::optional<double> recovered_margin =
                recovered.ranked.size() > 1
                    ? std::optional<double>(recovered.best.diagnostics.score - recovered.ranked[1].diagnostics.score)
                    : std::nullopt;
            if (detail::ShouldAcceptEdgeOcclusionRecovery(
                    result.ranking.best.template_index,
                    recovered.best.template_index,
                    recovered.best.diagnostics.score,
                    recovered_margin,
                    threshold)) {
                result.ranking = std::move(recovered);
                result.edge_recovery_used = true;
            }
        }
    }

    const auto& best = result.ranking.best;
    const auto& templ = result.bestTemplate();
    result.top2_margin = result.ranking.ranked.size() > 1
                             ? std::optional<double>(best.diagnostics.score - result.ranking.ranked[1].diagnostics.score)
                             : std::nullopt;
    const bool texture_rejected = best.diagnostics.score >= threshold && low_texture;
    result.accepted = best.diagnostics.score >= threshold && !texture_rejected;
    if (!result.accepted) {
        result.rejected_reason =
            texture_rejected ? "low-foreground-texture"
                             : (best.diagnostics.score < subpixel_threshold ? "below-subpixel-threshold" : "below-accept-threshold");
    }
    result.mask_kind = detail::DescribeMaskKind(active_mask_kind, templ.composite);
    if (!single_roi && result.edge_recovery_used) {
        result.mask_kind += result.edge_occlusion->side == detail::EdgeOcclusionSide::Top ? "+edge_top" : "+edge_bottom";
    }
    if (templ.region_unavailable) {
        result.mask_kind += "+region_unavailable_overlay";
    }
    return result;
}

std::vector<detail::PreparedTemplate> SelectRegionUnavailableVariants(
    const std::vector<detail::PreparedTemplate>& region_unavailable,
    const std::vector<detail::PreparedTemplate>& selected)
{
    std::unordered_map<std::string, const detail::PreparedTemplate*> selected_restricted;
    for (const auto& templ : selected) {
        if (templ.record.region_restricted) {
            selected_restricted.emplace(templ.record.item_id, &templ);
        }
    }
    std::vector<detail::PreparedTemplate> result;
    for (const auto& templ : region_unavailable) {
        const auto selected_templ = selected_restricted.find(templ.record.item_id);
        if (selected_templ != selected_restricted.end()) {
            auto variant = templ;
            // 后备模板只替换图像状态，候选筛选阶段聚合的别名仍属于同一代表物品。
            variant.record.aliases = selected_templ->second->record.aliases;
            result.push_back(std::move(variant));
        }
    }
    return result;
}

std::vector<detail::PreparedTemplate> BuildRegionUnavailableRecheckTemplates(
    const std::vector<detail::PreparedTemplate>& selected,
    const std::vector<detail::PreparedTemplate>& region_unavailable)
{
    std::unordered_map<std::string, detail::PreparedTemplate> unavailable_by_id;
    for (const auto& templ : region_unavailable) {
        unavailable_by_id.emplace(templ.record.item_id, templ);
    }

    std::vector<detail::PreparedTemplate> result;
    result.reserve(selected.size());
    for (const auto& templ : selected) {
        if (!templ.record.region_restricted) {
            result.push_back(templ);
            continue;
        }
        const auto unavailable = unavailable_by_id.find(templ.record.item_id);
        if (unavailable == unavailable_by_id.end()) {
            throw std::runtime_error("region-unavailable template missing for item: " + templ.record.item_id);
        }
        result.push_back(unavailable->second);
    }
    return result;
}

ItemInfo ItemFromTemplate(const detail::PreparedTemplate& templ)
{
    ItemInfo result {
        .item_id = templ.record.item_id,
        .name = templ.record.name_key,
        .category = templ.record.category,
        .storage_kind = templ.record.storage_kind,
        .category_type = templ.record.category_type,
        .rarity = templ.record.rarity,
    };
    result.aliases.reserve(templ.record.aliases.size());
    std::ranges::transform(templ.record.aliases, std::back_inserter(result.aliases), [](const auto& alias) {
        return ItemInfo::Alias { .item_id = alias.item_id, .name = alias.name_key };
    });
    return result;
}

bool ContainsRequestedItem(const ItemInfo& item, const std::unordered_set<std::string>& requested_ids)
{
    return requested_ids.contains(item.item_id)
           || std::ranges::any_of(item.aliases, [&](const auto& alias) { return requested_ids.contains(alias.item_id); });
}

void ValidateRecognitionRoi(const cv::Mat& image, const cv::Rect& roi)
{
    const cv::Rect bounds(0, 0, image.cols, image.rows);
    if (roi.width <= 0 || roi.height <= 0 || (roi & bounds) != roi) {
        throw std::invalid_argument("IconRecognition ROI must be positive and fully inside the input image");
    }
}

} // namespace

const std::vector<std::string>& detail::DefaultItemFilters(GridType type)
{
    return DefaultItemFiltersImpl(type);
}

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

    const std::vector<detail::PreparedTemplate>& TemplatesFor(GridType type, double grid_scale) const
    {
        return catalog_.load(TemplateSizeFor(type, grid_scale));
    }

    const std::vector<detail::PreparedTemplate>& RoiTemplates(int target_size) const { return catalog_.load(target_size); }

    bool preload(const std::vector<RecognitionRequest>& requests)
    {
        try {
            for (const auto& request : requests) {
                if (request.grid_type == GridType::SingleRoi) {
                    if (request.roi.width <= 0 || request.roi.width != request.roi.height) {
                        throw std::invalid_argument("single_roi preload must use a positive square ROI");
                    }
                    static_cast<void>(RoiTemplates(request.roi.width));
                }
                else {
                    for (const double grid_scale : detail::kSupportedControllerGridScales) {
                        const int target_size = TemplateSizeFor(request.grid_type, grid_scale);
                        static_cast<void>(catalog_.load(target_size));
                        if (request.recognize_region_unavailable && SupportsRegionUnavailableRecognition(request.grid_type)) {
                            static_cast<void>(catalog_.loadRegionUnavailable(target_size));
                        }
                    }
                }
            }
            return true;
        }
        catch (const std::exception& error) {
            LogError << "IconRecognizer template preload failed" << VAR(error.what());
            return false;
        }
    }

    RecognitionResult recognize(const cv::Mat& image, const RecognitionRequest& request) const
    {
        try {
            if (image.empty()) {
                return Error(request.roi, request.grid_type, "invalid_image", "Input image is empty");
            }
            ValidateRecognitionRoi(image, request.roi);
            return recognize_original(image, request);
        }
        catch (const std::invalid_argument& error) {
            LogError << "IconRecognizer recognition rejected invalid input" << VAR(error.what());
            return Error(request.roi, request.grid_type, "invalid_argument", error.what());
        }
        catch (const std::exception& error) {
            LogError << "IconRecognizer recognition failed" << VAR(error.what());
            return Error(request.roi, request.grid_type, "exception", error.what());
        }
    }

    RecognitionResult recognize_original(const cv::Mat& image, const RecognitionRequest& request) const
    {
        try {
            const auto recognition_started = request.debug ? PerformanceClock::now() : PerformanceClock::time_point {};
            std::optional<detail::RecognitionPerformanceDiagnostics> performance;
            if (request.debug) {
                performance.emplace();
            }
            auto* performance_ptr = performance ? &*performance : nullptr;
            if (image.empty()) {
                return Error(request.roi, request.grid_type, "invalid_image", "Input image is empty");
            }
            ValidateThresholds(request.threshold, request.subpixel_threshold);
            const bool recheck_enabled = !request.candidates.item_ids.empty() && !request.candidates.item_recheck_filters.empty();
            if (recheck_enabled) {
                detail::ValidateCandidateFilterList(request.candidates.item_recheck_filters, "item_recheck_filters");
            }
            const bool single_roi = request.grid_type == GridType::SingleRoi;
            std::vector<detail::GridCell> cells;
            std::vector<detail::GridLayout> detected_grids;
            std::vector<detail::PreparedTemplate> selected;
            double grid_scale = detail::kWin32ControllerGridScale;
            int template_size = 0;
            if (single_roi) {
                if (request.roi.width <= 0 || request.roi.width != request.roi.height) {
                    throw std::invalid_argument("single_roi must be a positive square");
                }
                const cv::Rect bounds(0, 0, image.cols, image.rows);
                if ((request.roi & bounds) != request.roi) {
                    throw std::invalid_argument("single_roi must be fully inside the image");
                }
                cells.push_back(detail::GridCell { .cell_box = request.roi });
                template_size = request.roi.width;
                const auto selection_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
                selected = detail::SelectCandidateTemplates(
                    RoiTemplates(request.roi.width),
                    request.candidates,
                    detail::DefaultItemFilters(request.grid_type));
                if (performance) {
                    performance->template_selection_ms += ElapsedMilliseconds(selection_started);
                }
            }
            else {
                const auto detection_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
                const detail::GridDetection detection = detail::DetectGrid(image, request.grid_type, request.roi, request.grid_scale_hint);
                if (performance) {
                    performance->grid_detection_ms += ElapsedMilliseconds(detection_started);
                }
                if (detection.cells.empty()) {
                    const std::string message =
                        detection.failure_message.empty() ? "Grid detection found no formal cells" : detection.failure_message;
                    return Error(request.roi, request.grid_type, "grid_detection_failed", message);
                }
                cells = detection.cells;
                detected_grids = detection.grids;
                grid_scale = detection.grid_scale;
                template_size = TemplateSizeFor(request.grid_type, grid_scale);
                const auto selection_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
                selected = detail::SelectCandidateTemplates(
                    TemplatesFor(request.grid_type, grid_scale),
                    request.candidates,
                    detail::DefaultItemFilters(request.grid_type));
                if (performance) {
                    performance->template_selection_ms += ElapsedMilliseconds(selection_started);
                }
            }
            RecognitionResult result;
            result.grid_type = request.grid_type;
            result.roi = request.roi;
            result.diagnostics = std::make_shared<detail::RecognitionDiagnostics>();
            for (const auto& grid : detected_grids) {
                if (grid.selection_diagnostics) {
                    result.diagnostics->grids.push_back(*grid.selection_diagnostics);
                }
            }
            const bool has_region_restricted_candidates =
                std::ranges::any_of(selected, [](const auto& templ) { return templ.record.region_restricted; });
            const bool region_unavailable_enabled =
                request.recognize_region_unavailable && SupportsRegionUnavailableRecognition(request.grid_type);
            std::optional<std::vector<detail::PreparedTemplate>> region_unavailable_selected;
            for (const auto& cell : cells) {
                const cv::Rect slot = single_roi ? cell.cell_box : SlotFor(request.grid_type, cell, grid_scale);
                const auto rarity_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
                const auto rarity = single_roi ? detail::RarityResult {} : detail::ClassifyRarity(image, slot, grid_scale);
                if (performance) {
                    performance->rarity_classification_ms += ElapsedMilliseconds(rarity_started);
                }
                CellEvaluation evaluation = EvaluateCellTemplates(
                    image,
                    request.grid_type,
                    cell.cell_box,
                    slot,
                    selected,
                    rarity.rarity,
                    single_roi,
                    grid_scale,
                    request.threshold,
                    request.subpixel_threshold,
                    performance_ptr);
                bool region_unavailable_fallback_used = false;
                if (!evaluation.accepted && region_unavailable_enabled && has_region_restricted_candidates) {
                    if (!region_unavailable_selected) {
                        const auto selection_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
                        region_unavailable_selected =
                            SelectRegionUnavailableVariants(catalog_.loadRegionUnavailable(template_size), selected);
                        if (performance) {
                            performance->template_selection_ms += ElapsedMilliseconds(selection_started);
                        }
                    }
                    if (!region_unavailable_selected->empty()) {
                        CellEvaluation fallback = EvaluateCellTemplates(
                            image,
                            request.grid_type,
                            cell.cell_box,
                            slot,
                            *region_unavailable_selected,
                            rarity.rarity,
                            single_roi,
                            grid_scale,
                            request.threshold,
                            request.subpixel_threshold,
                            performance_ptr);
                        if (fallback.accepted) {
                            evaluation = std::move(fallback);
                            region_unavailable_fallback_used = true;
                        }
                    }
                }

                const auto assembly_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
                const auto& best = evaluation.ranking.best;
                const auto& templ = evaluation.bestTemplate();
                result.diagnostics->cells.push_back(detail::CellRecognitionDiagnostics {
                    .cell_box = cell.cell_box,
                    .candidate_box = cv::Rect(best.diagnostics.position, templ.image.size()),
                    .best_candidate_id = templ.record.item_id,
                    .baseline_score = evaluation.ranking.baseline_score,
                    .score = best.diagnostics.score,
                    .top2_margin = evaluation.top2_margin,
                    .candidate_count = evaluation.ranking.ranked.size(),
                    .fallback_used = evaluation.ranking.fallback_used || evaluation.edge_recovery_used,
                    .region_unavailable_fallback_used = region_unavailable_fallback_used,
                    .best_phase = cv::Point2d(best.phase.x, best.phase.y),
                    .rejected_reason = evaluation.rejected_reason,
                    .foreground_texture = evaluation.foreground_texture,
                    .rarity = single_roi && evaluation.accepted ? std::optional<int>(templ.record.rarity) : rarity.rarity,
                    .rarity_coverage = single_roi ? 0.0 : rarity.coverage,
                    .rarity_row_offset = single_roi ? std::optional<int> {} : rarity.row_offset,
                    .mask_kind = evaluation.mask_kind,
                    .edge_occlusion_side = evaluation.edge_recovery_used ? std::optional<std::string>(
                                               evaluation.edge_occlusion->side == detail::EdgeOcclusionSide::Top ? "top" : "bottom")
                                                                         : std::nullopt,
                    .edge_occlusion_cutoff =
                        evaluation.edge_recovery_used ? std::optional<int>(evaluation.edge_occlusion->cutoff) : std::nullopt,
                    .edge_occlusion_residual_ratio =
                        evaluation.edge_recovery_used ? std::optional<double>(evaluation.edge_occlusion->residual_ratio) : std::nullopt,
                    .row = single_roi ? std::optional<int> {} : std::optional<int>(cell.row),
                    .column = single_roi ? std::optional<int> {} : std::optional<int>(cell.column),
                });
                if (!evaluation.accepted) {
                    if (performance) {
                        performance->result_assembly_ms += ElapsedMilliseconds(assembly_started);
                    }
                    continue;
                }
                result.matches.push_back(ItemMatch {
                    .item = ItemFromTemplate(templ),
                    .cell_box = cell.cell_box,
                    .item_box = cv::Rect(best.diagnostics.position, templ.image.size()),
                    .score = best.diagnostics.score,
                    .region_unavailable = templ.region_unavailable,
                    .row = single_roi ? std::optional<int> {} : std::optional<int>(cell.row),
                    .column = single_roi ? std::optional<int> {} : std::optional<int>(cell.column),
                });
                if (performance) {
                    performance->result_assembly_ms += ElapsedMilliseconds(assembly_started);
                }
            }
            const auto sort_started = performance ? PerformanceClock::now() : PerformanceClock::time_point {};
            std::ranges::stable_sort(result.matches, ItemMatchLess {});
            if (recheck_enabled) {
                const auto candidates = std::move(result.matches);
                result.matches.clear();
                CandidateFilter recheck_candidates;
                recheck_candidates.item_filters = request.candidates.item_recheck_filters;
                std::unordered_map<int, std::vector<detail::PreparedTemplate>> recheck_templates_by_size;
                std::unordered_map<int, std::vector<detail::PreparedTemplate>> region_unavailable_recheck_templates_by_size;
                const std::unordered_set<std::string> original_item_ids(
                    request.candidates.item_ids.begin(),
                    request.candidates.item_ids.end());
                std::unordered_set<std::string> rechecked_item_ids;
                for (const auto& candidate : candidates) {
                    if (request.deduplicate && rechecked_item_ids.contains(candidate.item.item_id)) {
                        continue;
                    }
                    // 附加类型不属于显式 item_ids，不能被只为原始 ID 配置的反查过滤器误删。
                    bool valid = true;
                    if (ContainsRequestedItem(candidate.item, original_item_ids)) {
                        auto& template_cache =
                            candidate.region_unavailable ? region_unavailable_recheck_templates_by_size : recheck_templates_by_size;
                        auto [templates, inserted] = template_cache.try_emplace(candidate.cell_box.width);
                        if (inserted) {
                            templates->second = detail::SelectCandidateTemplates(
                                RoiTemplates(candidate.cell_box.width),
                                recheck_candidates,
                                detail::DefaultItemFilters(GridType::SingleRoi),
                                false);
                            if (candidate.region_unavailable) {
                                // 当前地区不可用命中必须使用同一界面状态复核，避免普通模板替代受限物品后返回错误状态。
                                templates->second = BuildRegionUnavailableRecheckTemplates(
                                    templates->second,
                                    catalog_.loadRegionUnavailable(candidate.cell_box.width));
                            }
                        }
                        const auto expected = std::ranges::find_if(
                            selected,
                            [&](const auto& templ) { return templ.record.item_id == candidate.item.item_id; });
                        if (expected == selected.end()) {
                            throw std::runtime_error("selected template missing for recheck item: " + candidate.item.item_id);
                        }
                        valid = ValidateCandidateCell(
                            image,
                            candidate.cell_box,
                            expected->record,
                            templates->second,
                            request.threshold,
                            request.subpixel_threshold);
                    }
                    if (valid) {
                        result.matches.push_back(candidate);
                        if (request.deduplicate) {
                            rechecked_item_ids.insert(candidate.item.item_id);
                        }
                    }
                }
            }
            if (request.deduplicate && !recheck_enabled) {
                DeduplicateMatches(result.matches);
            }
            if (performance) {
                performance->result_sort_ms += ElapsedMilliseconds(sort_started);
            }
            result.matched = !result.matches.empty();
            if (!result.matched) {
                result.error_code = "no_match", result.message = "No item reached the configured threshold";
            }
            if (performance) {
                performance->cell_count = cells.size();
                performance->total_ms = ElapsedMilliseconds(recognition_started);
                result.diagnostics->performance = std::move(performance);
                LogInfo << "IconRecognition debug performance" << VAR(*result.diagnostics->performance);
            }
            return result;
        }
        catch (const std::invalid_argument& error) {
            LogError << "IconRecognizer recognition rejected invalid input" << VAR(error.what());
            return Error(request.roi, request.grid_type, "invalid_argument", error.what());
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

bool IconRecognizer::preload(const std::vector<RecognitionRequest>& requests)
{
    return impl_->preload(requests);
}

RecognitionResult IconRecognizer::recognize(const cv::Mat& image, const RecognitionRequest& request) const
{
    return impl_->recognize(image, request);
}

} // namespace iconrecognition
