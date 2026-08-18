#include "zipline_preference.h"

#include <cstring>
#include <string>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../utils.h"

namespace mapnavigator
{

namespace
{

// 设置项把三态写进这个节点的 attach。节点只用来存值,不会被执行,也不在任何 next 里。
constexpr const char* kPreferenceNode = "MapNavigatorZiplinePreference";

} // namespace

bool ResolveZiplineEnabled(MaaContext* context, bool requested)
{
    if (context == nullptr) {
        return requested;
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, kPreferenceNode, buffer.Get())) {
        LogWarn << "ZiplinePreference: node unavailable, keep what the request asked for" << VAR(kPreferenceNode) << VAR(requested);
        return requested;
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || std::strlen(raw) == 0) {
        return requested;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogWarn << "ZiplinePreference: node data is not a json object" << VAR(kPreferenceNode);
        return requested;
    }

    const auto& obj = parsed->as_object();
    if (!obj.contains("attach") || !obj.at("attach").is_object()) {
        return requested;
    }
    const std::string value = obj.at("attach").as_object().get("zipline", std::string {});

    if (value == "always") {
        return true;
    }
    if (value == "never") {
        return false;
    }
    // "auto" 与任何认不出的值都按跟随任务处理:一个读不懂的字符串不该改变寻路结果。
    return requested;
}

} // namespace mapnavigator
