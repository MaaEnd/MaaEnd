#pragma once

#include <vector>

#include "TemplateTypes.h"

namespace iconrecognition::detail
{

struct Phase
{
    double x = 0.0;
    double y = 0.0;
};

std::vector<Phase> PhaseGrid();
std::vector<Phase> BoundaryExtensionPhases(Phase winning);
PreparedTemplate ShiftTemplate(const PreparedTemplate& source, Phase phase);

} // namespace iconrecognition::detail
