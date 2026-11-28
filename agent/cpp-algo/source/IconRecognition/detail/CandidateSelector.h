#pragma once

#include <string>
#include <string_view>
#include <vector>

#include "../IconRecognitionTypes.h"
#include "TemplateTypes.h"

namespace iconrecognition::detail
{

void ValidateCandidateFilterList(const std::vector<std::string>& filters, std::string_view field);

// 按本次分类范围和图标身份判断覆盖率，兼容分类整页扫描、IMS 展开的 ID 和同图标别名。
bool CandidateSelectionCoversScene(
    const std::vector<PreparedTemplate>& all,
    const std::vector<PreparedTemplate>& selected,
    const CandidateFilter& candidates,
    const std::vector<std::string>& defaults);

std::vector<PreparedTemplate> SelectCandidateTemplates(
    const std::vector<PreparedTemplate>& all,
    const CandidateFilter& candidates,
    const std::vector<std::string>& defaults,
    bool preserve_aliases = true);

} // namespace iconrecognition::detail
