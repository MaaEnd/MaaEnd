"""MapLocator 投影共用件：BaseNav 的 zone 表 + 世界坐标 -> 底图像素。

zone 参数（zone_id / size / sx / tx / sy / ty）原样取自 BaseNav pack 的 zone 表，
投影公式 ``u = sx*x + tx, v = -sy*z + ty``，原点左上、y 向下。
delivery_destinations、teleport_anchors、collect_points 共用这一份。
"""

from __future__ import annotations

import gzip
import struct
import urllib.request
import zlib
from pathlib import Path
from typing import Any

from tablecfg_utils import (
    MAAEND_REPO_DIR,
    USER_AGENT,
    TableCfgError,
    assert_record,
    fetch_json,
)

# 几份数据的 coord 说明共用一句，免得各写一份写岔。
COORD_TEXT = (
    "u/v = MapLocator 底图像素，原点左上、y 向下；BaseNav 顶点也是这个平面（u, v, height），"
    "直接喂寻路即可。maps 里的 zone / sx / tx / sy / ty 原样取自 BaseNav pack 的 zone 表，"
    "世界坐标转进来是 u = sx*x + tx, v = -sy*z + ty"
)

NAVMESH_DIR = MAAEND_REPO_DIR / "assets" / "resource" / "model" / "map" / "navmesh"
# 与 CI 侧登记了 zone 参数的底图保持一致；nav 里另有 indie_dgXXX 的 zone，CI 未登记。
ZONE_MAPS = ("map01", "map02", "base01", "dung01")
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


class NavPrefixTooShort(Exception):
    pass


def find_geo_zone_table(raw: bytes, counts: tuple[int, ...]) -> int:
    at = raw.find(NAV_GEO_TAG, struct.calcsize(NAV_HEADER))
    while at >= 0:
        if (
            at + NAV_GEO_HEADER <= len(raw)
            and struct.unpack_from("<3I", raw, at + 8) == counts
        ):
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
        zones[name] = {
            "zone_id": int(fields[0]),
            "size": (int(width), int(height)),
            "sx": sx,
            "tx": tx,
            "sy": sy,
            "ty": ty,
        }
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
    sha = assert_record(fetch_json(NAV_SUBMODULE_API), "assets/resource/model").get(
        "sha"
    )
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
            (
                NAVMESH_DIR / name
                for name in NAV_CANDIDATES
                if (NAVMESH_DIR / name).is_file()
            ),
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


def project_to_pixel(
    zone: dict[str, Any], x: float, z: float, label: str
) -> tuple[float, float]:
    u = zone["sx"] * x + zone["tx"]
    v = -zone["sy"] * z + zone["ty"]
    width, height = zone["size"]
    if not (0.0 <= u < width and 0.0 <= v < height):
        raise TableCfgError(
            f"{label} 投影落在 {zone['name']} 底图外：u={u:.1f} v={v:.1f}（图 {width}x{height}）；"
            f"请确认几份输入取自同一版本"
        )
    return round(u, 3), round(v, 3)


def read_xz(value: Any, label: str) -> tuple[float, float]:
    record = assert_record(value, label)
    coords: list[float] = []
    for axis in ("x", "z"):
        number = record.get(axis)
        if not isinstance(number, (int, float)) or isinstance(number, bool):
            raise TableCfgError(f"{label} 的 {axis} 不是数值")
        coords.append(float(number))
    return coords[0], coords[1]


def describe_zones(used_zones: dict[str, dict[str, Any]]) -> dict[str, Any]:
    """精简数据里的 maps 块：只列本次用到的底图。"""
    return {
        map_id: {
            "zone": zone["name"],
            "zone_id": zone["zone_id"],
            "size": list(zone["size"]),
            "sx": zone["sx"],
            "tx": zone["tx"],
            "sy": zone["sy"],
            "ty": zone["ty"],
        }
        for map_id, zone in sorted(used_zones.items())
    }
