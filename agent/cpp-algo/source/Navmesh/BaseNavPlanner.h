#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "BaseNavPack.h"
#include "NavmeshTypes.h"

namespace navmesh
{

struct BaseNavSnapResult
{
    uint32_t triangle = 0;
    WorldPoint point;
    double distance = 0.0;
};

// 运行期在卡住处放的禁行圆, 端点落在圆内仍照常规划
struct BaseNavNoGoDisc
{
    WorldPoint center;
    double radius = 0.0;
};

struct BaseNavRouteRequest
{
    std::string zone_name;
    WorldPoint start;
    WorldPoint goal;
    std::vector<BaseNavNoGoDisc> no_go_discs;
    // 两端共用的楼层高, 下面的逐端值设置后优先
    float floor_y = kBaseNavFloorYNone;
    // 逐端楼层高, 大于 kBaseNavFloorYValidMin 时覆盖 floor_y
    float start_floor_y = kBaseNavFloorYNone;
    float goal_floor_y = kBaseNavFloorYNone;
    // 终点所在叠层面的高度, 决定搜索停在哪个 span, 未设置时不限
    float goal_deck_y = kBaseNavFloorYNone;
    // 起点所在面的确切高度, 设置后起点吸附到最接近该高度的面
    float start_deck_y = kBaseNavFloorYNone;
    // 为空表示不用升降索, 有值时列出用不了的升降索底端 uid
    std::optional<std::vector<uint32_t>> rope_off;
};

enum class BaseNavRouteStatus
{
    Success,
    ZoneNotFound,
    Unreachable,
    // 端点在人工标注的禁行区内, 不许任何兜底绕行
    NoGo,
};

struct BaseNavRouteResult
{
    BaseNavRouteStatus status = BaseNavRouteStatus::Unreachable;
    std::string error;
    WorldPath path;
    double cost = 0.0;
    // 起终点分属不同连通区时, 两区边界上的最近点对
    std::optional<WorldPoint> gap_start;
    std::optional<WorldPoint> gap_goal;
    std::optional<double> gap_distance;
};

inline bool RouteSucceeded(const BaseNavRouteResult& result)
{
    return result.status == BaseNavRouteStatus::Success;
}

// 每区一张铺满包围盒的格偏移表, 格内三角按下标升序, 包围盒外视为空格
struct BaseNavBinGrid
{
    uint16_t zone_id = 0;
    int32_t bx0 = 0;
    int32_t by0 = 0;
    int32_t nx = 0;
    int32_t ny = 0;
    std::vector<uint32_t> offsets; // nx*ny+1, 行主序 (bin_x - bx0) * ny + (bin_y - by0)
    std::vector<uint32_t> triangles;
};

// 由 BaseNavPack 建出的索引, 查询时与建它的包一起传入
struct BaseNavPlanner
{
    std::vector<uint16_t> triangle_zones;
    // 落在三角邻居槽上的链接按槽存一位, 其余按 (source<<32|target) 升序存进 bridge_links
    std::vector<uint8_t> neighbor_link_bits;
    std::vector<uint64_t> bridge_links;
    std::vector<uint32_t> natural_component_ids;
    std::vector<uint32_t> natural_component_sizes;
    std::vector<BaseNavBinGrid> bin_grids;
};

// links 传 std::move(pack.links), 建完随参数释放
BaseNavPlanner BuildBaseNavPlanner(const BaseNavPack& pack, std::vector<BaseNavLink> links);

std::optional<BaseNavSnapResult> SnapToMesh(
    const BaseNavPlanner& planner,
    const BaseNavPack& pack,
    uint16_t zone_id,
    const WorldPoint& point,
    double radius,
    float floor_y = kBaseNavFloorYNone);

bool PointOnMesh(const BaseNavPlanner& planner, const BaseNavPack& pack, uint16_t zone_id, const WorldPoint& point);

// half_width 为 0 只验中线, seed_height 是 a 点所在面的高度, 缺省时叠层处取最低面
bool IsRouteSegmentDrivable(
    const BaseNavPlanner& planner,
    const BaseNavPack& pack,
    uint16_t zone_id,
    const WorldPoint& a,
    const WorldPoint& b,
    double half_width = 0.0,
    std::optional<double> seed_height = std::nullopt);

// 仅含通过通行判据的链接
bool HasLink(const BaseNavPlanner& planner, const BaseNavPack& pack, uint32_t source, uint32_t target);

bool IsSmallIslandTriangle(const BaseNavPlanner& planner, uint32_t triangle_index);
std::optional<std::array<WorldPoint, 2>> ClosestEdgeBridgePoints(const BaseNavPack& pack, uint32_t lhs, uint32_t rhs);

// 返回 point±radius 覆盖的各格内的三角, 可能重复
std::vector<uint32_t> CandidateTriangles(const BaseNavPlanner& planner, uint16_t zone_id, const WorldPoint& point, double radius);
std::array<WorldPoint, 3> TrianglePoints(const BaseNavPack& pack, uint32_t triangle_index);
double TriangleHeight(const BaseNavPack& pack, uint32_t triangle_index);
uint32_t ComponentId(const BaseNavPlanner& planner, uint32_t triangle_index);
uint32_t ComponentSize(const BaseNavPlanner& planner, uint32_t triangle_index);

const char* ToString(BaseNavRouteStatus status);

}
