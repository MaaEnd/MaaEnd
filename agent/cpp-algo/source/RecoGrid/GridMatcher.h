#pragma once

#include "CellMask.h"
#include "PHashFilter.h"

#include <MaaUtils/NoWarningCV.hpp>

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
    double templateScore = 0.0;
    double hueScore = 0.0;
};

struct TemplateMatchOptions
{
    CellMaskRatios maskRatios;
    double hueWeight = 0.0;
};

std::vector<TemplateMatchResult> RankTemplateMatches(
    const cv::Mat& roi,
    const cv::Mat& target,
    const std::vector<Candidate>& candidates,
    const CellMaskRatios& maskRatios = {});
std::vector<TemplateMatchResult> RankTemplateMatches(
    const cv::Mat& roi,
    const cv::Mat& target,
    const std::vector<Candidate>& candidates,
    const TemplateMatchOptions& options);

} // namespace recogrid
