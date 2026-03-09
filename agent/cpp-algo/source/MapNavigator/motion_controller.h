#pragma once

#include "local_driver_lite.h"
#include "navi_domain_types.h"
#include "turn_scale_estimator.h"

#include <chrono>
#include <functional>
#include <string>

namespace mapnavigator
{

class ActionWrapper;
class PositionProvider;
struct NavigationSession;
struct NaviPosition;

class MotionController
{
public:
    MotionController(
        ActionWrapper* action_wrapper,
        PositionProvider* position_provider,
        NavigationSession* session,
        NaviPosition* position,
        bool enable_local_driver,
        std::function<bool()> should_stop);

    void Stop();
    void SetAction(LocalDriverAction action, bool force);
    void EnsureForwardMotion(bool force);
    void ResetForwardWalk(int release_millis);

    bool IsMoving() const;
    bool HasAppliedAction() const;

    void ArmForwardCommit(double delta_degrees, const char* reason);
    void ClearForwardCommit();
    bool IsForwardCommitActive(const std::chrono::steady_clock::time_point& now) const;
    bool adaptive_mode_enabled() const;
    void EnableAdaptiveMode(const char* reason, double metric);
    bool InjectMouseAndTrack(double delta_degrees, bool allow_learning, const std::string& expected_zone_id, int settle_wait_ms);

private:
    bool ActionMovesForward(LocalDriverAction action) const;
    bool ShouldLearnTurn(double delta_degrees, bool allow_learning) const;
    bool CaptureSettledTurnFeedback(NaviPosition* out_pos, const std::string& expected_zone_id, bool apply_feedback_pulse);
    bool IsTurnSampleSuspicious(double commanded_delta_degrees, int units_sent, bool sample_available, double observed_delta_degrees) const;
    bool RunTurnProbe(double target_yaw, const std::string& expected_zone_id, int pending_units, double pending_yaw_before);

    static double NormalizeHeading(double angle);
    void ReleaseFeedbackPulse(bool& feedback_key_down);

    ActionWrapper* action_wrapper_;
    PositionProvider* position_provider_;
    NavigationSession* session_;
    NaviPosition* position_;
    std::function<bool()> should_stop_;
    bool enable_local_driver_;
    LocalDriverAction applied_action_ = LocalDriverAction::Forward;
    bool has_applied_action_ = false;
    bool is_moving_ = false;
    bool adaptive_mode_enabled_ = false;
    std::chrono::steady_clock::time_point turn_forward_commit_until_ {};
    TurnScaleEstimator turn_scale_ {};
};

} // namespace mapnavigator
