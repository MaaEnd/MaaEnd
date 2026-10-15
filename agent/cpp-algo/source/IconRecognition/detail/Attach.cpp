#include "Attach.h"

#include <algorithm>
#include <array>
#include <stdexcept>
#include <string>
#include <string_view>
#include <utility>

namespace iconrecognition::detail
{
namespace
{

constexpr std::array kArrayParameters { "item_ids",
                                        "item_filters",
                                        "additional_item_filters",
                                        "excluded_item_ids",
                                        "item_recheck_filters" };

constexpr std::string_view kAttachPrefix = "IconRecognition.";

} // namespace

json::object ApplyAttach(const json::object& parameters, const json::object& data)
{
    auto output = parameters;
    if (!data.contains("attach")) {
        return output;
    }
    if (!data.at("attach").is_object()) {
        throw std::invalid_argument("IconRecognition node attach must be an object");
    }
    json::object overrides;
    for (const auto& [key, flag] : data.at("attach").as_object()) {
        if (!key.starts_with(kAttachPrefix)) {
            continue;
        }
        const auto separator = key.find('.', kAttachPrefix.size());
        if (separator == std::string::npos || separator == kAttachPrefix.size() || separator + 1 == key.size()) {
            throw std::invalid_argument("IconRecognition attach key must be IconRecognition.<parameter>.<entry>: " + key);
        }
        const auto parameter = key.substr(kAttachPrefix.size(), separator - kAttachPrefix.size());
        if (std::ranges::find(kArrayParameters, parameter) == kArrayParameters.end()) {
            throw std::invalid_argument("IconRecognition attach does not support parameter: " + parameter);
        }
        if (!flag.is_boolean()) {
            throw std::invalid_argument("IconRecognition attach values must be booleans: " + key);
        }
        if (!overrides.contains(parameter)) {
            overrides[parameter] = json::array {};
        }
        if (flag.as_boolean()) {
            overrides[parameter].as_array().emplace_back(key.substr(separator + 1));
        }
    }
    for (auto& [parameter, entries] : overrides) {
        // 全部取消表示不启用自定义配置，保留调用者原本的参数。
        if (!entries.as_array().empty()) {
            output[parameter] = std::move(entries);
        }
    }
    return output;
}

} // namespace iconrecognition::detail
