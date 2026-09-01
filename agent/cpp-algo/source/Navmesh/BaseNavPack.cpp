#include <algorithm>
#include <optional>
#include <utility>

#include "BaseNavPack.h"

#include <cstring>

namespace navmesh
{

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
    std::vector<BaseNavSection> sections)
{
    BaseNavPack pack;
    pack.path_ = std::move(path);
    pack.zones_ = std::move(zones);
    pack.vertices_ = std::move(vertices);
    pack.triangles_ = std::move(triangles);
    pack.links_ = std::move(links);
    pack.surfaces_ = std::move(surfaces);
    pack.off_mesh_links_ = std::move(off_mesh_links);
    pack.sections_ = std::move(sections);
    return pack;
}

}

const std::vector<BaseNavZone>& BaseNavPack::zones() const
{
    return zones_;
}

const std::vector<BaseNavVertex>& BaseNavPack::vertices() const
{
    return vertices_;
}

const std::vector<BaseNavTriangle>& BaseNavPack::triangles() const
{
    return triangles_;
}

const std::vector<BaseNavLink>& BaseNavPack::links() const
{
    return links_;
}

const std::vector<BaseNavSurface>& BaseNavPack::surfaces() const
{
    return surfaces_;
}

const std::vector<BaseNavOffMeshLink>& BaseNavPack::offMeshLinks() const
{
    return off_mesh_links_;
}

const BaseNavZone* BaseNavPack::findZone(uint16_t zone_id) const
{
    const auto iter = std::find_if(zones_.begin(), zones_.end(), [zone_id](const BaseNavZone& zone) { return zone.zone_id == zone_id; });
    return iter == zones_.end() ? nullptr : &*iter;
}

const BaseNavZone* BaseNavPack::findZoneByName(const std::string& name) const
{
    const auto iter = std::find_if(zones_.begin(), zones_.end(), [&name](const BaseNavZone& zone) { return zone.name == name; });
    return iter == zones_.end() ? nullptr : &*iter;
}

uint16_t BaseNavPack::geometryZoneId(uint16_t zone_id) const
{
    const BaseNavZone* zone = findZone(zone_id);
    return zone != nullptr && IsTierZone(*zone) ? static_cast<uint16_t>(zone->component_count) : zone_id;
}

std::optional<BaseNavBaseProjection> BaseNavPack::projectToBase(const std::string& zone_name, double x, double y) const
{
    const BaseNavZone* zone = findZoneByName(zone_name);
    if (zone == nullptr) {
        return std::nullopt;
    }
    if (!IsTierZone(*zone)) {
        // Geometry zone: python's geometry_zone_id() returns self and applies no affine.
        return BaseNavBaseProjection { zone, x, y, false };
    }
    const BaseNavZone* parent = findZone(static_cast<uint16_t>(zone->component_count));
    if (parent == nullptr) {
        return std::nullopt;
    }
    // base = s * tier + t, with transform = {sx, tx, sy, ty}.
    const std::array<float, 4>& t = zone->transform;
    return BaseNavBaseProjection {
        parent,
        static_cast<double>(t[0]) * x + static_cast<double>(t[1]),
        static_cast<double>(t[2]) * y + static_cast<double>(t[3]),
        true,
    };
}

float BaseNavPack::floorYForZoneName(const std::string& zone_name) const
{
    const BaseNavZone* zone = findZoneByName(zone_name);
    if (zone == nullptr || zone->floor_y <= kBaseNavFloorYValidMin) {
        return kBaseNavFloorYNone;
    }
    return zone->floor_y;
}

const char* ToString(BaseNavLoadStatus status)
{
    switch (status) {
    case BaseNavLoadStatus::Success:
        return "success";
    case BaseNavLoadStatus::FileOpenFailed:
        return "file_open_failed";
    case BaseNavLoadStatus::FileReadFailed:
        return "file_read_failed";
    case BaseNavLoadStatus::InvalidMagic:
        return "invalid_magic";
    case BaseNavLoadStatus::UnsupportedVersion:
        return "unsupported_version";
    case BaseNavLoadStatus::InvalidOffset:
        return "invalid_offset";
    case BaseNavLoadStatus::InvalidSize:
        return "invalid_size";
    case BaseNavLoadStatus::DuplicateZone:
        return "duplicate_zone";
    case BaseNavLoadStatus::HashMismatch:
        return "hash_mismatch";
    case BaseNavLoadStatus::ZoneNotFound:
        return "zone_not_found";
    }
    return "unknown";
}

void BaseNavPack::releaseLinks()
{
    // swap 空容器才还得掉容量: `= {}` 会挑上 initializer_list 赋值, 只把 size 清零。
    std::vector<BaseNavLink>().swap(links_);
}

const BaseNavSection* BaseNavPack::section(const char (&tag)[5]) const
{
    for (const BaseNavSection& s : sections_) {
        if (std::memcmp(s.tag.data(), tag, 4) == 0) {
            return &s;
        }
    }
    return nullptr;
}

}
