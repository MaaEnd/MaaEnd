#pragma once

#include <MaaFramework/MaaAPI.h>

#include "navi_param.h"

namespace mapnavigator
{

class NaviController
{
public:
    explicit NaviController(MaaContext* ctx);
    ~NaviController() = default;

    bool navigate(const NaviParam& requested_param);

private:
    MaaContext* ctx_;
};

} // namespace mapnavigator
