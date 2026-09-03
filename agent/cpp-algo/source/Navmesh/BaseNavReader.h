#pragma once

#include <filesystem>
#include <string_view>

#include "BaseNavPack.h"
#include "BaseNavPlanner.h"

namespace navmesh
{

BaseNavLoadResult LoadBaseNavPack(const std::filesystem::path& path, std::string_view zone_name = {});

// 读整份文件字节,.gz 后缀的先解压。主包与旁包共用这一条读法。
BaseNavLoadResult ReadNavFileBytes(const std::filesystem::path& path, std::vector<uint8_t>* output);

}
