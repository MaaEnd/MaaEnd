#include "MatchStrategy.h"
#include "MapAlgorithm.h"
#include <MaaUtils/Logger.h>
#include <filesystem>

namespace fs = std::filesystem;

namespace maplocator {

static cv::Mat ExtractPathHeatmapFeature(const cv::Mat& src) {
    cv::Mat bgr, alpha;
    if (src.channels() == 4) {
        cv::Mat bgra[4];
        cv::split(src, bgra);
        cv::Mat channels[3] = {bgra[0], bgra[1], bgra[2]};
        cv::merge(channels, 3, bgr);
        alpha = bgra[3];
    } else {
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
            if (alpha_row && alpha_row[x] < 128) continue;
            
            int b = bgr_row[x][0];
            int g = bgr_row[x][1];
            int r = bgr_row[x][2];
            
            // 计算曼哈顿颜色距离
            int dist = std::abs(b - targetB) + std::abs(g - targetG) + std::abs(r - targetR);
            
            // 距离越近越亮，生成热力图。暗部边缘和彩色游戏背景会被直接抹零
            if (dist < maxDist * 3) {
                feat_row[x] = static_cast<uchar>(std::max(0, 255 - (dist * 255 / (maxDist * 3))));
            }
        }
    }
    
    // 适度高斯模糊，为 NCC 优化器提供平滑的梯度下降盆地
    cv::GaussianBlur(feature, feature, cv::Size(5, 5), 0);
    return feature;
}


std::optional<MatchResultRaw> CoreMatch(
    const cv::Mat& searchImgRaw,
    const cv::Mat& templRaw,
    const cv::Mat& weightMask,
    int blurSize
) {
    if (searchImgRaw.rows < templRaw.rows || searchImgRaw.cols < templRaw.cols)
        return std::nullopt;

    cv::Mat searchImg;
    if (searchImgRaw.channels() == 4) cv::cvtColor(searchImgRaw, searchImg, cv::COLOR_BGRA2GRAY);
    else if (searchImgRaw.channels() == 3) cv::cvtColor(searchImgRaw, searchImg, cv::COLOR_BGR2GRAY);
    else searchImg = searchImgRaw.clone();

    cv::Mat templ;
    if (templRaw.channels() == 4) cv::cvtColor(templRaw, templ, cv::COLOR_BGRA2GRAY);
    else if (templRaw.channels() == 3) cv::cvtColor(templRaw, templ, cv::COLOR_BGR2GRAY);
    else templ = templRaw.clone();

    if (blurSize > 0) {
        cv::GaussianBlur(searchImg, searchImg, cv::Size(blurSize, blurSize), 0);
    }

    cv::Mat result;
    try {
        if (cv::countNonZero(weightMask) < 5) return std::nullopt;

        cv::matchTemplate(searchImg, templ, result, cv::TM_CCOEFF_NORMED, weightMask);
        
        cv::patchNaNs(result, -1.0);
        for (int y=0; y<result.rows; ++y) {
            float* row = result.ptr<float>(y);
            for (int x=0; x<result.cols; ++x) {
                if (!std::isfinite(row[x])) row[x] = -1.0f;
            }
        }
    } catch (cv::Exception& e) {
        LogError << "[CoreMatch] OpenCV Error: " << std::string(e.what());
        return std::nullopt;
    }

    double minVal, maxVal;
    cv::Point minLoc, maxLoc;
    cv::minMaxLoc(result, &minVal, &maxVal, &minLoc, &maxLoc);

    int ex = std::max(3, std::min(templ.cols, templ.rows) / 10);
    cv::Rect peakRect(maxLoc.x - ex, maxLoc.y - ex, ex*2+1, ex*2+1);
    peakRect &= cv::Rect(0,0,result.cols,result.rows);

    cv::Mat tmp = result.clone();
    tmp(peakRect).setTo(-2.0f);
    double secondVal;
    cv::Point secondLoc;
    cv::minMaxLoc(tmp, nullptr, &secondVal, nullptr, &secondLoc);

    cv::Mat sideMask(result.size(), CV_8U, cv::Scalar(255));
    sideMask(peakRect).setTo(0);
    cv::Scalar mean, stddev;
    cv::meanStdDev(result, mean, stddev, sideMask);
    double psr = (maxVal - mean[0]) / (stddev[0] + 1e-6);

    MatchResultRaw out;
    out.score = maxVal;
    out.loc = maxLoc;
    out.secondScore = secondVal;
    out.delta = maxVal - secondVal;
    out.psr = psr;

    return out;
}

class StandardMatchStrategy : public IMatchStrategy {
public:
    StandardMatchStrategy(bool isBase, const TrackingConfig& tCfg, const MatchConfig& mCfg, 
                          const ImageProcessingConfig& bCfg, const ImageProcessingConfig& trCfg)
        : _isBase(isBase), trackingCfg(tCfg), matchCfg(mCfg), baseCfg(bCfg), tierCfg(trCfg) {}

    MatchFeature extractTemplateFeature(const cv::Mat& minimap) override {
        MatchFeature feat;
        if (minimap.channels() == 4) {
            cv::cvtColor(minimap, feat.templRaw, cv::COLOR_BGRA2BGR);
            
            cv::Mat bgra[4];
            cv::split(minimap, bgra);
            cv::Mat alpha = bgra[3];
            cv::Mat mask = (alpha >= 220); // 取有效区域
            
            // 防止边界泄漏 腐蚀一下
            cv::erode(mask, mask, cv::getStructuringElement(cv::MORPH_ELLIPSE, cv::Size(3, 3)));
            
            cv::Mat templGray;
            cv::cvtColor(feat.templRaw, templGray, cv::COLOR_BGR2GRAY);
            
            // 填充透明区 取有效区均值
            cv::Scalar meanVScalar = cv::mean(templGray, mask);
            double meanV = meanVScalar[0];
            cv::Mat inv;
            cv::bitwise_not(mask, inv);
            cv::Mat templGrayFilled = templGray.clone();
            templGrayFilled.setTo(meanV, inv);
            
            cv::Mat bgr[3] = { bgra[0].clone(), bgra[1].clone(), bgra[2].clone() };
            for (int i=0; i<3; ++i) {
                bgr[i].setTo(meanV, inv);
            }
            cv::merge(bgr, 3, feat.image);
            
            feat.mask = GenerateMinimapMask(minimap, _isBase ? baseCfg : tierCfg);
            cv::bitwise_and(feat.mask, mask, feat.mask);
        } else {
            feat.templRaw = minimap.clone();
            minimap.copyTo(feat.image);
            feat.mask = GenerateMinimapMask(minimap, _isBase ? baseCfg : tierCfg);
        }
        return feat;
    }

    MatchFeature extractSearchFeature(const cv::Mat& mapRoi) override {
        MatchFeature feat;
        if (mapRoi.channels() == 4) {
            cv::cvtColor(mapRoi, feat.image, cv::COLOR_BGRA2BGR);
        } else {
            feat.image = mapRoi;
        }
        return feat;
    }

    TrackingValidation validateTracking(
        const MatchResultRaw& trackResult, 
        std::chrono::duration<double> dt,
        std::optional<MapPosition> lastPos,
        const cv::Rect& searchRect,
        int templCols,
        int templRows) override {
        
        TrackingValidation v;

        // 边缘吸附
        int maxX = searchRect.width - templCols;
        int maxY = searchRect.height - templRows;
        bool hitEdgeX = (trackResult.loc.x <= trackingCfg.edgeSnapMargin ||
                         trackResult.loc.x >= maxX - trackingCfg.edgeSnapMargin);
        bool hitEdgeY = (trackResult.loc.y <= trackingCfg.edgeSnapMargin ||
                         trackResult.loc.y >= maxY - trackingCfg.edgeSnapMargin);
        v.isEdgeSnapped = hitEdgeX || hitEdgeY;

        v.absX = (double)searchRect.x + trackResult.loc.x + templCols / 2.0;
        v.absY = (double)searchRect.y + trackResult.loc.y + templRows / 2.0;

        double currentSpeed = 0.0;
        if (lastPos) {
            double dx = v.absX - lastPos->x;
            double dy = v.absY - lastPos->y;
            double distanceMoved = std::sqrt(dx * dx + dy * dy);
            double dtSec = dt.count();
            if (dtSec < 0.001) dtSec = 0.001;
            currentSpeed = distanceMoved / dtSec;
        }
        v.isTeleported = currentSpeed > trackingCfg.maxNormalSpeed;

        bool lowScore = trackResult.score < 0.80;
        bool ambiguous = lowScore && (trackResult.psr < 6.0 || trackResult.delta < 0.02);
        v.isScreenBlocked = trackResult.score < trackingCfg.screenBlockedThreshold;

        v.isValid = !v.isEdgeSnapped && !v.isTeleported && !v.isScreenBlocked && !ambiguous;

        return v;
    }

    bool validateGlobalSearch(const MatchResultRaw& fineRes, double& outScore) override {
        if (fineRes.score < matchCfg.passThreshold) {
            return false;
        }
        outScore = fineRes.score;
        return true;
    }

private:
    bool _isBase;
    TrackingConfig trackingCfg;
    MatchConfig matchCfg;
    ImageProcessingConfig baseCfg;
    ImageProcessingConfig tierCfg;
};


class PathHeatmapMatchStrategy : public IMatchStrategy {
public:
    PathHeatmapMatchStrategy(bool isBase, const TrackingConfig& tCfg, const MatchConfig& mCfg, 
                            const ImageProcessingConfig& bCfg, const ImageProcessingConfig& trCfg)
        : _isBase(isBase), trackingCfg(tCfg), matchCfg(mCfg), baseCfg(bCfg), tierCfg(trCfg) {}

    MatchFeature extractTemplateFeature(const cv::Mat& minimap) override {
        MatchFeature feat;
        if (minimap.channels() == 4) {
            cv::cvtColor(minimap, feat.templRaw, cv::COLOR_BGRA2BGR);
        } else {
            feat.templRaw = minimap.clone();
        }

        feat.image = ExtractPathHeatmapFeature(minimap); // 梯度图

        ImageProcessingConfig alphaCfg = _isBase ? baseCfg : tierCfg;
        alphaCfg.minimapDarkMaskThreshold = -1; // 禁用暗部剔除
        alphaCfg.useHsvWhiteMask = false;       // 保证路面像素不被白名单剔除

        feat.mask = GenerateMinimapMask(feat.templRaw, alphaCfg);
        return feat;
    }

    MatchFeature extractSearchFeature(const cv::Mat& mapRoi) override {
        MatchFeature feat;
        feat.image = ExtractPathHeatmapFeature(mapRoi);
        return feat;
    }

    TrackingValidation validateTracking(
        const MatchResultRaw& trackResult, 
        std::chrono::duration<double> dt,
        std::optional<MapPosition> lastPos,
        const cv::Rect& searchRect,
        int templCols,
        int templRows) override {
        
        TrackingValidation v;

        int maxX = searchRect.width - templCols;
        int maxY = searchRect.height - templRows;
        bool hitEdgeX = (trackResult.loc.x <= trackingCfg.edgeSnapMargin ||
                         trackResult.loc.x >= maxX - trackingCfg.edgeSnapMargin);
        bool hitEdgeY = (trackResult.loc.y <= trackingCfg.edgeSnapMargin ||
                         trackResult.loc.y >= maxY - trackingCfg.edgeSnapMargin);
        v.isEdgeSnapped = hitEdgeX || hitEdgeY;

        v.absX = (double)searchRect.x + trackResult.loc.x + templCols / 2.0;
        v.absY = (double)searchRect.y + trackResult.loc.y + templRows / 2.0;

        double currentSpeed = 0.0;
        if (lastPos) {
            double dx = v.absX - lastPos->x;
            double dy = v.absY - lastPos->y;
            double distanceMoved = std::sqrt(dx * dx + dy * dy);
            double dtSec = dt.count();
            if (dtSec < 0.001) dtSec = 0.001;
            currentSpeed = distanceMoved / dtSec;
        }
        v.isTeleported = currentSpeed > trackingCfg.maxNormalSpeed;

        // 追踪态高分豁免
        bool accept = (trackResult.score >= 0.85) || 
                      (trackResult.score >= 0.42 && trackResult.delta >= 0.04 && trackResult.psr >= 3.8) ||
                      (trackResult.score >= 0.40 && trackResult.delta >= 0.05 && trackResult.psr >= 3.8);
        bool hold = trackResult.score >= 0.35 && trackResult.psr >= 4.0;
        
        bool ambiguous = !accept;
        v.isScreenBlocked = !accept && !hold;

        v.isValid = !v.isEdgeSnapped && !v.isTeleported && !v.isScreenBlocked && !ambiguous;

        return v;
    }

    bool validateGlobalSearch(const MatchResultRaw& fineRes, double& outScore) override {
        bool globalAccept = (fineRes.score >= 0.85) || 
                            (fineRes.score >= 0.42 && fineRes.delta >= 0.04 && fineRes.psr >= 3.8) || 
                            (fineRes.score >= 0.40 && fineRes.delta >= 0.05 && fineRes.psr >= 3.8);
        if (!globalAccept) {
            return false;
        }
        outScore = fineRes.score;
        return true;
    }

    bool needsChamferCompensation() const override {
        return true;
    }

private:
    bool _isBase;
    TrackingConfig trackingCfg;
    MatchConfig matchCfg;
    ImageProcessingConfig baseCfg;
    ImageProcessingConfig tierCfg;
};


std::unique_ptr<IMatchStrategy> MatchStrategyFactory::create(
        const std::string& zoneId,
        const TrackingConfig& trackingCfg,
        const MatchConfig& matchCfg,
        const ImageProcessingConfig& baseImgCfg,
        const ImageProcessingConfig& tierImgCfg,
        MatchMode mode) {
    bool isBase = (zoneId.find("Base") != std::string::npos);
    bool usePathHeatmap = (zoneId.find("OMVBase") != std::string::npos);

    if (mode == MatchMode::ForcePathHeatmap) usePathHeatmap = true;
    if (mode == MatchMode::ForceStandard)    usePathHeatmap = false;

    if (usePathHeatmap) {
        return std::make_unique<PathHeatmapMatchStrategy>(isBase, trackingCfg, matchCfg, baseImgCfg, tierImgCfg);
    }
    return std::make_unique<StandardMatchStrategy>(isBase, trackingCfg, matchCfg, baseImgCfg, tierImgCfg);
}

} // namespace maplocator
