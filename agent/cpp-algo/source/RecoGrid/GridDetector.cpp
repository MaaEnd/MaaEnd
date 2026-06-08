#include "GridDetector.h"

#include <MaaUtils/NoWarningCV.hpp>

#include <algorithm>
#include <cmath>
#include <stdexcept>

namespace recogrid
{
namespace
{

std::vector<Segment> FindSegments(const cv::Mat& projection, int threshold, int minLength)
{
    std::vector<Segment> segments;
    if (projection.empty()) {
        return segments;
    }

    const int* values = projection.ptr<int>();
    int length = projection.cols;
    if (projection.rows > 1) {
        length = projection.rows;
    }

    bool inSegment = false;
    int segmentStart = 0;

    for (int i = 0; i < length; ++i) {
        const int value = values[i];
        if (!inSegment && value > threshold) {
            inSegment = true;
            segmentStart = i;
        }
        else if (inSegment && value <= threshold) {
            if (i - segmentStart >= minLength) {
                segments.push_back({ segmentStart, i });
            }
            inSegment = false;
        }
    }

    if (inSegment && length - segmentStart >= minLength) {
        segments.push_back({ segmentStart, length });
    }

    return segments;
}

int MedianLength(std::vector<Segment> segments)
{
    if (segments.empty()) {
        return 0;
    }

    std::vector<int> lengths;
    lengths.reserve(segments.size());
    for (const auto& segment : segments) {
        lengths.push_back(SegmentLength(segment));
    }

    auto middle = lengths.begin() + static_cast<std::ptrdiff_t>(lengths.size() / 2);
    std::nth_element(lengths.begin(), middle, lengths.end());
    return *middle;
}

std::vector<Segment> FilterSmallSegments(const std::vector<Segment>& segments, double minRatio, int projectionLength, int& minLength)
{
    std::vector<Segment> filtered;
    filtered.reserve(segments.size());

    const int typicalLength = MedianLength(segments);
    minLength = static_cast<int>(std::round(static_cast<double>(typicalLength) * minRatio));
    if (minLength <= 0) {
        return segments;
    }

    std::vector<Segment> normalized;
    normalized.reserve(segments.size());
    const int maxMergeGap = std::max(2, minLength / 5);
    for (std::size_t i = 0; i < segments.size(); ++i) {
        Segment segment = segments[i];
        if (i + 1 < segments.size()) {
            const Segment& next = segments[i + 1];
            const int gap = next.start - segment.end;
            const int mergedLength = next.end - segment.start;
            const bool touchesBoundary = segment.start <= 0 || next.end >= projectionLength;
            if (SegmentLength(segment) < minLength && SegmentLength(next) < minLength && gap >= 0 &&
                gap <= maxMergeGap && mergedLength >= minLength && !touchesBoundary) {
                segment.end = next.end;
                ++i;
            }
        }
        normalized.push_back(segment);
    }

    for (const auto& segment : normalized) {
        if (SegmentLength(segment) >= minLength) {
            filtered.push_back(segment);
        }
    }

    return filtered;
}

cv::Mat ToGray(const cv::Mat& image)
{
    if (image.channels() == 1) {
        return image;
    }

    cv::Mat gray;
    if (image.channels() == 4) {
        cv::cvtColor(image, gray, cv::COLOR_BGRA2GRAY);
    }
    else if (image.channels() == 3) {
        cv::cvtColor(image, gray, cv::COLOR_BGR2GRAY);
    }
    else {
        throw std::invalid_argument("Unsupported image channel count for grid detection");
    }

    return gray;
}

} // namespace

int SegmentLength(const Segment& segment)
{
    return segment.end - segment.start;
}

cv::Mat NormalizeInputSize(const cv::Mat& src, cv::Size normalizedSize)
{
    if (src.empty()) {
        throw std::invalid_argument("Cannot normalize an empty image");
    }
    if (normalizedSize.width <= 0 || normalizedSize.height <= 0) {
        throw std::invalid_argument("Normalized grid image size must be positive");
    }

    if (src.cols == normalizedSize.width && src.rows == normalizedSize.height) {
        return src;
    }

    cv::Mat normalized;
    cv::resize(src, normalized, normalizedSize);
    return normalized;
}

cv::Mat CropRoi(const cv::Mat& src, cv::Rect roi)
{
    if (src.empty()) {
        throw std::invalid_argument("Cannot crop ROI from an empty image");
    }

    const cv::Rect bounds(0, 0, src.cols, src.rows);
    roi &= bounds;
    if (roi.empty()) {
        throw std::invalid_argument("Grid ROI is outside image bounds");
    }

    return src(roi).clone();
}

GridResult DetectGrid(const cv::Mat& image, const GridDetectOptions& options)
{
    GridResult result;
    const cv::Mat normalized = NormalizeInputSize(image, options.normalizedSize);
    result.roi = CropRoi(normalized, options.roi);

    const cv::Mat gray = ToGray(result.roi);
    cv::threshold(gray, result.binary, 0, 255, cv::THRESH_OTSU);

    cv::Mat rowSum;
    cv::reduce(result.binary, rowSum, 1, cv::REDUCE_SUM, CV_32S);

    cv::Mat colSum;
    cv::reduce(result.binary, colSum, 0, cv::REDUCE_SUM, CV_32S);

    double rowMax = 0.0;
    double colMax = 0.0;
    cv::minMaxLoc(rowSum, nullptr, &rowMax);
    cv::minMaxLoc(colSum, nullptr, &colMax);

    const int rowThreshold = static_cast<int>(rowMax * options.rowThresholdRatio);
    const int colThreshold = static_cast<int>(colMax * options.colThresholdRatio);

    auto rowSegments = FindSegments(rowSum, rowThreshold, options.minRawSegmentLength);
    auto colSegments = FindSegments(colSum, colThreshold, options.minRawSegmentLength);

    result.rawRows = rowSegments;
    result.rawCols = colSegments;
    rowSegments = FilterSmallSegments(rowSegments, options.minKeptSegmentRatio, rowSum.rows, result.minRowHeight);
    colSegments = FilterSmallSegments(colSegments, options.minKeptSegmentRatio, colSum.cols, result.minColWidth);
    result.rows = rowSegments;
    result.cols = colSegments;

    for (const auto& row : result.rows) {
        for (const auto& col : result.cols) {
            result.cells.emplace_back(col.start, row.start, SegmentLength(col), SegmentLength(row));
        }
    }

    return result;
}

} // namespace recogrid
