#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <filesystem>
#include <memory>
#include <string>
#include <string_view>
#include <vector>

namespace navmesh
{

// 单位米, y 朝上, 与 zipline_frames.json 同一坐标系
struct OccluderPoint
{
    double x = 0.0;
    double y = 0.0;
    double z = 0.0;
};

// s 是沿线段的参数, 0 为起点 1 为终点, triangle 是实例所用模板内的三角下标
struct OccluderHit
{
    double s = 0.0;
    OccluderPoint point;
    uint32_t instance = 0;
    uint32_t triangle = 0;
};

// 线段只与网格三角求交且零余量, 地形只参与建筑落地
struct OccluderScene
{
    using Vec3 = std::array<double, 3>;
    using Mat3 = std::array<Vec3, 3>;

    // count == 0 为内节点, 左子紧随其后, 右子在 first, 否则为叶子, 拥有 order[first, first + count)
    struct BoxNode
    {
        float lo[3] = {};
        float hi[3] = {};
        uint32_t first = 0;
        uint32_t count = 0;
    };

    struct Template
    {
        std::vector<Vec3> verts;
        std::vector<std::array<uint32_t, 3>> tris;
        Vec3 lo {};
        Vec3 hi {};
        std::vector<BoxNode> nodes;
        std::vector<uint32_t> order;
    };

    // world = a·local + t, a 可逆时 m 存其逆, 否则 m 存 a 本身
    struct Instance
    {
        uint32_t tpl = 0;
        bool invertible = false;
        Mat3 m {};
        Vec3 t {};
        Vec3 lo {};
        Vec3 hi {};
    };

    // 采样 (u, v) 位于 (x0 + u·cell, lattice_a + lattice_b·k[v·n + u], z0 + v·cell)
    struct Block
    {
        double x0 = 0.0;
        double z0 = 0.0;
        std::vector<int32_t> k;
        std::vector<uint8_t> holes; // 空表示无洞, 否则格 c = v·(n-1) + u 的洞位在 holes[c / 8] 第 c % 8 位
        Vec3 lo {};
        Vec3 hi {};
    };

    std::vector<Template> templates;
    std::vector<Instance> instances;
    std::vector<Block> blocks;
    uint32_t block_n = 0;
    double cell = 0.0;
    double lattice_a = 0.0;
    double lattice_b = 0.0;
    std::vector<BoxNode> instance_nodes;
    std::vector<uint32_t> instance_order;

    // 按沿线参数升序, 并列时按 instance 再按 triangle
    std::vector<OccluderHit> lineHits(const OccluderPoint& a, const OccluderPoint& b) const;

    // 以 base 为名义塔底的建筑落地后的高度, 结果夹在 base.y ± 1 m 内
    double groundHeight(const OccluderPoint& base) const;

    bool dropBlocked(const OccluderPoint& take, const OccluderPoint& land, double climb, double height) const;
};

// 输入须是解压后的整份容器, 数据有误返回 nullptr
std::shared_ptr<const OccluderScene> DecodeOccluderScene(const uint8_t* data, size_t size, std::string_view zone_name);

// base.nav.gz → base.occluder.gz
std::filesystem::path OccluderSidecarPath(const std::filesystem::path& main_pack);

std::shared_ptr<const OccluderScene> LoadOccluderScene(const std::filesystem::path& path, const std::string& zone_name);

} // namespace navmesh
