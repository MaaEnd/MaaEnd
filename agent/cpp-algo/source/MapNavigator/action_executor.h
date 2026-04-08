#pragma once

#include "navi_domain_types.h"

namespace mapnavigator
{

class MotionController;
class ActionWrapper;

struct ActionExecutionResult
{
    bool entered_portal_mode = false;
};

class IActionExecutor
{
public:
    virtual ~IActionExecutor() = default;
    virtual ActionExecutionResult Execute(ActionType action) = 0;
};

class ActionExecutor : public IActionExecutor
{
public:
    ActionExecutor(
        ActionWrapper* action_wrapper,
        MotionController* motion_controller,
        bool enable_local_driver);

    ActionExecutionResult Execute(ActionType action) override;

private:
    ActionWrapper* action_wrapper_;
    MotionController* motion_controller_;
    bool enable_local_driver_;
};

} // namespace mapnavigator
