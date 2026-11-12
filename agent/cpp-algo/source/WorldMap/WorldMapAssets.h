#pragma once

#include <filesystem>
#include <map>
#include <mutex>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

#include "WorldMapTypes.h"

namespace worldmap
{

struct WorldMapAssets
{
    // 控制器自己的资源层排在基础层之前, 同名图标由前者胜出
    std::vector<std::filesystem::path> image_roots;
    // 提供分层在底图上的摆放
    std::filesystem::path navmesh_pack;
    std::mutex mutex;
    std::map<std::string, cv::Mat> zone_bases;

    // 没有分层的区域也留一条空记录
    std::map<std::string, std::vector<TierPatch>> zone_tiers;

    // 加载失败的也留一条空记录，免得每帧都去翻一遍磁盘
    std::map<std::string, IconTemplate> icons;

    std::map<std::string, IconSpec> icon_table;
    bool icon_table_loaded = false;
};

// 控制器类型进程内恒定, 只有第一次调用会拿它建资源层次
WorldMapAssets* SharedAssets(std::string_view controller_type);

// 三个 Load 由调用方持有 assets->mutex
const cv::Mat* LoadZoneBase(WorldMapAssets* assets, const std::string& zone);
const std::vector<TierPatch>& LoadZoneTiers(WorldMapAssets* assets, const std::string& zone, const cv::Mat& base);
const IconTemplate* LoadIconTemplate(WorldMapAssets* assets, const std::string& name);

// 图标名 -> 认它的那套参数。表里没有这个名字就不给，调用方据此报错
std::optional<IconSpec> ResolveIcon(WorldMapAssets* assets, const std::string& name);

} // namespace worldmap
