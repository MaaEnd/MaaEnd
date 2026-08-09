#pragma once

#include <cstddef>
#include <optional>
#include <vector>

#include "TemplateTypes.h"

namespace iconrecognition::detail
{

struct RarityCandidatePasses
{
    std::vector<std::size_t> preferred_indices;
    std::vector<std::size_t> remaining_indices;
    bool prefiltered = false;
};

RarityCandidatePasses
    BuildRarityCandidatePasses(const std::vector<PreparedTemplate>& templates, std::optional<int> detected_rarity);

} // namespace iconrecognition::detail
