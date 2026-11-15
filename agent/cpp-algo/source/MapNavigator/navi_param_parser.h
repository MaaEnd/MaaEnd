#pragma once

#include <string_view>

#include <meojson/json.hpp>

#include "navi_param.h"

struct MaaContext;

namespace mapnavigator
{

bool TryParseNaviParam(NaviParam* out_param, const json::value& custom_action_param, std::string_view caller_name = "MapNavigateAction");
bool TryParseNaviParam(NaviParam* out_param, const char* custom_action_param, std::string_view caller_name = "MapNavigateAction");

// 须在 pipeline 可查询后, 开走前调用, 读不出的节点只让该点退回普通交互
void ResolveInteractTextNodes(NaviParam* param, MaaContext* context);

} // namespace mapnavigator
