#include "WorldMapAssets.h"

#include <algorithm>
#include <cmath>
#include <format>
#include <regex>
#include <utility>
#include <vector>

#include <MaaUtils/ImageIo.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <meojson/json.hpp>

#include "../Common/JsoncFile.h"
#include "MapNavigator/control/controller_type_utils.h"
#include "MapNavigator/planning/navmesh_cache.h"
#include "Navmesh/BaseNavReader.h"
#include "WorldMapImage.h"

namespace fs = std::filesystem;

namespace worldmap
{

namespace
{

constexpr const char* kIconDir = "SceneManager";

// 选中分层时大地图其余部分压暗后的亮度比例
constexpr double kTierDimRatio = 0.33;
// 分层四周带上的底图宽度，单位底图像素
constexpr int kTierMargin = 300;
// 游戏在地图外画的灰底亮度，用来填底图的透明处
constexpr int kOffMapGray = 34;

// 图标表与它认的那些模板同目录，一起走资源层次：某一端的图标画得不一样时，
// 那一层放自己的模板和自己的阈值即可
constexpr const char* kIconTableName = "MapIcons.json";

// 表里一条图标的原样。留空的项按 SpotConfig 的缺省走
struct IconEntry
{
    std::vector<std::string> templates;
    std::vector<double> scale;
    double scale_step = 0.0;
    double threshold = 0.0;
    double gate = 0.0;
    double radius = 0.0;
    double gold_ratio = 0.0;
    bool occluded_by_player = false;

    MEO_JSONIZATION(
        templates,
        MEO_OPT scale,
        MEO_OPT scale_step,
        MEO_OPT threshold,
        MEO_OPT gate,
        MEO_OPT radius,
        MEO_OPT gold_ratio,
        MEO_OPT occluded_by_player);
};

// 图标本体上最厚实的那一点，相对模板中心。取掩膜最大连通块（本体），
// 再取块内离边缘最远处：定居点核心的模板是「金雕像 + 等级牌」两块夹着一片空档，
// 框中心正落在空档里，交整框出去就会点到图标旁边的地面上。
// 距离并列时取最靠近本体重心的那个——全不透明的模板整条中轴线并列，取首会偏到一边
cv::Point2d body_hotspot(const cv::Mat& mask, const cv::Size& size)
{
    const cv::Point2d center((size.width - 1) / 2.0, (size.height - 1) / 2.0);
    if (mask.empty() || mask.size() != size) {
        return { 0.0, 0.0 };
    }

    cv::Mat labels;
    cv::Mat stats;
    cv::Mat centroids;
    const int count = cv::connectedComponentsWithStats(mask, labels, stats, centroids, 8, CV_32S);
    int body = 0;
    int body_area = 0;
    for (int i = 1; i < count; ++i) {
        const int area = stats.at<int>(i, cv::CC_STAT_AREA);
        if (area > body_area) {
            body_area = area;
            body = i;
        }
    }
    if (body == 0) {
        return { 0.0, 0.0 };
    }

    // 补一圈零再做距离变换，免得贴着模板边的图标被当成离边缘很远
    cv::Mat padded;
    cv::copyMakeBorder(labels == body, padded, 1, 1, 1, 1, cv::BORDER_CONSTANT, cv::Scalar(0));
    cv::Mat dist;
    cv::distanceTransform(padded, dist, cv::DIST_L2, 5);

    double peak = 0.0;
    cv::minMaxLoc(dist, nullptr, &peak);
    const cv::Point2d anchor(centroids.at<double>(body, 0), centroids.at<double>(body, 1));
    cv::Point2d best = anchor;
    double best_gap = -1.0;
    for (int y = 0; y < size.height; ++y) {
        for (int x = 0; x < size.width; ++x) {
            // 差半像素以内算并列
            if (dist.at<float>(y + 1, x + 1) < peak - 0.5) {
                continue;
            }
            const double gap = std::hypot(x - anchor.x, y - anchor.y);
            if (best_gap < 0.0 || gap < best_gap) {
                best_gap = gap;
                best = cv::Point2d(x, y);
            }
        }
    }
    return best - center;
}

cv::Mat base_gray(const cv::Mat& image)
{
    cv::Mat gray = ToGray(image);
    if (image.channels() == 4) {
        cv::Mat alpha;
        cv::extractChannel(image, alpha, 3);
        gray.setTo(kOffMapGray, alpha == 0);
    }
    return gray;
}

// 区域底图是目录下含 Base、不含 Tier 的那张。各区域文件名不统一，靠这条规则收敛
std::optional<fs::path> find_zone_base_file(const fs::path& zone_dir)
{
    std::error_code ec;
    if (!fs::is_directory(zone_dir, ec)) {
        return std::nullopt;
    }

    for (const auto& entry : fs::directory_iterator(zone_dir, ec)) {
        if (!entry.is_regular_file()) {
            continue;
        }
        const fs::path& path = entry.path();
        if (path.extension() != ".png") {
            continue;
        }
        std::string stem = MAA_NS::path_to_utf8_string(path.stem());
        std::string lowered = stem;
        std::ranges::transform(lowered, lowered.begin(), [](unsigned char c) { return static_cast<char>(std::tolower(c)); });
        if (lowered.find("base") != std::string::npos && lowered.find("tier") == std::string::npos) {
            return path;
        }
    }
    return std::nullopt;
}

// 导航包里的分层区名：LvNNNTierMMM 记作 <区域>_L<N>_<M>，其余用文件名
std::string tier_zone_name(const std::string& zone, const fs::path& file)
{
    const std::string stem = MAA_NS::path_to_utf8_string(file.stem());
    if (file.extension() != ".png" || stem.find("Tier") == std::string::npos) {
        return {};
    }
    static const std::regex kLayerFile(R"(Lv(\d+)Tier(\d+))");
    std::smatch match;
    if (std::regex_match(stem, match, kLayerFile)) {
        return std::format("{}_L{}_{}", zone, std::stoi(match[1].str()), std::stoi(match[2].str()));
    }
    return stem;
}

const navmesh::BaseNavZone* find_tier_placement(const std::vector<navmesh::BaseNavZone>& zones, const std::string& name)
{
    for (const navmesh::BaseNavZone& zone : zones) {
        if (navmesh::IsTierZone(zone) && zone.name == name) {
            return &zone;
        }
    }
    return nullptr;
}

TierPatch make_tier_patch(const std::string& name, const cv::Mat& image, const navmesh::BaseNavZone& placement, const cv::Mat& base)
{
    const auto& [sx, tx, sy, ty] = placement.transform;
    const cv::Rect rect(
        static_cast<int>(std::lround(tx)),
        static_cast<int>(std::lround(ty)),
        static_cast<int>(std::lround(image.cols * sx)),
        static_cast<int>(std::lround(image.rows * sy)));

    cv::Mat gray = ToGray(image);
    cv::Mat opaque(gray.size(), CV_8UC1, cv::Scalar(255));
    if (image.channels() == 4) {
        std::vector<cv::Mat> planes;
        cv::split(image, planes);
        opaque = planes[3] >= 128;
    }
    if (gray.size() != rect.size()) {
        cv::resize(gray, gray, rect.size(), 0.0, 0.0, cv::INTER_AREA);
        cv::resize(opaque, opaque, rect.size(), 0.0, 0.0, cv::INTER_NEAREST);
    }

    const cv::Rect bounds(0, 0, base.cols, base.rows);
    const cv::Rect crop =
        cv::Rect(rect.x - kTierMargin, rect.y - kTierMargin, rect.width + kTierMargin * 2, rect.height + kTierMargin * 2) & bounds;
    const cv::Rect pasted = rect & bounds;

    TierPatch patch;
    patch.name = name;
    patch.area = rect;
    patch.origin = crop.tl();
    patch.gray = base(crop) * kTierDimRatio;
    gray(pasted - rect.tl()).copyTo(patch.gray(pasted - crop.tl()), opaque(pasted - rect.tl()));
    return patch;
}

void load_icon_table(WorldMapAssets* assets)
{
    if (assets->icon_table_loaded) {
        return;
    }
    assets->icon_table_loaded = true;

    const auto file = mapnavigator::ResolveResourceImage(assets->image_roots, fs::path(kIconDir) / kIconTableName);
    if (!file) {
        LogError << "WorldMap: icon table not found" << VAR(kIconTableName) << VAR(mapnavigator::DescribeRoots(assets->image_roots));
        return;
    }

    // 仓库内 JSONC 允许注释、尾逗号与 BOM，用公共 OpenJsoncFile 读取。
    const auto parsed = common::OpenJsoncFile(*file);
    if (!parsed || !parsed->is_object()) {
        LogError << "WorldMap: icon table is not a JSON object" << VAR(MAA_NS::path_to_utf8_string(*file));
        return;
    }

    for (const auto& [name, raw] : parsed->as_object()) {
        IconEntry entry {};
        if (!entry.from_json(raw) || entry.templates.empty()) {
            LogError << "WorldMap: icon table entry needs a non-empty 'templates'" << VAR(name);
            continue;
        }

        IconSpec spec {};
        spec.spot.templates = entry.templates;
        if (entry.scale.size() == 2 && entry.scale[0] > 0.0 && entry.scale[1] >= entry.scale[0]) {
            spec.spot.scale_min = entry.scale[0];
            spec.spot.scale_max = entry.scale[1];
        }
        if (entry.scale_step > 0.0) {
            spec.spot.scale_step = entry.scale_step;
        }
        if (entry.threshold > 0.0) {
            spec.spot.min_score = entry.threshold;
        }
        if (entry.gate > 0.0) {
            spec.spot.gate_base = entry.gate;
        }
        spec.spot.radius_base = entry.radius;
        spec.spot.min_gold_ratio = entry.gold_ratio;
        spec.occluded_by_player = entry.occluded_by_player;
        assets->icon_table.emplace(name, std::move(spec));
    }

    LogInfo << "WorldMap: icon table loaded" << VAR(MAA_NS::path_to_utf8_string(*file)) << VAR(assets->icon_table.size());
}

} // namespace

WorldMapAssets* SharedAssets(std::string_view controller_type)
{
    static WorldMapAssets assets {
        .image_roots = mapnavigator::ResourceImageRoots(controller_type),
        .navmesh_pack = mapnavigator::ResolveNavmeshFilePath(),
    };
    return &assets;
}

const cv::Mat* LoadZoneBase(WorldMapAssets* assets, const std::string& zone)
{
    if (const auto it = assets->zone_bases.find(zone); it != assets->zone_bases.end()) {
        return it->second.empty() ? nullptr : &it->second;
    }

    const auto zone_dir = mapnavigator::ResolveResourceImage(assets->image_roots, fs::path("MapLocator") / MAA_NS::path(zone));
    const auto file = zone_dir ? find_zone_base_file(*zone_dir) : std::nullopt;
    if (!file) {
        LogError << "WorldMap: zone base image not found" << VAR(zone) << VAR(mapnavigator::DescribeRoots(assets->image_roots));
        assets->zone_bases[zone] = cv::Mat();
        return nullptr;
    }

    cv::Mat image = MAA_NS::imread(*file, cv::IMREAD_UNCHANGED);
    if (image.empty()) {
        LogError << "WorldMap: failed to read zone base" << VAR(MAA_NS::path_to_utf8_string(*file));
        assets->zone_bases[zone] = cv::Mat();
        return nullptr;
    }

    LogInfo << "WorldMap: zone base loaded" << VAR(zone) << VAR(image.cols) << VAR(image.rows);
    auto [it, _] = assets->zone_bases.emplace(zone, base_gray(image));
    return &it->second;
}

const std::vector<TierPatch>& LoadZoneTiers(WorldMapAssets* assets, const std::string& zone, const cv::Mat& base)
{
    if (const auto it = assets->zone_tiers.find(zone); it != assets->zone_tiers.end()) {
        return it->second;
    }
    std::vector<TierPatch>& tiers = assets->zone_tiers[zone];

    const auto zone_dir = mapnavigator::ResolveResourceImage(assets->image_roots, fs::path("MapLocator") / MAA_NS::path(zone));
    std::vector<std::pair<std::string, fs::path>> files;
    std::error_code ec;
    for (const auto& entry : fs::directory_iterator(zone_dir.value_or(fs::path()), ec)) {
        if (std::string name = tier_zone_name(zone, entry.path()); !name.empty()) {
            files.emplace_back(std::move(name), entry.path());
        }
    }
    const std::string navmesh_zone = mapnavigator::InferBaseNavZone(zone, {});
    if (files.empty() || navmesh_zone.empty()) {
        LogInfo << "WorldMap: zone has no tier images" << VAR(zone) << VAR(navmesh_zone);
        return tiers;
    }
    const navmesh::BaseNavLoadResult loaded = navmesh::LoadBaseNavPack(assets->navmesh_pack, navmesh_zone);
    if (!loaded.pack) {
        LogError << "WorldMap: failed to read tier placements" << VAR(zone) << VAR(navmesh_zone)
                 << VAR(MAA_NS::path_to_utf8_string(assets->navmesh_pack)) << VAR(loaded.message);
        return tiers;
    }

    for (const auto& [name, file] : files) {
        const navmesh::BaseNavZone* placement = find_tier_placement(loaded.pack->zones, name);
        if (placement == nullptr) {
            LogWarn << "WorldMap: tier image has no placement in the navmesh pack" << VAR(zone) << VAR(name);
            continue;
        }
        const cv::Mat image = MAA_NS::imread(file, cv::IMREAD_UNCHANGED);
        if (image.empty()) {
            LogError << "WorldMap: failed to read tier image" << VAR(MAA_NS::path_to_utf8_string(file));
            continue;
        }
        tiers.push_back(make_tier_patch(name, image, *placement, base));
    }
    std::ranges::sort(tiers, {}, &TierPatch::name);

    LogInfo << "WorldMap: zone tiers loaded" << VAR(zone) << VAR(navmesh_zone) << VAR(tiers.size());
    return tiers;
}

const IconTemplate* LoadIconTemplate(WorldMapAssets* assets, const std::string& name)
{
    const auto cached = assets->icons.find(name);
    if (cached != assets->icons.end()) {
        return cached->second.gray.empty() ? nullptr : &cached->second;
    }

    IconTemplate& slot = assets->icons[name];
    const auto file = mapnavigator::ResolveResourceImage(assets->image_roots, fs::path(kIconDir) / MAA_NS::path(name));
    const cv::Mat image = file ? MAA_NS::imread(*file, cv::IMREAD_UNCHANGED) : cv::Mat();
    if (image.empty()) {
        LogError << "WorldMap: icon template not found" << VAR(name) << VAR(mapnavigator::DescribeRoots(assets->image_roots));
        return nullptr;
    }

    slot.gray = ToGray(image);
    slot.saturation = SaturationOf(image);
    if (image.channels() == 4) {
        // alpha 就是掩膜：图标外那圈光晕在实拍里透着地形，让它参与相关只会压低分
        std::vector<cv::Mat> planes;
        cv::split(image, planes);
        slot.mask = planes[3] >= 128;
    }
    slot.hotspot = body_hotspot(slot.mask, image.size());

    LogInfo << "WorldMap: icon template loaded" << VAR(name) << VAR(image.cols) << VAR(image.rows) << VAR(image.channels())
            << VAR(slot.hotspot.x) << VAR(slot.hotspot.y);
    return &slot;
}

std::optional<IconSpec> ResolveIcon(WorldMapAssets* assets, const std::string& name)
{
    std::lock_guard guard(assets->mutex);
    load_icon_table(assets);

    const auto it = assets->icon_table.find(name);
    if (it == assets->icon_table.end()) {
        LogError << "WorldMap: no such icon in the table" << VAR(name);
        return std::nullopt;
    }
    return it->second;
}

} // namespace worldmap
