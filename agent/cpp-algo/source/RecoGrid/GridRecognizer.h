#pragma once

#include "CellMask.h"
#include "GridDetector.h"
#include "GridMatcher.h"
#include "PHashFilter.h"

#include <opencv2/core.hpp>

#include <meojson/json.hpp>

#include <cstddef>
#include <filesystem>
#include <string>
#include <vector>

namespace recogrid
{

struct GridRecognitionOptions
{
    GridDetectOptions detect;
    CellMaskRatios mask;
    int maxPhashDistance = 10;
    bool collectCells = true;
    int maxReturnedCells = 128;
    int maxReturnedMatches = 16;
};

struct GridRecognitionRequest
{
    GridRecognitionOptions options;
    std::string templatePath;

    bool from_json(const json::value& value);
};

struct GridRecognitionMatch
{
    std::size_t cellIndex = 0;
    cv::Rect cell;
    cv::Rect screenCell;
    cv::Rect match;
    cv::Rect screenMatch;
    int phashDistance = 0;
    double score = 0.0;
};

struct GridRecognitionResult
{
    GridResult grid;
    cv::Rect screenRoi;
    cv::Rect screenGrid;
    std::vector<cv::Rect> screenCells;
    std::vector<Candidate> candidates;
    std::vector<GridRecognitionMatch> matches;
    bool matched = false;
    std::string message;
};

GridRecognitionResult RecognizeGrid(const cv::Mat& image, const GridRecognitionOptions& options = {});
GridRecognitionResult RecognizeGridTemplate(
    const cv::Mat& image,
    const cv::Mat& target,
    const GridRecognitionOptions& options = {});
GridRecognitionResult RecognizeGridRequest(const cv::Mat& image, const GridRecognitionRequest& request);
GridRecognitionRequest ParseGridRecognitionRequest(const char* raw);
GridRecognitionRequest ApplyRoiOverride(const GridRecognitionRequest& request, const cv::Rect& roi);
cv::Mat LoadTemplate(const std::string& templatePath);
std::filesystem::path ResolveTemplatePath(const std::string& templatePath);

} // namespace recogrid
