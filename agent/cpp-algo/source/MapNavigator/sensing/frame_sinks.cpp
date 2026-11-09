#include "frame_sinks.h"
#include "../control/obstacle_device_recovery.h"
#include "../semantic/async_prompt_action.h"
#include "roi_template_scanner.h"

namespace mapnavigator
{

void SubmitFrameToSinks(const FrameSinks& sinks, const cv::Mat& frame)
{
    SubmitPromptFrame(sinks.collect, frame);
    SubmitPromptFrame(sinks.interact, frame);
    SubmitDeviceFrame(sinks.device, frame);
    if (*sinks.zipline_mount != nullptr) {
        (*sinks.zipline_mount)->submitFrame(frame);
    }
}

} // namespace mapnavigator
