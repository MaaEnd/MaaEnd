#include "zipline_preference.h"

#include <algorithm>
#include <atomic>
#include <cctype>
#include <string>

#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../../Common/notice.h"
#include "../../Zipline/ZiplineStore.h"
#include "../sensing/pipeline_node_reader.h"
#include "zipline_leg_planner.h"

namespace mapnavigator
{

namespace
{

// 三态存在这个节点的 attach 里, 节点只存值不执行
constexpr const char* kPreferenceNode = "MapNavigatorZiplinePreference";
constexpr const char* kAccountIdentityNode = "CurrentAccountIdentity";
constexpr size_t kAccountIdLength = 16;

bool read_preference(MaaContext* context, bool requested)
{
    const auto node = ReadNodeObject(context, kPreferenceNode, "ZiplinePreference");
    if (!node || !node->contains("attach") || !node->at("attach").is_object()) {
        return requested;
    }
    const std::string value = node->at("attach").as_object().get("zipline", std::string {});

    if (value == "always") {
        return true;
    }
    if (value == "never") {
        return false;
    }
    // "auto" 和认不出的值都跟随请求
    return requested;
}

std::string read_account_identity(MaaContext* context)
{
    const auto node = ReadNodeObject(context, kAccountIdentityNode, "ZiplineAccount");
    if (!node || !node->contains("attach") || !node->at("attach").is_object()) {
        return {};
    }
    const std::string account_id = node->at("attach").as_object().get("account_id", std::string {});
    const bool valid = account_id.size() == kAccountIdLength
                       && std::all_of(account_id.begin(), account_id.end(), [](unsigned char ch) { return std::isxdigit(ch) != 0; });
    if (!account_id.empty() && !valid) {
        LogWarn << "ZiplineAccount: identity has invalid format" << VAR(account_id.size());
        return {};
    }
    return account_id;
}

size_t claim_legacy_records(const std::string& account_id)
{
    const std::filesystem::path path = zipline::ZiplineStore::DefaultPath();

    zipline::ZiplineStore store;
    if (!store.load(path)) {
        LogError << "ZiplineAccount: load zipline records failed, skip claiming legacy records" << VAR(path);
        return 0;
    }
    return store.claimLegacyRecords(account_id, path);
}

void notice_claimed_legacy_records(MaaContext* context, size_t claimed)
{
    if (context == nullptr || claimed == 0) {
        return;
    }

    LogInfo << "ZiplineAccount: claimed legacy zipline records for the current account" << VAR(claimed);
    common::notice::Publish(context, common::notice::Text("zipline.legacy_claimed", { static_cast<int64_t>(claimed) }));
}

} // namespace

bool ResolveZiplineEnabled(MaaContext* context, bool requested)
{
    return read_preference(context, requested);
}

std::string ResolveZiplineAccountId(MaaContext* context)
{
    const std::string account_id = read_account_identity(context);
    if (account_id.empty()) {
        LogWarn << "ZiplineAccount: current game account is unavailable; zipline records will not be used";
        return account_id;
    }

    LogInfo << "ZiplineAccount: current game account selected" << VAR(account_id);
    notice_claimed_legacy_records(context, claim_legacy_records(account_id));
    return account_id;
}

void NoticeZiplineOutcome(MaaContext* context)
{
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
    told_account_unknown.store(false);

    // 两种都有时先说没数据, 只有它需要用户动手
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
