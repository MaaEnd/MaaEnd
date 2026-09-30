#include "CandidateAttach.h"

#include <cstring>

#include "../utils.h"

namespace iconrecognition
{
namespace
{

bool IsReservedAttachKey(std::string_view key)
{
    static constexpr std::string_view kReserved[] = {
        "ready",
        "visited",
        "scrollbar_top",
        "scrollbar_bottom",
        "scrollbar_missing",
        "zip",
    };
    return std::ranges::any_of(kReserved, [&](const std::string_view reserved) { return key == reserved; });
}

bool IsAttachEntryEnabled(const json::value& value)
{
    if (value.is_boolean()) {
        return value.as_boolean();
    }
    if (value.is_null()) {
        return false;
    }
    if (value.is_string()) {
        return !value.as_string().empty();
    }
    if (value.is_array()) {
        return !value.as_array().empty();
    }
    return false;
}

bool LooksLikeItemFilterKey(std::string_view key)
{
    const auto separator = key.find(':');
    return separator != std::string_view::npos && key.find(':', separator + 1) == std::string_view::npos;
}

void AppendUnique(std::vector<std::string>& target, std::string value)
{
    if (value.empty()) {
        return;
    }
    if (std::ranges::find(target, value) != target.end()) {
        return;
    }
    target.push_back(std::move(value));
}

void AppendUniqueRange(std::vector<std::string>& target, const std::vector<std::string>& values)
{
    for (const auto& value : values) {
        AppendUnique(target, value);
    }
}

CandidateFilter ReadCandidatesFromAttach(const json::object& attach)
{
    CandidateFilter from_attach;
    std::vector<std::string> keys;
    keys.reserve(attach.size());
    for (const auto& [key, _] : attach) {
        keys.push_back(key);
    }
    std::ranges::sort(keys);

    for (const auto& key : keys) {
        if (IsReservedAttachKey(key)) {
            continue;
        }
        const json::value& value = attach.at(key);
        if (!IsAttachEntryEnabled(value)) {
            continue;
        }
        if (LooksLikeItemFilterKey(key)) {
            AppendUnique(from_attach.additional_item_filters, key);
        }
        else {
            AppendUnique(from_attach.item_ids, key);
        }
    }
    return from_attach;
}

} // namespace

void MergeAttachIntoCandidates(CandidateFilter& candidates, const json::object& attach)
{
    const CandidateFilter from_attach = ReadCandidatesFromAttach(attach);
    AppendUniqueRange(candidates.item_ids, from_attach.item_ids);
    AppendUniqueRange(candidates.item_filters, from_attach.item_filters);
    AppendUniqueRange(candidates.additional_item_filters, from_attach.additional_item_filters);
}

std::optional<json::object> TryReadNodeAttach(MaaContext* context, const char* node_name)
{
    if (context == nullptr || node_name == nullptr || std::strlen(node_name) == 0) {
        return std::nullopt;
    }

    ScopedStringBuffer buffer;
    if (buffer.Get() == nullptr || !MaaContextGetNodeData(context, node_name, buffer.Get())) {
        return std::nullopt;
    }
    const char* raw = MaaStringBufferGet(buffer.Get());
    if (raw == nullptr || raw[0] == '\0') {
        return std::nullopt;
    }
    const auto parsed = json::parse(raw);
    if (!parsed || !parsed->is_object()) {
        return std::nullopt;
    }
    const json::object& node = parsed->as_object();
    if (!node.contains("attach") || !node.at("attach").is_object()) {
        return std::nullopt;
    }
    return node.at("attach").as_object();
}

} // namespace iconrecognition
