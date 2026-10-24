#pragma once

#include <string_view>

#include <meojson/json.hpp>

#include "navi_controller.h"

struct MaaContext;

namespace mapnavigator
{

bool TryParseNaviParam(const json::value& custom_action_param, NaviParam& out_param, std::string_view caller_name = "MapNavigateAction");
bool TryParseNaviParam(const char* custom_action_param, NaviParam& out_param, std::string_view caller_name = "MapNavigateAction");

// 须在 pipeline 可查询后, 开走前调用, 读不出的节点只让该点退回普通交互
void ResolveInteractTextNodes(MaaContext* context, NaviParam& param);

} // namespace mapnavigator
