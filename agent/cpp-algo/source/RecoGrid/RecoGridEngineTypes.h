#pragma once

#include "GridRecognizer.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <cstddef>
#include <filesystem>
#include <string>
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
    int detectedRows = 0;
    int detectedCols = 0;
    int detectedTotalCells = 0;
    int sessionRows = 0;
    int sessionCols = 0;
    int sessionTotalCells = 0;
    int knownCells = 0;
    int unknownCells = 0;
    bool incrementalUsed = false;
    bool hasProgress = false;
    bool reachedEnd = false;
    int rowOffset = 0;
    int rawAlignmentOffset = 0;
    int adjustedAlignmentOffset = 0;
    int supportRows = 0;
    std::string alignmentStatus = "not_applicable";
    bool deltaReliable = false;
    int matchedCells = 0;
    int comparedCells = 0;
    int totalDistance = 0;
    double averageDistance = 0.0;
    double deltaScore = 0.0;
    double matchRatio = 0.0;
    int previousViewportStartRow = 0;
    int currentViewportStartRow = 0;
    bool fallbackUsed = false;
    int fallbackStreak = 0;
    int endConfirmations = 0;
    std::string unresolvedReason;
    std::vector<std::size_t> newCellIndices;
    std::vector<GridScanCell> dispatchableCells;
    std::vector<GridScanCell> cells;
};

} // namespace recogrid
