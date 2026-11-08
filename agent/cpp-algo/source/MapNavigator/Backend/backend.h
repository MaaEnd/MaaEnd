#pragma once

#include <cstdint>
#include <string>

#include <MaaFramework/MaaAPI.h>

#include "Adb/adb_input_backend.h"
#include "Desktop/desktop_input_backend.h"
#include "backend_types.h"

namespace mapnavigator
{

struct InputBackend
{
    BackendKind kind = BackendKind::Desktop;
    MaaController* ctrl = nullptr;
    std::string controller_type;
    std::string unsupported_reason;
    double default_turn_units_per_degree = 0.0;
    double default_pitch_range_units = 0.0;
    backend::desktop::DesktopBackend desktop;
    backend::adb::AdbBackend adb;
    // 每次可能动过镜头就加一, 取位时记下的值仍等于当前值, 那次的镜头朝向才可当参考
    uint64_t heading_epoch = 0;
};

InputBackend OpenInputBackend(MaaController* ctrl);
void CloseInputBackend(InputBackend* backend);

bool UsesTouchBackend(const InputBackend& backend);
bool IsInputBackendSupported(const InputBackend& backend);
double TurnUnitsPerDegree(const InputBackend& backend);
double PitchRangeUnits(const InputBackend& backend);
SteeringTransportProfile SteeringProfileOf(const InputBackend& backend);
bool SupportsSprint(const InputBackend& backend);
// 返回 false 时走路请求不生效, 保持跑步速度
bool SupportsWalkToggle(const InputBackend& backend);

void SetMovementStateSync(InputBackend* backend, MovementState state, int delay_millis);
void TriggerJumpSync(InputBackend* backend, int hold_millis);
void TriggerInteractSync(InputBackend* backend, int hold_millis);
void PulseForwardSync(InputBackend* backend, int hold_millis);
void TriggerSprintSync(InputBackend* backend);
void ToggleWalkModeSync(InputBackend* backend);
void ResetForwardWalkSync(InputBackend* backend, int release_millis);
void ClickMouseLeftSync(InputBackend* backend);
void TriggerZiplineLaunchSync(InputBackend* backend);
void TriggerZiplineDismountSync(InputBackend* backend, int hold_millis);
bool SendViewDeltaSync(InputBackend* backend, int dx, int dy);

} // namespace mapnavigator
