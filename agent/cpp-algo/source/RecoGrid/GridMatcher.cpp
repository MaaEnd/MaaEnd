#include "GridMatcher.h"

#include <opencv2/imgproc.hpp>

#include <algorithm>
#include <cmath>
#include <limits>
#include <stdexcept>
#include <vector>

namespace recogrid
{
namespace
{

cv::Rect ClampRect(const cv::Rect& rect, const cv::Size& bounds)
{
    return rect & cv::Rect(0, 0, bounds.width, bounds.height);
}

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

} // namespace

std::vector<TemplateMatchResult> RankTemplateMatches(
    const cv::Mat& roi,
    const cv::Mat& target,
    const std::vector<Candidate>& candidates,
    const CellMaskRatios& maskRatios)
{
    if (roi.empty()) {
        throw std::invalid_argument("Cannot match template in an empty ROI");
    }

    std::vector<TemplateMatchResult> results;
    results.reserve(candidates.size());

    cv::Mat sourceTemplateBgr;
    cv::Mat sourceMask;
    PrepareTemplateSource(target, maskRatios, sourceTemplateBgr, sourceMask);

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

            if (maxScore > bestScore) {
                bestScore = maxScore;
                bestMatch = cv::Rect(
                    clipped.x + maxLocation.x,
                    clipped.y + maxLocation.y,
                    templateSize.width,
                    templateSize.height);
            }
        }

        results.push_back({ candidate.cellIndex, candidate.cell, bestMatch, candidate.distance, bestScore });
    }

    std::sort(results.begin(), results.end(), [](const TemplateMatchResult& lhs, const TemplateMatchResult& rhs) {
        if (lhs.score != rhs.score) {
            return lhs.score > rhs.score;
        }
        if (lhs.phashDistance != rhs.phashDistance) {
            return lhs.phashDistance < rhs.phashDistance;
        }
        return lhs.cellIndex < rhs.cellIndex;
    });

    return results;
}

} // namespace recogrid
