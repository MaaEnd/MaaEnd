#include "GridGeometry.h"

#include <map>

namespace recogrid
{

int ModalSegmentLength(const std::vector<Segment>& segments)
{
    if (segments.empty()) {
        return 0;
    }

    std::map<int, int> counts;
    for (const Segment& segment : segments) {
        const int length = segment.end - segment.start;
        if (length > 0) {
            counts[length]++;
        }
    }

    int bestLength = 0;
    int bestCount = 0;
    for (const auto& entry : counts) {
        const int length = entry.first;
        const int count = entry.second;
        if (count > bestCount || (count == bestCount && length > bestLength)) {
            bestLength = length;
            bestCount = count;
        }
    }
    return bestLength;
}

cv::Mat NormalizeInputSize(const cv::Mat& src, cv::Size normalizedSize)
{
    if (src.empty() || normalizedSize.width <= 0 || normalizedSize.height <= 0) {
        return {};
    }

    if (src.cols == normalizedSize.width && src.rows == normalizedSize.height) {
        return src;
    }

    cv::Mat normalized;
    cv::resize(src, normalized, normalizedSize);
    return normalized;
}

} // namespace recogrid
