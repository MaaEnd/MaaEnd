#include "WorldMapViewport.h"

#include <algorithm>
#include <cmath>
#include <utility>

#include <MaaUtils/Logger.h>

#include "MapLocator/MapAlgorithm.h"
#include "MapLocator/MatchStrategy.h"
#include "WorldMapImage.h"

namespace worldmap
{

namespace
{

// 两个分层解出的视口相距超过它就判失败，单位底图像素
constexpr double kTierAgreeBase = 10.0;

struct ScanRung
{
    double scale = 0.0;
    double score = -1.0;
};

std::vector<double> geometric_ladder(double lo, double hi, double ratio)
{
    std::vector<double> out;
    if (lo <= 0.0 || ratio <= 1.0) {
        return out;
    }
    for (double s = lo; s <= hi * (1.0 + 1e-6); s *= ratio) {
        out.push_back(s);
    }
    return out;
}

std::optional<ScanHit> scan_viewport_scales(
    const cv::Mat& haystack,
    const cv::Mat& needle,
    const std::vector<double>& scales,
    int slack,
    maplocator::PeakRefineMode refine_mode,
    std::vector<ScanRung>* rungs)
{
    if (haystack.empty() || needle.empty()) {
        return std::nullopt;
    }

    maplocator::PreparedSearchFeature prepared = maplocator::PrepareSearchFeature(haystack);
    prepared.image = maplocator::LocalContrastNormalize(prepared.image);

    std::optional<ScanHit> best;
    for (double scale : scales) {
        const cv::Size size = ScaledSize(needle, scale);
        if (size.width < kMinTemplateSide || size.height < kMinTemplateSide || size.width + slack > haystack.cols
            || size.height + slack > haystack.rows) {
            continue;
        }

        const cv::Mat scaled = maplocator::LocalContrastNormalize(ResizeTo(needle, size));
        const auto hit = maplocator::CoreMatchPrepared(prepared, scaled, cv::Mat(size, CV_8UC1, cv::Scalar(255)), refine_mode);
        if (!hit) {
            continue;
        }
        if (rungs != nullptr) {
            rungs->push_back(ScanRung { .scale = scale, .score = hit->score });
        }
        KeepBest(&best, *hit, scale, size);
    }
    return best;
}

struct ScreenRoi
{
    cv::Rect rect;
    cv::Mat full;
    cv::Mat small;
};

std::optional<ScreenRoi> crop_screen_roi(const cv::Mat& screen, const ViewportConfig& cfg)
{
    const cv::Rect rect = SafeArea(screen.size(), cfg.roi, 0);
    if (rect.width < kMinTemplateSide || rect.height < kMinTemplateSide) {
        LogError << "WorldMap: screen roi too small" << VAR(rect.width) << VAR(rect.height);
        return std::nullopt;
    }
    const int down = std::max(1, cfg.coarse_downscale);
    ScreenRoi roi { .rect = rect, .full = ToGray(screen)(rect) };
    cv::resize(roi.full, roi.small, cv::Size(roi.full.cols / down, roi.full.rows / down), 0.0, 0.0, cv::INTER_AREA);
    return roi;
}

struct ScanBase
{
    cv::Mat full;
    cv::Mat small;
    // 左上角在底图上的坐标
    cv::Point origin { 0, 0 };
    std::string tier;
};

// 粗解：降采样后扫全尺度带，只为把细解的搜索窗放到正确位置
ScanBase scan_base(const cv::Mat& gray, const cv::Point& origin, const std::string& tier, const ViewportConfig& cfg)
{
    const int down = std::max(1, cfg.coarse_downscale);
    ScanBase base { .full = gray, .origin = origin, .tier = tier };
    cv::resize(gray, base.small, cv::Size(gray.cols / down, gray.rows / down), 0.0, 0.0, cv::INTER_AREA);
    return base;
}

// 钉尺度时只留给定档与相邻两档，相邻两档同时充当算可信度的对手
std::vector<double> coarse_ladder(const ViewportConfig& cfg)
{
    return cfg.scale_hint ? geometric_ladder(*cfg.scale_hint / cfg.coarse_ratio, *cfg.scale_hint * cfg.coarse_ratio, cfg.coarse_ratio)
                          : geometric_ladder(cfg.scale_min, cfg.scale_max, cfg.coarse_ratio);
}

int coarse_slack(const ViewportConfig& cfg)
{
    return std::max(1, static_cast<int>(std::lround(cfg.scan_slack / static_cast<double>(std::max(1, cfg.coarse_downscale)))));
}

// 粗解在降采样图上定位置，细解回到原尺度定尺度，再拿粗解各档判这个解唯不唯一
std::optional<Viewport> scan_viewport(
    const ScanBase& base,
    const cv::Mat& roi,
    const cv::Mat& roi_small,
    const cv::Rect& roi_rect,
    const ViewportConfig& cfg,
    const std::string& zone)
{
    const int down = std::max(1, cfg.coarse_downscale);

    std::vector<ScanRung> coarse_rungs;
    const auto coarse = scan_viewport_scales(
        base.small,
        roi_small,
        coarse_ladder(cfg),
        coarse_slack(cfg),
        maplocator::PeakRefineMode::Parabola,
        &coarse_rungs);
    if (!coarse) {
        LogWarn << "WorldMap: coarse viewport scan found nothing" << VAR(zone) << VAR(base.tier);
        return std::nullopt;
    }

    // 细解：回到原尺度，搜索窗只覆盖粗解落点附近，两侧各留一个降采样格的不确定度。
    // 尺度只需覆盖到相邻两档粗解之间，再宽就是白扫
    const int pad = down * 3;
    const double span = coarse->scale * (cfg.coarse_ratio - 1.0) * 1.2;
    const int window_x = std::max(0, static_cast<int>(std::lround(coarse->loc.x * down)) - pad);
    const int window_y = std::max(0, static_cast<int>(std::lround(coarse->loc.y * down)) - pad);
    const cv::Rect window(
        window_x,
        window_y,
        std::min(static_cast<int>(std::lround(roi.cols * (coarse->scale + span))) + pad * 2, base.full.cols - window_x),
        std::min(static_cast<int>(std::lround(roi.rows * (coarse->scale + span))) + pad * 2, base.full.rows - window_y));
    if (window.width < kMinTemplateSide || window.height < kMinTemplateSide) {
        LogWarn << "WorldMap: fine window degenerated" << VAR(window.width) << VAR(window.height);
        return std::nullopt;
    }

    // 钉尺度时细解钉的是给定值本身，不跟着粗解落档走：缩放变了就该让分数掉下去被拒，
    // 跟过去反倒会拿一个差着一档的尺度算出错位置
    const std::vector<double> fine_ladder =
        cfg.scale_hint ? std::vector<double> { *cfg.scale_hint }
                       : LinearLadder(std::max(cfg.scale_min, coarse->scale - span), coarse->scale + span, cfg.fine_steps);

    // 细解窗口是照模板尺寸开的，本来就贴边，可落位置已由粗解锚定，不需要再留余量
    const auto fine = scan_viewport_scales(base.full(window), roi, fine_ladder, 0, maplocator::PeakRefineMode::Continuous, nullptr);
    if (!fine) {
        LogWarn << "WorldMap: fine viewport scan found nothing" << VAR(zone) << VAR(base.tier);
        return std::nullopt;
    }

    // 可信度只能拿粗解档来算：细解各档彼此相差不到一成，分数几乎一样高，
    // 互为次高分毫无意义。与尺度差 6% 以上的粗解档比，才说明这个解真的唯一
    double rival_best = 0.0;
    for (const ScanRung& rung : coarse_rungs) {
        if (std::abs(rung.scale - fine->scale) > 0.06 * fine->scale) {
            rival_best = std::max(rival_best, rung.score);
        }
    }
    const double delta = fine->score - rival_best;

    if (fine->score < cfg.min_score || delta < cfg.min_delta) {
        LogWarn << "WorldMap: viewport rejected" << VAR(zone) << VAR(base.tier) << VAR(fine->score) << VAR(delta) << VAR(fine->psr)
                << VAR(fine->scale) << VAR(cfg.min_score) << VAR(cfg.min_delta);
        return std::nullopt;
    }

    Viewport vp;
    vp.scale = fine->scale;
    vp.roi_origin = cv::Point2d(roi_rect.x, roi_rect.y);
    vp.base_origin = cv::Point2d(base.origin.x + window.x + fine->loc.x, base.origin.y + window.y + fine->loc.y);
    vp.roi_size = roi_rect.size();
    vp.score = fine->score;
    vp.delta = delta;
    vp.psr = fine->psr;
    vp.tier = base.tier;

    LogInfo << "WorldMap: viewport solved" << VAR(zone) << VAR(vp.tier) << VAR(vp.scale) << VAR(vp.base_origin.x) << VAR(vp.base_origin.y)
            << VAR(vp.score) << VAR(vp.delta) << VAR(vp.psr);
    return vp;
}

} // namespace

cv::Rect SafeArea(const cv::Size& screen_size, const ScreenMapRoi& roi, int margin)
{
    const int x0 = static_cast<int>(std::lround(screen_size.width * roi.left)) + margin;
    const int y0 = static_cast<int>(std::lround(screen_size.height * roi.top)) + margin;
    const int x1 = static_cast<int>(std::lround(screen_size.width * roi.right)) - margin;
    const int y1 = static_cast<int>(std::lround(screen_size.height * roi.bottom)) - margin;
    if (x1 <= x0 || y1 <= y0) {
        return {};
    }
    return cv::Rect(x0, y0, x1 - x0, y1 - y0) & cv::Rect(0, 0, screen_size.width, screen_size.height);
}

cv::Point2d ViewportToScreen(const Viewport& viewport, const cv::Point2d& base)
{
    return { (base.x - viewport.base_origin.x) / viewport.scale + viewport.roi_origin.x,
             (base.y - viewport.base_origin.y) / viewport.scale + viewport.roi_origin.y };
}

std::optional<Viewport> SolveViewport(const cv::Mat& base, const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg)
{
    const std::optional<ScreenRoi> screen_roi = crop_screen_roi(screen, cfg);
    if (!screen_roi) {
        return std::nullopt;
    }
    const auto& [roi_rect, roi, roi_small] = *screen_roi;
    return scan_viewport(scan_base(base, { 0, 0 }, {}, cfg), roi, roi_small, roi_rect, cfg, zone);
}

std::optional<Viewport> SolveViewportOnTiers(
    const std::vector<TierPatch>& tiers,
    const cv::Mat& screen,
    const std::string& zone,
    const ViewportConfig& cfg,
    const cv::Point2d& at)
{
    std::vector<const TierPatch*> covering;
    for (const TierPatch& tier : tiers) {
        if (cv::Rect2d(tier.area).contains(at)) {
            covering.push_back(&tier);
        }
    }
    if (covering.empty()) {
        return std::nullopt;
    }
    const std::optional<ScreenRoi> screen_roi = crop_screen_roi(screen, cfg);
    if (!screen_roi) {
        return std::nullopt;
    }
    const auto& [roi_rect, roi, roi_small] = *screen_roi;

    std::optional<Viewport> best;
    for (const TierPatch* tier : covering) {
        const ScanBase candidate = scan_base(tier->gray, tier->origin, tier->name, cfg);
        LogInfo << "WorldMap: solving the viewport on the target's tier" << VAR(zone) << VAR(tier->name) << VAR(at.x) << VAR(at.y);
        auto vp = scan_viewport(candidate, roi, roi_small, roi_rect, cfg, zone);
        if (!vp) {
            continue;
        }
        if (best && std::hypot(vp->base_origin.x - best->base_origin.x, vp->base_origin.y - best->base_origin.y) > kTierAgreeBase) {
            LogWarn << "WorldMap: tiers disagree on the viewport" << VAR(zone) << VAR(best->tier) << VAR(best->base_origin.x)
                    << VAR(best->base_origin.y) << VAR(vp->tier) << VAR(vp->base_origin.x) << VAR(vp->base_origin.y);
            return std::nullopt;
        }
        if (!best || vp->score > best->score) {
            best = std::move(vp);
        }
    }
    return best;
}

} // namespace worldmap
