#include "GridRecognizer.h"

#include <algorithm>

#include "GridGeometry.h"

namespace recogrid
{
namespace
{

cv::Mat to_gray(const cv::Mat& image)
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
        return {};
    }
    return gray;
}

bool is_occupied(const cv::Mat& image, const GridRecognitionOptions& options)
{
    const cv::Mat gray = to_gray(image);
    if (gray.empty()) {
        return false;
    }
    const cv::Mat keep_mask = BuildIgnoreMask(gray.size(), options.mask);
    const int kept_pixels = keep_mask.empty() ? gray.rows * gray.cols : cv::countNonZero(keep_mask);
    if (kept_pixels <= 0) {
        return false;
    }

    cv::Mat bright;
    cv::threshold(gray, bright, std::clamp(options.occupied_bright_threshold, 0, 255), 255, cv::THRESH_BINARY);
    if (!keep_mask.empty()) {
        cv::bitwise_and(bright, keep_mask, bright);
    }

    const double mean = keep_mask.empty() ? cv::mean(gray)[0] : cv::mean(gray, keep_mask)[0];
    const double bright_ratio = static_cast<double>(cv::countNonZero(bright)) / static_cast<double>(kept_pixels);
    return mean >= options.min_occupied_mean && bright_ratio >= options.min_occupied_bright_ratio;
}

} // namespace

GridFrame RecognizeGrid(const cv::Mat& image, const GridRecognitionOptions& options)
{
    GridFrame frame;
    if (image.empty()) {
        frame.message = "Grid image is empty";
        return frame;
    }

    const GridResult detected = DetectGrid(image, options.detect);
    frame.rows = static_cast<int>(detected.rows.size());
    frame.cols = static_cast<int>(detected.cols.size());
    frame.row_height = ModalSegmentLength(detected.rows);
    frame.col_width = ModalSegmentLength(detected.cols);
    frame.cells.reserve(detected.cells.size());

    for (int row = 0; row < frame.rows; ++row) {
        for (int col = 0; col < frame.cols; ++col) {
            const std::size_t index = static_cast<std::size_t>(row * frame.cols + col);
            if (index >= detected.cells.size()) {
                continue;
            }

            const cv::Rect roi_rect = ClampRect(detected.cells[index], detected.roi.size());
            if (roi_rect.empty()) {
                continue;
            }

            const cv::Mat cell_image = ApplyIgnoreMask(detected.roi(roi_rect), options.mask);
            frame.cells.push_back({
                row,
                col,
                index,
                RoiToScreen(detected.cells[index], options.detect, image.size()),
                ComputeHash(cell_image),
                ComputeCellFeature(cell_image),
                is_occupied(detected.roi(roi_rect), options),
            });
        }
    }

    frame.message = frame.empty() ? "Grid detected no cells" : "Grid detected";
    return frame;
}

} // namespace recogrid
