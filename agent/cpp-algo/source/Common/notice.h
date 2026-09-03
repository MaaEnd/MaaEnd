#pragma once

#include <cstdint>
#include <string>
#include <vector>

#include <MaaFramework/MaaDef.h>

namespace common::notice
{

// 按 PI_CLIENT_LANGUAGE 取 locales/cpp-algo 里的文案，逐个把 %d 替换成 values；
// 找不到 key 时原样返回 key，方便一眼看出是文案漏配还是逻辑没跑到。
std::string Text(const std::string& key, const std::vector<int64_t>& values = {});

// 借 focus 节点把文案送到客户端界面，和 go-service 的提示走同一条通道。
void Publish(MaaContext* context, const std::string& content);

} // namespace common::notice
