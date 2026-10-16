"""MaaEnd 精简游戏数据共用件：世界坐标读取。

带坐标的数据（``delivery_destinations`` / ``teleport_anchors`` / ``collect_points``）从 TableCfg /
GameplayConfig 里读出游戏世界坐标 x/z（送货点另有世界高度 y）。投影参数（``zone`` / ``sx`` / ``tx`` /
``sy`` / ``ty``）与底图像素属于 BaseNav pack，只有 MaaEnd 手里有，换算由入库步骤
（``tools/zmdmap/project.py``）完成；数据文件里不放字段说明，说明写在 ``datasets/`` 的模块与文档里。
"""

from typing import Any, cast


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
