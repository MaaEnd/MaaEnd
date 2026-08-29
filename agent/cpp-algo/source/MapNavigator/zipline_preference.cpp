#include "zipline_preference.h"

#include <atomic>
#include <cstring>
#include <string>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../Common/notice.h"
#include "../utils.h"
#include "zipline_leg_planner.h"

namespace mapnavigator
{

namespace
{

// 设置项把三态写进这个节点的 attach。节点只用来存值,不会被执行,也不在任何 next 里。
constexpr const char* kPreferenceNode = "MapNavigatorZiplinePreference";

bool ReadPreference(MaaContext* context, bool requested)
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

} // namespace

bool ResolveZiplineEnabled(MaaContext* context, bool requested)
{
    // 每次请求都从干净的账开始:上一次寻路留下的结论不该算到这一次头上。
    ResetZiplineOutcome();
    return ReadPreference(context, requested);
}

void NoticeZiplineOutcome(MaaContext* context)
{
    // 这两个闩故意跨请求存活:同一句话在一次运行里说一遍就够了,按请求重置等于每条腿都弹。
    static std::atomic_bool told_no_data { false };
    static std::atomic_bool told_not_chosen { false };

    if (context == nullptr) {
        return;
    }

    const ZiplineOutcome outcome = CurrentZiplineOutcome();
    if (outcome.used) {
        told_no_data.store(false);
        told_not_chosen.store(false);
        return;
    }

    // 两种都撞上时先说没数据:只有它需要用户动手。
    if (outcome.no_data) {
        if (!told_no_data.exchange(true)) {
            LogWarn << "Zipline requested but no zipline coordinates are available here; this route walks the whole way.";
            common::notice::Publish(context, common::notice::Text("zipline.no_data"));
        }
        return;
    }

    if (outcome.not_chosen && !told_not_chosen.exchange(true)) {
        LogInfo << "Zipline requested but no candidate beat walking on this route.";
        common::notice::Publish(context, common::notice::Text("zipline.not_chosen"));
    }
}

} // namespace mapnavigator
