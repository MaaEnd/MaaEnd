"""投影、多边形裁剪、去重叠与重心的纯数学函数。

这里只有数学，不读文件、不认识 prefab：输入是坐标和矩阵，输出是坐标和矩阵。数值上的容差只用来挡退化
输入（相邻顶点重复、面积趋于零），不当作安全距离；判断一律按真实边界（半平面）算。
"""

import math
from typing import Final

type Point = tuple[float, float]
type Vector3 = tuple[float, float, float]
type Quaternion = tuple[float, float, float, float]
type Matrix3 = tuple[tuple[float, float, float], ...]
type Matrix4 = tuple[tuple[float, float, float, float], ...]
type Polygon = list[Point]

EPS_LEN: Final[float] = 1e-9  # 像素：小于它的相邻顶点当作重复
EPS_AREA: Final[float] = 1e-7  # 平方像素：小于它的面积当作退化

IDENTITY4: Final[Matrix4] = (
    (1.0, 0.0, 0.0, 0.0),
    (0.0, 1.0, 0.0, 0.0),
    (0.0, 0.0, 1.0, 0.0),
    (0.0, 0.0, 0.0, 1.0),
)


# ── 四元数 / 4x4 矩阵 ──────────────────────────────────────────────────────


def quat_normalized(q: Quaternion) -> Quaternion:
    """Unity 的四元数按 (x, y, z, w) 存，导出时常有小数误差。

    统一归一化是一个明确的约定（对所有地区一样，不按地区例外）；长度为零的四元数不是旋转，直接报错，
    不能当成单位四元数糊过去。
    """
    x, y, z, w = q
    norm = math.sqrt(x * x + y * y + z * z + w * w)
    if norm <= EPS_LEN:
        raise ValueError("四元数长度为 0，不是旋转")
    return (x / norm, y / norm, z / norm, w / norm)


def quat_mul(a: Quaternion, b: Quaternion) -> Quaternion:
    """四元数乘法（Hamilton 积），分量顺序 (x, y, z, w)。"""
    x, y, z, w = a
    bx, by, bz, bw = b
    return (
        w * bx + x * bw + y * bz - z * by,
        w * by - x * bz + y * bw + z * bx,
        w * bz + x * by - y * bx + z * bw,
        w * bw - x * bx - y * by - z * bz,
    )


def quat_to_matrix3(q: Quaternion) -> Matrix3:
    x, y, z, w = quat_normalized(q)
    return (
        (1 - 2 * (y * y + z * z), 2 * (x * y - z * w), 2 * (x * z + y * w)),
        (2 * (x * y + z * w), 1 - 2 * (x * x + z * z), 2 * (y * z - x * w)),
        (2 * (x * z - y * w), 2 * (y * z + x * w), 1 - 2 * (x * x + y * y)),
    )


def mat4_mul(a: Matrix4, b: Matrix4) -> Matrix4:
    def cell(row: int, col: int) -> float:
        return a[row][0] * b[0][col] + a[row][1] * b[1][col] + a[row][2] * b[2][col] + a[row][3] * b[3][col]

    return (
        (cell(0, 0), cell(0, 1), cell(0, 2), cell(0, 3)),
        (cell(1, 0), cell(1, 1), cell(1, 2), cell(1, 3)),
        (cell(2, 0), cell(2, 1), cell(2, 2), cell(2, 3)),
        (cell(3, 0), cell(3, 1), cell(3, 2), cell(3, 3)),
    )


def trs_matrix(position: Vector3, rotation: Quaternion, scale: Vector3) -> Matrix4:
    """Unity 的 TRS：平移 @ 旋转 @ 缩放（列向量约定）。"""
    r = quat_to_matrix3(rotation)
    sx, sy, sz = scale
    return (
        (r[0][0] * sx, r[0][1] * sy, r[0][2] * sz, position[0]),
        (r[1][0] * sx, r[1][1] * sy, r[1][2] * sz, position[1]),
        (r[2][0] * sx, r[2][1] * sy, r[2][2] * sz, position[2]),
        (0.0, 0.0, 0.0, 1.0),
    )


def transform_point(m: Matrix4, p: Vector3) -> Vector3:
    x, y, z = p
    return (
        m[0][0] * x + m[0][1] * y + m[0][2] * z + m[0][3],
        m[1][0] * x + m[1][1] * y + m[1][2] * z + m[1][3],
        m[2][0] * x + m[2][1] * y + m[2][2] * z + m[2][3],
    )


def mat4_inverse_rigid(m: Matrix4) -> Matrix4:
    """仿射矩阵（旋转 + 缩放 + 平移）的逆：解 3x3 线性部分。

    相机 prefab 的 TRS 一定可逆；行列式为零或接近零时报错，不给一个凑出来的逆矩阵。
    """
    a = [[m[r][c] for c in range(3)] for r in range(3)]
    det = (
        a[0][0] * (a[1][1] * a[2][2] - a[1][2] * a[2][1])
        - a[0][1] * (a[1][0] * a[2][2] - a[1][2] * a[2][0])
        + a[0][2] * (a[1][0] * a[2][1] - a[1][1] * a[2][0])
    )
    if abs(det) <= EPS_LEN:
        raise ValueError("相机变换不可逆")
    inv = [[0.0] * 3 for _ in range(3)]
    for row in range(3):
        for col in range(3):
            # 代数余子式 / 行列式
            r0, r1 = [index for index in range(3) if index != row]
            c0, c1 = [index for index in range(3) if index != col]
            minor = a[r0][c0] * a[r1][c1] - a[r0][c1] * a[r1][c0]
            inv[col][row] = ((-1.0) ** (row + col)) * minor / det
    t = (m[0][3], m[1][3], m[2][3])
    inv_t = tuple(-sum(inv[r][k] * t[k] for k in range(3)) for r in range(3))
    return (
        (inv[0][0], inv[0][1], inv[0][2], inv_t[0]),
        (inv[1][0], inv[1][1], inv[1][2], inv_t[1]),
        (inv[2][0], inv[2][1], inv[2][2], inv_t[2]),
        (0.0, 0.0, 0.0, 1.0),
    )


# ── 投影 ───────────────────────────────────────────────────────────────────


def perspective_project(camera_space: Vector3, *, screen: Point, focal_y: float) -> tuple[float, float, float]:
    """整屏针孔投影（+Z 朝前，原点在屏幕中心，Y 轴向上，所以像素 Y 要翻）。

    ``focal_y = H / (2*tan(fov/2))`` 由垂直视场角决定。四个角各用自己的深度：倾斜的按钮如果把四角深度
    取平均，画面就会歪，这个函数就是为了让那种写法不可能出现。
    """
    x, y, z = camera_space
    if not (z > 0.0):
        raise ValueError(f"点在相机平面之后（z={z!r}）")
    f = focal_y
    return (screen[0] / 2.0 + f * x / z, screen[1] / 2.0 - f * y / z, z)


# ── 凸多边形运算 ───────────────────────────────────────────────────────────


def signed_area(poly: Polygon) -> float:
    return 0.5 * sum(a[0] * b[1] - b[0] * a[1] for a, b in zip(poly, poly[1:] + poly[:1], strict=True))


def clean(poly: Polygon) -> Polygon:
    """去掉重复顶点，拒收非有限值或退化多边形，并统一成有向面积为正（屏幕 Y 向下即视觉顺时针）。"""
    out: Polygon = []
    for p in poly:
        q = (float(p[0]), float(p[1]))
        if not all(math.isfinite(v) for v in q):
            raise ValueError("多边形坐标不是有限数")
        if not out or math.dist(q, out[-1]) > EPS_LEN:
            out.append(q)
    if len(out) > 1 and math.dist(out[0], out[-1]) <= EPS_LEN:
        _ = out.pop()
    if len(out) < 3 or abs(signed_area(out)) <= EPS_AREA:
        return []
    area = signed_area(out)
    return out if area > 0 else out[::-1]


def clip_halfplane(poly: Polygon, a: Point, b: Point, *, inside: bool) -> Polygon:
    """Sutherland-Hodgman 单步裁剪：按有向直线 a->b 切一刀，留下指定一侧。"""
    if not poly:
        return []
    dx, dy = b[0] - a[0], b[1] - a[1]
    length = math.hypot(dx, dy)
    if length <= EPS_LEN:
        raise ValueError("裁剪边长度为 0")
    sign = 1.0 if inside else -1.0

    def distance(p: Point) -> float:
        return sign * (dx * (p[1] - a[1]) - dy * (p[0] - a[0])) / length

    out: Polygon = []
    previous = poly[-1]
    dp = distance(previous)
    for current in poly:
        dc = distance(current)
        if (dp >= 0) != (dc >= 0):
            t = dp / (dp - dc)
            out.append(
                (
                    previous[0] + t * (current[0] - previous[0]),
                    previous[1] + t * (current[1] - previous[1]),
                )
            )
        if dc >= 0:
            out.append(current)
        previous, dp = current, dc
    return clean(out)


def subtract_convex(a: Polygon, b: Polygon) -> list[Polygon]:
    """凸多边形 A 减去凸多边形 B，结果是若干互不重叠的凸块。

    沿 B 的每条边各切一次：每次剥掉剩余部分落在这条边外侧的那块，继续处理留在内侧的部分。剥下来的块
    恰好覆盖 A-B；B 完全盖住 A 时什么都不剩。
    """
    remaining = clean(a)
    cutter = clean(b)
    if not remaining:
        return []
    if not cutter:
        return [remaining]
    result: list[Polygon] = []
    for x, y in zip(cutter, cutter[1:] + cutter[:1], strict=True):
        outer = clip_halfplane(remaining, x, y, inside=False)
        if outer:
            result.append(outer)
        remaining = clip_halfplane(remaining, x, y, inside=True)
        if not remaining:
            break
    return result


def clip_to_rect(poly: Polygon, rect_max: Point, rect_min: Point) -> Polygon:
    """把凸多边形裁到轴对齐矩形 [rect_min, rect_max] 里。"""
    x0, y0 = rect_min
    x1, y1 = rect_max
    result = clean(poly)
    for a, b in (
        ((x0, y0), (x1, y0)),
        ((x1, y0), (x1, y1)),
        ((x1, y1), (x0, y1)),
        ((x0, y1), (x0, y0)),
    ):
        result = clip_halfplane(result, a, b, inside=True)
        if not result:
            break
    return result


def exclusive_cells(polygons: dict[str, Polygon]) -> dict[str, list[Polygon]]:
    """每块多边形减去「其它所有多边形的并集」，得到只属于它的格。

    减的时候一律用其它多边形**原始**的形状，不用已经减过的结果，所以结果与遍历顺序无关；按地区键排序
    只是为了输出顺序固定。
    """
    output: dict[str, list[Polygon]] = {}
    for key in sorted(polygons):
        cells = [clean(polygons[key])] if polygons[key] else []
        for other in sorted(polygons):
            if other == key:
                continue
            if not cells:
                break
            cells = [piece for cell in cells for piece in subtract_convex(cell, polygons[other])]
        output[key] = cells
    return output


def area_and_centroid(poly: Polygon) -> tuple[float, Point | None]:
    poly = clean(poly)
    if not poly:
        return 0.0, None
    pairs = list(zip(poly, poly[1:] + poly[:1], strict=True))
    cross = [a[0] * b[1] - b[0] * a[1] for a, b in pairs]
    area = sum(cross) / 2
    cx = sum((a[0] + b[0]) * s for (a, b), s in zip(pairs, cross, strict=True)) / (6 * area)
    cy = sum((a[1] + b[1]) * s for (a, b), s in zip(pairs, cross, strict=True)) / (6 * area)
    return area, (cx, cy)


def cells_area_and_centroid(cells: list[Polygon]) -> tuple[float, Point | None]:
    terms = [area_and_centroid(p) for p in cells]
    total = sum(area for area, _ in terms)
    if total <= EPS_AREA:
        return 0.0, None
    cx = sum(area * c[0] for area, c in terms if c is not None) / total
    cy = sum(area * c[1] for area, c in terms if c is not None) / total
    return total, (cx, cy)


def point_in_polygon(p: Point, poly: Polygon) -> bool:
    """含边界判断：落在边上算在内部——重心本来就可能正好落在两块共用的边界上。"""
    inside = False
    n = len(poly)
    for i in range(n):
        a, b = poly[i], poly[(i + 1) % n]
        if _point_on_segment(p, a, b):
            return True
        if (a[1] > p[1]) != (b[1] > p[1]):
            t = (p[1] - a[1]) / (b[1] - a[1])
            if p[0] < a[0] + t * (b[0] - a[0]):
                inside = not inside
    return inside


def _point_on_segment(p: Point, a: Point, b: Point) -> bool:
    cross = (b[0] - a[0]) * (p[1] - a[1]) - (b[1] - a[1]) * (p[0] - a[0])
    if abs(cross) > 1e-9:
        return False
    dot = (p[0] - a[0]) * (b[0] - a[0]) + (p[1] - a[1]) * (b[1] - a[1])
    if dot < 0:
        return False
    length_sq = (b[0] - a[0]) ** 2 + (b[1] - a[1]) ** 2
    return dot <= length_sq


def point_segment_distance(p: Point, a: Point, b: Point) -> float:
    """点到**线段** ab 的距离（参数夹在 [0,1]）：不是点到延长线所在直线的距离。"""
    dx, dy = b[0] - a[0], b[1] - a[1]
    length_sq = dx * dx + dy * dy
    if length_sq <= EPS_LEN:
        return math.dist(p, a)
    t = ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / length_sq
    t = min(1.0, max(0.0, t))
    return math.dist(p, (a[0] + t * dx, a[1] + t * dy))


def polygon_boundary_distance(p: Point, poly: Polygon) -> float:
    poly = clean(poly)
    if not poly:
        return math.inf
    return min(point_segment_distance(p, a, b) for a, b in zip(poly, poly[1:] + poly[:1], strict=True))
