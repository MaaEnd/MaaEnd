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

// One mesh face a segment touches. s is the parameter along the segment (0 = start, 1 = end) and point is the
// intersection; the face is the triangle index within the template of the instance.
struct OccluderHit
{
    double s = 0.0;
    OccluderPoint point;
    uint32_t instance = 0;
    uint32_t triangle = 0;
};

// The collision world of one zone: mesh templates, their placements and a terrain height grid. A segment is intersected
// with the mesh triangles only, with zero margin: touching any face, from either side or on an edge, blocks it. The
// terrain takes part only in settling a structure onto the ground.
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

    // Every mesh face segment a -> b touches, sorted by the parameter along the segment from the start, ties by
    // instance and then triangle.
    std::vector<OccluderHit> lineHits(const OccluderPoint& a, const OccluderPoint& b) const;

    // The height a structure placed with its base at `base` settles to. Its footprint is the 1 m grid cell
    // [⌊x − 0.5⌋, ⌊x − 0.5⌋ + 1] × [⌊z − 0.5⌋, ⌊z − 0.5⌋ + 1], and a thin plate over it sits 1 m above base.y. When any
    // face, mesh or terrain and from either side, touches the plate, the base goes to the highest upward face that
    // the four footprint corners meet looking straight down from 1 m above base.y through 2 m. Otherwise the plate is
    // lowered through up to 2 m and the base goes to the first upward face inside the footprint it meets. Terrain
    // faces up everywhere; a mesh face faces up when (v1 − v0) × (v2 − v0) of its placed corners has a negative y,
    // the sign flipped under a mirroring placement. Meeting nothing leaves base.y. The result is kept within
    // base.y ± 1 m.
    double groundHeight(const OccluderPoint& base) const;
};

// Decodes only the scene of zone_name from the whole decompressed container. Returns nullptr on malformed bytes.
std::shared_ptr<const OccluderScene> DecodeOccluderScene(const uint8_t* data, size_t size, std::string_view zone_name);

// The occluder pack beside a main pack: base.nav.gz -> base.occluder.gz.
std::filesystem::path OccluderSidecarPath(const std::filesystem::path& main_pack);

} // namespace navmesh
