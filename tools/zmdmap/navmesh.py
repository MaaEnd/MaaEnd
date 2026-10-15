"""从 BaseNav 数据包读出投影用的 zone 表（原 ``project.mjs`` 的 nav 部分）。

BaseNav 包是二进制：头部是 ``BNAV``，版本 >= 5 时 zone 表的位置要去扫 ``BGEO`` 标记
（``find_geo_zone_table``），再按版本取定长的 zone 结构加变长的 zone 名。文件可能是 gzip
的一段前缀，所以按「多读一点、解出来够用就停」的方式反复尝试。

nav 数据默认读 ``assets/resource/model`` 子模块里的 ``map/navmesh/base.nav.gz``；本地没有时按
该子模块当前提交从远端取（Range 请求只拉前缀），``--nav`` 可以指定别的本地文件或 URL。
"""

from __future__ import annotations

import json
import math
import re
import struct
import zlib
from collections.abc import Callable
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

USER_AGENT = "MaaEnd-pipeline"
NAV_MAGIC = b"BNAV"
NAV_HEADER_SIZE = 64
NAV_GEO_TAG = b"BGEO"
NAV_GEO_HEADER_SIZE = 72
NAV_PREFIX_BYTES = 64 * 1024
NAV_PREFIX_LIMIT = 8 * 1024 * 1024
NAV_COMPRESSED_START = 256 * 1024
NAV_CANDIDATES = ("base.nav.gz", "base.nav")
NAV_SUBMODULE_API = "https://api.github.com/repos/MaaEnd/MaaEnd/contents/assets/resource/model?ref=v2"
NAV_REMOTE_TEMPLATE = "https://raw.githubusercontent.com/MaaEnd/MaaEnd-AI/{sha}/map/navmesh/base.nav.gz"

#: 读到的前缀还不足以解析出 zone 表，换更大的范围再试。
NavZone = dict[str, object]
Reader = Callable[[int], bytes]


class NavPrefixTooShort(Exception):
    """nav 数据只读到一段前缀，需要再读多一点。"""


def _read_remote(url: str, count: int) -> bytes:
    request = Request(url, headers={"Range": f"bytes=0-{count - 1}", "User-Agent": USER_AGENT})
    try:
        with urlopen(request, timeout=60) as response:
            return response.read()
    except HTTPError as error:
        raise ValueError(f"读取 nav 数据失败：{error.code} {error.reason}") from error
    except URLError as error:
        raise ValueError(f"读取 nav 数据失败：{error.reason}") from error


def _read_file_prefix(path: Path, count: int) -> bytes:
    with path.open("rb") as handle:
        return handle.read(count)


def _read_uint(raw: bytes, offset: int, size: int) -> int:
    return int.from_bytes(raw[offset : offset + size], "little")


def _read_float(raw: bytes, offset: int) -> float:
    return struct.unpack_from("<f", raw, offset)[0]


def find_geo_zone_table(raw: bytes, counts: list[int]) -> int:
    """在头部之后扫 ``BGEO`` 标记，按三段计数确认是 zone 表，返回它的偏移。"""
    at = raw.find(NAV_GEO_TAG, NAV_HEADER_SIZE)
    while at >= 0:
        if (
            at + NAV_GEO_HEADER_SIZE <= len(raw)
            and _read_uint(raw, at + 8, 4) == counts[0]
            and _read_uint(raw, at + 12, 4) == counts[1]
            and _read_uint(raw, at + 16, 4) == counts[2]
        ):
            return at + _read_uint(raw, at + 64, 8)
        at = raw.find(NAV_GEO_TAG, at + 4)
    raise NavPrefixTooShort("nav 数据里找不到 BGEO zone 表")


def parse_nav_zones(raw: bytes, source: str) -> dict[str, NavZone]:
    """解析 ``BNAV`` 头部与 zone 表；前缀不够时抛 :class:`NavPrefixTooShort`。"""
    if len(raw) < NAV_HEADER_SIZE:
        raise NavPrefixTooShort("nav 数据头部没读全")
    if raw[0:4] != NAV_MAGIC:
        raise ValueError(f"nav 数据头不合法：{source}")

    version = _read_uint(raw, 4, 2)
    zone_count = _read_uint(raw, 8, 4)
    counts = [_read_uint(raw, 12, 4), _read_uint(raw, 16, 4), _read_uint(raw, 20, 4)]
    zone_size = 48 if version >= 3 else 44
    offset = find_geo_zone_table(raw, counts) if version >= 5 else _read_uint(raw, 24, 8)

    zones: dict[str, NavZone] = {}
    for _ in range(zone_count):
        if offset + zone_size > len(raw):
            raise NavPrefixTooShort("nav 数据的 zone 表没读全")
        width = _read_float(raw, offset + 20)
        height = _read_float(raw, offset + 24)
        name_size = _read_uint(raw, offset + 4, 4)
        zone: NavZone = {
            "zone_id": _read_uint(raw, offset, 2),
            "size": [math.trunc(width), math.trunc(height)],
            "sx": _read_float(raw, offset + 28),
            "tx": _read_float(raw, offset + 32),
            "sy": _read_float(raw, offset + 36),
            "ty": _read_float(raw, offset + 40),
        }
        offset += zone_size
        if offset + name_size > len(raw):
            raise NavPrefixTooShort("nav 数据的 zone 名没读全")
        name = raw[offset : offset + name_size].decode("utf-8")
        offset += name_size
        if name in zones:
            raise ValueError(f"zone 名 {name} 重复：{source}")
        zones[name] = zone

    if not zones:
        raise ValueError(f"没有解析到 zone：{source}")
    return zones


def _inflate_prefix(compressed: bytes) -> bytes:
    if len(compressed) < 2 or compressed[0] != 0x1F or compressed[1] != 0x8B:
        return compressed
    # 拿到的可能只是 gzip 流的一段前缀：解到哪儿算哪儿，不因为没读完就报错。
    engine = zlib.decompressobj(16 + zlib.MAX_WBITS)
    return engine.decompress(compressed)


def _read_decompressed(read: Reader, size: int) -> bytes:
    """反复多读一些压缩数据，直到解出来的长度够用。"""
    compressed_size = max(size, NAV_COMPRESSED_START)
    for _ in range(8):
        data = _inflate_prefix(read(compressed_size))
        if len(data) >= size:
            return data
        compressed_size *= 4
    raise ValueError("没能读完 nav 数据的 zone 表")


def _resolve_nav_submodule_sha() -> str:
    request = Request(
        NAV_SUBMODULE_API,
        headers={"User-Agent": USER_AGENT, "Accept": "application/vnd.github+json"},
    )
    try:
        with urlopen(request, timeout=60) as response:
            body = json.loads(response.read().decode("utf-8"))
    except HTTPError as error:
        raise ValueError(f"查询 assets/resource/model 版本失败：{error.code}") from error
    except URLError as error:
        raise ValueError(f"查询 assets/resource/model 版本失败：{error.reason}") from error
    sha = body.get("sha") if isinstance(body, dict) else None
    if not isinstance(sha, str) or not sha:
        raise ValueError("没能确定 assets/resource/model 的版本")
    return sha


def resolve_nav_source(nav: str | None, repo_root: Path) -> tuple[str, Reader]:
    """定下 nav 数据的来源，返回 ``(日志用的标签, 读前缀的函数)``。"""
    if nav:
        if re.match(r"^https?://", nav):
            return nav, lambda count: _read_remote(nav, count)
        path = Path(nav).expanduser().resolve()
        if not path.exists():
            raise ValueError(f"nav 数据不存在：{path}")
        return str(path), lambda count: _read_file_prefix(path, count)

    nav_dir = repo_root / "assets" / "resource" / "model" / "map" / "navmesh"
    for name in NAV_CANDIDATES:
        candidate = nav_dir / name
        if candidate.exists():
            return str(candidate), lambda count: _read_file_prefix(candidate, count)

    url = NAV_REMOTE_TEMPLATE.format(sha=_resolve_nav_submodule_sha())
    return url, lambda count: _read_remote(url, count)


def load_nav_zones(nav: str | None, repo_root: Path) -> dict[str, NavZone]:
    """读出 zone 表：``zone 名 -> {zone_id, size, sx, tx, sy, ty}``。"""
    label, read = resolve_nav_source(nav, repo_root)
    print(f"[project] nav 数据：{label}")
    size = NAV_PREFIX_BYTES
    while size <= NAV_PREFIX_LIMIT:
        try:
            return parse_nav_zones(_read_decompressed(read, size), label)
        except NavPrefixTooShort:
            size *= 2
    raise ValueError(f"没能读全 zone 表：{label}")
