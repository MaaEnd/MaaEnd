#pragma once

#include "TemplateTypes.h"

namespace iconrecognition::detail
{

PreparedTemplate
    BuildCompositeIcon(const TemplateRecord& record, const cv::Mat& base, const cv::Mat& content, int target_size, int alpha_threshold);

} // namespace iconrecognition::detail
