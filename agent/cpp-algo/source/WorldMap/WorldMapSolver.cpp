#include "WorldMapSolver.h"

#include <algorithm>
#include <cmath>
#include <vector>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <meojson/json.hpp>

#include "../Common/JsoncFile.h"
#include "MapLocator/MatchStrategy.h"
#include "MapNavigator/controller_type_utils.h"
#include "utils.h"

namespace fs = std::filesystem;

namespace worldmap
{

namespace
{

constexpr int kMinTemplateSide = 24;
constexpr int kMinAnchorSide = 6;
constexpr const char* kIconDir = "SceneManager";

// 分块投票：块内像素几乎不起伏就给不出定位信息，归一化相关在这种块上只会放大噪声，
// 让它弃权。投票的块太少则中位数不再有多数可言，整档作废改由别的档说话
constexpr double kVoteBlockMinStdDev = 1.0;
constexpr size_t kVoteMinBlocks = 4;

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

// 把模板切成 grid×grid 块各自匹配，按块在模板内的偏移把各自的分数面平移到同一个窗口原点，
// 再逐像素取中位数。整窗匹配只出一个数，搜索窗里进了未探索迷雾这类局部遮挡就整帧塌掉；
// 分块之后被污染的块只是少数票，投不过其余的块
std::optional<maplocator::MatchResultRaw> VoteMatchPrepared(const cv::Mat& haystack, const cv::Mat& templ, int grid, int minBlockSide)
{
    if (haystack.cols < templ.cols || haystack.rows < templ.rows) {
        return std::nullopt;
    }
    if (templ.cols / grid < minBlockSide || templ.rows / grid < minBlockSide) {
        return std::nullopt;
    }

    const int spanX = haystack.cols - templ.cols + 1;
    const int spanY = haystack.rows - templ.rows + 1;

    std::vector<cv::Mat> votes;
    votes.reserve(static_cast<size_t>(grid) * grid);
    for (int iy = 0; iy < grid; ++iy) {
        for (int ix = 0; ix < grid; ++ix) {
            const int x0 = ix * templ.cols / grid;
            const int y0 = iy * templ.rows / grid;
            const cv::Rect cell(x0, y0, (ix + 1) * templ.cols / grid - x0, (iy + 1) * templ.rows / grid - y0);

            cv::Scalar mean, stddev;
            cv::meanStdDev(templ(cell), mean, stddev);
            if (stddev[0] < kVoteBlockMinStdDev) {
                continue;
            }

            cv::Mat scores;
            cv::matchTemplate(haystack, templ(cell), scores, cv::TM_CCOEFF_NORMED);
            cv::patchNaNs(scores, -1.0);
            // 块在模板里的偏移就是它的分数面要往回平移的量，平移完各块说的都是同一个窗口原点
            votes.push_back(scores(cv::Rect(x0, y0, spanX, spanY)).clone());
        }
    }

    if (votes.size() < kVoteMinBlocks) {
        return std::nullopt;
    }

    cv::Mat combined(spanY, spanX, CV_32FC1);
    std::vector<const float*> rows(votes.size());
    std::vector<float> bucket(votes.size());
    const size_t mid = bucket.size() / 2;
    for (int y = 0; y < spanY; ++y) {
        for (size_t i = 0; i < votes.size(); ++i) {
            rows[i] = votes[i].ptr<float>(y);
        }
        float* out = combined.ptr<float>(y);
        for (int x = 0; x < spanX; ++x) {
            for (size_t i = 0; i < rows.size(); ++i) {
                bucket[i] = rows[i][x];
            }
            std::nth_element(bucket.begin(), bucket.begin() + mid, bucket.end());
            // 块数为偶数时取中间两个的平均，与离线验证时的口径一致
            out[x] = bucket.size() % 2 != 0 ? bucket[mid] : (bucket[mid] + *std::max_element(bucket.begin(), bucket.begin() + mid)) / 2.0f;
        }
    }

    double peak = 0.0;
    cv::Point peakLoc;
    cv::minMaxLoc(combined, nullptr, &peak, nullptr, &peakLoc);

    // 峰值旁瓣比与整窗那条路径同口径：屏蔽主峰后拿剩下的均值方差算
    const int ex = std::max(3, std::min(templ.cols, templ.rows) / 10);
    cv::Rect peakRect(peakLoc.x - ex, peakLoc.y - ex, ex * 2 + 1, ex * 2 + 1);
    peakRect &= cv::Rect(0, 0, combined.cols, combined.rows);
    cv::Mat sidelobe(combined.size(), CV_8UC1, cv::Scalar(1));
    sidelobe(peakRect).setTo(0);
    cv::Scalar mean, stddev;
    cv::meanStdDev(combined, mean, stddev, sidelobe);

    maplocator::MatchResultRaw out;
    out.score = peak;
    // 中位数面不是相关面，抛物线外插那套在它上面没有依据。取整这一项不到半个底图像素，
    // 匹配本身偏多少另算，最终由认图标那道 10 像素的判定圈把关
    out.loc = cv::Point2d(peakLoc.x, peakLoc.y);
    out.psr = (peak - mean[0]) / (stddev[0] + 1e-6);
    return out;
}

// 在 haystack 上按给定尺度逐档缩放 needle 匹配，取分最高的一档。
// slack 是模板与搜索图的最小尺寸差：贴得太满时可落位置太少，分数会虚高。
// needleMask 空则整块模板等权。voteGrid 大于 1 时改走分块投票，那条路径不吃 needleMask
std::optional<ScanHit> ScanScales(
    const cv::Mat& haystack,
    const cv::Mat& needle,
    const cv::Mat& needleMask,
    const std::vector<double>& scales,
    int minSide,
    int slack,
    maplocator::PeakRefineMode refineMode,
    int voteGrid = 1,
    int voteMinBlockSide = 0,
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

        std::optional<maplocator::MatchResultRaw> hit;
        if (voteGrid > 1) {
            hit = VoteMatchPrepared(prepared.image, scaled, voteGrid, voteMinBlockSide);
        }
        else {
            cv::Mat mask(scaled.size(), CV_8UC1, cv::Scalar(255));
            if (!needleMask.empty()) {
                // 掩膜是二值的，插值会在边界上造出中间值，只能用最近邻
                cv::resize(needleMask, mask, scaled.size(), 0.0, 0.0, cv::INTER_NEAREST);
            }
            hit = maplocator::CoreMatchPrepared(prepared, scaled, mask, refineMode);
        }
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

// 粗解在降采样图上定位置，细解回到原尺度定尺度，再拿粗解各档判这个解唯不唯一。
// voteGrid 为 1 走整窗匹配，大于 1 则两级都改用分块投票
std::optional<Viewport> ScanViewport(
    const cv::Mat& base,
    const cv::Mat& baseSmall,
    const cv::Mat& roi,
    const cv::Mat& roiSmall,
    const cv::Rect& roiRect,
    const ViewportConfig& cfg,
    int voteGrid,
    const std::string& zone)
{
    const int down = std::max(1, cfg.coarseDownscale);

    std::vector<ScanRung> coarseRungs;
    const auto coarse = ScanScales(
        baseSmall,
        roiSmall,
        cv::Mat(),
        GeometricLadder(cfg.scaleMin, cfg.scaleMax, cfg.coarseRatio),
        kMinTemplateSide,
        std::max(1, static_cast<int>(std::lround(cfg.scanSlack / static_cast<double>(down)))),
        maplocator::PeakRefineMode::Parabola,
        voteGrid,
        // 粗解在降采样图上跑，块的边长下界也得跟着折算，否则低倍率档会被整档筛掉
        std::max(1, static_cast<int>(std::lround(cfg.voteMinBlockSide / static_cast<double>(down)))),
        &coarseRungs);
    if (!coarse) {
        LogWarn << "WorldMap: coarse viewport scan found nothing" << VAR(zone) << VAR(voteGrid);
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
        std::min(static_cast<int>(std::lround(roi.cols * (coarse->scale + span))) + pad * 2, base.cols - windowX),
        std::min(static_cast<int>(std::lround(roi.rows * (coarse->scale + span))) + pad * 2, base.rows - windowY));
    if (window.width < kMinTemplateSide || window.height < kMinTemplateSide) {
        LogWarn << "WorldMap: fine window degenerated" << VAR(window.width) << VAR(window.height) << VAR(voteGrid);
        return std::nullopt;
    }

    // 细解窗口是照模板尺寸开的，本来就贴边，可落位置已由粗解锚定，不需要再留余量
    const auto fine = ScanScales(
        base(window),
        roi,
        cv::Mat(),
        LinearLadder(std::max(cfg.scaleMin, coarse->scale - span), coarse->scale + span, cfg.fineSteps),
        kMinTemplateSide,
        0,
        maplocator::PeakRefineMode::Continuous,
        voteGrid,
        cfg.voteMinBlockSide);
    if (!fine) {
        LogWarn << "WorldMap: fine viewport scan found nothing" << VAR(zone) << VAR(voteGrid);
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
        LogWarn << "WorldMap: viewport rejected" << VAR(zone) << VAR(fine->score) << VAR(delta) << VAR(fine->psr) << VAR(fine->scale)
                << VAR(cfg.minScore) << VAR(cfg.minDelta) << VAR(voteGrid);
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
    vp.voteGrid = voteGrid;

    LogInfo << "WorldMap: viewport solved" << VAR(zone) << VAR(vp.scale) << VAR(vp.baseOrigin.x) << VAR(vp.baseOrigin.y) << VAR(vp.score)
            << VAR(vp.delta) << VAR(vp.psr) << VAR(vp.voteGrid);
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

WorldMapSolver::WorldMapSolver(std::vector<fs::path> imageRoots)
    : _imageRoots(std::move(imageRoots))
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
            // 图标只有二十来像素宽，最小边长得按它来卡，照视口那套会把整个模板筛掉
            const auto hit = ScanScales(patch, templ->gray, templ->mask, ladder, kMinAnchorSide, 0, maplocator::PeakRefineMode::Continuous);
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
    auto [it, _] = _zoneBases.emplace(zone, ToGray(image));
    return &it->second;
}

const WorldMapSolver::IconTemplate* WorldMapSolver::LoadIconTemplate(const std::string& name)
{
    const auto cached = _icons.find(name);
    if (cached != _icons.end()) {
        return cached->second.gray.empty() ? nullptr : &cached->second;
    }

    IconTemplate& slot = _icons[name];
    const auto file = mapnavigator::ResolveResourceImage(_imageRoots, fs::path(kIconDir) / name);
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

    const cv::Rect roiRect = SafeArea(screen.size(), cfg.roi, 0);
    if (roiRect.width < kMinTemplateSide || roiRect.height < kMinTemplateSide) {
        LogError << "WorldMap: screen roi too small" << VAR(roiRect.width) << VAR(roiRect.height);
        return std::nullopt;
    }
    const cv::Mat roi = ToGray(screen)(roiRect);

    // 粗解：降采样后扫全尺度带，只为把细解的搜索窗放到正确位置
    const int down = std::max(1, cfg.coarseDownscale);
    cv::Mat baseSmall;
    cv::Mat roiSmall;
    cv::resize(*base, baseSmall, cv::Size(base->cols / down, base->rows / down), 0.0, 0.0, cv::INTER_AREA);
    cv::resize(roi, roiSmall, cv::Size(roi.cols / down, roi.rows / down), 0.0, 0.0, cv::INTER_AREA);

    if (auto vp = ScanViewport(*base, baseSmall, roi, roiSmall, roiRect, cfg, 1, zone)) {
        return vp;
    }

    if (cfg.voteGrid <= 1) {
        return std::nullopt;
    }

    // 整窗匹配只出一个数，搜索窗里进了未探索迷雾这类局部遮挡就整帧塌掉。分块投票能把
    // 被污染的块变成少数票，代价是多跑一遍，所以只在整窗已经判失败时才付这笔钱
    LogInfo << "WorldMap: retrying the viewport with block voting" << VAR(zone) << VAR(cfg.voteGrid);
    return ScanViewport(*base, baseSmall, roi, roiSmall, roiRect, cfg, cfg.voteGrid, zone);
}

WorldMapSolver& GetSolver(std::string_view controller_type)
{
    static WorldMapSolver solver(mapnavigator::ResourceImageRoots(controller_type));
    return solver;
}

} // namespace worldmap
