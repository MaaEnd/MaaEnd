#pragma once

#include "../Backend/backend.h"
#include "../sensing/sensitivity_observer.h"

namespace mapnavigator
{

bool SendYawDeltaAndNoteTurn(InputBackend* backend, sensitivity::SensitivityRun* sensitivity, int dx, int dy);

} // namespace mapnavigator
