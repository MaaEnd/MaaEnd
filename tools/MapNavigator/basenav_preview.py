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
ROUTE_BOUNDARY_DISTANCE_LIMIT = 8.0
ROUTE_BOUNDARY_PENALTY_FACTOR = 2.0
ROUTE_MIN_POINT_DISTANCE = 6.0
ROUTE_SIMPLIFY_EPSILON = 3.0
ROUTE_MAX_POINT_DISTANCE = 4.0
ROUTE_CENTER_PROBE_LIMIT = 32.0  # 走廊横截面单侧探测上限(像素),覆盖较宽折线桥面的半宽
ROUTE_CENTER_MAX_SHIFT = 24.0  # 直段整体横移的上限(像素),允许贴边 A* 路线回到桥面中线
ROUTE_CORNER_ANGLE_DEG = 35.0  # >= 此转角视为结构性拐角(真直角):在此切分直段,且绝不跨拐角居中(保住直角)
ROUTE_RUN_STRAIGHT_TOL = 1.6  # 直段判据:段内每个内部点偏离首尾弦 <= 此值(像素)才算"直",方可整体平移
ROUTE_CORNER_MOVE_FACTOR = 1.5  # 重连后拐角相对原位的最大位移 = ROUTE_CENTER_MAX_SHIFT * 此系数
ROUTE_SHORTCUT_MIN_CLEARANCE = 6.0  # 拉直捷径沿途任一侧至少保留的横向余量(像素)
ROUTE_SHORTCUT_CLEARANCE_PROBE_LIMIT = 8.0  # 捷径余量探测上限(像素)
ROUTE_SHORTCUT_CLEARANCE_PROBE_STEP = 1.0  # 捷径余量探测步进(像素)
ROUTE_SHORTCUT_CLEARANCE_SAMPLE_STEP = 4.0  # 捷径沿线采样步进(像素)
CENTER_PROBE_STEP = 0.5  # 点包含式墙距的步进(像素):沿法向逐步外推,直到离开网格
CENTER_VALIDATE_STEP = 0.5  # 点包含式连段校验的采样步进(像素):候选边按此步采样须全部在网格内
ROUTE_PULL_SAMPLE_STEP = 0.5  # LOS 拉直 oracle 的采样步进(像素):捷径按此步采样查"点在网格上 + 相邻采样地面高度不跳变"
ROUTE_PULL_MAX_SKIP = 8  # 拉直时越过非单调视线遮挡的最大连续不可达点数(重叠/碎片网格上个别 portal 中点会外凸,挡近不挡远);真拐角整条对臂都被墙挡住、远超此值,绝不会被误跨
ROUTE_PULL_MAX_REACH = 64.0  # 单条直线捷径的最大长度(像素):拉直的性能上界,把 O(n·L) 的 L 钳住;开阔直路被切成共线小段(加密补回、居中整体平移,最终形状不变)
ROUTE_DECENTER_HUG_CLEARANCE = 4.0  # 单侧余量 < 此值即视为"贴边"(像素)
ROUTE_DECENTER_HUG_ASYMMETRY = 2.0  # 且两侧余量差 >= 此值,才有"可向开阔侧让开"的空间(像素)
ROUTE_DECENTER_WATER_DROP = 1.5  # 紧边外侧地面比脚下低于此值(或直接离开网格)即判为水/坎 —— 贴它才危险(像素)
ROUTE_RELAX_TURN_CAP = 88.0  # 松弛/平移允许的转角上限(度);仍 <= 原转角者放行(不在已有拐角上倒扣分)
ROUTE_RELAX_MIDPOINT_WEIGHT = 0.50  # 松弛目标 = 此权重·邻点中点 + (1-此)·余量中心:中点项消尖角、余量项推离窄边。
ROUTE_RELAX_MAX_TRANSLATE = 12.0  # 单点相对原位的最大位移(像素):钳住松弛,不让它把点拽过头
ROUTE_RELAX_ITERATIONS = 16  # Gauss-Seidel 松弛迭代次数。cap 使单步居中变温和(只按近墙距推),靠多迭代逐步收敛到中线
ROUTE_RELAX_BIAS_NEAR_CAP = True  # 余量偏置鲁棒:把 bias 钳在 min(左,右余量) 内。走廊(两侧都近)逐迭代渐进居中;
ROUTE_RELAX_ACTIVE_SET = True  # 提速(输出逐位不变):只处理"上轮自己或邻居动过"的点。松弛是局部 Gauss-Seidel,稳定点重算必不动,跳过等价
ROUTE_RELAX_FAST_PROBE = True  # 提速(BIAS_NEAR_CAP 下输出逐位不变):耦合双侧探墙,近墙 m 确定后远侧封顶 3m —— bias 钳在 ±m 内,远侧超 3m 无影响,省大量步进
ROUTE_WATER_SHIFT_SAFE = 4.0  # 贴水块整体平移力争达到的单侧安全余量(像素)
ROUTE_WATER_SHIFT_MAX = 14.0  # 贴水块整体平移上限(像素)
ROUTE_FLOOR_ENABLE = True
ROUTE_FLOOR_MIN_CLEARANCE = 2.0  # 目标最小离边余量(nav单位):各向同性最近边界 < 此的点(含凸角/pinned)向中轴推离边界
ROUTE_FLOOR_STEP = 0.6  # 单轮沿中轴最大推进(nav单位):小步多迭代,绝不一次到位 —— 防过冲到对侧反而把 d_min 变差
ROUTE_FLOOR_MAX_TRANSLATE = 4.0  # 单点相对原位的累计最大位移(nav单位):钳住,不把拐角拽过头
ROUTE_FLOOR_ITERATIONS = 14  # 中轴梯度上升迭代上限(收敛即停;已达标点设 settled 永久跳过)
ROUTE_FLOOR_PROBE_DIRS = 8  # 各向探测方向数(8 足够辨最近边界方向,比 16 省一半)
ROUTE_FLOOR_PROBE_MARGIN = 1.0  # 墙距探测早停余量:只探到 地板+此 即够判达标 —— 省大量步进,提速核心
ROUTE_GAP_REPEL_ENABLE = True
ROUTE_GAP_REPEL_TRIGGER = 1.5  # 仅当点距真实断崖(高度不连续=会掉落)< 此才处理;开阔/贴接缝的点不动
ROUTE_GAP_REPEL_SAFE = 2.0  # 推离断崖力争达到的距离(nav单位);达到即停,窄口够不到则尽力
ROUTE_GAP_REPEL_MAX_TRANSLATE = 4.0  # 单点相对原位的最大跳离位移(nav单位)
ROUTE_GAP_REPEL_STEP = 0.3  # 跳离步进(nav单位):地板小步会被断崖对岸卡住,这里允许大步跨到开阔侧
ROUTE_GAP_REPEL_PROBE_DIRS = 16  # 各向探测方向数(断崖方向任意,须够密以免漏判)
ROUTE_GAP_REPEL_PROBE_STEP = 0.15  # 离崖距探测步进(nav单位):须细于居中默认 0.5,否则窄口 0.1->0.4 的改善看不见、推不动
SEGMENT_WALK_SNAP_RADIUS = 1.0
SEGMENT_WALK_EPSILON = 1e-6
SEGMENT_PARALLEL_EPSILON = 1e-12
# 空间索引网格边长(像素)。网格仅是查询加速结构,不影响 snap/raycast 的输出(任何
# 落在查询半径内的三角形都会按包围盒插入到对应 bin)。烘焙后网格三角形极细碎
# (中位包围盒约 1px),96px 的粗 bin 会让单个 bin 堆叠上万个三角形,使纯 Python
# 的 snap 退化成线性扫描;取 8px 让每个 bin 仅含数十个三角形,snap 提前命中。
INDEX_BIN_SIZE = 4.0  # 空间索引网格边长。
SNAP_FALLBACK_RADIUS = 16.0  # snap 初查(按调用方半径)无果时的兜底扩搜半径。

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
        self.triangle_boundary_distance: list[float] = []
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

    def _is_small_island(self, triangle_index: int) -> bool:
        # A micro-component (baked wall-top / ledge, not the real floor) stacked over the dominant
        # surface; demote it in snap so a real surface always wins. Same cutoff the bridge logic uses.
        return self.natural_component_size[self.natural_component[triangle_index]] <= SMALL_BRIDGE_COMPONENT_MAX_TRIANGLES

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
        if not candidates and query_radius < SNAP_FALLBACK_RADIUS:
            candidates = self._candidate_triangles(zone_id, point, SNAP_FALLBACK_RADIUS)
        if floor_y is None or floor_y <= FLOOR_Y_VALID_MIN:
            # Floor-blind path: rank by (non-island, distance, index). With no island in play this is the
            # legacy order (containing surfaces win at distance 0, ties by smallest index) so golden-hash
            # parity holds; it only diverges to skip a micro-component when a real surface competes. Mirrors
            # C++ BaseNavPlanner::snap.
            best_rank: tuple[int, float, int] | None = None
            best: _SnapResult | None = None
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
                rank = (1 if self._is_small_island(triangle_index) else 0, distance, triangle_index)
                if best_rank is None or rank < best_rank:
                    best_rank = rank
                    best = _SnapResult(triangle=triangle_index, point=snapped, distance=distance)
            return best
        # Floor-aware path: a click in a multi-floor base projects onto several STACKED
        # triangles (other floors / walls overlap this (u,v)). Rank so an in-band surface
        # (|height-floor_y| <= FLOOR_BAND) always beats an off-band one, then by snap
        # distance, then by height proximity to floor_y. The band is a PREFERENCE — if
        # nothing lands in-band we still return the nearest surface (never None), so
        # floor_y only re-ranks the snap target onto the correct floor, never gates it out.
        best_key: tuple[int, int, float, float] | None = None
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
            key = (0 if delta <= FLOOR_BAND else 1, 1 if self._is_small_island(triangle_index) else 0, distance, delta)
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
        self.triangle_boundary_distance = self._compute_triangle_boundary_distances()
        _report_progress(progress_callback, 0.59)

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

    def _triangle_has_boundary_edge(self, triangle_index: int) -> bool:
        if triangle_index >= len(self.triangles):
            return False
        zone_id = self.triangle_zone[triangle_index]
        if zone_id == 0:
            return False
        for neighbor in self.triangles[triangle_index].neighbors:
            if neighbor < 0 or neighbor >= len(self.triangles) or self.triangle_zone[neighbor] != zone_id:
                return True
        return False

    def _compute_triangle_boundary_distances(self) -> list[float]:
        distances = [math.inf] * len(self.triangles)
        if self.adjacency is None:
            return distances

        open_heap: list[tuple[float, int]] = []
        for triangle_index in range(len(self.triangles)):
            if self._triangle_has_boundary_edge(triangle_index):
                distances[triangle_index] = 0.0
                heapq.heappush(open_heap, (0.0, triangle_index))

        while open_heap:
            distance, current = heapq.heappop(open_heap)
            if distance > distances[current]:
                continue
            if distance >= ROUTE_BOUNDARY_DISTANCE_LIMIT:
                continue
            current_center = self.triangles[current].center
            for neighbor in self.adjacency[current]:
                if neighbor < 0 or neighbor >= len(self.triangles) or self.triangle_zone[neighbor] != self.triangle_zone[current]:
                    continue
                candidate = distance + _point_distance(current_center, self.triangles[neighbor].center)
                if candidate >= distances[neighbor] or candidate > ROUTE_BOUNDARY_DISTANCE_LIMIT:
                    continue
                distances[neighbor] = candidate
                heapq.heappush(open_heap, (candidate, neighbor))
        return distances

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
        # 热点(密网格上每次 point_on_mesh 都走这):去掉了原先的 seen 去重 set —— 细分小三角形基本不跨桶,
        # 去重几乎全是白付出的 set.add(profile 实测占 ~4s)。偶尔同一三角形被重复返回完全无害:point_on_mesh
        # 命中即返回、snap/ground_height 按距离/高度取最优,重复值不改结果。输出不变,但省下去重开销。
        px, py = point
        result = []
        bin_size = self.bin_size
        bins = self.bins
        triangle_bounds = self.triangle_bounds
        left = math.floor((px - radius) / bin_size)
        right = math.floor((px + radius) / bin_size)
        top = math.floor((py - radius) / bin_size)
        bottom = math.floor((py + radius) / bin_size)
        for bin_x in range(left, right + 1):
            for bin_y in range(top, bottom + 1):
                for triangle_index in bins.get((zone_id, bin_x, bin_y), ()):
                    bounds = triangle_bounds[triangle_index]
                    if bounds[0] - radius <= px <= bounds[2] + radius and bounds[1] - radius <= py <= bounds[3] + radius:
                        result.append(triangle_index)
        return result

    def _triangle_points(
        self,
        triangle_index: int,
    ) -> tuple[tuple[float, float], tuple[float, float], tuple[float, float]]:
        triangle = self.triangles[triangle_index]
        return tuple((self.vertices[index].u, self.vertices[index].v) for index in triangle.vertices)  # type: ignore[return-value]

    def _point_on_mesh(self, zone_id: int, point: tuple[float, float], tri_cache: list[int] | None = None) -> bool:
        # 点是否落在该区任意三角形内。与 is_segment_walkable 的逐三角行进不同,本判定只看"点在不在网格上",
        # 不依赖共享边邻接,因此在重叠/碎片网格上不会像行进那样误判 -> 居中据此能看见真实走廊宽度。
        # tri_cache: 可选的单元素缓存 [last_hit_triangle]。密集探测(余量扫 ray、地板各向 probe)中相邻采样点
        # 多落在同一三角形内(实测命中 62-76%),先测缓存命中的三角形即可免去 _candidate_triangles bin 扫描。
        if tri_cache is not None and tri_cache[0] >= 0:
            v0, v1, v2 = self._triangle_points(tri_cache[0])
            if _point_in_triangle(point, v0, v1, v2):
                return True
        for triangle_index in self._candidate_triangles(zone_id, point, SEGMENT_WALK_SNAP_RADIUS):
            v0, v1, v2 = self._triangle_points(triangle_index)
            if _point_in_triangle(point, v0, v1, v2):
                if tri_cache is not None:
                    tri_cache[0] = triangle_index
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

    def _boundary_aware_transition_cost(self, lhs: int, rhs: int, base_cost: float) -> float:
        def penalty_ratio(triangle_index: int) -> float:
            if triangle_index >= len(self.triangle_boundary_distance):
                return 0.0
            distance = self.triangle_boundary_distance[triangle_index]
            if not math.isfinite(distance) or distance >= ROUTE_BOUNDARY_DISTANCE_LIMIT:
                return 0.0
            return (ROUTE_BOUNDARY_DISTANCE_LIMIT - distance) / ROUTE_BOUNDARY_DISTANCE_LIMIT

        ratio = (penalty_ratio(lhs) + penalty_ratio(rhs)) * 0.5
        return base_cost * (1.0 + ratio * ROUTE_BOUNDARY_PENALTY_FACTOR)

    def _transition_cost(self, lhs: int, rhs: int) -> float:
        # 缓存(输出不变):转移代价是静态网格的纯函数。A* 在细分密网格上会反复(跨查询)对同一边求值,
        # 而每次都要 _shared_edge_midpoint / _closest_edge_bridge_points(9 对边距),缓存后第二次起 O(1)。
        cache = self.__dict__.setdefault("_transition_cost_cache", {})
        key = (lhs, rhs)
        value = cache.get(key)
        if value is None:
            value = cache[key] = self._transition_cost_uncached(lhs, rhs)
        return value

    def _transition_cost_uncached(self, lhs: int, rhs: int) -> float:
        lhs_center = self.triangles[lhs].center
        rhs_center = self.triangles[rhs].center
        midpoint = self._shared_edge_midpoint(lhs, rhs)
        if midpoint is not None:
            base_cost = _point_distance(lhs_center, midpoint) + _point_distance(midpoint, rhs_center)
            return self._boundary_aware_transition_cost(lhs, rhs, base_cost)
        bridge_points = self._closest_edge_bridge_points(lhs, rhs)
        height_delta = abs(self.triangle_height[lhs] - self.triangle_height[rhs])
        if height_delta > BRIDGE_MAX_HEIGHT_DELTA:
            return math.inf
        if bridge_points is None:
            base_cost = self._heuristic(lhs, rhs) + BRIDGE_FIXED_COST + height_delta * BRIDGE_HEIGHT_COST_FACTOR
            return self._boundary_aware_transition_cost(lhs, rhs, base_cost)
        gap = _point_distance(bridge_points[0], bridge_points[1])
        base_cost = (
            _point_distance(lhs_center, bridge_points[0])
            + gap
            + _point_distance(bridge_points[1], rhs_center)
            + BRIDGE_FIXED_COST
            + gap * BRIDGE_GAP_COST_FACTOR
            + height_delta * BRIDGE_HEIGHT_COST_FACTOR
        )
        return self._boundary_aware_transition_cost(lhs, rhs, base_cost)

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
        # The corridor is single-zone (A* never crosses zones), so the first triangle's zone drives
        # the walkability check that keeps simplification from cutting a corner through a wall.
        zone_id = self.triangle_zone[triangle_path[0]] if triangle_path else 0
        tri_cache = [-1]  # 密集探测缓存:相邻采样点多落在同一三角形内,命中即免 bin 扫描
        height_walkable = lambda segment_a, segment_b: self._segment_height_walkable(zone_id, segment_a, segment_b)
        on_mesh = lambda point: self._point_on_mesh(zone_id, point, tri_cache)
        # SSF 漏斗:在三角形走廊内直接求最短折线,替代中点+thin 拉直.
        # 内部试双向握手、取在网格上且路径更短(更直)的那个;两向均离网格才回退到 thin.
        funnel_result = _funnel_route_points(self, triangle_path, start, goal, on_mesh)
        if funnel_result is not None:
            pulled_points, pulled_breaks = funnel_result
        else:
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
            # LOS 拉直用"运行参考高度"oracle(不是 march):march 只走边邻接,在共面重叠缝处会误拒直捷径、
            # 留住 portal 中点锯齿;高度 oracle 沿捷径按 ROUTE_PULL_SAMPLE_STEP 采样,要求处处在网格上且地面
            # 高度不跳变 -> 开阔锯齿被拉直走中线、绕 +9 墙的真拐角因踩墙高度跳变而留住直角。
            pulled_points, pulled_breaks = _thin_route_points_with_breaks(
                simplified_points, simplified_breaks, is_segment_walkable=height_walkable, point_on_mesh=on_mesh
            )
        # 拉直输出是"拐角到拐角"、中间无内部点;先加密恢复内部采样点,结构保持式居中才有"直段"可整体平移。
        densified_points, densified_breaks = _densify_route_points_with_breaks(pulled_points, pulled_breaks)
        # 居中:在结构性拐角处切分直段,仅把够直的直段整体平移到走廊中线、并在拐角延长线交点处重连 ->
        # 既居中又精确保住真直角(不抹圆、不锯齿)。墙距/校验用点包含式(march 在重叠/碎片网格上会低估
        # 余量、误拒候选,使居中失效)。
        centered_points, centered_breaks = _center_route_points_with_breaks(
            densified_points, densified_breaks, point_on_mesh=on_mesh
        )
        # 逐 run 刚性居中只搬得动"够直的长直段";贴着水弯过去的块、孤立尖角仍 0 余量贴水。图像识别坐标离散、
        # 滞后、有噪,贴水极危险,故再做两段"离水让边"细化:
        #   ① 守卫式松弛:逐点推向 中点 + 余量中心,消尖角、离窄边(不抹真直角、不新增折返);
        #   ② 贴水块整体平移:把方向一致的连续贴水点聚成块,带 Hann 渐隐整体让向开阔侧(松弛搬不动整块)。
        ground_height = lambda point: self._ground_height_near_indexed(zone_id, point, None)
        relaxed_points, relaxed_breaks = _route_clearance_relax_with_breaks(
            centered_points, centered_breaks, point_on_mesh=on_mesh, height_walkable=height_walkable
        )
        decentered_points, decentered_breaks = _route_water_edge_shift_with_breaks(
            relaxed_points, relaxed_breaks, point_on_mesh=on_mesh, ground_height=ground_height
        )
        # 居中/让边会把拐角搬到直段延长线交点上,可能拉出超过 ROUTE_MAX_POINT_DISTANCE 的长边;末尾再加密一次,
        # 保证最终点距 <= ROUTE_MAX_POINT_DISTANCE(机器人沿密集折线行走)。
        densified_final, densified_final_breaks = _densify_route_points_with_breaks(decentered_points, decentered_breaks)
        # 抗噪裕度地板(收尾):funnel 求最短会贴 navmesh 内角,前面几道对 pinned 拐角/凸角仍留贴边点(离边~0);
        # 上游图像定位有噪,贴可走面边界=出界风险。这里把残留贴边点沿中轴推离边界到地板,确保全程留抗噪余量(仿人走)。
        floored, floored_breaks = _route_clearance_floor_with_breaks(
            densified_final, densified_final_breaks, point_on_mesh=on_mesh
        )
        return _route_real_gap_repel_with_breaks(
            floored, floored_breaks, point_on_mesh=on_mesh, height_walkable=height_walkable
        )

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


def _funnel_route_points(
    field,
    triangle_path: list[int],
    start: tuple[float, float],
    goal: tuple[float, float],
    on_mesh,
) -> tuple[list[tuple[float, float]], list[int]] | None:
    """SSF (Simple Stupid Funnel) 在三角形走廊内求最短路,替代中点+thin 拉直阶段.

    对每个三角形按 CCW 绕序确定有向出边 u→v,分别以 (left=pu,right=pv) 和
    (left=pv,right=pu) 两种握手各跑一次漏斗,取二者中在网格上且路径总长更短的结果.
    若两次均有离网格线段则返回 None,由调用方回退到 thin.
    退化共边(共享顶点数!=2)→ 桥接点作收缩孔(pinch),强制 apex 经过该点.
    """

    def _ta2(a: tuple, b: tuple, c: tuple) -> float:
        return (b[0] - a[0]) * (c[1] - a[1]) - (c[0] - a[0]) * (b[1] - a[1])

    def _eq(a: tuple, b: tuple) -> bool:
        return abs(a[0] - b[0]) < 1e-7 and abs(a[1] - b[1]) < 1e-7

    def _ccw_vi(t: int) -> list[int]:
        vi = list(field.triangles[t].vertices)
        p = [(field.vertices[idx].u, field.vertices[idx].v) for idx in vi]
        if _ta2(p[0], p[1], p[2]) < 0:
            vi = [vi[0], vi[2], vi[1]]
        return vi

    def _ssf(portals: list) -> list:
        pts = [portals[0][0]]
        apex = portals[0][0]
        pl = portals[0][0]
        pr = portals[0][1]
        ai = li = ri = 0
        i = 1
        while i < len(portals):
            left, right = portals[i]
            if _ta2(apex, pr, right) <= 0.0:
                if _eq(apex, pr) or _ta2(apex, pl, right) > 0.0:
                    pr = right
                    ri = i
                else:
                    pts.append(pl)
                    apex = pl
                    ai = li
                    pl = pr = apex
                    li = ri = ai
                    i = ai + 1
                    continue
            if _ta2(apex, pl, left) >= 0.0:
                if _eq(apex, pl) or _ta2(apex, pr, left) < 0.0:
                    pl = left
                    li = i
                else:
                    pts.append(pr)
                    apex = pr
                    ai = ri
                    pl = pr = apex
                    li = ri = ai
                    i = ai + 1
                    continue
            i += 1
        pts.append(portals[-1][0])
        return pts

    # Pre-compute CCW vertex lists and bridge pairs (independent of swap).
    ccw_vi_cache = {}
    bridge_pairs: list[tuple[tuple[float, float], tuple[float, float]]] = []
    raw_edges: list[tuple[str, object]] = []  # ("portal", (i0,i1,va)) or ("bridge", (exit,entry)) or ("pinch", m)

    for tri_a, tri_b in zip(triangle_path, triangle_path[1:]):
        if tri_a not in ccw_vi_cache:
            ccw_vi_cache[tri_a] = _ccw_vi(tri_a)
        va = ccw_vi_cache[tri_a]
        sb = set(field.triangles[tri_b].vertices)
        shared = [idx for idx in va if idx in sb]
        if len(shared) == 2:
            pos = {vi: k for k, vi in enumerate(va)}
            i0, i1 = shared[0], shared[1]
            raw_edges.append(("portal", (i0, i1, va, pos)))
        else:
            bridge = field._closest_edge_bridge_points(tri_a, tri_b)
            if bridge is not None:
                exit_pt, entry_pt = bridge
                if not _eq(exit_pt, entry_pt):
                    bridge_pairs.append((exit_pt, entry_pt))
                raw_edges.append(("bridge", (exit_pt, entry_pt)))
            else:
                m = field._shared_edge_midpoint(tri_a, tri_b)
                raw_edges.append(("pinch", m))

    best: tuple[list, list, float] | None = None

    for swap in (False, True):
        # Build portal list for this handedness.
        portals: list[tuple[tuple[float, float], tuple[float, float]]] = [(start, start)]
        for kind, data in raw_edges:
            if kind == "portal":
                i0, i1, va, pos = data
                u, v = (i0, i1) if (pos[i0] + 1) % 3 == pos[i1] else (i1, i0)
                pu = (field.vertices[u].u, field.vertices[u].v)
                pv = (field.vertices[v].u, field.vertices[v].v)
                portals.append((pu, pv) if not swap else (pv, pu))
            elif kind == "bridge":
                exit_pt, _entry_pt = data
                portals.append((exit_pt, exit_pt))
            else:  # "pinch"
                m = data
                if m is not None:
                    portals.append((m, m))
        portals.append((goal, goal))

        # Run SSF, deduplicate.
        raw_pts = _ssf(portals)
        clean: list[tuple[float, float]] = [raw_pts[0]]
        for q in raw_pts[1:]:
            if not _eq(q, clean[-1]):
                clean.append(q)

        # Reconstruct segment breaks from bridge pairs.
        result: list[tuple[float, float]] = []
        breaks: list[int] = []
        for pt in clean:
            result.append(pt)
            for exit_pt, entry_pt in bridge_pairs:
                if abs(pt[0] - exit_pt[0]) < 1e-7 and abs(pt[1] - exit_pt[1]) < 1e-7:
                    breaks.append(len(result))
                    result.append(entry_pt)
                    break

        # Validate: non-bridge segments must be on-mesh.
        bridge_break_set = set(breaks)
        valid = True
        for k in range(len(result) - 1):
            if k + 1 in bridge_break_set:
                continue
            a, b = result[k], result[k + 1]
            seg_len = math.hypot(b[0] - a[0], b[1] - a[1])
            steps = max(1, int(seg_len / 1.0))
            for j in range(steps + 1):
                t = j / steps
                if not on_mesh((a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t)):
                    valid = False
                    break
            if not valid:
                break

        if not valid:
            continue

        # Pick by total path length (shorter = geometrically straighter = correct handedness).
        total_len = sum(
            math.hypot(result[k + 1][0] - result[k][0], result[k + 1][1] - result[k][1])
            for k in range(len(result) - 1)
            if k + 1 not in bridge_break_set
        )
        if best is None or total_len < best[2]:
            best = (result, breaks, total_len)

    if best is None:
        return None
    return best[0], best[1]


def _thin_route_points_with_breaks(
    points: list[tuple[float, float]],
    segment_breaks: list[int],
    is_segment_walkable=None,
    point_on_mesh=None,
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
        kept_indices = _thin_continuous_segment(points, start, end, is_segment_walkable, point_on_mesh)
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


def _relax_clearance_pair(origin, dir_x, dir_y, cap, point_on_mesh, step: float = CENTER_PROBE_STEP):
    # 同时探 +dir 与 -dir 两侧墙距,等价于两次 _max_offset_on_mesh。但一侧先触墙(=近墙 m)后,把另一侧探测
    # 上限封顶到 3m:松弛用 BIAS_NEAR_CAP 把 bias=(L-R)/2 钳在 ±m 内,远侧超过 3m 的部分对 bias 毫无影响,
    # 不必再探。近墙浅时远侧从 64 步降到 ~6 步,松弛探测开销骤降;在 BIAS_NEAR_CAP 下结果(bias)逐位不变。
    ox, oy = origin
    left = right = -1.0  # -1 = 该侧仍在探
    last_left = last_right = 0.0
    soft_cap = cap
    distance = step
    while distance <= soft_cap:
        if left < 0.0:
            if point_on_mesh((ox + dir_x * distance, oy + dir_y * distance)):
                last_left = distance
            else:
                left = last_left
        if right < 0.0:
            if point_on_mesh((ox - dir_x * distance, oy - dir_y * distance)):
                last_right = distance
            else:
                right = last_right
        if left >= 0.0 and right >= 0.0:
            break
        if left >= 0.0 and right < 0.0 and 3.0 * left < soft_cap:
            soft_cap = 3.0 * left  # 近墙=left,远侧(right)封顶 3*left
        elif right >= 0.0 and left < 0.0 and 3.0 * right < soft_cap:
            soft_cap = 3.0 * right
        distance += step
    if left < 0.0:
        left = cap if last_left >= cap - step * 0.5 else last_left
    if right < 0.0:
        right = cap if last_right >= cap - step * 0.5 else last_right
    return left, right


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


def _is_turn_back_at(points: list[tuple[float, float]], index: int) -> bool:
    # 单点折返判据:入边·出边点积 < 0(夹角 > 90°)。退化边(零长)不计。逐 run 局部居中用它
    # 判断"此次平移是否在窗口内新增折返",与 _route_turn_back_count 同源,保证两处判据一致。
    if index <= 0 or index >= len(points) - 1:
        return False
    ax = points[index][0] - points[index - 1][0]
    ay = points[index][1] - points[index - 1][1]
    bx = points[index + 1][0] - points[index][0]
    by = points[index + 1][1] - points[index][1]
    if ax * ax + ay * ay < 1e-9 or bx * bx + by * by < 1e-9:
        return False
    return ax * bx + ay * by < 0


def _route_turn_back_count(points: list[tuple[float, float]]) -> int:
    # 折返点数(相邻两段方向夹角 > 90°,点积 < 0):衡量"锯齿/绕圈"程度,居中绝不能让它变多。
    return sum(1 for index in range(1, len(points) - 1) if _is_turn_back_at(points, index))


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
    #   2. 仅当一个 run 足够"直"(内部点偏离首尾弦 <= ROUTE_RUN_STRAIGHT_TOL)时,才把整段沿其
    #      法向"刚性平移"到走廊中线(平移量 = 段内逐点居中量的中位数,夹紧并按可行性缩放)。刚性平移保持
    #      直段仍直 -> 不会产生逐点独立居中那种高频锯齿(那是把折返数翻三倍、被用户否决的根因)。
    #   3. 每条直 run 作为独立事务"局部提交":平移自身内部点、把自身拐角投影到平移后的直线;仅当其局部
    #      窗口仍全程在网格内、且不新增折返时才提交。单条失败只丢弃自己 —— 取代旧的"整段全有或全无"闸:
    #      旧闸在碎片化路线(结构性拐角多 -> run 多且互相牵连)上几乎总是整段回退 = 居中彻底失效,正是
    #      路线在窄走廊/水边贴边不被纠正的根因。
    #   4. 碎片/窄走廊里的弯曲 run 不满足"直"判据 -> 原样不动(no-op);贴住固定端点(整段首尾 = S/G 或
    #      桥接点)的 run 也不平移 —— 平移它必在固定端拽出一道绕行钩。
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
        samples = list(original[run_start + 1 : run_end])
        if not samples:
            # 单段短桥也要居中:结构性拐角切分后常只剩一条边,仅看端点会把 A* 贴边线原样保留。
            samples = [(original[run_start][0] + dx * t, original[run_start][1] + dy * t) for t in (0.25, 0.5, 0.75)]
        offsets = []
        for sample in samples:
            clearance_plus = _max_offset_on_mesh(sample, normal_x, normal_y, probe_limit, point_on_mesh)
            clearance_minus = _max_offset_on_mesh(sample, -normal_x, -normal_y, probe_limit, point_on_mesh)
            offsets.append((clearance_plus - clearance_minus) * 0.5)
        offsets.sort()
        target_shift = max(-max_shift, min(max_shift, offsets[len(offsets) // 2]))
        chosen_shift = 0.0
        for scale in (1.0, 0.75, 0.5, 0.25):  # 平移量逐步收缩到全程可行为止
            if all(
                point_on_mesh((sample[0] + normal_x * target_shift * scale, sample[1] + normal_y * target_shift * scale))
                for sample in samples
            ):
                chosen_shift = target_shift * scale
                break
        run["shift"] = chosen_shift
        anchor = samples[len(samples) // 2]  # 直线锚在 run 主体上,而非可能贴角的拐点
        run["base"] = (anchor[0] + normal_x * run["shift"], anchor[1] + normal_y * run["shift"])
        runs.append(run)
    # 3. 逐 run 局部提交。每条直 run 作为独立事务:平移自身内部点、把自身两个拐角投影到平移后的直线上,
    #    仅当其局部窗口 [run_start-1 .. run_end+1] 仍全程在网格内、且不新增折返时才提交;任一 run 失败只
    #    丢弃它自己,不连累其余。贴住固定端点(整段首尾)的 run 不平移 —— 平移它必在固定端拽出绕行钩。
    result = list(original)
    corner_move_limit = max_shift * ROUTE_CORNER_MOVE_FACTOR
    for run in runs:
        if not run["has_line"] or run["shift"] == 0.0:
            continue
        run_start, run_end = run["start"], run["end"]
        if run_start == 0 or run_end == point_count - 1:
            continue  # 贴住固定端点的 run:平移只会在 S/G/桥处拉出绕行钩
        normal_x, normal_y = run["normal"]
        shift = run["shift"]
        trial = list(result)
        for j in range(run_start + 1, run_end):
            trial[j] = (original[j][0] + normal_x * shift, original[j][1] + normal_y * shift)
        for ci in (run_start, run_end):  # 把本 run 自己的拐角投影到平移后的直线(全局端点除外)
            if ci == 0 or ci == point_count - 1:
                continue
            foot = _perpendicular_foot(original[ci], run["base"], run["direction"])
            if (
                point_on_mesh(foot)
                and math.hypot(foot[0] - original[ci][0], foot[1] - original[ci][1]) <= corner_move_limit
            ):
                trial[ci] = foot
        low = max(0, run_start - 1)
        high = min(point_count - 1, run_end + 1)
        if not all(_segment_on_mesh(trial[k], trial[k + 1], point_on_mesh) for k in range(low, high)):
            continue
        if any(
            _is_turn_back_at(trial, i) and not _is_turn_back_at(result, i)
            for i in range(low, high + 1)
            if 0 < i < point_count - 1
        ):
            continue
        result = trial
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


def _route_edge_open_direction(points, index, point_on_mesh, ground_height):
    # 判断 points[index] 是否"贴着水边",若是则回传可让开的方向。返回 None 表示不贴水边;
    # 否则返回 (开阔侧单位法向 x, y, 开阔侧余量, 紧边余量)。
    #   贴边:某一侧余量 < ROUTE_DECENTER_HUG_CLEARANCE 且两侧差 >= ROUTE_DECENTER_HUG_ASYMMETRY;
    #   水边:紧边(余量小的一侧)外侧地面离开网格、或骤降 > ROUTE_DECENTER_WATER_DROP(墙边不算,贴墙不危险)。
    n = len(points)
    if index <= 0 or index >= n - 1:
        return None
    a = points[index - 1]
    c = points[index + 1]
    dx, dy = c[0] - a[0], c[1] - a[1]
    length = math.hypot(dx, dy)
    if length < 1e-6:
        return None
    nx, ny = -dy / length, dx / length
    left = _max_offset_on_mesh(points[index], nx, ny, ROUTE_CENTER_PROBE_LIMIT, point_on_mesh)
    right = _max_offset_on_mesh(points[index], -nx, -ny, ROUTE_CENTER_PROBE_LIMIT, point_on_mesh)
    if not (min(left, right) < ROUTE_DECENTER_HUG_CLEARANCE and abs(left - right) >= ROUTE_DECENTER_HUG_ASYMMETRY):
        return None
    if left < right:
        tight_nx, tight_ny, tight = nx, ny, left
    else:
        tight_nx, tight_ny, tight = -nx, -ny, right
    here, _ = ground_height(points[index])
    beyond, _ = ground_height((points[index][0] + tight_nx * (tight + 2.0), points[index][1] + tight_ny * (tight + 2.0)))
    is_water = beyond is None or (here is not None and beyond < here - ROUTE_DECENTER_WATER_DROP)
    if not is_water:
        return None
    if left > right:
        return (nx, ny, left, right)
    return (-nx, -ny, right, left)


def _route_clearance_relax_with_breaks(points, segment_breaks, point_on_mesh=None, height_walkable=None):
    # 居中细化一:守卫式 Gauss-Seidel 松弛。逐 run 刚性居中只动"够直的长直段",留下弯曲块、孤立尖角仍贴边。
    # 这里对每个内部、非冻结、非真拐角(墙角)的点,把它推向 中点(消尖) 与 余量中心(离窄边) 的加权目标,
    # 总位移钳在 ROUTE_RELAX_MAX_TRANSLATE 内,且仅当候选点+两侧连段仍在网格上、且本点及左右邻的转角都不
    # 超过 max(CAP, 原转角) 时才提交 —— 既不抹掉真直角,也绝不新增折返。冻结整段首尾与桥接点附近(±2)。
    n = len(points)
    if n <= 3 or point_on_mesh is None or height_walkable is None:
        return points, segment_breaks
    weight_clear = 1.0 - ROUTE_RELAX_MIDPOINT_WEIGHT
    original = [tuple(point) for point in points]
    result = [list(point) for point in points]
    # 真拐角(墙角):弦 i-1 -> i+1 高度不可走 = 直连会穿墙,该点必须留在原拐角处,不参与松弛。
    pinned = {i for i in range(1, n - 1) if not height_walkable(original[i - 1], original[i + 1])}
    frozen = {0, 1, n - 2, n - 1}
    for break_index in segment_breaks:
        for delta in (-2, -1, 0, 1, 2):
            frozen.add(break_index + delta)
    origin_turn = [
        _route_turn_angle_deg(original[i - 1], original[i], original[i + 1]) if 0 < i < n - 1 else 0.0 for i in range(n)
    ]

    def turn_at(arr, i):
        return _route_turn_angle_deg(arr[i - 1], arr[i], arr[i + 1]) if 0 < i < n - 1 else 0.0

    # active-set(ROUTE_RELAX_ACTIVE_SET):dirty=待评估点;一点只在"自己或左右邻上次动过"时才需重算 —— 局部松弛
    # 里稳定点重算的目标必与现状相同、必不动,跳过严格等价。关闭则 dirty=None,退化为原"每轮全扫描"逐位一致。
    dirty = {i for i in range(1, n - 1) if i not in frozen and i not in pinned} if ROUTE_RELAX_ACTIVE_SET else None
    for _ in range(ROUTE_RELAX_ITERATIONS):
        if dirty is not None and not dirty:
            break  # 收敛:无待评估点
        next_dirty = set() if dirty is not None else None
        for i in range(1, n - 1):
            if dirty is not None and i not in dirty:
                continue
            if i in frozen or i in pinned:
                continue
            a, c = result[i - 1], result[i + 1]
            dx, dy = c[0] - a[0], c[1] - a[1]
            length = math.hypot(dx, dy)
            if length < 1e-6:
                continue
            nx, ny = -dy / length, dx / length
            if ROUTE_RELAX_FAST_PROBE and ROUTE_RELAX_BIAS_NEAR_CAP:
                clearance_left, clearance_right = _relax_clearance_pair(
                    tuple(result[i]), nx, ny, ROUTE_CENTER_PROBE_LIMIT, point_on_mesh
                )
            else:
                clearance_left = _max_offset_on_mesh(tuple(result[i]), nx, ny, ROUTE_CENTER_PROBE_LIMIT, point_on_mesh)
                clearance_right = _max_offset_on_mesh(tuple(result[i]), -nx, -ny, ROUTE_CENTER_PROBE_LIMIT, point_on_mesh)
            mid_x, mid_y = (a[0] + c[0]) * 0.5, (a[1] + c[1]) * 0.5
            bias = (clearance_left - clearance_right) * 0.5
            if ROUTE_RELAX_BIAS_NEAR_CAP:
                # 钳在最近墙距内:一侧射线穿开口逃逸(cl 爆大)时,bias 不跟着逃逸跑,只按近侧墙距轻推。
                near_wall = min(clearance_left, clearance_right)
                if bias > near_wall:
                    bias = near_wall
                elif bias < -near_wall:
                    bias = -near_wall
            clear_x, clear_y = result[i][0] + nx * bias, result[i][1] + ny * bias
            target_x = ROUTE_RELAX_MIDPOINT_WEIGHT * mid_x + weight_clear * clear_x
            target_y = ROUTE_RELAX_MIDPOINT_WEIGHT * mid_y + weight_clear * clear_y
            move_x, move_y = target_x - original[i][0], target_y - original[i][1]
            move_length = math.hypot(move_x, move_y)
            if move_length > ROUTE_RELAX_MAX_TRANSLATE:
                target_x = original[i][0] + move_x / move_length * ROUTE_RELAX_MAX_TRANSLATE
                target_y = original[i][1] + move_y / move_length * ROUTE_RELAX_MAX_TRANSLATE
            candidate = (target_x, target_y)
            if not point_on_mesh(candidate):
                continue
            if not (_segment_on_mesh(tuple(a), candidate, point_on_mesh) and _segment_on_mesh(candidate, tuple(c), point_on_mesh)):
                continue
            # 转角守卫:原先每个候选都 trial=整表拷贝(O(N)/候选 → 每道 pass O(N²),长路线上爆炸)。
            # 改为就地换入候选点算转角、被拒再换回 —— O(1),turn_at 看到的就是 result[i]=候选,输出逐位不变。
            saved = result[i]
            result[i] = [target_x, target_y]
            if not all(turn_at(result, k) <= max(ROUTE_RELAX_TURN_CAP, origin_turn[k]) + 1e-6 for k in (i - 1, i, i + 1)):
                result[i] = saved
                continue
            if dirty is not None:
                # 邻域输入变了需重评:i+1 本轮就重评(在升序扫描前方,加入当前 dirty 等同原地处理);
                # i-1/i/i+1 下轮重评。i-1 已扫过本轮、i 本轮只评一次,故二者只入 next_dirty。
                if i + 1 <= n - 2:
                    dirty.add(i + 1)
                for j in (i - 1, i, i + 1):
                    if 1 <= j <= n - 2:
                        next_dirty.add(j)
        if dirty is not None:
            dirty = next_dirty
    return [tuple(point) for point in result], segment_breaks


def _route_water_edge_shift_with_breaks(points, segment_breaks, point_on_mesh=None, ground_height=None):
    # 居中细化二:贴水块整体平移。松弛是逐点的,搬不动"整条贴着水弯过去"的块(搬一个点就在邻点处拐出折返)。
    # 这里把方向一致的连续贴水点聚成块,沿块的中位让开方向整体平移 —— 核心点满权、两端 M 个点按 Hann 斜坡渐隐
    # (把边界曲率摊开,不在某条边上一次性堆出折返)。在 (M, 平移量) 上搜索,按"触及窗口内剩余贴水点数"打分取
    # 最优,只在严格更优、且不离网格/不新增折返时提交。块的延展不跨桥接点(block_edge)。
    n = len(points)
    if n <= 3 or point_on_mesh is None or ground_height is None:
        return points, segment_breaks
    result = [list(point) for point in points]
    break_set = set(segment_breaks)

    def open_dir(route, i):
        return _route_edge_open_direction(route, i, point_on_mesh, ground_height)

    def is_block_edge(j):
        return j in break_set or (j + 1) in break_set

    info = [open_dir(result, i) for i in range(n)]
    i = 1
    while i < n - 1:
        if info[i] is None:
            i += 1
            continue
        start = i
        while (
            i + 1 < n - 1
            and info[i + 1] is not None
            and not is_block_edge(i)
            and (info[i + 1][0] * info[start][0] + info[i + 1][1] * info[start][1]) > -0.5
        ):
            i += 1
        end = i
        i += 1
        sum_x = sum(info[j][0] for j in range(start, end + 1))
        sum_y = sum(info[j][1] for j in range(start, end + 1))
        dir_length = math.hypot(sum_x, sum_y)
        if dir_length < 1e-6:
            continue
        unit_x, unit_y = sum_x / dir_length, sum_y / dir_length
        need = max(0.0, ROUTE_WATER_SHIFT_SAFE - min(info[j][3] for j in range(start, end + 1)))
        room = min(info[j][2] for j in range(start, end + 1))
        shift = min(need, max(0.0, room - ROUTE_WATER_SHIFT_SAFE), ROUTE_WATER_SHIFT_MAX)
        if shift < 0.5:
            continue
        window = range(max(1, start - 6), min(n - 1, end + 7))
        before = sum(1 for j in window if open_dir(result, j) is not None)
        best = None
        for margin in (2, 3, 4, 5, 6):
            low = max(1, start - margin)
            high = min(n - 2, end + margin)
            while low > 1 and is_block_edge(low - 1):
                low += 1
            while high < n - 2 and is_block_edge(high):
                high -= 1

            def taper_weight(j, low_bound=low, high_bound=high):
                if start <= j <= end:
                    return 1.0
                if j < start:
                    return max(0.0, (j - (start - margin)) / margin)
                return max(0.0, ((end + margin) - j) / margin)

            for scale in (shift, shift * 0.85, shift * 0.7, shift * 0.55):
                trial = [list(point) for point in result]
                for j in range(low, high + 1):
                    weight = taper_weight(j)
                    trial[j] = [result[j][0] + unit_x * scale * weight, result[j][1] + unit_y * scale * weight]
                window_range = range(low - 1, high + 2)
                on_mesh_ok = all(point_on_mesh(tuple(trial[j])) for j in range(low, high + 1)) and all(
                    _segment_on_mesh(tuple(trial[k]), tuple(trial[k + 1]), point_on_mesh) for k in range(low - 1, high + 1)
                )
                if on_mesh_ok:
                    on_mesh_ok = not any(
                        0 < k < n - 1
                        and _route_turn_angle_deg(trial[k - 1], trial[k], trial[k + 1]) > 90.0
                        and _route_turn_angle_deg(result[k - 1], result[k], result[k + 1]) <= 90.0
                        for k in window_range
                    )
                if not on_mesh_ok:
                    continue
                after = sum(1 for j in window if open_dir(trial, j) is not None)
                candidate_score = (after, margin, -scale)
                if best is None or candidate_score < best[0]:
                    best = (candidate_score, trial, after)
            if best is not None and best[2] == 0:
                break
        if best is not None and best[2] < before:
            result = best[1]
    return [tuple(point) for point in result], segment_breaks


def _route_clearance_floor_with_breaks(points, segment_breaks, point_on_mesh=None):
    # 居中细化三:抗噪裕度地板(收尾)。funnel 求最短会贴 navmesh 内角,前面几道对 pinned 拐角/凸角仍留贴边点
    # (各向同性最近边界 ~0)。这里对"最近边界 < 地板"的内部点(含 pinned 拐角)沿中轴梯度(各向探测的开阔向量)
    # 推离边界到地板;凸角的最近边界在斜对角,故必须各向探测、垂直 L/R 看不到。提速:① 单次探测同时算出最近边界
    # 距 d_min 与开阔向量;② 探测早停在 地板+裕度(够判达标);③ 已达标点设 settled 永久跳过;④ 收敛即停。
    # 守卫:候选+两侧连段在网格、推后 d_min 确有增益、不新增 > max(CAP, 原转角) 折返。冻结整段首尾与桥接±2。
    n = len(points)
    if n <= 3 or point_on_mesh is None or not ROUTE_FLOOR_ENABLE or ROUTE_FLOOR_MIN_CLEARANCE <= 0.0:
        return points, segment_breaks
    floor = ROUTE_FLOOR_MIN_CLEARANCE
    probe_cap = floor + ROUTE_FLOOR_PROBE_MARGIN  # 早停:探到 地板+裕度 即够判达标
    probe_dirs = [
        (math.cos(2.0 * math.pi * k / ROUTE_FLOOR_PROBE_DIRS), math.sin(2.0 * math.pi * k / ROUTE_FLOOR_PROBE_DIRS))
        for k in range(ROUTE_FLOOR_PROBE_DIRS)
    ]
    original = [tuple(point) for point in points]
    result = [list(point) for point in points]
    frozen = {0, 1, n - 2, n - 1}
    for break_index in segment_breaks:
        for delta in (-2, -1, 0, 1, 2):
            frozen.add(break_index + delta)
    origin_turn = [
        _route_turn_angle_deg(original[i - 1], original[i], original[i + 1]) if 0 < i < n - 1 else 0.0 for i in range(n)
    ]
    settled = [False] * n

    def probe(p):  # 单次扫 K 向:返回 (最近边界距 d_min, 开阔向量 ox, oy)
        d_min = probe_cap
        open_x = open_y = 0.0
        for dx, dy in probe_dirs:
            offset = _max_offset_on_mesh(p, dx, dy, probe_cap, point_on_mesh)
            open_x += dx * offset
            open_y += dy * offset
            if offset < d_min:
                d_min = offset
        return d_min, open_x, open_y

    def min_clearance(p):
        return min(_max_offset_on_mesh(p, dx, dy, probe_cap, point_on_mesh) for dx, dy in probe_dirs)

    def turn_at(arr, i):
        return _route_turn_angle_deg(arr[i - 1], arr[i], arr[i + 1]) if 0 < i < n - 1 else 0.0

    for _ in range(ROUTE_FLOOR_ITERATIONS):
        moved = False
        for i in range(1, n - 1):
            if i in frozen or settled[i]:
                continue
            here = (result[i][0], result[i][1])
            d_min, open_x, open_y = probe(here)
            if d_min >= floor:
                settled[i] = True  # 最近边界已达地板,后续轮次跳过
                continue
            open_length = math.hypot(open_x, open_y)
            if open_length < 1e-6:
                settled[i] = True  # 对称窄颈:开阔向量相消,半宽已到极限,推不动
                continue
            unit_x, unit_y = open_x / open_length, open_y / open_length
            step = min(floor - d_min, ROUTE_FLOOR_STEP)  # 小步:绝不一次推到地板,防过冲到对侧反而变差
            cand_x, cand_y = here[0] + unit_x * step, here[1] + unit_y * step
            move_x, move_y = cand_x - original[i][0], cand_y - original[i][1]
            move_length = math.hypot(move_x, move_y)
            if move_length > ROUTE_FLOOR_MAX_TRANSLATE:
                cand_x = original[i][0] + move_x / move_length * ROUTE_FLOOR_MAX_TRANSLATE
                cand_y = original[i][1] + move_y / move_length * ROUTE_FLOOR_MAX_TRANSLATE
            candidate = (cand_x, cand_y)
            if not point_on_mesh(candidate):
                continue
            a, c = result[i - 1], result[i + 1]
            if not (
                _segment_on_mesh((a[0], a[1]), candidate, point_on_mesh)
                and _segment_on_mesh(candidate, (c[0], c[1]), point_on_mesh)
            ):
                continue
            if min_clearance(candidate) <= d_min + 1e-6:
                continue  # 推后必须确有余量增益(防过冲到更差,稳定关键)
            # 转角守卫:就地换入候选、被拒再换回(O(1),不做整表拷贝 —— 否则每道 pass O(N²),长路线爆炸)。
            saved = result[i]
            result[i] = [cand_x, cand_y]
            if not all(turn_at(result, k) <= max(ROUTE_RELAX_TURN_CAP, origin_turn[k]) + 1e-6 for k in (i - 1, i, i + 1)):
                result[i] = saved
                continue
            moved = True
        if not moved:
            break  # 收敛即停
    return [tuple(point) for point in result], segment_breaks


def _route_real_gap_repel_with_breaks(points, segment_breaks, point_on_mesh=None, height_walkable=None):
    n = len(points)
    if n <= 3 or point_on_mesh is None or height_walkable is None or not ROUTE_GAP_REPEL_ENABLE:
        return points, segment_breaks
    trigger = ROUTE_GAP_REPEL_TRIGGER
    safe = ROUTE_GAP_REPEL_SAFE
    probe_dirs = [
        (math.cos(2.0 * math.pi * k / ROUTE_GAP_REPEL_PROBE_DIRS), math.sin(2.0 * math.pi * k / ROUTE_GAP_REPEL_PROBE_DIRS))
        for k in range(ROUTE_GAP_REPEL_PROBE_DIRS)
    ]
    original = [tuple(point) for point in points]
    result = [list(point) for point in points]
    frozen = {0, 1, n - 2, n - 1}
    for break_index in segment_breaks:
        for delta in (-2, -1, 0, 1, 2):
            frozen.add(break_index + delta)
    origin_turn = [
        _route_turn_angle_deg(original[i - 1], original[i], original[i + 1]) if 0 < i < n - 1 else 0.0 for i in range(n)
    ]

    def gap_distance(p):
        nearest = None
        for dx, dy in probe_dirs:
            offset = _max_offset_on_mesh(p, dx, dy, trigger, point_on_mesh, step=ROUTE_GAP_REPEL_PROBE_STEP)
            if offset >= trigger:
                continue
            beyond = (p[0] + dx * (offset + ROUTE_GAP_REPEL_PROBE_STEP), p[1] + dy * (offset + ROUTE_GAP_REPEL_PROBE_STEP))
            if not height_walkable(p, beyond):
                if nearest is None or offset < nearest:
                    nearest = offset
        return nearest

    def turn_at(arr, i):
        return _route_turn_angle_deg(arr[i - 1], arr[i], arr[i + 1]) if 0 < i < n - 1 else 0.0

    for i in range(1, n - 1):
        if i in frozen:
            continue
        here = (result[i][0], result[i][1])
        base = gap_distance(here)
        if base is None or base >= safe:
            continue
        a, c = result[i - 1], result[i + 1]
        best = here
        best_score = base
        for dx, dy in probe_dirs:
            push = ROUTE_GAP_REPEL_STEP
            while push <= ROUTE_GAP_REPEL_MAX_TRANSLATE + 1e-9:
                candidate = (here[0] + dx * push, here[1] + dy * push)
                if not point_on_mesh(candidate):
                    break  # 此向已出界,不必再远
                if _segment_on_mesh((a[0], a[1]), candidate, point_on_mesh) and _segment_on_mesh(
                    candidate, (c[0], c[1]), point_on_mesh
                ):
                    saved = result[i]
                    result[i] = [candidate[0], candidate[1]]
                    within_cap = all(
                        turn_at(result, k) <= max(ROUTE_RELAX_TURN_CAP, origin_turn[k]) + 1e-6 for k in (i - 1, i, i + 1)
                    )
                    result[i] = saved
                    if within_cap:
                        beyond_gap = gap_distance(candidate)
                        score = safe + 1.0 if beyond_gap is None else beyond_gap
                        if score > best_score:
                            best = candidate
                            best_score = score
                push += ROUTE_GAP_REPEL_STEP
            if best_score >= safe:
                break  # 已够远,不必再试其它方向
        result[i] = [best[0], best[1]]
    return [tuple(point) for point in result], segment_breaks


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


def _segment_shortcut_has_clearance(
    a: tuple[float, float],
    b: tuple[float, float],
    point_on_mesh,
) -> bool:
    # 捷径横向余量校验:沿 a→b 等距采样,每个采样点须在网格内,且左右法向各保留 >= ROUTE_SHORTCUT_MIN_CLEARANCE
    # 的余量。高度连续的 height_walkable 只保证捷径中线落在网格上,无法阻止贴着 L 形拐角内侧水边切过去的
    # "贴边切线"——而拉直会删掉拐角顶点,事后居中只能整体平移、无从复原 L 形,故必须在拉直时拦下。
    dx = b[0] - a[0]
    dy = b[1] - a[1]
    length = math.hypot(dx, dy)
    if length < 1e-6 or point_on_mesh is None:
        return True
    normal_x = -dy / length
    normal_y = dx / length
    sample_count = max(1, int(math.ceil(length / ROUTE_SHORTCUT_CLEARANCE_SAMPLE_STEP)))
    for index in range(1, sample_count + 1):
        t = index / (sample_count + 1)
        sample = (a[0] + dx * t, a[1] + dy * t)
        if not point_on_mesh(sample):
            return False
        left = _max_offset_on_mesh(
            sample, normal_x, normal_y, ROUTE_SHORTCUT_CLEARANCE_PROBE_LIMIT, point_on_mesh, ROUTE_SHORTCUT_CLEARANCE_PROBE_STEP
        )
        right = _max_offset_on_mesh(
            sample, -normal_x, -normal_y, ROUTE_SHORTCUT_CLEARANCE_PROBE_LIMIT, point_on_mesh, ROUTE_SHORTCUT_CLEARANCE_PROBE_STEP
        )
        if min(left, right) < ROUTE_SHORTCUT_MIN_CLEARANCE:
            return False
    return True


def _thin_continuous_segment(
    points: list[tuple[float, float]],
    start: int,
    end: int,
    is_segment_walkable=None,
    point_on_mesh=None,
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
            # 中线可走还不够:侧向余量不足的捷径(贴 L 形拐角内侧水边切线)必须拒绝,保留原拐角。
            has_clearance = point_on_mesh is None or _segment_shortcut_has_clearance(
                points[anchor], points[probe], point_on_mesh
            )
            if is_segment_walkable(points[anchor], points[probe]) and has_clearance:
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
