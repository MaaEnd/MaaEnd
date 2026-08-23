#include "CompositeIcon.h"

namespace iconrecognition::detail
{

PreparedTemplate
    BuildCompositeIcon(const TemplateRecord& record, const cv::Mat& base, const cv::Mat& content, int target_size, int alpha_threshold)
{
    return PrepareCompositeTemplate(record, base, content, target_size, alpha_threshold);
}

} // namespace iconrecognition::detail
