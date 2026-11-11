#include <algorithm>
#include <cmath>
#include <filesystem>
#include <optional>

#include <MaaUtils/Logger.h>

#include "MapAlgorithm.h"
#include "MatchStrategy.h"

namespace fs = std::filesystem;

namespace maplocator
{

namespace
{

cv::Mat extract_path_heatmap_feature(const cv::Mat& src)
{
    cv::Mat bgr, alpha;
    if (src.channels() == 4) {
        cv::Mat bgra[4];
        cv::split(src, bgra);
        cv::Mat channels[3] = { bgra[0], bgra[1], bgra[2] };
        cv::merge(channels, 3, bgr);
        alpha = bgra[3];
    }
    else {
        bgr = src;
    }

    cv::Mat feature = cv::Mat::zeros(bgr.size(), CV_8UC1);

    // 游戏内路面标准色 (浅灰偏蓝), 可根据实际采样微调
    const int targetB = 237, targetG = 233, targetR = 228;
    const int maxDist = 60; // 容差范围

    for (int y = 0; y < bgr.rows; ++y) {
        const cv::Vec3b* bgr_row = bgr.ptr<cv::Vec3b>(y);
        const uchar* alpha_row = alpha.empty() ? nullptr : alpha.ptr<uchar>(y);
        uchar* feat_row = feature.ptr<uchar>(y);

        for (int x = 0; x < bgr.cols; ++x) {
            if (alpha_row && alpha_row[x] < 128) {
                continue;
            }

            int b = bgr_row[x][0];
            int g = bgr_row[x][1];
            int r = bgr_row[x][2];

            // 计算曼哈顿颜色距离，代替欧式距离加速。曼哈顿在此场景下足以区分特定的青灰路面与外围复杂的带彩背景
            int dist = std::abs(b - targetB) + std::abs(g - targetG) + std::abs(r - targetR);

            // 距离越近目标色，像素越亮，构建连续梯度热力图。暗部边缘和差异过大的游戏造景会自动归零，将彩图简化为拓扑路网结构
            if (dist < maxDist * 3) {
                feat_row[x] = static_cast<uchar>(std::max(0, 255 - (dist * 255 / (maxDist * 3))));
            }
        }
    }

    // 适度高斯模糊，为 NCC 优化器提供平滑的梯度下降盆地
    cv::GaussianBlur(feature, feature, cv::Size(5, 5), 0);
    return feature;
}

double refine_peak_offset(float prev, float center, float next)
{
    const double denom = static_cast<double>(prev) - 2.0 * static_cast<double>(center) + static_cast<double>(next);
    if (std::abs(denom) < 1e-12) {
        return 0.0;
    }

    const double offset = 0.5 * (static_cast<double>(prev) - static_cast<double>(next)) / denom;
    return std::clamp(offset, -0.5, 0.5);
}

cv::Point2d refine_peak_subpixel(const cv::Mat& result, const cv::Point& maxLoc)
{
    CV_Assert(result.type() == CV_32F);
    CV_Assert(maxLoc.x >= 0 && maxLoc.x < result.cols && maxLoc.y >= 0 && maxLoc.y < result.rows);

    cv::Point2d refined(static_cast<double>(maxLoc.x), static_cast<double>(maxLoc.y));
    const float center = result.at<float>(maxLoc.y, maxLoc.x);

    if (maxLoc.x > 0 && maxLoc.x + 1 < result.cols) {
        refined.x += refine_peak_offset(result.at<float>(maxLoc.y, maxLoc.x - 1), center, result.at<float>(maxLoc.y, maxLoc.x + 1));
    }
    if (maxLoc.y > 0 && maxLoc.y + 1 < result.rows) {
        refined.y += refine_peak_offset(result.at<float>(maxLoc.y - 1, maxLoc.x), center, result.at<float>(maxLoc.y + 1, maxLoc.x));
    }

    return refined;
}

constexpr double kRefineMaxOffset = 1.0; // 相对整点峰的最大位移(px)
constexpr int kRefineMaxEvals = 40;
constexpr int kRefinePatchMargin = 3;    // 双三次插值取样需要的余量

struct RefinedPeak
{
    cv::Point2d loc;
    double score = -1.0;
};

// 在整点峰周围连续求极大。目标函数与 TM_CCOEFF_NORMED 同式（整点处两者逐位相同），
// 区别是底图按亚像素位置重采样，因此不受相关面格点的限制
std::optional<RefinedPeak> refine_peak_continuous(
    const cv::Mat& searchImage,
    const cv::Mat& templ,
    const cv::Mat& weightMask,
    const cv::Point& peak,
    const cv::Point2d& seed)
{
    cv::Rect patchRect(
        peak.x - kRefinePatchMargin,
        peak.y - kRefinePatchMargin,
        templ.cols + kRefinePatchMargin * 2,
        templ.rows + kRefinePatchMargin * 2);
    patchRect &= cv::Rect(0, 0, searchImage.cols, searchImage.rows);
    if (patchRect.width < templ.cols || patchRect.height < templ.rows) {
        return std::nullopt;
    }

    cv::Mat weight;
    weightMask.convertTo(weight, CV_32F, 1.0 / 255.0);
    const double weightSum = cv::sum(weight)[0];
    if (weightSum <= 0.0) {
        return std::nullopt;
    }

    cv::Mat patch;
    searchImage(patchRect).convertTo(patch, CV_32F);
    cv::Mat templF;
    templ.convertTo(templF, CV_32F);
    const cv::Mat templDev = (templF - cv::Scalar(cv::sum(templF.mul(weight))[0] / weightSum)).mul(weight);
    const double templNorm = std::sqrt(templDev.dot(templDev));
    if (templNorm <= 0.0) {
        return std::nullopt;
    }

    cv::Mat mapX(templ.size(), CV_32F);
    cv::Mat mapY(templ.size(), CV_32F);
    for (int y = 0; y < templ.rows; ++y) {
        float* rowX = mapX.ptr<float>(y);
        float* rowY = mapY.ptr<float>(y);
        for (int x = 0; x < templ.cols; ++x) {
            rowX[x] = static_cast<float>(peak.x - patchRect.x + x);
            rowY[x] = static_cast<float>(peak.y - patchRect.y + y);
        }
    }

    cv::Mat shiftedX;
    cv::Mat shiftedY;
    cv::Mat sampled;
    auto evaluate = [&](double tx, double ty) {
        cv::add(mapX, cv::Scalar(tx), shiftedX);
        cv::add(mapY, cv::Scalar(ty), shiftedY);
        cv::remap(patch, sampled, shiftedX, shiftedY, cv::INTER_CUBIC, cv::BORDER_REPLICATE);
        const cv::Mat dev = (sampled - cv::Scalar(cv::sum(sampled.mul(weight))[0] / weightSum)).mul(weight);
        return templDev.dot(dev) / (templNorm * std::sqrt(dev.dot(dev)) + 1e-12);
    };

    // 模式搜索：沿坐标轴试探，一轮都没走动就把步长减半
    double tx = std::clamp(seed.x - peak.x, -kRefineMaxOffset, kRefineMaxOffset);
    double ty = std::clamp(seed.y - peak.y, -kRefineMaxOffset, kRefineMaxOffset);
    double best = evaluate(tx, ty);
    double step = 0.25;
    int evals = 1;

    while (step > 0.01 && evals < kRefineMaxEvals) {
        bool moved = false;
        for (int axis = 0; axis < 2 && !moved; ++axis) {
            for (const double sign : { 1.0, -1.0 }) {
                double nextX = tx;
                double nextY = ty;
                (axis == 0 ? nextX : nextY) += sign * step;
                if (std::abs(nextX) > kRefineMaxOffset || std::abs(nextY) > kRefineMaxOffset) {
                    continue;
                }

                ++evals;
                const double value = evaluate(nextX, nextY);
                if (value > best) {
                    best = value;
                    tx = nextX;
                    ty = nextY;
                    moved = true;
                    break;
                }
            }
        }

        if (!moved) {
            step *= 0.5;
        }
    }

    return RefinedPeak { .loc = cv::Point2d(peak.x + tx, peak.y + ty), .score = best };
}

MatchFeature standard_extract_template_feature(const MatchStrategy& strategy, const cv::Mat& minimap)
{
    MatchFeature feat;
    if (minimap.channels() == 4) {
        cv::cvtColor(minimap, feat.templ_raw, cv::COLOR_BGRA2BGR);

        cv::Mat bgra[4];
        cv::split(minimap, bgra);
        cv::Mat alpha = bgra[3];
        cv::Mat mask = (alpha >= 220); // 取有效区域

        // 防止边界泄漏 腐蚀一下
        cv::erode(mask, mask, cv::getStructuringElement(cv::MORPH_ELLIPSE, cv::Size(3, 3)));

        cv::Mat templGray;
        cv::cvtColor(feat.templ_raw, templGray, cv::COLOR_BGR2GRAY);

        // 均值外推填补：由于原图周围是被抠空的透明区，在 NCC 基于均值的滑动互相关中，
        // 实体与虚无的黑边界线会产生极其强烈的“虚假强梯度”，导致系统倾向于匹配这种假边缘而非内部细节。
        // 提取有效遮罩均值往外灌注，彻底消融边缘轮廓造成的误诱导。
        cv::Scalar meanVScalar = cv::mean(templGray, mask);
        double meanV = meanVScalar[0];
        cv::Mat inv;
        cv::bitwise_not(mask, inv);
        cv::Mat templGrayFilled = templGray.clone();
        templGrayFilled.setTo(meanV, inv);
        feat.image = LocalContrastNormalize(templGrayFilled);

        feat.mask = MinimapRingMask(minimap.size(), strategy.is_base ? strategy.base_image : strategy.tier_image);
        cv::bitwise_and(feat.mask, mask, feat.mask);
    }
    else {
        feat.templ_raw = minimap.clone();
        cv::Mat templGray;
        cv::cvtColor(minimap, templGray, cv::COLOR_BGR2GRAY);
        feat.image = LocalContrastNormalize(templGray);
        feat.mask = MinimapRingMask(minimap.size(), strategy.is_base ? strategy.base_image : strategy.tier_image);
    }
    return feat;
}

MatchFeature heatmap_extract_template_feature(const MatchStrategy& strategy, const cv::Mat& minimap)
{
    MatchFeature feat;
    if (minimap.channels() == 4) {
        cv::cvtColor(minimap, feat.templ_raw, cv::COLOR_BGRA2BGR);
    }
    else {
        feat.templ_raw = minimap.clone();
    }

    feat.image = extract_path_heatmap_feature(minimap); // 梯度图
    feat.mask = GenerateMinimapMask(feat.templ_raw, strategy.is_base ? strategy.base_image : strategy.tier_image);
    return feat;
}

MatchFeature standard_extract_search_feature(const cv::Mat& mapRoi)
{
    MatchFeature feat;
    cv::Mat gray;
    cv::cvtColor(mapRoi, gray, mapRoi.channels() == 4 ? cv::COLOR_BGRA2GRAY : cv::COLOR_BGR2GRAY);
    feat.image = LocalContrastNormalize(gray);
    return feat;
}

MatchFeature heatmap_extract_search_feature(const cv::Mat& mapRoi)
{
    MatchFeature feat;
    feat.image = extract_path_heatmap_feature(mapRoi);
    return feat;
}

TemplateFeatureKind standard_template_kind(const MatchStrategy& strategy)
{
    return strategy.is_base ? TemplateFeatureKind::StandardBase : TemplateFeatureKind::StandardTier;
}

TemplateFeatureKind heatmap_template_kind(const MatchStrategy& strategy)
{
    return strategy.is_base ? TemplateFeatureKind::PathHeatmapBase : TemplateFeatureKind::PathHeatmapTier;
}

TrackingValidation standard_validate_tracking(
    const MatchStrategy& strategy,
    const MatchResultRaw& trackResult,
    std::chrono::duration<double> dt,
    std::optional<MapPosition> lastPos,
    const cv::Rect& searchRect,
    int templCols,
    int templRows)
{
    TrackingValidation v;

    // 边缘吸附
    int maxX = searchRect.width - templCols;
    int maxY = searchRect.height - templRows;
    bool hitEdgeX =
        (trackResult.loc.x <= strategy.tracking.edge_snap_margin || trackResult.loc.x >= maxX - strategy.tracking.edge_snap_margin);
    bool hitEdgeY =
        (trackResult.loc.y <= strategy.tracking.edge_snap_margin || trackResult.loc.y >= maxY - strategy.tracking.edge_snap_margin);
    v.is_edge_snapped = hitEdgeX || hitEdgeY;

    v.abs_x = (double)searchRect.x + trackResult.loc.x + templCols / 2.0;
    v.abs_y = (double)searchRect.y + trackResult.loc.y + templRows / 2.0;

    double currentSpeed = 0.0;
    if (lastPos) {
        double dx = v.abs_x - lastPos->x;
        double dy = v.abs_y - lastPos->y;
        double distanceMoved = std::sqrt(dx * dx + dy * dy);
        double dtSec = dt.count();
        if (dtSec < 0.001) {
            dtSec = 0.001;
        }
        currentSpeed = distanceMoved / dtSec;
    }
    // 突变防御：若帧间换算速度超过限制，则认为是发生了传送或视角剧变，需打断追踪状态强制重搜
    v.is_teleported = currentSpeed > strategy.tracking.max_normal_speed;

    // 分数硬下限：低于 kTrackingHardScoreFloor 时无论 psr/delta 如何均判为 ambiguous，走 hold 路径
    const bool belowHardFloor = trackResult.score < kTrackingHardScoreFloor;
    bool lowScore = trackResult.score < 0.71;
    bool ambiguous = belowHardFloor || (lowScore && (trackResult.psr < 6.0 || trackResult.delta < 0.02));
    v.is_screen_blocked = trackResult.score < strategy.tracking.screen_blocked_threshold;

    v.is_valid = !v.is_edge_snapped && !v.is_teleported && !v.is_screen_blocked && !ambiguous;

    return v;
}

TrackingValidation heatmap_validate_tracking(
    const MatchStrategy& strategy,
    const MatchResultRaw& trackResult,
    std::chrono::duration<double> dt,
    std::optional<MapPosition> lastPos,
    const cv::Rect& searchRect,
    int templCols,
    int templRows)
{
    TrackingValidation v;

    int maxX = searchRect.width - templCols;
    int maxY = searchRect.height - templRows;
    bool hitEdgeX =
        (trackResult.loc.x <= strategy.tracking.edge_snap_margin || trackResult.loc.x >= maxX - strategy.tracking.edge_snap_margin);
    bool hitEdgeY =
        (trackResult.loc.y <= strategy.tracking.edge_snap_margin || trackResult.loc.y >= maxY - strategy.tracking.edge_snap_margin);
    v.is_edge_snapped = hitEdgeX || hitEdgeY;

    v.abs_x = (double)searchRect.x + trackResult.loc.x + templCols / 2.0;
    v.abs_y = (double)searchRect.y + trackResult.loc.y + templRows / 2.0;

    double currentSpeed = 0.0;
    if (lastPos) {
        double dx = v.abs_x - lastPos->x;
        double dy = v.abs_y - lastPos->y;
        double distanceMoved = std::sqrt(dx * dx + dy * dy);
        double dtSec = dt.count();
        if (dtSec < 0.001) {
            dtSec = 0.001;
        }
        currentSpeed = distanceMoved / dtSec;
    }
    v.is_teleported = currentSpeed > strategy.tracking.max_normal_speed;

    // 追踪态高分豁免
    bool accept = (trackResult.score >= 0.85) || (trackResult.score >= 0.70 && trackResult.delta >= 0.25 && trackResult.psr >= 2.0)
                  || (trackResult.score >= 0.42 && trackResult.delta >= 0.04 && trackResult.psr >= 3.8)
                  || (trackResult.score >= 0.40 && trackResult.delta >= 0.05 && trackResult.psr >= 3.8);
    bool hold = (trackResult.score >= 0.70 && trackResult.delta >= 0.25 && trackResult.psr >= 2.0)
                || (trackResult.score >= 0.35 && trackResult.psr >= 4.0);

    bool ambiguous = !accept;
    v.is_screen_blocked = !accept && !hold;

    v.is_valid = !v.is_edge_snapped && !v.is_teleported && !v.is_screen_blocked && !ambiguous;

    return v;
}

} // namespace

PreparedSearchFeature PrepareSearchFeature(const cv::Mat& searchImgRaw)
{
    // 搜索图与模板必须经过同样的处理：任何只作用在一侧的低通都会让匹配分偏向被低通的那一侧
    PreparedSearchFeature prepared;
    if (searchImgRaw.channels() == 4) {
        cv::cvtColor(searchImgRaw, prepared.image, cv::COLOR_BGRA2GRAY);
    }
    else if (searchImgRaw.channels() == 3) {
        cv::cvtColor(searchImgRaw, prepared.image, cv::COLOR_BGR2GRAY);
    }
    else {
        prepared.image = searchImgRaw.clone();
    }

    return prepared;
}

std::optional<MatchResultRaw> CoreMatchPrepared(
    const PreparedSearchFeature& searchFeature,
    const cv::Mat& templRaw,
    const cv::Mat& weightMask,
    PeakRefineMode refineMode)
{
    if (searchFeature.image.rows < templRaw.rows || searchFeature.image.cols < templRaw.cols) {
        return std::nullopt;
    }

    cv::Mat templ;
    if (templRaw.channels() == 4) {
        cv::cvtColor(templRaw, templ, cv::COLOR_BGRA2GRAY);
    }
    else if (templRaw.channels() == 3) {
        cv::cvtColor(templRaw, templ, cv::COLOR_BGR2GRAY);
    }
    else {
        templ = templRaw.clone();
    }

    cv::Mat result;
    try {
        const int weighted = cv::countNonZero(weightMask);
        if (weighted < 5) {
            return std::nullopt;
        }

        // 带掩膜的实现在平坦窗口上会冒假峰，全 255 的掩膜改走等价的无掩膜实现
        if (weighted == static_cast<int>(weightMask.total())) {
            cv::matchTemplate(searchFeature.image, templ, result, cv::TM_CCOEFF_NORMED);
        }
        else {
            cv::matchTemplate(searchFeature.image, templ, result, cv::TM_CCOEFF_NORMED, weightMask);
        }

        cv::patchNaNs(result, -1.0);
        for (int y = 0; y < result.rows; ++y) {
            float* row = result.ptr<float>(y);
            for (int x = 0; x < result.cols; ++x) {
                if (!std::isfinite(row[x])) {
                    row[x] = -1.0f;
                }
            }
        }
    }
    catch (cv::Exception& e) {
        LogError << "[CoreMatch] OpenCV Error: " << std::string(e.what());
        return std::nullopt;
    }

    double minVal, maxVal;
    cv::Point minLoc, maxLoc;
    cv::minMaxLoc(result, &minVal, &maxVal, &minLoc, &maxLoc);

    cv::Point2d refinedLoc = refine_peak_subpixel(result, maxLoc);
    double peakScore = maxVal;

    if (refineMode == PeakRefineMode::Continuous) {
        if (const auto refined = refine_peak_continuous(searchFeature.image, templ, weightMask, maxLoc, refinedLoc)) {
            refinedLoc = refined->loc;
            peakScore = refined->score;
        }
    }

    int ex = std::max(3, std::min(templ.cols, templ.rows) / 10);
    cv::Rect peakRect(maxLoc.x - ex, maxLoc.y - ex, ex * 2 + 1, ex * 2 + 1);
    peakRect &= cv::Rect(0, 0, result.cols, result.rows);

    thread_local cv::Mat peakBackupCache;
    if (peakBackupCache.rows < peakRect.height || peakBackupCache.cols < peakRect.width || peakBackupCache.type() != result.type()) {
        peakBackupCache.create(peakRect.size(), result.type());
    }
    cv::Mat peakBackup = peakBackupCache(cv::Rect(0, 0, peakRect.width, peakRect.height));
    result(peakRect).copyTo(peakBackup);

    // 找次极大值：在最高分周围开辟“黑洞”区域屏蔽主峰，随后寻找全局次高分，用于计算 PSR 与 delta，判断周遭是否存在相似纹理导致匹配歧义
    result(peakRect).setTo(-2.0f);
    double secondVal;
    cv::Point secondLoc;
    cv::minMaxLoc(result, nullptr, &secondVal, nullptr, &secondLoc);
    peakBackup.copyTo(result(peakRect));

    thread_local cv::Mat sideMaskCache;
    if (sideMaskCache.size() != result.size() || sideMaskCache.type() != CV_8U) {
        sideMaskCache.create(result.size(), CV_8U);
    }
    sideMaskCache.setTo(static_cast<uchar>(1));
    sideMaskCache(peakRect).setTo(static_cast<uchar>(0));
    cv::Scalar mean, stddev;
    cv::meanStdDev(result, mean, stddev, sideMaskCache);
    // PSR (Peak to Sidelobe Ratio) 峰值旁瓣比：衡量最高分是否唯一。
    // 若匹配区域纹理单一或是重复铺装路面，旁瓣(周围区域)得分也会很高，导致 PSR 骤降，借此拒绝高分歧义解
    double psr = (maxVal - mean[0]) / (stddev[0] + 1e-6);

    MatchResultRaw out;
    out.score = peakScore;
    out.loc = refinedLoc;
    out.second_score = secondVal;
    out.delta = peakScore - secondVal;
    out.psr = psr;

    return out;
}

double DeliveredScore(TemplateFeatureKind kind, const MatchResultRaw& window, const MatchResultRaw& refined)
{
    if (kind == TemplateFeatureKind::PathHeatmapBase || kind == TemplateFeatureKind::PathHeatmapTier) {
        return refined.score;
    }
    if (window.score <= 0.0) {
        return 0.0;
    }
    return 1.0 - std::max(window.second_score, 0.0) / window.score;
}

std::optional<MatchResultRaw> CoreMatch(const cv::Mat& searchImgRaw, const cv::Mat& templRaw, const cv::Mat& weightMask)
{
    if (searchImgRaw.rows < templRaw.rows || searchImgRaw.cols < templRaw.cols) {
        return std::nullopt;
    }

    return CoreMatchPrepared(PrepareSearchFeature(searchImgRaw), templRaw, weightMask);
}

MatchStrategy MakeMatchStrategy(
    const std::string& zoneId,
    const TrackingConfig& trackingCfg,
    const MatchConfig& matchCfg,
    const ImageProcessingConfig& baseImgCfg,
    const ImageProcessingConfig& tierImgCfg)
{
    bool isBase = (zoneId.find("Base") != std::string::npos);
    return MatchStrategy {
        .kind = IsPathHeatmapZone(zoneId) ? MatchStrategyKind::PathHeatmap : MatchStrategyKind::Standard,
        .is_base = isBase,
        .tracking = trackingCfg,
        .match = matchCfg,
        .base_image = baseImgCfg,
        .tier_image = tierImgCfg,
    };
}

MatchFeature ExtractTemplateFeature(const MatchStrategy& strategy, const cv::Mat& minimap)
{
    switch (strategy.kind) {
    case MatchStrategyKind::Standard:
        return standard_extract_template_feature(strategy, minimap);
    case MatchStrategyKind::PathHeatmap:
        return heatmap_extract_template_feature(strategy, minimap);
    }
    return standard_extract_template_feature(strategy, minimap);
}

MatchFeature ExtractSearchFeature(const MatchStrategy& strategy, const cv::Mat& mapRoi)
{
    switch (strategy.kind) {
    case MatchStrategyKind::Standard:
        return standard_extract_search_feature(mapRoi);
    case MatchStrategyKind::PathHeatmap:
        return heatmap_extract_search_feature(mapRoi);
    }
    return standard_extract_search_feature(mapRoi);
}

TemplateFeatureKind TemplateKindOf(const MatchStrategy& strategy)
{
    switch (strategy.kind) {
    case MatchStrategyKind::Standard:
        return standard_template_kind(strategy);
    case MatchStrategyKind::PathHeatmap:
        return heatmap_template_kind(strategy);
    }
    return standard_template_kind(strategy);
}

TrackingValidation ValidateTracking(
    const MatchStrategy& strategy,
    const MatchResultRaw& trackResult,
    std::chrono::duration<double> dt,
    std::optional<MapPosition> lastPos,
    const cv::Rect& searchRect,
    int templCols,
    int templRows)
{
    switch (strategy.kind) {
    case MatchStrategyKind::Standard:
        return standard_validate_tracking(strategy, trackResult, dt, lastPos, searchRect, templCols, templRows);
    case MatchStrategyKind::PathHeatmap:
        return heatmap_validate_tracking(strategy, trackResult, dt, lastPos, searchRect, templCols, templRows);
    }
    return standard_validate_tracking(strategy, trackResult, dt, lastPos, searchRect, templCols, templRows);
}

bool ValidateGlobalSearch(const MatchStrategy& strategy, double score)
{
    return score >= strategy.match.pass_threshold;
}

bool NeedsChamferCompensation(const MatchStrategy& strategy)
{
    switch (strategy.kind) {
    case MatchStrategyKind::Standard:
        return false;
    case MatchStrategyKind::PathHeatmap:
        return true;
    }
    return false;
}

} // namespace maplocator
