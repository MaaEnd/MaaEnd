#pragma once

#include <MaaUtils/NoWarningCV.hpp>

#include <vector>

namespace recogrid
{

struct Segment
{
    int start = 0;
    int end = 0;
};

struct GridDetectOptions
{
    cv::Size normalizedSize { 1280, 720 };
    cv::Rect roi { 20, 70, 960, 600 };
    double rowThresholdRatio = 0.40;
    double colThresholdRatio = 0.40;
    int minRawSegmentLength = 10;
    double minKeptSegmentRatio = 0.70;
    int lockedRowHeight = 0;
    int lockedColWidth = 0;
    double lockedSegmentTolerance = 0.35;
};

struct GridResult
{
    cv::Mat roi;
    cv::Mat binary;
    std::vector<Segment> rows;
    std::vector<Segment> cols;
    std::vector<cv::Rect> cells;
    std::vector<Segment> rawRows;
    std::vector<Segment> rawCols;
    int minRowHeight = 0;
    int minColWidth = 0;
};

int SegmentLength(const Segment& segment);
int ModalSegmentLength(const std::vector<Segment>& segments);
cv::Mat NormalizeInputSize(const cv::Mat& src, cv::Size normalizedSize);
cv::Mat CropRoi(const cv::Mat& src, cv::Rect roi);
GridResult DetectGrid(const cv::Mat& image, const GridDetectOptions& options = {});

} // namespace recogrid
