#pragma once

#include <chrono>

namespace mapnavigator
{

enum class LocalDriverState
{
    Cruise,
    MicroAvoid,
    Recover,
};

enum class LocalDriverAction
{
    Forward,
    ForwardLeft,
    ForwardRight,
    JumpForward,
    RecoverLeft,
    RecoverRight,
};

struct LocalDriverObservation
{
    std::chrono::steady_clock::time_point now;
    double yaw_error = 0.0;
    double distance_delta = 0.0;
    double moved_distance = 0.0;
    double actual_distance = 0.0;
    bool prefer_jump_recovery = false;
};

struct LocalDriverDecision
{
    LocalDriverState state = LocalDriverState::Cruise;
    LocalDriverAction action = LocalDriverAction::Forward;
    bool action_changed = false;
    bool commitment_active = false;
    bool blocked = false;
    bool request_rejoin = false;
};

class LocalDriverLite
{
public:
    LocalDriverLite();

    void Reset();
    void OnWaypointChanged();
    void CancelCommitment();

    LocalDriverDecision Evaluate(const LocalDriverObservation& observation);

    LocalDriverAction CurrentAction() const;
    LocalDriverState CurrentState() const;

private:
    bool HasCommitment(const std::chrono::steady_clock::time_point& now) const;
    void StartAction(LocalDriverState state, LocalDriverAction action, int commitment_ms, const std::chrono::steady_clock::time_point& now);
    void ResolveCommitOutcome(bool still_blocked, bool made_progress);
    LocalDriverAction PickRecoverAction();

    LocalDriverState state_;
    LocalDriverAction action_;
    std::chrono::steady_clock::time_point commit_until_;
    std::chrono::steady_clock::time_point last_jump_time_;
    std::chrono::steady_clock::time_point blocked_since_;
    bool has_last_jump_time_ = false;
    bool has_blocked_since_ = false;
    bool awaiting_commit_result_ = false;
    bool last_side_left_ = false;
    bool last_recover_left_ = false;
    int consecutive_micro_failures_ = 0;
    int consecutive_recover_failures_ = 0;
};

} // namespace mapnavigator
