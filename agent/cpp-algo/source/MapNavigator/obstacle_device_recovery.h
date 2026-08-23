#pragma once

#include <cstdint>
#include <memory>

#include <opencv2/core.hpp>

#include "navi_domain_types.h"

struct MaaContext;

namespace mapnavigator
{

class MotionController;
class PositionProvider;
class RoiTemplateScanner;
struct NavigationSession;
struct RouteTrackingState;

// Result of one removal attempt. Everything except NotAttempted has run the subtask and spent the attempt.
enum class DeviceRemovalOutcome
{
    NotAttempted,  // nothing interactable in front of the agent
    NeedsFreshFix, // handled, but the fix is unusable, so the tick has to end here
    Escaped,       // handled and the agent is out
    StillPinned,   // handled and the agent is still stuck
};

// Carries off a device parked in the way instead of jumping over it, which is the precise fix when a device is
// the actual blocker. A background probe watches the interact button's ROI while a stall builds; a hit runs the
// pipeline subtask that picks the device up and drops it behind the agent. Owns the probe only — the caller
// owns the recovery ladder this sits at the bottom of.
class ObstacleDeviceRecovery
{
public:
    ObstacleDeviceRecovery(
        MaaContext* maa_context,
        MotionController* motion_controller,
        PositionProvider* position_provider,
        NavigationSession* session,
        NaviPosition* position);
    ~ObstacleDeviceRecovery();

    ObstacleDeviceRecovery(const ObstacleDeviceRecovery&) = delete;
    ObstacleDeviceRecovery& operator=(const ObstacleDeviceRecovery&) = delete;

    // base_roi is the pipeline node's own ROI, so the JSON stays the single source of truth. Stays off when the
    // interact-button template is missing: a probe that cannot recognize the button must never guess instead.
    void Start(const cv::Rect& base_roi);
    void Stop();

    // Capture-chokepoint side. Ignored unless the probe is both started and currently being fed.
    void SubmitFrame(const cv::Mat& frame);

    // Feeds the probe only while a stall is building and an attempt is still available, so a route that never
    // stalls pays nothing.
    void UpdateFeeding(int64_t stalled_ms, bool attempt_available);

    // Drops any latched observation. A hit only ever describes the frame it was seen in, so the caller clears
    // it whenever the anchor being fought changes and the old observation stops describing the way ahead.
    void ForgetObservation();

    DeviceRemovalOutcome TryRemove(const RouteTrackingState& route, const Waypoint& waypoint, bool attempt_available);

private:
    MaaContext* maa_context_;
    MotionController* motion_controller_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    NaviPosition* position_;

    std::unique_ptr<RoiTemplateScanner> scanner_;
    bool feeding_ = false;
};

} // namespace mapnavigator
