#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_config.h"
#include "../../navi_time.h"
#include "../Desktop/desktop_input_backend.h"
#include "../backend.h"
#include "linux_input_backend.h"

namespace mapnavigator::backend::Linux
{

namespace
{

constexpr int32_t kVkShift = 0x10;

} // namespace

InputBackend OpenLinuxInputBackend(MaaController* ctrl, std::string controller_type)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=linux";
    return desktop::MakeDesktopInputBackend(BackendKind::Linux, ctrl, std::move(controller_type), "linux");
}

SteeringTransportProfile LinuxSteeringProfile()
{
    return SteeringTransportProfile {
        .supports_concurrent_move_and_look = true,
        .min_send_interval_ms = 16,
        .min_emit_delta_deg = 2.0,
        .max_batch_delta_deg = 18.0,
        .action_quiet_period_ms = 0,
    };
}

void TriggerLinuxSprintSync(const InputBackend& backend)
{
    MaaControllerWait(backend.ctrl, MaaControllerPostKeyDown(backend.ctrl, kVkShift));
    SleepFor(kActionSprintPressMs);
    MaaControllerWait(backend.ctrl, MaaControllerPostKeyUp(backend.ctrl, kVkShift));
}

} // namespace mapnavigator::backend::Linux
