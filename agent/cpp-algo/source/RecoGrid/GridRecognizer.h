#pragma once

#include "CellMask.h"
#include "GridDetector.h"
#include "GridMatcher.h"
#include "PHashFilter.h"

#include <MaaUtils/NoWarningCV.hpp>

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
    double minScore = 0.0;
    double hueWeight = 0.0;
    int maxRankedCandidates = 0;
    bool collectCells = true;
    int maxReturnedCells = 128;
    int maxReturnedMatches = 16;
};

struct GridClassifyOptions
{
    int maxPhashDistance = 10;
    double minScore = 0.0;
    double hueWeight = 0.4;
    int maxRankedCandidates = 0;
};

struct GridRecognitionRequest
{
    GridRecognitionOptions options;
    GridClassifyOptions classify;
    std::string templatePath;
    std::vector<std::string> templatePaths;

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
    double templateScore = 0.0;
    double hueScore = 0.0;
};

struct GridRecognitionResult
{
    GridResult grid;
    cv::Rect screenRoi;
    cv::Rect screenGrid;
    std::vector<cv::Rect> screenCells;
    std::vector<Hash> cellHashes;
    std::vector<CellFeature> cellFeatures;
    std::vector<Candidate> candidates;
    std::vector<GridRecognitionMatch> matches;
    bool matched = false;
    std::string message;
};

struct GridTemplateMatchResult
{
    std::vector<Candidate> candidates;
    std::vector<GridRecognitionMatch> matches;
    int candidateCount = 0;
    int rankedCount = 0;
    bool bestRejected = false;
};

struct GridClassifyTemplate
{
    std::string id;
    cv::Mat image;
};

struct GridCellClassification
{
    std::size_t cellIndex = 0;
    cv::Rect screenCell;
    Hash hash = 0;
    bool matched = false;
    std::string templateId;
    double score = 0.0;
    double templateScore = 0.0;
    double hueScore = 0.0;
    int phashDistance = 0;
};

struct GridClassificationResult
{
    std::vector<GridCellClassification> cells;
    int templatesScanned = 0;
    int candidatesAfterPhash = 0;
    int matchesRanked = 0;
    int matchedCells = 0;
    int unmatchedCells = 0;
};

GridRecognitionResult RecognizeGrid(const cv::Mat& image, const GridRecognitionOptions& options = {});
GridRecognitionResult RecognizeGridTemplate(const cv::Mat& image, const cv::Mat& target, const GridRecognitionOptions& options = {});
GridRecognitionResult RecognizeGridRequest(const cv::Mat& image, const GridRecognitionRequest& request);
GridTemplateMatchResult MatchGridTemplate(
    const GridRecognitionResult& result,
    const cv::Mat& target,
    const GridRecognitionOptions& options,
    cv::Size imageSize,
    int maxRankedCandidates = 0);
GridClassificationResult ClassifyGridCells(
    const GridRecognitionResult& result,
    const std::vector<GridClassifyTemplate>& templates,
    const GridRecognitionOptions& gridOptions,
    const GridClassifyOptions& classifyOptions,
    cv::Size imageSize,
    const std::vector<std::size_t>& cellIndices = {});
GridRecognitionRequest ParseGridRecognitionRequest(const char* raw);
GridRecognitionRequest ApplyRoiOverride(const GridRecognitionRequest& request, const cv::Rect& roi);
cv::Mat LoadTemplate(const std::string& templatePath);
std::filesystem::path ResolveTemplatePath(const std::string& templatePath);

} // namespace recogrid
