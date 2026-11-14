#include "WorldMapIcon.h"

#include <algorithm>
#include <cmath>
#include <string>
#include <vector>

#include <MaaUtils/Logger.h>

#include "MapLocator/MatchStrategy.h"
#include "WorldMapImage.h"

namespace worldmap
{

namespace
{

constexpr int kMinAnchorSide = 6;

std::optional<ScanHit>
    scan_icon_scales(const cv::Mat& haystack, const cv::Mat& needle, const cv::Mat& needle_mask, const std::vector<double>& scales)
{
    if (haystack.empty() || needle.empty()) {
        return std::nullopt;
    }

    const maplocator::PreparedSearchFeature prepared = maplocator::PrepareSearchFeature(haystack);

    std::optional<ScanHit> best;
    for (double scale : scales) {
        const cv::Size size = ScaledSize(needle, scale);
        if (size.width < kMinAnchorSide || size.height < kMinAnchorSide || size.width > haystack.cols || size.height > haystack.rows) {
            continue;
        }

        cv::Mat mask(size, CV_8UC1, cv::Scalar(255));
        if (!needle_mask.empty()) {
            // 掩膜是二值的，只能用最近邻插值
            cv::resize(needle_mask, mask, size, 0.0, 0.0, cv::INTER_NEAREST);
        }
        const auto hit = maplocator::CoreMatchPrepared(prepared, ResizeTo(needle, size), mask, maplocator::PeakRefineMode::Continuous);
        if (hit) {
            KeepBest(&best, *hit, scale, size);
        }
    }
    return best;
}

// 模板哪些像素是金的，就到实拍的同一批像素上看还金不金。没解锁的整块褪成灰，比例会塌下去
double gold_ratio(const cv::Mat& screen, const cv::Mat& templ_sat, const cv::Mat& templ_mask, const cv::Rect& live, int sat_floor)
{
    if (templ_sat.empty() || (live & cv::Rect(0, 0, screen.cols, screen.rows)) != live) {
        return 0.0;
    }

    cv::Mat sat;
    cv::resize(templ_sat, sat, live.size(), 0.0, 0.0, cv::INTER_NEAREST);
    cv::Mat gold = (sat >= sat_floor);
    if (!templ_mask.empty()) {
        cv::Mat mask;
        cv::resize(templ_mask, mask, live.size(), 0.0, 0.0, cv::INTER_NEAREST);
        cv::bitwise_and(gold, mask, gold);
    }

    const int total = cv::countNonZero(gold);
    if (total == 0) {
        return 0.0;
    }

    cv::Mat lit = (SaturationOf(screen(live)) >= sat_floor);
    cv::bitwise_and(lit, gold, lit);
    return static_cast<double>(cv::countNonZero(lit)) / total;
}

struct IconSearch
{
    cv::Mat gray;
    cv::Point2d expected { 0.0, 0.0 };
    double viewport_scale = 0.0;
    std::vector<double> ladder;
    SpotConfig spot;
};

struct TemplateMatch
{
    std::string name;
    const IconTemplate* templ = nullptr;
    ScanHit hit;
};

struct IconCandidate
{
    SpotHit hit;
    const IconTemplate* templ = nullptr;
    cv::Rect live;
};

cv::Rect window_around(const cv::Point2d& center, int radius, const cv::Size& screen_size)
{
    const cv::Rect window(
        static_cast<int>(std::lround(center.x)) - radius,
        static_cast<int>(std::lround(center.y)) - radius,
        radius * 2,
        radius * 2);
    return window & cv::Rect(0, 0, screen_size.width, screen_size.height);
}

std::optional<TemplateMatch> best_template(
    WorldMapAssets* assets,
    const cv::Mat& patch,
    const std::vector<std::string>& templates,
    const std::vector<double>& ladder)
{
    std::optional<TemplateMatch> best;
    for (const std::string& name : templates) {
        const IconTemplate* templ = LoadIconTemplate(assets, name);
        if (templ == nullptr) {
            continue;
        }
        if (patch.cols < templ->gray.cols || patch.rows < templ->gray.rows) {
            LogWarn << "WorldMap: confirm window smaller than template" << VAR(name) << VAR(patch.cols) << VAR(patch.rows);
            continue;
        }
        const auto hit = scan_icon_scales(patch, templ->gray, templ->mask, ladder);
        if (hit && (!best || hit->score > best->hit.score)) {
            best = TemplateMatch { .name = name, .templ = templ, .hit = *hit };
        }
    }
    return best;
}

IconCandidate candidate_in_window(const TemplateMatch& match, const cv::Rect& window, const IconSearch& search)
{
    const ScanHit& best = match.hit;
    IconCandidate found;
    found.templ = match.templ;
    found.live = cv::Rect(
        window.x + static_cast<int>(std::lround(best.loc.x)),
        window.y + static_cast<int>(std::lround(best.loc.y)),
        best.size.width,
        best.size.height);
    found.hit.template_name = match.name;
    found.hit.center = cv::Point2d(window.x + best.loc.x + best.size.width / 2.0, window.y + best.loc.y + best.size.height / 2.0);
    found.hit.hotspot = found.hit.center + match.templ->hotspot * best.scale;
    found.hit.size = best.size;
    found.hit.score = best.score;
    found.hit.match_scale = best.scale;
    found.hit.offset_base =
        std::hypot(found.hit.center.x - search.expected.x, found.hit.center.y - search.expected.y) * search.viewport_scale;
    return found;
}

// 在期望位置开个半径 radius 的窗口扫一遍，只出结果不做判定
std::optional<IconCandidate> scan_icon_window(WorldMapAssets* assets, const IconSearch& search, int radius)
{
    const cv::Rect window = window_around(search.expected, radius, search.gray.size());
    if (window.empty()) {
        LogWarn << "WorldMap: confirm window off screen" << VAR(search.expected.x) << VAR(search.expected.y);
        return std::nullopt;
    }

    const auto match = best_template(assets, search.gray(window), search.spot.templates, search.ladder);
    if (!match) {
        LogWarn << "WorldMap: no icon candidate in the confirm window" << VAR(radius);
        return std::nullopt;
    }
    if (match->hit.score < search.spot.min_score) {
        LogWarn << "WorldMap: icon score below floor" << VAR(match->name) << VAR(match->hit.score) << VAR(search.spot.min_score)
                << VAR(radius);
        return std::nullopt;
    }
    return candidate_in_window(*match, window, search);
}

// 窗口比判定圈宽得多，里面坐着同类图标时取最高分未必取到期望的那个：
// 实测过隔壁传送点只高 0.024 分就把正对着期望位置的那个挤掉了。
// 判定圈外的先别扔，按判定圈的尺度再看一次近处——只多一次观测，近处没有就还用原来那个
IconCandidate look_closer_in(WorldMapAssets* assets, const IconSearch& search, int radius, const IconCandidate& found)
{
    const int tight = std::max(static_cast<int>(std::lround(search.spot.gate_base / search.viewport_scale)), kMinTemplateSide);
    if (tight >= radius) {
        return found;
    }
    LogInfo << "WorldMap: nearest icon sits outside the gate, looking again closer in" << VAR(found.hit.offset_base)
            << VAR(search.spot.gate_base) << VAR(radius) << VAR(tight);
    const auto closer = scan_icon_window(assets, search, tight);
    if (closer && closer->hit.offset_base < found.hit.offset_base) {
        return *closer;
    }
    return found;
}

std::optional<cv::Mat> white_pixels(cv::Mat patch, int white_floor)
{
    if (patch.channels() == 4) {
        cv::cvtColor(patch, patch, cv::COLOR_BGRA2BGR);
    }
    if (patch.channels() != 3) {
        LogWarn << "WorldMap: player marker needs a colour image" << VAR(patch.channels());
        return std::nullopt;
    }

    cv::Mat mask;
    cv::inRange(patch, cv::Scalar(white_floor, white_floor, white_floor), cv::Scalar(255, 255, 255), mask);
    return mask;
}

// 同亮度同面积的白色地图装饰过得了面积关，过不了这一关：
// 角色标记是实心三角，装饰的轮廓带大块凹陷
std::optional<double> blob_solidity(const cv::Mat& labels, const cv::Mat& stats, int label)
{
    const cv::Rect box(
        stats.at<int>(label, cv::CC_STAT_LEFT),
        stats.at<int>(label, cv::CC_STAT_TOP),
        stats.at<int>(label, cv::CC_STAT_WIDTH),
        stats.at<int>(label, cv::CC_STAT_HEIGHT));
    const cv::Mat blob = labels(box) == label;

    std::vector<std::vector<cv::Point>> contours;
    cv::findContours(blob, contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
    if (contours.empty()) {
        return std::nullopt;
    }
    const auto outer = std::ranges::max_element(contours, {}, [](const auto& c) { return cv::contourArea(c); });

    std::vector<cv::Point> hull;
    cv::convexHull(*outer, hull);
    const double hull_area = cv::contourArea(hull);
    const int area = stats.at<int>(label, cv::CC_STAT_AREA);
    return hull_area > 0.0 ? area / hull_area : 0.0;
}

} // namespace

std::optional<SpotHit>
    ConfirmSpot(WorldMapAssets* assets, const cv::Mat& screen, const cv::Point2d& expected, double viewport_scale, const SpotConfig& cfg)
{
    if (screen.empty() || viewport_scale <= 0.0 || cfg.templates.empty()) {
        return std::nullopt;
    }

    std::lock_guard guard(assets->mutex);
    const IconSearch search {
        .gray = ToGray(screen),
        .expected = expected,
        .viewport_scale = viewport_scale,
        .ladder =
            LinearLadder(cfg.scale_min, cfg.scale_max, static_cast<int>(std::lround((cfg.scale_max - cfg.scale_min) / cfg.scale_step)) + 1),
        .spot = cfg,
    };

    // 浮动的点位在一片范围里找，坐标只圈得住范围；钉死的点位就在期望位置开个小窗确认
    const bool floating = cfg.radius_base > 0.0;
    const int radius =
        floating ? static_cast<int>(std::lround(cfg.radius_base / viewport_scale)) : std::max(cfg.radius_screen, kMinTemplateSide);
    std::optional<IconCandidate> found = scan_icon_window(assets, search, radius);
    if (found && !floating && found->hit.offset_base > cfg.gate_base) {
        found = look_closer_in(assets, search, radius, *found);
    }
    if (!found) {
        return std::nullopt;
    }

    SpotHit out = found->hit;
    // 钉死的点位偏得太远就是认错了；浮动的点位本来就该在范围里晃，窗口自己就是那道闸
    if (!floating && out.offset_base > cfg.gate_base) {
        LogWarn << "WorldMap: icon too far from expected position" << VAR(out.template_name) << VAR(out.offset_base) << VAR(cfg.gate_base)
                << VAR(out.center.x) << VAR(out.center.y) << VAR(expected.x) << VAR(expected.y);
        return std::nullopt;
    }

    if (cfg.min_gold_ratio > 0.0) {
        out.gold_ratio = gold_ratio(screen, found->templ->saturation, found->templ->mask, found->live, cfg.saturation_floor);
        out.unlocked = out.gold_ratio >= cfg.min_gold_ratio;
    }

    LogInfo << "WorldMap: icon confirmed" << VAR(out.template_name) << VAR(out.center.x) << VAR(out.center.y) << VAR(out.hotspot.x)
            << VAR(out.hotspot.y) << VAR(out.score) << VAR(out.match_scale) << VAR(out.offset_base) << VAR(out.gold_ratio)
            << VAR(out.unlocked);
    return out;
}

std::optional<PlayerMarkerHit> DetectPlayerMarker(const cv::Mat& screen, const cv::Point2d& expected, const PlayerMarkerConfig& cfg)
{
    if (screen.empty()) {
        return std::nullopt;
    }

    const cv::Rect window = window_around(expected, cfg.search_radius, screen.size());
    if (window.empty()) {
        return std::nullopt;
    }

    const std::optional<cv::Mat> mask = white_pixels(screen(window), cfg.white_floor);
    if (!mask) {
        return std::nullopt;
    }

    cv::Mat labels;
    cv::Mat stats;
    cv::Mat centroids;
    const int count = cv::connectedComponentsWithStats(*mask, labels, stats, centroids, 8, CV_32S);

    std::optional<PlayerMarkerHit> best;
    for (int i = 1; i < count; ++i) {
        const int area = stats.at<int>(i, cv::CC_STAT_AREA);
        if (area < cfg.min_area || area > cfg.max_area || (best && area <= best->area)) {
            continue;
        }
        const std::optional<double> solidity = blob_solidity(labels, stats, i);
        if (!solidity || *solidity < cfg.min_solidity) {
            continue;
        }

        best = PlayerMarkerHit {
            .center = cv::Point2d(window.x + centroids.at<double>(i, 0), window.y + centroids.at<double>(i, 1)),
            .area = area,
            .solidity = *solidity,
        };
    }
    return best;
}

} // namespace worldmap
