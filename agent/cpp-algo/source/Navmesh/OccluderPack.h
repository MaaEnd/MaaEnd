#pragma once

#include <array>
#include <cstddef>
#include <cstdint>
#include <filesystem>
#include <memory>
#include <string_view>
#include <vector>

namespace navmesh
{

// Position in metres, y up, in the frame of zipline_frames.json.
struct OccluderPoint
{
    double x = 0.0;
    double y = 0.0;
    double z = 0.0;
};

// One collision face a segment touches. s is the parameter along the segment (0 = start, 1 = end) and point is the
// intersection. A mesh face records the instance and the triangle index within its template; a terrain face records
// the block, the cell (u, v) and which of the cell's two triangles it is.
struct OccluderHit
{
    double s = 0.0;
    OccluderPoint point;
    bool terrain = false;
    uint32_t instance = 0;
    uint32_t triangle = 0;
    uint32_t block = 0;
    uint32_t u = 0;
    uint32_t v = 0;
    bool second = false;
};

// The occluding faces of one zone: mesh templates, their placements and a terrain height grid. A segment is intersected
// with these triangles with zero margin: touching any face, from either side or on an edge, blocks it.
struct OccluderScene
{
    using Vec3 = std::array<double, 3>;
    using Mat3 = std::array<Vec3, 3>;

    // A node of a bounding-box tree, its box rounded outward to float. count == 0 marks an inner node whose left child
    // follows it directly and whose right child sits at first; otherwise it is a leaf owning order[first, first + count).
    struct BoxNode
    {
        float lo[3] = {};
        float hi[3] = {};
        uint32_t first = 0;
        uint32_t count = 0;
    };

    // A collision mesh template in local coordinates. Templates with many triangles get their own tree whose leaves hold
    // triangle indices.
    struct Template
    {
        std::vector<Vec3> verts;
        std::vector<std::array<uint32_t, 3>> tris;
        Vec3 lo {};
        Vec3 hi {};
        std::vector<BoxNode> nodes;
        std::vector<uint32_t> order;
    };

    // One placement of a template: world = a·local + t. When a is invertible m holds its inverse and segments are
    // intersected in the template's local frame; a flattened placement has no inverse, so m holds a itself and the
    // triangles are placed in the world before intersecting. lo/hi is the placed bounding box.
    struct Instance
    {
        uint32_t tpl = 0;
        bool invertible = false;
        Mat3 m {};
        Vec3 t {};
        Vec3 lo {};
        Vec3 hi {};
    };

    // One terrain block of block_n × block_n samples; sample (u, v) sits at
    // (x0 + u·cell, lattice_a + lattice_b·k[v·n + u], z0 + v·cell). Cell (u, v) splits into the triangles
    // (u,v)-(u,v+1)-(u+1,v) and (u+1,v)-(u,v+1)-(u+1,v+1). Empty holes means the block has no hole; otherwise cell
    // c = v·(n-1) + u is a hole when bit c % 8 of holes[c / 8] is set.
    struct Block
    {
        double x0 = 0.0;
        double z0 = 0.0;
        std::vector<int32_t> k;
        std::vector<uint8_t> holes;
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
    // Bounding-box tree over all instances; its leaves hold instance indices.
    std::vector<BoxNode> instance_nodes;
    std::vector<uint32_t> instance_order;

    // Every collision face segment a -> b touches, sorted by the parameter along the segment from the start; ties put
    // mesh faces before terrain, then order by index.
    std::vector<OccluderHit> lineHits(const OccluderPoint& a, const OccluderPoint& b) const;
};

// Decodes only the scene of zone_name from the whole decompressed container. Returns nullptr on malformed bytes.
std::shared_ptr<const OccluderScene> DecodeOccluderScene(const uint8_t* data, size_t size, std::string_view zone_name);

// The occluder pack beside a main pack: base.nav.gz -> base.occluder.gz.
std::filesystem::path OccluderSidecarPath(const std::filesystem::path& main_pack);

} // namespace navmesh
