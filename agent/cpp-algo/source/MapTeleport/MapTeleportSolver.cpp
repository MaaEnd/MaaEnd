#include "MapTeleportSolver.h"

#include <algorithm>
#include <cmath>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#include "MapLocator/MatchStrategy.h"
#include "MapNavigator/controller_type_utils.h"
#include "utils.h"

namespace fs = std::filesystem;

namespace mapteleport
{

namespace
{

constexpr int kMinTemplateSide = 24;
constexpr int kMinAnchorSide = 6;
constexpr const char* kAnchorTemplateDir = "SceneManager";
// 本组件自带一份锚点模板：SceneManager 的同名图各端出货尺寸不成比例，一套尺度阶梯罩不住
constexpr const char* kAnchorTemplateName = "MapTeleportAnchor.png";
constexpr const char* kCoreTemplateNames[] = { "MapTeleportCoreHub.png", "MapTeleportCoreSettlement.png" };

// HSV 的 S 通道。地图底色也能很艳，所以只在模板圈定的那些像素上取
cv::Mat SaturationOf(const cv::Mat& src)
{
    cv::Mat bgr = src;
    if (src.channels() == 4) {
        cv::cvtColor(src, bgr, cv::COLOR_BGRA2BGR);
    }
    if (bgr.channels() != 3) {
        return {};
    }

    cv::Mat hsv;
    cv::cvtColor(bgr, hsv, cv::COLOR_BGR2HSV);
    std::vector<cv::Mat> planes;
    cv::split(hsv, planes);
    return planes[1];
}

cv::Mat ToGray(const cv::Mat& src)
{
    if (src.channels() == 4) {
        cv::Mat gray;
        cv::cvtColor(src, gray, cv::COLOR_BGRA2GRAY);
        return gray;
    }
    if (src.channels() == 3) {
        cv::Mat gray;
        cv::cvtColor(src, gray, cv::COLOR_BGR2GRAY);
        return gray;
    }
    return src.clone();
}

// 区域底图是目录下含 Base、不含 Tier 的那张。各区域文件名不统一，靠这条规则收敛
std::optional<fs::path> FindZoneBaseFile(const fs::path& zoneDir)
{
    std::error_code ec;
    if (!fs::is_directory(zoneDir, ec)) {
        return std::nullopt;
    }

    for (const auto& entry : fs::directory_iterator(zoneDir, ec)) {
        if (!entry.is_regular_file()) {
            continue;
        }
        const fs::path& path = entry.path();
        if (path.extension() != ".png") {
            continue;
        }
        std::string stem = MAA_NS::path_to_utf8_string(path.stem());
        std::string lowered = stem;
        std::ranges::transform(lowered, lowered.begin(), [](unsigned char c) { return static_cast<char>(std::tolower(c)); });
        if (lowered.find("base") != std::string::npos && lowered.find("tier") == std::string::npos) {
            return path;
        }
    }
    return std::nullopt;
}

struct ScanHit
{
    double score = -1.0;
    double psr = 0.0;
    double scale = 0.0;
    cv::Point2d loc { 0.0, 0.0 };
    cv::Size size { 0, 0 };
};

struct ScanRung
{
    double scale = 0.0;
    double score = -1.0;
};

std::vector<double> GeometricLadder(double lo, double hi, double ratio)
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

std::vector<double> LinearLadder(double lo, double hi, int steps)
{
    std::vector<double> out;
    if (steps <= 0) {
        return out;
    }
    if (steps == 1) {
        out.push_back(lo);
        return out;
    }
    for (int i = 0; i < steps; ++i) {
        out.push_back(lo + (hi - lo) * i / (steps - 1));
    }
    return out;
}

// 在 haystack 上按给定尺度逐档缩放 needle 匹配，取分最高的一档。
// slack 是模板与搜索图的最小尺寸差：贴得太满时可落位置太少，分数会虚高。
// needleMask 空则整块模板等权
std::optional<ScanHit> ScanScales(
    const cv::Mat& haystack,
    const cv::Mat& needle,
    const cv::Mat& needleMask,
    const std::vector<double>& scales,
    int minSide,
    int slack,
    maplocator::PeakRefineMode refineMode,
    std::vector<ScanRung>* rungs = nullptr)
{
    if (haystack.empty() || needle.empty() || scales.empty()) {
        return std::nullopt;
    }

    const maplocator::PreparedSearchFeature prepared = maplocator::PrepareSearchFeature(haystack);

    std::optional<ScanHit> best;
    for (double scale : scales) {
        const int w = static_cast<int>(std::lround(needle.cols * scale));
        const int h = static_cast<int>(std::lround(needle.rows * scale));
        if (w < minSide || h < minSide || w + slack > haystack.cols || h + slack > haystack.rows) {
            continue;
        }

        cv::Mat scaled;
        cv::resize(needle, scaled, cv::Size(w, h), 0.0, 0.0, scale < 1.0 ? cv::INTER_AREA : cv::INTER_LINEAR);

        cv::Mat mask(scaled.size(), CV_8UC1, cv::Scalar(255));
        if (!needleMask.empty()) {
            // 掩膜是二值的，插值会在边界上造出中间值，只能用最近邻
            cv::resize(needleMask, mask, scaled.size(), 0.0, 0.0, cv::INTER_NEAREST);
        }

        const auto hit = maplocator::CoreMatchPrepared(prepared, scaled, mask, refineMode);
        if (!hit) {
            continue;
        }

        if (rungs != nullptr) {
            rungs->push_back(ScanRung { .scale = scale, .score = hit->score });
        }
        if (!best || hit->score > best->score) {
            best = ScanHit {
                .score = hit->score,
                .psr = hit->psr,
                .scale = scale,
                .loc = hit->loc,
                .size = cv::Size(w, h),
            };
        }
    }

    return best;
}

} // namespace

MapTeleportSolver::MapTeleportSolver(std::vector<fs::path> imageRoots)
    : _imageRoots(std::move(imageRoots))
{
}

cv::Rect MapTeleportSolver::SafeArea(const cv::Size& screenSize, const ScreenMapRoi& roi, int margin)
{
    const int x0 = static_cast<int>(std::lround(screenSize.width * roi.left)) + margin;
    const int y0 = static_cast<int>(std::lround(screenSize.height * roi.top)) + margin;
    const int x1 = static_cast<int>(std::lround(screenSize.width * roi.right)) - margin;
    const int y1 = static_cast<int>(std::lround(screenSize.height * roi.bottom)) - margin;
    if (x1 <= x0 || y1 <= y0) {
        return {};
    }
    return cv::Rect(x0, y0, x1 - x0, y1 - y0) & cv::Rect(0, 0, screenSize.width, screenSize.height);
}

std::optional<CoreIconHit> MapTeleportSolver::ConfirmCoreIcon(
    const cv::Mat& screen,
    const cv::Point2d& expected,
    double viewportScale,
    const CoreIconConfig& cfg)
{
    if (screen.empty() || viewportScale <= 0.0) {
        return std::nullopt;
    }

    std::lock_guard guard(_mutex);
    const std::vector<CoreTemplate>* templates = LoadCoreTemplates();
    if (templates == nullptr) {
        return std::nullopt;
    }

    const int radius = static_cast<int>(std::lround(cfg.searchBase / viewportScale));
    cv::Rect window(
        static_cast<int>(std::lround(expected.x)) - radius,
        static_cast<int>(std::lround(expected.y)) - radius,
        radius * 2,
        radius * 2);
    window &= cv::Rect(0, 0, screen.cols, screen.rows);
    if (window.empty()) {
        return std::nullopt;
    }

    const cv::Mat patch = ToGray(screen)(window);
    const std::vector<double> ladder =
        LinearLadder(cfg.scaleMin, cfg.scaleMax, static_cast<int>(std::lround((cfg.scaleMax - cfg.scaleMin) / cfg.scaleStep)) + 1);

    const CoreTemplate* pick = nullptr;
    std::optional<ScanHit> best;
    for (const CoreTemplate& templ : *templates) {
        const auto hit = ScanScales(patch, templ.gray, templ.mask, ladder, kMinAnchorSide, 0, maplocator::PeakRefineMode::Continuous);
        if (hit && (!best || hit->score > best->score)) {
            best = hit;
            pick = &templ;
        }
    }
    if (!best) {
        LogWarn << "MapTeleport: no core icon candidate in the float region";
        return std::nullopt;
    }
    if (best->score < cfg.minScore) {
        LogWarn << "MapTeleport: core icon score below floor" << VAR(pick->name) << VAR(best->score) << VAR(cfg.minScore);
        return std::nullopt;
    }

    CoreIconHit out;
    out.center = cv::Point2d(window.x + best->loc.x + best->size.width / 2.0, window.y + best->loc.y + best->size.height / 2.0);
    out.size = best->size;
    out.score = best->score;
    out.matchScale = best->scale;

    // 模板哪些像素是金的，就到实拍的同一批像素上看还金不金。没解锁的整块褪成灰，比例会塌下去
    cv::Mat templSat;
    cv::Mat templMask;
    cv::resize(pick->saturation, templSat, best->size, 0.0, 0.0, cv::INTER_NEAREST);
    cv::resize(pick->mask, templMask, best->size, 0.0, 0.0, cv::INTER_NEAREST);
    cv::Mat gold = (templSat >= cfg.saturationFloor);
    cv::bitwise_and(gold, templMask, gold);

    const cv::Rect live(
        window.x + static_cast<int>(std::lround(best->loc.x)),
        window.y + static_cast<int>(std::lround(best->loc.y)),
        best->size.width,
        best->size.height);
    const int total = cv::countNonZero(gold);
    if (total > 0 && (live & cv::Rect(0, 0, screen.cols, screen.rows)) == live) {
        const cv::Mat liveSat = SaturationOf(screen(live));
        cv::Mat lit = (liveSat >= cfg.saturationFloor);
        cv::bitwise_and(lit, gold, lit);
        out.goldRatio = static_cast<double>(cv::countNonZero(lit)) / total;
    }
    out.unlocked = out.goldRatio >= cfg.minGoldRatio;

    LogInfo << "MapTeleport: core icon matched" << VAR(pick->name) << VAR(out.center.x) << VAR(out.center.y) << VAR(out.score)
            << VAR(out.matchScale) << VAR(out.goldRatio) << VAR(out.unlocked);
    return out;
}

std::optional<PlayerMarkerHit> MapTeleportSolver::DetectPlayerMarker(
    const cv::Mat& screen,
    const cv::Point2d& expected,
    const PlayerMarkerConfig& cfg)
{
    if (screen.empty()) {
        return std::nullopt;
    }

    cv::Rect window(
        static_cast<int>(std::lround(expected.x)) - cfg.searchRadius,
        static_cast<int>(std::lround(expected.y)) - cfg.searchRadius,
        cfg.searchRadius * 2,
        cfg.searchRadius * 2);
    window &= cv::Rect(0, 0, screen.cols, screen.rows);
    if (window.empty()) {
        return std::nullopt;
    }

    cv::Mat patch = screen(window);
    if (patch.channels() == 4) {
        cv::cvtColor(patch, patch, cv::COLOR_BGRA2BGR);
    }
    if (patch.channels() != 3) {
        LogWarn << "MapTeleport: player marker needs a colour image" << VAR(patch.channels());
        return std::nullopt;
    }

    cv::Mat mask;
    cv::inRange(
        patch,
        cv::Scalar(cfg.whiteFloor, cfg.whiteFloor, cfg.whiteFloor),
        cv::Scalar(255, 255, 255),
        mask);

    cv::Mat labels;
    cv::Mat stats;
    cv::Mat centroids;
    const int count = cv::connectedComponentsWithStats(mask, labels, stats, centroids, 8, CV_32S);

    std::optional<PlayerMarkerHit> best;
    for (int i = 1; i < count; ++i) {
        const int area = stats.at<int>(i, cv::CC_STAT_AREA);
        if (area < cfg.minArea || area > cfg.maxArea || (best && area <= best->area)) {
            continue;
        }

        // 同亮度同面积的白色地图装饰过得了面积关，过不了这一关：
        // 角色标记是实心三角，装饰的轮廓带大块凹陷
        const cv::Rect box(
            stats.at<int>(i, cv::CC_STAT_LEFT),
            stats.at<int>(i, cv::CC_STAT_TOP),
            stats.at<int>(i, cv::CC_STAT_WIDTH),
            stats.at<int>(i, cv::CC_STAT_HEIGHT));
        const cv::Mat blob = labels(box) == i;

        std::vector<std::vector<cv::Point>> contours;
        cv::findContours(blob, contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
        if (contours.empty()) {
            continue;
        }
        const auto outer = std::ranges::max_element(contours, {}, [](const auto& c) { return cv::contourArea(c); });

        std::vector<cv::Point> hull;
        cv::convexHull(*outer, hull);
        const double hullArea = cv::contourArea(hull);
        const double solidity = hullArea > 0.0 ? area / hullArea : 0.0;
        if (solidity < cfg.minSolidity) {
            continue;
        }

        best = PlayerMarkerHit {
            .center = cv::Point2d(window.x + centroids.at<double>(i, 0), window.y + centroids.at<double>(i, 1)),
            .area = area,
            .solidity = solidity,
        };
    }
    return best;
}

const cv::Mat* MapTeleportSolver::LoadZoneBase(const std::string& zone)
{
    if (const auto it = _zoneBases.find(zone); it != _zoneBases.end()) {
        return it->second.empty() ? nullptr : &it->second;
    }

    const auto zoneDir = mapnavigator::ResolveResourceImage(_imageRoots, fs::path("MapLocator") / MAA_NS::path(zone));
    const auto file = zoneDir ? FindZoneBaseFile(*zoneDir) : std::nullopt;
    if (!file) {
        LogError << "MapTeleport: zone base image not found" << VAR(zone) << VAR(mapnavigator::DescribeRoots(_imageRoots));
        _zoneBases[zone] = cv::Mat();
        return nullptr;
    }

    cv::Mat image = cv::imread(MAA_NS::path_to_utf8_string(*file), cv::IMREAD_UNCHANGED);
    if (image.empty()) {
        LogError << "MapTeleport: failed to read zone base" << VAR(MAA_NS::path_to_utf8_string(*file));
        _zoneBases[zone] = cv::Mat();
        return nullptr;
    }

    LogInfo << "MapTeleport: zone base loaded" << VAR(zone) << VAR(image.cols) << VAR(image.rows);
    auto [it, _] = _zoneBases.emplace(zone, ToGray(image));
    return &it->second;
}

const cv::Mat* MapTeleportSolver::LoadAnchorTemplate()
{
    if (_anchorTemplateTried) {
        return _anchorTemplate.empty() ? nullptr : &_anchorTemplate;
    }
    _anchorTemplateTried = true;

    const auto file = mapnavigator::ResolveResourceImage(_imageRoots, fs::path(kAnchorTemplateDir) / kAnchorTemplateName);
    cv::Mat image = file ? cv::imread(MAA_NS::path_to_utf8_string(*file), cv::IMREAD_UNCHANGED) : cv::Mat();
    if (image.empty()) {
        LogError << "MapTeleport: anchor template not found" << VAR(kAnchorTemplateName) << VAR(mapnavigator::DescribeRoots(_imageRoots));
        return nullptr;
    }

    _anchorTemplate = ToGray(image);
    LogInfo << "MapTeleport: anchor template loaded" << VAR(MAA_NS::path_to_utf8_string(*file)) << VAR(_anchorTemplate.cols)
            << VAR(_anchorTemplate.rows);
    return &_anchorTemplate;
}

const std::vector<MapTeleportSolver::CoreTemplate>* MapTeleportSolver::LoadCoreTemplates()
{
    if (_coreTemplatesTried) {
        return _coreTemplates.empty() ? nullptr : &_coreTemplates;
    }
    _coreTemplatesTried = true;

    for (const char* name : kCoreTemplateNames) {
        const auto file = mapnavigator::ResolveResourceImage(_imageRoots, fs::path(kAnchorTemplateDir) / name);
        const cv::Mat image = file ? cv::imread(MAA_NS::path_to_utf8_string(*file), cv::IMREAD_UNCHANGED) : cv::Mat();
        if (image.empty() || image.channels() != 4) {
            LogError << "MapTeleport: core template unusable" << VAR(name) << VAR(mapnavigator::DescribeRoots(_imageRoots));
            continue;
        }

        // alpha 就是掩膜：图标外那圈光晕在实拍里透着地形，让它参与相关只会压低分
        std::vector<cv::Mat> planes;
        cv::split(image, planes);
        const cv::Mat mask = planes[3] >= 128;

        _coreTemplates.push_back(
            CoreTemplate { .name = name, .gray = ToGray(image), .mask = mask, .saturation = SaturationOf(image) });
        LogInfo << "MapTeleport: core template loaded" << VAR(name) << VAR(image.cols) << VAR(cv::countNonZero(mask));
    }

    return _coreTemplates.empty() ? nullptr : &_coreTemplates;
}

bool MapTeleportSolver::HasZone(const std::string& zone)
{
    std::lock_guard guard(_mutex);
    return LoadZoneBase(zone) != nullptr;
}

std::optional<Viewport> MapTeleportSolver::SolveViewport(const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg)
{
    if (screen.empty()) {
        LogError << "MapTeleport: empty screen image";
        return std::nullopt;
    }

    std::lock_guard guard(_mutex);
    const cv::Mat* base = LoadZoneBase(zone);
    if (base == nullptr) {
        return std::nullopt;
    }

    const cv::Rect roiRect = SafeArea(screen.size(), cfg.roi, 0);
    if (roiRect.width < kMinTemplateSide || roiRect.height < kMinTemplateSide) {
        LogError << "MapTeleport: screen roi too small" << VAR(roiRect.width) << VAR(roiRect.height);
        return std::nullopt;
    }
    const cv::Mat roi = ToGray(screen)(roiRect);

    // 粗解：降采样后扫全尺度带，只为把细解的搜索窗放到正确位置
    const int down = std::max(1, cfg.coarseDownscale);
    cv::Mat baseSmall;
    cv::Mat roiSmall;
    cv::resize(*base, baseSmall, cv::Size(base->cols / down, base->rows / down), 0.0, 0.0, cv::INTER_AREA);
    cv::resize(roi, roiSmall, cv::Size(roi.cols / down, roi.rows / down), 0.0, 0.0, cv::INTER_AREA);

    std::vector<ScanRung> coarseRungs;
    const auto coarse = ScanScales(
        baseSmall,
        roiSmall,
        cv::Mat(),
        GeometricLadder(cfg.scaleMin, cfg.scaleMax, cfg.coarseRatio),
        kMinTemplateSide,
        std::max(1, static_cast<int>(std::lround(cfg.scanSlack / static_cast<double>(down)))),
        maplocator::PeakRefineMode::Parabola,
        &coarseRungs);
    if (!coarse) {
        LogWarn << "MapTeleport: coarse viewport scan found nothing" << VAR(zone);
        return std::nullopt;
    }

    // 细解：回到原尺度，搜索窗只覆盖粗解落点附近，两侧各留一个降采样格的不确定度。
    // 尺度只需覆盖到相邻两档粗解之间，再宽就是白扫
    const int pad = down * 3;
    const double span = coarse->scale * (cfg.coarseRatio - 1.0) * 1.2;
    const int windowX = std::max(0, static_cast<int>(std::lround(coarse->loc.x * down)) - pad);
    const int windowY = std::max(0, static_cast<int>(std::lround(coarse->loc.y * down)) - pad);
    const cv::Rect window(
        windowX,
        windowY,
        std::min(static_cast<int>(std::lround(roi.cols * (coarse->scale + span))) + pad * 2, base->cols - windowX),
        std::min(static_cast<int>(std::lround(roi.rows * (coarse->scale + span))) + pad * 2, base->rows - windowY));
    if (window.width < kMinTemplateSide || window.height < kMinTemplateSide) {
        LogWarn << "MapTeleport: fine window degenerated" << VAR(window.width) << VAR(window.height);
        return std::nullopt;
    }

    // 细解窗口是照模板尺寸开的，本来就贴边，可落位置已由粗解锚定，不需要再留余量
    const auto fine = ScanScales(
        (*base)(window),
        roi,
        cv::Mat(),
        LinearLadder(std::max(cfg.scaleMin, coarse->scale - span), coarse->scale + span, cfg.fineSteps),
        kMinTemplateSide,
        0,
        maplocator::PeakRefineMode::Continuous);
    if (!fine) {
        LogWarn << "MapTeleport: fine viewport scan found nothing" << VAR(zone);
        return std::nullopt;
    }

    // 可信度只能拿粗解档来算：细解各档彼此相差不到一成，分数几乎一样高，
    // 互为次高分毫无意义。与尺度差 6% 以上的粗解档比，才说明这个解真的唯一
    double rivalBest = 0.0;
    for (const ScanRung& rung : coarseRungs) {
        if (std::abs(rung.scale - fine->scale) > 0.06 * fine->scale) {
            rivalBest = std::max(rivalBest, rung.score);
        }
    }
    const double delta = fine->score - rivalBest;

    if (fine->score < cfg.minScore || delta < cfg.minDelta) {
        LogWarn << "MapTeleport: viewport rejected" << VAR(zone) << VAR(fine->score) << VAR(delta) << VAR(fine->psr)
                << VAR(cfg.minScore) << VAR(cfg.minDelta);
        return std::nullopt;
    }

    Viewport vp;
    vp.scale = fine->scale;
    vp.roiOrigin = cv::Point2d(roiRect.x, roiRect.y);
    vp.baseOrigin = cv::Point2d(window.x + fine->loc.x, window.y + fine->loc.y);
    vp.roiSize = roiRect.size();
    vp.score = fine->score;
    vp.delta = delta;
    vp.psr = fine->psr;

    LogInfo << "MapTeleport: viewport solved" << VAR(zone) << VAR(vp.scale) << VAR(vp.baseOrigin.x) << VAR(vp.baseOrigin.y)
            << VAR(vp.score) << VAR(vp.delta) << VAR(vp.psr);
    return vp;
}

std::optional<AnchorHit> MapTeleportSolver::ConfirmAnchor(
    const cv::Mat& screen,
    const cv::Point2d& expected,
    double viewportScale,
    const AnchorConfig& cfg)
{
    if (screen.empty() || viewportScale <= 0.0) {
        return std::nullopt;
    }

    std::lock_guard guard(_mutex);
    const cv::Mat* templ = LoadAnchorTemplate();
    if (templ == nullptr) {
        return std::nullopt;
    }

    const int radius = std::max(cfg.searchRadius, kMinTemplateSide);
    cv::Rect window(
        static_cast<int>(std::lround(expected.x)) - radius,
        static_cast<int>(std::lround(expected.y)) - radius,
        radius * 2,
        radius * 2);
    window &= cv::Rect(0, 0, screen.cols, screen.rows);
    if (window.width < templ->cols || window.height < templ->rows) {
        LogWarn << "MapTeleport: confirm window too small" << VAR(window.width) << VAR(window.height);
        return std::nullopt;
    }

    // 图标只有二十来像素宽，最小边长得按它来卡，照视口那套会把整个模板筛掉
    const cv::Mat patch = ToGray(screen)(window);
    const auto hit = ScanScales(
        patch,
        *templ,
        cv::Mat(),
        LinearLadder(cfg.scaleMin, cfg.scaleMax, static_cast<int>(std::lround((cfg.scaleMax - cfg.scaleMin) / cfg.scaleStep)) + 1),
        kMinAnchorSide,
        0,
        maplocator::PeakRefineMode::Continuous);
    if (!hit) {
        LogWarn << "MapTeleport: no anchor candidate in confirm window";
        return std::nullopt;
    }

    if (hit->score < cfg.minScore) {
        LogWarn << "MapTeleport: anchor score below floor" << VAR(hit->score) << VAR(cfg.minScore);
        return std::nullopt;
    }

    AnchorHit out;
    out.center = cv::Point2d(window.x + hit->loc.x + hit->size.width / 2.0, window.y + hit->loc.y + hit->size.height / 2.0);
    out.size = hit->size;
    out.score = hit->score;
    out.matchScale = hit->scale;
    out.offsetBase = std::hypot(out.center.x - expected.x, out.center.y - expected.y) * viewportScale;

    if (out.offsetBase > cfg.gateBase) {
        LogWarn << "MapTeleport: anchor too far from expected position" << VAR(out.offsetBase) << VAR(cfg.gateBase)
                << VAR(out.center.x) << VAR(out.center.y) << VAR(expected.x) << VAR(expected.y);
        return std::nullopt;
    }

    LogInfo << "MapTeleport: anchor confirmed" << VAR(out.center.x) << VAR(out.center.y) << VAR(out.score) << VAR(out.matchScale)
            << VAR(out.offsetBase);
    return out;
}

MapTeleportSolver& GetSolver(std::string_view controller_type)
{
    static MapTeleportSolver solver(mapnavigator::ResourceImageRoots(controller_type));
    return solver;
}

} // namespace mapteleport
