#include "OccluderPack.h"

#include <algorithm>
#include <bit>
#include <cmath>
#include <cstring>
#include <limits>
#include <mutex>
#include <numbers>
#include <numeric>
#include <optional>
#include <string>
#include <tuple>
#include <unordered_map>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

#include "BaseNavReader.h"
#include "NavVarint.h"

// 禁止乘加融合, 求交结果须逐位可复现
#if defined(__clang__)
#pragma clang fp contract(off)
#elif defined(__GNUC__)
#pragma GCC optimize("fp-contract=off")
#endif

namespace navmesh
{

namespace
{

using Vec3 = OccluderScene::Vec3;
using Mat3 = OccluderScene::Mat3;
using BoxNode = OccluderScene::BoxNode;

constexpr char kContainerTag[4] = { 'O', 'C', 'L', 'S' };
constexpr char kSceneTag[4] = { 'O', 'C', 'L', 'Z' };
constexpr uint32_t kContainerVersion = 1;
constexpr uint32_t kSceneVersion = 1;
// 容器表头: 魔数、版本、场景数, 各 4 字节
constexpr size_t kContainerHeaderSize = 12;
// 场景表头: 魔数 4 B, 版本、模板数、实例数、块数、块边长 u32, 位置步长、格宽、格点 a/b f64, 旋转分母、缩放移位 u32, 原点 x/z i64
constexpr size_t kSceneHeaderSize = 80;

// 表头之后的流, 按此顺序排列
enum StreamIndex : size_t
{
    kTemplate,
    kVertex,
    kIndex,
    kInstanceCount,
    kPosition,
    kRotationCode,
    kRotation,
    kScale,
    kMatrix,
    kBlock,
    kHeight,
    kHole,
    kStreamCount,
};

// 线段与三角平面夹角正弦的平方低于此值视为平行
constexpr double kParallelSin2 = 1e-24;
// 包围盒判交时各面外扩的米数
constexpr double kBoxPad = 1e-6;
// 三角包围盒外扩量, 模板局部单位
constexpr double kTriangleBoxPad = 1e-3;
constexpr size_t kTreeMinTriangles = 8;
constexpr uint32_t kLeafSize = 4;
// 四元数除最大分量外的三个分量按 [-1/√2, 1/√2] 存
constexpr double kFrac1Sqrt2 = std::numbers::sqrt2 / 2.0;
// 落地占地格的半宽 m, 格按整米对齐
constexpr double kSettleHalfWidth = 0.5;
// 薄板起落高度在名义塔底上方的米数
constexpr double kSettleLift = 1.0;
// 薄板底面低于、顶面高于起落高度的米数
constexpr double kSettlePlateBelow = 5e-5;
constexpr double kSettlePlateAbove = 1.5e-4;
// 面的最高点在板底下这么多米内也算贴着
constexpr double kSettleTouch = 1e-5;
// 薄板最多下落的米数
constexpr double kSettleDrop = 2.0;
// 落地结果离名义塔底的最大米数
constexpr double kSettleClamp = 1.0;
constexpr size_t kClipMaxVertices = 8;
constexpr double kBaryEps = 1e-9;
constexpr double kFlatDet = 1e-12;
constexpr double kColumnEdge = 1e-6;
constexpr double kCurtainMinLength = 1e-9;

uint16_t read_u16(const uint8_t** cursor)
{
    const uint16_t value = static_cast<uint16_t>((*cursor)[0] | ((*cursor)[1] << 8U));
    *cursor += 2;
    return value;
}

uint32_t read_u32(const uint8_t** cursor)
{
    const uint32_t value = static_cast<uint32_t>((*cursor)[0]) | (static_cast<uint32_t>((*cursor)[1]) << 8U)
                           | (static_cast<uint32_t>((*cursor)[2]) << 16U) | (static_cast<uint32_t>((*cursor)[3]) << 24U);
    *cursor += 4;
    return value;
}

uint64_t read_u64(const uint8_t** cursor)
{
    const uint64_t low = read_u32(cursor);
    const uint64_t high = read_u32(cursor);
    return low | (high << 32U);
}

double read_f64(const uint8_t** cursor)
{
    return std::bit_cast<double>(read_u64(cursor));
}

// 读过头或变长整数不合规时 ok 置 false, 此后读到的都是 0
struct Stream
{
    const uint8_t* begin = nullptr;
    const uint8_t* cursor = nullptr;
    const uint8_t* end = nullptr;
    bool ok = true;
};

size_t stream_size(const Stream& st)
{
    return static_cast<size_t>(st.end - st.begin);
}

uint8_t stream_byte(Stream* st)
{
    if (!st->ok || st->cursor == st->end) {
        st->ok = false;
        return 0;
    }
    return *st->cursor++;
}

uint64_t stream_uv(Stream* st)
{
    uint64_t value = 0;
    if (!st->ok || !GetVarint(&value, &st->cursor, st->end)) {
        st->ok = false;
        return 0;
    }
    return value;
}

int64_t stream_sv(Stream* st)
{
    return UnZigzag(stream_uv(st));
}

float stream_f32(Stream* st)
{
    if (!st->ok || st->end - st->cursor < 4) {
        st->ok = false;
        return 0.0F;
    }
    return std::bit_cast<float>(read_u32(&st->cursor));
}

const uint8_t* stream_take(Stream* st, size_t bytes)
{
    if (!st->ok || static_cast<size_t>(st->end - st->cursor) < bytes) {
        st->ok = false;
        return nullptr;
    }
    const uint8_t* const start = st->cursor;
    st->cursor += bytes;
    return start;
}

// 按补码回绕, 坏数据不会触发有符号溢出
int64_t wrap_add(int64_t a, int64_t b)
{
    return static_cast<int64_t>(static_cast<uint64_t>(a) + static_cast<uint64_t>(b));
}

Vec3 vec_sub(const Vec3& a, const Vec3& b)
{
    return { a[0] - b[0], a[1] - b[1], a[2] - b[2] };
}

Vec3 vec_add(const Vec3& a, const Vec3& b)
{
    return { a[0] + b[0], a[1] + b[1], a[2] + b[2] };
}

Vec3 vec_scale(const Vec3& v, double s)
{
    return { v[0] * s, v[1] * s, v[2] * s };
}

double vec_dot(const Vec3& a, const Vec3& b)
{
    return a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
}

Vec3 vec_cross(const Vec3& a, const Vec3& b)
{
    return { a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0] };
}

Vec3 mat_vec_mul(const Mat3& m, const Vec3& v)
{
    return { vec_dot(m[0], v), vec_dot(m[1], v), vec_dot(m[2], v) };
}

double mat_det(const Mat3& m)
{
    return vec_dot(m[0], vec_cross(m[1], m[2]));
}

bool mat_inverse(Mat3* out, const Mat3& m)
{
    const Mat3 c { {
        { m[1][1] * m[2][2] - m[1][2] * m[2][1], m[1][2] * m[2][0] - m[1][0] * m[2][2], m[1][0] * m[2][1] - m[1][1] * m[2][0] },
        { m[0][2] * m[2][1] - m[0][1] * m[2][2], m[0][0] * m[2][2] - m[0][2] * m[2][0], m[0][1] * m[2][0] - m[0][0] * m[2][1] },
        { m[0][1] * m[1][2] - m[0][2] * m[1][1], m[0][2] * m[1][0] - m[0][0] * m[1][2], m[0][0] * m[1][1] - m[0][1] * m[1][0] },
    } };
    const double det = m[0][0] * c[0][0] + m[0][1] * c[0][1] + m[0][2] * c[0][2];
    if (det == 0.0 || !std::isfinite(det)) {
        return false;
    }
    for (size_t i = 0; i < 3; ++i) {
        for (size_t j = 0; j < 3; ++j) {
            (*out)[i][j] = c[j][i] / det;
        }
    }
    return true;
}

// code 是四元数 (x, y, z, w) 里略去的最大分量位置, rot 是其余三个分量, scale 是 s·scale_unit
Mat3 affine_decode(
    uint8_t code,
    const std::array<int64_t, 3>& rot,
    const std::array<int64_t, 3>& scale,
    double rot_scale,
    double scale_unit)
{
    double c[3];
    for (size_t i = 0; i < 3; ++i) {
        c[i] = static_cast<double>(rot[i]) / rot_scale * kFrac1Sqrt2;
    }
    const double big = std::sqrt(std::fmax(1.0 - (c[0] * c[0] + c[1] * c[1] + c[2] * c[2]), 0.0));
    double q[4];
    for (size_t i = 0, k = 0; i < 4; ++i) {
        q[i] = i == code ? big : c[k++];
    }
    const double x = q[0];
    const double y = q[1];
    const double z = q[2];
    const double w = q[3];
    const double r[3][3] = {
        { 1.0 - 2.0 * (y * y + z * z), 2.0 * (x * y - z * w), 2.0 * (x * z + y * w) },
        { 2.0 * (x * y + z * w), 1.0 - 2.0 * (x * x + z * z), 2.0 * (y * z - x * w) },
        { 2.0 * (x * z - y * w), 2.0 * (y * z + x * w), 1.0 - 2.0 * (x * x + y * y) },
    };
    double s[3];
    for (size_t j = 0; j < 3; ++j) {
        s[j] = static_cast<double>(scale[j]) / scale_unit;
    }
    Mat3 a {};
    for (size_t i = 0; i < 3; ++i) {
        for (size_t j = 0; j < 3; ++j) {
            a[i][j] = r[i][j] * s[j];
        }
    }
    return a;
}

bool seg_box(const Vec3& o, const Vec3& d, const Vec3& lo, const Vec3& hi)
{
    double s0 = 0.0;
    double s1 = 1.0;
    for (size_t k = 0; k < 3; ++k) {
        const double l = lo[k] - kBoxPad;
        const double h = hi[k] + kBoxPad;
        if (d[k] == 0.0) {
            if (o[k] < l || o[k] > h) {
                return false;
            }
        }
        else {
            const double a = (l - o[k]) / d[k];
            const double b = (h - o[k]) / d[k];
            s0 = std::fmax(s0, std::fmin(a, b));
            s1 = std::fmin(s1, std::fmax(a, b));
            if (s0 > s1) {
                return false;
            }
        }
    }
    return true;
}

// Möller–Trumbore, 正反面与边角端点都算交, d2 = |d|²
bool seg_tri(double* t, const Vec3& o, const Vec3& d, double d2, const Vec3& v0, const Vec3& v1, const Vec3& v2)
{
    const Vec3 e1 = vec_sub(v1, v0);
    const Vec3 e2 = vec_sub(v2, v0);
    const Vec3 n = vec_cross(e1, e2);
    const double n2 = vec_dot(n, n);
    const Vec3 p = vec_cross(d, e2);
    const double det = vec_dot(e1, p);
    if (n2 == 0.0 || det * det <= kParallelSin2 * n2 * d2) {
        return false;
    }
    const double inv = 1.0 / det;
    const Vec3 s = vec_sub(o, v0);
    const double u = vec_dot(s, p) * inv;
    if (!(u >= 0.0 && u <= 1.0)) {
        return false;
    }
    const Vec3 q = vec_cross(s, e1);
    const double v = vec_dot(d, q) * inv;
    if (v < 0.0 || u + v > 1.0) {
        return false;
    }
    *t = vec_dot(e2, q) * inv;
    return *t >= 0.0 && *t <= 1.0;
}

struct Cut
{
    size_t axis;
    double bound;
    bool keep_above;
};

bool cut_inside(const Cut& cut, const Vec3& p)
{
    return cut.keep_above ? p[cut.axis] >= cut.bound : p[cut.axis] <= cut.bound;
}

size_t clip_to_rect(std::array<Vec3, kClipMaxVertices>* poly, const Vec3 (&tri)[3], double x0, double x1, double z0, double z1)
{
    const Cut cuts[4] = { { 0, x0, true }, { 0, x1, false }, { 2, z0, true }, { 2, z1, false } };
    std::copy(std::begin(tri), std::end(tri), poly->begin());
    size_t count = 3;
    std::array<Vec3, kClipMaxVertices> next {};
    for (const Cut& cut : cuts) {
        size_t kept = 0;
        for (size_t i = 0; i < count; ++i) {
            const Vec3& p = (*poly)[i];
            const Vec3& q = (*poly)[(i + 1) % count];
            if (cut_inside(cut, p)) {
                next[kept++] = p;
            }
            if (cut_inside(cut, p) != cut_inside(cut, q)) {
                next[kept++] = vec_add(p, vec_scale(vec_sub(q, p), (cut.bound - p[cut.axis]) / (q[cut.axis] - p[cut.axis])));
            }
        }
        *poly = next;
        count = kept;
        if (count == 0) {
            break;
        }
    }
    return count;
}

float float_down(double value)
{
    constexpr double kMax = std::numeric_limits<float>::max();
    if (!(value > -kMax)) {
        return -std::numeric_limits<float>::infinity();
    }
    if (value >= kMax) {
        return std::numeric_limits<float>::max();
    }
    float f = static_cast<float>(value);
    if (static_cast<double>(f) > value) {
        f = std::nextafter(f, -std::numeric_limits<float>::infinity());
    }
    return f;
}

float float_up(double value)
{
    constexpr double kMax = std::numeric_limits<float>::max();
    if (!(value < kMax)) {
        return std::numeric_limits<float>::infinity();
    }
    if (value <= -kMax) {
        return -std::numeric_limits<float>::max();
    }
    float f = static_cast<float>(value);
    if (static_cast<double>(f) < value) {
        f = std::nextafter(f, std::numeric_limits<float>::infinity());
    }
    return f;
}

struct ItemBoxes
{
    const std::vector<Vec3>& lo;
    const std::vector<Vec3>& hi;
    const std::vector<Vec3>& centre;
    double pad;
};

uint32_t build_tree_node(std::vector<BoxNode>* nodes, std::vector<uint32_t>* order, const ItemBoxes& items, uint32_t begin, uint32_t end)
{
    const uint32_t index = static_cast<uint32_t>(nodes->size());
    nodes->emplace_back();
    Vec3 box_lo;
    Vec3 box_hi;
    box_lo.fill(std::numeric_limits<double>::infinity());
    box_hi.fill(-std::numeric_limits<double>::infinity());
    Vec3 centre_lo = box_lo;
    Vec3 centre_hi = box_hi;
    for (uint32_t i = begin; i < end; ++i) {
        const uint32_t item = (*order)[i];
        for (size_t k = 0; k < 3; ++k) {
            box_lo[k] = std::fmin(box_lo[k], items.lo[item][k] - items.pad);
            box_hi[k] = std::fmax(box_hi[k], items.hi[item][k] + items.pad);
            centre_lo[k] = std::fmin(centre_lo[k], items.centre[item][k]);
            centre_hi[k] = std::fmax(centre_hi[k], items.centre[item][k]);
        }
    }
    for (size_t k = 0; k < 3; ++k) {
        (*nodes)[index].lo[k] = float_down(box_lo[k]);
        (*nodes)[index].hi[k] = float_up(box_hi[k]);
    }
    if (end - begin <= kLeafSize) {
        (*nodes)[index].first = begin;
        (*nodes)[index].count = end - begin;
        return index;
    }
    size_t axis = 0;
    for (size_t k = 1; k < 3; ++k) {
        if (centre_hi[k] - centre_lo[k] > centre_hi[axis] - centre_lo[axis]) {
            axis = k;
        }
    }
    const uint32_t mid = begin + (end - begin) / 2;
    std::nth_element(order->begin() + begin, order->begin() + mid, order->begin() + end, [&](uint32_t a, uint32_t b) {
        return std::tie(items.centre[a][axis], a) < std::tie(items.centre[b][axis], b);
    });
    build_tree_node(nodes, order, items, begin, mid);
    const uint32_t right = build_tree_node(nodes, order, items, mid, end);
    (*nodes)[index].first = right;
    (*nodes)[index].count = 0;
    return index;
}

void build_tree(
    std::vector<BoxNode>* nodes,
    std::vector<uint32_t>* order,
    const std::vector<Vec3>& lo,
    const std::vector<Vec3>& hi,
    double pad)
{
    const uint32_t count = static_cast<uint32_t>(lo.size());
    order->resize(count);
    std::iota(order->begin(), order->end(), 0U);
    nodes->clear();
    if (count == 0) {
        return;
    }
    std::vector<Vec3> centre(count);
    for (uint32_t i = 0; i < count; ++i) {
        for (size_t k = 0; k < 3; ++k) {
            centre[i][k] = 0.5 * (lo[i][k] + hi[i][k]);
        }
    }
    nodes->reserve(2 * static_cast<size_t>(count));
    build_tree_node(nodes, order, ItemBoxes { .lo = lo, .hi = hi, .centre = centre, .pad = pad }, 0, count);
    nodes->shrink_to_fit();
}

// 只给第二个盒子外扩 kBoxPad
bool box_box(const Vec3& lo, const Vec3& hi, const Vec3& other_lo, const Vec3& other_hi)
{
    for (size_t k = 0; k < 3; ++k) {
        if (other_lo[k] - kBoxPad > hi[k] || other_hi[k] + kBoxPad < lo[k]) {
            return false;
        }
    }
    return true;
}

// 对半分的树深不过 32 层, 栈开 64 足够
std::vector<uint32_t>
    tree_leaves_in_box(const std::vector<BoxNode>& nodes, const std::vector<uint32_t>& order, const Vec3& query_lo, const Vec3& query_hi)
{
    std::vector<uint32_t> out;
    if (nodes.empty()) {
        return out;
    }
    uint32_t stack[64];
    size_t top = 0;
    stack[top++] = 0;
    while (top > 0) {
        const uint32_t index = stack[--top];
        const BoxNode& node = nodes[index];
        const Vec3 lo { node.lo[0], node.lo[1], node.lo[2] };
        const Vec3 hi { node.hi[0], node.hi[1], node.hi[2] };
        if (!box_box(query_lo, query_hi, lo, hi)) {
            continue;
        }
        if (node.count > 0) {
            for (uint32_t i = node.first; i < node.first + node.count; ++i) {
                out.push_back(order[i]);
            }
            continue;
        }
        stack[top++] = node.first;
        stack[top++] = index + 1;
    }
    return out;
}

// 对半分的树深不过 32 层, 栈开 64 足够
std::vector<uint32_t>
    tree_leaves_on_segment(const std::vector<BoxNode>& nodes, const std::vector<uint32_t>& order, const Vec3& a, const Vec3& d)
{
    std::vector<uint32_t> out;
    if (nodes.empty()) {
        return out;
    }
    uint32_t stack[64];
    size_t top = 0;
    stack[top++] = 0;
    while (top > 0) {
        const uint32_t index = stack[--top];
        const BoxNode& node = nodes[index];
        const Vec3 lo { node.lo[0], node.lo[1], node.lo[2] };
        const Vec3 hi { node.hi[0], node.hi[1], node.hi[2] };
        if (!seg_box(a, d, lo, hi)) {
            continue;
        }
        if (node.count > 0) {
            for (uint32_t i = node.first; i < node.first + node.count; ++i) {
                out.push_back(order[i]);
            }
            continue;
        }
        stack[top++] = node.first;
        stack[top++] = index + 1;
    }
    return out;
}

struct Face
{
    Vec3 v[3];
    bool up = false;
};

void push_instance_face(
    std::vector<Face>* faces,
    const Mat3& a,
    const OccluderScene::Template& tpl,
    const OccluderScene::Instance& inst,
    double facing,
    uint32_t k)
{
    Face face;
    for (size_t c = 0; c < 3; ++c) {
        face.v[c] = vec_add(mat_vec_mul(a, tpl.verts[tpl.tris[k][c]]), inst.t);
    }
    face.up = facing * vec_cross(vec_sub(face.v[1], face.v[0]), vec_sub(face.v[2], face.v[0]))[1] < 0.0;
    faces->push_back(face);
}

Vec3 block_sample_point(const OccluderScene& scene, const OccluderScene::Block& block, double cell, size_t n, size_t u, size_t v)
{
    return Vec3 { block.x0 + static_cast<double>(u) * cell,
                  scene.lattice_a + scene.lattice_b * static_cast<double>(block.k[v * n + u]),
                  block.z0 + static_cast<double>(v) * cell };
}

std::vector<Face> faces_in_box(const OccluderScene& scene, const Vec3& lo, const Vec3& hi)
{
    std::vector<Face> faces;
    for (const uint32_t index : tree_leaves_in_box(scene.instance_nodes, scene.instance_order, lo, hi)) {
        const OccluderScene::Instance& inst = scene.instances[index];
        if (!box_box(lo, hi, inst.lo, inst.hi)) {
            continue;
        }
        Mat3 a = inst.m;
        if (inst.invertible && !mat_inverse(&a, inst.m)) {
            continue;
        }
        const double facing = mat_det(a) < 0.0 ? -1.0 : 1.0;
        const OccluderScene::Template& tpl = scene.templates[inst.tpl];
        if (!inst.invertible || tpl.nodes.empty()) {
            for (uint32_t k = 0; k < tpl.tris.size(); ++k) {
                push_instance_face(&faces, a, tpl, inst, facing, k);
            }
            continue;
        }
        Vec3 local_lo;
        Vec3 local_hi;
        local_lo.fill(std::numeric_limits<double>::infinity());
        local_hi.fill(-std::numeric_limits<double>::infinity());
        for (uint32_t corner = 0; corner < 8; ++corner) {
            Vec3 w;
            for (size_t j = 0; j < 3; ++j) {
                w[j] = ((corner >> j) & 1U) == 0 ? lo[j] : hi[j];
            }
            const Vec3 l = mat_vec_mul(inst.m, vec_sub(w, inst.t));
            for (size_t j = 0; j < 3; ++j) {
                local_lo[j] = std::fmin(local_lo[j], l[j]);
                local_hi[j] = std::fmax(local_hi[j], l[j]);
            }
        }
        for (const uint32_t k : tree_leaves_in_box(tpl.nodes, tpl.order, local_lo, local_hi)) {
            push_instance_face(&faces, a, tpl, inst, facing, k);
        }
    }
    const size_t n = scene.block_n;
    const double cell = scene.cell;
    for (const OccluderScene::Block& block : scene.blocks) {
        if (!box_box(lo, hi, block.lo, block.hi)) {
            continue;
        }
        const double last = static_cast<double>(n - 2);
        const double u0 = std::fmax(std::floor((lo[0] - block.x0) / cell) - 1.0, 0.0);
        const double u1 = std::fmin(std::floor((hi[0] - block.x0) / cell) + 1.0, last);
        const double v0 = std::fmax(std::floor((lo[2] - block.z0) / cell) - 1.0, 0.0);
        const double v1 = std::fmin(std::floor((hi[2] - block.z0) / cell) + 1.0, last);
        if (!(u0 <= u1 && v0 <= v1)) {
            continue;
        }
        for (size_t v = static_cast<size_t>(v0); v <= static_cast<size_t>(v1); ++v) {
            for (size_t u = static_cast<size_t>(u0); u <= static_cast<size_t>(u1); ++u) {
                const size_t c = v * (n - 1) + u;
                if (!block.holes.empty() && ((block.holes[c / 8] >> (c % 8)) & 1U) != 0) {
                    continue;
                }
                const Vec3 p00 = block_sample_point(scene, block, cell, n, u, v);
                const Vec3 p01 = block_sample_point(scene, block, cell, n, u, v + 1);
                const Vec3 p10 = block_sample_point(scene, block, cell, n, u + 1, v);
                const Vec3 p11 = block_sample_point(scene, block, cell, n, u + 1, v + 1);
                faces.push_back(Face { .v = { p00, p01, p10 }, .up = true });
                faces.push_back(Face { .v = { p10, p01, p11 }, .up = true });
            }
        }
    }
    return faces;
}

std::optional<double> height_at(const Vec3 (&tri)[3], double x, double z)
{
    const Vec3& a = tri[0];
    const Vec3& b = tri[1];
    const Vec3& c = tri[2];
    const double det = (b[2] - c[2]) * (a[0] - c[0]) + (c[0] - b[0]) * (a[2] - c[2]);
    if (std::fabs(det) < kFlatDet) {
        return std::nullopt;
    }
    const double l0 = ((b[2] - c[2]) * (x - c[0]) + (c[0] - b[0]) * (z - c[2])) / det;
    const double l1 = ((c[2] - a[2]) * (x - c[0]) + (a[0] - c[0]) * (z - c[2])) / det;
    const double l2 = 1.0 - l0 - l1;
    if (l0 < -kBaryEps || l1 < -kBaryEps || l2 < -kBaryEps) {
        return std::nullopt;
    }
    return l0 * a[1] + l1 * b[1] + l2 * c[1];
}

using Point = std::array<double, 2>;

enum class ClipEdge
{
    AlongMin,
    AlongMax,
    HeightMin,
    HeightMax,
};

double clip_distance(ClipEdge edge, const Point& p, double len, double lo, double hi)
{
    switch (edge) {
    case ClipEdge::AlongMin:
        return p[0];
    case ClipEdge::AlongMax:
        return len - p[0];
    case ClipEdge::HeightMin:
        return p[1] - lo;
    case ClipEdge::HeightMax:
        return hi - p[1];
    }
    return 0.0;
}

void clip_section(std::vector<Point>* section, ClipEdge edge, double len, double lo, double hi)
{
    std::vector<Point> kept;
    for (size_t i = 0; i < section->size(); ++i) {
        const Point& cur = (*section)[i];
        const Point& next = (*section)[(i + 1) % section->size()];
        const double dc = clip_distance(edge, cur, len, lo, hi);
        const double dn = clip_distance(edge, next, len, lo, hi);
        if (dc >= 0.0) {
            kept.push_back(cur);
        }
        if ((dc > 0.0 && dn < 0.0) || (dc < 0.0 && dn > 0.0)) {
            const double t = dc / (dc - dn);
            kept.push_back({ cur[0] + t * (next[0] - cur[0]), cur[1] + t * (next[1] - cur[1]) });
        }
    }
    *section = std::move(kept);
}

bool crosses_curtain(const Vec3 (&tri)[3], double ax, double az, double bx, double bz, double lo, double hi)
{
    const double dx = bx - ax;
    const double dz = bz - az;
    const double len = std::hypot(dx, dz);
    if (len < kCurtainMinLength) {
        return false;
    }
    const double ux = dx / len;
    const double uz = dz / len;
    double side[3] {};
    double along[3] {};
    for (size_t k = 0; k < 3; ++k) {
        side[k] = (tri[k][2] - az) * ux - (tri[k][0] - ax) * uz;
        along[k] = (tri[k][0] - ax) * ux + (tri[k][2] - az) * uz;
    }
    std::vector<Point> section;
    for (size_t k = 0; k < 3; ++k) {
        const size_t j = (k + 1) % 3;
        if (side[k] == 0.0) {
            section.push_back({ along[k], tri[k][1] });
        }
        if ((side[k] < 0.0 && side[j] > 0.0) || (side[k] > 0.0 && side[j] < 0.0)) {
            const double t = side[k] / (side[k] - side[j]);
            section.push_back({ along[k] + t * (along[j] - along[k]), tri[k][1] + t * (tri[j][1] - tri[k][1]) });
        }
    }
    clip_section(&section, ClipEdge::AlongMin, len, lo, hi);
    clip_section(&section, ClipEdge::AlongMax, len, lo, hi);
    clip_section(&section, ClipEdge::HeightMin, len, lo, hi);
    clip_section(&section, ClipEdge::HeightMax, len, lo, hi);
    if (section.empty()) {
        return false;
    }
    double y_lo = std::numeric_limits<double>::infinity();
    double y_hi = -std::numeric_limits<double>::infinity();
    for (const Point& p : section) {
        y_lo = std::fmin(y_lo, p[1]);
        y_hi = std::fmax(y_hi, p[1]);
    }
    return y_hi > lo && y_lo < hi;
}

bool decode_templates(OccluderScene* scene, uint32_t count, Stream* st)
{
    scene->templates.resize(count);
    for (OccluderScene::Template& tpl : scene->templates) {
        const uint64_t vertex_count = stream_uv(&st[kTemplate]);
        const uint64_t triangle_count = stream_uv(&st[kTemplate]);
        const int64_t exponent = stream_sv(&st[kTemplate]);
        if (!st[kTemplate].ok || exponent < -60 || exponent > 60 || vertex_count > stream_size(st[kVertex])
            || triangle_count > stream_size(st[kIndex])) {
            return false;
        }
        const double step = std::ldexp(1.0, static_cast<int>(exponent));
        tpl.verts.resize(vertex_count);
        for (size_t axis = 0; axis < 3; ++axis) {
            int64_t prev = 0;
            for (Vec3& vertex : tpl.verts) {
                prev = wrap_add(prev, stream_sv(&st[kVertex]));
                vertex[axis] = static_cast<double>(prev) * step;
            }
        }
        tpl.tris.resize(triangle_count);
        uint64_t next = 0;
        for (std::array<uint32_t, 3>& tri : tpl.tris) {
            for (uint32_t& corner : tri) {
                const uint64_t back = stream_uv(&st[kIndex]);
                if (back > next) {
                    return false;
                }
                corner = static_cast<uint32_t>(next - back);
                if (back == 0) {
                    ++next;
                }
            }
        }
        if (!st[kVertex].ok || !st[kIndex].ok || next != vertex_count) {
            return false;
        }
        tpl.lo.fill(std::numeric_limits<double>::max());
        tpl.hi.fill(std::numeric_limits<double>::lowest());
        for (const Vec3& vertex : tpl.verts) {
            for (size_t k = 0; k < 3; ++k) {
                tpl.lo[k] = std::fmin(tpl.lo[k], vertex[k]);
                tpl.hi[k] = std::fmax(tpl.hi[k], vertex[k]);
            }
        }
    }
    return true;
}

// 位置按位置步长的整数存, x、z 相对表头原点, y 原点恒为 0
bool decode_instances(
    OccluderScene* scene,
    uint32_t count,
    double pos_q,
    const std::array<int64_t, 3>& origin,
    double rot_scale,
    double scale_unit,
    Stream* st)
{
    // 实例按模板排好, 逐模板存一个实例数
    std::vector<uint32_t> tpl_of;
    tpl_of.reserve(count);
    for (uint32_t k = 0; k < scene->templates.size(); ++k) {
        const uint64_t n = stream_uv(&st[kInstanceCount]);
        if (!st[kInstanceCount].ok || n > count - tpl_of.size()) {
            return false;
        }
        tpl_of.insert(tpl_of.end(), n, k);
    }
    if (tpl_of.size() != count) {
        return false;
    }
    std::vector<std::array<int64_t, 3>> pos(count);
    for (size_t axis = 0; axis < 3; ++axis) {
        int64_t prev = 0;
        for (std::array<int64_t, 3>& p : pos) {
            prev = wrap_add(prev, stream_sv(&st[kPosition]));
            p[axis] = wrap_add(prev, origin[axis]);
        }
    }
    const uint8_t* const codes = stream_take(&st[kRotationCode], count);
    if (!st[kPosition].ok || codes == nullptr || std::any_of(codes, codes + count, [](uint8_t code) { return code > 4; })) {
        return false;
    }
    const size_t quat_count = static_cast<size_t>(std::count_if(codes, codes + count, [](uint8_t code) { return code < 4; }));
    std::vector<std::array<int64_t, 3>> rot(quat_count);
    const int64_t rot_limit = static_cast<int64_t>(rot_scale);
    for (size_t k = 0; k < 3; ++k) {
        for (std::array<int64_t, 3>& r : rot) {
            r[k] = stream_sv(&st[kRotation]);
            if (r[k] > rot_limit || r[k] < -rot_limit) {
                return false;
            }
        }
    }
    const uint64_t scale_count = stream_uv(&st[kScale]);
    if (!st[kRotation].ok || !st[kScale].ok || scale_count > stream_size(st[kScale])) {
        return false;
    }
    std::vector<std::array<int64_t, 3>> scales(scale_count);
    for (std::array<int64_t, 3>& s : scales) {
        for (int64_t& v : s) {
            v = stream_sv(&st[kScale]);
        }
    }

    scene->instances.resize(count);
    size_t quat_index = 0;
    for (uint32_t i = 0; i < count; ++i) {
        OccluderScene::Instance& inst = scene->instances[i];
        Mat3 a {};
        if (codes[i] < 4) {
            const uint64_t scale_index = stream_uv(&st[kScale]);
            if (!st[kScale].ok || scale_index >= scales.size()) {
                return false;
            }
            a = affine_decode(codes[i], rot[quat_index++], scales[scale_index], rot_scale, scale_unit);
        }
        else {
            for (Vec3& row : a) {
                for (double& v : row) {
                    v = static_cast<double>(stream_f32(&st[kMatrix]));
                }
            }
            if (!st[kMatrix].ok) {
                return false;
            }
        }
        inst.tpl = tpl_of[i];
        for (size_t k = 0; k < 3; ++k) {
            inst.t[k] = static_cast<double>(pos[i][k]) * pos_q;
        }
        const OccluderScene::Template& tpl = scene->templates[inst.tpl];
        inst.lo.fill(std::numeric_limits<double>::max());
        inst.hi.fill(std::numeric_limits<double>::lowest());
        for (uint32_t corner = 0; corner < 8; ++corner) {
            Vec3 c;
            for (size_t j = 0; j < 3; ++j) {
                c[j] = ((corner >> j) & 1U) == 0 ? tpl.lo[j] : tpl.hi[j];
            }
            const Vec3 w = vec_add(mat_vec_mul(a, c), inst.t);
            for (size_t j = 0; j < 3; ++j) {
                inst.lo[j] = std::fmin(inst.lo[j], w[j]);
                inst.hi[j] = std::fmax(inst.hi[j], w[j]);
            }
        }
        inst.invertible = mat_inverse(&inst.m, a);
        if (!inst.invertible) {
            inst.m = a;
        }
    }
    return true;
}

// 块原点同样相对表头原点存
bool decode_blocks(OccluderScene* scene, uint32_t count, double pos_q, const std::array<int64_t, 3>& origin, Stream* st)
{
    const size_t n = scene->block_n;
    const size_t m = n - 1;
    // 每个采样至少占高度流一字节, 先拦住撑爆内存的坏表头
    if (count > 0 && static_cast<uint64_t>(n) * n > stream_size(st[kHeight]) / count) {
        return false;
    }
    const size_t hole_bytes = (m * m + 7) / 8;
    scene->blocks.resize(count);
    int64_t ox = 0;
    int64_t oz = 0;
    int64_t prev_first = 0;
    for (OccluderScene::Block& block : scene->blocks) {
        ox = wrap_add(ox, stream_sv(&st[kBlock]));
        oz = wrap_add(oz, stream_sv(&st[kBlock]));
        const uint8_t flag = stream_byte(&st[kBlock]);
        if (!st[kBlock].ok || flag > 1) {
            return false;
        }
        if (flag == 1) {
            const uint8_t* const mask = stream_take(&st[kHole], hole_bytes);
            if (mask == nullptr) {
                return false;
            }
            block.holes.assign(mask, mask + hole_bytes);
        }
        // 高度按左、上、左上三邻点预测存残差, 每块首个采样接上一块的首个
        std::vector<int64_t> k(n * n);
        for (size_t v = 0; v < n; ++v) {
            for (size_t u = 0; u < n; ++u) {
                const size_t i = v * n + u;
                int64_t pred = prev_first;
                if (u > 0 && v > 0) {
                    pred = k[i - 1] + k[i - n] - k[i - n - 1];
                }
                else if (u > 0) {
                    pred = k[i - 1];
                }
                else if (v > 0) {
                    pred = k[i - n];
                }
                k[i] = wrap_add(pred, stream_sv(&st[kHeight]));
            }
        }
        if (!st[kHeight].ok) {
            return false;
        }
        prev_first = k[0];
        block.k.resize(n * n);
        double y_lo = std::numeric_limits<double>::max();
        double y_hi = std::numeric_limits<double>::lowest();
        for (size_t i = 0; i < k.size(); ++i) {
            if (k[i] < std::numeric_limits<int32_t>::min() || k[i] > std::numeric_limits<int32_t>::max()) {
                return false;
            }
            block.k[i] = static_cast<int32_t>(k[i]);
            const double y = scene->lattice_a + scene->lattice_b * static_cast<double>(k[i]);
            y_lo = std::fmin(y_lo, y);
            y_hi = std::fmax(y_hi, y);
        }
        block.x0 = static_cast<double>(wrap_add(ox, origin[0])) * pos_q;
        block.z0 = static_cast<double>(wrap_add(oz, origin[2])) * pos_q;
        const double span = static_cast<double>(m) * scene->cell;
        block.lo = { block.x0, y_lo, block.z0 };
        block.hi = { block.x0 + span, y_hi, block.z0 + span };
    }
    return true;
}

void build_trees(OccluderScene* scene)
{
    std::vector<Vec3> lo;
    std::vector<Vec3> hi;
    for (OccluderScene::Template& tpl : scene->templates) {
        if (tpl.tris.size() <= kTreeMinTriangles) {
            continue;
        }
        lo.resize(tpl.tris.size());
        hi.resize(tpl.tris.size());
        for (size_t i = 0; i < tpl.tris.size(); ++i) {
            lo[i] = tpl.verts[tpl.tris[i][0]];
            hi[i] = lo[i];
            for (size_t c = 1; c < 3; ++c) {
                const Vec3& vertex = tpl.verts[tpl.tris[i][c]];
                for (size_t k = 0; k < 3; ++k) {
                    lo[i][k] = std::fmin(lo[i][k], vertex[k]);
                    hi[i][k] = std::fmax(hi[i][k], vertex[k]);
                }
            }
        }
        build_tree(&tpl.nodes, &tpl.order, lo, hi, kTriangleBoxPad);
    }
    lo.resize(scene->instances.size());
    hi.resize(scene->instances.size());
    for (size_t i = 0; i < scene->instances.size(); ++i) {
        lo[i] = scene->instances[i].lo;
        hi[i] = scene->instances[i].hi;
    }
    build_tree(&scene->instance_nodes, &scene->instance_order, lo, hi, 0.0);
}

std::shared_ptr<const OccluderScene> decode_scene_body(const uint8_t* data, size_t size)
{
    if (size < kSceneHeaderSize || std::memcmp(data, kSceneTag, sizeof(kSceneTag)) != 0) {
        return nullptr;
    }
    const uint8_t* cursor = data + sizeof(kSceneTag);
    const uint8_t* const end = data + size;
    if (read_u32(&cursor) != kSceneVersion) {
        return nullptr;
    }
    auto scene = std::make_shared<OccluderScene>();
    const uint32_t template_count = read_u32(&cursor);
    const uint32_t instance_count = read_u32(&cursor);
    const uint32_t block_count = read_u32(&cursor);
    scene->block_n = read_u32(&cursor);
    const double pos_q = read_f64(&cursor);
    scene->cell = read_f64(&cursor);
    scene->lattice_a = read_f64(&cursor);
    scene->lattice_b = read_f64(&cursor);
    const double rot_scale = static_cast<double>(read_u32(&cursor));
    const uint32_t scale_shift = read_u32(&cursor);
    const int64_t origin_x = static_cast<int64_t>(read_u64(&cursor));
    const int64_t origin_z = static_cast<int64_t>(read_u64(&cursor));
    const std::array<int64_t, 3> origin { origin_x, 0, origin_z };
    if (scale_shift > 52 || rot_scale == 0.0) {
        return nullptr;
    }
    const double scale_unit = static_cast<double>(uint64_t { 1 } << scale_shift);
    if (block_count > 0 && (scene->block_n < 2 || !(scene->cell > 0.0))) {
        return nullptr;
    }

    Stream st[kStreamCount];
    for (Stream& stream : st) {
        if (static_cast<size_t>(end - cursor) < sizeof(uint32_t)) {
            return nullptr;
        }
        const uint32_t bytes = read_u32(&cursor);
        if (static_cast<size_t>(end - cursor) < bytes) {
            return nullptr;
        }
        stream.begin = cursor;
        stream.cursor = cursor;
        stream.end = cursor + bytes;
        cursor += bytes;
    }
    if (cursor != end) {
        return nullptr;
    }
    // 模板与块在各自流里至少 3 字节, 实例在旋转码流里正好 1 字节
    if (template_count > stream_size(st[kTemplate]) / 3 || instance_count != stream_size(st[kRotationCode])
        || block_count > stream_size(st[kBlock]) / 3) {
        return nullptr;
    }
    if (!decode_templates(scene.get(), template_count, st)
        || !decode_instances(scene.get(), instance_count, pos_q, origin, rot_scale, scale_unit, st)
        || !decode_blocks(scene.get(), block_count, pos_q, origin, st)) {
        return nullptr;
    }
    for (const Stream& stream : st) {
        if (!stream.ok || stream.cursor != stream.end) {
            return nullptr;
        }
    }
    build_trees(scene.get());
    return scene;
}

void push_mesh_hit(std::vector<OccluderHit>* out, const Vec3& a, const Vec3& d, uint32_t index, uint32_t triangle, double s)
{
    OccluderHit& hit = out->emplace_back();
    hit.s = s;
    hit.point = OccluderPoint { .x = a[0] + d[0] * s, .y = a[1] + d[1] * s, .z = a[2] + d[2] * s };
    hit.instance = index;
    hit.triangle = triangle;
}

struct Level
{
    double y = 0.0;
    bool up = false;
};

std::vector<Level> column_levels(const OccluderScene& scene, const OccluderPoint& at, double bottom, double top)
{
    std::vector<Level> levels;
    for (const Face& face : faces_in_box(scene, Vec3 { at.x, bottom, at.z }, Vec3 { at.x, top, at.z })) {
        const std::optional<double> y = height_at(face.v, at.x, at.z);
        if (y.has_value() && *y > bottom && *y < top) {
            levels.push_back(Level { .y = *y, .up = face.up });
        }
    }
    return levels;
}

std::optional<double> floor_under(const std::vector<Level>& levels, double climb, double y)
{
    std::optional<double> best;
    for (const Level& level : levels) {
        if (level.up && std::fabs(level.y - y) <= climb && (!best.has_value() || level.y > *best)) {
            best = level.y;
        }
    }
    return best;
}

struct InstanceSegment
{
    Vec3 a;
    Vec3 d;
    Vec3 la;
    Vec3 ld;
    double d2 = 0.0;
};

void test_local_triangle(
    std::vector<OccluderHit>* out,
    const InstanceSegment& seg,
    const OccluderScene::Template& tpl,
    uint32_t index,
    uint32_t k)
{
    const auto& tri = tpl.tris[k];
    double s = 0.0;
    if (seg_tri(&s, seg.la, seg.ld, seg.d2, tpl.verts[tri[0]], tpl.verts[tri[1]], tpl.verts[tri[2]])) {
        push_mesh_hit(out, seg.a, seg.d, index, k, s);
    }
}

} // namespace

std::vector<OccluderHit> OccluderLineHits(const OccluderScene& scene, const OccluderPoint& start, const OccluderPoint& finish)
{
    const Vec3 a { start.x, start.y, start.z };
    const Vec3 b { finish.x, finish.y, finish.z };
    const Vec3 d = vec_sub(b, a);
    std::vector<OccluderHit> out;

    for (const uint32_t index : tree_leaves_on_segment(scene.instance_nodes, scene.instance_order, a, d)) {
        const OccluderScene::Instance& inst = scene.instances[index];
        if (!seg_box(a, d, inst.lo, inst.hi)) {
            continue;
        }
        const OccluderScene::Template& tpl = scene.templates[inst.tpl];
        if (!inst.invertible) {
            const double d2 = vec_dot(d, d);
            for (uint32_t k = 0; k < tpl.tris.size(); ++k) {
                const auto& tri = tpl.tris[k];
                const Vec3 v0 = vec_add(mat_vec_mul(inst.m, tpl.verts[tri[0]]), inst.t);
                const Vec3 v1 = vec_add(mat_vec_mul(inst.m, tpl.verts[tri[1]]), inst.t);
                const Vec3 v2 = vec_add(mat_vec_mul(inst.m, tpl.verts[tri[2]]), inst.t);
                double s = 0.0;
                if (seg_tri(&s, a, d, d2, v0, v1, v2)) {
                    push_mesh_hit(&out, a, d, index, k, s);
                }
            }
            continue;
        }
        // 换进模板局部系求交, 沿线参数不变
        const Vec3 la = mat_vec_mul(inst.m, vec_sub(a, inst.t));
        const Vec3 ld = vec_sub(mat_vec_mul(inst.m, vec_sub(b, inst.t)), la);
        const double d2 = vec_dot(ld, ld);
        const InstanceSegment seg { .a = a, .d = d, .la = la, .ld = ld, .d2 = d2 };
        if (tpl.nodes.empty()) {
            for (uint32_t k = 0; k < tpl.tris.size(); ++k) {
                test_local_triangle(&out, seg, tpl, index, k);
            }
        }
        else {
            for (const uint32_t k : tree_leaves_on_segment(tpl.nodes, tpl.order, la, ld)) {
                test_local_triangle(&out, seg, tpl, index, k);
            }
        }
    }

    std::sort(out.begin(), out.end(), [&](const OccluderHit& x, const OccluderHit& y) {
        if (x.s != y.s) {
            return x.s < y.s;
        }
        if (std::signbit(x.s) != std::signbit(y.s)) {
            return std::signbit(x.s);
        }
        return std::tie(x.instance, x.triangle) < std::tie(y.instance, y.triangle);
    });
    return out;
}

double OccluderGroundHeight(const OccluderScene& scene, const OccluderPoint& base)
{
    const double x0 = std::floor(base.x - kSettleHalfWidth);
    const double z0 = std::floor(base.z - kSettleHalfWidth);
    const double x1 = x0 + 2.0 * kSettleHalfWidth;
    const double z1 = z0 + 2.0 * kSettleHalfWidth;
    const double start = base.y + kSettleLift;
    const double bottom = start - kSettlePlateBelow;
    const double top = start + kSettlePlateAbove;
    const Vec3 lo { x0, bottom - kSettleDrop, z0 };
    const Vec3 hi { x1, top, z1 };
    const std::vector<Face> faces = faces_in_box(scene, lo, hi);

    bool touching = false;
    double landed = -std::numeric_limits<double>::infinity();
    for (const Face& face : faces) {
        std::array<Vec3, kClipMaxVertices> poly;
        const size_t count = clip_to_rect(&poly, face.v, x0, x1, z0, z1);
        if (count == 0) {
            continue;
        }
        double y_lo = std::numeric_limits<double>::infinity();
        double y_hi = -std::numeric_limits<double>::infinity();
        for (size_t i = 0; i < count; ++i) {
            y_lo = std::fmin(y_lo, poly[i][1]);
            y_hi = std::fmax(y_hi, poly[i][1]);
        }
        if (y_hi >= bottom - kSettleTouch && y_lo <= top) {
            touching = true;
        }
        else if (face.up && y_hi < bottom && y_hi >= bottom - kSettleDrop) {
            landed = std::fmax(landed, y_hi);
        }
    }
    if (touching) {
        landed = -std::numeric_limits<double>::infinity();
        const Vec3 d { 0.0, -kSettleDrop, 0.0 };
        const double d2 = vec_dot(d, d);
        for (const double cx : { x0, x1 }) {
            for (const double cz : { z0, z1 }) {
                const Vec3 o { cx, start, cz };
                for (const Face& face : faces) {
                    double s = 0.0;
                    if (face.up && seg_tri(&s, o, d, d2, face.v[0], face.v[1], face.v[2])) {
                        landed = std::fmax(landed, start + d[1] * s);
                    }
                }
            }
        }
    }
    const double ground = std::isfinite(landed) ? landed : base.y;
    return std::fmin(std::fmax(ground, base.y - kSettleClamp), base.y + kSettleClamp);
}

bool OccluderDropBlocked(const OccluderScene& scene, const OccluderPoint& take, const OccluderPoint& land, double climb, double height)
{
    const std::vector<Level> at_take = column_levels(scene, take, take.y - climb - kColumnEdge, take.y + climb + kColumnEdge);
    const std::vector<Level> at_land =
        column_levels(scene, land, land.y - climb - kColumnEdge, std::fmax(take.y, land.y + climb + kColumnEdge));
    if (!floor_under(at_land, climb, land.y).has_value()) {
        return false;
    }
    for (const Level& level : at_land) {
        if (!level.up && level.y > land.y + climb && level.y < take.y) {
            return true;
        }
    }
    const std::optional<double> take_floor = floor_under(at_take, climb, take.y);
    if (!take_floor.has_value()) {
        return false;
    }
    const double base = std::fmax(*take_floor, take.y);
    const double lo = base + climb;
    const double hi = base + height;
    const Vec3 box_lo { std::fmin(take.x, land.x), lo, std::fmin(take.z, land.z) };
    const Vec3 box_hi { std::fmax(take.x, land.x), hi, std::fmax(take.z, land.z) };
    for (const Face& face : faces_in_box(scene, box_lo, box_hi)) {
        if (crosses_curtain(face.v, take.x, take.z, land.x, land.z, lo, hi)) {
            return true;
        }
    }
    return false;
}

std::shared_ptr<const OccluderScene> DecodeOccluderScene(const uint8_t* data, size_t size, std::string_view zone_name)
{
    if (size < kContainerHeaderSize || std::memcmp(data, kContainerTag, sizeof(kContainerTag)) != 0) {
        return nullptr;
    }
    const uint8_t* cursor = data + sizeof(kContainerTag);
    const uint8_t* const end = data + size;
    if (read_u32(&cursor) != kContainerVersion) {
        return nullptr;
    }
    const uint32_t count = read_u32(&cursor);
    const uint8_t* body = nullptr;
    size_t body_size = 0;
    for (uint32_t i = 0; i < count; ++i) {
        // 每条记录: u16 名长、zone 名、u32 本体长、本体
        if (static_cast<size_t>(end - cursor) < sizeof(uint16_t)) {
            return nullptr;
        }
        const uint16_t name_bytes = read_u16(&cursor);
        if (static_cast<size_t>(end - cursor) < name_bytes) {
            return nullptr;
        }
        const std::string_view name(reinterpret_cast<const char*>(cursor), name_bytes);
        cursor += name_bytes;
        if (static_cast<size_t>(end - cursor) < sizeof(uint32_t)) {
            return nullptr;
        }
        const uint32_t bytes = read_u32(&cursor);
        if (static_cast<size_t>(end - cursor) < bytes) {
            return nullptr;
        }
        if (name == zone_name) {
            body = cursor;
            body_size = bytes;
        }
        cursor += bytes;
    }
    if (cursor != end || body == nullptr) {
        return nullptr;
    }
    return decode_scene_body(body, body_size);
}

std::filesystem::path OccluderSidecarPath(const std::filesystem::path& main_pack)
{
    std::string name = MAA_NS::path_to_utf8_string(main_pack.filename());
    for (const char* suffix : { ".nav.gz", ".nav" }) {
        const size_t width = std::strlen(suffix);
        if (name.size() >= width && name.compare(name.size() - width, width, suffix) == 0) {
            name.resize(name.size() - width);
            break;
        }
    }
    name += ".occluder.gz";
    return main_pack.parent_path() / MAA_NS::path(name);
}

std::shared_ptr<const OccluderScene> LoadOccluderScene(const std::filesystem::path& path, const std::string& zone_name)
{
    static std::mutex mutex;
    static std::unordered_map<std::string, std::shared_ptr<const OccluderScene>> cache;
    std::error_code ec;
    std::filesystem::path absolute = std::filesystem::absolute(path, ec);
    if (ec) {
        absolute = path;
    }
    const std::string cache_key = MAA_NS::path_to_utf8_string(absolute.lexically_normal()) + "#" + zone_name;

    const std::lock_guard<std::mutex> lock(mutex);
    if (const auto iter = cache.find(cache_key); iter != cache.end()) {
        return iter->second;
    }
    std::shared_ptr<const OccluderScene> scene;
    std::vector<uint8_t> bytes;
    const BaseNavLoadResult read = ReadNavFileBytes(path, &bytes);
    if (read.status != BaseNavLoadStatus::Success) {
        LogError << "Failed to read the occluder pack." << VAR(path) << VAR(read.message);
    }
    else {
        scene = DecodeOccluderScene(bytes.data(), bytes.size(), zone_name);
        if (!scene) {
            LogError << "Failed to decode the occluder scene." << VAR(path) << VAR(zone_name) << VAR(bytes.size());
        }
        else {
            LogInfo << "Occluder scene loaded." << VAR(zone_name) << VAR(scene->templates.size()) << VAR(scene->instances.size())
                    << VAR(scene->blocks.size());
        }
    }
    cache.emplace(cache_key, scene);
    return scene;
}

} // namespace navmesh
