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
    return RecognizeGrid(image, options, DetectGrid(image, options.detect));
}

GridFrame RecognizeGrid(const cv::Mat& image, const GridRecognitionOptions& options, const GridResult& detected)
{
    GridFrame frame;
    if (image.empty()) {
        frame.message = "Grid image is empty";
        return frame;
    }

    frame.rows = static_cast<int>(detected.rows.size());
    frame.cols = static_cast<int>(detected.cols.size());
    frame.row_height = ModalSegmentLength(detected.rows);
    frame.col_width = ModalSegmentLength(detected.cols);
    frame.cells.reserve(detected.cells.size());
    if (frame.rows == 0 || frame.cols == 0 || frame.col_width <= 0 || frame.row_height <= 0 || detected.roi.empty()) {
        frame.message = "Grid detected no cells";
        return frame;
    }

    // Include the gap in each tile so the icon occupies the same fraction of the
    // sample on both controller layouts, even when only one row is visible.
    const int pitch = frame.cols > 1
                          ? cvRound(static_cast<double>(detected.cols.back().start - detected.cols.front().start) / (frame.cols - 1))
                          : frame.col_width;
    if (pitch <= 0) {
        return {};
    }
    cv::Mat colors = detected.roi;
    if (colors.channels() == 1) {
        cv::cvtColor(detected.roi, colors, cv::COLOR_GRAY2BGR);
    }
    else if (colors.channels() == 4) {
        cv::cvtColor(detected.roi, colors, cv::COLOR_BGRA2BGR);
    }
    frame.alignment_tiles = cv::Mat::zeros(frame.rows * kAlignmentTileSize, frame.cols * kAlignmentTileSize, CV_8UC3);

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
            const cv::Rect sample_rect = ClampRect({ roi_rect.tl(), cv::Size(pitch, pitch) }, colors.size());
            cv::Mat sample = colors(sample_rect);
            if (sample.cols != pitch || sample.rows != pitch) {
                cv::copyMakeBorder(
                    sample,
                    sample,
                    0,
                    pitch - sample.rows,
                    0,
                    pitch - sample.cols,
                    cv::BORDER_REPLICATE | cv::BORDER_ISOLATED);
            }
            const cv::Rect tile(col * kAlignmentTileSize, row * kAlignmentTileSize, kAlignmentTileSize, kAlignmentTileSize);
            cv::resize(sample, frame.alignment_tiles(tile), tile.size(), 0, 0, cv::INTER_AREA);

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
