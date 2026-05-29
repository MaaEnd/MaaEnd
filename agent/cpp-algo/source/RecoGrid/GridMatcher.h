#pragma once

#include "PHashFilter.h"

#include <opencv2/core.hpp>

#include <vector>

namespace recogrid
{

struct TemplateMatchResult
{
    std::size_t cellIndex = 0;
    cv::Rect cell;
    cv::Rect match;
    int phashDistance = 0;
    double score = 0.0;
};

std::vector<TemplateMatchResult> RankTemplateMatches(
    const cv::Mat& roi,
    const cv::Mat& target,
    const std::vector<Candidate>& candidates);

} // namespace recogrid
