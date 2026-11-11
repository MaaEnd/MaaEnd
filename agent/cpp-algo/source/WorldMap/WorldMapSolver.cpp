#include "WorldMapSolver.h"

#include <algorithm>
#include <cmath>
#include <format>
#include <regex>
#include <utility>
#include <vector>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <meojson/json.hpp>

#include "../Common/JsoncFile.h"
#include "MapLocator/MapAlgorithm.h"
#include "MapLocator/MatchStrategy.h"
#include "MapNavigator/control/controller_type_utils.h"
#include "MapNavigator/planning/navmesh_cache.h"
#include "Navmesh/BaseNavReader.h"
#include "utils.h"

namespace fs = std::filesystem;

namespace worldmap
{

namespace
{

constexpr int kMinTemplateSide = 24;
constexpr int kMinAnchorSide = 6;
constexpr const char* kIconDir = "SceneManager";

// 选中分层时大地图其余部分压暗后的亮度比例
constexpr double kTierDimRatio = 0.33;
// 分层四周带上的底图宽度，单位底图像素
constexpr int kTierMargin = 300;
// 两个分层解出的视口相距超过它就判失败，单位底图像素
constexpr double kTierAgreeBase = 10.0;
// 游戏在地图外画的灰底亮度，用来填底图的透明处
constexpr int kOffMapGray = 34;

// 图标表与它认的那些模板同目录，一起走资源层次：某一端的图标画得不一样时，
// 那一层放自己的模板和自己的阈值即可
constexpr const char* kIconTableName = "MapIcons.json";

// 表里一条图标的原样。留空的项按 SpotConfig 的缺省走
struct IconEntry
{
    std::vector<std::string> templates;
    std::vector<double> scale;
    double scale_step = 0.0;
    double threshold = 0.0;
    double gate = 0.0;
    double radius = 0.0;
    double gold_ratio = 0.0;
    bool occluded_by_player = false;

    MEO_JSONIZATION(
        templates,
        MEO_OPT scale,
        MEO_OPT scale_step,
        MEO_OPT threshold,
        MEO_OPT gate,
        MEO_OPT radius,
        MEO_OPT gold_ratio,
        MEO_OPT occluded_by_player);
};

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

// 图标本体上最厚实的那一点，相对模板中心。取掩膜最大连通块（本体），
// 再取块内离边缘最远处：定居点核心的模板是「金雕像 + 等级牌」两块夹着一片空档，
// 框中心正落在空档里，交整框出去就会点到图标旁边的地面上。
// 距离并列时取最靠近本体重心的那个——全不透明的模板整条中轴线并列，取首会偏到一边
cv::Point2d BodyHotspot(const cv::Mat& mask, const cv::Size& size)
{
    const cv::Point2d center((size.width - 1) / 2.0, (size.height - 1) / 2.0);
    if (mask.empty() || mask.size() != size) {
        return { 0.0, 0.0 };
    }

    cv::Mat labels;
    cv::Mat stats;
    cv::Mat centroids;
    const int count = cv::connectedComponentsWithStats(mask, labels, stats, centroids, 8, CV_32S);
    int body = 0;
    int bodyArea = 0;
    for (int i = 1; i < count; ++i) {
        const int area = stats.at<int>(i, cv::CC_STAT_AREA);
        if (area > bodyArea) {
            bodyArea = area;
            body = i;
        }
    }
    if (body == 0) {
        return { 0.0, 0.0 };
    }

    // 补一圈零再做距离变换，免得贴着模板边的图标被当成离边缘很远
    cv::Mat padded;
    cv::copyMakeBorder(labels == body, padded, 1, 1, 1, 1, cv::BORDER_CONSTANT, cv::Scalar(0));
    cv::Mat dist;
    cv::distanceTransform(padded, dist, cv::DIST_L2, 5);

    double peak = 0.0;
    cv::minMaxLoc(dist, nullptr, &peak);
    const cv::Point2d anchor(centroids.at<double>(body, 0), centroids.at<double>(body, 1));
    cv::Point2d best = anchor;
    double bestGap = -1.0;
    for (int y = 0; y < size.height; ++y) {
        for (int x = 0; x < size.width; ++x) {
            // 差半像素以内算并列
            if (dist.at<float>(y + 1, x + 1) < peak - 0.5) {
                continue;
            }
            const double gap = std::hypot(x - anchor.x, y - anchor.y);
            if (bestGap < 0.0 || gap < bestGap) {
                bestGap = gap;
                best = cv::Point2d(x, y);
            }
        }
    }
    return best - center;
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

cv::Mat BaseGray(const cv::Mat& image)
{
    cv::Mat gray = ToGray(image);
    if (image.channels() == 4) {
        cv::Mat alpha;
        cv::extractChannel(image, alpha, 3);
        gray.setTo(kOffMapGray, alpha == 0);
    }
    return gray;
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

// 导航包里的分层区名：LvNNNTierMMM 记作 <区域>_L<N>_<M>，其余用文件名
std::string TierZoneName(const std::string& zone, const fs::path& file)
{
    const std::string stem = MAA_NS::path_to_utf8_string(file.stem());
    if (file.extension() != ".png" || stem.find("Tier") == std::string::npos) {
        return {};
    }
    static const std::regex kLayerFile(R"(Lv(\d+)Tier(\d+))");
    std::smatch match;
    if (std::regex_match(stem, match, kLayerFile)) {
        return std::format("{}_L{}_{}", zone, std::stoi(match[1].str()), std::stoi(match[2].str()));
    }
    return stem;
}

const navmesh::BaseNavZone* FindTierPlacement(const std::vector<navmesh::BaseNavZone>& zones, const std::string& name)
{
    for (const navmesh::BaseNavZone& zone : zones) {
        if (navmesh::IsTierZone(zone) && zone.name == name) {
            return &zone;
        }
    }
    return nullptr;
}

TierPatch MakeTierPatch(const std::string& name, const cv::Mat& image, const navmesh::BaseNavZone& placement, const cv::Mat& base)
{
    const auto& [sx, tx, sy, ty] = placement.transform;
    const cv::Rect rect(
        static_cast<int>(std::lround(tx)),
        static_cast<int>(std::lround(ty)),
        static_cast<int>(std::lround(image.cols * sx)),
        static_cast<int>(std::lround(image.rows * sy)));

    cv::Mat gray = ToGray(image);
    cv::Mat opaque(gray.size(), CV_8UC1, cv::Scalar(255));
    if (image.channels() == 4) {
        std::vector<cv::Mat> planes;
        cv::split(image, planes);
        opaque = planes[3] >= 128;
    }
    if (gray.size() != rect.size()) {
        cv::resize(gray, gray, rect.size(), 0.0, 0.0, cv::INTER_AREA);
        cv::resize(opaque, opaque, rect.size(), 0.0, 0.0, cv::INTER_NEAREST);
    }

    const cv::Rect bounds(0, 0, base.cols, base.rows);
    const cv::Rect crop =
        cv::Rect(rect.x - kTierMargin, rect.y - kTierMargin, rect.width + kTierMargin * 2, rect.height + kTierMargin * 2) & bounds;
    const cv::Rect pasted = rect & bounds;

    TierPatch patch;
    patch.name = name;
    patch.area = rect;
    patch.origin = crop.tl();
    patch.gray = base(crop) * kTierDimRatio;
    gray(pasted - rect.tl()).copyTo(patch.gray(pasted - crop.tl()), opaque(pasted - rect.tl()));
    return patch;
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

cv::Size ScaledSize(const cv::Mat& image, double scale)
{
    return { static_cast<int>(std::lround(image.cols * scale)), static_cast<int>(std::lround(image.rows * scale)) };
}

cv::Mat ResizeTo(const cv::Mat& image, const cv::Size& size)
{
    cv::Mat resized;
    cv::resize(image, resized, size, 0.0, 0.0, size.width < image.cols ? cv::INTER_AREA : cv::INTER_LINEAR);
    return resized;
}

void KeepBest(std::optional<ScanHit>* best, const maplocator::MatchResultRaw& hit, double scale, const cv::Size& size)
{
    if (!*best || hit.score > (*best)->score) {
        *best = ScanHit { .score = hit.score, .psr = hit.psr, .scale = scale, .loc = hit.loc, .size = size };
    }
}

std::optional<ScanHit> ScanViewportScales(
    const cv::Mat& haystack,
    const cv::Mat& needle,
    const std::vector<double>& scales,
    int slack,
    maplocator::PeakRefineMode refineMode,
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
        const auto hit = maplocator::CoreMatchPrepared(prepared, scaled, cv::Mat(size, CV_8UC1, cv::Scalar(255)), refineMode);
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

std::optional<ScanHit>
    ScanIconScales(const cv::Mat& haystack, const cv::Mat& needle, const cv::Mat& needleMask, const std::vector<double>& scales)
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
        if (!needleMask.empty()) {
            // 掩膜是二值的，只能用最近邻插值
            cv::resize(needleMask, mask, size, 0.0, 0.0, cv::INTER_NEAREST);
        }
        const auto hit = maplocator::CoreMatchPrepared(prepared, ResizeTo(needle, size), mask, maplocator::PeakRefineMode::Continuous);
        if (hit) {
            KeepBest(&best, *hit, scale, size);
        }
    }
    return best;
}

struct ScreenRoi
{
    cv::Rect rect;
    cv::Mat full;
    cv::Mat small;
};

std::optional<ScreenRoi> CropScreenRoi(const cv::Mat& screen, const ViewportConfig& cfg)
{
    const cv::Rect rect = WorldMapSolver::SafeArea(screen.size(), cfg.roi, 0);
    if (rect.width < kMinTemplateSide || rect.height < kMinTemplateSide) {
        LogError << "WorldMap: screen roi too small" << VAR(rect.width) << VAR(rect.height);
        return std::nullopt;
    }
    const int down = std::max(1, cfg.coarseDownscale);
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

// 钉尺度时只留给定档与相邻两档，相邻两档同时充当算可信度的对手
std::vector<double> CoarseLadder(const ViewportConfig& cfg)
{
    return cfg.scaleHint ? GeometricLadder(*cfg.scaleHint / cfg.coarseRatio, *cfg.scaleHint * cfg.coarseRatio, cfg.coarseRatio)
                         : GeometricLadder(cfg.scaleMin, cfg.scaleMax, cfg.coarseRatio);
}

int CoarseSlack(const ViewportConfig& cfg)
{
    return std::max(1, static_cast<int>(std::lround(cfg.scanSlack / static_cast<double>(std::max(1, cfg.coarseDownscale)))));
}

// 粗解在降采样图上定位置，细解回到原尺度定尺度，再拿粗解各档判这个解唯不唯一
std::optional<Viewport> ScanViewport(
    const ScanBase& base,
    const cv::Mat& roi,
    const cv::Mat& roiSmall,
    const cv::Rect& roiRect,
    const ViewportConfig& cfg,
    const std::string& zone)
{
    const int down = std::max(1, cfg.coarseDownscale);

    std::vector<ScanRung> coarseRungs;
    const auto coarse =
        ScanViewportScales(base.small, roiSmall, CoarseLadder(cfg), CoarseSlack(cfg), maplocator::PeakRefineMode::Parabola, &coarseRungs);
    if (!coarse) {
        LogWarn << "WorldMap: coarse viewport scan found nothing" << VAR(zone) << VAR(base.tier);
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
        std::min(static_cast<int>(std::lround(roi.cols * (coarse->scale + span))) + pad * 2, base.full.cols - windowX),
        std::min(static_cast<int>(std::lround(roi.rows * (coarse->scale + span))) + pad * 2, base.full.rows - windowY));
    if (window.width < kMinTemplateSide || window.height < kMinTemplateSide) {
        LogWarn << "WorldMap: fine window degenerated" << VAR(window.width) << VAR(window.height);
        return std::nullopt;
    }

    // 钉尺度时细解钉的是给定值本身，不跟着粗解落档走：缩放变了就该让分数掉下去被拒，
    // 跟过去反倒会拿一个差着一档的尺度算出错位置
    const std::vector<double> fineLadder =
        cfg.scaleHint ? std::vector<double> { *cfg.scaleHint }
                      : LinearLadder(std::max(cfg.scaleMin, coarse->scale - span), coarse->scale + span, cfg.fineSteps);

    // 细解窗口是照模板尺寸开的，本来就贴边，可落位置已由粗解锚定，不需要再留余量
    const auto fine = ScanViewportScales(base.full(window), roi, fineLadder, 0, maplocator::PeakRefineMode::Continuous, nullptr);
    if (!fine) {
        LogWarn << "WorldMap: fine viewport scan found nothing" << VAR(zone) << VAR(base.tier);
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
        LogWarn << "WorldMap: viewport rejected" << VAR(zone) << VAR(base.tier) << VAR(fine->score) << VAR(delta) << VAR(fine->psr)
                << VAR(fine->scale) << VAR(cfg.minScore) << VAR(cfg.minDelta);
        return std::nullopt;
    }

    Viewport vp;
    vp.scale = fine->scale;
    vp.roiOrigin = cv::Point2d(roiRect.x, roiRect.y);
    vp.baseOrigin = cv::Point2d(base.origin.x + window.x + fine->loc.x, base.origin.y + window.y + fine->loc.y);
    vp.roiSize = roiRect.size();
    vp.score = fine->score;
    vp.delta = delta;
    vp.psr = fine->psr;
    vp.tier = base.tier;

    LogInfo << "WorldMap: viewport solved" << VAR(zone) << VAR(vp.tier) << VAR(vp.scale) << VAR(vp.baseOrigin.x) << VAR(vp.baseOrigin.y)
            << VAR(vp.score) << VAR(vp.delta) << VAR(vp.psr);
    return vp;
}

// 模板哪些像素是金的，就到实拍的同一批像素上看还金不金。没解锁的整块褪成灰，比例会塌下去
double GoldRatio(const cv::Mat& screen, const cv::Mat& templSat, const cv::Mat& templMask, const cv::Rect& live, int satFloor)
{
    if (templSat.empty() || (live & cv::Rect(0, 0, screen.cols, screen.rows)) != live) {
        return 0.0;
    }

    cv::Mat sat;
    cv::resize(templSat, sat, live.size(), 0.0, 0.0, cv::INTER_NEAREST);
    cv::Mat gold = (sat >= satFloor);
    if (!templMask.empty()) {
        cv::Mat mask;
        cv::resize(templMask, mask, live.size(), 0.0, 0.0, cv::INTER_NEAREST);
        cv::bitwise_and(gold, mask, gold);
    }

    const int total = cv::countNonZero(gold);
    if (total == 0) {
        return 0.0;
    }

    cv::Mat lit = (SaturationOf(screen(live)) >= satFloor);
    cv::bitwise_and(lit, gold, lit);
    return static_cast<double>(cv::countNonZero(lit)) / total;
}

} // namespace

WorldMapSolver::WorldMapSolver(std::vector<fs::path> imageRoots, fs::path navmeshPack)
    : _imageRoots(std::move(imageRoots))
    , _navmeshPack(std::move(navmeshPack))
{
}

cv::Rect WorldMapSolver::SafeArea(const cv::Size& screenSize, const ScreenMapRoi& roi, int margin)
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

void WorldMapSolver::LoadIconTable()
{
    if (_iconTableLoaded) {
        return;
    }
    _iconTableLoaded = true;

    const auto file = mapnavigator::ResolveResourceImage(_imageRoots, fs::path(kIconDir) / kIconTableName);
    if (!file) {
        LogError << "WorldMap: icon table not found" << VAR(kIconTableName) << VAR(mapnavigator::DescribeRoots(_imageRoots));
        return;
    }

    // 仓库内 JSONC 允许注释、尾逗号与 BOM，用公共 OpenJsoncFile 读取。
    const auto parsed = common::OpenJsoncFile(*file);
    if (!parsed || !parsed->is_object()) {
        LogError << "WorldMap: icon table is not a JSON object" << VAR(MAA_NS::path_to_utf8_string(*file));
        return;
    }

    for (const auto& [name, raw] : parsed->as_object()) {
        IconEntry entry {};
        if (!entry.from_json(raw) || entry.templates.empty()) {
            LogError << "WorldMap: icon table entry needs a non-empty 'templates'" << VAR(name);
            continue;
        }

        IconSpec spec {};
        spec.spot.templates = entry.templates;
        if (entry.scale.size() == 2 && entry.scale[0] > 0.0 && entry.scale[1] >= entry.scale[0]) {
            spec.spot.scaleMin = entry.scale[0];
            spec.spot.scaleMax = entry.scale[1];
        }
        if (entry.scale_step > 0.0) {
            spec.spot.scaleStep = entry.scale_step;
        }
        if (entry.threshold > 0.0) {
            spec.spot.minScore = entry.threshold;
        }
        if (entry.gate > 0.0) {
            spec.spot.gateBase = entry.gate;
        }
        spec.spot.radiusBase = entry.radius;
        spec.spot.minGoldRatio = entry.gold_ratio;
        spec.occludedByPlayer = entry.occluded_by_player;
        _iconTable.emplace(name, std::move(spec));
    }

    LogInfo << "WorldMap: icon table loaded" << VAR(MAA_NS::path_to_utf8_string(*file)) << VAR(_iconTable.size());
}

std::optional<IconSpec> WorldMapSolver::ResolveIcon(const std::string& name)
{
    std::lock_guard guard(_mutex);
    LoadIconTable();

    const auto it = _iconTable.find(name);
    if (it == _iconTable.end()) {
        LogError << "WorldMap: no such icon in the table" << VAR(name);
        return std::nullopt;
    }
    return it->second;
}

std::optional<SpotHit>
    WorldMapSolver::ConfirmSpot(const cv::Mat& screen, const cv::Point2d& expected, double viewportScale, const SpotConfig& cfg)
{
    if (screen.empty() || viewportScale <= 0.0 || cfg.templates.empty()) {
        return std::nullopt;
    }

    std::lock_guard guard(_mutex);
    const cv::Mat gray = ToGray(screen);
    const std::vector<double> ladder =
        LinearLadder(cfg.scaleMin, cfg.scaleMax, static_cast<int>(std::lround((cfg.scaleMax - cfg.scaleMin) / cfg.scaleStep)) + 1);

    struct Candidate
    {
        SpotHit hit;
        const IconTemplate* templ = nullptr;
        cv::Rect live;
    };

    // 在期望位置开个半径 radius 的窗口扫一遍，只出结果不做判定
    auto scan = [&](int radius) -> std::optional<Candidate> {
        cv::Rect window(
            static_cast<int>(std::lround(expected.x)) - radius,
            static_cast<int>(std::lround(expected.y)) - radius,
            radius * 2,
            radius * 2);
        window &= cv::Rect(0, 0, screen.cols, screen.rows);
        if (window.empty()) {
            LogWarn << "WorldMap: confirm window off screen" << VAR(expected.x) << VAR(expected.y);
            return std::nullopt;
        }

        const cv::Mat patch = gray(window);
        const IconTemplate* pick = nullptr;
        std::string pickName;
        std::optional<ScanHit> best;
        for (const std::string& name : cfg.templates) {
            const IconTemplate* templ = LoadIconTemplate(name);
            if (templ == nullptr) {
                continue;
            }
            if (window.width < templ->gray.cols || window.height < templ->gray.rows) {
                LogWarn << "WorldMap: confirm window smaller than template" << VAR(name) << VAR(window.width) << VAR(window.height);
                continue;
            }
            const auto hit = ScanIconScales(patch, templ->gray, templ->mask, ladder);
            if (hit && (!best || hit->score > best->score)) {
                best = hit;
                pick = templ;
                pickName = name;
            }
        }
        if (!best) {
            LogWarn << "WorldMap: no icon candidate in the confirm window" << VAR(radius);
            return std::nullopt;
        }
        if (best->score < cfg.minScore) {
            LogWarn << "WorldMap: icon score below floor" << VAR(pickName) << VAR(best->score) << VAR(cfg.minScore) << VAR(radius);
            return std::nullopt;
        }

        Candidate found;
        found.templ = pick;
        found.live = cv::Rect(
            window.x + static_cast<int>(std::lround(best->loc.x)),
            window.y + static_cast<int>(std::lround(best->loc.y)),
            best->size.width,
            best->size.height);
        found.hit.templateName = pickName;
        found.hit.center = cv::Point2d(window.x + best->loc.x + best->size.width / 2.0, window.y + best->loc.y + best->size.height / 2.0);
        found.hit.hotspot = found.hit.center + pick->hotspot * best->scale;
        found.hit.size = best->size;
        found.hit.score = best->score;
        found.hit.matchScale = best->scale;
        found.hit.offsetBase = std::hypot(found.hit.center.x - expected.x, found.hit.center.y - expected.y) * viewportScale;
        return found;
    };

    // 浮动的点位在一片范围里找，坐标只圈得住范围；钉死的点位就在期望位置开个小窗确认
    const bool floating = cfg.radiusBase > 0.0;
    const int radius =
        floating ? static_cast<int>(std::lround(cfg.radiusBase / viewportScale)) : std::max(cfg.radiusScreen, kMinTemplateSide);
    auto found = scan(radius);

    // 窗口比判定圈宽得多，里面坐着同类图标时取最高分未必取到期望的那个：
    // 实测过隔壁传送点只高 0.024 分就把正对着期望位置的那个挤掉了。
    // 判定圈外的先别扔，按判定圈的尺度再看一次近处——只多一次观测，近处没有就还用原来那个
    if (found && !floating && found->hit.offsetBase > cfg.gateBase) {
        const int tight = std::max(static_cast<int>(std::lround(cfg.gateBase / viewportScale)), kMinTemplateSide);
        if (tight < radius) {
            LogInfo << "WorldMap: nearest icon sits outside the gate, looking again closer in" << VAR(found->hit.offsetBase)
                    << VAR(cfg.gateBase) << VAR(radius) << VAR(tight);
            if (auto closer = scan(tight); closer && closer->hit.offsetBase < found->hit.offsetBase) {
                found = closer;
            }
        }
    }

    if (!found) {
        return std::nullopt;
    }

    SpotHit out = found->hit;
    // 钉死的点位偏得太远就是认错了；浮动的点位本来就该在范围里晃，窗口自己就是那道闸
    if (!floating && out.offsetBase > cfg.gateBase) {
        LogWarn << "WorldMap: icon too far from expected position" << VAR(out.templateName) << VAR(out.offsetBase) << VAR(cfg.gateBase)
                << VAR(out.center.x) << VAR(out.center.y) << VAR(expected.x) << VAR(expected.y);
        return std::nullopt;
    }

    if (cfg.minGoldRatio > 0.0) {
        out.goldRatio = GoldRatio(screen, found->templ->saturation, found->templ->mask, found->live, cfg.saturationFloor);
        out.unlocked = out.goldRatio >= cfg.minGoldRatio;
    }

    LogInfo << "WorldMap: icon confirmed" << VAR(out.templateName) << VAR(out.center.x) << VAR(out.center.y) << VAR(out.hotspot.x)
            << VAR(out.hotspot.y) << VAR(out.score) << VAR(out.matchScale) << VAR(out.offsetBase) << VAR(out.goldRatio)
            << VAR(out.unlocked);
    return out;
}

std::optional<PlayerMarkerHit>
    WorldMapSolver::DetectPlayerMarker(const cv::Mat& screen, const cv::Point2d& expected, const PlayerMarkerConfig& cfg)
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
        LogWarn << "WorldMap: player marker needs a colour image" << VAR(patch.channels());
        return std::nullopt;
    }

    cv::Mat mask;
    cv::inRange(patch, cv::Scalar(cfg.whiteFloor, cfg.whiteFloor, cfg.whiteFloor), cv::Scalar(255, 255, 255), mask);

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

const cv::Mat* WorldMapSolver::LoadZoneBase(const std::string& zone)
{
    if (const auto it = _zoneBases.find(zone); it != _zoneBases.end()) {
        return it->second.empty() ? nullptr : &it->second;
    }

    const auto zoneDir = mapnavigator::ResolveResourceImage(_imageRoots, fs::path("MapLocator") / MAA_NS::path(zone));
    const auto file = zoneDir ? FindZoneBaseFile(*zoneDir) : std::nullopt;
    if (!file) {
        LogError << "WorldMap: zone base image not found" << VAR(zone) << VAR(mapnavigator::DescribeRoots(_imageRoots));
        _zoneBases[zone] = cv::Mat();
        return nullptr;
    }

    cv::Mat image = MAA_NS::imread(*file, cv::IMREAD_UNCHANGED);
    if (image.empty()) {
        LogError << "WorldMap: failed to read zone base" << VAR(MAA_NS::path_to_utf8_string(*file));
        _zoneBases[zone] = cv::Mat();
        return nullptr;
    }

    LogInfo << "WorldMap: zone base loaded" << VAR(zone) << VAR(image.cols) << VAR(image.rows);
    auto [it, _] = _zoneBases.emplace(zone, BaseGray(image));
    return &it->second;
}

const WorldMapSolver::IconTemplate* WorldMapSolver::LoadIconTemplate(const std::string& name)
{
    const auto cached = _icons.find(name);
    if (cached != _icons.end()) {
        return cached->second.gray.empty() ? nullptr : &cached->second;
    }

    IconTemplate& slot = _icons[name];
    const auto file = mapnavigator::ResolveResourceImage(_imageRoots, fs::path(kIconDir) / MAA_NS::path(name));
    const cv::Mat image = file ? MAA_NS::imread(*file, cv::IMREAD_UNCHANGED) : cv::Mat();
    if (image.empty()) {
        LogError << "WorldMap: icon template not found" << VAR(name) << VAR(mapnavigator::DescribeRoots(_imageRoots));
        return nullptr;
    }

    slot.gray = ToGray(image);
    slot.saturation = SaturationOf(image);
    if (image.channels() == 4) {
        // alpha 就是掩膜：图标外那圈光晕在实拍里透着地形，让它参与相关只会压低分
        std::vector<cv::Mat> planes;
        cv::split(image, planes);
        slot.mask = planes[3] >= 128;
    }
    slot.hotspot = BodyHotspot(slot.mask, image.size());

    LogInfo << "WorldMap: icon template loaded" << VAR(name) << VAR(image.cols) << VAR(image.rows) << VAR(image.channels())
            << VAR(slot.hotspot.x) << VAR(slot.hotspot.y);
    return &slot;
}

bool WorldMapSolver::HasZone(const std::string& zone)
{
    std::lock_guard guard(_mutex);
    return LoadZoneBase(zone) != nullptr;
}

std::optional<Viewport> WorldMapSolver::SolveViewport(const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg)
{
    if (screen.empty()) {
        LogError << "WorldMap: empty screen image";
        return std::nullopt;
    }

    std::lock_guard guard(_mutex);
    const cv::Mat* base = LoadZoneBase(zone);
    if (base == nullptr) {
        return std::nullopt;
    }

    const std::optional<ScreenRoi> screenRoi = CropScreenRoi(screen, cfg);
    if (!screenRoi) {
        return std::nullopt;
    }
    const auto& [roiRect, roi, roiSmall] = *screenRoi;

    // 粗解：降采样后扫全尺度带，只为把细解的搜索窗放到正确位置
    const int down = std::max(1, cfg.coarseDownscale);
    cv::Mat baseSmall;
    cv::resize(*base, baseSmall, cv::Size(base->cols / down, base->rows / down), 0.0, 0.0, cv::INTER_AREA);

    return ScanViewport(ScanBase { .full = *base, .small = baseSmall }, roi, roiSmall, roiRect, cfg, zone);
}

const std::vector<TierPatch>& WorldMapSolver::LoadZoneTiers(const std::string& zone, const cv::Mat& base)
{
    if (const auto it = _zoneTiers.find(zone); it != _zoneTiers.end()) {
        return it->second;
    }
    std::vector<TierPatch>& tiers = _zoneTiers[zone];

    const auto zoneDir = mapnavigator::ResolveResourceImage(_imageRoots, fs::path("MapLocator") / MAA_NS::path(zone));
    std::vector<std::pair<std::string, fs::path>> files;
    std::error_code ec;
    for (const auto& entry : fs::directory_iterator(zoneDir.value_or(fs::path()), ec)) {
        if (std::string name = TierZoneName(zone, entry.path()); !name.empty()) {
            files.emplace_back(std::move(name), entry.path());
        }
    }
    const std::string navmeshZone = mapnavigator::InferBaseNavZone(zone, {});
    if (files.empty() || navmeshZone.empty()) {
        LogInfo << "WorldMap: zone has no tier images" << VAR(zone) << VAR(navmeshZone);
        return tiers;
    }
    const navmesh::BaseNavLoadResult loaded = navmesh::LoadBaseNavPack(_navmeshPack, navmeshZone);
    if (!loaded.pack) {
        LogError << "WorldMap: failed to read tier placements" << VAR(zone) << VAR(navmeshZone)
                 << VAR(MAA_NS::path_to_utf8_string(_navmeshPack)) << VAR(loaded.message);
        return tiers;
    }

    for (const auto& [name, file] : files) {
        const navmesh::BaseNavZone* placement = FindTierPlacement(loaded.pack->zones, name);
        if (placement == nullptr) {
            LogWarn << "WorldMap: tier image has no placement in the navmesh pack" << VAR(zone) << VAR(name);
            continue;
        }
        const cv::Mat image = MAA_NS::imread(file, cv::IMREAD_UNCHANGED);
        if (image.empty()) {
            LogError << "WorldMap: failed to read tier image" << VAR(MAA_NS::path_to_utf8_string(file));
            continue;
        }
        tiers.push_back(MakeTierPatch(name, image, *placement, base));
    }
    std::ranges::sort(tiers, {}, &TierPatch::name);

    LogInfo << "WorldMap: zone tiers loaded" << VAR(zone) << VAR(navmeshZone) << VAR(tiers.size());
    return tiers;
}

std::optional<Viewport>
    WorldMapSolver::SolveViewportOnTiers(const cv::Mat& screen, const std::string& zone, const ViewportConfig& cfg, const cv::Point2d& at)
{
    std::lock_guard guard(_mutex);
    const cv::Mat* base = LoadZoneBase(zone);
    if (base == nullptr) {
        return std::nullopt;
    }
    std::vector<const TierPatch*> tiers;
    for (const TierPatch& tier : LoadZoneTiers(zone, *base)) {
        if (cv::Rect2d(tier.area).contains(at)) {
            tiers.push_back(&tier);
        }
    }
    if (tiers.empty()) {
        return std::nullopt;
    }
    const std::optional<ScreenRoi> screenRoi = CropScreenRoi(screen, cfg);
    if (!screenRoi) {
        return std::nullopt;
    }
    const auto& [roiRect, roi, roiSmall] = *screenRoi;

    const int down = std::max(1, cfg.coarseDownscale);
    std::optional<Viewport> best;
    for (const TierPatch* tier : tiers) {
        ScanBase candidate { .full = tier->gray, .origin = tier->origin, .tier = tier->name };
        cv::resize(tier->gray, candidate.small, cv::Size(tier->gray.cols / down, tier->gray.rows / down), 0.0, 0.0, cv::INTER_AREA);
        LogInfo << "WorldMap: solving the viewport on the target's tier" << VAR(zone) << VAR(tier->name) << VAR(at.x) << VAR(at.y);
        auto vp = ScanViewport(candidate, roi, roiSmall, roiRect, cfg, zone);
        if (!vp) {
            continue;
        }
        if (best && std::hypot(vp->baseOrigin.x - best->baseOrigin.x, vp->baseOrigin.y - best->baseOrigin.y) > kTierAgreeBase) {
            LogWarn << "WorldMap: tiers disagree on the viewport" << VAR(zone) << VAR(best->tier) << VAR(best->baseOrigin.x)
                    << VAR(best->baseOrigin.y) << VAR(vp->tier) << VAR(vp->baseOrigin.x) << VAR(vp->baseOrigin.y);
            return std::nullopt;
        }
        if (!best || vp->score > best->score) {
            best = std::move(vp);
        }
    }
    return best;
}

WorldMapSolver& GetSolver(std::string_view controller_type)
{
    static WorldMapSolver solver(mapnavigator::ResourceImageRoots(controller_type), mapnavigator::ResolveNavmeshFilePath());
    return solver;
}

} // namespace worldmap
