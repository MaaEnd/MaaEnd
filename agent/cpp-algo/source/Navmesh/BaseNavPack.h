#pragma once

#include <array>
#include <cstdint>
#include <filesystem>
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

// Sentinel for BaseNavZone::floor_y: the zone has no baked dominant-floor height (the
// "…_Base" overview tiers, geometry zones, and any v2 pack which predates the field). The
// floor-aware snap treats anything <= kBaseNavFloorYValidMin as "no floor" and falls back to
// the floor-blind path.
inline constexpr float kBaseNavFloorYNone = -1.0e30F;
inline constexpr float kBaseNavFloorYValidMin = -1.0e29F;

// Half-width (in pixels == world Y == mesh height) of the band around a zone's baked floor_y that
// the floor-aware snap PREFERS. A surface within the band always outranks an off-band one; the band
// is a preference, never a hard gate (the nearest surface is still returned if nothing is in-band).
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
    // v3: dominant walkable-floor height (== world Y) the tier depicts; snap prefers triangles
    // within kBaseNavFloorBand of it so a multi-floor base resolves onto the right floor.
    float floor_y = kBaseNavFloorYNone;
};

// Bit0 of BaseNavZone::flags marks a "tier" zone: a 0-triangle zone that carries only the
// tier-template-pixel -> base-pixel affine onto its parent geometry zone (parent zone_id in
// component_count, affine in transform = {sx, tx, sy, ty}).
inline constexpr uint16_t kBaseNavTierFlag = 0x0001U;

inline bool IsTierZone(const BaseNavZone& zone)
{
    return (zone.flags & kBaseNavTierFlag) != 0U;
}

// Result of mapping a (zone_name, x, y) query onto the base-pixel frame via the navmesh's own
// baked affine. For a non-tier (geometry) zone the projection is the identity; for an unknown
// zone projectToBase returns std::nullopt (callers treat that as identity / no-op).
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
    uint32_t component_id = 0;
    float center_u = 0.0F;
    float center_v = 0.0F;
};

struct BaseNavLink
{
    uint32_t source = 0;
    uint32_t target = 0;
};

// v4 起包尾可以挂若干独立数据段,靠头里的段目录定位。四段原有数据一个字节不动,
// 认不出某个 tag 的读取器跳过它即可。
struct BaseNavSection
{
    std::array<char, 4> tag { ' ', ' ', ' ', ' ' };
    uint32_t flags = 0;
    std::vector<uint8_t> bytes;
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
    std::vector<BaseNavSection> sections);

}

class BaseNavPack
{
public:
    BaseNavPack() = default;

    const std::vector<BaseNavZone>& zones() const;
    const std::vector<BaseNavVertex>& vertices() const;
    const std::vector<BaseNavTriangle>& triangles() const;
    const std::vector<BaseNavLink>& links() const;
    const BaseNavZone* findZone(uint16_t zone_id) const;
    const BaseNavZone* findZoneByName(const std::string& name) const;

    // The zone that actually owns the triangles: a tier carries none, its parent's zone_id sits in
    // component_count. Non-tier and unknown ids map to themselves. Snap / A* / mesh all address this.
    uint16_t geometryZoneId(uint16_t zone_id) const;

    // Maps (zone_name, x, y) onto the base-pixel frame using the navmesh's OWN baked tier affine,
    // mirroring the python tool (is_tier -> geometry_zone_id + base = s*tier + t). A geometry zone
    // projects to identity; an unknown zone returns std::nullopt. Never consults any external table.
    std::optional<BaseNavBaseProjection> projectToBase(const std::string& zone_name, double x, double y) const;

    // Baked dominant-floor height of the named zone (a tier carries the floor it depicts; geometry / base /
    // unknown zones return kBaseNavFloorYNone -> floor-blind). The route planner feeds this into snap so a
    // multi-floor base resolves onto the right floor.
    float floorYForZoneName(const std::string& zone_name) const;

    // v4 附加段的原始字节;认不出的 tag 直接留着不解析。没有该段返回 nullptr。
    const BaseNavSection* section(const char (&tag)[5]) const;

private:
    friend BaseNavPack detail::MakeBaseNavPack(
        std::filesystem::path path,
        std::vector<BaseNavZone> zones,
        std::vector<BaseNavVertex> vertices,
        std::vector<BaseNavTriangle> triangles,
        std::vector<BaseNavLink> links,
        std::vector<BaseNavSection> sections);

    std::filesystem::path path_;
    std::vector<BaseNavZone> zones_;
    std::vector<BaseNavVertex> vertices_;
    std::vector<BaseNavTriangle> triangles_;
    std::vector<BaseNavLink> links_;
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
