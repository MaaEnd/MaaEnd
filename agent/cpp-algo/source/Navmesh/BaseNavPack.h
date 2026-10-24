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

    bool anchored() const
    {
        return start_triangle != kBaseNavOffMeshTriangleNone && end_triangle != kBaseNavOffMeshTriangleNone
               && start_triangle != end_triangle;
    }
};

// v4 起包尾的附加段, 由头里的段目录定位, 认不出的 tag 跳过
struct BaseNavSection
{
    std::array<char, 4> tag { ' ', ' ', ' ', ' ' };
    uint32_t flags = 0;
    // 只有要活过载入的段才有字节, 其余为空
    std::shared_ptr<const std::vector<uint8_t>> bytes;
};

class BaseNavPack;

namespace detail
{

BaseNavPack MakeBaseNavPack(
    std::filesystem::path path,
    std::vector<BaseNavZone> zones,
    std::vector<BaseNavVertex> vertices,
    std::vector<BaseNavTriangle> triangles,
    std::vector<BaseNavLink> links,
    std::vector<BaseNavSurface> surfaces,
    std::vector<BaseNavOffMeshLink> off_mesh_links,
    std::vector<BaseNavSection> sections,
    uint64_t build_hash,
    uint64_t file_fnv);

}

class BaseNavPack
{
public:
    BaseNavPack() = default;

    const std::vector<BaseNavZone>& zones() const;
    const std::vector<BaseNavVertex>& vertices() const;
    const std::vector<BaseNavTriangle>& triangles() const;
    const std::vector<BaseNavLink>& links() const;
    const std::vector<BaseNavSurface>& surfaces() const;
    const std::vector<BaseNavOffMeshLink>& offMeshLinks() const;
    const BaseNavZone* findZone(uint16_t zone_id) const;
    const BaseNavZone* findZoneByName(const std::string& name) const;

    // 非 tier 与未知 id 原样返回
    uint16_t geometryZoneId(uint16_t zone_id) const;

    // 几何区原样返回, 未知区返回 std::nullopt
    std::optional<BaseNavBaseProjection> projectToBase(const std::string& zone_name, double x, double y) const;

    // 没有烘焙楼层高的区与未知区返回 kBaseNavFloorYNone
    float floorYForZoneName(const std::string& zone_name) const;

    const BaseNavSection* section(const char (&tag)[5]) const;

    // 只能在 BaseNavPlanner 建完 CSR 邻接后调用
    void releaseLinks();

    // fileFnv 是解压后整份字节的 FNV-1a, 按区裁剪载入也按整份文件算
    const std::filesystem::path& path() const { return path_; }

    uint64_t buildHash() const { return build_hash_; }

    uint64_t fileFnv() const { return file_fnv_; }

private:
    friend BaseNavPack detail::MakeBaseNavPack(
        std::filesystem::path path,
        std::vector<BaseNavZone> zones,
        std::vector<BaseNavVertex> vertices,
        std::vector<BaseNavTriangle> triangles,
        std::vector<BaseNavLink> links,
        std::vector<BaseNavSurface> surfaces,
        std::vector<BaseNavOffMeshLink> off_mesh_links,
        std::vector<BaseNavSection> sections,
        uint64_t build_hash,
        uint64_t file_fnv);

    std::filesystem::path path_;
    uint64_t build_hash_ = 0;
    uint64_t file_fnv_ = 0;
    std::vector<BaseNavZone> zones_;
    std::vector<BaseNavVertex> vertices_;
    std::vector<BaseNavTriangle> triangles_;
    std::vector<BaseNavLink> links_;
    std::vector<BaseNavSurface> surfaces_;
    std::vector<BaseNavOffMeshLink> off_mesh_links_;
    std::vector<BaseNavSection> sections_;
};

struct BaseNavLoadResult
{
    BaseNavLoadStatus status = BaseNavLoadStatus::Success;
    std::string message;
    std::optional<BaseNavPack> pack;

    bool ok() const { return status == BaseNavLoadStatus::Success && pack.has_value(); }
};

const char* ToString(BaseNavLoadStatus status);

}
