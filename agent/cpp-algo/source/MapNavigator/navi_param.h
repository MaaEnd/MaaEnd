#pragma once

#include <cstdint>
#include <string>
#include <vector>

#include "navi_domain_types.h"

namespace mapnavigator
{

struct NaviParam
{
    std::string map_name;
    // 本次导航全程固定, 滑索途中也不变
    HeadingSource heading_source = HeadingSource::Character;
    std::vector<Waypoint> path;
    std::string navmesh_file;
    double navmesh_snap_radius = 5.0;
    int64_t arrival_timeout = 60000;
    double sprint_threshold = 16.0;
    // 关掉则整次导航不冲刺, 路线里的 SPRINT 点也跳过
    bool enable_sprint = true;
    bool enable_local_driver = true;
    // 起步 A* 开关, 关掉则起步直接照录制点走
    bool enable_bootstrap_navmesh = true;
    // 为真时实时定位换算到 navmesh 基础像素坐标
    bool normalize_position_via_navmesh = false;
    bool zipline_enabled = false;
    // 伪匿名账号标识, 滑索记录先按它隔离再按地图筛选, 为空则不用滑索
    std::string zipline_account_id;
    // 展开前的作者路线, path 是展开后的
    std::vector<Waypoint> authored_path;
    // 本次导航各跳的记录, 重展开时用来排除出过问题的索
    HopLedger zipline_ledger;
    // 为空则本次规划不用升降索
    std::vector<std::string> rope_interact_text;
    // 本次导航中认不出提示的升降索底端 uid, 后续规划不再选
    std::vector<uint32_t> rope_off;
};

} // namespace mapnavigator
