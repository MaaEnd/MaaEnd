"""生成 delivery_destinations.json。"""

from __future__ import annotations

import argparse
import gzip
import json
import re
import struct
import sys
import urllib.error
import urllib.request
import zlib
from pathlib import Path
from typing import Any, Sequence

from tablecfg_utils import (
    DATA_DIR,
    LOCALE_TABLES,
    TableCfgError,
    assert_list,
    assert_record,
    build_locale_tables,
    build_localized_names,
    load_tables,
    resolve_table_paths,
    should_skip,
    sorted_entries,
    write_dataset,
)


LABEL = "DeliveryDestinations"
OUTPUT_PATH = DATA_DIR / "delivery_destinations.json"
USER_AGENT = "MaaEnd-pipeline"

TABLE_NAMES = (
    "DomainDataTable.json",
    "DomainDepotTable.json",
    "DomainDepotDeliverTargetTable.json",
    "DomainDepotBuyerTable.json",
    *LOCALE_TABLES,
)
GAMEPLAY_CONFIG_NAMES = ("NpcProxyTable.json", "LevelMapMark.json")

DATA_BASE_URL = "https://assets.fz.wiki/output_maaend"
TABLE_CFG_BASE_URL: str | None = None
GAMEPLAY_CONFIG_BASE_URL: str | None = DATA_BASE_URL

REPO_ROOT = DATA_DIR.parents[2]
NAVMESH_DIR = REPO_ROOT / "assets" / "resource" / "model" / "map" / "navmesh"
NAV_CANDIDATES = ("base.nav.gz", "base.nav")
NAV_SUBMODULE_API = (
    "https://api.github.com/repos/MaaEnd/MaaEnd/contents/assets/resource/model?ref=v2"
)
NAV_REMOTE_TEMPLATE = (
    "https://raw.githubusercontent.com/MaaEnd/MaaEnd-AI/{sha}/map/navmesh/base.nav.gz"
)
NAV_PREFIX_BYTES = 64 * 1024
NAV_PREFIX_LIMIT = 8 * 1024 * 1024

NAV_MAGIC = b"BNAV"
NAV_HEADER = "<4sHHIIIIQQQQQ"
NAV_ZONE_V3 = ("<HHIIIIfffffff", 48)
NAV_ZONE_V2 = ("<HHIIIIffffff", 44)
NAV_GEO_TAG = b"BGEO"
NAV_GEO_HEADER = 72

RICH_TEXT_TAG = re.compile(r"<[^<>]*>")

ENTITY_TYPE_NPC_PROXY = 1
ENTITY_TYPE_RECYCLE_BIN = 2
ENTITY_TYPE_ALIASES = {
    1: ENTITY_TYPE_NPC_PROXY,
    2: ENTITY_TYPE_RECYCLE_BIN,
    "NpcProxy": ENTITY_TYPE_NPC_PROXY,
    "RecycleBin": ENTITY_TYPE_RECYCLE_BIN,
}
ENTITY_TYPE_KINDS = {
    ENTITY_TYPE_NPC_PROXY: "npc",
    ENTITY_TYPE_RECYCLE_BIN: "recycle_bin",
}


class NavPrefixTooShort(Exception):
    pass


def fetch_json(url: str) -> Any:
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(request, timeout=120) as response:
        return json.loads(response.read().decode("utf-8"))


def load_json_group(
    names: Sequence[str],
    directory: Path | None,
    base_url: str | None,
    label: str,
    option: str,
) -> dict[str, Any]:
    if directory is not None:
        return load_tables(resolve_table_paths(directory, names))
    if not base_url:
        raise TableCfgError(f"{label} 暂无公开地址，请用 {option} 指定本地目录")
    return {name: fetch_json(f"{base_url}/{name}") for name in names}


def find_geo_zone_table(raw: bytes, counts: tuple[int, ...]) -> int:
    at = raw.find(NAV_GEO_TAG, struct.calcsize(NAV_HEADER))
    while at >= 0:
        if at + NAV_GEO_HEADER <= len(raw) and struct.unpack_from("<3I", raw, at + 8) == counts:
            return at + struct.unpack_from("<Q", raw, at + 64)[0]
        at = raw.find(NAV_GEO_TAG, at + 4)
    raise NavPrefixTooShort


def parse_nav_zones(raw: bytes, source: str) -> dict[str, dict[str, Any]]:
    if len(raw) < struct.calcsize(NAV_HEADER):
        raise NavPrefixTooShort
    magic, version, _flags, zone_count, *rest = struct.unpack_from(NAV_HEADER, raw, 0)
    if magic != NAV_MAGIC:
        raise TableCfgError(f"nav 数据头不合法：{source}")

    zone_format, zone_size = NAV_ZONE_V3 if version >= 3 else NAV_ZONE_V2
    zones: dict[str, dict[str, Any]] = {}
    offset = find_geo_zone_table(raw, tuple(rest[0:3])) if version >= 5 else rest[3]
    for _ in range(zone_count):
        if offset + zone_size > len(raw):
            raise NavPrefixTooShort
        fields = struct.unpack_from(zone_format, raw, offset)
        name_size = fields[2]
        width, height, sx, tx, sy, ty = fields[6:12]
        offset += zone_size
        if offset + name_size > len(raw):
            raise NavPrefixTooShort
        name = raw[offset : offset + name_size].decode("utf-8")
        offset += name_size
        if name in zones:
            raise TableCfgError(f"zone 名 {name} 重复：{source}")
        zones[name] = {"size": (int(width), int(height)), "sx": sx, "tx": tx, "sy": sy, "ty": ty}
    if not zones:
        raise TableCfgError(f"没有解析到 zone：{source}")
    return zones


def decompress_prefix(chunk: bytes) -> bytes:
    if chunk[:2] != b"\x1f\x8b":
        return chunk
    return zlib.decompressobj(31).decompress(chunk)


def read_local_prefix(path: Path, size: int) -> bytes:
    with path.open("rb") as probe:
        compressed = probe.read(2) == b"\x1f\x8b"
    opener = gzip.open if compressed else open
    with opener(path, "rb") as handle:  # type: ignore[operator]
        return handle.read(size)


def read_remote_prefix(url: str, size: int) -> bytes:
    request = urllib.request.Request(
        url, headers={"Range": f"bytes=0-{size - 1}", "User-Agent": USER_AGENT}
    )
    with urllib.request.urlopen(request, timeout=60) as response:
        return decompress_prefix(response.read(size))


def resolve_remote_nav_url() -> str:
    sha = assert_record(fetch_json(NAV_SUBMODULE_API), "assets/resource/model").get("sha")
    if not isinstance(sha, str) or not sha:
        raise TableCfgError("没能确定 assets/resource/model 的版本")
    return NAV_REMOTE_TEMPLATE.format(sha=sha)


def load_nav_zones(explicit: str | None) -> dict[str, dict[str, Any]]:
    if explicit:
        if explicit.startswith(("http://", "https://")):
            source, reader = explicit, read_remote_prefix
        else:
            path = Path(explicit).expanduser()
            if not path.is_file():
                raise TableCfgError(f"nav 数据不存在：{path}")
            source, reader = path, read_local_prefix
    else:
        local = next(
            (NAVMESH_DIR / name for name in NAV_CANDIDATES if (NAVMESH_DIR / name).is_file()),
            None,
        )
        if local is not None:
            source, reader = local, read_local_prefix
        else:
            source, reader = resolve_remote_nav_url(), read_remote_prefix

    size = NAV_PREFIX_BYTES
    while size <= NAV_PREFIX_LIMIT:
        try:
            return parse_nav_zones(reader(source, size), str(source))  # type: ignore[arg-type]
        except NavPrefixTooShort:
            size *= 2
    raise TableCfgError(f"没能读全 zone 表：{source}")


def resolve_zone(zones: dict[str, dict[str, Any]], map_id: str) -> dict[str, Any]:
    for name in (map_id, f"{map_id}base"):
        zone = zones.get(name)
        if zone is not None:
            return {"name": name, **zone}
    raise TableCfgError(f"没有 {map_id} 对应的 zone")


def project_to_pixel(zone: dict[str, Any], x: float, z: float, label: str) -> tuple[float, float]:
    u = zone["sx"] * x + zone["tx"]
    v = -zone["sy"] * z + zone["ty"]
    width, height = zone["size"]
    if not (0.0 <= u < width and 0.0 <= v < height):
        raise TableCfgError(
            f"{label} 投影落在 {zone['name']} 底图外：u={u:.1f} v={v:.1f}（图 {width}x{height}）；"
            f"请确认几份输入取自同一版本"
        )
    return round(u, 3), round(v, 3)


def normalize_entity_type(value: Any, label: str) -> int:
    entity_type = ENTITY_TYPE_ALIASES.get(value)
    if entity_type is None:
        raise TableCfgError(f"{label} 的 entityType {value!r} 不是已知枚举值")
    return entity_type


def build_mark_index(level_map_mark: dict[str, Any]) -> dict[str, dict[str, Any]]:
    index: dict[str, dict[str, Any]] = {}
    for level_key, marks_value in sorted_entries(level_map_mark):
        marks = assert_list(marks_value, f"LevelMapMark[{level_key}]")
        for offset, mark_value in enumerate(marks):
            mark = assert_record(mark_value, f"LevelMapMark[{level_key}][{offset}]")
            basic = assert_record(
                mark.get("basicData"), f"LevelMapMark[{level_key}][{offset}].basicData"
            )
            mark_inst_id = basic.get("markInstId")
            if not isinstance(mark_inst_id, str) or not mark_inst_id:
                continue
            if mark_inst_id in index:
                raise TableCfgError(f"标记 {mark_inst_id} 在 LevelMapMark 中重复")
            index[mark_inst_id] = basic
    return index


def read_xz(value: Any, label: str) -> tuple[float, float]:
    record = assert_record(value, label)
    coords: list[float] = []
    for axis in ("x", "z"):
        number = record.get(axis)
        if not isinstance(number, (int, float)) or isinstance(number, bool):
            raise TableCfgError(f"{label} 的 {axis} 不是数值")
        coords.append(float(number))
    return coords[0], coords[1]


def locate_target(
    entity_type: int,
    entity_key: str,
    npc_positions: dict[str, Any],
    mark_index: dict[str, dict[str, Any]],
    label: str,
) -> tuple[float, float]:
    if entity_type == ENTITY_TYPE_NPC_PROXY:
        npc = assert_record(
            npc_positions.get(entity_key), f"{label} 在 NpcProxyTable 中的 {entity_key}"
        )
        return read_xz(npc.get("position"), f"NpcProxyTable[{entity_key}].position")

    basic = mark_index.get(entity_key)
    if basic is None:
        raise TableCfgError(f"{label} 在 LevelMapMark 中没有 {entity_key}")
    return read_xz(basic.get("pos"), f"LevelMapMark[{entity_key}].pos")


def strip_rich_text(names: dict[str, str], label: str) -> dict[str, str]:
    stripped: dict[str, str] = {}
    for locale, text in names.items():
        value = RICH_TEXT_TAG.sub("", text).strip()
        if not value:
            raise TableCfgError(f"{label} 在 {locale} 中为空")
        stripped[locale] = value
    return stripped


def build_area_names(
    depot_table: dict[str, Any],
    domain_table: dict[str, Any],
    locale_tables: dict[str, dict[str, Any]],
) -> tuple[dict[str, Any], dict[str, Any]]:
    depot_by_level: dict[str, Any] = {}
    for depot_id, entry_value in sorted_entries(depot_table):
        entry = assert_record(entry_value, f"仓储节点 {depot_id}")
        level_id = entry.get("refLevelId")
        if not isinstance(level_id, str) or not level_id:
            raise TableCfgError(f"仓储节点 {depot_id} 缺少 refLevelId")
        if level_id in depot_by_level:
            raise TableCfgError(f"层级 {level_id} 对应多个仓储节点")
        depot_by_level[level_id] = build_localized_names(
            entry.get("depotName"), locale_tables, f"仓储节点 {depot_id}"
        )

    region_names = {
        region_id: build_localized_names(
            assert_record(entry_value, f"地区 {region_id}").get("domainName"),
            locale_tables,
            f"地区 {region_id}",
        )
        for region_id, entry_value in sorted_entries(domain_table)
    }
    return depot_by_level, region_names


def build_delivery_destinations_data(
    tables: dict[str, Any],
    gameplay_config: dict[str, Any],
    zones: dict[str, dict[str, Any]],
) -> list[dict[str, Any]]:
    deliver_target_table = assert_record(
        tables["DomainDepotDeliverTargetTable.json"], "DomainDepotDeliverTargetTable"
    )
    buyer_table = assert_record(
        tables["DomainDepotBuyerTable.json"], "DomainDepotBuyerTable"
    )
    depot_table = assert_record(tables["DomainDepotTable.json"], "DomainDepotTable")
    domain_table = assert_record(tables["DomainDataTable.json"], "DomainDataTable")
    locale_tables = build_locale_tables(tables)

    npc_positions = assert_record(
        assert_record(gameplay_config["NpcProxyTable.json"], "NpcProxyTable").get("dataTable"),
        "NpcProxyTable.dataTable",
    )
    mark_index = build_mark_index(
        assert_record(gameplay_config["LevelMapMark.json"], "LevelMapMark")
    )
    depot_by_level, region_names = build_area_names(depot_table, domain_table, locale_tables)

    destinations: list[dict[str, Any]] = []
    used_zones: dict[str, dict[str, Any]] = {}
    for target_id, entry_value in sorted_entries(deliver_target_table):
        entry = assert_record(entry_value, f"送货目的地 {target_id}")
        label = f"送货目的地 {target_id}"

        level_id = entry.get("level")
        if not isinstance(level_id, str) or level_id.count("_") != 1:
            raise TableCfgError(f"{label} 的 level {level_id!r} 不是 mapXX_lvYYY")
        map_id = level_id.split("_")[0]

        entity_key = entry.get("targetId")
        if not isinstance(entity_key, str) or not entity_key:
            raise TableCfgError(f"{label} 缺少 targetId")

        entity_type = normalize_entity_type(entry.get("entityType"), label)
        x, z = locate_target(entity_type, entity_key, npc_positions, mark_index, label)

        zone = used_zones.get(map_id)
        if zone is None:
            zone = resolve_zone(zones, map_id)
            used_zones[map_id] = zone
        u, v = project_to_pixel(zone, x, z, label)

        buyer_id = target_id.replace("deliver_target", "buyer", 1)
        buyer = assert_record(buyer_table.get(buyer_id), f"{label} 对应的收货人 {buyer_id}")
        if buyer.get("level") != level_id:
            raise TableCfgError(f"收货人 {buyer_id} 的层级与{label}的层级 {level_id} 不一致")

        area = depot_by_level.get(level_id) or region_names.get(entry.get("domainId"))
        if area is None:
            raise TableCfgError(f"{label} 既没有仓储节点名也没有地区名")

        destinations.append(
            {
                "id": target_id,
                "kind": ENTITY_TYPE_KINDS[entity_type],
                "name": build_localized_names(
                    buyer.get("buyerName"), locale_tables, f"收货人 {buyer_id}"
                ),
                "mission": strip_rich_text(
                    build_localized_names(
                        entry.get("missionObjDesc"), locale_tables, f"{label}任务目标"
                    ),
                    f"{label}任务目标",
                ),
                "area": area,
                "map": map_id,
                "u": u,
                "v": v,
            }
        )

    if not destinations:
        raise TableCfgError("DomainDepotDeliverTargetTable 里一个送货点都没有")

    for locale in destinations[0]["name"]:
        values = [item["name"][locale] for item in destinations]
        if len(set(values)) != len(values):
            dupes = sorted({value for value in values if values.count(value) > 1})
            raise TableCfgError(f"{locale} 的 name 有重复：{dupes}")

    return destinations


def parse_arguments(args: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="生成送货点数据")
    parser.add_argument("--table-cfg-dir", type=Path, default=None, help="TableCfg 目录")
    parser.add_argument(
        "--gameplay-config-dir", type=Path, default=None, help="GameplayConfig 目录"
    )
    parser.add_argument("--nav", default=None, help="nav 数据的本地路径或 URL")
    parser.add_argument("--output", type=Path, default=OUTPUT_PATH, help="输出文件")
    parser.add_argument("--force", action="store_true", help="强制重写输出文件")
    return parser.parse_args(args)


def main(args: Sequence[str] | None = None) -> int:
    options = parse_arguments(args)
    try:
        tables = load_json_group(
            TABLE_NAMES, options.table_cfg_dir, TABLE_CFG_BASE_URL, "TableCfg", "--table-cfg-dir"
        )
        gameplay_config = load_json_group(
            GAMEPLAY_CONFIG_NAMES,
            options.gameplay_config_dir,
            GAMEPLAY_CONFIG_BASE_URL,
            "GameplayConfig",
            "--gameplay-config-dir",
        )
        zones = load_nav_zones(options.nav)
        data = build_delivery_destinations_data(tables, gameplay_config, zones)
        if should_skip(options.output, data, options.force):
            print(f"[{LABEL}] 生成结果未变化，跳过写入；可使用 --force 强制重写")
            return 0
        write_dataset(options.output, data)
        print(f"[{LABEL}] 已生成 {len(data)} 个送货点：{options.output}")
    except (OSError, ValueError, struct.error, zlib.error, urllib.error.URLError) as error:
        print(f"[{LABEL}] {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
