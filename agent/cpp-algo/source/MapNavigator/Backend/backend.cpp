#include <utility>

#include <MaaUtils/Logger.h>

#include "../../MapLocator/MapLocateAction.h"
#include "../control/controller_info_utils.h"
#include "../control/controller_type_utils.h"
#include "../sensing/sensitivity_observer.h"
#include "Adb/adb_input_backend.h"
#include "Desktop/desktop_input_backend.h"
#include "Linux/linux_input_backend.h"
#include "backend.h"

namespace mapnavigator
{

namespace
{

bool send_view_delta(const InputBackend& backend, int dx, int dy)
{
    if (backend.kind == BackendKind::Adb) {
        return backend::adb::SendAdbViewDeltaSync(backend, dx, dy);
    }
    return backend::desktop::SendDesktopViewDeltaSync(backend, dx, dy);
}

} // namespace

InputBackend OpenInputBackend(MaaController* ctrl)
{
    std::string controller_type = DetectControllerType(ctrl);
    if (controller_type.empty()) {
        controller_type = "unknown";
    }

    if (IsAdbLikeControllerType(controller_type)) {
        return backend::adb::OpenAdbInputBackend(ctrl, std::move(controller_type), maplocator::getOrInitLocator());
    }

    if (IsLinuxControllerType(controller_type)) {
        return backend::Linux::OpenLinuxInputBackend(ctrl, std::move(controller_type));
    }

    return backend::desktop::OpenDesktopInputBackend(ctrl, std::move(controller_type), "win32");
}

void CloseInputBackend(InputBackend* backend)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::CloseAdbInputBackend(backend);
        return;
    }
    backend::desktop::CloseDesktopInputBackend(*backend);
}

bool UsesTouchBackend(const InputBackend& backend)
{
    return backend.kind == BackendKind::Adb;
}

bool IsInputBackendSupported(const InputBackend& backend)
{
    if (backend.kind == BackendKind::Adb) {
        return backend.ctrl != nullptr && backend.adb.has_locator;
    }
    return backend.ctrl != nullptr;
}

double TurnUnitsPerDegree(const InputBackend& backend)
{
    // 灵敏度校正系数只在这里和下面的俯仰处乘
    return backend.default_turn_units_per_degree * sensitivity::TurnUnitsScale();
}

double PitchRangeUnits(const InputBackend& backend)
{
    // 游戏视角灵敏度两轴共用, 偏航测出的倍率俯仰照乘
    return backend.default_pitch_range_units * sensitivity::TurnUnitsScale();
}

SteeringTransportProfile SteeringProfileOf(const InputBackend& backend)
{
    if (backend.kind == BackendKind::Adb) {
        return backend::adb::AdbSteeringProfile();
    }
    if (backend.kind == BackendKind::Linux) {
        return backend::Linux::LinuxSteeringProfile();
    }
    return backend::desktop::DesktopSteeringProfile();
}

bool SupportsSprint(const InputBackend&)
{
    return true;
}

bool SupportsWalkToggle(const InputBackend&)
{
    return true;
}

void SetMovementStateSync(InputBackend* backend, MovementState state, int delay_millis)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::SetAdbMovementStateSync(backend, state, delay_millis);
        return;
    }
    backend::desktop::SetDesktopMovementStateSync(backend, state, delay_millis);
}

void TriggerJumpSync(InputBackend* backend, int hold_millis)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::TriggerAdbJumpSync(*backend, hold_millis);
        return;
    }
    backend::desktop::TriggerDesktopJumpSync(backend, hold_millis);
}

void TriggerInteractSync(InputBackend* backend, int hold_millis)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::TriggerAdbInteractSync(*backend, hold_millis);
        return;
    }
    backend::desktop::TriggerDesktopInteractSync(backend, hold_millis);
}

void PulseForwardSync(InputBackend* backend, int hold_millis)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::PulseAdbForwardSync(backend, hold_millis);
        return;
    }
    backend::desktop::PulseDesktopForwardSync(backend, hold_millis);
}

void TriggerSprintSync(InputBackend* backend)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::TriggerAdbSprintSync(backend);
        return;
    }
    if (backend->kind == BackendKind::Linux) {
        backend::Linux::TriggerLinuxSprintSync(*backend);
        return;
    }
    backend::desktop::TriggerDesktopSprintSync(backend);
}

void ToggleWalkModeSync(InputBackend* backend)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::ToggleAdbWalkModeSync(backend);
        return;
    }
    backend::desktop::ToggleDesktopWalkModeSync(backend);
}

void ResetForwardWalkSync(InputBackend* backend, int release_millis)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::ResetAdbForwardWalkSync(backend, release_millis);
        return;
    }
    backend::desktop::ResetDesktopForwardWalkSync(backend, release_millis);
}

void ClickMouseLeftSync(InputBackend* backend)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::ClickAdbMouseLeftSync(*backend);
        return;
    }
    backend::desktop::ClickDesktopMouseLeftSync(backend);
}

void TriggerZiplineLaunchSync(InputBackend* backend)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::TriggerAdbZiplineLaunchSync(*backend);
        return;
    }
    backend::desktop::TriggerDesktopZiplineLaunchSync(backend);
}

void TriggerZiplineDismountSync(InputBackend* backend, int hold_millis)
{
    if (backend->kind == BackendKind::Adb) {
        backend::adb::TriggerAdbZiplineDismountSync(*backend, hold_millis);
        return;
    }
    backend::desktop::TriggerDesktopZiplineDismountSync(backend, hold_millis);
}

bool SendViewDeltaSync(InputBackend* backend, int dx, int dy)
{
    const bool sent = send_view_delta(*backend, dx, dy);
    const double units_per_degree = TurnUnitsPerDegree(*backend);
    if (sent && dx != 0 && units_per_degree > 0.0) {
        sensitivity::NoteTurnIssued(static_cast<double>(dx) / units_per_degree);
    }
    // 送失败的拖拽也可能已动了视角, 照样记一次
    if (dx != 0) {
        ++backend->heading_epoch;
    }
    return sent;
}

} // namespace mapnavigator
