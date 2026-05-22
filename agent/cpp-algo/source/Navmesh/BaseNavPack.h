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
};

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

class BaseNavPack;

namespace detail
{

BaseNavPack MakeBaseNavPack(
    std::filesystem::path path,
    std::vector<BaseNavZone> zones,
    std::vector<BaseNavVertex> vertices,
    std::vector<BaseNavTriangle> triangles,
    std::vector<BaseNavLink> links);

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

private:
    friend BaseNavPack detail::MakeBaseNavPack(
        std::filesystem::path path,
        std::vector<BaseNavZone> zones,
        std::vector<BaseNavVertex> vertices,
        std::vector<BaseNavTriangle> triangles,
        std::vector<BaseNavLink> links);

    std::filesystem::path path_;
    std::vector<BaseNavZone> zones_;
    std::vector<BaseNavVertex> vertices_;
    std::vector<BaseNavTriangle> triangles_;
    std::vector<BaseNavLink> links_;
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
