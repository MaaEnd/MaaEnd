"""把 ``data/raw/`` 里的原始数据材料化进入库的 ``data/*.json``（原 ``project.mjs``）。

入库看数据集是否需要投影（``datasets/`` 里的 ``Dataset.needs_projection``）：需要投影的按 zone 表补底图
像素（:func:`project_data`），不需要投影的由 :func:`copy_files` 原样复制。

带坐标的数据只发布游戏世界坐标 x/z（送货点另有世界高度 y），而底图像素与投影参数
（``zone`` / ``sx`` / ``tx`` / ``sy`` / ``ty``）都在 BaseNav 包里 —— 只有 MaaEnd 手里有。所以
下载或本地生成出来的原始数据要再过这一步：把 x/z 原位换成底图像素 ``u = sx*x + tx``、
``v = -sy*z + ty``（``y`` 是世界高度，原样留着），并把本次用到的 zone 写进 ``maps``。

说明字段不再进数据文件：字段含义写在 ``datasets/`` 的模块与文档里。

输出用 Python 的 ``json`` 序列化（``ensure_ascii=False``、缩进 4）：与旧 Node 实现只在数字写法上
有差别（整型浮点写成 ``520.0`` 而不是 ``520``），不影响语义。
"""

from __future__ import annotations

import json
import math
import sys
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from .datasets import PROJECT_FILES, dataset_of
from .navmesh import NavZone, load_nav_zones
from .paths import ZMDMAP_DATA_DIR, REPO_ROOT

VERSION_FILE = "version.txt"

#: 投影前的世界坐标轴与投影后原位顶替它的底图像素轴，一一对应（x -> u、z -> v）。
#: ``y`` 是世界高度，不参与换算，原样保留。
WORLD_AXES = ("x", "z")
PIXEL_AXES = ("u", "v")


def dump_json(value: Any) -> str:
    """入库 JSON 的写法：缩进 4、非 ASCII 原样输出、结尾一个换行。"""
    return json.dumps(value, ensure_ascii=False, indent=4) + "\n"


def round3(value: float) -> float:
    """按精确值取三位小数、逢半取偶（``round`` 就是这个语义，旧 Node 实现是它的等价模拟）。"""
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
                f"{label} 的原始数据只有 u/v、没有世界坐标 x/z：上游还是旧格式的发布数据，"
                "等 zmdmap 重新发布后再跑 uv run zmdmap fetch"
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
    data: Any,
    zones: dict[str, NavZone],
    skipped_maps: set[str],
) -> Any:
    """投影一份数据；不需要投影的数据集原样返回。"""
    dataset = dataset_of(file_name)
    if dataset is None or not dataset.needs_projection:
        return data

    used_zones: dict[str, dict[str, Any]] = {}
    projected = dict(data)
    for container in dataset.projection_containers:
        projected[container] = project_entries(
            data.get(container) if isinstance(data, dict) else None,
            zones,
            used_zones,
            skipped_maps,
            f"{file_name}[{container}]",
        )
    projected["maps"] = describe_zones(used_zones)
    return projected


def select_files(only: tuple[str, ...] | list[str] | None) -> list[str]:
    """``--only`` 选出的文件名；没给就是全部。"""
    if not only:
        return list(PROJECT_FILES)
    files: list[str] = []
    for keyword in only:
        file_name = keyword if keyword.endswith(".json") else f"{keyword}.json"
        if file_name not in PROJECT_FILES:
            choices = ", ".join(name.removesuffix(".json") for name in PROJECT_FILES)
            raise ValueError(f"未知数据集 {keyword}；可选：{choices}")
        if file_name not in files:
            files.append(file_name)
    return files


def read_dataset(path: Path) -> Any:
    # 超出 Number 安全范围的整数在这里天然是精确的 Python int，不会掉精度。
    return json.loads(path.read_text(encoding="utf-8"))


def is_unchanged(output_path: Path, data: Any) -> bool:
    if not output_path.exists():
        return False
    try:
        return output_path.read_text(encoding="utf-8") == dump_json(data)
    except (OSError, UnicodeDecodeError):
        return False


def copy_version(raw_dir: Path, out_dir: Path, force: bool) -> None:
    """把原始数据的版本号带到入库目录（``version.txt`` 不带尾换行）。"""
    source = raw_dir / VERSION_FILE
    if not source.exists():
        print(f"[project] 没有 {source}，保留 data/{VERSION_FILE} 不变", file=sys.stderr)
        return
    version = source.read_text(encoding="utf-8").strip()
    target = out_dir / VERSION_FILE
    if not force and target.exists() and target.read_text(encoding="utf-8").strip() == version:
        return
    target.write_text(version, encoding="utf-8", newline="\n")
    print(f"[project] {VERSION_FILE} -> {target}")


def copy_files(raw_dir: Path, out_dir: Path, file_names: Sequence[str], *, force: bool = False) -> int:
    """不需要投影的数据：原样从 ``raw_dir`` 复制进 ``out_dir``；返回写入份数。"""
    out_dir.mkdir(parents=True, exist_ok=True)
    written = 0
    for file_name in file_names:
        source_path = raw_dir / file_name
        if not source_path.exists():
            raise ValueError(
                f"缺少原始数据文件：{source_path}"
                "（先跑 uv run zmdmap fetch 下载，或 uv run zmdmap generate 本地生成）"
            )
        text = source_path.read_text(encoding="utf-8")
        output_path = out_dir / file_name
        if not force and output_path.exists() and output_path.read_text(encoding="utf-8") == text:
            print(f"[project] {file_name} 未变化，跳过写入")
            continue
        output_path.write_text(text, encoding="utf-8", newline="\n")
        print(f"[project] {file_name} -> {output_path}")
        written += 1
    return written


def project_data(
    raw_dir: Path,
    out_dir: Path = ZMDMAP_DATA_DIR,
    *,
    nav: str | None = None,
    only: tuple[str, ...] | list[str] | None = None,
    force: bool = False,
    repo_root: Path = REPO_ROOT,
) -> dict[str, Any]:
    """把 ``raw_dir`` 里带坐标的原始数据投影到 ``out_dir``（缺省就是入库的 data/）。"""
    files = select_files(only)
    zones = load_nav_zones(nav, repo_root) if files else {}

    skipped_maps: set[str] = set()
    pending: list[tuple[str, Path, Any]] = []
    for file_name in files:
        source_path = raw_dir / file_name
        if not source_path.exists():
            raise ValueError(
                f"缺少原始数据文件：{source_path}"
                "（先跑 uv run zmdmap fetch 下载，或 uv run zmdmap generate 本地生成）"
            )
        result = project_dataset(file_name, read_dataset(source_path), zones, skipped_maps)
        output_path = out_dir / file_name
        if not force and is_unchanged(output_path, result):
            print(f"[project] {file_name} 未变化，跳过写入")
            continue
        pending.append((file_name, output_path, result))

    out_dir.mkdir(parents=True, exist_ok=True)
    for file_name, output_path, result in pending:
        output_path.write_text(dump_json(result), encoding="utf-8", newline="\n")
        print(f"[project] {file_name} -> {output_path}")
    if len(files) == len(PROJECT_FILES):
        copy_version(raw_dir, out_dir, force)
    if skipped_maps:
        names = ", ".join(sorted(skipped_maps))
        print(f"[project] 以下底图没有对应的 BaseNav zone，其条目未纳入：{names}", file=sys.stderr)
    return {"written": len(pending), "skippedMaps": sorted(skipped_maps)}
