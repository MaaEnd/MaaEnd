#pragma once

#include "../RecoGrid/GridRecognizer.h"

namespace essencegridscan
{

// All rectangles use the pipeline's normalized screenshot coordinates.
struct InventoryLayout
{
    cv::Rect first_cell;
    cv::Rect scrollbar_track;
    double pitch;
};

struct InventoryPage
{
    recogrid::GridFrame frame;
    bool empty_inventory = false;
    bool has_scrollbar = false;
    int thumb_offset = 0;
    bool at_top = false;
    bool at_bottom = false;
    double estimated_start_row = 0;
    double position_tolerance_rows = 0;
};

InventoryPage ReadInventoryPage(
    const cv::Mat& image,
    const recogrid::GridRecognitionOptions& options,
    const InventoryLayout& layout,
    int inventory_count);

cv::Range ReadScrollThumb(const cv::Mat& image, cv::Size normalized_size, const cv::Rect& track);

} // namespace essencegridscan
