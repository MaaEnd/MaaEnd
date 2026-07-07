#pragma once

#include <string_view>

#include <meojson/json.hpp>

#include "navi_controller.h"

namespace mapnavigator
{

bool TryParseNaviParam(
    const json::value& custom_action_param,
    NaviParam& out_param,
    std::string_view caller_name = "MapNavigateAction");
bool TryParseNaviParam(
    const char* custom_action_param,
    NaviParam& out_param,
    std::string_view caller_name = "MapNavigateAction");

} // namespace mapnavigator
