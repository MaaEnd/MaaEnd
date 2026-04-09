#pragma once

#include <functional>

#include "../navi_domain_types.h"

namespace mapnavigator
{

class ActionWrapper;

class BackendTurnActuator : public ITurnActuator
{
public:
    BackendTurnActuator(ActionWrapper& action_wrapper, std::function<bool()> should_stop = {});
    TurnActuationResult TurnByUnits(int units, int duration_millis) override;

private:
    ActionWrapper& action_wrapper_;
    std::function<bool()> should_stop_;
};

} // namespace mapnavigator
