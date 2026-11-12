#pragma once

#include <array>
#include <cstdint>
#include <filesystem>
#include <memory>
#include <optional>
#include <string>
#include <vector>

namespace navmesh
{

enum class BaseNavLoadStatus
{
    Success,
    FileOpenFailed,
    FileReadFailed,
    InvalidMagic,
    UnsupportedVersion,
    InvalidOffset,
    InvalidSize,
    DuplicateZone,
    HashMismatch,
    ZoneNotFound,
};

// floor_y 哨兵值, 不大于 kBaseNavFloorYValidMin 即表示该区没有烘焙楼层高
inline constexpr float kBaseNavFloorYNone = -1.0e30F;
inline constexpr float kBaseNavFloorYValidMin = -1.0e29F;

// 吸附偏好的 floor_y 上下半宽 px, 带内无面时仍取最近面
inline constexpr float kBaseNavFloorBand = 12.0F;

struct BaseNavZone
{
    uint16_t zone_id = 0;
    uint16_t flags = 0;
    std::string name;
    uint32_t first_triangle = 0;
    uint32_t triangle_count = 0;
    uint32_t component_count = 0;
    float width = 0.0F;
    float height = 0.0F;
    std::array<float, 4> transform { 1.0F, 0.0F, 1.0F, 0.0F };
    // v3 起才有, 该层主楼面高度, 即世界 Y
    float floor_y = kBaseNavFloorYNone;
};

// tier 区没有三角, 父区 zone_id 存在 component_count, 仿射存在 transform = {sx, tx, sy, ty}
inline constexpr uint16_t kBaseNavTierFlag = 0x0001U;

inline bool IsTierZone(const BaseNavZone& zone)
{
    return (zone.flags & kBaseNavTierFlag) != 0U;
}

struct BaseNavBaseProjection
{
    const BaseNavZone* geometry_zone = nullptr;
    double x = 0.0;
    double y = 0.0;
    bool was_tier = false;
};

struct BaseNavVertex
{
    float u = 0.0F;
    float v = 0.0F;
    float height = 0.0F;
};

struct BaseNavTriangle
{
    std::array<uint32_t, 3> vertices { 0, 0, 0 };
    std::array<int32_t, 3> neighbors { -1, -1, -1 };
};

struct BaseNavLink
{
    uint32_t source = 0;
    uint32_t target = 0;
};

struct BaseNavSurface
{
    uint8_t area = 0;
    uint8_t poly_type = 0;
    uint32_t flags = 0;
};

inline constexpr uint8_t kBaseNavOffMeshRegular = 0;
inline constexpr uint8_t kBaseNavOffMeshExtended = 1;

// 两沿三角的哨兵值, 用于 v1 记录与未落上的锚点
inline constexpr uint32_t kBaseNavOffMeshTriangleNone = 0xFFFFFFFFU;
// bit0 = 起→终可走, bit1 = 终→起可走
inline constexpr uint8_t kBaseNavDirOkStartEnd = 0x01U;
inline constexpr uint8_t kBaseNavDirOkEndStart = 0x02U;

struct BaseNavOffMeshLink
{
    uint16_t zone_id = 0;
    uint8_t kind = kBaseNavOffMeshRegular;
    int32_t is_ext = 0;
    int32_t bidirectional = 0;
    int32_t area = 0;
    uint16_t link_type = 0;
    uint8_t direction = 0;
    float radius = 0.0F;
    float cost_modifier = 0.0F;
    std::array<BaseNavVertex, 4> points;
    // v2 起才有, v1 记录保持默认值
    uint8_t dir_ok = 0;
    uint32_t start_triangle = kBaseNavOffMeshTriangleNone;
    uint32_t end_triangle = kBaseNavOffMeshTriangleNone;
};

inline bool OffMeshLinkAnchored(const BaseNavOffMeshLink& link)
{
    return link.start_triangle != kBaseNavOffMeshTriangleNone && link.end_triangle != kBaseNavOffMeshTriangleNone
           && link.start_triangle != link.end_triangle;
}

// v4 起包尾的附加段, 由头里的段目录定位, 认不出的 tag 跳过
struct BaseNavSection
{
    std::array<char, 4> tag { ' ', ' ', ' ', ' ' };
    uint32_t flags = 0;
    // 只有要活过载入的段才有字节, 其余为空
    std::shared_ptr<const std::vector<uint8_t>> bytes;
};

struct BaseNavPack
{
    std::filesystem::path path;
    uint64_t build_hash = 0;
    // 解压后整份字节的 FNV-1a, 按区裁剪载入也按整份文件算
    uint64_t file_fnv = 0;
    std::vector<BaseNavZone> zones;
    std::vector<BaseNavVertex> vertices;
    std::vector<BaseNavTriangle> triangles;
    // 只供 BuildBaseNavPlanner 移走一次
    std::vector<BaseNavLink> links;
    std::vector<BaseNavSurface> surfaces;
    std::vector<BaseNavOffMeshLink> off_mesh_links;
    std::vector<BaseNavSection> sections;
};

const BaseNavZone* FindBaseNavZone(const BaseNavPack& pack, uint16_t zone_id);
const BaseNavZone* FindBaseNavZoneByName(const BaseNavPack& pack, const std::string& name);

// 非 tier 与未知 id 原样返回
uint16_t GeometryZoneId(const BaseNavPack& pack, uint16_t zone_id);

// 几何区原样返回, 未知区返回 std::nullopt
std::optional<BaseNavBaseProjection> ProjectToBase(const BaseNavPack& pack, const std::string& zone_name, double x, double y);

// 没有烘焙楼层高的区与未知区返回 kBaseNavFloorYNone
float FloorYForZoneName(const BaseNavPack& pack, const std::string& zone_name);

const BaseNavSection* FindBaseNavSection(const BaseNavPack& pack, const char (&tag)[5]);

struct BaseNavLoadResult
{
    BaseNavLoadStatus status = BaseNavLoadStatus::Success;
    std::string message;
    std::optional<BaseNavPack> pack;
};

inline bool LoadSucceeded(const BaseNavLoadResult& result)
{
    return result.status == BaseNavLoadStatus::Success && result.pack.has_value();
}

const char* ToString(BaseNavLoadStatus status);

}
