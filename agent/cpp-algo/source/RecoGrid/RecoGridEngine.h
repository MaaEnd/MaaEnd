#pragma once

#include "GridAlignment.h"
#include "GridRecognizer.h"

#include <opencv2/core.hpp>

#include <cstddef>
#include <filesystem>
#include <map>
#include <string>
#include <unordered_map>
#include <vector>

namespace recogrid
{

struct TemplateLoadOptions
{
    bool recursive = false;
};

struct GridScanOptions
{
    GridRecognitionOptions recognition;
    bool incremental = true;
    int expectedTotalCells = 0;
    int matchDistanceThreshold = 12;
    double minMatchRatio = 0.5;
    double endMinMatchRatio = 0.6;
    int occupiedBrightThreshold = 70;
    double minOccupiedMean = 55.0;
    double minOccupiedBrightRatio = 0.20;
    std::string unknownTemplateId = "unknown";
};

struct GridScanCell
{
    int row = 0;
    int col = 0;
    std::size_t cellIndex = 0;
    cv::Rect screenCell;
    std::string templateId = "unknown";
    bool matched = false;
    bool visible = false;
    double score = 0.0;
    double templateScore = 0.0;
    double hueScore = 0.0;
    int phashDistance = 0;
};

struct GridScanResult
{
    bool success = false;
    std::string message;
    int rows = 0;
    int cols = 0;
    int totalCells = 0;
    int sessionRows = 0;
    int sessionCols = 0;
    int sessionTotalCells = 0;
    int expectedTotalCells = 0;
    int normalizedCellDelta = 0;
    int knownCells = 0;
    int unknownCells = 0;
    bool incrementalUsed = false;
    bool hasProgress = false;
    bool reachedEnd = false;
    int rowOffset = 0;
    bool deltaReliable = false;
    int matchedCells = 0;
    int comparedCells = 0;
    int totalDistance = 0;
    double averageDistance = 0.0;
    double deltaScore = 0.0;
    double matchRatio = 0.0;
    std::vector<std::size_t> newCellIndices;
    std::vector<GridScanCell> cells;
};

class RecoGridEngine
{
public:
    void LoadTemplatesFromDirectory(
        const std::filesystem::path& directory,
        const TemplateLoadOptions& options = {});
    void SetTemplates(std::vector<GridClassifyTemplate> templates);
    void ResetSession(const std::string& sessionId);
    void ClearSessions();

    [[nodiscard]] const std::vector<GridClassifyTemplate>& Templates() const noexcept;
    [[nodiscard]] GridScanResult Scan(
        const std::string& sessionId,
        const cv::Mat& image,
        const GridScanOptions& options = {});

private:
    struct SessionState
    {
        GridHashSnapshot snapshot;
        int viewportStartRow = 0;
        int cols = 0;
        std::map<std::pair<int, int>, GridScanCell> cells;
    };

    std::vector<GridClassifyTemplate> templates_;
    std::unordered_map<std::string, SessionState> sessions_;
};

} // namespace recogrid
