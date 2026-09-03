#include "notice.h"

#include <cctype>
#include <cstdlib>
#include <filesystem>
#include <unordered_map>
#include <unordered_set>

#include <MaaFramework/Instance/MaaContext.h>
#include <MaaUtils/Logger.h>
#include <meojson/json.hpp>

#include "../utils.h"
#include "JsoncFile.h"

namespace common::notice
{

namespace
{

constexpr const char* kDefaultLang = "zh_cn";
constexpr const char* kFocusNode = "_CPP_ALGO_NOTICE_";
constexpr const char* kLocaleRelativeDir = "locales/cpp-algo";

std::string ResolveLanguage()
{
    std::string lang;
#ifdef _MSC_VER
    // MSVC 把 getenv 判成 C4996，本项目又开了 /WX。
    char* buffer = nullptr;
    size_t buffer_size = 0;
    if (_dupenv_s(&buffer, &buffer_size, "PI_CLIENT_LANGUAGE") != 0 || buffer == nullptr) {
        return kDefaultLang;
    }
    lang.assign(buffer);
    std::free(buffer);
#else
    const char* raw = std::getenv("PI_CLIENT_LANGUAGE");
    if (raw == nullptr) {
        return kDefaultLang;
    }
    lang.assign(raw);
#endif

    for (char& ch : lang) {
        ch = static_cast<char>(std::tolower(static_cast<unsigned char>(ch)));
    }

    static const std::unordered_set<std::string> supported { "zh_cn", "zh_tw", "en_us", "ja_jp", "ko_kr" };
    return supported.count(lang) != 0 ? lang : kDefaultLang;
}

bool LoadInto(const std::filesystem::path& dir, const std::string& lang, std::unordered_map<std::string, std::string>& out)
{
    const std::filesystem::path path = dir / (lang + ".json");
    // 仓库内 JSONC 允许注释、尾逗号与 BOM，用公共 OpenJsoncFile 读取。
    const auto parsed = common::OpenJsoncFile(path);
    if (!parsed || !parsed->is_object()) {
        LogWarn << "Failed to parse notice locale file." << VAR(path);
        return false;
    }

    for (const auto& [key, value] : parsed->as_object()) {
        if (value.is_string()) {
            out[key] = value.as_string();
        }
    }
    return true;
}

const std::unordered_map<std::string, std::string>& Messages()
{
    static const std::unordered_map<std::string, std::string> messages = [] {
        std::unordered_map<std::string, std::string> loaded;
        // 装在 assets/locales 下，安装后落到包根的 locales/，所以从 exe 往上一级找。
        const std::filesystem::path dir = get_exe_dir() / ".." / kLocaleRelativeDir;
        const std::string lang = ResolveLanguage();
        // 先铺默认语言再覆盖目标语言，新加的 key 还没翻译时不至于变成裸 key。
        LoadInto(dir, kDefaultLang, loaded);
        if (lang != kDefaultLang) {
            LoadInto(dir, lang, loaded);
        }
        if (loaded.empty()) {
            LogWarn << "No notice locale loaded, notices will show raw keys." << VAR(dir) << VAR(lang);
        }
        else {
            LogInfo << "Notice locale loaded." << VAR(lang) << VAR(loaded.size());
        }
        return loaded;
    }();
    return messages;
}

} // namespace

std::string Text(const std::string& key, const std::vector<int64_t>& values)
{
    const auto& messages = Messages();
    const auto it = messages.find(key);
    if (it == messages.end()) {
        LogWarn << "Notice key missing from locale, falling back to the raw key." << VAR(key);
        return key;
    }

    const std::string& tmpl = it->second;
    std::string out;
    out.reserve(tmpl.size() + values.size() * 4);
    std::size_t next = 0;
    for (std::size_t i = 0; i < tmpl.size(); ++i) {
        if (tmpl[i] != '%' || i + 1 >= tmpl.size()) {
            out.push_back(tmpl[i]);
            continue;
        }
        if (tmpl[i + 1] == '%') {
            out.push_back('%');
            ++i;
        }
        else if (tmpl[i + 1] == 'd' && next < values.size()) {
            out += std::to_string(values[next++]);
            ++i;
        }
        else {
            out.push_back(tmpl[i]);
        }
    }
    return out;
}

void Publish(MaaContext* context, const std::string& content)
{
    if (context == nullptr || content.empty()) {
        LogWarn << "Skip publishing notice." << VAR(context == nullptr) << VAR(content.empty());
        return;
    }

    json::object node {
        { "focus", json::object { { "Node.Action.Starting", content } } },
        { "pre_delay", 0 },
        { "post_delay", 0 },
    };
    const std::string overrides = json::value(json::object { { kFocusNode, node } }).dumps();
    const MaaRect box {};
    if (MaaContextRunAction(context, kFocusNode, overrides.c_str(), &box, "") == MaaInvalidId) {
        LogWarn << "Failed to publish notice." << VAR(content);
    }
}

} // namespace common::notice
