#include "OccluderPack.h"

#include <algorithm>
#include <bit>
#include <cmath>
#include <cstring>
#include <limits>
#include <numbers>
#include <numeric>
#include <string>
#include <tuple>

#include <MaaUtils/Platform.h>

#include "NavVarint.h"

// 求交要与烘焙端逐位相同,不许把乘加合成一条指令。
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

// 容器与场景本体的魔数和格式版本,与烘焙端写出的一致,对不上就整份不认。
constexpr char kContainerTag[4] = { 'O', 'C', 'L', 'S' };
constexpr char kSceneTag[4] = { 'O', 'C', 'L', 'Z' };
constexpr uint32_t kContainerVersion = 1;
constexpr uint32_t kSceneVersion = 1;
// 容器表头:魔数、版本、场景数,各 4 字节。
constexpr size_t kContainerHeaderSize = 12;
// 场景表头:魔数、版本、模板数、实例数、地形块数、块边长各 4 字节,位置量化步长、格宽、高度格点 a/b 各 8 字节,
// 旋转量化分母、缩放移位各 4 字节,原点 x、z 各 8 字节。
constexpr size_t kSceneHeaderSize = 80;

// 表头之后的流,顺序与烘焙端一致。
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

// 线段与三角所在平面夹角正弦的平方不到这个数就当平行,不算交。取值与烘焙端的问线相同,两边逐条判得一样。
constexpr double kParallelSin2 = 1e-24;
// 包围盒判交时各面外扩这么多米,吸收摆放时的舍入。取值与烘焙端的问线相同。
constexpr double kBoxPad = 1e-6;
// 三角的包围盒再外扩这么多(模板局部单位),挡住近乎贴面的线段求交时的舍入,树只用来少算,不能漏算。
// 只要盖得住舍入,取大只多算几个三角,不改结果。
constexpr double kTriangleBoxPad = 1e-3;
// 三角超过这么多的模板才建树,一片叶子最多装这么多项。两个值只影响查询快慢,不影响结果。
constexpr size_t kTreeMinTriangles = 8;
constexpr uint32_t kLeafSize = 4;
// 四元数除最大分量外的三个分量按 [-1/√2, 1/√2] 存。
constexpr double kFrac1Sqrt2 = std::numbers::sqrt2 / 2.0;
// 建筑落地只看塔底附近一个 1 m 见方的格,这是它的半宽(米)。格按整米对齐,与建造网格同一步长。
constexpr double kSettleHalfWidth = 0.5;
// 薄板从名义塔底上方这么多米处起落。名义塔底是建造格的整米高度,实际地面可能高出它,从上方起落才接得住。
constexpr double kSettleLift = 1.0;
// 薄板底面比起落高度低这么多米、顶面高这么多米。薄板只用来判断起落处是否已经贴着面,
// 厚度只在面恰好停在起落高度附近万分之一米内时起作用,远小于绳与障碍之间的余量。
constexpr double kSettlePlateBelow = 5e-5;
constexpr double kSettlePlateAbove = 1.5e-4;
// 面的最高点伸到板底下这么多米以内也算贴着,吸收浮点舍入。
constexpr double kSettleTouch = 1e-5;
// 薄板最多往下落这么多米找地,落到底还没碰上面就保留名义塔底。与 kSettleLift 一起盖住名义塔底上下各 1 m。
constexpr double kSettleDrop = 2.0;
// 落地结果离名义塔底最多这么多米。名义塔底按整米取,实际地面与它差不出这个范围。
constexpr double kSettleClamp = 1.0;
// 三角裁进竖直柱子后最多几个顶点:三角本身 3 个,四刀每刀最多多出一个。
constexpr size_t kClipMaxVertices = 8;

uint16_t ReadU16(const uint8_t*& cursor)
{
    const uint16_t value = static_cast<uint16_t>(cursor[0] | (cursor[1] << 8U));
    cursor += 2;
    return value;
}

uint32_t ReadU32(const uint8_t*& cursor)
{
    const uint32_t value = static_cast<uint32_t>(cursor[0]) | (static_cast<uint32_t>(cursor[1]) << 8U)
                           | (static_cast<uint32_t>(cursor[2]) << 16U) | (static_cast<uint32_t>(cursor[3]) << 24U);
    cursor += 4;
    return value;
}

uint64_t ReadU64(const uint8_t*& cursor)
{
    const uint64_t low = ReadU32(cursor);
    const uint64_t high = ReadU32(cursor);
    return low | (high << 32U);
}

double ReadF64(const uint8_t*& cursor)
{
    return std::bit_cast<double>(ReadU64(cursor));
}

// 一条流的顺序读取。读过头或变长整数不合规时 ok 置 false,此后读到的都是 0。
struct Stream
{
    const uint8_t* begin = nullptr;
    const uint8_t* cursor = nullptr;
    const uint8_t* end = nullptr;
    bool ok = true;

    size_t size() const { return static_cast<size_t>(end - begin); }

    uint8_t Byte()
    {
        if (!ok || cursor == end) {
            ok = false;
            return 0;
        }
        return *cursor++;
    }

    uint64_t Uv()
    {
        uint64_t value = 0;
        if (!ok || !GetVarint(cursor, end, value)) {
            ok = false;
            return 0;
        }
        return value;
    }

    int64_t Sv() { return UnZigzag(Uv()); }

    float F32()
    {
        if (!ok || end - cursor < 4) {
            ok = false;
            return 0.0F;
        }
        return std::bit_cast<float>(ReadU32(cursor));
    }

    const uint8_t* Take(size_t bytes)
    {
        if (!ok || static_cast<size_t>(end - cursor) < bytes) {
            ok = false;
            return nullptr;
        }
        const uint8_t* const start = cursor;
        cursor += bytes;
        return start;
    }
};

// 增量累加按补码回绕,坏数据不至于触发有符号溢出。
int64_t WrapAdd(int64_t a, int64_t b)
{
    return static_cast<int64_t>(static_cast<uint64_t>(a) + static_cast<uint64_t>(b));
}

Vec3 Sub(const Vec3& a, const Vec3& b)
{
    return { a[0] - b[0], a[1] - b[1], a[2] - b[2] };
}

Vec3 Add(const Vec3& a, const Vec3& b)
{
    return { a[0] + b[0], a[1] + b[1], a[2] + b[2] };
}

Vec3 Scale(const Vec3& v, double s)
{
    return { v[0] * s, v[1] * s, v[2] * s };
}

double Dot(const Vec3& a, const Vec3& b)
{
    return a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
}

Vec3 Cross(const Vec3& a, const Vec3& b)
{
    return { a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0] };
}

Vec3 Mul(const Mat3& m, const Vec3& v)
{
    return { Dot(m[0], v), Dot(m[1], v), Dot(m[2], v) };
}

double Det(const Mat3& m)
{
    return Dot(m[0], Cross(m[1], m[2]));
}

// 行列式恰为 0 或不是有限数(压扁成面、线的摆放)时没有逆。
bool Inverse(const Mat3& m, Mat3& out)
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
            out[i][j] = c[j][i] / det;
        }
    }
    return true;
}

// 量化的旋转与缩放还原成 a = R·diag(s)。code 是四元数 (x, y, z, w) 里被略去的最大分量的位置,
// rot 是其余三个分量按原顺序,scale 是 s·scale_unit。
Mat3 AffineDecode(uint8_t code, const std::array<int64_t, 3>& rot, const std::array<int64_t, 3>& scale, double rot_scale, double scale_unit)
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

// 线段 o + s·d(s ∈ [0, 1])碰不碰得到轴对齐盒(各面外扩 kBoxPad)。盒子变大时结果只会从不碰变成碰,
// 所以拿装得下子节点的盒子先判,不会漏掉子节点里碰得到的项。
bool SegBox(const Vec3& o, const Vec3& d, const Vec3& lo, const Vec3& hi)
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

// 线段 o + s·d(s ∈ [0, 1])与三角求交(Möller–Trumbore),正反面都算,落在边、角、线段端点上也算。
// 三角退化成线或点、线段与三角所在平面平行时不算交。d2 = |d|²。
bool SegTri(const Vec3& o, const Vec3& d, double d2, const Vec3& v0, const Vec3& v1, const Vec3& v2, double& t)
{
    const Vec3 e1 = Sub(v1, v0);
    const Vec3 e2 = Sub(v2, v0);
    const Vec3 n = Cross(e1, e2);
    const double n2 = Dot(n, n);
    const Vec3 p = Cross(d, e2);
    const double det = Dot(e1, p);
    if (n2 == 0.0 || det * det <= kParallelSin2 * n2 * d2) {
        return false;
    }
    const double inv = 1.0 / det;
    const Vec3 s = Sub(o, v0);
    const double u = Dot(s, p) * inv;
    if (!(u >= 0.0 && u <= 1.0)) {
        return false;
    }
    const Vec3 q = Cross(s, e1);
    const double v = Dot(d, q) * inv;
    if (v < 0.0 || u + v > 1.0) {
        return false;
    }
    t = Dot(e2, q) * inv;
    return t >= 0.0 && t <= 1.0;
}

// 三角裁到竖直的柱子 [x0, x1] × [z0, z1] 里(边界上的也留),裁剩的多边形写进 poly,返回顶点数,裁没了是 0。
size_t ClipToRect(const Vec3 (&tri)[3], double x0, double x1, double z0, double z1, std::array<Vec3, kClipMaxVertices>& poly)
{
    struct Cut
    {
        size_t axis;
        double bound;
        bool keep_above;
    };

    const Cut cuts[4] = { { 0, x0, true }, { 0, x1, false }, { 2, z0, true }, { 2, z1, false } };
    std::copy(std::begin(tri), std::end(tri), poly.begin());
    size_t count = 3;
    std::array<Vec3, kClipMaxVertices> next {};
    for (const Cut& cut : cuts) {
        const auto inside = [&](const Vec3& p) {
            return cut.keep_above ? p[cut.axis] >= cut.bound : p[cut.axis] <= cut.bound;
        };
        size_t kept = 0;
        for (size_t i = 0; i < count; ++i) {
            const Vec3& p = poly[i];
            const Vec3& q = poly[(i + 1) % count];
            if (inside(p)) {
                next[kept++] = p;
            }
            if (inside(p) != inside(q)) {
                next[kept++] = Add(p, Scale(Sub(q, p), (cut.bound - p[cut.axis]) / (q[cut.axis] - p[cut.axis])));
            }
        }
        poly = next;
        count = kept;
        if (count == 0) {
            break;
        }
    }
    return count;
}

// double 向下、向上取到 float,保证 float 盒子装得下原来的盒子。
float FloatDown(double value)
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

float FloatUp(double value)
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

struct TreeBuild
{
    const std::vector<Vec3>& lo;
    const std::vector<Vec3>& hi;
    const std::vector<Vec3>& centre;
    double pad;
    std::vector<BoxNode>& nodes;
    std::vector<uint32_t>& order;

    uint32_t Node(uint32_t begin, uint32_t end)
    {
        const uint32_t index = static_cast<uint32_t>(nodes.size());
        nodes.emplace_back();
        Vec3 box_lo;
        Vec3 box_hi;
        box_lo.fill(std::numeric_limits<double>::infinity());
        box_hi.fill(-std::numeric_limits<double>::infinity());
        Vec3 centre_lo = box_lo;
        Vec3 centre_hi = box_hi;
        for (uint32_t i = begin; i < end; ++i) {
            const uint32_t item = order[i];
            for (size_t k = 0; k < 3; ++k) {
                box_lo[k] = std::fmin(box_lo[k], lo[item][k] - pad);
                box_hi[k] = std::fmax(box_hi[k], hi[item][k] + pad);
                centre_lo[k] = std::fmin(centre_lo[k], centre[item][k]);
                centre_hi[k] = std::fmax(centre_hi[k], centre[item][k]);
            }
        }
        for (size_t k = 0; k < 3; ++k) {
            nodes[index].lo[k] = FloatDown(box_lo[k]);
            nodes[index].hi[k] = FloatUp(box_hi[k]);
        }
        if (end - begin <= kLeafSize) {
            nodes[index].first = begin;
            nodes[index].count = end - begin;
            return index;
        }
        size_t axis = 0;
        for (size_t k = 1; k < 3; ++k) {
            if (centre_hi[k] - centre_lo[k] > centre_hi[axis] - centre_lo[axis]) {
                axis = k;
            }
        }
        const uint32_t mid = begin + (end - begin) / 2;
        std::nth_element(order.begin() + begin, order.begin() + mid, order.begin() + end, [&](uint32_t a, uint32_t b) {
            return std::tie(centre[a][axis], a) < std::tie(centre[b][axis], b);
        });
        Node(begin, mid);
        const uint32_t right = Node(mid, end);
        nodes[index].first = right;
        nodes[index].count = 0;
        return index;
    }
};

// 按包围盒中心沿最长轴对半分的包围盒树。每项的盒子各面先外扩 pad。
void BuildTree(
    const std::vector<Vec3>& lo,
    const std::vector<Vec3>& hi,
    double pad,
    std::vector<BoxNode>& nodes,
    std::vector<uint32_t>& order)
{
    const uint32_t count = static_cast<uint32_t>(lo.size());
    order.resize(count);
    std::iota(order.begin(), order.end(), 0U);
    nodes.clear();
    if (count == 0) {
        return;
    }
    std::vector<Vec3> centre(count);
    for (uint32_t i = 0; i < count; ++i) {
        for (size_t k = 0; k < 3; ++k) {
            centre[i][k] = 0.5 * (lo[i][k] + hi[i][k]);
        }
    }
    nodes.reserve(2 * static_cast<size_t>(count));
    TreeBuild { .lo = lo, .hi = hi, .centre = centre, .pad = pad, .nodes = nodes, .order = order }.Node(0, count);
    nodes.shrink_to_fit();
}

// 两个轴对齐盒碰不碰得到(第二个各面外扩 kBoxPad)。与 SegBox 一样,盒子变大时只会从不碰变成碰。
bool BoxBox(const Vec3& lo, const Vec3& hi, const Vec3& other_lo, const Vec3& other_hi)
{
    for (size_t k = 0; k < 3; ++k) {
        if (other_lo[k] - kBoxPad > hi[k] || other_hi[k] + kBoxPad < lo[k]) {
            return false;
        }
    }
    return true;
}

// 沿树找 reaches(节点盒)为真的叶子,逐项交给 visit。对半分的树深不过 32 层,栈开 64 足够。
template <typename Reaches, typename Visit>
void VisitTree(const std::vector<BoxNode>& nodes, const std::vector<uint32_t>& order, Reaches&& reaches, Visit&& visit)
{
    if (nodes.empty()) {
        return;
    }
    uint32_t stack[64];
    size_t top = 0;
    stack[top++] = 0;
    while (top > 0) {
        const uint32_t index = stack[--top];
        const BoxNode& node = nodes[index];
        const Vec3 lo { node.lo[0], node.lo[1], node.lo[2] };
        const Vec3 hi { node.hi[0], node.hi[1], node.hi[2] };
        if (!reaches(lo, hi)) {
            continue;
        }
        if (node.count > 0) {
            for (uint32_t i = node.first; i < node.first + node.count; ++i) {
                visit(order[i]);
            }
            continue;
        }
        stack[top++] = node.first;
        stack[top++] = index + 1;
    }
}

bool DecodeTemplates(OccluderScene& scene, uint32_t count, Stream* st)
{
    scene.templates.resize(count);
    for (OccluderScene::Template& tpl : scene.templates) {
        const uint64_t vertex_count = st[kTemplate].Uv();
        const uint64_t triangle_count = st[kTemplate].Uv();
        const int64_t exponent = st[kTemplate].Sv();
        if (!st[kTemplate].ok || exponent < -60 || exponent > 60 || vertex_count > st[kVertex].size()
            || triangle_count > st[kIndex].size()) {
            return false;
        }
        const double step = std::ldexp(1.0, static_cast<int>(exponent));
        tpl.verts.resize(vertex_count);
        for (size_t axis = 0; axis < 3; ++axis) {
            int64_t prev = 0;
            for (Vec3& vertex : tpl.verts) {
                prev = WrapAdd(prev, st[kVertex].Sv());
                vertex[axis] = static_cast<double>(prev) * step;
            }
        }
        tpl.tris.resize(triangle_count);
        uint64_t next = 0;
        for (std::array<uint32_t, 3>& tri : tpl.tris) {
            for (uint32_t& corner : tri) {
                const uint64_t back = st[kIndex].Uv();
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

// 位置按位置步长的整数存,x、z 是相对表头原点的增量,y 原点恒为 0。
bool DecodeInstances(
    OccluderScene& scene,
    uint32_t count,
    double pos_q,
    const std::array<int64_t, 3>& origin,
    double rot_scale,
    double scale_unit,
    Stream* st)
{
    // 实例按模板排好,逐模板一个数
    std::vector<uint32_t> tpl_of;
    tpl_of.reserve(count);
    for (uint32_t k = 0; k < scene.templates.size(); ++k) {
        const uint64_t n = st[kInstanceCount].Uv();
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
            prev = WrapAdd(prev, st[kPosition].Sv());
            p[axis] = WrapAdd(prev, origin[axis]);
        }
    }
    const uint8_t* const codes = st[kRotationCode].Take(count);
    if (!st[kPosition].ok || codes == nullptr || std::any_of(codes, codes + count, [](uint8_t code) { return code > 4; })) {
        return false;
    }
    const size_t quat_count = static_cast<size_t>(std::count_if(codes, codes + count, [](uint8_t code) { return code < 4; }));
    std::vector<std::array<int64_t, 3>> rot(quat_count);
    const int64_t rot_limit = static_cast<int64_t>(rot_scale);
    for (size_t k = 0; k < 3; ++k) {
        for (std::array<int64_t, 3>& r : rot) {
            r[k] = st[kRotation].Sv();
            if (r[k] > rot_limit || r[k] < -rot_limit) {
                return false;
            }
        }
    }
    const uint64_t scale_count = st[kScale].Uv();
    if (!st[kRotation].ok || !st[kScale].ok || scale_count > st[kScale].size()) {
        return false;
    }
    std::vector<std::array<int64_t, 3>> scales(scale_count);
    for (std::array<int64_t, 3>& s : scales) {
        for (int64_t& v : s) {
            v = st[kScale].Sv();
        }
    }

    scene.instances.resize(count);
    size_t quat_index = 0;
    for (uint32_t i = 0; i < count; ++i) {
        OccluderScene::Instance& inst = scene.instances[i];
        Mat3 a {};
        if (codes[i] < 4) {
            const uint64_t scale_index = st[kScale].Uv();
            if (!st[kScale].ok || scale_index >= scales.size()) {
                return false;
            }
            a = AffineDecode(codes[i], rot[quat_index++], scales[scale_index], rot_scale, scale_unit);
        }
        else {
            for (Vec3& row : a) {
                for (double& v : row) {
                    v = static_cast<double>(st[kMatrix].F32());
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
        const OccluderScene::Template& tpl = scene.templates[inst.tpl];
        inst.lo.fill(std::numeric_limits<double>::max());
        inst.hi.fill(std::numeric_limits<double>::lowest());
        for (uint32_t corner = 0; corner < 8; ++corner) {
            Vec3 c;
            for (size_t j = 0; j < 3; ++j) {
                c[j] = ((corner >> j) & 1U) == 0 ? tpl.lo[j] : tpl.hi[j];
            }
            const Vec3 w = Add(Mul(a, c), inst.t);
            for (size_t j = 0; j < 3; ++j) {
                inst.lo[j] = std::fmin(inst.lo[j], w[j]);
                inst.hi[j] = std::fmax(inst.hi[j], w[j]);
            }
        }
        inst.invertible = Inverse(a, inst.m);
        if (!inst.invertible) {
            inst.m = a;
        }
    }
    return true;
}

// 块原点与实例位置同样相对表头原点存。
bool DecodeBlocks(OccluderScene& scene, uint32_t count, double pos_q, const std::array<int64_t, 3>& origin, Stream* st)
{
    const size_t n = scene.block_n;
    const size_t m = n - 1;
    // 每个采样至少占高度流一个字节,先拦住撑爆内存的坏表头
    if (count > 0 && static_cast<uint64_t>(n) * n > st[kHeight].size() / count) {
        return false;
    }
    const size_t hole_bytes = (m * m + 7) / 8;
    scene.blocks.resize(count);
    int64_t ox = 0;
    int64_t oz = 0;
    int64_t prev_first = 0;
    for (OccluderScene::Block& block : scene.blocks) {
        ox = WrapAdd(ox, st[kBlock].Sv());
        oz = WrapAdd(oz, st[kBlock].Sv());
        const uint8_t flag = st[kBlock].Byte();
        if (!st[kBlock].ok || flag > 1) {
            return false;
        }
        if (flag == 1) {
            const uint8_t* const mask = st[kHole].Take(hole_bytes);
            if (mask == nullptr) {
                return false;
            }
            block.holes.assign(mask, mask + hole_bytes);
        }
        // 高度是格点整数,按左、上、左上三个邻点预测存残差;每块第一个采样接着上一块的第一个
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
                k[i] = WrapAdd(pred, st[kHeight].Sv());
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
            const double y = scene.lattice_a + scene.lattice_b * static_cast<double>(k[i]);
            y_lo = std::fmin(y_lo, y);
            y_hi = std::fmax(y_hi, y);
        }
        block.x0 = static_cast<double>(WrapAdd(ox, origin[0])) * pos_q;
        block.z0 = static_cast<double>(WrapAdd(oz, origin[2])) * pos_q;
        const double span = static_cast<double>(m) * scene.cell;
        block.lo = { block.x0, y_lo, block.z0 };
        block.hi = { block.x0 + span, y_hi, block.z0 + span };
    }
    return true;
}

void BuildTrees(OccluderScene& scene)
{
    std::vector<Vec3> lo;
    std::vector<Vec3> hi;
    for (OccluderScene::Template& tpl : scene.templates) {
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
        BuildTree(lo, hi, kTriangleBoxPad, tpl.nodes, tpl.order);
    }
    lo.resize(scene.instances.size());
    hi.resize(scene.instances.size());
    for (size_t i = 0; i < scene.instances.size(); ++i) {
        lo[i] = scene.instances[i].lo;
        hi[i] = scene.instances[i].hi;
    }
    BuildTree(lo, hi, 0.0, scene.instance_nodes, scene.instance_order);
}

std::shared_ptr<const OccluderScene> DecodeSceneBody(const uint8_t* data, size_t size)
{
    if (size < kSceneHeaderSize || std::memcmp(data, kSceneTag, sizeof(kSceneTag)) != 0) {
        return nullptr;
    }
    const uint8_t* cursor = data + sizeof(kSceneTag);
    const uint8_t* const end = data + size;
    if (ReadU32(cursor) != kSceneVersion) {
        return nullptr;
    }
    auto scene = std::make_shared<OccluderScene>();
    const uint32_t template_count = ReadU32(cursor);
    const uint32_t instance_count = ReadU32(cursor);
    const uint32_t block_count = ReadU32(cursor);
    scene->block_n = ReadU32(cursor);
    const double pos_q = ReadF64(cursor);
    scene->cell = ReadF64(cursor);
    scene->lattice_a = ReadF64(cursor);
    scene->lattice_b = ReadF64(cursor);
    const double rot_scale = static_cast<double>(ReadU32(cursor));
    const uint32_t scale_shift = ReadU32(cursor);
    const int64_t origin_x = static_cast<int64_t>(ReadU64(cursor));
    const int64_t origin_z = static_cast<int64_t>(ReadU64(cursor));
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
        const uint32_t bytes = ReadU32(cursor);
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
    // 每个模板在模板流里至少三个字节,每个实例在旋转码流里正好一个字节,每块在块流里至少三个字节
    if (template_count > st[kTemplate].size() / 3 || instance_count != st[kRotationCode].size() || block_count > st[kBlock].size() / 3) {
        return nullptr;
    }
    if (!DecodeTemplates(*scene, template_count, st) || !DecodeInstances(*scene, instance_count, pos_q, origin, rot_scale, scale_unit, st)
        || !DecodeBlocks(*scene, block_count, pos_q, origin, st)) {
        return nullptr;
    }
    for (const Stream& stream : st) {
        if (!stream.ok || stream.cursor != stream.end) {
            return nullptr;
        }
    }
    BuildTrees(*scene);
    return scene;
}

} // namespace

std::vector<OccluderHit> OccluderScene::lineHits(const OccluderPoint& start, const OccluderPoint& finish) const
{
    const Vec3 a { start.x, start.y, start.z };
    const Vec3 b { finish.x, finish.y, finish.z };
    const Vec3 d = Sub(b, a);
    std::vector<OccluderHit> out;
    const auto push = [&](double s) -> OccluderHit& {
        OccluderHit& hit = out.emplace_back();
        hit.s = s;
        hit.point = OccluderPoint { .x = a[0] + d[0] * s, .y = a[1] + d[1] * s, .z = a[2] + d[2] * s };
        return hit;
    };

    const auto reaches = [&](const Vec3& lo, const Vec3& hi) {
        return SegBox(a, d, lo, hi);
    };
    VisitTree(instance_nodes, instance_order, reaches, [&](uint32_t index) {
        const Instance& inst = instances[index];
        if (!SegBox(a, d, inst.lo, inst.hi)) {
            return;
        }
        const Template& tpl = templates[inst.tpl];
        const auto mesh_hit = [&](uint32_t triangle, double s) {
            OccluderHit& hit = push(s);
            hit.instance = index;
            hit.triangle = triangle;
        };
        if (!inst.invertible) {
            const double d2 = Dot(d, d);
            for (uint32_t k = 0; k < tpl.tris.size(); ++k) {
                const auto& tri = tpl.tris[k];
                const Vec3 v0 = Add(Mul(inst.m, tpl.verts[tri[0]]), inst.t);
                const Vec3 v1 = Add(Mul(inst.m, tpl.verts[tri[1]]), inst.t);
                const Vec3 v2 = Add(Mul(inst.m, tpl.verts[tri[2]]), inst.t);
                double s = 0.0;
                if (SegTri(a, d, d2, v0, v1, v2, s)) {
                    mesh_hit(k, s);
                }
            }
            return;
        }
        // 可逆的摆放把线段换进模板局部系求交,沿线参数不变
        const Vec3 la = Mul(inst.m, Sub(a, inst.t));
        const Vec3 ld = Sub(Mul(inst.m, Sub(b, inst.t)), la);
        const double d2 = Dot(ld, ld);
        const auto test = [&](uint32_t k) {
            const auto& tri = tpl.tris[k];
            double s = 0.0;
            if (SegTri(la, ld, d2, tpl.verts[tri[0]], tpl.verts[tri[1]], tpl.verts[tri[2]], s)) {
                mesh_hit(k, s);
            }
        };
        if (tpl.nodes.empty()) {
            for (uint32_t k = 0; k < tpl.tris.size(); ++k) {
                test(k);
            }
        }
        else {
            const auto local_reaches = [&](const Vec3& lo, const Vec3& hi) {
                return SegBox(la, ld, lo, hi);
            };
            VisitTree(tpl.nodes, tpl.order, local_reaches, test);
        }
    });

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

double OccluderScene::groundHeight(const OccluderPoint& base) const
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

    // 盒子里的每个面,放到世界里,记下朝不朝上
    struct Face
    {
        Vec3 v[3];
        bool up = false;
    };

    std::vector<Face> faces;
    const auto reaches = [&](const Vec3& node_lo, const Vec3& node_hi) {
        return BoxBox(lo, hi, node_lo, node_hi);
    };
    VisitTree(instance_nodes, instance_order, reaches, [&](uint32_t index) {
        const Instance& inst = instances[index];
        if (!BoxBox(lo, hi, inst.lo, inst.hi)) {
            return;
        }
        Mat3 a = inst.m;
        if (inst.invertible && !Inverse(inst.m, a)) {
            return;
        }
        // 镜像的摆放把绕序反过来,朝向跟着翻
        const double facing = Det(a) < 0.0 ? -1.0 : 1.0;
        const Template& tpl = templates[inst.tpl];
        const auto take = [&](uint32_t k) {
            Face face;
            for (size_t c = 0; c < 3; ++c) {
                face.v[c] = Add(Mul(a, tpl.verts[tpl.tris[k][c]]), inst.t);
            }
            face.up = facing * Cross(Sub(face.v[1], face.v[0]), Sub(face.v[2], face.v[0]))[1] < 0.0;
            faces.push_back(face);
        };
        if (!inst.invertible || tpl.nodes.empty()) {
            for (uint32_t k = 0; k < tpl.tris.size(); ++k) {
                take(k);
            }
            return;
        }
        // 盒子八个角换进模板局部系,取装得下它们的盒子去问模板的树
        Vec3 local_lo;
        Vec3 local_hi;
        local_lo.fill(std::numeric_limits<double>::infinity());
        local_hi.fill(-std::numeric_limits<double>::infinity());
        for (uint32_t corner = 0; corner < 8; ++corner) {
            Vec3 w;
            for (size_t j = 0; j < 3; ++j) {
                w[j] = ((corner >> j) & 1U) == 0 ? lo[j] : hi[j];
            }
            const Vec3 l = Mul(inst.m, Sub(w, inst.t));
            for (size_t j = 0; j < 3; ++j) {
                local_lo[j] = std::fmin(local_lo[j], l[j]);
                local_hi[j] = std::fmax(local_hi[j], l[j]);
            }
        }
        const auto local_reaches = [&](const Vec3& node_lo, const Vec3& node_hi) {
            return BoxBox(local_lo, local_hi, node_lo, node_hi);
        };
        VisitTree(tpl.nodes, tpl.order, local_reaches, take);
    });
    // 地形处处朝上;挖了洞的格没有面
    const size_t n = block_n;
    for (const Block& block : blocks) {
        if (!BoxBox(lo, hi, block.lo, block.hi)) {
            continue;
        }
        const double last = static_cast<double>(n - 2);
        const double u0 = std::fmax(std::floor((x0 - block.x0) / cell) - 1.0, 0.0);
        const double u1 = std::fmin(std::floor((x1 - block.x0) / cell) + 1.0, last);
        const double v0 = std::fmax(std::floor((z0 - block.z0) / cell) - 1.0, 0.0);
        const double v1 = std::fmin(std::floor((z1 - block.z0) / cell) + 1.0, last);
        if (!(u0 <= u1 && v0 <= v1)) {
            continue;
        }
        const auto sample = [&](size_t u, size_t v) {
            return Vec3 { block.x0 + static_cast<double>(u) * cell,
                          lattice_a + lattice_b * static_cast<double>(block.k[v * n + u]),
                          block.z0 + static_cast<double>(v) * cell };
        };
        for (size_t v = static_cast<size_t>(v0); v <= static_cast<size_t>(v1); ++v) {
            for (size_t u = static_cast<size_t>(u0); u <= static_cast<size_t>(u1); ++u) {
                const size_t c = v * (n - 1) + u;
                if (!block.holes.empty() && ((block.holes[c / 8] >> (c % 8)) & 1U) != 0) {
                    continue;
                }
                const Vec3 p00 = sample(u, v);
                const Vec3 p01 = sample(u, v + 1);
                const Vec3 p10 = sample(u + 1, v);
                const Vec3 p11 = sample(u + 1, v + 1);
                faces.push_back(Face { .v = { p00, p01, p10 }, .up = true });
                faces.push_back(Face { .v = { p10, p01, p11 }, .up = true });
            }
        }
    }

    // 每个面裁到占地格里,看它的高度范围:贴上薄板就是原地碰上了,否则薄板往下落时先碰到最高的那个朝上面
    bool touching = false;
    double landed = -std::numeric_limits<double>::infinity();
    for (const Face& face : faces) {
        std::array<Vec3, kClipMaxVertices> poly;
        const size_t count = ClipToRect(face.v, x0, x1, z0, z1, poly);
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
    // 原地碰上时改从占地四个角往下看,取最高的朝上面
    if (touching) {
        landed = -std::numeric_limits<double>::infinity();
        const Vec3 d { 0.0, -kSettleDrop, 0.0 };
        const double d2 = Dot(d, d);
        for (const double cx : { x0, x1 }) {
            for (const double cz : { z0, z1 }) {
                const Vec3 o { cx, start, cz };
                for (const Face& face : faces) {
                    double s = 0.0;
                    if (face.up && SegTri(o, d, d2, face.v[0], face.v[1], face.v[2], s)) {
                        landed = std::fmax(landed, start + d[1] * s);
                    }
                }
            }
        }
    }
    const double ground = std::isfinite(landed) ? landed : base.y;
    return std::fmin(std::fmax(ground, base.y - kSettleClamp), base.y + kSettleClamp);
}

std::shared_ptr<const OccluderScene> DecodeOccluderScene(const uint8_t* data, size_t size, std::string_view zone_name)
{
    if (size < kContainerHeaderSize || std::memcmp(data, kContainerTag, sizeof(kContainerTag)) != 0) {
        return nullptr;
    }
    const uint8_t* cursor = data + sizeof(kContainerTag);
    const uint8_t* const end = data + size;
    if (ReadU32(cursor) != kContainerVersion) {
        return nullptr;
    }
    const uint32_t count = ReadU32(cursor);
    const uint8_t* body = nullptr;
    size_t body_size = 0;
    for (uint32_t i = 0; i < count; ++i) {
        // 每条记录是 zone 名与本体,认场景按 zone 名,主包的仿射也是按它取的。
        if (static_cast<size_t>(end - cursor) < sizeof(uint16_t)) {
            return nullptr;
        }
        const uint16_t name_bytes = ReadU16(cursor);
        if (static_cast<size_t>(end - cursor) < name_bytes) {
            return nullptr;
        }
        const std::string_view name(reinterpret_cast<const char*>(cursor), name_bytes);
        cursor += name_bytes;
        if (static_cast<size_t>(end - cursor) < sizeof(uint32_t)) {
            return nullptr;
        }
        const uint32_t bytes = ReadU32(cursor);
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
    return DecodeSceneBody(body, body_size);
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

} // namespace navmesh
