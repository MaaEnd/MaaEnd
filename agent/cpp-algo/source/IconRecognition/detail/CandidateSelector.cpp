#include "CandidateSelector.h"

#include <algorithm>
#include <stdexcept>
#include <unordered_set>

namespace iconrecognition::detail
{
namespace
{

struct ParsedFilter
{
    std::string_view storage_kind;
    std::string_view category_type;
};

ParsedFilter ParseFilter(std::string_view filter, std::string_view field)
{
    const auto separator = filter.find(':');
    if (separator == std::string_view::npos || filter.find(':', separator + 1) != std::string_view::npos) {
        throw std::invalid_argument(std::string(field) + " must use storageKind:categoryType");
    }
    const std::string_view storage_kind = filter.substr(0, separator);
    const std::string_view category_type = filter.substr(separator + 1);
    if (storage_kind.empty() || category_type.empty()) {
        throw std::invalid_argument(std::string(field) + " must use non-empty storageKind:categoryType");
    }
    return { storage_kind, category_type };
}

bool MatchesFilter(const TemplateRecord& record, std::string_view filter, std::string_view field)
{
    const auto parsed = ParseFilter(filter, field);
    return parsed.storage_kind == record.storage_kind && (parsed.category_type == "*" || parsed.category_type == record.category_type);
}

void ValidateKnownFilters(const std::vector<PreparedTemplate>& all, const std::vector<std::string>& filters, std::string_view field)
{
    ValidateCandidateFilterList(filters, field);
    for (const auto& filter : filters) {
        if (!std::ranges::any_of(all, [&](const auto& templ) { return MatchesFilter(templ.record, filter, field); })) {
            throw std::invalid_argument(std::string(field) + " contains unknown filter: " + filter);
        }
    }
}

void ValidateKnownIDs(const std::vector<PreparedTemplate>& all, const std::vector<std::string>& item_ids, std::string_view field)
{
    for (const auto& item_id : item_ids) {
        if (!std::ranges::any_of(all, [&](const auto& templ) { return templ.record.item_id == item_id; })) {
            throw std::invalid_argument(std::string(field) + " contains unknown item_id: " + item_id);
        }
    }
}

bool MatchesAnyFilter(const TemplateRecord& record, const std::vector<std::string>& filters, std::string_view field)
{
    return std::ranges::any_of(filters, [&](const auto& filter) { return MatchesFilter(record, filter, field); });
}

} // namespace

void ValidateCandidateFilterList(const std::vector<std::string>& filters, std::string_view field)
{
    for (const auto& filter : filters) {
        static_cast<void>(ParseFilter(filter, field));
    }
}

std::vector<PreparedTemplate> SelectCandidateTemplates(
    const std::vector<PreparedTemplate>& all,
    const CandidateFilter& candidates,
    const std::vector<std::string>& defaults)
{
    const auto& base_filters = candidates.item_filters.empty() ? defaults : candidates.item_filters;
    ValidateKnownFilters(all, base_filters, "item_filters");
    ValidateKnownFilters(all, candidates.additional_item_filters, "additional_item_filters");
    ValidateKnownIDs(all, candidates.item_ids, "item_ids");
    ValidateKnownIDs(all, candidates.excluded_item_ids, "excluded_item_ids");

    const std::unordered_set<std::string> requested_ids(candidates.item_ids.begin(), candidates.item_ids.end());
    const std::unordered_set<std::string> excluded_ids(candidates.excluded_item_ids.begin(), candidates.excluded_item_ids.end());
    std::vector<PreparedTemplate> result;
    result.reserve(all.size());
    for (const auto& templ : all) {
        const bool base_match = MatchesAnyFilter(templ.record, base_filters, "item_filters")
                                && (requested_ids.empty() || requested_ids.contains(templ.record.item_id));
        const bool additional_match = MatchesAnyFilter(templ.record, candidates.additional_item_filters, "additional_item_filters");
        if ((base_match || additional_match) && !excluded_ids.contains(templ.record.item_id)) {
            result.push_back(templ);
        }
    }
    if (result.empty()) {
        throw std::invalid_argument("candidate filters selected no candidate templates");
    }
    return result;
}

} // namespace iconrecognition::detail
