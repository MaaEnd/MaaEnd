#include "MapNavigator.h"

#include <cstring>

#include <MaaUtils/Logger.h>

#include "navi_config.h"
#include "navi_controller.h"
#include "navi_param_parser.h"
#include "process_state.h"
#include "sensing/pipeline_node_reader.h"
#include "zipline/zipline_leg_planner.h"
#include "zipline/zipline_preference.h"

namespace mapnavigator
{

namespace
{

constexpr MaaBool kMaaTrue = 1;
constexpr MaaBool kMaaFalse = 0;

// 跨导航、跨 tasker 共用, 只此一份
NavProcessState g_process_state;

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
    if (!TryParseNaviParam(&param, custom_action_param)) {
        return kMaaFalse;
    }

    if (param.path.empty()) {
        return kMaaTrue;
    }

    ResolveInteractTextNodes(&param, context);
    // 读不出就留空, 本次不用升降索
    TryReadNodeInteractTexts(context, kRopeInteractTextNode, &param.rope_interact_text);
    ZiplineOutcome* zipline_outcome = ThreadZiplineOutcome();
    *zipline_outcome = {};
    param.zipline_enabled = ResolveZiplineEnabled(context, param.zipline_enabled);
    if (param.zipline_enabled) {
        param.zipline_account_id = ReadOrCaptureZiplineAccountId(context);
        if (!param.zipline_account_id.empty()) {
            NoticeClaimedLegacyZiplineRecords(context, ClaimLegacyZiplineRecords(param.zipline_account_id));
        }
    }

    // 规划在 navmesh 基础层上进行, 实时定位要先换算到基础像素坐标
    param.normalize_position_via_navmesh = true;

    const bool arrived = RunNavigation(&g_process_state, zipline_outcome, context, param);
    NoticeZiplineOutcome(&g_process_state.zipline_notices, context, *zipline_outcome);
    return arrived ? kMaaTrue : kMaaFalse;
}

} // namespace mapnavigator
