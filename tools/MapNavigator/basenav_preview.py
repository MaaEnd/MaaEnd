from __future__ import annotations

import gzip
import heapq
import math
import struct
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
ROUTE_MIN_POINT_DISTANCE = 6.0
ROUTE_SIMPLIFY_EPSILON = 3.0
ROUTE_MAX_POINT_DISTANCE = 4.0
INDEX_BIN_SIZE = 96.0

HEADER_STRUCT = struct.Struct("<4sHHIIIIQQQQQ")
ZONE_STRUCT = struct.Struct("<HHIIIIff4f")
VERTEX_STRUCT = struct.Struct("<fff")
TRIANGLE_STRUCT = struct.Struct("<IIIiiiIff")
LINK_STRUCT = struct.Struct("<II")


@dataclass(frozen=True)
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


@dataclass(frozen=True)
class _BaseNavVertex:
    u: float
    v: float
    height: float


@dataclass(frozen=True)
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


def load_basenav_field(input_file: Path) -> BaseNavField:
    return BaseNavField(input_file)


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
    def __init__(self, path: Path, bin_size: float = INDEX_BIN_SIZE) -> None:
        self.path = path
        self.zones, self.vertices, self.triangles, self.links = _read_basenav(path)
        self.bin_size = bin_size
        self.zone_by_id = {zone.zone_id: zone for zone in self.zones}
        self.zone_by_name = {zone.name: zone for zone in self.zones}
        self.triangle_zone: list[int] = [0] * len(self.triangles)
        self.triangle_bounds: list[tuple[float, float, float, float]] = []
        self.bins: dict[tuple[int, int, int], list[int]] = {}
        self.adjacency: list[list[int]] = [[] for _triangle in self.triangles]
        self.triangle_height: list[float] = []
        self.overlay_cache = {}
        self._build_index()

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

    def overlay_image(self, zone_id: int):
        if zone_id in self.overlay_cache:
            return self.overlay_cache[zone_id]
        try:
            from PIL import Image, ImageDraw
        except ImportError:
            return None

        zone = self.zone_by_id.get(zone_id)
        if zone is None or zone.width <= 0 or zone.height <= 0:
            return None
        image = Image.new("RGBA", (math.ceil(zone.width), math.ceil(zone.height)), (0, 0, 0, 0))
        draw = ImageDraw.Draw(image)
        start = zone.first_triangle
        end = start + zone.triangle_count
        for triangle_index in range(start, end):
            points = [(self.vertices[index].u, self.vertices[index].v) for index in self.triangles[triangle_index].vertices]
            draw.polygon(points, fill=(255, 0, 0, 46))
        self.overlay_cache[zone_id] = image
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
            snapped = _closest_point_on_triangle(point, triangle_vertices)
            distance = math.hypot(snapped[0] - point[0], snapped[1] - point[1])
            if distance > query_radius and not _point_in_triangle(point, *triangle_vertices):
                continue
            if best is None or distance < best.distance:
                best = _SnapResult(triangle=triangle_index, point=snapped, distance=distance)
        return best

    def _build_index(self) -> None:
        zone_ranges = []
        for zone in self.zones:
            zone_ranges.append((zone.first_triangle, zone.first_triangle + zone.triangle_count, zone.zone_id))
        for source, target in self.links:
            if 0 <= source < len(self.adjacency) and 0 <= target < len(self.adjacency):
                self.adjacency[source].append(target)
        range_index = 0
        for triangle_index, _triangle in enumerate(self.triangles):
            while range_index + 1 < len(zone_ranges) and triangle_index >= zone_ranges[range_index][1]:
                range_index += 1
            if range_index < len(zone_ranges) and zone_ranges[range_index][0] <= triangle_index < zone_ranges[range_index][1]:
                zone_id = zone_ranges[range_index][2]
                self.triangle_zone[triangle_index] = zone_id
            else:
                zone_id = 0
            points = self._triangle_points(triangle_index)
            left = min(point[0] for point in points)
            top = min(point[1] for point in points)
            right = max(point[0] for point in points)
            bottom = max(point[1] for point in points)
            self.triangle_bounds.append((left, top, right, bottom))
            if zone_id == 0:
                continue
            for bin_x in range(math.floor(left / self.bin_size), math.floor(right / self.bin_size) + 1):
                for bin_y in range(math.floor(top / self.bin_size), math.floor(bottom / self.bin_size) + 1):
                    self.bins.setdefault((zone_id, bin_x, bin_y), []).append(triangle_index)
        self.triangle_height = [self._triangle_average_height(triangle_index) for triangle_index in range(len(self.triangles))]

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
        deduped_points, deduped_breaks = _dedupe_points_with_breaks(points, segment_breaks)
        simplified_points, simplified_breaks = _remove_collinear_with_breaks(deduped_points, deduped_breaks)
        thinned_points, thinned_breaks = _thin_route_points_with_breaks(simplified_points, simplified_breaks)
        return _densify_route_points_with_breaks(thinned_points, thinned_breaks)

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


def _read_basenav(path: Path) -> tuple[list[_BaseNavZone], list[_BaseNavVertex], list[_BaseNavTriangle], list[tuple[int, int]]]:
    data = _read_basenav_bytes(path)
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

    zone_table = _read_exact(data, zone_table_offset, vertex_offset - zone_table_offset)
    vertex_data = _read_exact(data, vertex_offset, VERTEX_STRUCT.size * vertex_count)
    triangle_data = _read_exact(data, triangle_offset, TRIANGLE_STRUCT.size * triangle_count)
    link_data = _read_exact(data, link_offset, LINK_STRUCT.size * link_count)
    if _fnv64_parts((zone_table, vertex_data, triangle_data, link_data)) != build_hash:
        raise ValueError("BaseNav build hash mismatch")

    zones = []
    cursor = zone_table_offset
    for _index in range(zone_count):
        values = ZONE_STRUCT.unpack(_read_exact(data, cursor, ZONE_STRUCT.size))
        cursor += ZONE_STRUCT.size
        name_size = int(values[2])
        name = _read_exact(data, cursor, name_size).decode("utf-8")
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

    vertices = []
    cursor = vertex_offset
    for _index in range(vertex_count):
        values = VERTEX_STRUCT.unpack(_read_exact(data, cursor, VERTEX_STRUCT.size))
        cursor += VERTEX_STRUCT.size
        vertices.append(_BaseNavVertex(float(values[0]), float(values[1]), float(values[2])))

    triangles = []
    cursor = triangle_offset
    for _index in range(triangle_count):
        values = TRIANGLE_STRUCT.unpack(_read_exact(data, cursor, TRIANGLE_STRUCT.size))
        cursor += TRIANGLE_STRUCT.size
        triangles.append(
            _BaseNavTriangle(
                vertices=(int(values[0]), int(values[1]), int(values[2])),
                neighbors=(int(values[3]), int(values[4]), int(values[5])),
                component_id=int(values[6]),
                center=(float(values[7]), float(values[8])),
            )
        )

    links = []
    cursor = link_offset
    for _index in range(link_count):
        source, target = LINK_STRUCT.unpack(_read_exact(data, cursor, LINK_STRUCT.size))
        cursor += LINK_STRUCT.size
        if int(source) < triangle_count and int(target) < triangle_count:
            links.append((int(source), int(target)))
    return zones, vertices, triangles, links


def _read_basenav_bytes(path: Path) -> bytes:
    if path.suffix.lower() != ".gz":
        return path.read_bytes()
    with gzip.open(path, "rb") as handle:
        return handle.read()


def _read_exact(data: bytes, offset: int, size: int) -> bytes:
    end = offset + size
    if end > len(data):
        raise ValueError("truncated basenav")
    return data[offset:end]


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
    min_distance: float = ROUTE_MIN_POINT_DISTANCE,
    simplify_epsilon: float = ROUTE_SIMPLIFY_EPSILON,
) -> tuple[list[tuple[float, float]], list[int]]:
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
        kept_indices = _thin_continuous_segment(points, start, end, min_distance, simplify_epsilon)
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
    min_distance: float,
    simplify_epsilon: float,
) -> list[int]:
    if end - start <= 2:
        return list(range(start, end))
    critical = set(_rdp_keep_indices(points, start, end - 1, simplify_epsilon))
    kept = [start]
    distance_since_kept = 0.0
    for index in range(start + 1, end - 1):
        distance_since_kept += math.hypot(points[index][0] - points[index - 1][0], points[index][1] - points[index - 1][1])
        if index in critical or distance_since_kept >= min_distance:
            kept.append(index)
            distance_since_kept = 0.0
    kept.append(end - 1)
    return kept


def _rdp_keep_indices(
    points: list[tuple[float, float]],
    start: int,
    end: int,
    epsilon: float,
) -> list[int]:
    keep = {start, end}
    stack = [(start, end)]
    while stack:
        left, right = stack.pop()
        best_distance = -1.0
        best_index = -1
        for index in range(left + 1, right):
            distance = _point_line_distance(points[index], points[left], points[right])
            if distance > best_distance:
                best_distance = distance
                best_index = index
        if best_distance > epsilon and best_index >= 0:
            keep.add(best_index)
            stack.append((left, best_index))
            stack.append((best_index, right))
    return sorted(keep)


def _point_line_distance(
    point: tuple[float, float],
    start: tuple[float, float],
    end: tuple[float, float],
) -> float:
    dx = end[0] - start[0]
    dy = end[1] - start[1]
    length_sq = dx * dx + dy * dy
    if length_sq <= 1e-9:
        return math.hypot(point[0] - start[0], point[1] - start[1])
    t = max(0.0, min(1.0, ((point[0] - start[0]) * dx + (point[1] - start[1]) * dy) / length_sq))
    snapped = (start[0] + dx * t, start[1] + dy * t)
    return math.hypot(point[0] - snapped[0], point[1] - snapped[1])
