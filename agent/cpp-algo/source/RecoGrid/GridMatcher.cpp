#include "GridMatcher.h"

#include "GridGeometry.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <algorithm>
#include <cmath>
#include <limits>
#include <stdexcept>
#include <vector>

namespace recogrid
{
namespace
{

cv::Rect VisibleAlphaBounds(const cv::Mat& image)
{
    if (image.channels() != 4) {
        return cv::Rect(0, 0, image.cols, image.rows);
    }

    std::vector<cv::Mat> bgra;
    cv::split(image, bgra);

    cv::Mat alphaMask;
    cv::threshold(bgra[3], alphaMask, 10, 255, cv::THRESH_BINARY);

    std::vector<cv::Point> points;
    cv::findNonZero(alphaMask, points);
    if (points.empty()) {
        return cv::Rect(0, 0, image.cols, image.rows);
    }

    return cv::boundingRect(points);
}

void PrepareTemplateSource(
    const cv::Mat& target,
    const CellMaskRatios& maskRatios,
    cv::Mat& templateBgr,
    cv::Mat& matchMask)
{
    if (target.empty()) {
        throw std::invalid_argument("Cannot match an empty template");
    }

    const cv::Mat maskedTarget = ApplyTemplateMask(target, maskRatios);
    const cv::Rect visible = VisibleAlphaBounds(maskedTarget);
    const cv::Mat cropped = maskedTarget(visible).clone();

    matchMask.release();
    if (cropped.channels() == 4) {
        std::vector<cv::Mat> bgra;
        cv::split(cropped, bgra);
        cv::threshold(bgra[3], matchMask, 10, 255, cv::THRESH_BINARY);
        cv::cvtColor(cropped, templateBgr, cv::COLOR_BGRA2BGR);
    }
    else if (cropped.channels() == 3) {
        templateBgr = cropped;
        matchMask = BuildIgnoreMask(cropped.size(), maskRatios);
    }
    else if (cropped.channels() == 1) {
        cv::cvtColor(cropped, templateBgr, cv::COLOR_GRAY2BGR);
        matchMask = BuildIgnoreMask(cropped.size(), maskRatios);
    }
    else {
        throw std::invalid_argument("Unsupported template channel count");
    }
}

cv::Mat ToBgr(const cv::Mat& image)
{
    if (image.channels() == 3) {
        return image;
    }

    cv::Mat bgr;
    if (image.channels() == 4) {
        cv::cvtColor(image, bgr, cv::COLOR_BGRA2BGR);
    }
    else if (image.channels() == 1) {
        cv::cvtColor(image, bgr, cv::COLOR_GRAY2BGR);
    }
    else {
        throw std::invalid_argument("Unsupported image channel count");
    }
    return bgr;
}

double HueHistogramScore(const cv::Mat& source, const cv::Mat& target, const cv::Mat& mask)
{
    if (source.empty() || target.empty()) {
        return 0.0;
    }

    cv::Mat sourceBgr = ToBgr(source);
    cv::Mat targetBgr = ToBgr(target);
    if (sourceBgr.empty() || targetBgr.empty()) {
        return 0.0;
    }

    if (sourceBgr.size() != targetBgr.size()) {
        cv::resize(targetBgr, targetBgr, sourceBgr.size(), 0, 0, cv::INTER_AREA);
    }

    cv::Mat histMask;
    if (!mask.empty()) {
        cv::resize(mask, histMask, sourceBgr.size(), 0, 0, cv::INTER_NEAREST);
        cv::threshold(histMask, histMask, 10, 255, cv::THRESH_BINARY);
    }

    cv::Mat sourceHsv;
    cv::Mat targetHsv;
    cv::cvtColor(sourceBgr, sourceHsv, cv::COLOR_BGR2HSV);
    cv::cvtColor(targetBgr, targetHsv, cv::COLOR_BGR2HSV);

    const int histSize[] = { 30 };
    const float hueRange[] = { 0.0F, 180.0F };
    const float* ranges[] = { hueRange };
    const int channels[] = { 0 };

    cv::Mat sourceHist;
    cv::Mat targetHist;
    cv::calcHist(&sourceHsv, 1, channels, histMask, sourceHist, 1, histSize, ranges);
    cv::calcHist(&targetHsv, 1, channels, histMask, targetHist, 1, histSize, ranges);

    if (cv::sum(sourceHist)[0] <= 0.0 || cv::sum(targetHist)[0] <= 0.0) {
        return 0.0;
    }

    cv::normalize(sourceHist, sourceHist, 1.0, 0.0, cv::NORM_L1);
    cv::normalize(targetHist, targetHist, 1.0, 0.0, cv::NORM_L1);
    return std::clamp(cv::compareHist(sourceHist, targetHist, cv::HISTCMP_CORREL), 0.0, 1.0);
}

} // namespace

std::vector<TemplateMatchResult> RankTemplateMatches(
    const cv::Mat& roi,
    const cv::Mat& target,
    const std::vector<Candidate>& candidates,
    const CellMaskRatios& maskRatios)
{
    return RankTemplateMatches(roi, target, candidates, TemplateMatchOptions { maskRatios, 0.0 });
}

std::vector<TemplateMatchResult> RankTemplateMatches(
    const cv::Mat& roi,
    const cv::Mat& target,
    const std::vector<Candidate>& candidates,
    const TemplateMatchOptions& options)
{
    if (roi.empty()) {
        throw std::invalid_argument("Cannot match template in an empty ROI");
    }

    std::vector<TemplateMatchResult> results;
    results.reserve(candidates.size());

    cv::Mat sourceTemplateBgr;
    cv::Mat sourceMask;
    PrepareTemplateSource(target, options.maskRatios, sourceTemplateBgr, sourceMask);
    const double hueWeight = std::clamp(options.hueWeight, 0.0, 1.0);
    const double templateWeight = 1.0 - hueWeight;

    static const std::vector<double> scaleMultipliers {
        1.00,
        0.90,
        0.80,
    };

    for (const auto& candidate : candidates) {
        const cv::Rect clipped = ClampRect(candidate.cell, roi.size());
        if (clipped.empty()) {
            continue;
        }

        const cv::Mat cellBgr = ToBgr(roi(clipped));
        const double maxScale = std::min(
            static_cast<double>(clipped.width) / sourceTemplateBgr.cols,
            static_cast<double>(clipped.height) / sourceTemplateBgr.rows);

        double bestScore = -std::numeric_limits<double>::infinity();
        double bestTemplateScore = 0.0;
        double bestHueScore = 0.0;
        cv::Rect bestMatch;
        for (double multiplier : scaleMultipliers) {
            const double scale = maxScale * multiplier;
            const cv::Size templateSize(
                std::max(1, static_cast<int>(std::round(sourceTemplateBgr.cols * scale))),
                std::max(1, static_cast<int>(std::round(sourceTemplateBgr.rows * scale))));
            if (templateSize.width > cellBgr.cols || templateSize.height > cellBgr.rows) {
                continue;
            }

            cv::Mat templateBgr;
            cv::resize(sourceTemplateBgr, templateBgr, templateSize, 0, 0, cv::INTER_AREA);

            cv::Mat mask;
            if (!sourceMask.empty()) {
                cv::resize(sourceMask, mask, templateSize, 0, 0, cv::INTER_NEAREST);
                cv::threshold(mask, mask, 10, 255, cv::THRESH_BINARY);
            }

            cv::Mat match;
            if (!mask.empty() && cv::countNonZero(mask) > 0) {
                cv::matchTemplate(cellBgr, templateBgr, match, cv::TM_CCORR_NORMED, mask);
            }
            else {
                cv::matchTemplate(cellBgr, templateBgr, match, cv::TM_CCORR_NORMED);
            }

            double maxScore = 0.0;
            cv::Point maxLocation;
            cv::minMaxLoc(match, nullptr, &maxScore, nullptr, &maxLocation);
            if (!std::isfinite(maxScore)) {
                continue;
            }

            const cv::Rect localMatch(maxLocation.x, maxLocation.y, templateSize.width, templateSize.height);
            double hueScore = 0.0;
            if (hueWeight > 0.0) {
                hueScore = HueHistogramScore(cellBgr(localMatch), templateBgr, mask);
            }
            const double finalScore = std::clamp(templateWeight * maxScore + hueWeight * hueScore, 0.0, 1.0);

            if (finalScore > bestScore || (finalScore == bestScore && maxScore > bestTemplateScore)) {
                bestScore = finalScore;
                bestTemplateScore = maxScore;
                bestHueScore = hueScore;
                bestMatch =
                    cv::Rect(clipped.x + maxLocation.x, clipped.y + maxLocation.y, templateSize.width, templateSize.height);
            }
        }

        results.push_back({ candidate.cellIndex, candidate.cell, bestMatch, candidate.distance, bestScore, bestTemplateScore, bestHueScore });
    }

    std::sort(results.begin(), results.end(), [](const TemplateMatchResult& lhs, const TemplateMatchResult& rhs) {
        if (lhs.score != rhs.score) {
            return lhs.score > rhs.score;
        }
        if (lhs.templateScore != rhs.templateScore) {
            return lhs.templateScore > rhs.templateScore;
        }
        if (lhs.phashDistance != rhs.phashDistance) {
            return lhs.phashDistance < rhs.phashDistance;
        }
        return lhs.cellIndex < rhs.cellIndex;
    });

    return results;
}

} // namespace recogrid
