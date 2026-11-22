#include "InventoryLayout.h"

#include <algorithm>
#include <cmath>
#include <vector>

namespace essencegridscan
{
namespace
{

// The cap, antialiasing and selection outline can each move a detected edge by a few pixels at 720p.
constexpr int kEdgeTolerance = 3;
constexpr int kMinThumbLength = 5;
constexpr int kMinThumbContrast = 45;

cv::Range find_thumb(const cv::Mat& gray, const cv::Rect& track)
{
    cv::Range longest(0, 0);
    int start = track.y;
    for (int y = track.y; y <= track.br().y; ++y) {
        if (y < track.br().y) {
            const int x = track.x + track.width / 2;
            const int background = std::max(gray.at<uchar>(y, track.x - 2), gray.at<uchar>(y, track.br().x + 1));
            if (gray.at<uchar>(y, x) >= background + kMinThumbContrast) {
                continue;
            }
        }
        if (y - start > longest.size()) {
            longest = { start, y };
        }
        start = y + 1;
    }
    return longest;
}

} // namespace

cv::Range ReadScrollThumb(const cv::Mat& image, cv::Size normalized_size, const cv::Rect& track)
{
    const auto normalized = recogrid::NormalizeInputSize(image, normalized_size);
    const cv::Rect strip(track.x - 2, track.y, track.width + 4, track.height);
    if (track.empty() || (strip & cv::Rect({}, normalized.size())) != strip) {
        return {};
    }
    cv::Mat gray = normalized(strip);
    if (gray.channels() != 1) {
        cv::cvtColor(gray, gray, cv::COLOR_BGR2GRAY);
    }
    const auto thumb = find_thumb(gray, { 2, 0, track.width, track.height });
    return thumb.size() < kMinThumbLength ? cv::Range() : thumb + track.y;
}

InventoryPage ReadInventoryPage(
    const cv::Mat& image,
    const recogrid::GridRecognitionOptions& options,
    const InventoryLayout& layout,
    int inventory_count)
{
    InventoryPage page;
    const cv::Mat normalized = recogrid::NormalizeInputSize(image, options.region_.normalized_size_);
    const cv::Rect bounds({}, normalized.size());
    const auto& roi = options.region_.roi_;
    const auto& first = layout.first_cell;
    const auto& track = layout.scrollbar_track;
    if (normalized.empty() || roi.empty() || (roi & bounds) != roi || first.empty() || (first & roi) != first || layout.pitch <= first.width
        || layout.pitch <= first.height || track.empty() || (track & bounds) != track || track.x < 2 || track.br().x + 1 >= bounds.width
        || inventory_count < 0) {
        return page;
    }

    cv::Mat gray;
    if (normalized.channels() == 1) {
        gray = normalized;
    }
    else if (normalized.channels() == 3 || normalized.channels() == 4) {
        cv::cvtColor(normalized, gray, cv::COLOR_BGR2GRAY);
    }
    else {
        return page;
    }
    const cv::Range thumb = ReadScrollThumb(normalized, normalized.size(), track);
    page.has_scrollbar = !thumb.empty();
    page.thumb_offset = page.has_scrollbar ? thumb.start - track.y : 0;
    page.at_top = page.has_scrollbar && thumb.start <= track.y + kEdgeTolerance;
    page.at_bottom = page.has_scrollbar && thumb.end >= track.br().y - kEdgeTolerance;

    // Infer vertical phase from complete card backgrounds. A single card contributes the same
    // evidence as a full row. In particular the dark central icon cannot split a sparse row.
    if (inventory_count == 0) {
        cv::Mat bright;
        cv::compare(gray(roi), 120, bright, cv::CMP_GT);
        page.empty_inventory = cv::countNonZero(bright) < first.area() / 8;
        return page;
    }
    cv::Mat binary;
    cv::threshold(gray(roi), binary, 0, 255, cv::THRESH_OTSU);
    std::vector<std::vector<cv::Point>> contours;
    cv::findContours(binary, contours, cv::RETR_EXTERNAL, cv::CHAIN_APPROX_SIMPLE);
    const int cols = 1 + static_cast<int>((roi.br().x - first.br().x) / layout.pitch);
    const int tolerance = std::max(kEdgeTolerance, first.height / 12);
    std::vector<int> tops;
    for (const auto& contour : contours) {
        cv::Rect box = cv::boundingRect(contour);
        box += roi.tl();
        const int col = cvRound((box.x - first.x) / layout.pitch);
        if (col < 0 || col >= cols || std::abs(box.x - (first.x + col * layout.pitch)) > tolerance
            || std::abs(box.width - first.width) > tolerance || std::abs(box.height - first.height) > tolerance) {
            continue;
        }
        // Both side edges move out when a selected shell expands. Its dark
        // rarity stripe may be absent from this bright contour, so recover
        // the nominal top using the measured growth on both sides.
        const int left = first.x + cvRound(col * layout.pitch);
        const int growth = std::min(left - box.x, box.br().x - (left + first.width));
        const int top_adjustment =
            growth > 0 ? cvRound(static_cast<double>(growth) * first.height / first.width) : cvRound((box.height - first.height) * 0.5);
        tops.push_back(box.y + top_adjustment);
    }
    std::sort(tops.begin(), tops.end());
    recogrid::GridLayout detected;
    detected.roi = normalized(roi);
    for (size_t i = 0; i < tops.size();) {
        const auto end = std::upper_bound(tops.begin() + i, tops.end(), tops[i] + tolerance);
        const size_t j = static_cast<size_t>(end - tops.begin());
        int y = tops[(i + j) / 2];
        i = j;
        if (y + first.height > roi.br().y + kEdgeTolerance) {
            continue;
        }
        y = std::min(y, roi.br().y - first.height);
        if (!detected.rows.empty() && std::abs(y - roi.y - detected.rows.back().start - layout.pitch) > tolerance) {
            page.frame.message = "Missing row inside inventory viewport";
            return page;
        }
        detected.rows.push_back({ y - roi.y, y - roi.y + first.height });
    }
    for (int col = 0; col < cols; ++col) {
        const int x = first.x - roi.x + cvRound(col * layout.pitch);
        detected.cols.push_back({ x, x + first.width });
    }
    for (const auto& row : detected.rows) {
        for (const auto& col : detected.cols) {
            detected.cells.emplace_back(col.start, row.start, first.width, first.height);
        }
    }
    page.frame = recogrid::RecognizeGrid(image, options, detected);
    if (page.frame.empty()) {
        return page;
    }

    // Estimate an absolute row independently of card hashes. Slider quantization matters
    // for long inventories. Keep that uncertainty when checking candidate overlap offsets.
    const int total_rows = (inventory_count + cols - 1) / cols;
    const int travel = track.height - thumb.size();
    const int first_y = detected.rows.front().start + roi.y;
    const double content_travel = std::max(0.0, (total_rows - 1) * layout.pitch + first.height - track.height);
    if (page.has_scrollbar && travel > kEdgeTolerance) {
        const double pixels_per_thumb_pixel = content_travel / travel;
        page.estimated_start_row = (page.thumb_offset * pixels_per_thumb_pixel + first_y - first.y) / layout.pitch;
        page.position_tolerance_rows = 0.45 + kEdgeTolerance * pixels_per_thumb_pixel / layout.pitch;
    }
    // Some layouts hide a full-height thumb. A top-aligned page whose visible card
    // positions account for the independent inventory count needs no scrolling.
    if (!page.has_scrollbar && std::abs(first_y - first.y) <= tolerance && total_rows == page.frame.rows) {
        page.at_top = true;
        page.at_bottom = true;
    }
    if (page.at_top && std::abs(first_y - first.y) > tolerance) {
        page.at_top = false;
    }
    return page;
}

} // namespace essencegridscan
