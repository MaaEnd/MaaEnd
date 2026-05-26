#pragma once

#include <filesystem>
#include <string_view>

#include "BaseNavPack.h"
#include "BaseNavPlanner.h"

namespace navmesh
{

BaseNavLoadResult LoadBaseNavPack(const std::filesystem::path& path, std::string_view zone_name = {});

}
