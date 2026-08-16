#include "RarityCandidates.h"

namespace iconrecognition::detail
{

RarityCandidatePasses BuildRarityCandidatePasses(const std::vector<PreparedTemplate>& templates, std::optional<int> detected_rarity)
{
    RarityCandidatePasses passes;
    passes.preferred_indices.reserve(templates.size());
    if (!detected_rarity) {
        for (std::size_t index = 0; index < templates.size(); ++index) {
            passes.preferred_indices.push_back(index);
        }
        return passes;
    }

    passes.remaining_indices.reserve(templates.size());
    for (std::size_t index = 0; index < templates.size(); ++index) {
        if (templates[index].record.rarity == *detected_rarity) {
            passes.preferred_indices.push_back(index);
        }
        else {
            passes.remaining_indices.push_back(index);
        }
    }

    // 没有形成真正的候选子集时，保持原有一次全量排名路径。
    if (passes.preferred_indices.empty() || passes.remaining_indices.empty()) {
        passes.preferred_indices.clear();
        passes.remaining_indices.clear();
        for (std::size_t index = 0; index < templates.size(); ++index) {
            passes.preferred_indices.push_back(index);
        }
        return passes;
    }
    passes.prefiltered = true;
    return passes;
}

} // namespace iconrecognition::detail
