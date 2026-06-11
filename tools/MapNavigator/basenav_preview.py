from __future__ import annotations

import gzip
import heapq
import math
import struct
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path


MAGIC = b"BNAV"
VERSION = 2
FNV_OFFSET = 14695981039346656037
FNV_PRIME = 1099511628211
BRIDGE_FIXED_COST = 12.0
BRIDGE_GAP_COST_FACTOR = 3.0
BRIDGE_HEIGHT_COST_FACTOR = 40.0
BRIDGE_MAX_HEIGHT_DELTA = 3.0
SMALL_BRIDGE_COMPONENT_MAX_TRIANGLES = 512
SMALL_BRIDGE_MAX_GAP = 4.0
ROUTE_MIN_POINT_DISTANCE = 6.0
ROUTE_SIMPLIFY_EPSILON = 3.0
ROUTE_MAX_POINT_DISTANCE = 4.0
ROUTE_CENTER_PROBE_LIMIT = 12.0  # 走廊横截面单侧探测上限(像素),够覆盖常见走廊半宽
ROUTE_CENTER_MAX_SHIFT = 8.0  # 直段整体横移的上限(像素),低破坏性夹紧
ROUTE_CORNER_ANGLE_DEG = 35.0  # >= 此转角视为结构性拐角(真直角):在此切分直段,且绝不跨拐角居中(保住直角)
ROUTE_RUN_STRAIGHT_TOL = 1.6  # 直段判据:段内每个内部点偏离首尾弦 <= 此值(像素)才算"直",方可整体平移
ROUTE_CORNER_MOVE_FACTOR = 1.5  # 重连后拐角相对原位的最大位移 = ROUTE_CENTER_MAX_SHIFT * 此系数
CENTER_PROBE_STEP = 0.5  # 点包含式墙距的步进(像素):沿法向逐步外推,直到离开网格
CENTER_VALIDATE_STEP = 0.5  # 点包含式连段校验的采样步进(像素):候选边按此步采样须全部在网格内
ROUTE_PULL_SAMPLE_STEP = 0.5  # LOS 拉直 oracle 的采样步进(像素):捷径按此步采样查"点在网格上 + 相邻采样地面高度不跳变"
ROUTE_PULL_MAX_SKIP = 8  # 拉直时越过非单调视线遮挡的最大连续不可达点数(重叠/碎片网格上个别 portal 中点会外凸,挡近不挡远);真拐角整条对臂都被墙挡住、远超此值,绝不会被误跨
ROUTE_PULL_MAX_REACH = 64.0  # 单条直线捷径的最大长度(像素):拉直的性能上界,把 O(n·L) 的 L 钳住;开阔直路被切成共线小段(加密补回、居中整体平移,最终形状不变)
SEGMENT_WALK_SNAP_RADIUS = 1.0
SEGMENT_WALK_EPSILON = 1e-6
SEGMENT_PARALLEL_EPSILON = 1e-12
# 空间索引网格边长(像素)。网格仅是查询加速结构,不影响 snap/raycast 的输出(任何
# 落在查询半径内的三角形都会按包围盒插入到对应 bin)。烘焙后网格三角形极细碎
# (中位包围盒约 1px),96px 的粗 bin 会让单个 bin 堆叠上万个三角形,使纯 Python
# 的 snap 退化成线性扫描;取 8px 让每个 bin 仅含数十个三角形,snap 提前命中。
INDEX_BIN_SIZE = 16.0

HEADER_STRUCT = struct.Struct("<4sHHIIIIQQQQQ")
ZONE_STRUCT = struct.Struct("<HHIIIIff4f")
VERTEX_STRUCT = struct.Struct("<fff")
TRIANGLE_STRUCT = struct.Struct("<IIIiiiIff")
LINK_STRUCT = struct.Struct("<II")


@dataclass(frozen=True, slots=True)
class _BaseNavZone:
    zone_id: int
    name: str
    first_triangle: int
    triangle_count: int
    component_count: int
    width: float
    height: float
    transform: tuple[float, float, float, float]
    flags: int = 0


@dataclass(frozen=True, slots=True)
class _BaseNavVertex:
    u: float
    v: float
    height: float


@dataclass(frozen=True, slots=True)
class _BaseNavTriangle:
    vertices: tuple[int, int, int]
    neighbors: tuple[int, int, int]
    component_id: int
    center: tuple[float, float]


@dataclass(frozen=True)
class _SnapResult:
    triangle: int
    point: tuple[float, float]
    distance: float


@dataclass
class _BaseNavRoute:
    points: list[tuple[float, float]]
    triangles: list[int]
    cost: float
    segment_breaks: list[int]


def _report_progress(callback, progress: float) -> None:
    if callback is not None:
        try:
            callback(progress)
        except Exception:
            pass


def load_basenav_field(input_file: Path, progress_callback=None) -> BaseNavField:
    return BaseNavField(input_file, progress_callback=progress_callback)


@dataclass(frozen=True)
class PreviewRoute:
    points: list[tuple[float, float]]
    world_points: list[tuple[float, float]]
    cells: list[object]
    segment_breaks: list[int] | None = None


def find_preview_route(
    field: BaseNavField,
    zone_id: int,
    display_zone_id: str,
    start: tuple[float, float],
    goal: tuple[float, float],
    snap_radius: float,
) -> PreviewRoute:
    del display_zone_id
    route = field.find_route(zone_id, start, goal, snap_radius)
    return PreviewRoute(points=route.points, world_points=route.points, cells=[], segment_breaks=route.segment_breaks)


class BaseNavField:
    def __init__(self, path: Path, bin_size: float = INDEX_BIN_SIZE, progress_callback=None) -> None:
        self.path = path
        self.zones, self.vertices, self.triangles, self.links = _read_basenav(path, progress_callback=progress_callback)
        self.bin_size = bin_size
        self.zone_by_id = {zone.zone_id: zone for zone in self.zones}
        self.zone_by_name = {zone.name: zone for zone in self.zones}
        self.triangle_zone: list[int] = [0] * len(self.triangles)
        self.triangle_bounds: list[tuple[float, float, float, float]] = []
        self.bins: dict[tuple[int, int, int], list[int]] = defaultdict(list)
        self.adjacency: list[list[int]] = [[] for _triangle in self.triangles]
        self.natural_component: list[int] = []
        self.natural_component_size: list[int] = []
        self.triangle_height: list[float] = [0.0] * len(self.triangles)
        self.overlay_cache: dict[int, object] = {}
        self.dots_cache: dict[int, object] = {}
        self._build_index(progress_callback=progress_callback)

    def zone_ids(self) -> list[int]:
        return [zone.zone_id for zone in self.zones]

    def zone_label(self, zone_id: int) -> str:
        zone = self.zone_by_id.get(zone_id)
        return f"{zone.zone_id}:{zone.name}" if zone is not None else str(zone_id)

    def suggested_zone_label(self, display_zone_id: str) -> str:
        zone = self.zone_by_name.get(display_zone_id)
        if zone is not None:
            return self.zone_label(zone.zone_id)
        return ""

    def zone_bounds(self, zone_id: int, display_zone_id: str = "") -> tuple[float, float, float, float] | None:
        del display_zone_id
        zone = self.zone_by_id.get(zone_id)
        if zone is None:
            return None
        return 0.0, 0.0, zone.width, zone.height

    def walkable_preview_points(
        self,
        zone_id: int,
        max_points: int = 60000,
        display_zone_id: str = "",
    ) -> list[tuple[float, float]]:
        del display_zone_id
        zone = self.zone_by_id.get(zone_id)
        if zone is None or zone.triangle_count <= 0:
            return []
        stride = max(1, math.ceil(zone.triangle_count / max_points))
        start = zone.first_triangle
        end = start + zone.triangle_count
        return [self.triangles[index].center for index in range(start, end, stride)]

    def overlay_image(self, zone_id: int, progress_callback=None):
        if zone_id in self.overlay_cache:
            return self.overlay_cache[zone_id]
        try:
            from PIL import Image, ImageDraw
        except ImportError:
            return None

        zone = self.zone_by_id.get(zone_id)
        if zone is None or zone.width <= 0 or zone.height <= 0:
            return None
        _scale = 0.5
        _w = math.ceil(zone.width * _scale)
        _h = math.ceil(zone.height * _scale)
        image = Image.new("RGBA", (_w, _h), (0, 0, 0, 0))
        draw = ImageDraw.Draw(image)
        start = zone.first_triangle
        end = start + zone.triangle_count
        total = end - start
        _step = max(1, total // 50) if total > 100 else 1
        for _idx, triangle_index in enumerate(range(start, end)):
            tv = self.triangles[triangle_index].vertices
            pts = [
                (self.vertices[tv[0]].u * _scale, self.vertices[tv[0]].v * _scale),
                (self.vertices[tv[1]].u * _scale, self.vertices[tv[1]].v * _scale),
                (self.vertices[tv[2]].u * _scale, self.vertices[tv[2]].v * _scale),
            ]
            draw.polygon(pts, fill=(255, 0, 0, 46))
            if total > 100 and _idx % _step == 0:
                _report_progress(progress_callback, _idx / total)
        image = image.resize((math.ceil(zone.width), math.ceil(zone.height)), Image.Resampling.NEAREST)
        self.overlay_cache[zone_id] = image
        return image

    def walkable_dots_image(
        self,
        zone_id: int,
        max_points: int = 60000,
        display_zone_id: str = "",
        progress_callback=None,
    ):
        cache_key = (zone_id, max_points)
        if cache_key in self.dots_cache:
            return self.dots_cache[cache_key]
        try:
            from PIL import Image, ImageDraw
        except ImportError:
            return None

        zone = self.zone_by_id.get(zone_id)
        if zone is None or zone.width <= 0 or zone.height <= 0:
            return None
        points = self.walkable_preview_points(zone_id, max_points, display_zone_id)
        if not points:
            return None
        _scale = 0.5
        _w = math.ceil(zone.width * _scale)
        _h = math.ceil(zone.height * _scale)
        image = Image.new("RGBA", (_w, _h), (0, 0, 0, 0))
        draw = ImageDraw.Draw(image)
        total = len(points)
        _step = max(1, total // 50) if total > 100 else 1
        for _idx, (x, y) in enumerate(points):
            draw.ellipse(
                ((x - 1.5) * _scale, (y - 1.5) * _scale, (x + 1.5) * _scale, (y + 1.5) * _scale),
                fill=(100, 100, 100, 200),
            )
            if total > 100 and _idx % _step == 0:
                _report_progress(progress_callback, _idx / total)
        image = image.resize((math.ceil(zone.width), math.ceil(zone.height)), Image.Resampling.NEAREST)
        self.dots_cache[cache_key] = image
        return image

    def find_route(
        self,
        zone_id: int,
        start: tuple[float, float],
        goal: tuple[float, float],
        snap_radius: float,
    ) -> _BaseNavRoute:
        start_snap = self.snap(zone_id, start, snap_radius)
        if start_snap is None:
            raise ValueError("起点附近没有可走三角面")
        goal_snap = self.snap(zone_id, goal, snap_radius)
        if goal_snap is None:
            raise ValueError("终点附近没有可走三角面")

        triangle_path, cost = self._astar(start_snap.triangle, goal_snap.triangle)
        if not triangle_path:
            raise ValueError("A* 未找到可达路径")
        points, segment_breaks = self._triangle_path_points(triangle_path, start_snap.point, goal_snap.point)
        return _BaseNavRoute(points=points, triangles=triangle_path, cost=cost, segment_breaks=segment_breaks)

    def snap(self, zone_id: int, point: tuple[float, float], radius: float) -> _SnapResult | None:
        zone = self.zone_by_id.get(zone_id)
        if zone is None or zone.triangle_count <= 0:
            return None
        query_radius = max(0.0, radius)
        candidates = self._candidate_triangles(zone_id, point, query_radius)
        if not candidates and query_radius < self.bin_size:
            candidates = self._candidate_triangles(zone_id, point, self.bin_size)
        best: _SnapResult | None = None
        for triangle_index in candidates:
            triangle_vertices = self._triangle_points(triangle_index)
            if _point_in_triangle(point, *triangle_vertices):
                # 命中即最优(距离 0),提前返回;与 C++ BaseNavPlanner::snap 行为一致,
                # 避免在细碎三角形堆叠的 bin 中线性扫遍上万个候选。
                return _SnapResult(triangle=triangle_index, point=point, distance=0.0)
            snapped = _closest_point_on_triangle(point, triangle_vertices)
            distance = math.hypot(snapped[0] - point[0], snapped[1] - point[1])
            if distance > query_radius:
                continue
            if best is None or distance < best.distance:
                best = _SnapResult(triangle=triangle_index, point=snapped, distance=distance)
        return best

    def is_segment_walkable(self, zone_id: int, a: tuple[float, float], b: tuple[float, float]) -> bool:
        # Navmesh raycast: True when the straight segment a->b stays on walkable mesh within zone_id.
        # Marches across shared triangle edges; fails closed on any ambiguity. Mirrors the C++
        # BaseNavPlanner::isSegmentWalkable so preview matches runtime route simplification.
        if self.zone_by_id.get(zone_id) is None:
            return False
        if math.hypot(b[0] - a[0], b[1] - a[1]) < SEGMENT_WALK_EPSILON:
            return True
        start = self.snap(zone_id, a, SEGMENT_WALK_SNAP_RADIUS)
        if start is None:
            return False  # origin not on the mesh; fail closed
        triangles = self.triangles
        current = start.triangle
        # 直线 a->b 穿越的三角形其交点参数 t 单调递增;记录进入当前三角形的 t,要求
        # 出边严格向前(t > entry_t)。这把因重叠/共面三角形导致的横向打转直接截断,
        # 让不可达的射线在墙处快速失败,而非游走整张网格(原先会跑满 max_steps)。
        # 仅改变 False 路径的速度,合法可达射线始终向前推进,判定结果不变。
        # 起点 a 常落在 portal 共享边上,起始三角形的合法出边参数可能 t≈0,故 entry_t
        # 初值必须 < 0(而非 0),否则首个三角形的真实出边会被单调过滤误杀 -> 误判不可达。
        # 单调约束从第二个三角形起生效(那时 entry_t=best_t>0),防游走的提速效果不变。
        entry_t = -1.0
        max_steps = len(triangles) + 4
        for _step in range(max_steps):
            points = self._triangle_points(current)
            if _point_in_triangle(b, points[0], points[1], points[2]):
                return True
            triangle = triangles[current]
            best_t = entry_t
            exit_va = 0
            exit_vb = 0
            has_exit = False
            for edge in range(3):
                ok, t, s = _segment_intersect_params(a, b, points[edge], points[(edge + 1) % 3])
                if not ok:
                    continue
                if t <= entry_t + SEGMENT_WALK_EPSILON or t > 1.0 + SEGMENT_WALK_EPSILON:
                    continue
                if s < -SEGMENT_WALK_EPSILON or s > 1.0 + SEGMENT_WALK_EPSILON:
                    continue
                if t > best_t:
                    best_t = t
                    exit_va = triangle.vertices[edge]
                    exit_vb = triangle.vertices[(edge + 1) % 3]
                    has_exit = True
            if not has_exit:
                return False  # numeric edge case; fail closed
            next_triangle = -1
            for neighbor in triangle.neighbors:
                if neighbor < 0:
                    continue
                candidate = triangles[neighbor].vertices
                if exit_va in candidate and exit_vb in candidate:
                    next_triangle = neighbor
                    break
            if next_triangle < 0 or next_triangle >= len(triangles) or self.triangle_zone[next_triangle] != zone_id:
                return False  # wall edge or zone boundary
            entry_t = best_t  # 进入下一三角形的参数,强制单调向前
            current = next_triangle
        return False

    def _build_index(self, progress_callback=None) -> None:
        zone_ranges = []
        for zone in self.zones:
            zone_ranges.append((zone.first_triangle, zone.first_triangle + zone.triangle_count, zone.zone_id))
        range_index = 0
        total_triangles = len(self.triangles)
        _bin_report_step = max(1, total_triangles // 50) if total_triangles > 100 else 1
        for triangle_index, _triangle in enumerate(self.triangles):
            while range_index + 1 < len(zone_ranges) and triangle_index >= zone_ranges[range_index][1]:
                range_index += 1
            if range_index < len(zone_ranges) and zone_ranges[range_index][0] <= triangle_index < zone_ranges[range_index][1]:
                zone_id = zone_ranges[range_index][2]
                self.triangle_zone[triangle_index] = zone_id
            else:
                zone_id = 0
            tv = self.triangles[triangle_index].vertices
            v0, v1, v2 = self.vertices[tv[0]], self.vertices[tv[1]], self.vertices[tv[2]]
            left = min(v0.u, v1.u, v2.u)
            top = min(v0.v, v1.v, v2.v)
            right = max(v0.u, v1.u, v2.u)
            bottom = max(v0.v, v1.v, v2.v)
            self.triangle_bounds.append((left, top, right, bottom))
            self.triangle_height[triangle_index] = (v0.height + v1.height + v2.height) / 3.0
            if zone_id == 0:
                continue
            bin_sx = int(left / self.bin_size)
            bin_ex = int(right / self.bin_size)
            bin_sy = int(top / self.bin_size)
            bin_ey = int(bottom / self.bin_size)
            for bin_x in range(bin_sx, bin_ex + 1):
                for bin_y in range(bin_sy, bin_ey + 1):
                    self.bins[(zone_id, bin_x, bin_y)].append(triangle_index)
            if total_triangles > 100 and triangle_index % _bin_report_step == 0:
                _report_progress(progress_callback, 0.36 + 0.17 * (triangle_index / total_triangles))
        _report_progress(progress_callback, 0.53)
        self._build_natural_components()
        _report_progress(progress_callback, 0.56)
        for source, target in self.links:
            if self._is_traversable_link(source, target):
                self.adjacency[source].append(target)
        _report_progress(progress_callback, 0.58)

    def _build_natural_components(self) -> None:
        self.natural_component = [-1] * len(self.triangles)
        self.natural_component_size = []
        for triangle_index in range(len(self.triangles)):
            if self.natural_component[triangle_index] >= 0:
                continue
            component_id = len(self.natural_component_size)
            self.natural_component[triangle_index] = component_id
            stack = [triangle_index]
            size = 0
            while stack:
                current = stack.pop()
                size += 1
                for neighbor in self.triangles[current].neighbors:
                    if (
                        neighbor < 0
                        or neighbor >= len(self.triangles)
                        or self.natural_component[neighbor] >= 0
                        or self.triangle_zone[neighbor] != self.triangle_zone[current]
                    ):
                        continue
                    self.natural_component[neighbor] = component_id
                    stack.append(neighbor)
            self.natural_component_size.append(size)

    def _is_traversable_link(self, source: int, target: int) -> bool:
        if (
            source < 0
            or source >= len(self.triangles)
            or target < 0
            or target >= len(self.triangles)
            or self.triangle_zone[source] == 0
            or self.triangle_zone[source] != self.triangle_zone[target]
        ):
            return False
        if target in self.triangles[source].neighbors:
            return True

        source_component = self.natural_component[source]
        target_component = self.natural_component[target]
        min_component_size = min(self.natural_component_size[source_component], self.natural_component_size[target_component])
        if min_component_size > SMALL_BRIDGE_COMPONENT_MAX_TRIANGLES:
            return True

        bridge_points = self._closest_edge_bridge_points(source, target)
        return bridge_points is not None and _point_distance(bridge_points[0], bridge_points[1]) <= SMALL_BRIDGE_MAX_GAP

    def _triangle_average_height(self, triangle_index: int) -> float:
        triangle = self.triangles[triangle_index]
        return sum(self.vertices[index].height for index in triangle.vertices) / 3.0

    def _candidate_triangles(self, zone_id: int, point: tuple[float, float], radius: float) -> list[int]:
        px, py = point
        seen: set[int] = set()
        result = []
        left = math.floor((px - radius) / self.bin_size)
        right = math.floor((px + radius) / self.bin_size)
        top = math.floor((py - radius) / self.bin_size)
        bottom = math.floor((py + radius) / self.bin_size)
        for bin_x in range(left, right + 1):
            for bin_y in range(top, bottom + 1):
                for triangle_index in self.bins.get((zone_id, bin_x, bin_y), []):
                    if triangle_index in seen:
                        continue
                    seen.add(triangle_index)
                    bounds = self.triangle_bounds[triangle_index]
                    if bounds[0] - radius <= px <= bounds[2] + radius and bounds[1] - radius <= py <= bounds[3] + radius:
                        result.append(triangle_index)
        return result

    def _triangle_points(
        self,
        triangle_index: int,
    ) -> tuple[tuple[float, float], tuple[float, float], tuple[float, float]]:
        triangle = self.triangles[triangle_index]
        return tuple((self.vertices[index].u, self.vertices[index].v) for index in triangle.vertices)  # type: ignore[return-value]

    def _point_on_mesh(self, zone_id: int, point: tuple[float, float]) -> bool:
        # 点是否落在该区任意三角形内。与 is_segment_walkable 的逐三角行进不同,本判定只看"点在不在网格上",
        # 不依赖共享边邻接,因此在重叠/碎片网格上不会像行进那样误判 -> 居中据此能看见真实走廊宽度。
        for triangle_index in self._candidate_triangles(zone_id, point, SEGMENT_WALK_SNAP_RADIUS):
            v0, v1, v2 = self._triangle_points(triangle_index)
            if _point_in_triangle(point, v0, v1, v2):
                return True
        return False

    def _ground_height_near_indexed(
        self,
        zone_id: int,
        point: tuple[float, float],
        reference: float | None,
    ) -> tuple[float | None, int]:
        # 机器人在 point 实际所站三角形的高度:取包含 point 的候选三角形里高度与 reference 最接近的那一个
        # (连续性 —— 跨共面重叠缝时据此选回脚下的路面,而非恰好重叠其上的 +9 墙瓦片)。reference 为
        # None(直线起点)时取最低高度 = 路面而非墙。无任何三角形包含 point 时返回 (None, -1)(离开网格)。
        # 同时回传被选中的三角形下标,供 _segment_height_walkable 缓存(相邻采样点常落在同一三角形内)。
        best: float | None = None
        best_index = -1
        for triangle_index in self._candidate_triangles(zone_id, point, SEGMENT_WALK_SNAP_RADIUS):
            v0, v1, v2 = self._triangle_points(triangle_index)
            if not _point_in_triangle(point, v0, v1, v2):
                continue
            height = self.triangle_height[triangle_index]
            if best is None:
                best = height
                best_index = triangle_index
            elif reference is None:
                if height < best:
                    best = height
                    best_index = triangle_index
            elif abs(height - reference) < abs(best - reference):
                best = height
                best_index = triangle_index
        return best, best_index

    def _segment_height_walkable(self, zone_id: int, a: tuple[float, float], b: tuple[float, float]) -> bool:
        # LOS 拉直(string-pull)用的走查 oracle,取代抽稀里的逐三角行进 march。沿直线 a->b 每
        # ROUTE_PULL_SAMPLE_STEP 采样,要求:每个采样点都落在该区网格上,且相邻采样间地面高度跳变
        # <= BRIDGE_MAX_HEIGHT_DELTA(running reference,非端点线性插值,故与捷径长度/坡度无关)。
        # 平坦同面路上的"假拐点"捷径全程保持平坦 -> 判定可走 -> 拐点被拉直走中线;绕 +9 墙的"真拐点"
        # 捷径会踩上墙 -> 高度跳变 -> 判定不可走 -> 该拐点(真直角)被保住。逐三角行进会在共面重叠缝处
        # 误判不可走、使抽稀永远拉不直(这正是路线贴边的根因),故拉直改用这条高度连续性判据。
        if self.zone_by_id.get(zone_id) is None:
            return False
        length = math.hypot(b[0] - a[0], b[1] - a[1])
        samples = max(1, int(length / ROUTE_PULL_SAMPLE_STEP))
        previous: float | None = None
        cached = -1  # 上一个采样点选中的三角形:相邻 0.5px 采样点多落在同一三角形内(实测命中 62-76%),
        #              先测它,命中即复用其高度。命中时其高度必等于 previous(上一步刚由它得出),到 reference
        #              的距离为 0 = 全局最小,与完整候选扫描结果完全等价,却省掉昂贵的 _candidate_triangles 扫描。
        for index in range(samples + 1):
            t = index / samples
            point = (a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t)
            if previous is not None and cached >= 0:
                cv0, cv1, cv2 = self._triangle_points(cached)
                if _point_in_triangle(point, cv0, cv1, cv2):
                    continue  # 命中缓存:高度 == previous(高度差 0,绝不触发突跳),直接续走下一采样
            height, cached = self._ground_height_near_indexed(zone_id, point, previous)
            if height is None:
                return False  # 采样点离开网格 -> 捷径不可走
            if previous is not None and abs(height - previous) > BRIDGE_MAX_HEIGHT_DELTA:
                return False  # 地面高度突跳(踩上墙/掉下台)-> 真拐点,捷径不可走
            previous = height
        return True

    def _astar(self, start: int, goal: int) -> tuple[list[int], float]:
        open_heap: list[tuple[float, int, int]] = []
        counter = 0
        parent: dict[int, int] = {}
        g_score: dict[int, float] = {start: 0.0}
        heapq.heappush(open_heap, (self._heuristic(start, goal), counter, start))
        closed: set[int] = set()

        while open_heap:
            _priority, _counter, current = heapq.heappop(open_heap)
            if current in closed:
                continue
            if current == goal:
                return self._reconstruct(parent, start, goal), g_score[current]
            closed.add(current)
            for neighbor in self.adjacency[current]:
                if neighbor < 0 or self.triangle_zone[neighbor] != self.triangle_zone[current]:
                    continue
                step = self._transition_cost(current, neighbor)
                tentative = g_score[current] + step
                if tentative >= g_score.get(neighbor, math.inf):
                    continue
                parent[neighbor] = current
                g_score[neighbor] = tentative
                counter += 1
                heapq.heappush(open_heap, (tentative + self._heuristic(neighbor, goal), counter, neighbor))
        return [], math.inf

    def _heuristic(self, lhs: int, rhs: int) -> float:
        ax, ay = self.triangles[lhs].center
        bx, by = self.triangles[rhs].center
        return math.hypot(ax - bx, ay - by)

    def _transition_cost(self, lhs: int, rhs: int) -> float:
        lhs_center = self.triangles[lhs].center
        rhs_center = self.triangles[rhs].center
        midpoint = self._shared_edge_midpoint(lhs, rhs)
        if midpoint is not None:
            return _point_distance(lhs_center, midpoint) + _point_distance(midpoint, rhs_center)
        bridge_points = self._closest_edge_bridge_points(lhs, rhs)
        height_delta = abs(self.triangle_height[lhs] - self.triangle_height[rhs])
        if height_delta > BRIDGE_MAX_HEIGHT_DELTA:
            return math.inf
        if bridge_points is None:
            return self._heuristic(lhs, rhs) + BRIDGE_FIXED_COST + height_delta * BRIDGE_HEIGHT_COST_FACTOR
        gap = _point_distance(bridge_points[0], bridge_points[1])
        return (
            _point_distance(lhs_center, bridge_points[0])
            + gap
            + _point_distance(bridge_points[1], rhs_center)
            + BRIDGE_FIXED_COST
            + gap * BRIDGE_GAP_COST_FACTOR
            + height_delta * BRIDGE_HEIGHT_COST_FACTOR
        )

    @staticmethod
    def _reconstruct(parent: dict[int, int], start: int, goal: int) -> list[int]:
        path = [goal]
        cursor = goal
        while cursor != start:
            if cursor not in parent:
                return []
            cursor = parent[cursor]
            path.append(cursor)
        path.reverse()
        return path

    def _triangle_path_points(
        self,
        triangle_path: list[int],
        start: tuple[float, float],
        goal: tuple[float, float],
    ) -> tuple[list[tuple[float, float]], list[int]]:
        if len(triangle_path) <= 1:
            return _dedupe_points([start, goal]), []
        points = [start]
        segment_breaks = []
        for lhs, rhs in zip(triangle_path, triangle_path[1:]):
            midpoint = self._shared_edge_midpoint(lhs, rhs)
            if midpoint is not None:
                points.append(midpoint)
                continue
            bridge_points = self._closest_edge_bridge_points(lhs, rhs)
            if bridge_points is not None:
                points.append(bridge_points[0])
                segment_breaks.append(len(points))
                points.append(bridge_points[1])
        points.append(goal)
        # The corridor is single-zone (A* never crosses zones), so the first triangle's zone drives
        # the walkability check that keeps simplification from cutting a corner through a wall.
        zone_id = self.triangle_zone[triangle_path[0]] if triangle_path else 0
        deduped_points, deduped_breaks = _dedupe_points_with_breaks(points, segment_breaks)
        simplified_points, simplified_breaks = _remove_collinear_with_breaks(deduped_points, deduped_breaks)
        # LOS 拉直用"运行参考高度"oracle(不是 march):march 只走边邻接,在共面重叠缝处会误拒直捷径、
        # 留住 portal 中点锯齿;高度 oracle 沿捷径按 ROUTE_PULL_SAMPLE_STEP 采样,要求处处在网格上且地面
        # 高度不跳变 -> 开阔锯齿被拉直走中线、绕 +9 墙的真拐角因踩墙高度跳变而留住直角。
        height_walkable = lambda segment_a, segment_b: self._segment_height_walkable(zone_id, segment_a, segment_b)
        pulled_points, pulled_breaks = _thin_route_points_with_breaks(
            simplified_points, simplified_breaks, is_segment_walkable=height_walkable
        )
        # 拉直输出是"拐角到拐角"、中间无内部点;先加密恢复内部采样点,结构保持式居中才有"直段"可整体平移。
        densified_points, densified_breaks = _densify_route_points_with_breaks(pulled_points, pulled_breaks)
        # 居中:在结构性拐角处切分直段,仅把够直的直段整体平移到走廊中线、并在拐角延长线交点处重连 ->
        # 既居中又精确保住真直角(不抹圆、不锯齿)。墙距/校验用点包含式(march 在重叠/碎片网格上会低估
        # 余量、误拒候选,使居中失效)。
        on_mesh = lambda point: self._point_on_mesh(zone_id, point)
        centered_points, centered_breaks = _center_route_points_with_breaks(
            densified_points, densified_breaks, point_on_mesh=on_mesh
        )
        # 居中会把拐角搬到直段延长线交点上,可能拉出超过 ROUTE_MAX_POINT_DISTANCE 的长边;末尾再加密一次,
        # 保证最终点距 <= ROUTE_MAX_POINT_DISTANCE(机器人沿密集折线行走)。
        return _densify_route_points_with_breaks(centered_points, centered_breaks)

    def _shared_edge_portal(self, lhs: int, rhs: int) -> tuple[tuple[float, float], tuple[float, float]] | None:
        lhs_vertices = set(self.triangles[lhs].vertices)
        shared = [index for index in self.triangles[rhs].vertices if index in lhs_vertices]
        if len(shared) != 2:
            return self._overlapping_edge_portal(lhs, rhs)
        a = self.vertices[shared[0]]
        b = self.vertices[shared[1]]
        return (a.u, a.v), (b.u, b.v)

    def _shared_edge_midpoint(self, lhs: int, rhs: int) -> tuple[float, float] | None:
        portal = self._shared_edge_portal(lhs, rhs)
        if portal is None:
            return None
        return (portal[0][0] + portal[1][0]) * 0.5, (portal[0][1] + portal[1][1]) * 0.5

    def _overlapping_edge_portal(self, lhs: int, rhs: int) -> tuple[tuple[float, float], tuple[float, float]] | None:
        lhs_points = self._triangle_points(lhs)
        rhs_points = self._triangle_points(rhs)
        lhs_edges = ((lhs_points[0], lhs_points[1]), (lhs_points[1], lhs_points[2]), (lhs_points[2], lhs_points[0]))
        rhs_edges = ((rhs_points[0], rhs_points[1]), (rhs_points[1], rhs_points[2]), (rhs_points[2], rhs_points[0]))
        for lhs_a, lhs_b in lhs_edges:
            for rhs_a, rhs_b in rhs_edges:
                portal = _overlapping_segment_portal(lhs_a, lhs_b, rhs_a, rhs_b)
                if portal is not None:
                    return portal
        return None

    def _closest_edge_bridge_points(self, lhs: int, rhs: int) -> tuple[tuple[float, float], tuple[float, float]] | None:
        lhs_points = self._triangle_points(lhs)
        rhs_points = self._triangle_points(rhs)
        lhs_edges = ((lhs_points[0], lhs_points[1]), (lhs_points[1], lhs_points[2]), (lhs_points[2], lhs_points[0]))
        rhs_edges = ((rhs_points[0], rhs_points[1]), (rhs_points[1], rhs_points[2]), (rhs_points[2], rhs_points[0]))
        best: tuple[float, tuple[float, float], tuple[float, float]] | None = None
        for lhs_edge in lhs_edges:
            for rhs_edge in rhs_edges:
                distance, lhs_point, rhs_point = _closest_segment_points(lhs_edge[0], lhs_edge[1], rhs_edge[0], rhs_edge[1])
                if best is None or distance < best[0]:
                    best = (distance, lhs_point, rhs_point)
        if best is None:
            return None
        return best[1], best[2]


def _read_basenav(path: Path, progress_callback=None) -> tuple[list[_BaseNavZone], list[_BaseNavVertex], list[_BaseNavTriangle], list[tuple[int, int]]]:
    data = _read_basenav_bytes_mv(path)
    _report_progress(progress_callback, 0.03)
    if len(data) < HEADER_STRUCT.size:
        raise ValueError("file is smaller than BaseNav header")
    header_values = HEADER_STRUCT.unpack_from(data, 0)
    magic = header_values[0]
    version = header_values[1]
    if magic != MAGIC:
        raise ValueError("invalid BaseNav magic")
    if version != VERSION:
        raise ValueError("unsupported BaseNav version")

    zone_count = int(header_values[3])
    vertex_count = int(header_values[4])
    triangle_count = int(header_values[5])
    link_count = int(header_values[6])
    zone_table_offset = int(header_values[7])
    vertex_offset = int(header_values[8])
    triangle_offset = int(header_values[9])
    link_offset = int(header_values[10])
    build_hash = int(header_values[11])

    if zone_table_offset < HEADER_STRUCT.size:
        raise ValueError("invalid BaseNav zone offset")
    if vertex_offset < zone_table_offset:
        raise ValueError("invalid BaseNav vertex offset")
    if triangle_offset < vertex_offset:
        raise ValueError("invalid BaseNav triangle offset")
    if link_offset < triangle_offset:
        raise ValueError("invalid BaseNav link offset")
    if link_count <= 0:
        raise ValueError("BaseNav v2 requires link table")

    zone_table = data[zone_table_offset:vertex_offset]
    vertex_data = data[vertex_offset:vertex_offset + VERTEX_STRUCT.size * vertex_count]
    triangle_data = data[triangle_offset:triangle_offset + TRIANGLE_STRUCT.size * triangle_count]
    link_data = data[link_offset:link_offset + LINK_STRUCT.size * link_count]
    if _fnv64_parts((zone_table, vertex_data, triangle_data, link_data)) != build_hash:
        raise ValueError("BaseNav build hash mismatch")

    zones = []
    cursor = zone_table_offset
    for _index in range(zone_count):
        values = ZONE_STRUCT.unpack_from(data, offset=cursor)
        cursor += ZONE_STRUCT.size
        name_size = int(values[2])
        name = data[cursor:cursor + name_size].tobytes().decode("utf-8")
        cursor += name_size
        zones.append(
            _BaseNavZone(
                zone_id=int(values[0]),
                flags=int(values[1]),
                name=name,
                first_triangle=int(values[3]),
                triangle_count=int(values[4]),
                component_count=int(values[5]),
                width=float(values[6]),
                height=float(values[7]),
                transform=(float(values[8]), float(values[9]), float(values[10]), float(values[11])),
            )
        )
    if cursor != vertex_offset:
        raise ValueError("invalid BaseNav zone table size")

    _report_progress(progress_callback, 0.05)

    vertices: list = [None] * vertex_count
    cursor = vertex_offset
    _vertex_report_step = max(1, vertex_count // 50) if vertex_count > 100 else 1
    for _index in range(vertex_count):
        v0, v1, v2 = VERTEX_STRUCT.unpack_from(data, offset=cursor)
        cursor += VERTEX_STRUCT.size
        vertices[_index] = _BaseNavVertex(v0, v1, v2)
        if vertex_count > 100 and _index % _vertex_report_step == 0:
            _report_progress(progress_callback, 0.05 + 0.12 * (_index / vertex_count))

    _report_progress(progress_callback, 0.17)

    triangles: list = [None] * triangle_count
    cursor = triangle_offset
    _triangle_report_step = max(1, triangle_count // 50) if triangle_count > 100 else 1
    for _index in range(triangle_count):
        v_idx0, v_idx1, v_idx2, n0, n1, n2, comp_id, cx, cy = TRIANGLE_STRUCT.unpack_from(data, offset=cursor)
        cursor += TRIANGLE_STRUCT.size
        triangles[_index] = _BaseNavTriangle(
            vertices=(v_idx0, v_idx1, v_idx2),
            neighbors=(n0, n1, n2),
            component_id=comp_id,
            center=(cx, cy),
        )
        if triangle_count > 100 and _index % _triangle_report_step == 0:
            _report_progress(progress_callback, 0.17 + 0.18 * (_index / triangle_count))

    links = []
    cursor = link_offset
    for _index in range(link_count):
        source, target = LINK_STRUCT.unpack_from(data, offset=cursor)
        cursor += LINK_STRUCT.size
        if int(source) < triangle_count and int(target) < triangle_count:
            links.append((int(source), int(target)))
    _report_progress(progress_callback, 0.36)
    return zones, vertices, triangles, links


def _read_basenav_bytes_mv(path: Path) -> memoryview:
    if path.suffix.lower() != ".gz":
        return memoryview(path.read_bytes())
    with gzip.open(path, "rb") as handle:
        return memoryview(handle.read())


def _fnv64(data: bytes) -> int:
    return _fnv64_parts((data,))


def _fnv64_parts(parts) -> int:
    value = FNV_OFFSET
    for data in parts:
        for byte in data:
            value ^= byte
            value = (value * FNV_PRIME) & 0xFFFFFFFFFFFFFFFF
    return value


def _point_in_triangle(
    point: tuple[float, float],
    a: tuple[float, float],
    b: tuple[float, float],
    c: tuple[float, float],
    epsilon: float = 1e-5,
) -> bool:
    px, py = point
    ax, ay = a
    bx, by = b
    cx, cy = c
    d1 = (px - bx) * (ay - by) - (ax - bx) * (py - by)
    d2 = (px - cx) * (by - cy) - (bx - cx) * (py - cy)
    d3 = (px - ax) * (cy - ay) - (cx - ax) * (py - ay)
    has_neg = d1 < -epsilon or d2 < -epsilon or d3 < -epsilon
    has_pos = d1 > epsilon or d2 > epsilon or d3 > epsilon
    return not (has_neg and has_pos)


def _closest_point_on_triangle(
    point: tuple[float, float],
    vertices: tuple[tuple[float, float], tuple[float, float], tuple[float, float]],
) -> tuple[float, float]:
    if _point_in_triangle(point, vertices[0], vertices[1], vertices[2]):
        return point
    candidates = [
        _closest_point_on_segment(point, vertices[0], vertices[1]),
        _closest_point_on_segment(point, vertices[1], vertices[2]),
        _closest_point_on_segment(point, vertices[2], vertices[0]),
    ]
    return min(candidates, key=lambda item: math.hypot(item[0] - point[0], item[1] - point[1]))


def _closest_point_on_segment(
    point: tuple[float, float],
    a: tuple[float, float],
    b: tuple[float, float],
) -> tuple[float, float]:
    px, py = point
    ax, ay = a
    bx, by = b
    abx = bx - ax
    aby = by - ay
    denom = abx * abx + aby * aby
    if denom <= 1e-12:
        return a
    t = max(0.0, min(1.0, ((px - ax) * abx + (py - ay) * aby) / denom))
    return ax + abx * t, ay + aby * t


def _dedupe_points(points: list[tuple[float, float]], epsilon: float = 0.25) -> list[tuple[float, float]]:
    result: list[tuple[float, float]] = []
    for point in points:
        if result and math.hypot(point[0] - result[-1][0], point[1] - result[-1][1]) <= epsilon:
            continue
        result.append(point)
    return result


def _point_distance(lhs: tuple[float, float], rhs: tuple[float, float]) -> float:
    return math.hypot(lhs[0] - rhs[0], lhs[1] - rhs[1])


def _dedupe_points_with_breaks(
    points: list[tuple[float, float]],
    segment_breaks: list[int],
    epsilon: float = 0.25,
) -> tuple[list[tuple[float, float]], list[int]]:
    result: list[tuple[float, float]] = []
    mapped_breaks = []
    break_set = set(segment_breaks)
    for index, point in enumerate(points):
        if result and math.hypot(point[0] - result[-1][0], point[1] - result[-1][1]) <= epsilon:
            if index in break_set:
                mapped_breaks.append(len(result))
            continue
        if index in break_set:
            mapped_breaks.append(len(result))
        result.append(point)
    return result, sorted(set(mapped_breaks))


def _overlapping_segment_portal(
    a: tuple[float, float],
    b: tuple[float, float],
    c: tuple[float, float],
    d: tuple[float, float],
    epsilon: float = 1e-3,
) -> tuple[tuple[float, float], tuple[float, float]] | None:
    abx = b[0] - a[0]
    aby = b[1] - a[1]
    length_sq = abx * abx + aby * aby
    if length_sq <= epsilon * epsilon:
        return None
    length = math.sqrt(length_sq)

    def line_distance(point: tuple[float, float]) -> float:
        return abs(abx * (point[1] - a[1]) - aby * (point[0] - a[0])) / length

    if line_distance(c) > epsilon or line_distance(d) > epsilon:
        return None
    c_t = ((c[0] - a[0]) * abx + (c[1] - a[1]) * aby) / length_sq
    d_t = ((d[0] - a[0]) * abx + (d[1] - a[1]) * aby) / length_sq
    overlap_left = max(0.0, min(c_t, d_t))
    overlap_right = min(1.0, max(c_t, d_t))
    if overlap_right - overlap_left <= epsilon:
        return None
    return (
        (a[0] + abx * overlap_left, a[1] + aby * overlap_left),
        (a[0] + abx * overlap_right, a[1] + aby * overlap_right),
    )


def _closest_segment_points(
    a: tuple[float, float],
    b: tuple[float, float],
    c: tuple[float, float],
    d: tuple[float, float],
) -> tuple[float, tuple[float, float], tuple[float, float]]:
    candidates = []
    for point, edge in ((a, (c, d)), (b, (c, d)), (c, (a, b)), (d, (a, b))):
        snapped = _closest_point_on_segment(point, edge[0], edge[1])
        if point in (c, d):
            candidates.append((math.hypot(point[0] - snapped[0], point[1] - snapped[1]), snapped, point))
        else:
            candidates.append((math.hypot(point[0] - snapped[0], point[1] - snapped[1]), point, snapped))
    return min(candidates, key=lambda item: item[0])


def _remove_collinear_with_breaks(
    points: list[tuple[float, float]],
    segment_breaks: list[int],
    epsilon: float = 1e-3,
) -> tuple[list[tuple[float, float]], list[int]]:
    if len(points) <= 2:
        return points, segment_breaks
    break_set = set(segment_breaks)
    result = [points[0]]
    mapped_breaks = []
    for index in range(1, len(points) - 1):
        if index in break_set:
            mapped_breaks.append(len(result))
            result.append(points[index])
            continue
        ax, ay = result[-1]
        bx, by = points[index]
        cx, cy = points[index + 1]
        area = abs((bx - ax) * (cy - ay) - (by - ay) * (cx - ax))
        length = math.hypot(cx - ax, cy - ay)
        if length > epsilon and area / length <= epsilon:
            continue
        result.append(points[index])
    result.append(points[-1])
    return result, sorted(set(mapped_breaks))


def _thin_route_points_with_breaks(
    points: list[tuple[float, float]],
    segment_breaks: list[int],
    is_segment_walkable=None,
) -> tuple[list[tuple[float, float]], list[int]]:
    # LOS 拉直(string-pull):逐 bridge 段把 portal 中点锯齿沿走廊拉直,只在 oracle 判定捷径不可走处
    # (真拐角)留点。取代旧的 RDP+min-distance+march 修复——RDP 是纯几何会留住锯齿,march 又在共面
    # 重叠缝处误拒直捷径,二者叠加正是"路线贴边走不直"的根因(详见 _thin_continuous_segment)。
    if len(points) <= 2:
        return points, segment_breaks
    valid_breaks = sorted(index for index in set(segment_breaks) if 0 < index < len(points))
    segment_starts = [0, *valid_breaks]
    segment_ends = [*valid_breaks, len(points)]
    result: list[tuple[float, float]] = []
    mapped_breaks: list[int] = []
    for segment_index, (start, end) in enumerate(zip(segment_starts, segment_ends)):
        if segment_index > 0:
            mapped_breaks.append(len(result))
        kept_indices = _thin_continuous_segment(points, start, end, is_segment_walkable)
        result.extend(points[index] for index in kept_indices)
    return result, sorted(set(mapped_breaks))


def _densify_route_points_with_breaks(
    points: list[tuple[float, float]],
    segment_breaks: list[int],
    max_distance: float = ROUTE_MAX_POINT_DISTANCE,
) -> tuple[list[tuple[float, float]], list[int]]:
    if len(points) <= 1:
        return points, segment_breaks
    valid_breaks = sorted(index for index in set(segment_breaks) if 0 < index < len(points))
    segment_starts = [0, *valid_breaks]
    segment_ends = [*valid_breaks, len(points)]
    result: list[tuple[float, float]] = []
    mapped_breaks: list[int] = []
    for segment_index, (start, end) in enumerate(zip(segment_starts, segment_ends)):
        if segment_index > 0:
            mapped_breaks.append(len(result))
        result.extend(_densify_continuous_segment(points, start, end, max_distance))
    return result, sorted(set(mapped_breaks))


def _max_offset_on_mesh(
    origin: tuple[float, float],
    dir_x: float,
    dir_y: float,
    cap: float,
    point_on_mesh,
    step: float = CENTER_PROBE_STEP,
) -> float:
    # 点包含式墙距:从 origin 沿 dir 以 step 逐步外推,返回仍落在网格内的最大偏移 r∈[0, cap]。
    # 不走逐三角行进,故不会被重叠/碎片网格的邻接断裂误判 -> 还原走廊真实横向余量(march 会严重低估)。
    distance = step
    last = 0.0
    while distance <= cap:
        if not point_on_mesh((origin[0] + dir_x * distance, origin[1] + dir_y * distance)):
            return last
        last = distance
        distance += step
    return cap


def _segment_on_mesh(
    a: tuple[float, float],
    b: tuple[float, float],
    point_on_mesh,
    step: float = CENTER_VALIDATE_STEP,
) -> bool:
    # 点包含式连段校验:沿 a->b 以 step 采样,全部采样点都在网格内才算可走。
    # 走廊内真实墙体远宽于亚像素步长,故采样不会跨过真墙;而 march 在重叠网格上会误拒,反而扼杀居中。
    sample_count = max(1, int(math.hypot(b[0] - a[0], b[1] - a[1]) / step))
    for index in range(sample_count + 1):
        t = index / sample_count
        if not point_on_mesh((a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t)):
            return False
    return True


def _route_turn_angle_deg(
    a: tuple[float, float],
    b: tuple[float, float],
    c: tuple[float, float],
) -> float:
    # a->b->c 在 b 处的转角(度):0=直行,90=直角,180=掉头。
    ux, uy = b[0] - a[0], b[1] - a[1]
    vx, vy = c[0] - b[0], c[1] - b[1]
    length_u = math.hypot(ux, uy)
    length_v = math.hypot(vx, vy)
    if length_u < 1e-9 or length_v < 1e-9:
        return 0.0
    cos_value = max(-1.0, min(1.0, (ux * vx + uy * vy) / (length_u * length_v)))
    return math.degrees(math.acos(cos_value))


def _route_turn_back_count(points: list[tuple[float, float]]) -> int:
    # 折返点数(相邻两段方向夹角 > 90°,点积 < 0):衡量"锯齿/绕圈"程度,居中绝不能让它变多。
    count = 0
    for index in range(1, len(points) - 1):
        ax = points[index][0] - points[index - 1][0]
        ay = points[index][1] - points[index - 1][1]
        bx = points[index + 1][0] - points[index][0]
        by = points[index + 1][1] - points[index][1]
        if ax * ax + ay * ay < 1e-9 or bx * bx + by * by < 1e-9:
            continue
        if ax * bx + ay * by < 0:
            count += 1
    return count


def _perpendicular_distance(
    point: tuple[float, float],
    line_a: tuple[float, float],
    line_b: tuple[float, float],
) -> float:
    # point 到直线 line_a->line_b 的垂距;线退化时取到 line_a 的距离。
    dx = line_b[0] - line_a[0]
    dy = line_b[1] - line_a[1]
    length = math.hypot(dx, dy)
    if length < 1e-9:
        return math.hypot(point[0] - line_a[0], point[1] - line_a[1])
    return abs((point[0] - line_a[0]) * (-dy) + (point[1] - line_a[1]) * dx) / length


def _line_intersection(
    base_a: tuple[float, float],
    dir_a: tuple[float, float],
    base_b: tuple[float, float],
    dir_b: tuple[float, float],
) -> tuple[float, float] | None:
    # 两直线 (base_a + t*dir_a) 与 (base_b + s*dir_b) 的交点;近平行时返回 None。
    cross = dir_a[0] * dir_b[1] - dir_a[1] * dir_b[0]
    if abs(cross) < 1e-9:
        return None
    rx = base_b[0] - base_a[0]
    ry = base_b[1] - base_a[1]
    t = (rx * dir_b[1] - ry * dir_b[0]) / cross
    return (base_a[0] + dir_a[0] * t, base_a[1] + dir_a[1] * t)


def _perpendicular_foot(
    point: tuple[float, float],
    base: tuple[float, float],
    direction: tuple[float, float],
) -> tuple[float, float]:
    # point 在直线 (base + t*direction) 上的垂足(direction 为单位向量)。
    t = (point[0] - base[0]) * direction[0] + (point[1] - base[1]) * direction[1]
    return (base[0] + direction[0] * t, base[1] + direction[1] * t)


def _center_continuous_segment(
    points: list[tuple[float, float]],
    start: int,
    end: int,
    probe_limit: float,
    max_shift: float,
    point_on_mesh,
) -> list[tuple[float, float]]:
    # 结构保持式居中(不做平滑,绝不抹圆真直角):
    #   1. 在结构性拐角(转角 >= ROUTE_CORNER_ANGLE_DEG)处把连续段切成若干"直段(run)";段两端固定。
    #   2. 仅当一个 run 足够"直"(内部点偏离首尾弦 <= ROUTE_RUN_STRAIGHT_TOL)且含内部点时,才把整段沿其
    #      法向"刚性平移"到走廊中线(平移量 = 段内逐点居中量的中位数,夹紧并按可行性缩放)。刚性平移保持
    #      直段仍直 -> 不会产生逐点独立居中那种高频锯齿(那是把折返数翻三倍、被用户否决的根因)。
    #   3. 拐角移到相邻两条已平移直段延长线的交点 -> 转角角度被精确保留 -> 真直角不被抹掉;只有整条
    #      路线的端点(S/G)真正固定。
    #   4. 碎片/窄走廊里的弯曲 run 不满足"直"判据 -> 原样不动(no-op)。
    #   5. 安全闸:整段仅在"全程仍在网格内 且 折返数不超过原值"时才接受,否则整段回退原路线 -> 最坏 no-op。
    original = list(points[start:end])
    point_count = len(original)
    if point_count < 3 or point_on_mesh is None:
        return original
    # 1. 结构性拐角(段内局部下标);两端点恒为拐角。
    corners = [0]
    for index in range(1, point_count - 1):
        if _route_turn_angle_deg(original[index - 1], original[index], original[index + 1]) >= ROUTE_CORNER_ANGLE_DEG:
            corners.append(index)
    corners.append(point_count - 1)
    corners = sorted(set(corners))
    # 2. 每个 run 的直线与刚性平移量。只有"直"的 run(含长度为 1 的单段)才拥有用于重连拐角的直线。
    runs: list[dict] = []
    for run_start, run_end in zip(corners, corners[1:]):
        dx = original[run_end][0] - original[run_start][0]
        dy = original[run_end][1] - original[run_start][1]
        length = math.hypot(dx, dy)
        run: dict = {"start": run_start, "end": run_end, "has_line": False, "shift": 0.0}
        if length < 1e-6:
            runs.append(run)
            continue
        unit_x, unit_y = dx / length, dy / length
        normal_x, normal_y = -unit_y, unit_x
        is_straight = all(
            _perpendicular_distance(original[j], original[run_start], original[run_end]) <= ROUTE_RUN_STRAIGHT_TOL
            for j in range(run_start + 1, run_end)
        )
        if not is_straight:
            runs.append(run)  # 弯曲 run:无直线、不平移
            continue
        run["has_line"] = True
        run["direction"] = (unit_x, unit_y)
        run["normal"] = (normal_x, normal_y)
        if run_end - run_start >= 2:  # 含可居中的内部点
            offsets = []
            for j in range(run_start + 1, run_end):
                clearance_plus = _max_offset_on_mesh(original[j], normal_x, normal_y, probe_limit, point_on_mesh)
                clearance_minus = _max_offset_on_mesh(original[j], -normal_x, -normal_y, probe_limit, point_on_mesh)
                offsets.append((clearance_plus - clearance_minus) * 0.5)
            offsets.sort()
            target_shift = max(-max_shift, min(max_shift, offsets[len(offsets) // 2]))
            chosen_shift = 0.0
            for scale in (1.0, 0.75, 0.5, 0.25):  # 平移量逐步收缩到全程可行为止
                if all(
                    point_on_mesh(
                        (original[j][0] + normal_x * target_shift * scale, original[j][1] + normal_y * target_shift * scale)
                    )
                    for j in range(run_start + 1, run_end)
                ):
                    chosen_shift = target_shift * scale
                    break
            run["shift"] = chosen_shift
            anchor = original[run_start + 1]  # 直线锚在 run 主体上,而非可能贴角的拐点
        else:
            anchor = original[run_start]  # 长度为 1 的 run:直线即该单段本身(shift=0)
        run["base"] = (anchor[0] + normal_x * run["shift"], anchor[1] + normal_y * run["shift"])
        runs.append(run)
    # 3. 重连内部拐角:移到相邻两条已平移直线的交点(转角角度被精确保留)。
    result = list(original)
    corner_move_limit = max_shift * ROUTE_CORNER_MOVE_FACTOR
    for corner_index in range(1, len(corners) - 1):
        ci = corners[corner_index]
        left = runs[corner_index - 1]
        right = runs[corner_index]
        candidate = None
        if left["has_line"] and right["has_line"]:
            candidate = _line_intersection(left["base"], left["direction"], right["base"], right["direction"])
        elif left["has_line"]:
            candidate = _perpendicular_foot(original[ci], left["base"], left["direction"])
        elif right["has_line"]:
            candidate = _perpendicular_foot(original[ci], right["base"], right["direction"])
        if (
            candidate is not None
            and point_on_mesh(candidate)
            and math.hypot(candidate[0] - original[ci][0], candidate[1] - original[ci][1]) <= corner_move_limit
        ):
            result[ci] = candidate
    # 4. 内部点:按所在 run 的平移量做刚性平移。
    for run in runs:
        if not run["has_line"] or run["shift"] == 0.0:
            continue
        normal_x, normal_y = run["normal"]
        shift = run["shift"]
        for j in range(run["start"] + 1, run["end"]):
            result[j] = (original[j][0] + normal_x * shift, original[j][1] + normal_y * shift)
    # 5. 安全闸:全程在网格内 且 不比原路线更锯齿,否则整段回退。
    stays_on_mesh = all(_segment_on_mesh(result[k], result[k + 1], point_on_mesh) for k in range(point_count - 1))
    if not stays_on_mesh or _route_turn_back_count(result) > _route_turn_back_count(original):
        return original
    return result


def _center_route_points_with_breaks(
    points: list[tuple[float, float]],
    segment_breaks: list[int],
    point_on_mesh=None,
) -> tuple[list[tuple[float, float]], list[int]]:
    if len(points) <= 2 or point_on_mesh is None:
        return points, segment_breaks
    valid_breaks = sorted(index for index in set(segment_breaks) if 0 < index < len(points))
    segment_starts = [0, *valid_breaks]
    segment_ends = [*valid_breaks, len(points)]
    result: list[tuple[float, float]] = []
    mapped_breaks: list[int] = []
    for segment_index, (start, end) in enumerate(zip(segment_starts, segment_ends)):
        if segment_index > 0:
            mapped_breaks.append(len(result))
        result.extend(
            _center_continuous_segment(
                points, start, end, ROUTE_CENTER_PROBE_LIMIT, ROUTE_CENTER_MAX_SHIFT, point_on_mesh
            )
        )
    return result, sorted(set(mapped_breaks))


def _densify_continuous_segment(
    points: list[tuple[float, float]],
    start: int,
    end: int,
    max_distance: float,
) -> list[tuple[float, float]]:
    if start >= end:
        return []
    safe_max_distance = max(max_distance, 0.25)
    result = [points[start]]
    for index in range(start + 1, end):
        from_point = points[index - 1]
        to_point = points[index]
        distance = _point_distance(from_point, to_point)
        if distance <= 1e-6:
            continue
        step_count = max(1, math.ceil(distance / safe_max_distance))
        for step in range(1, step_count):
            t = step / step_count
            result.append(
                (
                    from_point[0] + (to_point[0] - from_point[0]) * t,
                    from_point[1] + (to_point[1] - from_point[1]) * t,
                )
            )
        result.append(to_point)
    return result


def _thin_continuous_segment(
    points: list[tuple[float, float]],
    start: int,
    end: int,
    is_segment_walkable=None,
) -> list[int]:
    # 贪心 LOS 拉直(最远可达版):从锚点出发找"最远可直达点"并跳过去,只在真拐角处落点。
    # oracle 走"运行参考高度"——开阔共面锯齿(假拐角)的捷径保持贴地 → 可走 → 拉直走中线;绕 +9 墙的
    # 真拐角捷径会踩上墙 → 高度跳变 → 不可走 → 拐点留住直角。视线在重叠/碎片网格上可能非单调(个别
    # portal 中点外凸,挡住近处却不挡远处),故越过至多 ROUTE_PULL_MAX_SKIP 个连续不可达点继续探查;
    # 真拐角整条对臂都被墙挡住、远超此值,绝不会被误跨。相邻原始点天然可走,故捷径从 anchor+2 起测。
    if end - start <= 2 or is_segment_walkable is None:
        return list(range(start, end))
    kept = [start]
    anchor = start
    while anchor < end - 1:
        farthest = anchor + 1  # 至少前进一个:anchor->anchor+1 是单条原始边,必可走
        misses = 0
        probe = anchor + 2
        while probe < end and misses <= ROUTE_PULL_MAX_SKIP:
            # 捷径长度封顶:超过 ROUTE_PULL_MAX_REACH 不再延伸(性能上界,O(n·L) 的 L 被钳住);
            # 开阔直路因此被切成共线小段,加密会补回内部点、居中把它们当一条直段整体平移,形状不变。
            if _point_distance(points[anchor], points[probe]) > ROUTE_PULL_MAX_REACH:
                break
            if is_segment_walkable(points[anchor], points[probe]):
                farthest = probe
                misses = 0
            else:
                misses += 1
            probe += 1
        if farthest < end - 1:
            kept.append(farthest)
        anchor = farthest
    kept.append(end - 1)
    return kept


def _segment_intersect_params(
    a: tuple[float, float],
    b: tuple[float, float],
    c: tuple[float, float],
    d: tuple[float, float],
) -> tuple[bool, float, float]:
    # Intersection params of a->b with c->d; (False, ...) if (near) parallel. t is the fraction
    # along a->b, s along c->d.
    rx = b[0] - a[0]
    ry = b[1] - a[1]
    sx = d[0] - c[0]
    sy = d[1] - c[1]
    denom = rx * sy - ry * sx
    if abs(denom) < SEGMENT_PARALLEL_EPSILON:
        return False, 0.0, 0.0
    qpx = c[0] - a[0]
    qpy = c[1] - a[1]
    t = (qpx * sy - qpy * sx) / denom
    s = (qpx * ry - qpy * rx) / denom
    return True, t, s
