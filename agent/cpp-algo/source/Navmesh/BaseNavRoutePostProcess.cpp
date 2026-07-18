#include <algorithm>
#include <cmath>
#include <numbers>
#include <optional>
#include <unordered_set>
#include <utility>

#include "BaseNavGeometry.h"
#include "BaseNavRoutePostProcess.h"

namespace navmesh::detail
{

namespace
{

constexpr double kDedupePointEpsilon = 0.25;
constexpr double kCollinearEpsilon = 1e-3;
constexpr double kRouteMaxPointDistance = 4.0;
constexpr double kRouteCenterProbeLimit = 32.0;           // 居中时单侧探测墙距的上限(px)
constexpr double kRouteCenterMaxShift = 24.0;             // 直段整体横移上限(px)
constexpr double kCenterProbeStep = 0.5;                  // 墙距探测步进(px)
constexpr double kCenterValidateStep = 0.5;               // 连段校验采样步进(px)
constexpr int kRoutePullMaxSkip = 8;                      // 拉直可越过的最大连续不可达点数,用于跨越非单调视线遮挡
constexpr double kRoutePullMaxReach = 64.0;               // 单条捷径的最大长度(px)
constexpr double kRouteCornerAngleDeg = 35.0;             // 转角达此值即视为结构性拐角(px),在此切分直段
constexpr double kRouteRunStraightTol = 1.6;              // 直段判据:内部点偏离首尾弦的上限(px)
constexpr double kRouteCornerMoveFactor = 1.5;            // 拐角重连位移上限 = kRouteCenterMaxShift × 此系数
constexpr double kRouteShortcutMinClearance = 6.0;        // 拉直捷径沿途任一侧至少保留的横向余量(px)
constexpr double kRouteShortcutClearanceProbeLimit = 8.0; // 捷径余量探测上限(px)
constexpr double kRouteShortcutClearanceProbeStep = 1.0;  // 捷径余量探测步进(px)
constexpr double kRouteShortcutClearanceSampleStep = 4.0; // 捷径沿线采样步进(px)
constexpr double kRouteDecenterHugClearance = 4.0;        // 单侧余量 < 此值即视为"贴边"(px)
constexpr double kRouteDecenterHugAsymmetry = 2.0;        // 且两侧余量差 >= 此值,才有"可向开阔侧让开"的空间(px)
constexpr double kRouteDecenterWaterDrop = 1.5;           // 紧边外侧地面低于脚下此值(或离开网格)即判为水/坎(px)
constexpr double kRouteRelaxTurnCap = 88.0;               // 松弛允许的转角上限(度);仍 <= 原转角者放行
constexpr double kRouteRelaxMidpointWeight = 0.50;        // 松弛目标 = 此权重·邻点中点 + (1-此)·余量中心。
constexpr double kRouteRelaxMaxTranslate = 12.0;          // 单点相对原位的最大位移(px)
constexpr int kRouteRelaxIterations = 16; // Gauss-Seidel 松弛迭代次数。cap 使单步居中变温和,靠多迭代逐步收敛到中线
constexpr bool kRouteRelaxBiasNearCap = true; // 余量偏置鲁棒:bias 钳在 min(左,右余量) 内 —— 走廊渐进居中
constexpr double kRouteWaterShiftSafe = 4.0;  // 贴水块整体平移力争达到的单侧安全余量(px)
constexpr double kRouteWaterShiftMax = 14.0;  // 贴水块整体平移上限(px)
// 居中细化三:抗噪裕度地板。funnel/拉直求最短会贴 navmesh 内角,前两道对 pinned 拐角/凸角仍留贴边点
// (各向同性最近边界 ~0)。上游图像定位有噪,贴可走面边界=出界风险。把"最近边界 < 地板"的点(含 pinned)
// 沿各向探测的开阔向量(中轴梯度)推离边界到地板;凸角最近边界在斜对角,故必须各向探测、垂直 L/R 看不到。
constexpr bool kRouteFloorEnable = true;
constexpr double kRouteFloorMinClearance = 2.0; // 目标最小离边余量(px):最近边界 < 此的点向中轴推离边界
constexpr double kRouteFloorStep = 0.6;         // 单轮沿中轴最大推进(px):小步多迭代,防过冲到对侧反而变差
constexpr double kRouteFloorMaxTranslate = 4.0; // 单点相对原位的累计最大位移(px)
constexpr int kRouteFloorIterations = 14;       // 中轴梯度上升迭代上限(收敛即停;已达标点设 settled 永久跳过)
constexpr int kRouteFloorProbeDirs = 8;         // 各向探测方向数(8 足够辨最近边界方向)
constexpr double kRouteFloorProbeMargin = 1.0;  // 墙距探测早停余量:只探到 地板+此 即够判达标(px)
// 断崖抗掉落(收尾)。真实断崖(高度不连续,踩上去掉落)与无害接缝(高度连续,可走)在点包含式墙距里都是"边界",
// 前面几道居中一视同仁、且小步/刚性块移;遇窄口(一侧断崖、开阔侧也不宽)贴断崖点卡在原地——小步一迈就跨崖出界被
// 连段守卫拒掉,永远推不动(离崖~0)。这里只针对贴真实断崖的点,允许大步跨到开阔侧,取离崖最远的合法候选。
constexpr bool kRouteGapRepelEnable = true;
constexpr double kRouteGapRepelTrigger = 1.5;      // 仅处理点距真实断崖 < 此的点(px);开阔/贴接缝者不动
constexpr double kRouteGapRepelSafe = 2.0;         // 推离断崖力争达到的距离(px);达到即停,窄口够不到则尽力
constexpr double kRouteGapRepelMaxTranslate = 4.0; // 单点相对原位的最大跳离位移(px)
constexpr double kRouteGapRepelStep = 0.3;       // 跳离步进(px):允许大步跨到开阔侧,不像地板小步会被断崖对岸卡住
constexpr int kRouteGapRepelProbeDirs = 16;      // 各向探测方向数(断崖方向任意,须够密以免漏判)
constexpr double kRouteGapRepelProbeStep = 0.15; // 离崖距探测步进(px):须细于居中默认,否则窄口 0.1->0.4 的改善看不见

double MaxOffsetOnMesh(const WorldPoint& origin, double dir_x, double dir_y, double cap, const PointOnMeshFn& point_on_mesh, double step);

std::vector<size_t> SortedUniqueBreaks(std::vector<size_t> breaks)
{
    std::sort(breaks.begin(), breaks.end());
    breaks.erase(std::unique(breaks.begin(), breaks.end()), breaks.end());
    return breaks;
}

RoutePointsWithBreaks DedupePointsWithBreaks(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks)
{
    RoutePointsWithBreaks result;
    const std::unordered_set<size_t> break_set(segment_breaks.begin(), segment_breaks.end());
    for (size_t index = 0; index < points.size(); ++index) {
        if (!result.points.empty() && Distance(points[index], result.points.back()) <= kDedupePointEpsilon) {
            if (break_set.contains(index)) {
                result.segment_breaks.push_back(result.points.size());
            }
            continue;
        }
        if (break_set.contains(index)) {
            result.segment_breaks.push_back(result.points.size());
        }
        result.points.push_back(points[index]);
    }
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

RoutePointsWithBreaks RemoveCollinearWithBreaks(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks)
{
    if (points.size() <= 2) {
        return RoutePointsWithBreaks { .points = points, .segment_breaks = segment_breaks };
    }

    RoutePointsWithBreaks result;
    result.points.push_back(points.front());
    const std::unordered_set<size_t> break_set(segment_breaks.begin(), segment_breaks.end());
    for (size_t index = 1; index + 1 < points.size(); ++index) {
        if (break_set.contains(index)) {
            result.segment_breaks.push_back(result.points.size());
            result.points.push_back(points[index]);
            continue;
        }
        const WorldPoint& a = result.points.back();
        const WorldPoint& b = points[index];
        const WorldPoint& c = points[index + 1];
        const double area = std::abs((b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x));
        const double length = Distance(a, c);
        if (length > kCollinearEpsilon && area / length <= kCollinearEpsilon) {
            continue;
        }
        result.points.push_back(points[index]);
    }
    result.points.push_back(points.back());
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

// 捷径横向余量校验:沿 a→b 等距采样,每个采样点须在网格内,且左右法向各保留 >= kRouteShortcutMinClearance 的余量。
bool SegmentShortcutHasClearance(const WorldPoint& a, const WorldPoint& b, const PointOnMeshFn& point_on_mesh)
{
    const double dx = b.x - a.x;
    const double dy = b.y - a.y;
    const double length = std::sqrt(dx * dx + dy * dy);
    if (length < 1e-6 || !point_on_mesh) {
        return true;
    }

    const double normal_x = -dy / length;
    const double normal_y = dx / length;
    const int sample_count = std::max(1, static_cast<int>(std::ceil(length / kRouteShortcutClearanceSampleStep)));
    for (int index = 1; index <= sample_count; ++index) {
        const double t = static_cast<double>(index) / static_cast<double>(sample_count + 1);
        const WorldPoint sample { .x = a.x + dx * t, .y = a.y + dy * t };
        if (!point_on_mesh(sample)) {
            return false;
        }
        const double left =
            MaxOffsetOnMesh(sample, normal_x, normal_y, kRouteShortcutClearanceProbeLimit, point_on_mesh, kRouteShortcutClearanceProbeStep);
        const double right = MaxOffsetOnMesh(
            sample,
            -normal_x,
            -normal_y,
            kRouteShortcutClearanceProbeLimit,
            point_on_mesh,
            kRouteShortcutClearanceProbeStep);
        if (std::min(left, right) < kRouteShortcutMinClearance) {
            return false;
        }
    }
    return true;
}

// 贪心 LOS 拉直:从锚点跳至最远可直达点,仅在结构性拐角处保留落点。可行性以高度连续性为判据——
// 共面锯齿被拉直至走廊中线,绕墙拐角因踩墙产生高度跳变而保留为直角。视线在重叠网格上可能非单调,
// 故允许越过至多 kRoutePullMaxSkip 个不可达点。取代旧 RDP+march 方案(几何抽稀留锯齿、march 误拒直捷径)。
// 额外用 SegmentShortcutHasClearance 拦下贴边切线:中线在网格上但侧向余量不足的捷径不予拉直,保留原拐角。
std::vector<size_t> ThinContinuousSegment(
    const std::vector<WorldPoint>& points,
    size_t start,
    size_t end,
    const SegmentWalkableFn& is_segment_walkable,
    const PointOnMeshFn& point_on_mesh)
{
    if (end - start <= 2 || !is_segment_walkable) {
        std::vector<size_t> result;
        for (size_t index = start; index < end; ++index) {
            result.push_back(index);
        }
        return result;
    }

    std::vector<size_t> kept { start };
    size_t anchor = start;
    while (anchor < end - 1) {
        size_t farthest = anchor + 1; // 至少前进一步:相邻原始边必定可走
        int misses = 0;
        size_t probe = anchor + 2;
        while (probe < end && misses <= kRoutePullMaxSkip) {
            // 捷径长度封顶(性能上界);超长直路被切为共线小段,居中时仍作一条直段处理,形状不变。
            if (Distance(points[anchor], points[probe]) > kRoutePullMaxReach) {
                break;
            }
            // 中线可走还不够:侧向余量不足的捷径(贴 L 形拐角内侧水边切线)必须拒绝,保留原拐角。
            const bool has_clearance = !point_on_mesh || SegmentShortcutHasClearance(points[anchor], points[probe], point_on_mesh);
            if (is_segment_walkable(points[anchor], points[probe]) && has_clearance) {
                farthest = probe;
                misses = 0;
            }
            else {
                ++misses;
            }
            ++probe;
        }
        if (farthest < end - 1) {
            kept.push_back(farthest);
        }
        anchor = farthest;
    }
    kept.push_back(end - 1);
    return kept;
}

RoutePointsWithBreaks ThinRoutePointsWithBreaks(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const SegmentWalkableFn& is_segment_walkable,
    const PointOnMeshFn& point_on_mesh)
{
    if (points.size() <= 2) {
        return RoutePointsWithBreaks { .points = points, .segment_breaks = segment_breaks };
    }

    std::vector<size_t> valid_breaks;
    for (size_t break_index : segment_breaks) {
        if (break_index > 0 && break_index < points.size()) {
            valid_breaks.push_back(break_index);
        }
    }
    valid_breaks = SortedUniqueBreaks(std::move(valid_breaks));

    std::vector<size_t> segment_starts { 0 };
    segment_starts.insert(segment_starts.end(), valid_breaks.begin(), valid_breaks.end());
    std::vector<size_t> segment_ends(valid_breaks.begin(), valid_breaks.end());
    segment_ends.push_back(points.size());

    RoutePointsWithBreaks result;
    for (size_t segment_index = 0; segment_index < segment_starts.size(); ++segment_index) {
        if (segment_index > 0) {
            result.segment_breaks.push_back(result.points.size());
        }
        const std::vector<size_t> kept_indices =
            ThinContinuousSegment(points, segment_starts[segment_index], segment_ends[segment_index], is_segment_walkable, point_on_mesh);
        for (size_t index : kept_indices) {
            result.points.push_back(points[index]);
        }
    }
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

std::vector<WorldPoint> DensifyContinuousSegment(const std::vector<WorldPoint>& points, size_t start, size_t end, double max_distance)
{
    if (start >= end) {
        return {};
    }
    const double safe_max_distance = std::max(max_distance, 0.25);
    std::vector<WorldPoint> result { points[start] };
    for (size_t index = start + 1; index < end; ++index) {
        const WorldPoint from_point = points[index - 1];
        const WorldPoint to_point = points[index];
        const double distance = Distance(from_point, to_point);
        if (distance <= 1e-6) {
            continue;
        }
        const int step_count = std::max(1, static_cast<int>(std::ceil(distance / safe_max_distance)));
        for (int step = 1; step < step_count; ++step) {
            const double t = static_cast<double>(step) / static_cast<double>(step_count);
            result.push_back(WorldPoint {
                .x = from_point.x + (to_point.x - from_point.x) * t,
                .y = from_point.y + (to_point.y - from_point.y) * t,
            });
        }
        result.push_back(to_point);
    }
    return result;
}

RoutePointsWithBreaks DensifyRoutePointsWithBreaks(const std::vector<WorldPoint>& points, const std::vector<size_t>& segment_breaks)
{
    if (points.size() <= 1) {
        return RoutePointsWithBreaks { .points = points, .segment_breaks = segment_breaks };
    }

    std::vector<size_t> valid_breaks;
    for (size_t break_index : segment_breaks) {
        if (break_index > 0 && break_index < points.size()) {
            valid_breaks.push_back(break_index);
        }
    }
    valid_breaks = SortedUniqueBreaks(std::move(valid_breaks));

    std::vector<size_t> segment_starts { 0 };
    segment_starts.insert(segment_starts.end(), valid_breaks.begin(), valid_breaks.end());
    std::vector<size_t> segment_ends(valid_breaks.begin(), valid_breaks.end());
    segment_ends.push_back(points.size());

    RoutePointsWithBreaks result;
    for (size_t segment_index = 0; segment_index < segment_starts.size(); ++segment_index) {
        if (segment_index > 0) {
            result.segment_breaks.push_back(result.points.size());
        }
        std::vector<WorldPoint> segment =
            DensifyContinuousSegment(points, segment_starts[segment_index], segment_ends[segment_index], kRouteMaxPointDistance);
        result.points.insert(result.points.end(), segment.begin(), segment.end());
    }
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

// 点包含式墙距:沿 (dir_x, dir_y) 外推,返回仍在网格内的最大偏移 ∈[0, cap]。不依赖三角邻接,
// 避开 march 在重叠网格上对横向余量的低估。
double MaxOffsetOnMesh(
    const WorldPoint& origin,
    double dir_x,
    double dir_y,
    double cap,
    const PointOnMeshFn& point_on_mesh,
    double step = kCenterProbeStep)
{
    double distance = step;
    double last = 0.0;
    while (distance <= cap) {
        if (!point_on_mesh(WorldPoint { .x = origin.x + dir_x * distance, .y = origin.y + dir_y * distance })) {
            return last;
        }
        last = distance;
        distance += step;
    }
    return cap;
}

// 点包含式连段校验:沿 a→b 采样,全部采样点在网格内方判可走(亚像素步长不会跨越真实墙体)。
bool SegmentOnMesh(const WorldPoint& a, const WorldPoint& b, const PointOnMeshFn& point_on_mesh, double step = kCenterValidateStep)
{
    const double length = std::sqrt((b.x - a.x) * (b.x - a.x) + (b.y - a.y) * (b.y - a.y));
    const int sample_count = std::max(1, static_cast<int>(length / step));
    for (int index = 0; index <= sample_count; ++index) {
        const double t = static_cast<double>(index) / sample_count;
        if (!point_on_mesh(WorldPoint { .x = a.x + (b.x - a.x) * t, .y = a.y + (b.y - a.y) * t })) {
            return false;
        }
    }
    return true;
}

// a→b→c 在 b 处的转角(度):0 为直行,90 为直角,180 为掉头。
double RouteTurnAngleDeg(const WorldPoint& a, const WorldPoint& b, const WorldPoint& c)
{
    const double ux = b.x - a.x;
    const double uy = b.y - a.y;
    const double vx = c.x - b.x;
    const double vy = c.y - b.y;
    const double length_u = std::sqrt(ux * ux + uy * uy);
    const double length_v = std::sqrt(vx * vx + vy * vy);
    if (length_u < 1e-9 || length_v < 1e-9) {
        return 0.0;
    }
    const double cos_value = std::clamp((ux * vx + uy * vy) / (length_u * length_v), -1.0, 1.0);
    return std::acos(cos_value) * 180.0 / std::numbers::pi;
}

// 单点折返判据:入边·出边点积 < 0(夹角 > 90°)。退化边(零长)不计。逐 run 局部居中用它判断
// “此次平移是否在窗口内新增折返”。
bool IsTurnBackAt(const std::vector<WorldPoint>& points, size_t index)
{
    if (index == 0 || index + 1 >= points.size()) {
        return false;
    }
    const double ax = points[index].x - points[index - 1].x;
    const double ay = points[index].y - points[index - 1].y;
    const double bx = points[index + 1].x - points[index].x;
    const double by = points[index + 1].y - points[index].y;
    if (ax * ax + ay * ay < 1e-9 || bx * bx + by * by < 1e-9) {
        return false;
    }
    return ax * bx + ay * by < 0;
}

// point 到直线 line_a→line_b 的垂距;直线退化时取到 line_a 的距离。
double PerpendicularDistance(const WorldPoint& point, const WorldPoint& line_a, const WorldPoint& line_b)
{
    const double dx = line_b.x - line_a.x;
    const double dy = line_b.y - line_a.y;
    const double length = std::sqrt(dx * dx + dy * dy);
    if (length < 1e-9) {
        return Distance(point, line_a);
    }
    return std::abs((point.x - line_a.x) * (-dy) + (point.y - line_a.y) * dx) / length;
}

// point 在直线 (base + t·direction) 上的垂足(direction 为单位向量)。
WorldPoint PerpendicularFoot(const WorldPoint& point, const WorldPoint& base, const WorldPoint& direction)
{
    const double t = (point.x - base.x) * direction.x + (point.y - base.y) * direction.y;
    return WorldPoint { .x = base.x + direction.x * t, .y = base.y + direction.y * t };
}

// 直段(run):相邻两个结构性拐角之间的子段。仅当 run 足够直时,才持有用于重连拐角的直线(has_line)。
struct CenterRun
{
    size_t start = 0;
    size_t end = 0;
    bool has_line = false;
    double shift = 0.0;
    WorldPoint direction { .x = 0.0, .y = 0.0 };
    WorldPoint normal { .x = 0.0, .y = 0.0 };
    WorldPoint base { .x = 0.0, .y = 0.0 };
};

// 结构保持式居中(不平滑、不圆角化直角):在拐角处切分直段,将直段沿法向刚性平移至走廊中线——直段仍直、
// 直角精确保留。墙距用点包含判据(避免 march 在重叠网格上低估余量)。
// 提交策略:逐 run 局部事务,取代旧的“整段全有或全无”安全闸。每条直 run 独立平移自身内部点、并把自身两个
// 拐角投影到平移后的直线上,仅当其局部窗口 [run_start-1 .. run_end+1] 仍全程在网格内、且不新增折返时才提交;
// 任一 run 失败只丢弃它自己,不连累其余。贴住固定端点(整段首尾 = S/G 或桥接点)的 run 不平移——平移它必在
// 固定端拽出绕行钩。旧闸在碎片化路线(结构性拐角多 → run 短而互锁)上几乎总是整段回退 = 居中静默失效,正是
// 窄走廊/水边贴边不被纠正的根因。
std::vector<WorldPoint> CenterContinuousSegment(
    const std::vector<WorldPoint>& points,
    size_t start,
    size_t end,
    double probe_limit,
    double max_shift,
    const PointOnMeshFn& point_on_mesh)
{
    std::vector<WorldPoint> original(
        points.begin() + static_cast<std::ptrdiff_t>(start),
        points.begin() + static_cast<std::ptrdiff_t>(end));
    const size_t point_count = original.size();
    if (point_count < 3 || !point_on_mesh) {
        return original; // 段端点固定,至少需前/中/后三点方能居中
    }

    // 结构性拐角(两端点恒为拐角);下标天然严格递增,与 Python sorted(set(...)) 对齐。
    std::vector<size_t> corners { 0 };
    for (size_t index = 1; index + 1 < point_count; ++index) {
        if (RouteTurnAngleDeg(original[index - 1], original[index], original[index + 1]) >= kRouteCornerAngleDeg) {
            corners.push_back(index);
        }
    }
    corners.push_back(point_count - 1);

    // 计算每个 run 的直线与刚性平移量;仅直段 run 才持有用于重连拐角的直线。
    std::vector<CenterRun> runs;
    for (size_t corner_index = 0; corner_index + 1 < corners.size(); ++corner_index) {
        const size_t run_start = corners[corner_index];
        const size_t run_end = corners[corner_index + 1];
        const double dx = original[run_end].x - original[run_start].x;
        const double dy = original[run_end].y - original[run_start].y;
        const double length = std::sqrt(dx * dx + dy * dy);
        CenterRun run { .start = run_start, .end = run_end, .has_line = false, .shift = 0.0 };
        if (length < 1e-6) {
            runs.push_back(run);
            continue;
        }
        const double unit_x = dx / length;
        const double unit_y = dy / length;
        const double normal_x = -unit_y;
        const double normal_y = unit_x;
        bool is_straight = true;
        for (size_t j = run_start + 1; j < run_end; ++j) {
            if (PerpendicularDistance(original[j], original[run_start], original[run_end]) > kRouteRunStraightTol) {
                is_straight = false;
                break;
            }
        }
        if (!is_straight) {
            runs.push_back(run); // 弯曲 run:无直线,不平移
            continue;
        }
        run.has_line = true;
        run.direction = WorldPoint { .x = unit_x, .y = unit_y };
        run.normal = WorldPoint { .x = normal_x, .y = normal_y };
        std::vector<WorldPoint> samples;
        for (size_t j = run_start + 1; j < run_end; ++j) {
            samples.push_back(original[j]);
        }
        if (samples.empty()) {
            // 单段短桥也要居中:结构性拐角切分后常只剩一条边,仅看端点会把 A* 贴边线原样保留。
            for (const double t : { 0.25, 0.5, 0.75 }) {
                samples.push_back(WorldPoint {
                    .x = original[run_start].x + dx * t,
                    .y = original[run_start].y + dy * t,
                });
            }
        }

        std::vector<double> offsets;
        offsets.reserve(samples.size());
        for (const WorldPoint& sample : samples) {
            const double clearance_plus = MaxOffsetOnMesh(sample, normal_x, normal_y, probe_limit, point_on_mesh);
            const double clearance_minus = MaxOffsetOnMesh(sample, -normal_x, -normal_y, probe_limit, point_on_mesh);
            offsets.push_back((clearance_plus - clearance_minus) * 0.5);
        }
        std::sort(offsets.begin(), offsets.end());
        const double target_shift = std::clamp(offsets[offsets.size() / 2], -max_shift, max_shift);
        double chosen_shift = 0.0;
        for (const double scale : { 1.0, 0.75, 0.5, 0.25 }) { // 平移量逐步收缩,直至全程可行
            bool feasible = true;
            for (const WorldPoint& sample : samples) {
                const WorldPoint shifted {
                    .x = sample.x + normal_x * target_shift * scale,
                    .y = sample.y + normal_y * target_shift * scale,
                };
                if (!point_on_mesh(shifted)) {
                    feasible = false;
                    break;
                }
            }
            if (feasible) {
                chosen_shift = target_shift * scale;
                break;
            }
        }
        run.shift = chosen_shift;
        const WorldPoint& anchor = samples[samples.size() / 2]; // 直线锚定于 run 主体,而非可能贴角的拐点
        run.base = WorldPoint { .x = anchor.x + normal_x * run.shift, .y = anchor.y + normal_y * run.shift };
        runs.push_back(run);
    }

    // 逐 run 局部提交(取代旧的“整段全有或全无”安全闸)。每条直 run 作为独立事务:平移自身内部点、把
    // 自身两个拐角投影到平移后的直线上,仅当其局部窗口 [run_start-1 .. run_end+1] 仍全程在网格内、且不新增
    // 折返时才提交;任一 run 失败只丢弃它自己。贴住固定端点(整段首尾)的 run 不平移——平移它必在固定端拽出
    // 绕行钩。
    std::vector<WorldPoint> result = original;
    const double corner_move_limit = max_shift * kRouteCornerMoveFactor;
    for (const CenterRun& run : runs) {
        if (!run.has_line || run.shift == 0.0) {
            continue;
        }
        const size_t run_start = run.start;
        const size_t run_end = run.end;
        if (run_start == 0 || run_end == point_count - 1) {
            continue; // 贴住固定端点的 run:平移只会在 S/G/桥处拉出绕行钩
        }
        const double normal_x = run.normal.x;
        const double normal_y = run.normal.y;
        const double shift = run.shift;
        std::vector<WorldPoint> trial = result;
        for (size_t j = run_start + 1; j < run_end; ++j) {
            trial[j] = WorldPoint { .x = original[j].x + normal_x * shift, .y = original[j].y + normal_y * shift };
        }
        for (const size_t ci : { run_start, run_end }) { // 把本 run 自己的拐角投影到平移后的直线(全局端点除外)
            if (ci == 0 || ci == point_count - 1) {
                continue;
            }
            const WorldPoint foot = PerpendicularFoot(original[ci], run.base, run.direction);
            if (point_on_mesh(foot) && Distance(foot, original[ci]) <= corner_move_limit) {
                trial[ci] = foot;
            }
        }
        const size_t low = run_start > 0 ? run_start - 1 : 0;
        const size_t high = std::min(point_count - 1, run_end + 1);
        bool stays_on_mesh = true;
        for (size_t k = low; k < high; ++k) {
            if (!SegmentOnMesh(trial[k], trial[k + 1], point_on_mesh)) {
                stays_on_mesh = false;
                break;
            }
        }
        if (!stays_on_mesh) {
            continue;
        }
        bool adds_turn_back = false;
        for (size_t i = low; i <= high; ++i) {
            if (i == 0 || i + 1 >= point_count) {
                continue;
            }
            if (IsTurnBackAt(trial, i) && !IsTurnBackAt(result, i)) {
                adds_turn_back = true;
                break;
            }
        }
        if (adds_turn_back) {
            continue;
        }
        result = trial;
    }
    return result;
}

RoutePointsWithBreaks CenterRoutePointsWithBreaks(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const PointOnMeshFn& point_on_mesh)
{
    if (points.size() <= 2 || !point_on_mesh) {
        return RoutePointsWithBreaks { .points = points, .segment_breaks = segment_breaks };
    }

    std::vector<size_t> valid_breaks;
    for (size_t break_index : segment_breaks) {
        if (break_index > 0 && break_index < points.size()) {
            valid_breaks.push_back(break_index);
        }
    }
    valid_breaks = SortedUniqueBreaks(std::move(valid_breaks));

    std::vector<size_t> segment_starts { 0 };
    segment_starts.insert(segment_starts.end(), valid_breaks.begin(), valid_breaks.end());
    std::vector<size_t> segment_ends(valid_breaks.begin(), valid_breaks.end());
    segment_ends.push_back(points.size());

    RoutePointsWithBreaks result;
    for (size_t segment_index = 0; segment_index < segment_starts.size(); ++segment_index) {
        if (segment_index > 0) {
            result.segment_breaks.push_back(result.points.size());
        }
        std::vector<WorldPoint> segment = CenterContinuousSegment(
            points,
            segment_starts[segment_index],
            segment_ends[segment_index],
            kRouteCenterProbeLimit,
            kRouteCenterMaxShift,
            point_on_mesh);
        result.points.insert(result.points.end(), segment.begin(), segment.end());
    }
    result.segment_breaks = SortedUniqueBreaks(std::move(result.segment_breaks));
    return result;
}

// 贴水边判定结果:开阔侧单位法向 + 两侧余量。仅当 points[index] 既贴边又临水时返回,否则 nullopt。
struct EdgeOpenDirection
{
    double open_x = 0.0;
    double open_y = 0.0;
    double open_room = 0.0;  // 开阔侧(余量大)的横向余量
    double tight_room = 0.0; // 紧边(余量小)的横向余量
};

// 判断 points[index] 是否"贴着边",若是则回传可让开的方向(开阔侧单位法向 + 两侧余量)。
//   贴边:某一侧余量 < kRouteDecenterHugClearance 且两侧差 >= kRouteDecenterHugAsymmetry;
//   水边:紧边(余量小的一侧)外侧地面离开网格、或骤降 > kRouteDecenterWaterDrop(墙边不算,贴墙不危险)。
std::optional<EdgeOpenDirection> RouteEdgeOpenDirection(
    const std::vector<WorldPoint>& points,
    size_t index,
    const PointOnMeshFn& point_on_mesh,
    const GroundHeightFn& ground_height)
{
    const size_t point_count = points.size();
    if (index == 0 || index + 1 >= point_count) {
        return std::nullopt;
    }
    const WorldPoint& a = points[index - 1];
    const WorldPoint& c = points[index + 1];
    const double dx = c.x - a.x;
    const double dy = c.y - a.y;
    const double length = std::sqrt(dx * dx + dy * dy);
    if (length < 1e-6) {
        return std::nullopt;
    }
    const double normal_x = -dy / length;
    const double normal_y = dx / length;
    const double left = MaxOffsetOnMesh(points[index], normal_x, normal_y, kRouteCenterProbeLimit, point_on_mesh);
    const double right = MaxOffsetOnMesh(points[index], -normal_x, -normal_y, kRouteCenterProbeLimit, point_on_mesh);
    if (!(std::min(left, right) < kRouteDecenterHugClearance && std::abs(left - right) >= kRouteDecenterHugAsymmetry)) {
        return std::nullopt;
    }
    double tight_nx = 0.0;
    double tight_ny = 0.0;
    double tight = 0.0;
    if (left < right) {
        tight_nx = normal_x;
        tight_ny = normal_y;
        tight = left;
    }
    else {
        tight_nx = -normal_x;
        tight_ny = -normal_y;
        tight = right;
    }
    const std::optional<double> here = ground_height(points[index]);
    const std::optional<double> beyond = ground_height(WorldPoint {
        .x = points[index].x + tight_nx * (tight + 2.0),
        .y = points[index].y + tight_ny * (tight + 2.0),
    });
    const bool is_water = !beyond || (here && *beyond < *here - kRouteDecenterWaterDrop);
    if (!is_water) {
        return std::nullopt;
    }
    if (left > right) {
        return EdgeOpenDirection { .open_x = normal_x, .open_y = normal_y, .open_room = left, .tight_room = right };
    }
    return EdgeOpenDirection { .open_x = -normal_x, .open_y = -normal_y, .open_room = right, .tight_room = left };
}

// 居中细化一:守卫式 Gauss-Seidel 松弛。逐 run 刚性居中只动"够直的长直段",留下弯曲块、孤立尖角仍贴边。
// 这里对每个内部、非冻结、非真拐角(墙角)的点,把它推向 中点(消尖) 与 余量中心(离窄边) 的加权目标,
// 总位移钳在 kRouteRelaxMaxTranslate 内,且仅当候选点+两侧连段仍在网格上、且本点及左右邻的转角都不
// 超过 max(CAP, 原转角) 时才提交 —— 既不抹掉真直角,也绝不新增折返。冻结整段首尾与桥接点附近(±2)。
std::vector<WorldPoint> ClearanceRelaxWithBreaks(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const PointOnMeshFn& point_on_mesh,
    const SegmentWalkableFn& height_walkable)
{
    const size_t point_count = points.size();
    if (point_count <= 3 || !point_on_mesh || !height_walkable) {
        return points;
    }
    const double weight_clear = 1.0 - kRouteRelaxMidpointWeight;
    const std::vector<WorldPoint> original = points;
    std::vector<WorldPoint> result = points;
    // 真拐角(墙角):弦 i-1 -> i+1 高度不可走 = 直连会穿墙,该点必须留在原拐角处,不参与松弛。
    std::unordered_set<size_t> pinned;
    for (size_t index = 1; index + 1 < point_count; ++index) {
        if (!height_walkable(original[index - 1], original[index + 1])) {
            pinned.insert(index);
        }
    }
    std::unordered_set<size_t> frozen { size_t { 0 }, size_t { 1 }, point_count - 2, point_count - 1 };
    for (const size_t break_index : segment_breaks) {
        for (int delta = -2; delta <= 2; ++delta) {
            const long long idx = static_cast<long long>(break_index) + delta;
            if (idx >= 0) {
                frozen.insert(static_cast<size_t>(idx));
            }
        }
    }
    std::vector<double> origin_turn(point_count, 0.0);
    for (size_t index = 1; index + 1 < point_count; ++index) {
        origin_turn[index] = RouteTurnAngleDeg(original[index - 1], original[index], original[index + 1]);
    }
    const auto turn_at = [&](const std::vector<WorldPoint>& arr, size_t index) -> double {
        if (index == 0 || index + 1 >= point_count) {
            return 0.0;
        }
        return RouteTurnAngleDeg(arr[index - 1], arr[index], arr[index + 1]);
    };

    for (int iteration = 0; iteration < kRouteRelaxIterations; ++iteration) {
        for (size_t index = 1; index + 1 < point_count; ++index) {
            if (frozen.contains(index) || pinned.contains(index)) {
                continue;
            }
            const WorldPoint& a = result[index - 1];
            const WorldPoint& c = result[index + 1];
            const double dx = c.x - a.x;
            const double dy = c.y - a.y;
            const double length = std::sqrt(dx * dx + dy * dy);
            if (length < 1e-6) {
                continue;
            }
            const double normal_x = -dy / length;
            const double normal_y = dx / length;
            const double clearance_left = MaxOffsetOnMesh(result[index], normal_x, normal_y, kRouteCenterProbeLimit, point_on_mesh);
            const double clearance_right = MaxOffsetOnMesh(result[index], -normal_x, -normal_y, kRouteCenterProbeLimit, point_on_mesh);
            const double mid_x = (a.x + c.x) * 0.5;
            const double mid_y = (a.y + c.y) * 0.5;
            double bias = (clearance_left - clearance_right) * 0.5;
            if (kRouteRelaxBiasNearCap) {
                // 钳在最近墙距内:一侧射线穿开口逃逸(clearance 爆大)时,bias 不跟着逃逸跑,只按近侧墙距轻推。
                const double near_wall = std::min(clearance_left, clearance_right);
                if (bias > near_wall) {
                    bias = near_wall;
                }
                else if (bias < -near_wall) {
                    bias = -near_wall;
                }
            }
            const double clear_x = result[index].x + normal_x * bias;
            const double clear_y = result[index].y + normal_y * bias;
            double target_x = kRouteRelaxMidpointWeight * mid_x + weight_clear * clear_x;
            double target_y = kRouteRelaxMidpointWeight * mid_y + weight_clear * clear_y;
            const double move_x = target_x - original[index].x;
            const double move_y = target_y - original[index].y;
            const double move_length = std::sqrt(move_x * move_x + move_y * move_y);
            if (move_length > kRouteRelaxMaxTranslate) {
                target_x = original[index].x + move_x / move_length * kRouteRelaxMaxTranslate;
                target_y = original[index].y + move_y / move_length * kRouteRelaxMaxTranslate;
            }
            const WorldPoint candidate { .x = target_x, .y = target_y };
            if (!point_on_mesh(candidate)) {
                continue;
            }
            if (!(SegmentOnMesh(a, candidate, point_on_mesh) && SegmentOnMesh(candidate, c, point_on_mesh))) {
                continue;
            }
            // 转角守卫:原先每候选 trial=整表拷贝(O(N)/候选 → 每道 pass O(N²),密网格长路线爆炸)。
            // 改为就地换入候选、被拒再换回 —— O(1),turn_at 看到的就是 result[index]=候选,输出逐位不变。
            const WorldPoint saved = result[index];
            result[index] = candidate;
            bool within_cap = true;
            for (const size_t k : { index - 1, index, index + 1 }) {
                if (turn_at(result, k) > std::max(kRouteRelaxTurnCap, origin_turn[k]) + 1e-6) {
                    within_cap = false;
                    break;
                }
            }
            if (!within_cap) {
                result[index] = saved;
                continue;
            }
            // 已接受:result[index] 即 candidate
        }
    }
    return result;
}

// 居中细化二:贴水块整体平移。松弛是逐点的,搬不动"整条贴着水弯过去"的块(搬一个点就在邻点处拐出折返)。
// 这里把方向一致的连续贴水点聚成块,沿块的中位让开方向整体平移 —— 核心点满权、两端 M 个点按斜坡渐隐
// (把边界曲率摊开,不在某条边上一次性堆出折返)。在 (M, 平移量) 上搜索,按"触及窗口内剩余贴水点数"打分取
// 最优,只在严格更优、且不离网格/不新增折返时提交。块的延展不跨桥接点(block_edge)。info 仅在开头计算一次
// 用于分块,提交时不重算(与已验证原型一致)。
std::vector<WorldPoint> WaterEdgeShiftWithBreaks(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const PointOnMeshFn& point_on_mesh,
    const GroundHeightFn& ground_height)
{
    const long long point_count = static_cast<long long>(points.size());
    if (point_count <= 3 || !point_on_mesh || !ground_height) {
        return points;
    }
    std::vector<WorldPoint> result = points;
    const std::unordered_set<size_t> break_set(segment_breaks.begin(), segment_breaks.end());

    const auto open_dir = [&](const std::vector<WorldPoint>& route, long long index) {
        return RouteEdgeOpenDirection(route, static_cast<size_t>(index), point_on_mesh, ground_height);
    };
    const auto is_block_edge = [&](long long j) {
        return break_set.contains(static_cast<size_t>(j)) || break_set.contains(static_cast<size_t>(j + 1));
    };

    std::vector<std::optional<EdgeOpenDirection>> info(static_cast<size_t>(point_count));
    for (long long index = 0; index < point_count; ++index) {
        info[static_cast<size_t>(index)] = open_dir(result, index);
    }

    long long i = 1;
    while (i < point_count - 1) {
        if (!info[static_cast<size_t>(i)]) {
            ++i;
            continue;
        }
        const long long start = i;
        const EdgeOpenDirection& head = *info[static_cast<size_t>(start)];
        while (i + 1 < point_count - 1 && info[static_cast<size_t>(i + 1)] && !is_block_edge(i)
               && (info[static_cast<size_t>(i + 1)]->open_x * head.open_x + info[static_cast<size_t>(i + 1)]->open_y * head.open_y)
                      > -0.5) {
            ++i;
        }
        const long long end = i;
        ++i;

        double sum_x = 0.0;
        double sum_y = 0.0;
        double min_tight = info[static_cast<size_t>(start)]->tight_room;
        double min_open = info[static_cast<size_t>(start)]->open_room;
        for (long long j = start; j <= end; ++j) {
            const EdgeOpenDirection& entry = *info[static_cast<size_t>(j)];
            sum_x += entry.open_x;
            sum_y += entry.open_y;
            min_tight = std::min(min_tight, entry.tight_room);
            min_open = std::min(min_open, entry.open_room);
        }
        const double dir_length = std::sqrt(sum_x * sum_x + sum_y * sum_y);
        if (dir_length < 1e-6) {
            continue;
        }
        const double unit_x = sum_x / dir_length;
        const double unit_y = sum_y / dir_length;
        const double need = std::max(0.0, kRouteWaterShiftSafe - min_tight);
        const double shift = std::min({ need, std::max(0.0, min_open - kRouteWaterShiftSafe), kRouteWaterShiftMax });
        if (shift < 0.5) {
            continue;
        }
        const long long window_lo = std::max(1LL, start - 6);
        const long long window_hi = std::min(point_count - 1, end + 7); // exclusive
        const auto count_hugs = [&](const std::vector<WorldPoint>& route) {
            int count = 0;
            for (long long j = window_lo; j < window_hi; ++j) {
                if (open_dir(route, j)) {
                    ++count;
                }
            }
            return count;
        };
        const int before = count_hugs(result);

        bool have_best = false;
        int best_after = 0;
        int best_margin = 0;
        double best_neg_scale = 0.0;
        std::vector<WorldPoint> best_trial;

        for (int margin = 2; margin <= 6; ++margin) {
            long long low = std::max(1LL, start - margin);
            long long high = std::min(point_count - 2, end + margin);
            while (low > 1 && is_block_edge(low - 1)) {
                ++low;
            }
            while (high < point_count - 2 && is_block_edge(high)) {
                --high;
            }
            const auto taper_weight = [&](long long j) -> double {
                if (start <= j && j <= end) {
                    return 1.0;
                }
                if (j < start) {
                    return std::max(0.0, static_cast<double>(j - (start - margin)) / margin);
                }
                return std::max(0.0, static_cast<double>((end + margin) - j) / margin);
            };
            const double scales[] = { shift, shift * 0.85, shift * 0.7, shift * 0.55 };
            for (const double scale : scales) {
                std::vector<WorldPoint> trial = result;
                for (long long j = low; j <= high; ++j) {
                    const double weight = taper_weight(j);
                    trial[static_cast<size_t>(j)] = WorldPoint {
                        .x = result[static_cast<size_t>(j)].x + unit_x * scale * weight,
                        .y = result[static_cast<size_t>(j)].y + unit_y * scale * weight,
                    };
                }
                bool feasible = true;
                for (long long j = low; j <= high && feasible; ++j) {
                    if (!point_on_mesh(trial[static_cast<size_t>(j)])) {
                        feasible = false;
                    }
                }
                if (feasible) {
                    for (long long k = low - 1; k <= high && feasible; ++k) {
                        if (!SegmentOnMesh(trial[static_cast<size_t>(k)], trial[static_cast<size_t>(k + 1)], point_on_mesh)) {
                            feasible = false;
                        }
                    }
                }
                if (feasible) {
                    for (long long k = low - 1; k <= high + 1; ++k) {
                        if (k > 0 && k < point_count - 1
                            && RouteTurnAngleDeg(
                                   trial[static_cast<size_t>(k - 1)],
                                   trial[static_cast<size_t>(k)],
                                   trial[static_cast<size_t>(k + 1)])
                                   > 90.0
                            && RouteTurnAngleDeg(
                                   result[static_cast<size_t>(k - 1)],
                                   result[static_cast<size_t>(k)],
                                   result[static_cast<size_t>(k + 1)])
                                   <= 90.0) {
                            feasible = false;
                            break;
                        }
                    }
                }
                if (!feasible) {
                    continue;
                }
                const int after = count_hugs(trial);
                const double neg_scale = -scale;
                bool better = false;
                if (!have_best) {
                    better = true;
                }
                else if (after != best_after) {
                    better = after < best_after;
                }
                else if (margin != best_margin) {
                    better = margin < best_margin;
                }
                else {
                    better = neg_scale < best_neg_scale;
                }
                if (better) {
                    have_best = true;
                    best_after = after;
                    best_margin = margin;
                    best_neg_scale = neg_scale;
                    best_trial = trial;
                }
            }
            if (have_best && best_after == 0) {
                break;
            }
        }
        if (have_best && best_after < before) {
            result = best_trial;
        }
    }
    return result;
}

// 居中细化三:抗噪裕度地板(收尾)。对"各向同性最近边界 < 地板"的内部点(含 pinned 拐角)沿中轴梯度
// (各向探测的开阔向量)推离边界到地板;早停探测、已达标点 settled 永久跳过、收敛即停。守卫:候选+两侧连段
// 在网格、推后 d_min 确有增益、不新增 > max(CAP, 原转角) 折返。冻结整段首尾与桥接±2。
std::vector<WorldPoint> ClearanceFloorWithBreaks(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const PointOnMeshFn& point_on_mesh)
{
    const size_t point_count = points.size();
    if (point_count <= 3 || !point_on_mesh || !kRouteFloorEnable || kRouteFloorMinClearance <= 0.0) {
        return points;
    }
    const double floor_clearance = kRouteFloorMinClearance;
    const double probe_cap = floor_clearance + kRouteFloorProbeMargin;
    double probe_dx[kRouteFloorProbeDirs];
    double probe_dy[kRouteFloorProbeDirs];
    for (int k = 0; k < kRouteFloorProbeDirs; ++k) {
        const double angle = 2.0 * std::numbers::pi * k / kRouteFloorProbeDirs;
        probe_dx[k] = std::cos(angle);
        probe_dy[k] = std::sin(angle);
    }
    const std::vector<WorldPoint> original = points;
    std::vector<WorldPoint> result = points;
    std::unordered_set<size_t> frozen { size_t { 0 }, size_t { 1 }, point_count - 2, point_count - 1 };
    for (const size_t break_index : segment_breaks) {
        for (int delta = -2; delta <= 2; ++delta) {
            const long long idx = static_cast<long long>(break_index) + delta;
            if (idx >= 0) {
                frozen.insert(static_cast<size_t>(idx));
            }
        }
    }
    std::vector<double> origin_turn(point_count, 0.0);
    for (size_t index = 1; index + 1 < point_count; ++index) {
        origin_turn[index] = RouteTurnAngleDeg(original[index - 1], original[index], original[index + 1]);
    }
    std::vector<char> settled(point_count, 0);
    const auto min_clearance = [&](const WorldPoint& p) -> double {
        double value = probe_cap;
        for (int k = 0; k < kRouteFloorProbeDirs; ++k) {
            value = std::min(value, MaxOffsetOnMesh(p, probe_dx[k], probe_dy[k], probe_cap, point_on_mesh));
        }
        return value;
    };
    const auto turn_at = [&](const std::vector<WorldPoint>& arr, size_t index) -> double {
        if (index == 0 || index + 1 >= point_count) {
            return 0.0;
        }
        return RouteTurnAngleDeg(arr[index - 1], arr[index], arr[index + 1]);
    };

    for (int iteration = 0; iteration < kRouteFloorIterations; ++iteration) {
        bool moved = false;
        for (size_t index = 1; index + 1 < point_count; ++index) {
            if (frozen.contains(index) || settled[index]) {
                continue;
            }
            const WorldPoint here = result[index];
            // 单次扫 K 向:最近边界距 d_min 与开阔向量(中轴梯度)
            double d_min = probe_cap;
            double open_x = 0.0;
            double open_y = 0.0;
            for (int k = 0; k < kRouteFloorProbeDirs; ++k) {
                const double offset = MaxOffsetOnMesh(here, probe_dx[k], probe_dy[k], probe_cap, point_on_mesh);
                open_x += probe_dx[k] * offset;
                open_y += probe_dy[k] * offset;
                d_min = std::min(d_min, offset);
            }
            if (d_min >= floor_clearance) {
                settled[index] = 1; // 最近边界已达地板,后续轮次跳过
                continue;
            }
            const double open_length = std::sqrt(open_x * open_x + open_y * open_y);
            if (open_length < 1e-6) {
                settled[index] = 1; // 对称窄颈:开阔向量相消,半宽已到极限
                continue;
            }
            const double unit_x = open_x / open_length;
            const double unit_y = open_y / open_length;
            const double step = std::min(floor_clearance - d_min, kRouteFloorStep);
            double cand_x = here.x + unit_x * step;
            double cand_y = here.y + unit_y * step;
            const double move_x = cand_x - original[index].x;
            const double move_y = cand_y - original[index].y;
            const double move_length = std::sqrt(move_x * move_x + move_y * move_y);
            if (move_length > kRouteFloorMaxTranslate) {
                cand_x = original[index].x + move_x / move_length * kRouteFloorMaxTranslate;
                cand_y = original[index].y + move_y / move_length * kRouteFloorMaxTranslate;
            }
            const WorldPoint candidate { .x = cand_x, .y = cand_y };
            if (!point_on_mesh(candidate)) {
                continue;
            }
            const WorldPoint& a = result[index - 1];
            const WorldPoint& c = result[index + 1];
            if (!(SegmentOnMesh(a, candidate, point_on_mesh) && SegmentOnMesh(candidate, c, point_on_mesh))) {
                continue;
            }
            if (min_clearance(candidate) <= d_min + 1e-6) {
                continue; // 推后必须确有余量增益(防过冲到更差)
            }
            // 转角守卫:就地换入候选、被拒再换回(O(1),不做整表拷贝 —— 否则每道 pass O(N²),密网格长路线爆炸)。
            const WorldPoint saved = result[index];
            result[index] = candidate;
            bool within_cap = true;
            for (const size_t k : { index - 1, index, index + 1 }) {
                if (turn_at(result, k) > std::max(kRouteRelaxTurnCap, origin_turn[k]) + 1e-6) {
                    within_cap = false;
                    break;
                }
            }
            if (!within_cap) {
                result[index] = saved;
                continue;
            }
            // 已接受:result[index] 即 candidate
            moved = true;
        }
        if (!moved) {
            break; // 收敛即停
        }
    }
    return result;
}

std::vector<WorldPoint> RealGapRepelWithBreaks(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const PointOnMeshFn& point_on_mesh,
    const SegmentWalkableFn& height_walkable)
{
    const size_t point_count = points.size();
    if (point_count <= 3 || !point_on_mesh || !height_walkable || !kRouteGapRepelEnable) {
        return points;
    }
    const double trigger = kRouteGapRepelTrigger;
    const double safe = kRouteGapRepelSafe;
    double probe_dx[kRouteGapRepelProbeDirs];
    double probe_dy[kRouteGapRepelProbeDirs];
    for (int k = 0; k < kRouteGapRepelProbeDirs; ++k) {
        const double angle = 2.0 * std::numbers::pi * k / kRouteGapRepelProbeDirs;
        probe_dx[k] = std::cos(angle);
        probe_dy[k] = std::sin(angle);
    }
    const std::vector<WorldPoint> original = points;
    std::vector<WorldPoint> result = points;
    std::unordered_set<size_t> frozen { size_t { 0 }, size_t { 1 }, point_count - 2, point_count - 1 };
    for (const size_t break_index : segment_breaks) {
        for (int delta = -2; delta <= 2; ++delta) {
            const long long idx = static_cast<long long>(break_index) + delta;
            if (idx >= 0) {
                frozen.insert(static_cast<size_t>(idx));
            }
        }
    }
    std::vector<double> origin_turn(point_count, 0.0);
    for (size_t index = 1; index + 1 < point_count; ++index) {
        origin_turn[index] = RouteTurnAngleDeg(original[index - 1], original[index], original[index + 1]);
    }
    const auto turn_at = [&](const std::vector<WorldPoint>& arr, size_t index) -> double {
        if (index == 0 || index + 1 >= point_count) {
            return 0.0;
        }
        return RouteTurnAngleDeg(arr[index - 1], arr[index], arr[index + 1]);
    };
    const auto gap_distance = [&](const WorldPoint& p) -> double {
        double nearest = -1.0;
        for (int k = 0; k < kRouteGapRepelProbeDirs; ++k) {
            const double offset = MaxOffsetOnMesh(p, probe_dx[k], probe_dy[k], trigger, point_on_mesh, kRouteGapRepelProbeStep);
            if (offset >= trigger) {
                continue;
            }
            const WorldPoint beyond {
                .x = p.x + probe_dx[k] * (offset + kRouteGapRepelProbeStep),
                .y = p.y + probe_dy[k] * (offset + kRouteGapRepelProbeStep),
            };
            if (!height_walkable(p, beyond)) {
                if (nearest < 0.0 || offset < nearest) {
                    nearest = offset;
                }
            }
        }
        return nearest;
    };

    for (size_t index = 1; index + 1 < point_count; ++index) {
        if (frozen.contains(index)) {
            continue;
        }
        const WorldPoint here = result[index];
        const double base = gap_distance(here);
        if (base < 0.0 || base >= safe) {
            continue;
        }
        const WorldPoint& a = result[index - 1];
        const WorldPoint& c = result[index + 1];
        WorldPoint best = here;
        double best_score = base;
        for (int k = 0; k < kRouteGapRepelProbeDirs; ++k) {
            for (double push = kRouteGapRepelStep; push <= kRouteGapRepelMaxTranslate + 1e-9; push += kRouteGapRepelStep) {
                const WorldPoint candidate { .x = here.x + probe_dx[k] * push, .y = here.y + probe_dy[k] * push };
                if (!point_on_mesh(candidate)) {
                    break; // 此向已出界,不必再远
                }
                if (!(SegmentOnMesh(a, candidate, point_on_mesh) && SegmentOnMesh(candidate, c, point_on_mesh))) {
                    continue;
                }
                const WorldPoint saved = result[index];
                result[index] = candidate;
                bool within_cap = true;
                for (const size_t neighbor : { index - 1, index, index + 1 }) {
                    if (turn_at(result, neighbor) > std::max(kRouteRelaxTurnCap, origin_turn[neighbor]) + 1e-6) {
                        within_cap = false;
                        break;
                    }
                }
                result[index] = saved;
                if (!within_cap) {
                    continue;
                }
                const double beyond_gap = gap_distance(candidate);
                const double score = (beyond_gap < 0.0) ? (safe + 1.0) : beyond_gap;
                if (score > best_score) {
                    best = candidate;
                    best_score = score;
                }
            }
            if (best_score >= safe) {
                break; // 已够远,不必再试其它方向
            }
        }
        result[index] = best;
    }
    return result;
}

}

RoutePointsWithBreaks PostProcessRoutePoints(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const SegmentWalkableFn& is_segment_walkable,
    const PointOnMeshFn& point_on_mesh,
    const GroundHeightFn& ground_height)
{
    const auto deduped = DedupePointsWithBreaks(points, segment_breaks);
    auto simplified = RemoveCollinearWithBreaks(deduped.points, deduped.segment_breaks);
    // LOS 拉直:将走廊内的锯齿拉直,仅在真拐角处保留点。
    auto thinned = ThinRoutePointsWithBreaks(simplified.points, simplified.segment_breaks, is_segment_walkable, point_on_mesh);
    // 加密恢复内部采样点,使后续居中有直段可整体平移(拉直输出仅含拐角)。
    auto densified = DensifyRoutePointsWithBreaks(thinned.points, thinned.segment_breaks);
    // 居中:将直段平移至走廊中线、在拐角处重连,精确保留直角。
    auto centered = CenterRoutePointsWithBreaks(densified.points, densified.segment_breaks, point_on_mesh);
    // 居中细化一:守卫式松弛,把逐 run 刚性居中漏掉的弯块/孤立尖角逐点推离窄边、消尖角(保点数与桥接点)。
    auto relaxed = ClearanceRelaxWithBreaks(centered.points, centered.segment_breaks, point_on_mesh, is_segment_walkable);
    // 居中细化二:方向一致的连续贴水点聚成块整体平移,让出单侧安全余量(保点数与桥接点)。
    auto decentered = WaterEdgeShiftWithBreaks(relaxed, centered.segment_breaks, point_on_mesh, ground_height);
    // 居中/平移可能拉出超过 kRouteMaxPointDistance 的长边,末尾再加密一次以保证点距上界。
    auto densified_final = DensifyRoutePointsWithBreaks(decentered, centered.segment_breaks);
    // 居中细化三:抗噪裕度地板,把残留贴 navmesh 内角的点(含 pinned 拐角)沿中轴推离边界到地板,
    // 确保全程留抗噪余量(上游图像定位有噪,贴可走面边界=出界风险)。
    densified_final.points = ClearanceFloorWithBreaks(densified_final.points, densified_final.segment_breaks, point_on_mesh);
    // 断崖抗掉落(收尾):上面各道把真实断崖(会掉落)与无害接缝一视同仁、且小步/刚性块移,遇窄口贴断崖点卡在原地。
    // 这里只挑贴真实断崖(高度不连续)的点,允许大步向开阔侧跳离,尽力拉开抗噪余量;守卫同地板(网格/连段/不新增折返)。
    densified_final.points =
        RealGapRepelWithBreaks(densified_final.points, densified_final.segment_breaks, point_on_mesh, is_segment_walkable);
    return densified_final;
}

}
