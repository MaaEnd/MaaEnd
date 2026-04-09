#pragma once

#include <chrono>
#include <functional>
#include <string>

#include "Motion/turn_execution_engine.h"
#include "navi_domain_types.h"

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
    void NotifySprintTriggered();
    bool CancelSprintIfActive(int release_millis);

    bool IsMoving() const;
    bool IsMovingForward() const;
    bool HasAppliedAction() const;
    MotionPredictMode predict_mode() const;

    void ArmForwardCommit(double delta_degrees, const char* reason);
    void ClearForwardCommit();
    bool IsForwardCommitActive(const std::chrono::steady_clock::time_point& now) const;
    bool adaptive_mode_enabled() const;
    void EnableAdaptiveMode(const char* reason, double metric);
    TurnCommandResult InjectMouseAndTrack(
        double delta_degrees,
        bool allow_learning,
        const std::string& expected_zone_id,
        int settle_wait_ms,
        TurnActionKind action_kind,
        uint64_t frame_seq = 0,
        bool block_forward_until_confirmed = false,
        double confirm_heading_tolerance_degrees = 0.0);
    void UpdateTurnLifecycle(uint64_t frame_seq = 0);
    bool HasActiveTurnLifecycle() const;
    bool HasActiveSteeringTrimLifecycle() const;
    bool HasPendingTrimRetryRequest() const;
    bool ShouldBlockForwardMotion() const;
    void ClearPendingTrimRetry(const char* reason);
    void HoldPosition();

private:
    bool ActionProducesTranslation(LocalDriverAction action) const;
    bool ActionMovesForward(LocalDriverAction action) const;

    ActionWrapper* action_wrapper_;
    NavigationSession* session_;
    std::function<bool()> should_stop_;
    bool enable_local_driver_;
    LocalDriverAction applied_action_ = LocalDriverAction::Forward;
    bool has_applied_action_ = false;
    bool is_moving_ = false;
    bool is_moving_forward_ = false;
    bool sprint_active_ = false;
    std::chrono::steady_clock::time_point turn_forward_commit_until_ {};
    TurnExecutionEngine turn_execution_engine_;
};

} // namespace mapnavigator
