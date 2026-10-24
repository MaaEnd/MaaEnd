"""屏幕裁剪、去重叠、面积重心和安全的整数点击点。"""

import math

from .errors import GenerationError
from .geometry import (
    EPS_AREA,
    Point,
    Polygon,
    cells_area_and_centroid,
    clean,
    clip_to_rect,
    exclusive_cells,
    point_in_polygon,
    polygon_boundary_distance,
)

CANDIDATE_OFFSETS = ((0, 0), (-1, 0), (0, -1), (1, 0), (0, 1), (-1, -1), (1, -1), (1, 1), (-1, 1))


def derive_clicks(polygons: dict[str, Polygon], *, screen: Point, min_clearance: float) -> dict[str, tuple[int, int]]:
    """每个子地图一个点击点：先裁到屏幕、去掉同地区其它地图的重叠，再在重心附近挑整数像素。"""
    clipped = {key: clip_to_rect(poly, screen, (0.0, 0.0)) for key, poly in polygons.items()}
    cells = exclusive_cells(clipped)
    clicks: dict[str, tuple[int, int]] = {}
    for key in sorted(clipped):
        area, center = cells_area_and_centroid(cells.get(key, []))
        if area <= EPS_AREA or center is None:
            raise GenerationError(f"{key}：去掉重叠后没有可点面积")
        # 去重叠用原始多边形统一算；安全距离也对真实边界量，不把切出来的分界线当边界。
        try:
            clicks[key] = choose_click_point(
                clipped[key],
                {other: clipped[other] for other in clipped if other != key},
                geometric_center=center,
                min_clearance=min_clearance,
                screen=screen,
            )
        except GenerationError as error:
            raise GenerationError(f"{key}：{error}") from error
    return clicks


def round_half_up(value: float) -> int:
    """就近取整，``.5`` 一律往远离零的方向走：不用 Python 的银行家舍入，免得结果取决于相邻整数的奇偶。"""
    return math.floor(value + 0.5) if value >= 0 else math.ceil(value - 0.5)


def net_clearance(point: Point, own: Polygon, others: dict[str, Polygon]) -> float | None:
    """*point* 到最终可点区域边界的距离，用原始多边形表达。

    点必须在 *own* 里、且在其它每个多边形之外；距离取「到自己边界」和「到其它每个边界」中的最小。点压根
    不在可点区域时返回 ``None``。
    """
    if not own or not point_in_polygon(point, own):
        return None
    value = polygon_boundary_distance(point, own)
    for other in others.values():
        polygon = clean(other)
        if not polygon:
            continue
        if point_in_polygon(point, polygon):
            return None
        value = min(value, polygon_boundary_distance(point, polygon))
    return value


def _candidates(center: Point, offsets: tuple[tuple[int, int], ...]) -> list[tuple[float, int, int]]:
    """先取四舍五入后的重心，再取它周围八个点；按到重心的距离、再按 y、再按 x 排序，并列时与遍历顺序无关。"""
    base_x, base_y = round_half_up(center[0]), round_half_up(center[1])
    seen: set[tuple[int, int]] = set()
    scored: list[tuple[float, int, int]] = []
    for dx, dy in offsets:
        x, y = base_x + dx, base_y + dy
        if (x, y) in seen:
            continue
        seen.add((x, y))
        scored.append((math.dist((x, y), center), y, x))
    scored.sort()
    return scored


def choose_click_point(
    own: Polygon,
    others: dict[str, Polygon],
    *,
    geometric_center: Point | None,
    min_clearance: float,
    screen: Point,
) -> tuple[int, int]:
    """挑一个核对过的整数像素。

    顺序是：重心取整点和它的八个邻居，都不满足安全距离时，再在可点区域的包围盒里找净距最大的整数点。
    每个候选都对着**原始**多边形核对；不吸附、不悄悄降低安全距离。
    """
    if geometric_center is None:
        raise GenerationError("没有重心可用作起点")

    for _, y, x in _candidates(geometric_center, CANDIDATE_OFFSETS):
        point = (float(x), float(y))
        clearance = net_clearance(point, own, others)
        if clearance is None or clearance < min_clearance:
            continue
        if not _on_screen(x, y, screen):
            continue
        return x, y

    best = _max_clearance_integer(own, others, min_clearance=min_clearance, screen=screen)
    if best is not None:
        return best
    raise GenerationError("没有满足安全距离的整数像素")


def _on_screen(x: int, y: int, screen: Point) -> bool:
    return 0 <= x < int(screen[0]) and 0 <= y < int(screen[1])


def _max_clearance_integer(
    own: Polygon, others: dict[str, Polygon], *, min_clearance: float, screen: Point
) -> tuple[int, int] | None:
    if not own:
        return None
    xs = [p[0] for p in own]
    ys = [p[1] for p in own]
    x_start = max(0, math.ceil(min(xs)))
    x_end = min(int(screen[0]) - 1, math.floor(max(xs)))
    y_start = max(0, math.ceil(min(ys)))
    y_end = min(int(screen[1]) - 1, math.floor(max(ys)))
    best: tuple[float, int, int] | None = None
    for y in range(y_start, y_end + 1):
        for x in range(x_start, x_end + 1):
            point = (float(x), float(y))
            if not point_in_polygon(point, own):
                continue
            clearance = net_clearance(point, own, others)
            if clearance is None or clearance < min_clearance:
                continue
            # 净距并列时选最小的 (y, x)，与遍历顺序无关。
            candidate = (clearance, -y, -x)
            if best is None or candidate > best:
                best = candidate
    if best is None:
        return None
    _, negative_y, negative_x = best
    return -negative_x, -negative_y
