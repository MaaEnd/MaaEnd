from __future__ import annotations

import gzip
import heapq
import math
import struct
import threading
from array import array
from dataclasses import dataclass
from pathlib import Path

import numpy as np


MAGIC = b"BNAV"
VERSION = 3  # v3 appends a per-zone float `floor_y` to each zone record; v2 had none
VERSION_MIN = 2  # oldest on-disk version still accepted; v2 zone records lack floor_y -> FLOOR_Y_NONE
FNV_OFFSET = 14695981039346656037
FNV_PRIME = 1099511628211
# Sentinel floor height for tier zones whose dominant walkable floor is unknown (the two
# "…_Base" overview tiers, and any geometry zone). Anything below FLOOR_Y_VALID_MIN means
# "no floor", so the floor-aware snap/route/overlay degrade to the legacy floor-blind path.
FLOOR_Y_NONE = -1.0e30
FLOOR_Y_VALID_MIN = -1.0e29
# Height half-band (px == world-Y units) around a tier's baked floor_y. snap/route/overlay
# PREFER triangles within floor_y±FLOOR_BAND; off-band surfaces are a graceful fallback,
# never a hard gate — so floor_y only re-ranks, never fails snap to None / a route to empty.
FLOOR_BAND = 12.0
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

# BaseNavZone.flags bit0: zone is a tier overlay (zero triangles; its mesh lives in
# the parent geometry zone addressed by component_count; transform = tier_px→base_px).
TIER_FLAG = 0x0001

HEADER_STRUCT = struct.Struct("<4sHHIIIIQQQQQ")
ZONE_STRUCT = struct.Struct("<HHIIIIff4ff")  # v3: ...transform(4f) + floor_y(f)
ZONE_STRUCT_V2 = struct.Struct("<HHIIIIff4f")  # v2: legacy zone record without floor_y
VERTEX_STRUCT = struct.Struct("<fff")
TRIANGLE_STRUCT = struct.Struct("<IIIiiiIff")
LINK_STRUCT = struct.Struct("<II")

# numpy 解析用的紧凑(无对齐填充)dtype,与上面的 struct 字节布局一一对应。
# 顶点/三角形/链接表都是巨量等长记录,用 np.frombuffer 一次性矢量化解析,
# 取代逐元素 unpack_from 的 Python 循环。
VERTEX_DTYPE = np.dtype([("u", "<f4"), ("v", "<f4"), ("h", "<f4")])
TRIANGLE_DTYPE = np.dtype(
    [
        ("v0", "<u4"),
        ("v1", "<u4"),
        ("v2", "<u4"),
        ("n0", "<i4"),
        ("n1", "<i4"),
        ("n2", "<i4"),
        ("comp", "<u4"),
        ("cx", "<f4"),
        ("cy", "<f4"),
    ]
)
LINK_DTYPE = np.dtype([("s", "<u4"), ("t", "<u4")])


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
    floor_y: float = FLOOR_Y_NONE


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
    floor_y: float | None = None,
) -> PreviewRoute:
    del display_zone_id
    route = field.find_route(zone_id, start, goal, snap_radius, floor_y)
    return PreviewRoute(points=route.points, world_points=route.points, cells=[], segment_breaks=route.segment_breaks)


class _CSRAdjacency:
    """邻接表的紧凑 CSR 表示。

    `flat` 为按源三角形分组、组内严格保持原始 link 表顺序的目标数组(``array('i')``);
    `offsets[i]:offsets[i + 1]` 是源 ``i`` 的邻居切片。对外行为与 ``list[list[int]]`` 等价
    (``adjacency[i]`` 返回可迭代/可索引的整型序列),但避免了上千万个 Python list/int
    对象的构造,内存与构建都更省。组内顺序的保持对 A* 同代价时的 counter tie-break 至关重要。
    """

    __slots__ = ("flat", "offsets")

    def __init__(self, flat: array, offsets: list[int]) -> None:
        self.flat = flat
        self.offsets = offsets

    def __getitem__(self, index: int):
        return self.flat[self.offsets[index] : self.offsets[index + 1]]

    def __len__(self) -> int:
        return len(self.offsets) - 1


class _NavArrays:
    """``_read_basenav`` 解析出、供 ``_build_index`` 矢量化使用的临时 numpy 数组容器。

    索引建好后即整体释放(``BaseNavField`` 在 ``__init__`` 末尾置空)。
    """

    __slots__ = ("tri_v", "tri_n", "vu", "vv", "vh", "link_src", "link_tgt")

    def __init__(self, tri_v, tri_n, vu, vv, vh, link_src, link_tgt) -> None:
        self.tri_v = tri_v
        self.tri_n = tri_n
        self.vu = vu
        self.vv = vv
        self.vh = vh
        self.link_src = link_src
        self.link_tgt = link_tgt


class _DeferredVerifier:
    """把 FNV-64 完整性校验从加载关键路径挪到后台。

    FNV-1a 是逐字节串行递推(异或后乘,不可矢量化),在前台会平白增加约 9s 的加载等待。
    解析阶段只记下原始字节分片与期望哈希,地图显示后再由后台线程比对;不匹配时告警而非抛错
    (本工具仅用于预览,文件损坏概率极低,告警足以提示重新烘焙)。
    """

    __slots__ = ("_parts", "_expected", "_thread", "_lock", "result")

    def __init__(self, parts, expected: int) -> None:
        self._parts = parts
        self._expected = expected
        self._thread: threading.Thread | None = None
        self._lock = threading.Lock()  # 串行化前台/后台两条校验路径
        self.result: bool | None = None  # None=未校验, True=通过, False=不匹配

    def run(self) -> bool:
        # 幂等且线程安全:前台(verify_integrity)与后台(start_background)无论何序调用,
        # 都只真正计算一次。锁保证若一条路径正在算,另一条会等其算完并复用结果,
        # 不会重复计算,也不会在 _parts 已释放后再次访问它。
        with self._lock:
            if self.result is not None:
                return self.result
            parts, self._parts = self._parts, None  # 锁内取出并释放,后台不会与之争用
            # result 为 None 时 parts 必非空(二者仅在锁内成对更新),无需再判空
            actual = _fnv64_parts(parts)
            self.result = actual == self._expected
        if not self.result:
            print(f"[basenav] 警告: build hash 不匹配 (期望 {self._expected:016x}, 实际 {actual:016x})")
        return self.result

    def start_background(self) -> None:
        with self._lock:
            if self._thread is not None or self.result is not None:
                return
            self._thread = threading.Thread(target=self.run, name="basenav-verify", daemon=True)
            self._thread.start()


class BaseNavField:
    def __init__(self, path: Path, bin_size: float = INDEX_BIN_SIZE, progress_callback=None) -> None:
        self.path = path
        self.bin_size = bin_size
        self.zones, self.vertices, self.triangles, self._arrays, self._verifier = _read_basenav(
            path, progress_callback=progress_callback
        )
        self.zone_by_id = {zone.zone_id: zone for zone in self.zones}
        self.zone_by_name = {zone.name: zone for zone in self.zones}
        self.triangle_zone: list[int] = []
        self.triangle_bounds: list[tuple[float, float, float, float]] = []
        self.bins: dict[tuple[int, int, int], list[int]] = {}
        self.adjacency: _CSRAdjacency | None = None
        self.natural_component: list[int] = []
        self.natural_component_size: list[int] = []
        self.triangle_height: list[float] = []
        self.overlay_cache: dict[int, object] = {}
        self.dots_cache: dict[int, object] = {}
        self._build_index(progress_callback=progress_callback)
        self._arrays = None  # 释放矢量化临时数组,只留下索引结果

    def start_background_verify(self) -> None:
        """地图显示后调用:在后台线程异步校验 build hash(不阻塞交互)。"""
        self._verifier.start_background()

    def verify_integrity(self) -> bool:
        """同步执行 build hash 校验并返回是否通过(供测试/校验脚本使用)。"""
        return self._verifier.run()

    def zone_ids(self) -> list[int]:
        return [zone.zone_id for zone in self.zones]

    def is_tier(self, zone_id: int) -> bool:
        zone = self.zone_by_id.get(zone_id)
        return bool(zone is not None and zone.flags & TIER_FLAG)

    def floor_y_for(self, zone_id: int) -> float | None:
        # The zone's baked dominant-floor height, or None when unset (the "…_Base" overview
        # tiers and geometry zones). Callers pass this into snap/find_route to scope routing
        # to the floor the selected tier depicts; None keeps the floor-blind legacy path.
        zone = self.zone_by_id.get(zone_id)
        if zone is None or zone.floor_y <= FLOOR_Y_VALID_MIN:
            return None
        return zone.floor_y

    def geometry_zone_id(self, zone_id: int) -> int:
        # tier zones carry zero triangles (their mesh lives in the parent geometry
        # zone, addressed via component_count). Snap / A* / walkable-preview must run
        # against that parent; clicks are already in the parent's (base) pixel system.
        zone = self.zone_by_id.get(zone_id)
        if zone is not None and zone.flags & TIER_FLAG:
            return zone.component_count
        return zone_id

    def tier_to_base(self, zone_id: int, x: float, y: float) -> tuple[float, float]:
        # tier_px -> base_px via the baked affine (base = s*tier + t). Identity for
        # non-tier / unknown zones so callers can pass any zone_id unconditionally.
        zone = self.zone_by_id.get(zone_id)
        if zone is None or not (zone.flags & TIER_FLAG):
            return x, y
        sx, tx, sy, ty = zone.transform
        return sx * x + tx, sy * y + ty

    def base_to_tier(self, zone_id: int, x: float, y: float) -> tuple[float, float]:
        # base_px -> tier_px via the inverse affine (tier = (base - t) / s). Identity
        # for non-tier / unknown / degenerate zones.
        zone = self.zone_by_id.get(zone_id)
        if zone is None or not (zone.flags & TIER_FLAG):
            return x, y
        sx, tx, sy, ty = zone.transform
        if sx == 0.0 or sy == 0.0:
            return x, y
        return (x - tx) / sx, (y - ty) / sy

    def tier_zone_ids_for(self, parent_zone_id: int) -> list[int]:
        # Tiers whose parent geometry zone == parent_zone_id, identity ("…_Base")
        # first so the dropdown defaults to the whole-base view.
        tiers = [
            zone for zone in self.zones
            if zone.flags & TIER_FLAG and zone.component_count == parent_zone_id
        ]
        tiers.sort(key=lambda z: (z.transform[1] != 0.0 or z.transform[3] != 0.0, z.name))
        return [zone.zone_id for zone in tiers]

    def zone_choices_for_base(self, base_name: str) -> list[str]:
        # Right-hand "zone" dropdown content for the selected base底图: ONLY this
        # base's tiers (no cross-base mixing). Bases without tiers fall back to the
        # base itself so the dropdown is never empty and routing still works.
        base = self.zone_by_name.get(base_name)
        if base is None:
            return []
        tier_ids = self.tier_zone_ids_for(base.zone_id)
        if tier_ids:
            return [self.zone_label(zone_id) for zone_id in tier_ids]
        return [self.zone_label(base.zone_id)]

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
        if zone.flags & TIER_FLAG:
            image = self._tier_overlay_image(zone, Image, ImageDraw)
        else:
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

    def _tier_overlay_image(self, zone, Image, ImageDraw):
        # A tier has no triangles of its own; its mesh lives in the PARENT geometry zone
        # (base pixels). Render the walkable surface onto the tier's OWN template canvas
        # (tier pixels, sized to this tier zone), mapping each parent triangle inside the
        # tier footprint base_px -> tier_px via the inverse affine (tier = (base - t)/s).
        # So a selected tier shows its real template底图 with the可行走面 aligned on top —
        # the visual oracle for the baked affine — instead of boxing a region in the base.
        parent = self.zone_by_id.get(zone.component_count)
        if parent is None or parent.width <= 0 or parent.height <= 0:
            return None
        sx, tx, sy, ty = zone.transform
        if sx == 0.0 or sy == 0.0:
            return None
        # footprint in base_px (this tier's extent) — cheap cull of parent triangles
        fxs = (tx, sx * zone.width + tx)
        fys = (ty, sy * zone.height + ty)
        fx0, fx1 = min(fxs), max(fxs)
        fy0, fy1 = min(fys), max(fys)
        # Only paint THIS tier's floor. The parent base zone stacks every floor's mesh at
        # each (u,v); without the band filter the overlay shows other floors / walls bleeding
        # through the tier底图. Keep triangles within floor_y±FLOOR_BAND (height == world Y),
        # so the可行走面 rendering matches the floor the tier actually depicts. floor_y unset
        # (the "…_Base" overview tiers) falls back to painting all triangles, as before.
        floor_y = zone.floor_y
        has_floor = floor_y > FLOOR_Y_VALID_MIN
        image = Image.new("RGBA", (math.ceil(zone.width), math.ceil(zone.height)), (0, 0, 0, 0))
        draw = ImageDraw.Draw(image)
        start = parent.first_triangle
        end = start + parent.triangle_count
        for triangle_index in range(start, end):
            cx, cy = self.triangles[triangle_index].center
            if cx < fx0 or cx > fx1 or cy < fy0 or cy > fy1:
                continue
            if has_floor and abs(self.triangle_height[triangle_index] - floor_y) > FLOOR_BAND:
                continue
            points = [
                ((self.vertices[index].u - tx) / sx, (self.vertices[index].v - ty) / sy)
                for index in self.triangles[triangle_index].vertices
            ]
            draw.polygon(points, fill=(255, 0, 0, 46))
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
        floor_y: float | None = None,
    ) -> _BaseNavRoute:
        # floor_y (the active tier's baked dominant-floor height) only steers the endpoint
        # snap onto the correct floor; once both ends land on that floor's A* component the
        # corridor stays on it (coplanar-stitch links don't cross heights). A* itself is left
        # unfiltered so a legitimate on-floor ramp is never gated out — never fail closed.
        start_snap = self.snap(zone_id, start, snap_radius, floor_y)
        if start_snap is None:
            raise ValueError("起点附近没有可走三角面")
        goal_snap = self.snap(zone_id, goal, snap_radius, floor_y)
        if goal_snap is None:
            raise ValueError("终点附近没有可走三角面")

        triangle_path, cost = self._astar(start_snap.triangle, goal_snap.triangle)
        if not triangle_path:
            # A* 在破碎/重叠网格上会把"目标落在微小不连通分量里"或"恰好隔着一道被 BRIDGE_MAX_HEIGHT_DELTA
            # 拒绝的高度台阶"误报为不可达 —— 哪怕目标其实只是一小段平地之外。两端都已 snap 到网格上,故当两
            # 点间的直线全程落在可走网格、地面高度连续时,这就是目标可达的充分证明,按直连路径接受。对齐 C++
            # BaseNavPlanner::findPath 的直连可达性证明,使预览不再把这些"明明可走"的线路画成不可达。预览只做
            # 直连查询(无绕障/封堵三角形),故无需 C++ 那个 blocked 掩码分支。
            if self._segment_height_walkable(zone_id, start_snap.point, goal_snap.point):
                direct_triangles = [start_snap.triangle]
                if goal_snap.triangle != start_snap.triangle:
                    direct_triangles.append(goal_snap.triangle)
                direct_points = _dedupe_points([start_snap.point, goal_snap.point])
                direct_cost = math.hypot(
                    goal_snap.point[0] - start_snap.point[0],
                    goal_snap.point[1] - start_snap.point[1],
                )
                return _BaseNavRoute(
                    points=direct_points,
                    triangles=direct_triangles,
                    cost=direct_cost,
                    segment_breaks=[],
                )
            raise ValueError("A* 未找到可达路径")
        points, segment_breaks = self._triangle_path_points(triangle_path, start_snap.point, goal_snap.point)
        return _BaseNavRoute(points=points, triangles=triangle_path, cost=cost, segment_breaks=segment_breaks)

    def snap(
        self,
        zone_id: int,
        point: tuple[float, float],
        radius: float,
        floor_y: float | None = None,
    ) -> _SnapResult | None:
        zone = self.zone_by_id.get(zone_id)
        if zone is None or zone.triangle_count <= 0:
            return None
        query_radius = max(0.0, radius)
        candidates = self._candidate_triangles(zone_id, point, query_radius)
        if not candidates and query_radius < self.bin_size:
            candidates = self._candidate_triangles(zone_id, point, self.bin_size)
        if floor_y is None or floor_y <= FLOOR_Y_VALID_MIN:
            # Legacy floor-blind path — byte-identical to the pre-floor behavior so a base
            # view / unbaked zone keeps the 9 golden-hash routing parity exactly.
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
        # Floor-aware path: a click in a multi-floor base projects onto several STACKED
        # triangles (other floors / walls overlap this (u,v)). Rank so an in-band surface
        # (|height-floor_y| <= FLOOR_BAND) always beats an off-band one, then by snap
        # distance, then by height proximity to floor_y. The band is a PREFERENCE — if
        # nothing lands in-band we still return the nearest surface (never None), so
        # floor_y only re-ranks the snap target onto the correct floor, never gates it out.
        best_key: tuple[int, float, float] | None = None
        best_floor: _SnapResult | None = None
        for triangle_index in candidates:
            triangle_vertices = self._triangle_points(triangle_index)
            if _point_in_triangle(point, *triangle_vertices):
                snapped = point
                distance = 0.0
            else:
                snapped = _closest_point_on_triangle(point, triangle_vertices)
                distance = math.hypot(snapped[0] - point[0], snapped[1] - point[1])
                if distance > query_radius:
                    continue
            delta = abs(self.triangle_height[triangle_index] - floor_y)
            key = (0 if delta <= FLOOR_BAND else 1, distance, delta)
            if best_key is None or key < best_key:
                best_key = key
                best_floor = _SnapResult(triangle=triangle_index, point=snapped, distance=distance)
        return best_floor

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
        arrays = self._arrays
        total_triangles = len(self.triangles)
        bin_size = self.bin_size

        # --- 三角形所属区(triangle_zone)------------------------------------
        # 原逐三角形沿区间单调推进的语义,等价于按各区 [first, first+count) 切片直接赋 zone_id;
        # 未落入任何区的三角形保持 0。区在烘焙文件中是不重叠的有序分区,故结果逐位一致。
        tz = np.zeros(total_triangles, dtype=np.int64)
        for zone in self.zones:
            start = zone.first_triangle
            end = start + zone.triangle_count
            if end > start:
                tz[start:end] = zone.zone_id
        self.triangle_zone = tz.tolist()
        _report_progress(progress_callback, 0.40)

        # --- 包围盒(triangle_bounds)与平均高(triangle_height)---------------
        tri_v = arrays.tri_v
        vu, vv, vh = arrays.vu, arrays.vv, arrays.vh
        t0, t1, t2 = tri_v[:, 0], tri_v[:, 1], tri_v[:, 2]
        u0, u1, u2 = vu[t0], vu[t1], vu[t2]
        w0, w1, w2 = vv[t0], vv[t1], vv[t2]
        # float32 上取 min/max 与原 Python min()/max() 选出同一顶点值;.tolist() 再统一拓宽到 float64。
        left = np.minimum(np.minimum(u0, u1), u2)
        right = np.maximum(np.maximum(u0, u1), u2)
        top = np.minimum(np.minimum(w0, w1), w2)
        bottom = np.maximum(np.maximum(w0, w1), w2)
        bounds = np.empty((total_triangles, 4), dtype=np.float32)
        bounds[:, 0] = left
        bounds[:, 1] = top
        bounds[:, 2] = right
        bounds[:, 3] = bottom
        self.triangle_bounds = [tuple(b) for b in bounds.tolist()]
        # 高度按 (h0 + h1 + h2) / 3.0 在 float64 上左结合求和,与原 Python 表达式逐位一致。
        vh64 = vh.astype(np.float64)
        height = (vh64[t0] + vh64[t1] + vh64[t2]) / 3.0
        self.triangle_height = height.tolist()
        _report_progress(progress_callback, 0.46)

        # --- 空间分箱(bins)-------------------------------------------------
        # 仅对 zone != 0 的三角形建箱;每个三角形按其包围盒覆盖的 (bx, by) 网格逐一登记。
        # 用 float64 做除法+向零截断,复现原 int(left / bin_size) 的取整结果。
        valid = tz != 0
        bsx = (left.astype(np.float64) / bin_size).astype(np.int64)
        bex = (right.astype(np.float64) / bin_size).astype(np.int64)
        bsy = (top.astype(np.float64) / bin_size).astype(np.int64)
        bey = (bottom.astype(np.float64) / bin_size).astype(np.int64)
        single = valid & (bsx == bex) & (bsy == bey)
        multi = valid & ~((bsx == bex) & (bsy == bey))

        s_idx = np.nonzero(single)[0]
        zone_all = [tz[s_idx]]
        binx_all = [bsx[s_idx]]
        biny_all = [bsy[s_idx]]
        tri_all = [s_idx]
        m_idx = np.nonzero(multi)[0]
        if m_idx.size:
            ez: list[int] = []
            ex_: list[int] = []
            ey_: list[int] = []
            et: list[int] = []
            tz_m = tz[m_idx].tolist()
            sx_m = bsx[m_idx].tolist()
            ex_m = bex[m_idx].tolist()
            sy_m = bsy[m_idx].tolist()
            ey_m = bey[m_idx].tolist()
            ti_m = m_idx.tolist()
            for k in range(len(ti_m)):
                z = tz_m[k]
                ti = ti_m[k]
                for bx in range(sx_m[k], ex_m[k] + 1):
                    for by in range(sy_m[k], ey_m[k] + 1):
                        ez.append(z)
                        ex_.append(bx)
                        ey_.append(by)
                        et.append(ti)
            zone_all.append(np.array(ez, dtype=np.int64))
            binx_all.append(np.array(ex_, dtype=np.int64))
            biny_all.append(np.array(ey_, dtype=np.int64))
            tri_all.append(np.array(et, dtype=np.int64))
        za = np.concatenate(zone_all)
        xa = np.concatenate(binx_all)
        ya = np.concatenate(biny_all)
        ta = np.concatenate(tri_all)
        # 主键 zone、次键 bx、再 by、末键 tri,使每个 bin 内三角形按下标升序——与原插入顺序一致。
        order = np.lexsort((ta, ya, xa, za))
        za, xa, ya, ta = za[order], xa[order], ya[order], ta[order]
        bins: dict[tuple[int, int, int], list[int]] = {}
        nn = za.shape[0]
        if nn:
            boundary = np.empty(nn, dtype=bool)
            boundary[0] = True
            boundary[1:] = (za[1:] != za[:-1]) | (xa[1:] != xa[:-1]) | (ya[1:] != ya[:-1])
            starts = np.nonzero(boundary)[0].tolist()
            zal, xal, yal, tal = za.tolist(), xa.tolist(), ya.tolist(), ta.tolist()
            for gi in range(len(starts)):
                gs = starts[gi]
                ge = starts[gi + 1] if gi + 1 < len(starts) else nn
                bins[(zal[gs], xal[gs], yal[gs])] = tal[gs:ge]
        self.bins = bins
        _report_progress(progress_callback, 0.53)

        # --- 自然连通分量(natural components)------------------------------
        self._build_natural_components()
        _report_progress(progress_callback, 0.56)

        # --- 邻接表(adjacency, CSR)----------------------------------------
        self._build_adjacency_csr(tz)
        _report_progress(progress_callback, 0.58)

    def _build_adjacency_csr(self, tz: np.ndarray) -> None:
        """矢量化复现原 ``_is_traversable_link`` 过滤,产出 CSR 邻接表。

        每条 link(已按原表顺序过滤掉越界端点)接受当且仅当:同区且区非 0,且
        (互为网格邻接 或 两端所在自然分量较小者 > 阈值);否则落入残量,按"最近边间隙 <= 阈值"
        逐条判定桥接。最终按源稳定排序(保持组内原始 link 顺序)写成 CSR。
        """
        arrays = self._arrays
        link_src = arrays.link_src
        link_tgt = arrays.link_tgt
        total_triangles = len(self.triangles)
        if link_src.size == 0:
            self.adjacency = _CSRAdjacency(array("i"), [0] * (total_triangles + 1))
            return

        zs = tz[link_src]
        zt = tz[link_tgt]
        valid = (zs != 0) & (zs == zt)

        tri_n = arrays.tri_n
        is_neighbor = (
            (tri_n[link_src, 0] == link_tgt)
            | (tri_n[link_src, 1] == link_tgt)
            | (tri_n[link_src, 2] == link_tgt)
        )

        ncomp = np.asarray(self.natural_component, dtype=np.int64)
        ncsize = np.asarray(self.natural_component_size, dtype=np.int64)
        min_size = np.minimum(ncsize[ncomp[link_src]], ncsize[ncomp[link_tgt]])
        large = min_size > SMALL_BRIDGE_COMPONENT_MAX_TRIANGLES

        accept = valid & (is_neighbor | large)
        residual = valid & ~(is_neighbor | large)

        res_idx = np.nonzero(residual)[0]
        if res_idx.size:
            self._resolve_residual_bridges(res_idx, link_src, link_tgt, accept)

        acc_src = link_src[accept]
        acc_tgt = link_tgt[accept]
        # 稳定排序保持组内原始 link 顺序(A* 同代价 tie-break 依赖之)。
        sort_order = np.argsort(acc_src, kind="stable")
        sorted_tgt = acc_tgt[sort_order]
        counts = np.bincount(acc_src, minlength=total_triangles)[:total_triangles]
        offsets = np.zeros(total_triangles + 1, dtype=np.int64)
        np.cumsum(counts, out=offsets[1:])
        flat = array("i")
        flat.frombytes(np.ascontiguousarray(sorted_tgt, dtype=np.int32).tobytes())
        self.adjacency = _CSRAdjacency(flat, offsets.tolist())

    def _resolve_residual_bridges(self, res_idx, link_src, link_tgt, accept) -> None:
        """判定"非邻接且分量小"的残量 link 是否桥接(最近边间隙 <= 阈值)。

        逐 link 调用纯 Python 的 ``_closest_edge_bridge_points`` 是加载的真正瓶颈
        (本机约 8s / 36 万条),故先矢量化算出两三角形间最近的"顶点-对边"距离
        (18 个候选取最小,与原 9 边对×4 候选完全等价),用它对远离阈值的 link 直接判定;
        只有落在阈值 ±EPS 窄带内、float64 取整可能与原逐元素 ``hypot`` 不一致的极少数 link,
        才回退到原精确例程——从而既快又与原结果逐位等价(adj_hash 不变)。
        """
        gap = SMALL_BRIDGE_MAX_GAP
        eps = 1e-6
        arrays = self._arrays
        vu, vv, tri_v = arrays.vu, arrays.vv, arrays.tri_v
        s = link_src[res_idx]
        t = link_tgt[res_idx]
        sv = tri_v[s]  # (R, 3) 源三角形三顶点下标
        tv = tri_v[t]
        sx = vu[sv].astype(np.float64)  # (R, 3)
        sy = vv[sv].astype(np.float64)
        tx = vu[tv].astype(np.float64)
        ty = vv[tv].astype(np.float64)

        def seg2(px, py, ax, ay, bx, by):
            # 点 (px,py) 到线段 (a,b) 的最小平方距离,复刻 _closest_point_on_segment 的夹紧/退化语义。
            abx = bx - ax
            aby = by - ay
            denom = abx * abx + aby * aby
            with np.errstate(divide="ignore", invalid="ignore"):
                tt = ((px - ax) * abx + (py - ay) * aby) / denom
            tt = np.clip(tt, 0.0, 1.0)
            degen = denom <= 1e-12
            qx = np.where(degen, ax, ax + abx * tt)
            qy = np.where(degen, ay, ay + aby * tt)
            dx = px - qx
            dy = py - qy
            return dx * dx + dy * dy

        best = None
        for i in range(3):  # 源三顶点 -> 目标三条边
            for j in range(3):
                k = (j + 1) % 3
                d2 = seg2(sx[:, i], sy[:, i], tx[:, j], ty[:, j], tx[:, k], ty[:, k])
                best = d2 if best is None else np.minimum(best, d2)
        for i in range(3):  # 目标三顶点 -> 源三条边
            for j in range(3):
                k = (j + 1) % 3
                d2 = seg2(tx[:, i], ty[:, i], sx[:, j], sy[:, j], sx[:, k], sy[:, k])
                best = np.minimum(best, d2)
        dvec = np.sqrt(best)

        accept_fast = dvec <= gap - eps
        if accept_fast.any():
            accept[res_idx[accept_fast]] = True
        borderline = ~accept_fast & (dvec < gap + eps)
        b_idx = np.nonzero(borderline)[0]
        if b_idx.size:
            res_pos = res_idx[b_idx].tolist()
            bs = s[b_idx].tolist()
            bt = t[b_idx].tolist()
            extra: list[int] = []
            for k in range(len(bs)):
                bridge_points = self._closest_edge_bridge_points(bs[k], bt[k])
                if bridge_points is not None and _point_distance(bridge_points[0], bridge_points[1]) <= gap:
                    extra.append(res_pos[k])
            if extra:
                accept[np.array(extra, dtype=np.int64)] = True

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


def _read_basenav(
    path: Path, progress_callback=None
) -> tuple[list[_BaseNavZone], list[_BaseNavVertex], list[_BaseNavTriangle], _NavArrays, _DeferredVerifier]:
    data = _read_basenav_bytes_mv(path)
    _report_progress(progress_callback, 0.03)
    if len(data) < HEADER_STRUCT.size:
        raise ValueError("file is smaller than BaseNav header")
    header_values = HEADER_STRUCT.unpack_from(data, 0)
    magic = header_values[0]
    version = header_values[1]
    if magic != MAGIC:
        raise ValueError("invalid BaseNav magic")
    if not (VERSION_MIN <= version <= VERSION):
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
    # build hash 校验是逐字节串行的 FNV-1a,无法矢量化;挪到后台线程,显示后再核对(见 _DeferredVerifier)。
    verifier = _DeferredVerifier((zone_table, vertex_data, triangle_data, link_data), build_hash)

    # 区表含变长名字,数量很小,保留 Python 解析。
    zones = []
    cursor = zone_table_offset
    zone_struct = ZONE_STRUCT if version >= 3 else ZONE_STRUCT_V2
    for _index in range(zone_count):
        values = zone_struct.unpack_from(data, offset=cursor)
        cursor += zone_struct.size
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
                floor_y=float(values[12]) if version >= 3 else FLOOR_Y_NONE,
            )
        )
    if cursor != vertex_offset:
        raise ValueError("invalid BaseNav zone table size")
    _report_progress(progress_callback, 0.10)

    # 顶点:一次性 frombuffer 解析,再构建 dataclass 列表(供 routing 使用)。
    varr = np.frombuffer(vertex_data, dtype=VERTEX_DTYPE, count=vertex_count)
    vertices: list = [_BaseNavVertex(u, v, h) for (u, v, h) in varr.tolist()]
    vu = np.array(varr["u"])  # 拷贝成连续数组,脱离对解压缓冲区的视图依赖
    vv = np.array(varr["v"])
    vh = np.array(varr["h"])
    _report_progress(progress_callback, 0.20)

    # 三角形:frombuffer 解析,构建 dataclass 列表 + 索引建图所需的整型数组。
    tarr = np.frombuffer(triangle_data, dtype=TRIANGLE_DTYPE, count=triangle_count)
    triangles: list = [
        _BaseNavTriangle(vertices=(a, b, c), neighbors=(d, e, f), component_id=g, center=(cx, cy))
        for (a, b, c, d, e, f, g, cx, cy) in tarr.tolist()
    ]
    tri_v = np.empty((triangle_count, 3), dtype=np.int32)
    tri_v[:, 0] = tarr["v0"]
    tri_v[:, 1] = tarr["v1"]
    tri_v[:, 2] = tarr["v2"]
    tri_n = np.empty((triangle_count, 3), dtype=np.int32)
    tri_n[:, 0] = tarr["n0"]
    tri_n[:, 1] = tarr["n1"]
    tri_n[:, 2] = tarr["n2"]
    _report_progress(progress_callback, 0.33)

    # 链接:frombuffer 解析,过滤掉越界端点(保持原表顺序,A* tie-break 依赖之)。
    larr = np.frombuffer(link_data, dtype=LINK_DTYPE, count=link_count)
    ls = larr["s"].astype(np.int32)
    lt = larr["t"].astype(np.int32)
    link_mask = (ls < triangle_count) & (lt < triangle_count)
    link_src = np.ascontiguousarray(ls[link_mask])
    link_tgt = np.ascontiguousarray(lt[link_mask])
    _report_progress(progress_callback, 0.36)

    arrays = _NavArrays(tri_v, tri_n, vu, vv, vh, link_src, link_tgt)
    return zones, vertices, triangles, arrays, verifier


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
