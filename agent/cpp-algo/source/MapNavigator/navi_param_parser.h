#pragma once

#include <string_view>

#include <meojson/json.hpp>

#include "navi_controller.h"

struct MaaContext;

namespace mapnavigator
{

bool TryParseNaviParam(const json::value& custom_action_param, NaviParam& out_param, std::string_view caller_name = "MapNavigateAction");
bool TryParseNaviParam(const char* custom_action_param, NaviParam& out_param, std::string_view caller_name = "MapNavigateAction");

// Second pass, run once the pipeline is reachable and before walking starts: turns every text node an INTERACT point
// named into the texts it holds. Best-effort - an unreadable node costs that point its async upgrade, not the route.
void ResolveInteractTextNodes(MaaContext* context, NaviParam& param);

} // namespace mapnavigator
