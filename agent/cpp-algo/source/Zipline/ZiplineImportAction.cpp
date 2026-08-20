#include "ZiplineImportAction.h"

#include <algorithm>
#include <chrono>
#include <condition_variable>
#include <memory>
#include <mutex>
#include <string>
#include <tuple>
#include <unordered_map>
#include <unordered_set>
#include <vector>

#include <meojson/json.hpp>

#include <MaaUtils/Logger.h>

#include "../Common/WebView2.h"
#include "ZiplineFrames.h"
#include "ZiplineStore.h"

namespace zipline
{

namespace
{

constexpr const char* kDefaultMapUrl = "https://game.skland.com/map/endfield";
// 标记列表接口的路径片段。只匹配路径，避免被 query 里的参数顺序影响。
constexpr const char* kMarkListPathFragment = "/map/mark/list";
constexpr int64_t kDefaultTimeoutMs = 300000;
constexpr int kPollIntervalMs = 200;
// 标定过的地图全抓到之后再静默这么久就收工，留一点余量给同批次的最后几条。
constexpr int kSettleMs = 1200;
// 抓到了一些但凑不齐标定过的地图时的兜底：静默这么久也收工，不干等满超时。
constexpr int kIdleCloseMs = 20000;
constexpr int kDefaultWindowWidth = 960;
constexpr int kDefaultWindowHeight = 640;
// 日志里回显响应体的上限，够看清结构又不至于把整份标记列表刷进日志。
constexpr size_t kBodyLogPreviewBytes = 256;

struct ImportParam
{
    std::string url = kDefaultMapUrl;
    int64_t timeout = kDefaultTimeoutMs;
    int width = kDefaultWindowWidth;
    int height = kDefaultWindowHeight;
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

bool ParseParam(const char* raw, ImportParam& out)
{
    if (!raw || *raw == '\0') {
        return true;
    }

    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        LogError << "ZiplineImport: param is not a json object" << VAR(raw);
        return false;
    }

    const auto& obj = parsed->as_object();
    out.url = obj.get("url", out.url);
    out.timeout = obj.get("timeout", out.timeout);
    out.width = obj.get("width", out.width);
    out.height = obj.get("height", out.height);

    if (obj.contains("template_ids") && obj.at("template_ids").is_array()) {
        for (const auto& item : obj.at("template_ids").as_array()) {
            if (item.is_string()) {
                out.template_ids.push_back(item.as_string());
            }
        }
    }
    return true;
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

// 把抓到的响应并进磁盘记录。返回本次新写入的滑索条数。
size_t PersistCaptured(const std::vector<CapturedResponse>& captured, const std::vector<std::string>& template_ids)
{
    const std::filesystem::path path = ZiplineStore::DefaultPath();

    ZiplineStore store;
    if (!store.load(path)) {
        LogError << "ZiplineImport: load existing record failed, refuse to overwrite" << VAR(path);
        return 0;
    }

    // 先把所有响应并到一起再落盘：同一张图可能被不止一条响应带回来，
    // 一条一次 replaceMap 会让后一条把前一条整个抹掉。
    std::unordered_map<std::string, std::vector<ZiplineMark>> by_map;
    for (const auto& response : captured) {
        ParseMarks(response.body, template_ids, QueryValue(response.url, "mapId"), by_map);
    }

    size_t total = 0;
    for (auto& [map_id, marks] : by_map) {
        // 并起来之后去掉完全重合的重复标记，顺带把落盘顺序定死。
        auto key = [](const ZiplineMark& m) { return std::tie(m.template_id, m.level_id, m.x, m.y, m.z); };
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
        record.map_id = map_id;
        record.fetched_at = CurrentTimestamp();
        record.marks = std::move(marks);
        store.replaceMap(std::move(record));
    }

    if (total == 0) {
        return 0;
    }
    if (!store.save(path)) {
        return 0;
    }
    return total;
}

void SubscribeSniffers(const std::shared_ptr<WebView2>& webview, const std::shared_ptr<SniffState>& state)
{
    // 响应头到达：只记下路径命中的请求，此刻响应体还没收完，不能取。
    webview->SubscribeDevToolsEvent("Network.responseReceived", [state](std::string params_json) {
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
        if (url.find(kMarkListPathFragment) == std::string::npos) {
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
                    LogWarn << "ZiplineImport: getResponseBody failed" << VAR(url);
                    return;
                }
                const auto parsed_body = json::parse(result_json);
                if (!parsed_body || !parsed_body->is_object()) {
                    return;
                }
                const auto& body_obj = parsed_body->as_object();
                if (body_obj.get("base64Encoded", false)) {
                    LogWarn << "ZiplineImport: response body is base64, not handled" << VAR(url);
                    return;
                }

                const std::string body = body_obj.get("body", std::string {});
                if (body.empty()) {
                    return;
                }
                LogDebug << "ZiplineImport: body preview" << VAR(url) << VAR(body.substr(0, kBodyLogPreviewBytes));

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
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    if (!context) {
        LogError << "ZiplineImport: null context";
        return false;
    }

    ImportParam param;
    if (!ParseParam(custom_action_param, param)) {
        return false;
    }

    auto webview = std::make_shared<WebView2>();
    webview->SetContextMenuEnabled(false);
    webview->SetTouchEmulation(true);
    webview->SetSize(param.width, param.height);
    webview->SetURL(param.url);
    if (!webview->Open()) {
        LogError << "ZiplineImport: webview open failed" << VAR(param.url);
        return false;
    }

    auto state = std::make_shared<SniffState>();
    SubscribeSniffers(webview, state);
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

    LogInfo << "ZiplineImport: waiting for the page to fetch its marks" << VAR(param.url) << VAR(param.timeout);

    MaaTasker* tasker = MaaContextGetTasker(context);
    const auto deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(param.timeout);

    // 关窗判据要看抓全了没有，而「该抓哪些图」以标定过的地图为准：没标定的地图本来也不参与规划。
    ZiplineFrames frames;
    frames.load(ZiplineFrames::DefaultPath());
    const std::vector<std::string> expected = frames.mapIds();

    std::vector<CapturedResponse> captured;
    std::unordered_set<std::string> covered;
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
            if (ParseMarks(response.body, {}, QueryValue(response.url, "mapId"), by_map)) {
                for (const auto& entry : by_map) {
                    covered.insert(entry.first);
                }
            }
            captured.push_back(std::move(response));
        }

        const auto now = std::chrono::steady_clock::now();
        if (!captured.empty() && !inflight) {
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
        if (now >= deadline) {
            LogWarn << "ZiplineImport: timed out waiting for the mark list";
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

    if (captured.empty()) {
        LogWarn << "ZiplineImport: no mark list response captured";
        return false;
    }

    const size_t total = PersistCaptured(captured, param.template_ids);
    LogInfo << "ZiplineImport: done" << VAR(captured.size()) << VAR(total);
    return total > 0;
}

} // namespace zipline
