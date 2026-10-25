#pragma once

#include <filesystem>
#include <memory>
#include <string>
#include <string_view>
#include <utility>

#include "../../Navmesh/RecastNavRoute.h"

namespace mapnavigator
{

struct NaviParam;

std::filesystem::path ResolveNavmeshFilePath(const std::string& configured_path = {});
// 相对 exe 定位: <exe>/../data/MapNavigator/nogo_zones.json
std::filesystem::path NoGoTablePath();

struct CachedNavmesh
{
    navmesh::BaseNavPack pack;
    navmesh::BaseNavPlanner planner;
    mutable navmesh::recast::RecastNavEngine engine;

    explicit CachedNavmesh(navmesh::BaseNavPack nav_pack)
        : pack(std::move(nav_pack))
        , planner(pack)
        , engine(pack, planner, NoGoTablePath())
    {
        pack.releaseLinks();
    }
};

bool IsBaseNavZoneName(std::string_view zone_id);
std::string InferBaseNavZone(const std::string& locator_zone, const std::string& map_name);

void PreloadNavmesh(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone);
std::shared_ptr<CachedNavmesh> LoadCachedNavmesh(const std::filesystem::path& navmesh_path, const std::string& navmesh_zone);

struct ZoneNavmesh
{
    std::string navmesh_zone;
    std::shared_ptr<CachedNavmesh> mesh;
};

ZoneNavmesh LoadZoneNavmesh(const NaviParam& param, const std::string& locator_zone);

} // namespace mapnavigator
