#include <chrono>
#include <iostream>

#include "startup_notice_policy.h"

int main()
{
    using Policy = cloud::StartupNoticePolicy;
    using Action = Policy::Action;
    using namespace std::chrono_literals;

    const auto now = Policy::Clock::time_point {};
    int failures = 0;
    int checks = 0;
    const auto expect = [&](Action actual, Action expected, const char* scenario) {
        ++checks;
        if (actual != expected) {
            std::cerr << "FAIL: " << scenario << '\n';
            ++failures;
        }
    };

    Policy stuck;
    expect(stuck.observe(false, now), Action::None, "normal page does not acknowledge a notice");
    expect(stuck.observe(true, now), Action::Confirm, "first notice is acknowledged once");
    expect(stuck.observe(true, now + 9999ms), Action::Wait, "wait for dismissal without clicking again");
    expect(stuck.observe(true, now + 10s), Action::Fail, "unclosed notice fails at deadline");
    expect(stuck.observe(true, now + 1h), Action::Fail, "waiting cannot extend the deadline");

    Policy repeated;
    expect(repeated.observe(true, now), Action::Confirm, "first queue timeout can recover");
    expect(repeated.observe(false, now + 500ms), Action::None, "landing page clears pending acknowledgement");
    expect(repeated.observe(false, now + 20min), Action::None, "normal queue is not timed out by notice policy");
    expect(repeated.observe(true, now + 21min), Action::Fail, "second queue timeout fails even after dismissal");

    Policy ended;
    expect(ended.observe(true, now), Action::Confirm, "ended notice has its own recovery allowance");
    expect(ended.observe(false, now + 1s), Action::None, "ended notice can return to landing");
    expect(ended.observe(true, now + 2s), Action::Fail, "repeated ended notice does not loop");

    Policy another_run;
    expect(another_run.observe(true, now), Action::Confirm, "a new preparation attempt has a fresh allowance");

    Policy delayed_button;
    expect(delayed_button.observe(true, now, false), Action::Wait, "notice waits for its button to become actionable");
    expect(delayed_button.observe(true, now + 2s, false), Action::Wait, "waiting for hydration does not consume confirmation");
    expect(delayed_button.observe(true, now + 3s, true), Action::Confirm, "hydrated button is confirmed once");
    expect(delayed_button.observe(true, now + 4s, true), Action::Wait, "confirmed button is never clicked twice");
    expect(delayed_button.observe(false, now + 5s), Action::None, "dismissed hydrated notice returns to landing");
    expect(delayed_button.observe(true, now + 6s, true), Action::Fail, "reappearing notice cannot replenish confirmation");

    Policy missing_button;
    expect(missing_button.observe(true, now, false), Action::Wait, "missing confirmation starts a bounded readiness wait");
    expect(missing_button.observe(true, now + 9999ms, false), Action::Wait, "polling cannot reset button readiness deadline");
    expect(missing_button.observe(true, now + 10s, false), Action::Fail, "permanently missing button fails at deadline");
    expect(missing_button.observe(true, now + 11s, true), Action::Fail, "late hydration cannot revive an expired notice");

    using ResourcePolicy = cloud::StartupResourcePolicy;
    using ResourceAction = ResourcePolicy::Action;
    const auto expect_resource = [&](ResourceAction actual, ResourceAction expected, const char* scenario) {
        ++checks;
        if (actual != expected) {
            std::cerr << "FAIL: " << scenario << '\n';
            ++failures;
        }
    };
    ResourcePolicy resource;
    expect_resource(resource.recover(), ResourceAction::Reload, "first explicit dependency failure permits a normal reload");
    ++checks;
    if (resource.retry_delay() != 1s) {
        std::cerr << "FAIL: first bootstrap recovery has bounded network backoff\n";
        ++failures;
    }
    expect_resource(resource.recover(), ResourceAction::ReloadIgnoringCache, "repeated dependency failure bypasses cached response");
    ++checks;
    if (resource.retry_delay() != 2s) {
        std::cerr << "FAIL: uncached bootstrap recovery backs off without changing normal input\n";
        ++failures;
    }
    expect_resource(resource.recover(), ResourceAction::Fail, "uncached failure does not cause an unbounded reload loop");
    expect_resource(resource.recover(), ResourceAction::Fail, "exhausted bootstrap budget remains exhausted");
    ResourcePolicy new_preparation;
    expect_resource(new_preparation.recover(), ResourceAction::Reload, "a separate preparation has its own reload budget");
    if (failures) {
        return 1;
    }
    std::cout << "Startup policies: " << checks << " checks passed.\n";
    return 0;
}
