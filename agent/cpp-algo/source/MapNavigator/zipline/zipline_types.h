#pragma once

#include <chrono>
#include <cmath>
#include <cstdint>
#include <optional>
#include <string>
#include <variant>
#include <vector>

#include "../navi_config.h"
#include "../navi_position.h"

namespace mapnavigator
{

// 身份靠 level_id 加世界坐标, x/y 是定位器像素坐标, 现发现的架子没有世界坐标只能按像素认
struct ZiplineNodeRef
{
    std::string level_id;
    double world_x = 0.0;
    double world_y = 0.0;
    double world_z = 0.0;
    bool has_world = false;
    double x = 0.0;
    double y = 0.0;
    double height = 0.0;

    bool sameTower(const ZiplineNodeRef& other) const
    {
        if (has_world && other.has_world) {
            return level_id == other.level_id
                   && std::hypot(world_x - other.world_x, world_y - other.world_y, world_z - other.world_z) < kZiplineTowerIdentityWu;
        }
        return std::hypot(x - other.x, y - other.y) < kZiplineLandingBandWu;
    }
};

struct ZiplineHopPlan
{
    ZiplineNodeRef mount;
    ZiplineNodeRef landing;
    // 同一上索架上其它索通向的架子, 挂错索时靠它们认出落在哪
    std::vector<ZiplineNodeRef> siblings;
    // 正数往上滑, 按两端世界坐标算好
    double planned_elevation_deg = 0.0;
    // 第一个就是航点本身的落脚点, 只有链首那一跳有
    std::vector<ZiplineMountSpot> mount_spots;
    bool chain_continues = false;
    // 只有链尾那一跳有值
    std::optional<ZiplineMountSpot> exit_aim;
};

enum class LandingClass
{
    Pending,
    AtTarget, // 首发和重试时是规划落点, 回程时是上索架
    AtOrigin,
    AtOther,
    AtStrayTower, // 滑行过但不在任何已知节点圈内, 脚下是未登记的架子
    Unknown,      // 没滑行过且不在已知圈内, 定位还没对上
};

struct ZiplineLaunch
{
    std::chrono::steady_clock::time_point fired_at {};
    double heading_deg = 0.0;
    int pitch_tier = 0;
    double aim_bias_deg = 0.0;
    LandingClass result = LandingClass::Pending;
    std::optional<ZiplineNodeRef> reached;
    std::optional<NaviPosition> stopped_at;
};

enum class HopOutcome
{
    Completed,
    WrongRope,   // 滑错过, 滑回后重试仍没到
    NoLaunch,    // 逐档俯仰都发射过仍未起滑, 索被挡或未通电
    NotMounted,  // 按键后没上架, 索和架子都不判死
    Unboardable, // 每个站位都没出提示, 不再当上索点
    Lost,        // 落地定位一直对不上
    Dismounted,  // 主动下索交给恢复
    AimFailed,   // 在架上瞄不准放弃
};

struct ZiplineHopRecord
{
    ZiplineHopPlan plan;
    std::vector<ZiplineLaunch> launches;
    HopOutcome outcome = HopOutcome::NoLaunch;
    // 从上索架指向到过的错误落点的方位
    std::vector<double> wrong_rope_bearings_deg;
    std::chrono::steady_clock::time_point began_at {};
    std::chrono::steady_clock::time_point ended_at {};
};

using HopLedger = std::vector<ZiplineHopRecord>;

inline bool IsZiplineRopeFailure(HopOutcome outcome)
{
    return outcome == HopOutcome::NoLaunch || outcome == HopOutcome::Lost || outcome == HopOutcome::WrongRope;
}

inline int32_t CountZiplineRopeFailures(const HopLedger& ledger)
{
    int32_t count = 0;
    for (const ZiplineHopRecord& record : ledger) {
        if (IsZiplineRopeFailure(record.outcome)) {
            ++count;
        }
    }
    return count;
}

enum class ZiplineStage
{
    Idle,
    Mounting,
    OnTower,
    Aiming,
    Fired,
    Riding,
    Landed,
    Classified,
    ExitAiming,
    ReturnAiming,
    Dismounting,
    Handoff,
    Failed,
};

enum class MountVerdict
{
    OnTower,
    OnGround,
    Unclear,
};

// fix 只在定位到、不是 held 且朝向有效时有值
struct ZiplineObservation
{
    std::optional<NaviPosition> fix;
    std::chrono::steady_clock::time_point at {};
};

struct HopCompleted
{
    NaviPosition at;
    bool still_on_tower = false;
};

struct ChainAbandoned
{
    const char* reason = "";
};

struct ReplanRequested
{
    bool still_on_tower = false;
    std::optional<ZiplineNodeRef> on_tower;
};

// 人已经下来了, 换下一个站位再上
struct NeedsReposition
{
};

using StageResult = std::variant<std::monostate, HopCompleted, ChainAbandoned, ReplanRequested, NeedsReposition>;

} // namespace mapnavigator
