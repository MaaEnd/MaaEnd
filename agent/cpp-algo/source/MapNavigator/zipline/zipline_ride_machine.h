#pragma once

#include <chrono>
#include <cstdint>
#include <optional>
#include <vector>

#include "zipline_types.h"

namespace mapnavigator
{

// 定义在 zipline_action.cpp
struct ZiplineIo;

// hint_nodes 是这一帧定位优先搜索的位置
ZiplineObservation ZiplineIoObserveTracked(ZiplineIo* io, const std::vector<ZiplineNodeRef>& hint_nodes);
ZiplineObservation ZiplineIoObserveGlobal(ZiplineIo* io, const std::vector<ZiplineNodeRef>& hint_nodes);
// 只在上索与下索确认期间调, 每次都要跑识别, 开销比一帧定位高
MountVerdict ZiplineIoCheckMounted(ZiplineIo* io);
void ZiplineIoResetTracking(ZiplineIo* io);
// 每个动作同步发出、发完即回, 到没到位靠后续观测
bool ZiplineIoResetPitchToMaximum(ZiplineIo* io);
// 只发一个后端批次, 返回实际发出的度数, 发不出去返回空
std::optional<double> ZiplineIoTurnYaw(ZiplineIo* io, double delta_deg);
bool ZiplineIoLowerPitchTo(ZiplineIo* io, double elevation_deg);
// 识别不到交互提示时不发按键, 返回是否识别到
bool ZiplineIoPressMount(ZiplineIo* io);
void ZiplineIoFireLaunch(ZiplineIo* io);
void ZiplineIoDismount(ZiplineIo* io);
void ZiplineIoWait(ZiplineIo* io, int32_t ms);

// reached 返回命中的节点, 圈外且滑行过时按定位坐标现造一根未登记架子
LandingClass ClassifyLanding(
    const std::optional<NaviPosition>& fix,
    const ZiplineNodeRef& origin,
    const ZiplineNodeRef& target,
    const std::vector<ZiplineNodeRef>& known,
    bool riding_entered,
    ZiplineNodeRef* reached);

struct YawAim
{
    std::optional<double> prev_heading;
    int stable_heading_hits = 0;
    bool turn_pending = false;
    std::chrono::steady_clock::time_point turn_sent_at {};
    double turn_ref_heading = 0.0;
    double turn_cmd_deg = 0.0;
    // 实际转角与指令之比, 不小于 1
    std::optional<double> yaw_response_ratio;
};

int32_t YawTurnBudgetMs(double residual);
// 保留转向响应比
void RestartYawAim(YawAim* aim);
// 转向生效且朝向读稳后才返回朝向, 同时结束在途转向
std::optional<double> PollYawAim(YawAim* aim, const ZiplineObservation& obs, std::chrono::steady_clock::time_point now);
std::optional<double> IssueYawTurn(YawAim* aim, ZiplineIo* io, double heading, double residual, std::chrono::steady_clock::time_point now);
double YawResponseRatio(const YawAim& aim);

struct LandingWatch
{
    std::optional<NaviPosition> launch_fix;
    int miss_streak = 0;
    int settle_hits = 0;
    bool riding_entered = false;
    std::optional<std::chrono::steady_clock::time_point> minimap_back_at;
    std::optional<std::chrono::steady_clock::time_point> unknown_deadline;
};

void ArmLandingWatch(LandingWatch* watch, const std::optional<NaviPosition>& fix);

struct DismountWatch
{
    int presses = 0;
    std::optional<NaviPosition> stable_pos;
    int stable_hits = 0;
};

void ArmDismountWatch(DismountWatch* watch);

struct ZiplineRide
{
    ZiplineStage stage = ZiplineStage::Idle;
    std::chrono::steady_clock::time_point stage_entered_at {};
    ZiplineHopPlan plan;
    ZiplineHopRecord record;
    bool hop_open = false;
    HopLedger ledger;

    // 回程时 origin 是滑到的那根架子, target 是上索架
    ZiplineNodeRef origin;
    ZiplineNodeRef target;
    double seed_elevation_deg = 0.0;
    // 相对落点方位的瞄准偏置, 一跳内跨发射保留
    double aim_bias_deg = 0.0;
    bool pitch_lowered = false;
    int pitch_tier = 0;
    bool returning = false;
    int hop_retry_count = 0;
    int mount_presses = 0;
    int on_ground_hits = 0;
    std::vector<ZiplineNodeRef> discovered_towers;
    // 交回导航后人还站着的架子
    std::optional<ZiplineNodeRef> parked_on;

    std::optional<NaviPosition> last_fix;
    LandingWatch landing;

    YawAim aim;
    std::optional<int32_t> aim_budget_ms;

    ZiplineMountSpot exit_aim;
    int32_t exit_aim_budget_ms = 0;

    DismountWatch dismount;
    StageResult pending_exit;
};

bool ZiplineHopResumable(const ZiplineRide& ride, const ZiplineHopPlan& plan);
void CloseOpenHop(ZiplineRide* ride, std::chrono::steady_clock::time_point now);
// 人不在架子上时调用方要先按上索键再调
void BeginZiplineHop(ZiplineRide* ride, const ZiplineHopPlan& plan, std::chrono::steady_clock::time_point now);
StageResult TickZiplineHop(ZiplineRide* ride, ZiplineIo* io);
void MarkZiplineMountUnreachable(ZiplineRide* ride, const ZiplineHopPlan& plan);

bool ZiplineOnTower(const ZiplineRide& ride);
std::optional<ZiplineNodeRef> ZiplineTowerUnderfoot(const ZiplineRide& ride);

void RecordDismissedHop(ZiplineRide* ride);
// 清掉这一跳, 账本留着
void ResetZiplineHop(ZiplineRide* ride);
void ResetZiplineNavigation(ZiplineRide* ride);

} // namespace mapnavigator
