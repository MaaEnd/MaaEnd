#pragma once

#include <algorithm>
#include <array>
#include <bit>
#include <cstdint>
#include <functional>
#include <optional>
#include <unordered_set>
#include <vector>

#include "BaseNavPack.h"
#include "NavmeshTypes.h"
#include "RecastNavGridIO.h"

namespace navmesh::recast
{

inline constexpr double kCS = 0.25;                // 体素边长 px
inline constexpr double kClimb = 3.0;              // 相邻格可连通最大高差 px
inline constexpr double kSlope = 1.0;              // 可攀爬坡度上限 tanθ, 抬升超过水平位移的这个倍数即立面
inline constexpr double kStepUp = 0.5;             // 可直接迈上的台阶高 px, 是角色属性所以不跟体素边长挂钩
inline constexpr double kBumpUp = 1.25;            // 跨过路面窄凸起/浅坑允许的抬升 px, 仅在落差不延伸时生效
inline constexpr int64_t kBumpCells = 3;           // 前探格数: 抬升在这么多格内回到出发高度即窄凸起
inline constexpr int64_t kDipCells = 2;            // 后探格数: 目标高度在身后这么多格内出现即浅坑
inline constexpr double kWallH = 1.25;             // 禁步边同时挡住拉直视线所需的落差 px, 取路面起伏上限
inline constexpr double kMergeH = kSlope * kCS;    // 同列 span 合并容差 px, 取一格坡面起伏
inline constexpr double kQH = 1.0;                 // 体素取样高差容差 px, 需装下斜面单格起伏与格心取样偏差
inline constexpr double kEdtCap = 12.0;            // 距离场截断 px
inline constexpr double kR = 1.75;                 // 期望余量上限 px
inline constexpr double kMaxErr = 0.5;             // 轮廓 DP 容差 px
inline constexpr double kMcHBand = 8.0;            // 层高度带(边界边筛/盖章)px
inline constexpr double kBkt = 4.0;                // 挡线索引桶边长 px
inline constexpr double kBktPad = 1e-6;            // 入桶时给挡线包围盒的放量 px, 需盖过相交判据放给挡线两端的余量
inline constexpr double kSnapRadius = 8.0;         // 起终点吸附半径 px
inline constexpr double kDeckBand = 2.0;           // 声明面高度匹配容差 px, 需远小于相邻面间距
inline constexpr double kBlockedPointRadius = 1.0; // 封堵点盖章半径 px
inline constexpr int64_t kHoleMaxCells = 32;       // 封闭小洞填充上限(格 = 2px²)
// 整类窗口在类范围外留的圈(格)。场都是局部算子, 依赖半径合起来不到这一圈, 留出它,
// 类边缘那几格算出来的场就与在整区图上算的逐位相同。旁包按同一个值烘。
inline constexpr int64_t kFieldHalo = 32;
// 单区格数上限。规划铺满整区, 这道闸只用来挡烘出来就不正常的区, 正常区离它很远。
inline constexpr int64_t kMaxCells = 400'000'000;

// VV(c) 的期望净空 c: 障碍按 c 膨胀后仍自由的地方走可见图, 膨胀后被吃掉的窄处走中脊。净空在
// 拓扑层是掩膜而不是价格 —— 按每格加价的写法会把中途钻的一小段窄缝摊进整条路长里平均掉,
// 那一小段再窄也只体现成一点点均价。取通行余量 kR: 这是烘焙腐蚀用的同一条角色半宽。
inline constexpr double kClrPref = kR; // 期望净空 px
// 中轴判据的毛刺尺度 px: 两侧最近障碍点隔不开这么远的起伏, 当边界噪声看待。上界由最窄那条
// 可通行缝定死 —— 净空 w 的缝两壁只隔开约 2w, 比这还大就提不出中轴, 通道在那里断掉, 线只能
// 绕整栋楼。两个格已经挡得住单格边界抖动, 再抬就开始吃掉实测里真走得通的窄走廊。
inline constexpr double kClrLambda = 2.0 * kCS;
// 定通道那一层单价按净空取值的区间, 单位 px。到 kClrPref 就封顶的话, 中轴上处处够宽单价一律
// 为一, 平行分支里最短的那条必然中标, 而窄缝总比宽道短 —— 于是线钻缝; 上限抬到这里, 绕去宽道
// 的那点长度才买得起。两端之比就是这层肯买的绕路倍数上限: 一条全程走窄处的腿, 会被长到这个
// 倍数的宽路顶掉, 取 4 时实测那条 5.25 倍外环买不动。窄到下限以下不再细分, 那是几何层的事。
inline constexpr double kClrWide = 4.0;
inline constexpr double kClrNarrow = 1.0;
// 弦的容许净空占所在走廊半宽的比例。弦贴着障碍角切过去时净空恰好等于这道阈值 —— 按走廊比例
// 定, 宽走廊就把线推离墙, 窄缝里自动松到弦仍走得通; 换成常数则收紧走锯齿、放开贴角切。
inline constexpr double kChordFrac = 0.8;
// 中脊上的突变抬升每次记一笔, 单位是格步价。连续缓坡不计, 只有必须迈上去的那种算。
inline constexpr double kStepTax = 6.0;
// 终线取直的拐角余量 px: 挡线按 kClrPref 加这一笔算, 弦贴角切过去时的净空即是这个和。
inline constexpr double kGeoMargin = 0.0;
// 拐点朝净空更高一侧能挪的最大位移 px。上界给到半格宽以内, 越界那些拐角就不是余量问题了。
inline constexpr double kLiftMax = 0.5;

// 相邻格允许的抬升 px, w 是水平位移(格)。坡面按坡度上限放行, 台阶按可迈高度放行, 两者取大。
// 只按坡度判会把可迈的台阶高绑死成体素边长, 于是路缘、地面微起伏这些连续地面全被切成立面。
inline double UpAllow(double w)
{
    return std::max(kStepUp, kSlope * w * kCS);
}

template <typename T>
struct Grid
{
    int64_t nx = 0;
    int64_t ny = 0;
    std::vector<T> v;

    Grid() = default;

    Grid(int64_t nx_in, int64_t ny_in, T fill)
        : nx(nx_in)
        , ny(ny_in)
        , v(static_cast<size_t>(nx_in * ny_in), fill)
    {
    }

    T at(int64_t y, int64_t x) const { return v[static_cast<size_t>(y * nx + x)]; }

    T& at(int64_t y, int64_t x) { return v[static_cast<size_t>(y * nx + x)]; }
};

using Mask = Grid<uint8_t>;

struct CellPt
{
    int64_t x = 0;
    int64_t y = 0;

    bool operator==(const CellPt&) const = default;
};

struct RasterCells
{
    std::vector<int64_t> cell;
    std::vector<float> h;
    std::vector<uint8_t> ins;
};

// 同一格的 span 在 sp_h 里是连续一段: 下标 [cstart(j), cstart(j)+ccnt(j))。逐格取这一段,
// 不必按全窗口最大叠层数逐槽扫 —— 那个上限取全窗最大值, 而绝大多数格只有一张面。
struct SpanTable
{
    std::vector<int32_t> sp_cell;
    std::vector<float> sp_h;
    // CSR 边界, 长度 = 占用格数 + 1, 末位是 span 总数。每格的起点、条数、所在格号都由它
    // 与 sp_cell 现推, 各存一份逐格数组是同一份信息的三个副本。
    std::vector<int32_t> cs;
    // 逐占用格一位: 这一列是不是被栅格化的立面。判据只看该格自己的叠层, 建表时算一次。
    std::vector<uint8_t> face;
    // 格号 → 占用格下标: 逐格一位的占用图加每 64 格的前缀占用数, 查一次是一位测试加一个
    // popcount。邻格查询在 BFS 与垂直可达判据里是最内层的一步, 建表时摊掉一次, 就不必每次
    // 二分; 比逐格 4 字节的直查表小 30 倍。长度只到最大占用格, 表外一律当空格。
    std::vector<uint64_t> occ_bits;
    std::vector<int32_t> occ_rank;

    int64_t nOcc() const { return cs.empty() ? 0 : static_cast<int64_t>(cs.size()) - 1; }

    int64_t cstart(int64_t ci) const { return cs[static_cast<size_t>(ci)]; }

    int64_t ccnt(int64_t ci) const { return cs[static_cast<size_t>(ci) + 1] - cs[static_cast<size_t>(ci)]; }

    int64_t occ(int64_t ci) const { return sp_cell[static_cast<size_t>(cs[static_cast<size_t>(ci)])]; }

    int64_t j(int64_t cid) const
    {
        const auto w = static_cast<size_t>(cid >> 6);
        if (cid < 0 || w >= occ_bits.size()) {
            return -1;
        }
        const uint64_t bits = occ_bits[w];
        const int b = static_cast<int>(cid & 63);
        if (((bits >> b) & 1U) == 0) {
            return -1;
        }
        return occ_rank[w] + std::popcount(bits & ((uint64_t { 1 } << b) - 1));
    }
};

// 相邻格垂直可达判据: cid 格上高 h0 的 span 能否迈到 (dx,dy) 邻格上高 h1 的 span。
bool RiseOk(const SpanTable& st, int64_t nx, int64_t ny, int64_t cid, int64_t dx, int64_t dy, float h0, float h1);

// walkable 非空时按三角下标逐个过滤:标 0 的不体素化。掩码须与 T 同长同序;
// 调用方自己压缩过的子集三角表留 nullptr。
RasterCells Rasterize(
    const BaseNavVertex* V,
    const std::vector<std::array<int32_t, 3>>& T,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny,
    const std::vector<uint8_t>* walkable = nullptr);

void AppendSeamBridge(RasterCells& rc, int64_t nx, int64_t ny);

SpanTable BuildSpans(const std::vector<int64_t>& cell, const std::vector<float>& h);

// flags / aux 是与 cell 同长的随行列, 按同一置换重排后写回原处。
SpanTable PackSpans(
    std::vector<int32_t> cell, std::vector<float> h, std::vector<uint8_t>* flags = nullptr, std::vector<uint32_t>* aux = nullptr);

std::vector<uint8_t> Flood(int64_t seed, const SpanTable& st, int64_t nx);

// Flood 的无种子版本:逐 span 给出所在的连通类号。邻接关系与 Flood 逐条相同,
// 所以 Flood(seed) 命中的正是种子那一类,类号可以离线算好。
std::vector<int32_t> LabelRegions(const SpanTable& st, int64_t nx);

std::vector<uint8_t> SpanReach(int64_t seed, const SpanTable& st, const std::vector<uint8_t>& ok, int64_t nx, int64_t ny);

Grid<float> Clearance(const Mask& mask);

std::vector<uint8_t> StampWalls(
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    const std::vector<double>& hh,
    double ox,
    double oy,
    int64_t nx,
    int64_t ny,
    const SpanTable& st);

// 逐墙一位: 沿墙采样, 任一样本落在层高图 lh 的带内即留下。落在 lh 外的采样跳过。
// 运行期不再调它(留墙已烘进旁包), 留作烘焙口径的参考实现与旁包校验。
std::vector<uint8_t> WallsAtLayer(
    const std::vector<WorldPoint>& p0,
    const std::vector<WorldPoint>& p1,
    const std::vector<double>& hh,
    const Grid<float>& lh,
    double ox,
    double oy);

// 逐格一位: 这一格里落过边界边的采样点。距离场对跨边界无感, 用它把边所在的格从自由集里扣掉。
Mask WallHits(const std::vector<WorldPoint>& p0, const std::vector<WorldPoint>& p1, double ox, double oy, int64_t nx, int64_t ny);

// (dy+1)*3+(dx+1) → 位号 |(位存在终点格 ? 8 : 0), -1 表示这个位移不是八邻。
// 表由 kGridStepDx/Dy 现推, 于是与包里 stepbits 的位序同一份定义。
inline constexpr std::array<int8_t, 9> MakeEdgeSlots()
{
    std::array<int8_t, 9> t {};
    t.fill(-1);
    for (int i = 0; i < 4; ++i) {
        const int dx = static_cast<int>(kGridStepDx[i]);
        const int dy = static_cast<int>(kGridStepDy[i]);
        t[static_cast<size_t>((dy + 1) * 3 + dx + 1)] = static_cast<int8_t>(2 * i);
        t[static_cast<size_t>((1 - dy) * 3 + 1 - dx)] = static_cast<int8_t>(2 * i + 1 + 8);
    }
    return t;
}

inline constexpr std::array<int8_t, 9> kEdgeSlot = MakeEdgeSlots();

// 禁行边集。边只连八邻 ⇒ 一格一字节, 四个规范方向各占两位(正向、反向)即可存下全部边。
// 非八邻的格对没有位可存, locate 一律返回 -1: 这既是越界保护, 也正是散列集查不到那个键
// 时给的答案, 两种实现因此逐位同答。
struct EdgeBits
{
    std::vector<uint8_t> v;
    int64_t nx = 0;
    int64_t ny = 0;
    bool any = false;

    void resize(int64_t w, int64_t h)
    {
        nx = w;
        ny = h;
        v.assign(static_cast<size_t>(w * h), 0);
        any = false;
    }

    // 返回 (存位的格 << 3) | 位号
    int64_t locate(int64_t a, int64_t b) const
    {
        const int64_t dx = b % nx - a % nx;
        const int64_t dy = b / nx - a / nx;
        if (dx < -1 || dx > 1 || dy < -1 || dy > 1) {
            return -1;
        }
        const int8_t s = kEdgeSlot[static_cast<size_t>((dy + 1) * 3 + dx + 1)];
        if (s < 0) {
            return -1;
        }
        return (((s & 8) != 0 ? b : a) << 3) | (s & 7);
    }

    void set(int64_t a, int64_t b)
    {
        const int64_t loc = locate(a, b);
        if (loc < 0) {
            return;
        }
        v[static_cast<size_t>(loc >> 3)] |= static_cast<uint8_t>(1U << (loc & 7));
        any = true;
    }

    bool has(int64_t a, int64_t b) const
    {
        const int64_t loc = locate(a, b);
        return loc >= 0 && (v[static_cast<size_t>(loc >> 3)] & (1U << (loc & 7))) != 0;
    }

    bool empty() const { return !any; }
};

struct StepBarrier
{
    EdgeBits steps;
    std::vector<WorldPoint> p0;
    std::vector<WorldPoint> p1;
};

std::vector<int64_t> Comps4(const Mask& mask);

Mask FillHoles(const Mask& mask, int64_t max_cells, const Mask* protect);

Mask CloseCracks(const Mask& core, const Mask& lay, const Mask* protect);

// 逐格单价: 净空夹进 [lo, hi] 后按亏欠比例上浮, 最宽处恒为一; dist 为空表示处处一价。
// 它是净空场的逐元素函数, 每次取值现算 —— 单独铺一张全窗口的 float 表, 存的是同一份
// 数据的第二个副本。
struct PriceField
{
    const Grid<float>* dist = nullptr;
    double lo = 0.0;
    double hi = 0.0;

    float v(size_t i) const
    {
        if (dist == nullptr) {
            return 1.0F;
        }
        return static_cast<float>(hi / std::min(std::max(static_cast<double>(dist->v[i]), lo), hi));
    }

    float at(int64_t y, int64_t x) const
    {
        return dist == nullptr ? 1.0F : v(static_cast<size_t>(y * dist->nx + x));
    }
};

std::optional<std::vector<CellPt>> CostAstar(
    const Mask& mask,
    CellPt s,
    CellPt g,
    const PriceField& mult,
    const EdgeBits* banned,
    const double* bnp,
    const EdgeBits* forbidden = nullptr,
    double* out_cost = nullptr);

class Visibility;

// vis 非空则按 Lazy Theta* 展开: 松弛先把父指针接到祖父, 弹出时才验一次视线, 验不过退回格步。
// 于是路径由父链上的直线段构成, 紧绷这件事在搜索里完成, 不再靠事后拉直。
// 返回值恒为逐格路径, 拓扑判据按格读。corners 非空则另交出父链本身, 那才是几何要走的折线。
// out_cost 非空则交出终点的累计代价; 单价恒 ≥1, 它就是路径格长的上界, 小窗验收拿它判搜索有没有碰边。
std::optional<std::vector<int64_t>> SpanAstar(
    const SpanTable& st,
    const std::vector<uint8_t>& ok,
    const Mask& ok2,
    int64_t s,
    const std::vector<int64_t>& gset,
    const PriceField& mult,
    const EdgeBits* banned,
    const double* bnp,
    const EdgeBits* forbidden = nullptr,
    const Visibility* vis = nullptr,
    std::vector<int64_t>* corners = nullptr,
    double* out_cost = nullptr);

Mask MedialAxis(const Grid<float>& dist, double lam);

Grid<float> CorridorWidth(const Grid<float>& dist, const Mask& seed, Mask* band = nullptr);

std::vector<std::vector<WorldPoint>> TraceContours(const Mask& mask);

std::vector<WorldPoint> SimplifyLoop(const std::vector<WorldPoint>& P, double max_err);

// 挡线段表与它的桶索引。一条腿里几个视图的段几何逐字相同、只有 on 掩膜不同, 段表因此拆出来
// 共享: 世代戳每次查询开头先自增再比较, 多个视图共用一套仍只在单次查询内去重。
class BlockerSegments
{
public:
    BlockerSegments() = default;
    BlockerSegments(
        const std::vector<std::vector<WorldPoint>>& loops,
        const std::vector<WorldPoint>* extra_a,
        const std::vector<WorldPoint>* extra_b);

private:
    friend class Blockers;

    void buildIndex() const;
    // 段包围盒是两端点的逐分量 min/max, 存成表只是把同一个数再摊一份内存。
    WorldPoint lo(size_t i) const { return { std::min(a_[i].x, b_[i].x), std::min(a_[i].y, b_[i].y) }; }
    WorldPoint hi(size_t i) const { return { std::max(a_[i].x, b_[i].x), std::max(a_[i].y, b_[i].y) }; }

    std::vector<WorldPoint> a_;
    std::vector<WorldPoint> b_;
    // 挡线按均匀桶建 CSR 索引。弦与某段相交必然共有一点: 那点在段的包围盒里 ⇒ 段登记在它所在
    // 的桶, 那点又在弦上 ⇒ 弦的取样覆盖到它所在的桶, 于是只测弦扫过的桶与全表扫描同答。
    // 索引首次查询时才建: 一条腿里要造好几个视图, 其中几个一次都不查。
    mutable bool built_ = false;
    mutable double bx0_ = 0.0;
    mutable double by0_ = 0.0;
    mutable int64_t bnx_ = 0;
    mutable int64_t bny_ = 0;
    mutable std::vector<int32_t> bstart_;
    mutable std::vector<int32_t> bitem_;
    // 同一段会落进多个桶、同一个桶又会被相邻取样点重复扫到, 段与桶各挂一组世代戳去重,
    // 逐腿不清零。相交测试就地做, 命中即返回: 返回值是候选集上的或, 短路不改变它。
    mutable std::vector<uint32_t> seen_;
    mutable std::vector<uint32_t> bseen_;
    mutable uint32_t epoch_ = 0;
};

// on 掩膜密采样兜底:精确 45° 弦会从轮廓顶点缝溜走
class Blockers
{
public:
    struct OnMask
    {
        const Mask* mask = nullptr;
        double x0 = 0.0;
        double y0 = 0.0;
        double cs = kCS;
    };

    Blockers(const BlockerSegments& segs, std::optional<OnMask> on)
        : segs_(&segs)
        , on_(on)
    {
    }

    bool blocked(const WorldPoint& p, const WorldPoint& q) const;

private:
    bool offMask(const WorldPoint& p, const WorldPoint& q) const;

    const BlockerSegments* segs_ = nullptr;
    std::optional<OnMask> on_;
};

// 走查只用来跟住弦所在的层, 立面本身由挡线集与拓扑禁步管住, 故抬升按体素取样
// 容差放宽: 高度取自格心, 斜面与接缝上相邻格的取样差本就能超出一步抬升上限,
// 照拓扑口径卡会把大量直弦判死, 拉直退化成网格锯齿。
class LayerOracle
{
public:
    LayerOracle(const SpanTable* st, int64_t nx, int64_t ny, double x0, double y0)
        : st_(st)
        , nx_(nx)
        , ny_(ny)
        , x0_(x0)
        , y0_(y0)
    {
    }

    // h 取起点高度或一组可达高度
    std::optional<std::vector<float>> walk(const std::vector<WorldPoint>& pts, float h) const;

    std::optional<std::vector<float>> walk(const std::vector<WorldPoint>& pts, const std::vector<float>& h) const;

    bool ok(const WorldPoint& p, const WorldPoint& q, float h, float hq) const;

private:
    const SpanTable* st_ = nullptr;
    int64_t nx_ = 0;
    int64_t ny_ = 0;
    double x0_ = 0.0;
    double y0_ = 0.0;
};

// 搜索与终线共用的唯一视线判据。四件事一次答完: 通道掩膜、立面禁步、计税台阶、层走查。
// 禁步与台阶按边集逐格查, 与拓扑层同一份口径; 弦跨过台阶就白拿了那笔税, 因此一律当挡, 台阶
// 处自然退化成格步。墙不必单列: 弦被限在净空 ≥ c 的实心区内, 够不着墙。
class Visibility
{
public:
    Visibility(
        const Blockers* blk,
        const LayerOracle* lyo,
        const EdgeBits* faces,
        const EdgeBits* steps,
        int64_t nx,
        int64_t ny,
        double x0,
        double y0)
        : blk_(blk)
        , lyo_(lyo)
        , faces_(faces)
        , steps_(steps)
        , nx_(nx)
        , ny_(ny)
        , x0_(x0)
        , y0_(y0)
    {
    }

    bool ok(const WorldPoint& p, const WorldPoint& q, float hp, float hq) const;

    // 格心 + 半格 = 世界坐标, 与拉直取点同一口径
    WorldPoint at(int64_t cell) const
    {
        return { x0_ + (static_cast<double>(cell % nx_) + 0.5) * kCS, y0_ + (static_cast<double>(cell / nx_) + 0.5) * kCS };
    }

private:
    bool crossesStep(const WorldPoint& p, const WorldPoint& q) const;

    const Blockers* blk_ = nullptr;
    const LayerOracle* lyo_ = nullptr;
    const EdgeBits* faces_ = nullptr;
    const EdgeBits* steps_ = nullptr;
    int64_t nx_ = 0;
    int64_t ny_ = 0;
    double x0_ = 0.0;
    double y0_ = 0.0;
};

// vis 非空则整条弦只问它一句 —— 与搜索同一个判据, 拉直因此不会跨过搜索认为看不见的地方。
std::vector<WorldPoint> StringPull(
    const std::vector<WorldPoint>& pts,
    const Blockers& blk,
    const LayerOracle* lyo = nullptr,
    const std::vector<float>* hs = nullptr,
    const Visibility* vis = nullptr);

}
