#pragma once

#include <vector>

#include "PerformanceDiagnostics.h"
#include "SubpixelMatcher.h"

namespace iconrecognition::detail
{

struct MatchDiagnostics
{
    double tm_score = -1.0;
    double color_score = 0.0;
    double score = -1.0;
    cv::Point position;
    Phase phase;
    bool fallback_used = false;
};

MatchDiagnostics
    ScoreTemplateAt(const cv::Mat& image, const cv::Rect& slot, const PreparedTemplate& templ, int search_radius, Phase phase = {});
MatchDiagnostics ScoreTemplateAt(
    const cv::Mat& image,
    const cv::Rect& slot,
    const PreparedTemplate& templ,
    int search_radius,
    Phase phase,
    MatcherPerformanceDiagnostics* performance);
MatchDiagnostics
    MatchTemplateAt(const cv::Mat& image, const cv::Rect& slot, const PreparedTemplate& templ, double threshold, double subpixel_threshold);

} // namespace iconrecognition::detail
