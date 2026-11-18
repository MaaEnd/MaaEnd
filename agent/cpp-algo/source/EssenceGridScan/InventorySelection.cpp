#include "InventorySelection.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <vector>

#include "InventoryLayout.h"

namespace essencegridscan
{
namespace
{

// The comparison uses the symbol on the face of the orb. Subtract one mean
// over all colour channels so both the shape and the colour survive.
constexpr double kGlyphMatch = 0.90;
constexpr double kPossibleMatch = 0.85;

cv::Mat feature(const cv::Mat& image, cv::Rect rect)
{
    cv::Mat small;
    cv::resize(image(rect), small, { 20, 20 }, 0, 0, cv::INTER_AREA);
    if (small.channels() == 4) {
        cv::cvtColor(small, small, cv::COLOR_BGRA2BGR);
    }
    cv::Mat result;
    small.reshape(1, 1).convertTo(result, CV_32F);
    result -= cv::mean(result)[0];
    cv::normalize(result, result);
    return result;
}

cv::Mat card_glyph(const cv::Mat& image, const cv::Rect& box)
{
    const int w = std::max(1, cvRound(box.width * 0.28)), h = std::max(1, cvRound(box.height * 0.28));
    return feature(image, { box.x + cvRound(box.width * 0.56) - w / 2, box.y + cvRound(box.height * 0.41) - h / 2, w, h });
}

std::vector<cv::Mat> preview_glyphs(const cv::Mat& image, const cv::Rect& rect)
{
    std::vector<cv::Mat> glyphs;
    const int step = std::max(1, cvRound(rect.width * 0.05));
    for (const int y : { -step, 0, step }) {
        for (const int x : { -step, 0, step }) {
            const cv::Rect sample = rect + cv::Point(x, y);
            if ((sample & cv::Rect({}, image.size())) == sample) {
                glyphs.push_back(feature(image, sample));
            }
        }
    }
    return glyphs;
}

double similarity(const cv::Mat& card, const std::vector<cv::Mat>& preview)
{
    double best = -1;
    for (const auto& glyph : preview) {
        best = std::max(best, card.dot(glyph));
    }
    return best;
}

// Median position of the inward brightness step along the middle half of a
// straight side. The pointer, corner brackets and corner badges are excluded.
int edge_position(const cv::Mat& gray, cv::Point origin, cv::Point along, cv::Point inward, int length)
{
    const int radius = std::max(2, cvRound(length * 0.08));
    std::vector<int> positions;
    for (int i = length / 4; i < 3 * length / 4; ++i) {
        const auto start = origin + along * i;
        int best = -256, position = 0;
        for (int j = 1 - radius; j < radius; ++j) {
            const auto p = start + inward * j;
            const int gradient = gray.at<uchar>(p + inward) - gray.at<uchar>(p - inward);
            if (gradient > best) {
                best = gradient;
                position = j;
            }
        }
        positions.push_back(position);
    }
    auto middle = positions.begin() + positions.size() / 2;
    std::nth_element(positions.begin(), middle, positions.end());
    return *middle;
}

std::array<int, 4> edges(const cv::Mat& gray, const cv::Rect& box)
{
    return {
        edge_position(gray, box.tl(), { 0, 1 }, { 1, 0 }, box.height),
        edge_position(gray, { box.br().x - 1, box.y }, { 0, 1 }, { -1, 0 }, box.height),
        edge_position(gray, box.tl(), { 1, 0 }, { 0, 1 }, box.width),
        edge_position(gray, { box.x, box.br().y - 1 }, { 1, 0 }, { 0, -1 }, box.width),
    };
}

} // namespace

bool ConfirmInventorySelection(
    const cv::Mat& before,
    const cv::Mat& after,
    std::span<const gridtracker::Cell> visible,
    const cv::Rect& target,
    const cv::Rect& preview,
    std::optional<cv::Rect> previous_selection,
    bool single_item)
{
    const cv::Rect bounds({}, after.size());
    if (before.empty() || before.size() != after.size() || before.type() != after.type()
        || (before.channels() != 3 && before.channels() != 4) || before.depth() != CV_8U || target.empty() || (target & bounds) != target
        || preview.empty() || (preview & bounds) != preview || visible.empty()) {
        return false;
    }
    const auto panel = preview_glyphs(after, preview);
    const auto expected = card_glyph(before, target);
    if (similarity(expected, panel) < kGlyphMatch) {
        return false;
    }
    bool another_match = false;
    for (const auto& cell : visible) {
        if (cell.screen_rect != target) {
            if (IsSelectedCell(after, cell.screen_rect)) {
                return false;
            }
            another_match |= similarity(card_glyph(before, cell.screen_rect), panel) >= kPossibleMatch;
        }
    }
    if ((single_item && visible.size() == 1) || IsSelectedCell(after, target)) {
        return true;
    }
    const bool changed_type = similarity(expected, preview_glyphs(before, preview)) < kPossibleMatch;

    // Identical symbols can legitimately have identical detail panels. Require
    // a transition at the requested coordinate, including release of the old
    // selection if the preview stayed the same. Hover alone can enlarge a card.
    cv::Mat old_gray, new_gray;
    cv::cvtColor(before, old_gray, cv::COLOR_BGR2GRAY);
    cv::cvtColor(after, new_gray, cv::COLOR_BGR2GRAY);
    bool target_grew = false, previous_shrank = false;
    for (const auto& cell : visible) {
        const auto& box = cell.screen_rect;
        const int pad = std::max(2, cvRound(std::max(box.width, box.height) * 0.08));
        const cv::Rect strip = box + cv::Size(2 * pad, 2 * pad) - cv::Point(pad, pad);
        if (box.width < 4 || box.height < 4 || (strip & bounds) != strip) {
            return false;
        }
        const auto old = edges(old_gray, box), current = edges(new_gray, box);
        int outward = 0, inward = 0;
        for (size_t i = 0; i < old.size(); ++i) {
            outward += current[i] < old[i];
            inward += current[i] > old[i];
        }
        if (outward >= 3 && inward == 0) {
            if (box != target) {
                return false;
            }
            target_grew = true;
        }
        if (inward >= 3 && outward == 0 && (previous_selection == box || IsSelectedCell(before, box))) {
            previous_shrank = true;
        }
    }
    return (changed_type && !another_match) || (target_grew && (changed_type || previous_shrank));
}

} // namespace essencegridscan
