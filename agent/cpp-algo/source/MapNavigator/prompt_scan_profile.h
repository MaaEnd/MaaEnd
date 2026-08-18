#pragma once

#include <string>
#include <string_view>

#include <opencv2/core.hpp>

struct MaaContext;

namespace mapnavigator
{

// What one walking pre-filter looks at. Authored as an ordinary TemplateMatch node so the pipeline stays the
// single source of truth for the scan region, the icon and how strict the match is, and so a platform overlay
// retargets it for free. A route names the node; nothing here is a route-level number.
struct PromptScanProfile
{
    std::string scan_node;            // node it was read from; empty = the shipped default
    cv::Rect base_roi {};             // in the authored base frame
    cv::Mat templ;                    // grayscale, base scale
    cv::Mat mask;                     // CV_8UC1 match mask; empty = match the whole template
    double threshold = 0.0;
};

// Scan ROI of any node, read out of the pipeline rather than hardcoded, so editing that node retargets whoever
// reads it. Authored in the base frame.
bool TryReadNodeRoi(MaaContext* context, const std::string& node_name, cv::Rect* out);

// Reads roi / template / threshold out of one TemplateMatch node and loads the template off disk; nothing else in
// the node is read. Every precondition the matcher and the scanner have is checked here, so a mis-authored node
// costs one error line and no pre-filter instead of misbehaving at walking speed.
// controller_type picks which resource overlay the template path resolves against; the caller already holds it.
bool TryLoadPromptScanProfile(
    MaaContext* context,
    const std::string& scan_node,
    std::string_view controller_type,
    PromptScanProfile* out);

} // namespace mapnavigator
