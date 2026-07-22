from __future__ import annotations

import gzip
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
SMALL_BRIDGE_COMPONENT_MAX_TRIANGLES = 512
SMALL_BRIDGE_MAX_GAP = 4.0
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


def _report_progress(callback, progress: float) -> None:
    if callback is not None:
        try:
            callback(progress)
        except Exception:
            pass


def load_basenav_field(input_file: Path, progress_callback=None) -> BaseNavField:
    return BaseNavField(input_file, progress_callback=progress_callback)


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
        # tiers and geometry zones). Callers pass this into snap/route to scope routing
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


def _point_distance(lhs: tuple[float, float], rhs: tuple[float, float]) -> float:
    return math.hypot(lhs[0] - rhs[0], lhs[1] - rhs[1])


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
