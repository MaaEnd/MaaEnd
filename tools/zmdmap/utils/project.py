"""投影：把原始数据里的游戏世界坐标换成 MaaEnd 底图的像素坐标。

带坐标的数据只发布游戏世界坐标 x/z（送货点另有世界高度 y），而底图像素与投影参数
（``zone`` / ``sx`` / ``tx`` / ``sy`` / ``ty``）都在 BaseNav 包里 —— 只有 MaaEnd 手里有。所以下载或
本地生成出来的原始数据要再过这一步：把 x/z 原位换成底图像素 ``u = sx*x + tx``、``v = -sy*z + ty``
（``y`` 是世界高度，原样留着），并把本次用到的 zone 写进 ``maps``。

这里只做投影本身：读原始数据、把投影结果写进入库目录见 :mod:`tools.zmdmap.materialize`（``version`` /
``sha256`` 原样沿用原始形态那份，投影之后不重算），元数据的形态见 :mod:`tools.zmdmap.utils.datafile`。
要不要投影由 ``datasets/`` 里那份数据的 ``needs_projection`` 与 ``projection_containers`` 说了算。
"""

from __future__ import annotations

import math
from typing import Any

from ..datasets import dataset_of
from .navmesh import NavZone

#: 投影前的世界坐标轴与投影后原位顶替它的底图像素轴，一一对应（x -> u、z -> v）。
#: ``y`` 是世界高度，不参与换算，原样保留。
WORLD_AXES = ("x", "z")
PIXEL_AXES = ("u", "v")


def round3(value: float) -> float:
    """按精确值取三位小数、逢半取偶（Python 的 ``round`` 就是这个语义）。"""
    return round(value, 3)


def _read_axes(entry: dict[str, Any], axes: tuple[str, ...], label: str) -> list[float]:
    values: list[float] = []
    for axis in axes:
        value = entry.get(axis)
        if not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value):
            raise ValueError(f"{label} 的 {axis} 不是数值")
        values.append(float(value))
    return values


def replace_world_axes(entry: dict[str, Any], u: float, v: float) -> dict[str, Any]:
    """按原键序把世界坐标换成底图像素：``x`` 的位置给 ``u``、``z`` 的位置给 ``v``，其余字段原样。"""
    renames = dict(zip(WORLD_AXES, PIXEL_AXES, strict=True))
    pixels = dict(zip(WORLD_AXES, (u, v), strict=True))
    replaced: dict[str, Any] = {}
    for key, value in entry.items():
        replaced[renames.get(key, key)] = pixels.get(key, value)
    return replaced


def resolve_zone(zones: dict[str, NavZone], map_id: str) -> dict[str, Any]:
    """按 ``<map>`` / ``<map>base`` 找 zone。"""
    for name in (map_id, f"{map_id}base"):
        if name in zones:
            return {"name": name, **zones[name]}
    raise ValueError(f"没有 {map_id} 对应的 zone")


def project_to_pixel(zone: dict[str, Any], x: float, z: float, label: str) -> list[float]:
    u = zone["sx"] * x + zone["tx"]
    v = -zone["sy"] * z + zone["ty"]
    width, height = zone["size"]
    if not (u >= 0 and u < width and v >= 0 and v < height):
        raise ValueError(
            f"{label} 投影落在 {zone['name']} 底图外：u={u:.1f} v={v:.1f}"
            f"（图 {width}x{height}）；请确认几份输入取自同一版本"
        )
    return [round3(u), round3(v)]


def project_entries(
    entries: Any,
    zones: dict[str, NavZone],
    used_zones: dict[str, dict[str, Any]],
    skipped_maps: set[str],
    label: str,
) -> list[dict[str, Any]]:
    if not isinstance(entries, list):
        raise ValueError(f"{label} 不是数组")

    projected: list[dict[str, Any]] = []
    for entry in entries:
        if not isinstance(entry, dict):
            raise ValueError(f"{label} 条目不是对象")
        map_id = entry.get("map")
        if not isinstance(map_id, str) or not map_id:
            raise ValueError(f"{label} 条目的 map 不是字符串")
        if "x" not in entry and "u" in entry:
            raise ValueError(
                f"{label} 的原始数据是投影过的形态（只有 u/v、没有 x/z）：data/raw/ 里这份不是当前发布的"
                "原始形态，等 zmdmap 重新发布后再跑 uv run zmdmap fetch"
            )

        zone = used_zones.get(map_id)
        if zone is None:
            try:
                zone = resolve_zone(zones, map_id)
            except ValueError:
                skipped_maps.add(map_id)
                continue
            used_zones[map_id] = zone

        entry_id = entry.get("id")
        if entry_id is None:
            entry_id = ""
        x, z = _read_axes(entry, WORLD_AXES, f"{label} {entry_id}")
        u, v = project_to_pixel(zone, x, z, f"{label} {entry_id}")
        projected.append(replace_world_axes(entry, u, v))
    return projected


def describe_zones(used_zones: dict[str, dict[str, Any]]) -> dict[str, Any]:
    return {
        map_id: {
            "zone": used_zones[map_id]["name"],
            "zone_id": used_zones[map_id]["zone_id"],
            "size": list(used_zones[map_id]["size"]),
            "sx": used_zones[map_id]["sx"],
            "tx": used_zones[map_id]["tx"],
            "sy": used_zones[map_id]["sy"],
            "ty": used_zones[map_id]["ty"],
        }
        for map_id in sorted(used_zones)
    }


def project_dataset(
    file_name: str,
    payload: Any,
    zones: dict[str, NavZone],
    skipped_maps: set[str],
) -> Any:
    """投影一份数据的**本体**；不需要投影的数据集原样返回，本体不是对象时报 ``ValueError``。"""
    dataset = dataset_of(file_name)
    if dataset is None or not dataset.needs_projection:
        return payload

    if not isinstance(payload, dict):
        # 本体按约定是「容器名 -> 条目」的对象；不是就当数据坏了报明确错误。若先 dict(payload)，
        # 数组 / null 会抛 TypeError，而 run_projection 只接 OSError / ValueError，用户会看到栈。
        raise ValueError(f"{file_name} 的本体不是对象，没法投影：拿到 {type(payload).__name__}")

    used_zones: dict[str, dict[str, Any]] = {}
    projected = dict(payload)
    for container in dataset.projection_containers:
        projected[container] = project_entries(
            payload.get(container),
            zones,
            used_zones,
            skipped_maps,
            f"{file_name}[{container}]",
        )
    projected["maps"] = describe_zones(used_zones)
    return projected
