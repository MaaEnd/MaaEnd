#pragma once

#include <string>
#include <string_view>
#include <vector>

#include "../IconRecognitionTypes.h"
#include "TemplateTypes.h"

namespace iconrecognition::detail
{

void ValidateCandidateFilterList(const std::vector<std::string>& filters, std::string_view field);

std::vector<PreparedTemplate> SelectCandidateTemplates(
    const std::vector<PreparedTemplate>& all,
    const CandidateFilter& candidates,
    const std::vector<std::string>& defaults);

} // namespace iconrecognition::detail
