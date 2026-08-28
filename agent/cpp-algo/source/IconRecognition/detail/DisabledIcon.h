#pragma once

#include <MaaUtils/NoWarningCV.hpp>

#include "TemplateTypes.h"

namespace iconrecognition::detail
{

PreparedTemplate
    BuildRegionUnavailableTemplate(const PreparedTemplate& base, const cv::Mat& dark_band, const cv::Mat& white_mark, int alpha_threshold);

} // namespace iconrecognition::detail
