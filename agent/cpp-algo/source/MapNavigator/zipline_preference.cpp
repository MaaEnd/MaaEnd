#include "zipline_preference.h"

#include <algorithm>
#include <atomic>
#include <cctype>
#include <cstring>
#include <string>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../Common/notice.h"
#include "../Zipline/ZiplineStore.h"
#include "../utils.h"
#include "zipline_leg_planner.h"

namespace mapnavigator
{

namespace
{

// 设置项把三态写进这个节点的 attach。节点只用来存值,不会被执行,也不在任何 next 里。
constexpr const char* kPreferenceNode = "MapNavigatorZiplinePreference";
constexpr const char* kAccountIdentityNode = "CurrentAccountIdentity";
constexpr size_t kAccountIdLength = 16;

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

std::string ReadAccountIdentity(MaaContext* context)
{
    if (context == nullptr) {
        return {};
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, kAccountIdentityNode, buffer.Get())) {
        LogWarn << "ZiplineAccount: identity node unavailable" << VAR(kAccountIdentityNode);
        return {};
    }
    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || std::strlen(raw) == 0) {
        return {};
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogWarn << "ZiplineAccount: identity node data is not a json object" << VAR(kAccountIdentityNode);
        return {};
    }
    const auto& obj = parsed->as_object();
    if (!obj.contains("attach") || !obj.at("attach").is_object()) {
        return {};
    }
    const std::string account_id = obj.at("attach").as_object().get("account_id", std::string {});
    const bool valid = account_id.size() == kAccountIdLength
                       && std::all_of(account_id.begin(), account_id.end(), [](unsigned char ch) { return std::isxdigit(ch) != 0; });
    if (!account_id.empty() && !valid) {
        LogWarn << "ZiplineAccount: identity has invalid format" << VAR(account_id.size());
        return {};
    }
    return account_id;
}

// 旧版本落盘的坐标不带账号字段，规划按账号筛选会把它们全部跳过。这里在寻路入口把它们认领给
// 当前账号：升级后用户什么都没动，旧坐标仍能用。认领只在当前账号还没有任何记录时发生，
// 详细理由见 ZiplineStore::claimLegacyRecords。
size_t ClaimLegacyRecords(const std::string& account_id)
{
    const std::filesystem::path path = zipline::ZiplineStore::DefaultPath();

    zipline::ZiplineStore store;
    if (!store.load(path)) {
        LogError << "ZiplineAccount: load zipline records failed, skip claiming legacy records" << VAR(path);
        return 0;
    }
    return store.claimLegacyRecords(account_id, path);
}

void NoticeClaimedLegacyRecords(MaaContext* context, size_t claimed)
{
    if (context == nullptr || claimed == 0) {
        return;
    }

    // 认领只发生一次，不必加闩：之后同一台机器上不会再有无账号的记录可认领。
    LogInfo << "ZiplineAccount: claimed legacy zipline records for the current account" << VAR(claimed);
    common::notice::Publish(context, common::notice::Text("zipline.legacy_claimed", { static_cast<int64_t>(claimed) }));
}

} // namespace

bool ResolveZiplineEnabled(MaaContext* context, bool requested)
{
    // 每次请求都从干净的账开始:上一次寻路留下的结论不该算到这一次头上。
    ResetZiplineOutcome();
    return ReadPreference(context, requested);
}

std::string ResolveZiplineAccountId(MaaContext* context)
{
    const std::string account_id = ReadAccountIdentity(context);
    if (account_id.empty()) {
        LogWarn << "ZiplineAccount: current game account is unavailable; zipline records will not be used";
        return account_id;
    }

    LogInfo << "ZiplineAccount: current game account selected" << VAR(account_id);
    NoticeClaimedLegacyRecords(context, ClaimLegacyRecords(account_id));
    return account_id;
}

void NoticeZiplineOutcome(MaaContext* context)
{
    // 这些闩故意跨请求存活:同一句话在一次运行里说一遍就够了,按请求重置等于每条腿都弹。
    static std::atomic_bool told_no_data { false };
    static std::atomic_bool told_not_chosen { false };
    static std::atomic_bool told_account_unknown { false };

    if (context == nullptr) {
        return;
    }

    const ZiplineOutcome outcome = CurrentZiplineOutcome();
    if (outcome.used) {
        told_no_data.store(false);
        told_not_chosen.store(false);
        told_account_unknown.store(false);
        return;
    }

    if (outcome.account_unknown) {
        if (!told_account_unknown.exchange(true)) {
            LogWarn << "Zipline requested but the current game account could not be identified; this route walks the whole way.";
            common::notice::Publish(context, common::notice::Text("zipline.account_unknown"));
        }
        return;
    }
    // 身份恢复后允许下一次真正丢失身份时再次提示；不必等到滑索实际被采用。
    told_account_unknown.store(false);

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
