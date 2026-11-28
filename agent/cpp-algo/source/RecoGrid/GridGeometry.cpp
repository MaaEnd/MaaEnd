#include "GridGeometry.h"

namespace recogrid
{

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
