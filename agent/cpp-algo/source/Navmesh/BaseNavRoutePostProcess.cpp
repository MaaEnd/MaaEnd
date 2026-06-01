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
constexpr double kRouteCenterProbeLimit = 12.0;   // 居中时单侧探测墙距的上限(px)
constexpr double kRouteCenterMaxShift = 8.0;      // 直段整体横移上限(px)
constexpr double kCenterProbeStep = 0.5;          // 墙距探测步进(px)
constexpr double kCenterValidateStep = 0.5;       // 连段校验采样步进(px)
constexpr int kRoutePullMaxSkip = 8;              // 拉直可越过的最大连续不可达点数,用于跨越非单调视线遮挡
constexpr double kRoutePullMaxReach = 64.0;       // 单条捷径的最大长度(px)
constexpr double kRouteCornerAngleDeg = 35.0;     // 转角达此值即视为结构性拐角(px),在此切分直段
constexpr double kRouteRunStraightTol = 1.6;      // 直段判据:内部点偏离首尾弦的上限(px)
constexpr double kRouteCornerMoveFactor = 1.5;    // 拐角重连位移上限 = kRouteCenterMaxShift × 此系数

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

// 贪心 LOS 拉直:从锚点跳至最远可直达点,仅在结构性拐角处保留落点。可行性以高度连续性为判据——
// 共面锯齿被拉直至走廊中线,绕墙拐角因踩墙产生高度跳变而保留为直角。视线在重叠网格上可能非单调,
// 故允许越过至多 kRoutePullMaxSkip 个不可达点。取代旧 RDP+march 方案(几何抽稀留锯齿、march 误拒直捷径)。
std::vector<size_t> ThinContinuousSegment(
    const std::vector<WorldPoint>& points,
    size_t start,
    size_t end,
    const SegmentWalkableFn& is_segment_walkable)
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
            if (is_segment_walkable(points[anchor], points[probe])) {
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
    const SegmentWalkableFn& is_segment_walkable)
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
            ThinContinuousSegment(points, segment_starts[segment_index], segment_ends[segment_index], is_segment_walkable);
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
            result.push_back(
                WorldPoint {
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

// 折返点数(相邻两段方向夹角 > 90°,即点积 < 0):衡量锯齿/回绕程度,居中不得使其增大。
int RouteTurnBackCount(const std::vector<WorldPoint>& points)
{
    int count = 0;
    for (size_t index = 1; index + 1 < points.size(); ++index) {
        const double ax = points[index].x - points[index - 1].x;
        const double ay = points[index].y - points[index - 1].y;
        const double bx = points[index + 1].x - points[index].x;
        const double by = points[index + 1].y - points[index].y;
        if (ax * ax + ay * ay < 1e-9 || bx * bx + by * by < 1e-9) {
            continue;
        }
        if (ax * bx + ay * by < 0) {
            ++count;
        }
    }
    return count;
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

// 两直线 (base_a + t·dir_a) 与 (base_b + s·dir_b) 的交点;近平行时返回 nullopt。
std::optional<WorldPoint> LineIntersection(
    const WorldPoint& base_a,
    const WorldPoint& dir_a,
    const WorldPoint& base_b,
    const WorldPoint& dir_b)
{
    const double cross = dir_a.x * dir_b.y - dir_a.y * dir_b.x;
    if (std::abs(cross) < 1e-9) {
        return std::nullopt;
    }
    const double rx = base_b.x - base_a.x;
    const double ry = base_b.y - base_a.y;
    const double t = (rx * dir_b.y - ry * dir_b.x) / cross;
    return WorldPoint { .x = base_a.x + dir_a.x * t, .y = base_a.y + dir_a.y * t };
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

// 结构保持式居中(不平滑、不圆角化直角):在拐角处切分直段,将直段沿法向刚性平移至走廊中线,拐角移到
// 相邻直段延长线的交点——直段仍直、直角精确保留。墙距用点包含判据(避免 march 在重叠网格上低估余量)。
// 安全闸:整段仅在全程在网格内且折返数不增时接受,否则回退。
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
        WorldPoint anchor;
        if (run_end - run_start >= 2) { // 含可居中的内部点
            std::vector<double> offsets;
            for (size_t j = run_start + 1; j < run_end; ++j) {
                const double clearance_plus = MaxOffsetOnMesh(original[j], normal_x, normal_y, probe_limit, point_on_mesh);
                const double clearance_minus = MaxOffsetOnMesh(original[j], -normal_x, -normal_y, probe_limit, point_on_mesh);
                offsets.push_back((clearance_plus - clearance_minus) * 0.5);
            }
            std::sort(offsets.begin(), offsets.end());
            const double target_shift = std::clamp(offsets[offsets.size() / 2], -max_shift, max_shift);
            double chosen_shift = 0.0;
            for (const double scale : { 1.0, 0.75, 0.5, 0.25 }) { // 平移量逐步收缩,直至全程可行
                bool feasible = true;
                for (size_t j = run_start + 1; j < run_end; ++j) {
                    const WorldPoint shifted {
                        .x = original[j].x + normal_x * target_shift * scale,
                        .y = original[j].y + normal_y * target_shift * scale,
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
            anchor = original[run_start + 1]; // 直线锚定于 run 主体,而非可能贴角的拐点
        }
        else {
            anchor = original[run_start]; // 长度为 1 的 run:直线即该单段本身(shift = 0)
        }
        run.base = WorldPoint { .x = anchor.x + normal_x * run.shift, .y = anchor.y + normal_y * run.shift };
        runs.push_back(run);
    }

    // 重连内部拐角:移至相邻两条已平移直线的交点(精确保留转角角度)。
    std::vector<WorldPoint> result = original;
    const double corner_move_limit = max_shift * kRouteCornerMoveFactor;
    for (size_t corner_index = 1; corner_index + 1 < corners.size(); ++corner_index) {
        const size_t ci = corners[corner_index];
        const CenterRun& left = runs[corner_index - 1];
        const CenterRun& right = runs[corner_index];
        std::optional<WorldPoint> candidate;
        if (left.has_line && right.has_line) {
            candidate = LineIntersection(left.base, left.direction, right.base, right.direction);
        }
        else if (left.has_line) {
            candidate = PerpendicularFoot(original[ci], left.base, left.direction);
        }
        else if (right.has_line) {
            candidate = PerpendicularFoot(original[ci], right.base, right.direction);
        }
        if (candidate.has_value() && point_on_mesh(*candidate) && Distance(*candidate, original[ci]) <= corner_move_limit) {
            result[ci] = *candidate;
        }
    }

    // 内部点:按所在 run 的平移量刚性平移。
    for (const CenterRun& run : runs) {
        if (!run.has_line || run.shift == 0.0) {
            continue;
        }
        const double normal_x = run.normal.x;
        const double normal_y = run.normal.y;
        const double shift = run.shift;
        for (size_t j = run.start + 1; j < run.end; ++j) {
            result[j] = WorldPoint { .x = original[j].x + normal_x * shift, .y = original[j].y + normal_y * shift };
        }
    }

    // 安全闸:全程在网格内、且不比原路线更锯齿,否则整段回退。
    bool stays_on_mesh = true;
    for (size_t k = 0; k + 1 < point_count; ++k) {
        if (!SegmentOnMesh(result[k], result[k + 1], point_on_mesh)) {
            stays_on_mesh = false;
            break;
        }
    }
    if (!stays_on_mesh || RouteTurnBackCount(result) > RouteTurnBackCount(original)) {
        return original;
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

}

RoutePointsWithBreaks PostProcessRoutePoints(
    const std::vector<WorldPoint>& points,
    const std::vector<size_t>& segment_breaks,
    const SegmentWalkableFn& is_segment_walkable,
    const PointOnMeshFn& point_on_mesh)
{
    const auto deduped = DedupePointsWithBreaks(points, segment_breaks);
    auto simplified = RemoveCollinearWithBreaks(deduped.points, deduped.segment_breaks);
    // LOS 拉直:将走廊内的锯齿拉直,仅在真拐角处保留点。
    auto thinned = ThinRoutePointsWithBreaks(simplified.points, simplified.segment_breaks, is_segment_walkable);
    // 加密恢复内部采样点,使后续居中有直段可整体平移(拉直输出仅含拐角)。
    auto densified = DensifyRoutePointsWithBreaks(thinned.points, thinned.segment_breaks);
    // 居中:将直段平移至走廊中线、在拐角处重连,精确保留直角。
    auto centered = CenterRoutePointsWithBreaks(densified.points, densified.segment_breaks, point_on_mesh);
    // 居中可能拉出超过 kRouteMaxPointDistance 的长边,末尾再加密一次以保证点距上界。
    return DensifyRoutePointsWithBreaks(centered.points, centered.segment_breaks);
}

}
