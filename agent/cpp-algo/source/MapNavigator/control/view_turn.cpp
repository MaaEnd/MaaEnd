#include "view_turn.h"

namespace mapnavigator
{

bool SendYawDeltaAndNoteTurn(InputBackend* backend, sensitivity::SensitivityRun* sensitivity, int dx, int dy)
{
    const bool sent = SendViewDeltaSync(backend, dx, dy);
    const double units_per_degree = TurnUnitsPerDegree(*backend, *sensitivity);
    if (sent && dx != 0 && units_per_degree > 0.0) {
        sensitivity::NoteTurnIssued(sensitivity, static_cast<double>(dx) / units_per_degree);
    }
    // 送失败的拖拽也可能已动了视角, 照样记一次
    if (dx != 0) {
        AdvanceHeadingEpoch(backend);
    }
    return sent;
}

} // namespace mapnavigator
