#include "MapNavigator.h"

#include <cstring>

#include <MaaUtils/Logger.h>

#include "navi_controller.h"
#include "navi_param_parser.h"

namespace mapnavigator
{

namespace
{

constexpr MaaBool kMaaTrue = 1;
constexpr MaaBool kMaaFalse = 0;

} // namespace

MaaBool MAA_CALL MapNavigateActionRun(
    MaaContext* context,
    [[maybe_unused]] MaaTaskId task_id,
    [[maybe_unused]] const char* node_name,
    [[maybe_unused]] const char* custom_action_name,
    const char* custom_action_param,
    [[maybe_unused]] MaaRecoId reco_id,
    [[maybe_unused]] const MaaRect* box,
    [[maybe_unused]] void* trans_arg)
{
    if (custom_action_param == nullptr || std::strlen(custom_action_param) == 0) {
        return kMaaTrue;
    }

    LogInfo << "MapNavigateActionRun param string: " << custom_action_param;

    NaviParam param;
    if (!TryParseNaviParam(custom_action_param, param)) {
        return kMaaFalse;
    }

    if (param.path.empty()) {
        return kMaaTrue;
    }

    // Native entry plans on the navmesh base mesh, so normalize live fixes onto the navmesh base-pixel
    // frame using the navmesh's own baked tier affine. The Compatible entry leaves this off (its frame
    // is the MapTracker base-px), keeping that path byte-identical.
    param.normalize_position_via_navmesh = true;

    NaviController controller(context);
    return controller.Navigate(param) ? kMaaTrue : kMaaFalse;
}

} // namespace mapnavigator
