#include "MapNavigator.h"

#include <cstring>

#include <MaaUtils/Logger.h>

#include "navi_config.h"
#include "navi_controller.h"
#include "navi_param_parser.h"
#include "sensing/pipeline_node_reader.h"
#include "zipline/zipline_leg_planner.h"
#include "zipline/zipline_preference.h"

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

    ResolveInteractTextNodes(context, param);
    // 读不出就留空, 本次不用升降索
    TryReadNodeInteractTexts(context, kRopeInteractTextNode, &param.rope_interact_text);
    ResetZiplineOutcome();
    param.zipline_enabled = ResolveZiplineEnabled(context, param.zipline_enabled);
    if (param.zipline_enabled) {
        param.zipline_account_id = ResolveZiplineAccountId(context);
    }

    // 规划在 navmesh 基础层上进行, 实时定位要先换算到基础像素坐标
    param.normalize_position_via_navmesh = true;

    NaviController controller(context);
    const bool arrived = controller.navigate(param);
    NoticeZiplineOutcome(context);
    return arrived ? kMaaTrue : kMaaFalse;
}

} // namespace mapnavigator
