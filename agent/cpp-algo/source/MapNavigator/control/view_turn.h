#pragma once

#include "../Backend/backend.h"
#include "../sensing/sensitivity_observer.h"

namespace mapnavigator
{

int YawUnitsAtLeastOne(const InputBackend& backend, const sensitivity::SensitivityRun& sensitivity, double delta_deg);

bool SendYawDeltaAndNoteTurn(InputBackend* backend, sensitivity::SensitivityRun* sensitivity, int dx, int dy);

} // namespace mapnavigator
