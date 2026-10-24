#pragma once

#include <chrono>
#include <optional>

namespace cloud
{

// One acknowledgement per preparation attempt. An unclosed or repeated notice
// is a failure, not permission to keep clicking or wait indefinitely.
class StartupNoticePolicy
{
public:
    using Clock = std::chrono::steady_clock;
    enum class Action
    {
        None,
        Confirm,
        Wait,
        Fail,
    };

    Action observe(bool visible, Clock::time_point now, bool actionable = true)
    {
        if (!visible) {
            acknowledgement_deadline_.reset();
            return Action::None;
        }
        if (!used_) {
            if (!acknowledgement_deadline_) {
                acknowledgement_deadline_ = now + kAcknowledgementTimeout;
            }
            if (now >= *acknowledgement_deadline_) {
                return Action::Fail;
            }
            if (!actionable) {
                return Action::Wait;
            }
            used_ = true;
            acknowledgement_deadline_ = now + kAcknowledgementTimeout;
            return Action::Confirm;
        }
        if (!acknowledgement_deadline_ || now >= *acknowledgement_deadline_) {
            return Action::Fail;
        }
        return Action::Wait;
    }

private:
    // Dispatch is not proof of dismissal; polling must not extend this budget.
    static constexpr auto kAcknowledgementTimeout = std::chrono::seconds(10);
    bool used_ = false;
    std::optional<Clock::time_point> acknowledgement_deadline_;
};

// Retry only an explicitly failed bootstrap dependency. A normal reload keeps
// the fast path; one uncached reload recovers a repeated cached/CDN failure.
// Successful observations do not replenish the preparation attempt's budget.
class StartupResourcePolicy
{
public:
    enum class Action
    {
        Reload,
        ReloadIgnoringCache,
        Fail,
    };

    Action recover()
    {
        if (attempts_ == 0) {
            ++attempts_;
            return Action::Reload;
        }
        if (attempts_ == 1) {
            ++attempts_;
            return Action::ReloadIgnoringCache;
        }
        return Action::Fail;
    }

    std::chrono::milliseconds retry_delay() const
    {
        // Consecutive TLS/CDN failures occurred within one second in testing.
        // Back off only this failed bootstrap, not normal startup or input.
        constexpr auto kInitialRetryDelay = std::chrono::milliseconds(1000);
        constexpr auto kUncachedRetryDelay = std::chrono::milliseconds(2000);
        return attempts_ == 1 ? kInitialRetryDelay : kUncachedRetryDelay;
    }

private:
    unsigned attempts_ = 0;
};

} // namespace cloud
