#include "ZiplineImportAction.h"

#include <algorithm>
#include <chrono>
#include <condition_variable>
#include <cstring>
#include <iterator>
#include <memory>
#include <mutex>
#include <optional>
#include <string>
#include <tuple>
#include <unordered_map>
#include <unordered_set>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/Logger.h>

#include "../Common/GameRegion.h"
#include "../Common/WebView2.h"
#include "../Common/notice.h"
#include "../utils.h"
#include "ZiplineFrames.h"
#include "ZiplineStore.h"
#include "account_identity.h"

namespace zipline
{

namespace
{

// 标记列表接口的路径片段。只匹配路径，避免被 query 里的参数顺序影响。
constexpr const char* kMarkListPathFragment = "/map/mark/list";
constexpr const char* kMapUrlCN = "https://game.skland.com/map/endfield";
constexpr const char* kMapUrlGlobal = "https://game.skport.com/map/endfield";
// 窗口的存活上限，留给用户登录：登录后抓齐只要几秒，正常路径根本用不到这个数。
constexpr int64_t kDefaultTimeoutMs = 180000;
constexpr int kPollIntervalMs = 200;
// 标定过的地图全抓到之后再静默这么久就收工，留一点余量给同批次的最后几条。
constexpr int kSettleMs = 1200;
// 抓到了一些但凑不齐标定过的地图时的兜底：静默这么久也收工，不干等满超时。
constexpr int kIdleCloseMs = 20000;
constexpr int kDefaultWindowWidth = 1280;
constexpr int kDefaultWindowHeight = 720;

struct ImportParam
{
    // 由 gamesetting::DetectGameRegion 填入，不接受 attach 覆盖。
    std::string url;
    std::string mark_list_path = kMarkListPathFragment;
    int64_t timeout = kDefaultTimeoutMs;
    int width = kDefaultWindowWidth;
    int height = kDefaultWindowHeight;
    bool clear_login = false;
    std::vector<std::string> template_ids;
};

// 抓到的一份标记列表响应。
struct CapturedResponse
{
    std::string url;
    std::string body;
};

// UI 线程（CDP 回调）与业务线程（轮询落盘）之间的共享状态。
struct SniffState
{
    std::mutex mutex;
    // 有新进展就叫醒业务线程，省得它盲睡到超时。
    std::condition_variable cv;
    // 最近一次「命中的请求有动静」的时刻，业务线程据此判断页面是不是已经取完了。
    std::chrono::steady_clock::time_point last_event {};
    // 响应头已到、路径命中的请求；等 loadingFinished 才能安全取响应体。
    std::unordered_set<std::string> watching;
    std::unordered_map<std::string, std::string> request_urls;
    std::vector<CapturedResponse> captured;
};

// 只从 attach 读 option 会写的字段（目前仅 clear_login）；其余用 ImportParam 默认值。
ImportParam LoadParam(MaaContext* context, const char* node_name)
{
    ImportParam out;
    if (!context || !node_name || *node_name == '\0') {
        return out;
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, node_name, buffer.Get())) {
        return out;
    }

    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || std::strlen(raw) == 0) {
        return out;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogWarn << "ZiplineImport: node data is not a json object" << VAR(node_name);
        return out;
    }

    const auto& obj = parsed->as_object();
    if (!obj.contains("attach") || !obj.at("attach").is_object()) {
        return out;
    }

    const auto& attach = obj.at("attach").as_object();
    out.clear_login = attach.get("clear_login", out.clear_login);
    return out;
}

// 取 URL query 里某个参数的值，取不到返回空串。
std::string QueryValue(const std::string& url, const std::string& key)
{
    const std::string needle = key + "=";
    size_t pos = url.find('?');
    if (pos == std::string::npos) {
        return {};
    }

    while (pos != std::string::npos) {
        const size_t start = pos + 1;
        if (url.compare(start, needle.size(), needle) == 0) {
            const size_t value_start = start + needle.size();
            const size_t value_end = url.find('&', value_start);
            return url.substr(value_start, value_end == std::string::npos ? std::string::npos : value_end - value_start);
        }
        pos = url.find('&', start);
    }
    return {};
}

// 请求 URL 会携带 roleId/serverId。它们只能在内存里参与账号匹配，任何日志都必须先打码；
// 同名参数可能重复出现，每一处都要打。
std::string RedactAccountQuery(std::string url)
{
    const std::string mask = "<redacted>";
    for (const std::string key : { "roleId", "serverId" }) {
        const std::string needle = key + "=";
        size_t pos = url.find('?');
        while (pos != std::string::npos) {
            const size_t start = pos + 1;
            if (url.compare(start, needle.size(), needle) == 0) {
                const size_t value_start = start + needle.size();
                const size_t value_end = url.find_first_of("&#", value_start);
                url.replace(value_start, value_end == std::string::npos ? std::string::npos : value_end - value_start, mask);
                pos = url.find('&', value_start + mask.size());
                continue;
            }
            pos = url.find('&', start);
        }
    }
    return url;
}

// 从标记列表响应里挑出滑索，按各自的 mapId 分组。template_ids 为空表示不过滤，全部收下——
// 哪些 templateId 是滑索由调用方给，这里不猜。
// mapId 每条标记自带，只在标记里缺它时才退回请求 URL 上的那个。
bool ParseMarks(
    const std::string& body,
    const std::vector<std::string>& template_ids,
    const std::string& fallback_map_id,
    std::unordered_map<std::string, std::vector<ZiplineMark>>& out)
{
    const auto parsed = json::parse(body);
    if (!parsed || !parsed->is_object()) {
        LogError << "ZiplineImport: response is not a json object";
        return false;
    }

    const auto& root = parsed->as_object();
    if (!root.contains("data") || !root.at("data").is_object()) {
        LogWarn << "ZiplineImport: response has no data object";
        return false;
    }

    const auto& data = root.at("data").as_object();
    if (!data.contains("saveMarks") || !data.at("saveMarks").is_array()) {
        LogWarn << "ZiplineImport: response has no saveMarks array";
        return false;
    }

    for (const auto& item : data.at("saveMarks").as_array()) {
        if (!item.is_object()) {
            continue;
        }
        const auto& mark_obj = item.as_object();

        ZiplineMark mark;
        mark.template_id = mark_obj.get("templateId", std::string {});
        if (!template_ids.empty() && std::find(template_ids.begin(), template_ids.end(), mark.template_id) == template_ids.end()) {
            continue;
        }

        std::string map_id = mark_obj.get("mapId", std::string {});
        if (map_id.empty()) {
            map_id = fallback_map_id;
        }
        if (map_id.empty()) {
            continue;
        }

        // 连线之类的标记没有 pos，落不到地图上，也就没法参与规划。
        if (!mark_obj.contains("pos") || !mark_obj.at("pos").is_object()) {
            continue;
        }
        const auto& pos = mark_obj.at("pos").as_object();
        mark.level_id = mark_obj.get("levelId", std::string {});
        mark.x = pos.get("x", 0.0);
        mark.y = pos.get("y", 0.0);
        mark.z = pos.get("z", 0.0);
        out[map_id].push_back(std::move(mark));
    }
    return true;
}

// 把抓到的响应并进磁盘记录。成功时带回条数、本次原始 roleId（仅供提示，不落盘）与磁盘账号数。
struct PersistResult
{
    size_t total = 0;
    size_t disk_account_count = 0;
    std::string role_id;
};

PersistResult PersistCaptured(const std::vector<CapturedResponse>& captured, const std::vector<std::string>& template_ids)
{
    PersistResult result;
    const std::filesystem::path path = ZiplineStore::DefaultPath();

    ZiplineStore store;
    if (!store.load(path)) {
        LogError << "ZiplineImport: load existing record failed, refuse to overwrite" << VAR(path);
        return result;
    }

    // 先把所有响应并到一起再落盘：同一张图可能被不止一条响应带回来，一条一次 replaceMap
    // 会让后一条把前一条整个抹掉。只有实际带回 saveMarks 的响应才参与账号判定，避免登录前
    // 那批 roleId 为空的公开空列表污染结果。
    std::unordered_map<std::string, std::vector<ZiplineMark>> by_map;
    std::unordered_set<std::string> role_ids;
    for (const auto& response : captured) {
        const std::string fallback_map_id = QueryValue(response.url, "mapId");
        std::unordered_map<std::string, std::vector<ZiplineMark>> all_marks;
        if (!ParseMarks(response.body, {}, fallback_map_id, all_marks) || all_marks.empty()) {
            continue;
        }

        const std::string role_id = QueryValue(response.url, "roleId");
        if (!IsValidRawUid(role_id)) {
            // 页面初始化期间可能先返回一份带旧登录态标记、但尚未绑定当前角色的响应。
            // 它无法安全归属账号，忽略并等待同一列表后续带 roleId 的正式响应。
            LogDebug << "ZiplineImport: ignore mark response without valid roleId" << VAR(role_id.size())
                     << VAR(RedactAccountQuery(response.url));
            continue;
        }
        role_ids.insert(role_id);

        std::unordered_map<std::string, std::vector<ZiplineMark>> filtered;
        if (!ParseMarks(response.body, template_ids, fallback_map_id, filtered)) {
            continue;
        }
        for (auto& [map_id, marks] : filtered) {
            auto& target = by_map[map_id];
            target.insert(target.end(), std::make_move_iterator(marks.begin()), std::make_move_iterator(marks.end()));
        }
    }

    if (role_ids.size() != 1) {
        LogError << "ZiplineImport: one import must contain exactly one roleId; refuse to persist" << VAR(role_ids.size());
        return result;
    }
    const std::string role_id = *role_ids.begin();
    const auto account_id = HashUidForAccount(role_id);
    if (!account_id) {
        LogError << "ZiplineImport: failed to derive account identity; refuse to persist";
        return result;
    }

    size_t total = 0;
    for (auto& [map_id, marks] : by_map) {
        // 并起来之后去掉完全重合的重复标记，顺带把落盘顺序定死。
        auto key = [](const ZiplineMark& m) {
            return std::tie(m.template_id, m.level_id, m.x, m.y, m.z);
        };
        std::sort(marks.begin(), marks.end(), [&key](const ZiplineMark& a, const ZiplineMark& b) { return key(a) < key(b); });
        marks.erase(
            std::unique(marks.begin(), marks.end(), [&key](const ZiplineMark& a, const ZiplineMark& b) { return key(a) == key(b); }),
            marks.end());

        // 逐 templateId 报数。标记类型只有编号没有名字，拿这行日志和游戏里数出来的数量对照，
        // 就能认出哪个编号是滑索、哪个是别的标记。
        std::unordered_map<std::string, size_t> by_template;
        for (const auto& mark : marks) {
            ++by_template[mark.template_id];
        }
        for (const auto& [template_id, count] : by_template) {
            LogInfo << "ZiplineImport: template" << VAR(map_id) << VAR(template_id) << VAR(count);
        }

        LogInfo << "ZiplineImport: captured map" << VAR(map_id) << VAR(marks.size());
        total += marks.size();

        ZiplineMapRecord record;
        record.account_id = *account_id;
        record.map_id = map_id;
        record.fetched_at = CurrentTimestamp();
        record.marks = std::move(marks);
        store.replaceMap(std::move(record));
    }

    if (total == 0) {
        return result;
    }
    if (!store.save(path)) {
        return result;
    }
    result.total = total;
    result.role_id = role_id;
    {
        std::unordered_set<std::string> accounts;
        for (const auto& record : store.maps()) {
            if (!record.account_id.empty()) {
                accounts.insert(record.account_id);
            }
        }
        result.disk_account_count = accounts.size();
    }
    return result;
}

void SubscribeSniffers(const std::shared_ptr<WebView2>& webview, const std::shared_ptr<SniffState>& state, std::string mark_list_path)
{
    // 响应头到达：只记下路径命中的请求，此刻响应体还没收完，不能取。
    webview->SubscribeDevToolsEvent("Network.responseReceived", [state, mark_list_path](std::string params_json) {
        const auto parsed = json::parse(params_json);
        if (!parsed || !parsed->is_object()) {
            return;
        }
        const auto& obj = parsed->as_object();
        const std::string request_id = obj.get("requestId", std::string {});
        if (request_id.empty() || !obj.contains("response") || !obj.at("response").is_object()) {
            return;
        }

        const std::string url = obj.at("response").as_object().get("url", std::string {});
        if (url.find(mark_list_path) == std::string::npos) {
            return;
        }

        {
            std::lock_guard<std::mutex> lock(state->mutex);
            state->watching.insert(request_id);
            state->request_urls[request_id] = url;
            state->last_event = std::chrono::steady_clock::now();
        }
        state->cv.notify_all();
    });

    // 响应体收完：此时 getResponseBody 才拿得到完整内容。
    webview->SubscribeDevToolsEvent("Network.loadingFinished", [webview_raw = webview.get(), state](std::string params_json) {
        const auto parsed = json::parse(params_json);
        if (!parsed || !parsed->is_object()) {
            return;
        }
        const std::string request_id = parsed->as_object().get("requestId", std::string {});

        std::string url;
        {
            std::lock_guard<std::mutex> lock(state->mutex);
            auto it = state->watching.find(request_id);
            if (it == state->watching.end()) {
                return;
            }
            state->watching.erase(it);
            url = state->request_urls[request_id];
            state->request_urls.erase(request_id);
        }

        json::object params;
        params["requestId"] = request_id;
        webview_raw->CallDevToolsMethod(
            "Network.getResponseBody",
            json::value(std::move(params)).dumps(),
            [state, url](bool ok, std::string result_json) {
                if (!ok) {
                    // 页面对同一接口常发两次请求，经 Service Worker / 缓存应答的那份取不到响应体，
                    // 属预期竞态，另一份会补上；真缺数据由收尾的 covered / expected 校验兜底。
                    LogDebug << "ZiplineImport: getResponseBody failed" << VAR(RedactAccountQuery(url));
                    return;
                }
                const auto parsed_body = json::parse(result_json);
                if (!parsed_body || !parsed_body->is_object()) {
                    return;
                }
                const auto& body_obj = parsed_body->as_object();
                if (body_obj.get("base64Encoded", false)) {
                    LogWarn << "ZiplineImport: response body is base64, not handled" << VAR(RedactAccountQuery(url));
                    return;
                }

                const std::string body = body_obj.get("body", std::string {});
                if (body.empty()) {
                    return;
                }
                LogDebug << "ZiplineImport: response body captured" << VAR(RedactAccountQuery(url)) << VAR(body.size());

                {
                    std::lock_guard<std::mutex> lock(state->mutex);
                    state->captured.push_back(CapturedResponse { .url = url, .body = body });
                    state->last_event = std::chrono::steady_clock::now();
                }
                state->cv.notify_all();
            });
    });
}

} // namespace

MaaBool MAA_CALL ZiplineImportActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    [[maybe_unused]] const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    if (!context) {
        LogError << "ZiplineImport: null context";
        return false;
    }

    ImportParam param = LoadParam(context, node_name);
    switch (gamesetting::DetectGameRegion()) {
    case gamesetting::Region::CN:
        param.url = kMapUrlCN;
        break;
    case gamesetting::Region::Global:
        param.url = kMapUrlGlobal;
        break;
    case gamesetting::Region::Unknown:
        LogError << "ZiplineImport: failed to resolve map URL from game region";
        return false;
    }

    auto webview = std::make_shared<WebView2>();
    webview->SetContextMenuEnabled(false);
    webview->SetTouchEmulation(true);
    webview->SetSize(param.width, param.height);
    webview->SetURL(param.url);
    webview->SetClearWebData(param.clear_login);
    if (!webview->Open()) {
        LogError << "ZiplineImport: webview open failed" << VAR(param.url);
        return false;
    }
    common::notice::Publish(context, common::notice::Text("zipline.import_sign_in_hint"));

    auto state = std::make_shared<SniffState>();
    SubscribeSniffers(webview, state, param.mark_list_path);
    // 订阅必须先于 Network.enable 排队，两者都走同一条 UI 线程待办，顺序有保证。
    webview->CallDevToolsMethod("Network.enable", "{}", [](bool ok, std::string) {
        if (!ok) {
            LogError << "ZiplineImport: Network.enable failed";
        }
    });
    // 关缓存，否则重跑时页面可能直接吃缓存：请求照样有、响应体照样取得到，导进来的却是上次那份。
    webview->CallDevToolsMethod("Network.setCacheDisabled", R"({"cacheDisabled":true})", [](bool ok, std::string) {
        if (!ok) {
            LogWarn << "ZiplineImport: Network.setCacheDisabled failed, the page may answer from cache";
        }
    });

    LogInfo << "ZiplineImport: waiting for the page to fetch its marks" << VAR(param.url) << VAR(param.mark_list_path)
            << VAR(param.timeout);

    MaaTasker* tasker = MaaContextGetTasker(context);
    const auto started_at = std::chrono::steady_clock::now();
    const auto deadline = started_at + std::chrono::milliseconds(param.timeout);

    // 关窗判据要看抓全了没有，而「该抓哪些图」以标定过的地图为准：没标定的地图本来也不参与规划。
    ZiplineFrames frames;
    frames.load(ZiplineFrames::DefaultPath());
    const std::vector<std::string> expected = frames.mapIds();

    std::vector<CapturedResponse> captured;
    std::unordered_set<std::string> covered;
    // 没有可归属账号的标记时继续等用户登录或选择角色。这行提示只打一次。
    bool signin_hint_logged = false;
    // 已登录判定：主地图列表「先空后非空」＝窗口里刚完成登录；首条就非空＝本来就登录着。
    // 关卡/基地子列表对多数用户恒为空，永远进不了非空集合，不会干扰判定。
    std::unordered_map<std::string, bool> list_first_parse_empty;
    bool login_transition_seen = false;
    bool signed_in_notice_decided = false;
    bool timed_out = false;
    while (true) {
        std::vector<CapturedResponse> fresh;
        bool inflight = false;
        std::chrono::steady_clock::time_point last_event {};
        {
            std::unique_lock<std::mutex> lock(state->mutex);
            if (state->captured.empty()) {
                // 有新响应会被立刻叫醒；这个节拍只用来复查停止请求和窗口状态。
                state->cv.wait_for(lock, std::chrono::milliseconds(kPollIntervalMs));
            }
            fresh.swap(state->captured);
            inflight = !state->watching.empty();
            last_event = state->last_event;
        }

        for (auto& response : fresh) {
            // 只为了知道这条覆盖了哪几张图，过滤留到落盘时再做。
            std::unordered_map<std::string, std::vector<ZiplineMark>> by_map;
            if (!ParseMarks(response.body, {}, QueryValue(response.url, "mapId"), by_map)) {
                captured.push_back(std::move(response));
                continue;
            }

            const bool non_empty = !by_map.empty();
            const bool account_ready = non_empty && IsValidRawUid(QueryValue(response.url, "roleId"));
            // 没有有效 roleId 的非空响应仍处在账号上下文初始化阶段，不能据此关窗或落盘。
            const std::string list_key = QueryValue(response.url, "mapId") + "|" + QueryValue(response.url, "levelId");
            const auto [it, inserted] = list_first_parse_empty.try_emplace(list_key, !account_ready);
            if (account_ready) {
                for (const auto& entry : by_map) {
                    covered.insert(entry.first);
                }
                if (!inserted && it->second) {
                    login_transition_seen = true;
                }
            }
            else if (non_empty) {
                LogDebug << "ZiplineImport: marks arrived before account identity, keep waiting" << VAR(RedactAccountQuery(response.url));
            }
            captured.push_back(std::move(response));
        }

        if (!covered.empty() && !signed_in_notice_decided) {
            signed_in_notice_decided = true;
            if (login_transition_seen) {
                LogInfo << "ZiplineImport: mark list went from empty to non-empty, the user just signed in";
            }
            common::notice::Publish(context, common::notice::Text("zipline.import_login_ok"));
        }

        const auto now = std::chrono::steady_clock::now();
        // 关窗只认「真抓到标记」：未登录时也有响应，拿收到响应当进展会在用户还在登录时把窗口关掉。
        if (!covered.empty() && !inflight) {
            const auto quiet = now - last_event;
            const bool all_covered =
                std::all_of(expected.begin(), expected.end(), [&covered](const std::string& id) { return covered.count(id) != 0; });
            if (all_covered && quiet >= std::chrono::milliseconds(kSettleMs)) {
                LogInfo << "ZiplineImport: marks captured, closing" << VAR(captured.size()) << VAR(covered.size());
                break;
            }
            if (quiet >= std::chrono::milliseconds(kIdleCloseMs)) {
                // 报出缺哪张图：接口变了、或者标定表里留着一张早就没有的图，都只有这行日志能看出来。
                std::string missing;
                for (const auto& id : expected) {
                    if (!covered.count(id)) {
                        missing += (missing.empty() ? "" : ",") + id;
                    }
                }
                LogWarn << "ZiplineImport: closing without every calibrated map" << VAR(missing) << VAR(captured.size());
                break;
            }
        }
        else if (!captured.empty() && !inflight && !signin_hint_logged && now - last_event >= std::chrono::milliseconds(kIdleCloseMs)) {
            signin_hint_logged = true;
            LogInfo << "ZiplineImport: no account-scoped marks yet, waiting for sign-in or role selection" << VAR(captured.size());
        }
        if (now >= deadline) {
            LogWarn << "ZiplineImport: timed out waiting for the mark list";
            timed_out = true;
            break;
        }
        if (tasker && MaaTaskerStopping(tasker)) {
            LogInfo << "ZiplineImport: stop requested";
            break;
        }
        // 用户自己关了窗口也算结束，不必等满超时。
        if (!webview->IsOpened()) {
            LogInfo << "ZiplineImport: window closed by user";
            break;
        }
    }
    // Close() 会等 UI 线程退出，只能在业务线程上调，CDP 回调里调就是自己等自己。
    webview->Close();

    if (timed_out) {
        common::notice::Publish(context, common::notice::Text("zipline.import_timeout"));
    }

    if (covered.empty()) {
        // 没抓到同时具有标记和账号身份的响应，不能安全归属后落盘。
        LogWarn << "ZiplineImport: no account-scoped marks captured, was the page signed in with a role selected?" << VAR(captured.size());
        return false;
    }

    const PersistResult persisted = PersistCaptured(captured, param.template_ids);
    LogInfo << "ZiplineImport: done" << VAR(captured.size()) << VAR(persisted.total);
    if (persisted.total > 0) {
        common::notice::Publish(
            context,
            common::notice::Text(
                "zipline.import_done",
                { std::stoll(persisted.role_id),
                  static_cast<int64_t>(persisted.total),
                  static_cast<int64_t>(persisted.disk_account_count) }));
    }
    return persisted.total > 0;
}

} // namespace zipline
