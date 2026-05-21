#pragma once

#include <filesystem>

#include "BaseNavPack.h"
#include "BaseNavPlanner.h"

namespace navmesh
{

BaseNavLoadResult LoadBaseNavPack(const std::filesystem::path& path);

}
