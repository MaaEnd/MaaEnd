"""MaaEnd 精简游戏数据共用件：世界坐标读取 + ``coord`` 字段文案。

六份数据里带坐标的三份（``delivery_destinations`` / ``teleport_anchors`` /
``collect_points``）只发布游戏世界坐标 x/z（送货点另有世界高度 y）：投影参数
（``zone`` / ``sx`` / ``tx`` / ``sy`` / ``ty``）与底图像素都属于 BaseNav pack，只有
MaaEnd 手里有，换算由 MaaEnd 侧 ``tools/pipeline-generate/fetch-data.mjs`` 在下载完
发布数据后完成。本文件与 MaaEnd 的 ``tools/pipeline-generate/data/scripts/coords.py``
逐字对应（只差 import 行）。
"""

from typing import Any, cast

# 三份带坐标数据的 coord 说明（MaaEnd data/scripts/coords.py::COORD_TEXT 同文）。
COORD_TEXT = (
    "x/z = 游戏世界坐标（米）；数据本身不含底图像素，投影由消费侧按 BaseNav pack 的 "
    "zone 表完成：u = sx*x + tx, v = -sy*z + ty。map 是底图 ID"
    "（map01 / map02 / base01 / indie_dgXXX …），没有对应 zone 的底图由消费侧跳过"
)
# 送货点数据的 coord 在通用说明后再补一句 y 的用途（两边同名常量逐字一致）。
COORD_Y_NOTE = (
    "y 是未换算的世界高度，直接作为 NAVMESH 终点的 target_deck_y 用来在重叠可走面中选中目标层"
)


class CoordinateError(ValueError):
    """坐标记录缺字段或不是数值。"""


def _read_axes(pos: Any, axes: tuple[str, ...], label: str) -> tuple[float, ...]:
    if not isinstance(pos, dict):
        raise CoordinateError(f"{label} 不是坐标记录")
    record = cast("dict[str, Any]", pos)
    values: list[float] = []
    for axis in axes:
        number = record.get(axis)
        if not isinstance(number, (int, float)) or isinstance(number, bool):
            raise CoordinateError(f"{label} 缺少或不是数值的 {axis}")
        values.append(float(number))
    return tuple(values)


def read_xz(pos: Any, label: str) -> tuple[float, float]:
    """从坐标记录取 (x, z)；缺字段时报出是哪条数据。"""
    x, z = _read_axes(pos, ("x", "z"), label)
    return x, z


def read_xyz(pos: Any, label: str) -> tuple[float, float, float]:
    """从坐标记录取 (x, y, z)；y 是世界高度，原样透传给 NAVMESH 终点。"""
    x, y, z = _read_axes(pos, ("x", "y", "z"), label)
    return x, y, z
