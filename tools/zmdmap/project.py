"""把 ``data/raw/`` 里的原始数据材料化进入库的 ``data/*.json``。

入库看数据集是否需要投影（``datasets/`` 里的 ``Dataset.needs_projection``）：需要投影的按 zone 表补底图
像素（:func:`project_data`），不需要投影的由 :func:`copy_files` 原样复制。

带坐标的数据只发布游戏世界坐标 x/z（送货点另有世界高度 y），而底图像素与投影参数
（``zone`` / ``sx`` / ``tx`` / ``sy`` / ``ty``）都在 BaseNav 包里 —— 只有 MaaEnd 手里有。所以
下载或本地生成出来的原始数据要再过这一步：把 x/z 原位换成底图像素 ``u = sx*x + tx``、
``v = -sy*z + ty``（``y`` 是世界高度，原样留着），并把本次用到的 zone 写进 ``maps``。

数据文件里只有数据字段，字段含义写在 ``datasets/`` 的模块与各任务文档里；最前面两个元数据字段是例外：
``version``（这份数据来自哪一版游戏数据）与 ``sha256``（数据本体的内容指纹，投影后按投影后的本体重算），
有了它们就不必再另外记「本地是哪一版、内容是哪一份」。

输出用 Python 的 ``json`` 序列化（``ensure_ascii=False``、缩进 4、LF 换行）：整型浮点会带小数点
（``520.0`` 而不是 ``520``），解析出来的值与键顺序不受影响。
"""

from __future__ import annotations

import hashlib
import json
import math
import sys
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from .constants import MANIFEST_FILE
from .datasets import PROJECT_FILES, dataset_of
from .navmesh import NavZone, load_nav_zones
from .paths import ZMDMAP_DATA_DIR, REPO_ROOT

#: 数据文件最前面的两个元数据字段：这份数据来自哪一版游戏数据，以及数据本体的内容指纹。
#: ``sha256`` 只用来说明「内容是不是同一份」，不参与业务，消费端可以忽略。
VERSION_KEY = "version"
HASH_KEY = "sha256"
METADATA_KEYS = (VERSION_KEY, HASH_KEY)

#: 投影前的世界坐标轴与投影后原位顶替它的底图像素轴，一一对应（x -> u、z -> v）。
#: ``y`` 是世界高度，不参与换算，原样保留。
WORLD_AXES = ("x", "z")
PIXEL_AXES = ("u", "v")


def dump_json(value: Any) -> str:
    """入库 JSON 的写法：缩进 4、非 ASCII 原样输出、结尾一个换行。"""
    return json.dumps(value, ensure_ascii=False, indent=4) + "\n"


def payload_of(data: Any) -> Any:
    """去掉最前面的元数据字段之后的数据本体。"""
    if not isinstance(data, dict):
        return data
    return {key: value for key, value in data.items() if key not in METADATA_KEYS}


def payload_hash(payload: Any) -> str:
    """数据本体的 sha256：键序无关（排序后紧凑序列化），数值写法与数组顺序照旧。

    与文件怎么排版无关，所以重新格式化不会换指纹；同一份内容无论谁生成，指纹都一样。
    """
    text = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def version_of(data: Any) -> str | None:
    """解析好的数据里记的版本；没有、或不是非空字符串时返回 ``None``。"""
    value = data.get(VERSION_KEY) if isinstance(data, dict) else None
    return value if isinstance(value, str) and value else None


def read_version(path: Path) -> str | None:
    """读数据文件里记的版本；读不到（没文件 / 没字段 / 字段不是非空字符串）返回 ``None``。"""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    return version_of(data)


def read_payload_hash(path: Path) -> str | None:
    """读数据文件的本体指纹（按文件里的内容重算，不信它自称的那个）；读不到返回 ``None``。"""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None
    return payload_hash(payload_of(data))


def read_payload(path: Path) -> Any:
    """读数据文件里的数据本体：去掉最前面的元数据字段，读不到就报错。

    别的工具读 ``tools/zmdmap/data/*.json`` 用这个（只要本体时也可以用 :func:`payload_of` 处理自己读进来
    的 JSON），不必自己知道元数据是哪几个字段。
    """
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except OSError as error:
        raise ValueError(
            f"缺少 zmdmap 数据 {path}（先跑 uv run zmdmap fetch，或 uv run zmdmap generate）"
        ) from error
    except json.JSONDecodeError as error:
        raise ValueError(f"{path} 不是合法 JSON：{error}") from error
    return payload_of(data)


def write_manifest(out_dir: Path, file_names: Sequence[str], version: str | None) -> Path | None:
    """把发布清单写进 ``out_dir``：版本 + 每份原始形态的本体指纹；内容没变时返回 ``None``。

    清单是发布端与 ``fetch`` 之间的约定：有了它就不必再问版本接口，也能认出「同一个版本号下重新发布过」。
    清单里收哪些文件按 ``out_dir`` 里实际存在的算，所以只生成一部分时清单照样是完整的。
    """
    files = {
        file_name: digest
        for file_name in sorted(file_names)
        if (digest := read_payload_hash(out_dir / file_name)) is not None
    }
    manifest: dict[str, Any] = {}
    if version:
        manifest[VERSION_KEY] = version
    manifest["files"] = files

    path = out_dir / MANIFEST_FILE
    if is_unchanged(path, manifest):
        return None
    path.write_text(dump_json(manifest), encoding="utf-8", newline="\n")
    return path


def with_metadata(data: Any, version: str | None = None) -> Any:
    """把版本与内容指纹写在数据本体前面；``version`` 不给时沿用数据里已有的版本。

    ``sha256`` 按传进来的数据本体重算，所以对已经是「元数据 + 本体」形态的数据也安全（幂等）。
    """
    payload = payload_of(data)
    if not isinstance(payload, dict):
        return payload
    metadata: dict[str, Any] = {}
    stamp = version or version_of(data)
    if stamp:
        metadata[VERSION_KEY] = stamp
    metadata[HASH_KEY] = payload_hash(payload)
    return {**metadata, **payload}


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
    data: Any,
    zones: dict[str, NavZone],
    skipped_maps: set[str],
) -> Any:
    """投影一份数据；不需要投影的数据集原样返回。"""
    dataset = dataset_of(file_name)
    if dataset is None or not dataset.needs_projection:
        return data

    payload = payload_of(data)
    used_zones: dict[str, dict[str, Any]] = {}
    projected = dict(payload)
    for container in dataset.projection_containers:
        projected[container] = project_entries(
            payload.get(container) if isinstance(payload, dict) else None,
            zones,
            used_zones,
            skipped_maps,
            f"{file_name}[{container}]",
        )
    projected["maps"] = describe_zones(used_zones)
    # 投影之后内容变了，指纹按投影后的本体重算；版本沿用原始数据那份。
    return with_metadata(projected, version_of(data))


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
    if skipped_maps:
        names = ", ".join(sorted(skipped_maps))
        print(f"[project] 以下底图没有对应的 BaseNav zone，其条目未纳入：{names}", file=sys.stderr)
    return {"written": len(pending), "skippedMaps": sorted(skipped_maps)}
