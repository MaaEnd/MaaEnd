#include <algorithm>
#include <optional>

#include "BaseNavPack.h"

#include <cstring>

namespace navmesh
{

const BaseNavZone* FindBaseNavZone(const BaseNavPack& pack, uint16_t zone_id)
{
    const auto iter =
        std::find_if(pack.zones.begin(), pack.zones.end(), [zone_id](const BaseNavZone& zone) { return zone.zone_id == zone_id; });
    return iter == pack.zones.end() ? nullptr : &*iter;
}

const BaseNavZone* FindBaseNavZoneByName(const BaseNavPack& pack, const std::string& name)
{
    const auto iter = std::find_if(pack.zones.begin(), pack.zones.end(), [&name](const BaseNavZone& zone) { return zone.name == name; });
    return iter == pack.zones.end() ? nullptr : &*iter;
}

uint16_t GeometryZoneId(const BaseNavPack& pack, uint16_t zone_id)
{
    const BaseNavZone* zone = FindBaseNavZone(pack, zone_id);
    return zone != nullptr && IsTierZone(*zone) ? static_cast<uint16_t>(zone->component_count) : zone_id;
}

std::optional<BaseNavBaseProjection> ProjectToBase(const BaseNavPack& pack, const std::string& zone_name, double x, double y)
{
    const BaseNavZone* zone = FindBaseNavZoneByName(pack, zone_name);
    if (zone == nullptr) {
        return std::nullopt;
    }
    if (!IsTierZone(*zone)) {
        return BaseNavBaseProjection { zone, x, y, false };
    }
    const BaseNavZone* parent = FindBaseNavZone(pack, static_cast<uint16_t>(zone->component_count));
    if (parent == nullptr) {
        return std::nullopt;
    }
    // base = s * tier + t, transform = {sx, tx, sy, ty}
    const std::array<float, 4>& t = zone->transform;
    return BaseNavBaseProjection {
        parent,
        static_cast<double>(t[0]) * x + static_cast<double>(t[1]),
        static_cast<double>(t[2]) * y + static_cast<double>(t[3]),
        true,
    };
}

float FloorYForZoneName(const BaseNavPack& pack, const std::string& zone_name)
{
    const BaseNavZone* zone = FindBaseNavZoneByName(pack, zone_name);
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

const BaseNavSection* FindBaseNavSection(const BaseNavPack& pack, const char (&tag)[5])
{
    for (const BaseNavSection& s : pack.sections) {
        if (std::memcmp(s.tag.data(), tag, 4) == 0) {
            return &s;
        }
    }
    return nullptr;
}

}
