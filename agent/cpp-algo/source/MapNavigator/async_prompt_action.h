#pragma once

#include <chrono>
#include <memory>
#include <string>
#include <string_view>
#include <vector>

#include <opencv2/core.hpp>

#include "navi_config.h"
#include "navi_domain_types.h"

struct MaaContext;

namespace mapnavigator
{

class MotionController;
class RoiTemplateScanner;
struct NavigationSession;

// One kind of prompt-driven waypoint action: a background pre-filter watches the on-screen prompt while the agent
// walks, and the pipeline node named here makes the authoritative call. The kinds differ only here.
struct AsyncPromptActionSpec
{
    ActionType action;
    const char* tag;               // scanner label carried into the logs
    const char* entry_node;        // subtask entry the nav thread runs
    const char* recognition_node;  // fallback ROI source, pre-warm target, injection target for the route's text
    const char* exit_node;         // subtask exit; its next is truncated so the subtask returns instead of going on
    const char* default_scan_node; // TemplateMatch node holding this kind's default pre-filter; nullptr = built in
    bool text_from_route;          // false = the node owns one shared table (collect)

    bool Matches(const Waypoint& waypoint) const
    {
        return waypoint.action == action && waypoint.HasPosition() && (!text_from_route || !waypoint.interact_text.empty());
    }

    // A route that named its target also said which point the prompt belongs to, so a hit completes that point.
    bool CompletesWaypointOnTrigger() const { return text_from_route; }
};

// One shared name table for every collectible, so nothing here is route-configurable.
inline constexpr AsyncPromptActionSpec kCollectPromptSpec {
    .action = ActionType::COLLECT,
    .tag = "collect",
    .entry_node = kCollectEntryNode,
    .recognition_node = kCollectRecognitionNode,
    .exit_node = kCollectExitNode,
    .default_scan_node = nullptr,
    .text_from_route = false,
};

// Interaction prompts differ per business, so the route supplies both recognitions: the text the shared node looks
// for once stopped, and the pre-filter node that decides when to stop. The action stays the interact key either way.
inline constexpr AsyncPromptActionSpec kInteractPromptSpec {
    .action = ActionType::INTERACT,
    .tag = "interact",
    .entry_node = kInteractEntryNode,
    .recognition_node = kInteractRecognitionNode,
    .exit_node = kInteractExitNode,
    .default_scan_node = kInteractScanNode,
    .text_from_route = true,
};

// Runs one prompt subtask and waits out the settle after it. expected replaces the recognition node's authored text
// list for this one run; nullptr or empty leaves the node's own table in place.
void RunPromptSubtask(MaaContext* context, const AsyncPromptActionSpec& spec, const std::vector<std::string>* expected);

// Owns one kind's pre-filter and pacing. A route without this kind of point costs one bool check per tick.
class AsyncPromptAction
{
public:
    AsyncPromptAction(const AsyncPromptActionSpec& spec, MaaContext* context, NavigationSession* session, const NaviPosition* position);
    ~AsyncPromptAction();

    AsyncPromptAction(const AsyncPromptAction&) = delete;
    AsyncPromptAction& operator=(const AsyncPromptAction&) = delete;

    const AsyncPromptActionSpec& spec() const { return spec_; }

    bool armed() const { return started_; }

    // Whether this run has any point of this kind at all, and whether the last positioned one is of this kind.
    bool RouteHasPoint() const;
    bool RouteEndsWithPoint() const;

    // fallback_roi is the recognition node's own ROI, used by the built-in pre-filter when no scan node supplies one.
    void Start(const cv::Rect& fallback_roi, std::string_view controller_type);
    void Stop();
    void SubmitFrame(const cv::Mat& frame);

    // Pays the recognition model's cold start while the agent is stopped, never on a walking tick.
    void PreWarmRecognition();

    // Squared distance to the nearest point of this kind; -1 when not armed, unlocalized, or none are left.
    double NearestDistanceSq() const;

    // Stops for the authoritative recognition. true = the tick was spent, and a route-named kind's waypoint is
    // now walked (see CompletesWaypointOnTrigger).
    bool TryTriggerWhileWalking(MotionController* motion_controller, double waypoint_distance, size_t node_idx);
    // Not paced: the caller stands still at the route tail and gives this its own window.
    bool TryTriggerAtRouteTail();

    // Drops a latched observation without acting on it.
    void ForgetDetection();

private:
    const Waypoint* CurrentWaypointOfThisKind() const;
    const Waypoint* LastWaypointOfThisKind() const;

    // Only the point being walked to can fire, so one scanner is enough; it re-points when that point's scan
    // node changes. Rebuilding drops the old latch with the old scanner, which is what we want.
    void EnsureScannerFor(const Waypoint* waypoint);
    void BuildScanner(const std::string& scan_node);
    bool ConsumeDetection();

    AsyncPromptActionSpec spec_;
    MaaContext* context_;
    NavigationSession* session_;
    const NaviPosition* position_;
    cv::Rect fallback_roi_ {};
    std::string controller_type_;
    std::string scan_node_;
    std::unique_ptr<RoiTemplateScanner> scanner_;
    bool started_ = false;
    std::chrono::steady_clock::time_point last_trigger_at_ {};
};

} // namespace mapnavigator
