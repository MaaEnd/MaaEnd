#include <utility>

#include <MaaUtils/Logger.h>

#include "../../navi_config.h"
#include "../Desktop/desktop_input_backend.h"
#include "wlroots_input_backend.h"

namespace mapnavigator::backend::wlroots
{

namespace
{

class WlrootsInputBackend final : public desktop::DesktopInputBackend
{
public:
    static constexpr int32_t kVkShift = 0x10;

    WlrootsInputBackend(MaaController* ctrl, std::string controller_type)
        : desktop::DesktopInputBackend(ctrl, std::move(controller_type), "wlroots", desktop::MakeDesktopKeyCodes())
    {
    }

    SteeringTransportProfile steering_transport_profile() const override
    {
        return SteeringTransportProfile {
            .supports_concurrent_move_and_look = true,
            .min_send_interval_ms = 16,
            .min_emit_delta_deg = 2.0,
            .max_batch_delta_deg = 18.0,
            .action_quiet_period_ms = 0,
        };
    }

    void TriggerSprintSync() override
    {
        MaaControllerWait(GetCtrl(), MaaControllerPostKeyDown(GetCtrl(), kVkShift));
        SleepIfNeeded(kActionSprintPressMs);
        MaaControllerWait(GetCtrl(), MaaControllerPostKeyUp(GetCtrl(), kVkShift));
    }
};

} // namespace

std::unique_ptr<IInputBackend> CreateWlrootsInputBackend(MaaController* ctrl, std::string controller_type)
{
    LogInfo << "MapNavigator input backend selected." << VAR(controller_type) << " backend=wlroots";
    return std::make_unique<WlrootsInputBackend>(ctrl, std::move(controller_type));
}

} // namespace mapnavigator::backend::wlroots
