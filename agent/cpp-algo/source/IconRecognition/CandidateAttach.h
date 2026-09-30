#pragma once

#include <optional>
#include <string>
#include <string_view>

#include <MaaFramework/MaaAPI.h>
#include <meojson/json.hpp>

#include "IconRecognitionTypes.h"

namespace iconrecognition
{

// 从节点 attach 解析启用的 item_id 与分类 filter，并与 custom_recognition_param 候选取并集。
void MergeAttachIntoCandidates(CandidateFilter& candidates, const json::object& attach);

std::optional<json::object> TryReadNodeAttach(MaaContext* context, const char* node_name);

} // namespace iconrecognition
