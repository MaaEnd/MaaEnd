"""整理 IconRecognition 的物品、武器数据并下载图标资源。

物品表与武器表都来自 zmdmap（``tools/zmdmap/data/``，由 ``uv run zmdmap fetch`` 下载或
``uv run zmdmap generate`` 本地生成）：条目自带多语言名字，所以不再下载 ``lang_*.json`` 那几份
i18n 包（每份约 950 KB）。图标仍然只能从站点下载。

图标走 httpx 的共享连接池并发下载：几百个图标共用一个 TLS 会话池，比每个文件重新握手快得多。
"""

import argparse
import hashlib
import json
import struct
import sys
import time
from collections import defaultdict
from collections.abc import Callable, Mapping, Sequence
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path
from typing import Any
from urllib.parse import parse_qsl, quote, urlencode, urlsplit, urlunsplit

import httpx
import json5
from fixed_items import FIXED_ITEMS
from text import clean_text, validate_identifier

from tools.zmdmap import read_payload

REPO_ROOT = Path(__file__).resolve().parents[2]
#: zmdmap 的数据目录：物品表与武器表的来源。
ZMDMAP_DATA_DIR = REPO_ROOT / "tools" / "zmdmap" / "data"
ITEM_TABLE_NAME = "item_mini_table.json"
WEAPON_TABLE_NAME = "weapons.json"
# 图标与 zmdmap 数据一样从 voice.fz.wiki 取：同一个源站，但前置的 EdgeOne 有海外节点。
IMAGE_BASE_URL = "https://voice.fz.wiki/output_image/itemicon"
BIG_IMAGE_BASE_URL = "https://voice.fz.wiki/output_image/itemiconbig"
PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"
USER_AGENT = "MaaEnd-IconRecognition/1.0"
DEFAULT_CACHE_ROOT = REPO_ROOT / "tools" / "icon_recognition" / ".cache" / "downloads"
DEFAULT_BLACKLIST_PATH = Path(__file__).with_name("blacklist.json")
BIG_ICON_IDS_PATH = Path(__file__).with_name("big_icon_ids.json")
#: 共享连接池上限；要大于 `--workers`，否则线程会排队等连接。
MAX_CONNECTIONS = 32
#: httpx 自带的建连重试次数（只覆盖建连阶段）。
CONNECT_RETRIES = 2
#: 网络层瞬时失败的重试次数：CDN 会在并发下直接断开连接，重试一次通常就好。
REQUEST_ATTEMPTS = 3
#: 重试前的等待秒数，按次数递增。
RETRY_BACKOFF = 1.0


@lru_cache(maxsize=1)
def http_client() -> httpx.Client:
    """进程内共享的连接池客户端；线程池里的下载共用它复用连接。"""
    return httpx.Client(
        headers={"User-Agent": USER_AGENT},
        follow_redirects=True,
        limits=httpx.Limits(max_connections=MAX_CONNECTIONS),
        transport=httpx.HTTPTransport(retries=CONNECT_RETRIES),
    )


def timeout_for(seconds: float) -> httpx.Timeout:
    """单次请求超时；连接（含 TLS 握手）单独限时，握手卡住时不必把整个超时耗满。"""
    return httpx.Timeout(seconds, connect=min(seconds, 20.0))


def request_with_retry(
    url: str,
    *,
    headers: Mapping[str, str] | None = None,
    timeout: float,
) -> httpx.Response:
    """带瞬时失败重试的 GET：网络层错误与被断开都会重试，返回可用的响应。"""
    for attempt in range(REQUEST_ATTEMPTS):
        if attempt:
            time.sleep(RETRY_BACKOFF * attempt)
        try:
            response = http_client().get(
                url, headers=headers or {}, timeout=timeout_for(timeout)
            )
        except httpx.TransportError as error:
            failure: Exception = error
        else:
            if response.status_code < 500:
                return response
            failure = httpx.HTTPStatusError(
                f"{response.status_code} {response.reason_phrase}",
                request=response.request,
                response=response,
            )
    raise failure


def load_big_icon_ids(path: Path = BIG_ICON_IDS_PATH) -> set[str]:
    ids = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(ids, list) or any(not isinstance(icon_id, str) for icon_id in ids):
        raise ValueError("大图名单必须是 iconId 字符串数组")
    for icon_id in ids:
        validate_identifier(icon_id, field="大图名单 iconId")
    if len(ids) != len(set(ids)):
        raise ValueError("大图名单存在重复 iconId")
    return set(ids)


def rarity_directories(root: Path) -> list[Path]:
    if not root.is_dir():
        return []
    return [
        path
        for path in root.iterdir()
        if path.is_dir() and path.name.isascii() and path.name.isdecimal()
    ]


def validate_icon_png_bytes(content: bytes) -> int:
    """校验图标 PNG 的 IHDR，并返回合法的正方形边长。"""
    if len(content) < 29 or not content.startswith(PNG_SIGNATURE):
        raise ValueError("响应内容不是完整的 PNG IHDR")
    chunk_length, chunk_type, width, height = struct.unpack(">I4sII", content[8:24])
    if chunk_length != 13 or chunk_type != b"IHDR" or width <= 0 or height <= 0:
        raise ValueError("响应内容不是完整的 PNG IHDR")
    if width != height:
        raise ValueError(f"图标 PNG 必须是正方形，实际为 {width}x{height}")
    if width & (width - 1):
        raise ValueError(f"图标 PNG 边长必须是 2 的整数次幂，实际为 {width}")
    return width



ITEM_FIELDS = (
    "name",
    "category",
    "storageKind",
    "categoryType",
    "iconId",
    "rarity",
    "sortId1",
    "sortId2",
    "fluidType",
    "fluid",
    "fullContainers",
    "emptyContainers",
)
CATEGORY_MAPPING = {
    "ValuableDepot": {
        "Weapon": "武器",
        "WeaponGem": "武器基质",
        "Equip": "装备",
        "SpecialItem": "培养素材",
        "MissionItem": "任务物品",
        "CommercialItem": "珍贵物品",
    },
    "Normal": {
        "Ore": "矿物",
        "Plant": "植物",
        "Product": "产物",
        "Doodad": "采集材料",
        "Nurturance": "培养素材",
        "Usable": "可用道具",
        "Producer": "生产工具",
        "PortableDevice": "随身装置",
    },
    "Isolate": {},
}


@dataclass(frozen=True, slots=True)
class DownloadJob:
    icon_id: str
    rarity: int
    url: str
    destination: Path


def _category_name(storage_kind: str, category_type: str) -> str:
    if storage_kind == "Isolate":
        return "独立资源"
    try:
        return CATEGORY_MAPPING[storage_kind][category_type]
    except KeyError as error:
        raise ValueError(
            f"未知物品分类: {storage_kind}:{category_type}"
        ) from error


def _require_string(
    source: Mapping[str, Any],
    field: str,
    context: str,
    *,
    allow_empty: bool = False,
) -> str:
    value = source.get(field)
    if not isinstance(value, str) or (not allow_empty and not value):
        expected = "字符串" if allow_empty else "非空字符串"
        raise ValueError(f"{context}.{field} 必须是{expected}")
    return value


def _require_rarity(source: Mapping[str, Any], context: str) -> int:
    value = source.get("rarity")
    if isinstance(value, bool) or not isinstance(value, int) or value < 1:
        raise ValueError(f"{context}.rarity 必须是正整数")
    return value


def _require_integer(
    source: Mapping[str, Any], field: str, context: str
) -> int:
    value = source.get(field)
    if isinstance(value, bool) or not isinstance(value, int):
        raise ValueError(f"{context}.{field} 必须是整数")
    return value


def _require_optional_string(
    source: Mapping[str, Any], field: str, context: str
) -> str | None:
    value = source.get(field)
    if value is not None and not isinstance(value, str):
        raise ValueError(f"{context}.{field} 必须是字符串或 null")
    return value


def _require_optional_boolean(
    source: Mapping[str, Any], field: str, context: str
) -> bool:
    if field not in source:
        return False
    value = source[field]
    if not isinstance(value, bool):
        raise ValueError(f"{context}.{field} 必须是布尔值")
    return value


def _require_string_list(
    source: Mapping[str, Any], field: str, context: str
) -> list[str]:
    value = source.get(field)
    if not isinstance(value, list) or not all(
        isinstance(item, str) and item for item in value
    ):
        raise ValueError(f"{context}.{field} 必须是非空字符串数组")
    return list(value)


ItemBlacklistRule = tuple[str, str, frozenset[str]]


def load_item_blacklist(
    path: str | Path = DEFAULT_BLACKLIST_PATH,
) -> tuple[ItemBlacklistRule, ...]:
    source_path = Path(path)
    payload = json5.loads(source_path.read_text(encoding="utf-8-sig"))
    if not isinstance(payload, Mapping):
        raise ValueError(f"黑名单顶层必须是对象: {source_path}")
    raw_rules = payload.get("rules")
    if not isinstance(raw_rules, list):
        raise ValueError(f"黑名单 rules 必须是数组: {source_path}")

    rules: list[ItemBlacklistRule] = []
    for index, raw_rule in enumerate(raw_rules):
        context = f"{source_path}.rules[{index}]"
        if not isinstance(raw_rule, Mapping):
            raise ValueError(f"{context} 必须是对象")
        storage_kind = _require_string(raw_rule, "storageKind", context)
        category_type = _require_string(raw_rule, "categoryType", context)
        ids = frozenset(_require_string_list(raw_rule, "ids", context))
        rules.append((storage_kind, category_type, ids))
    return tuple(rules)


def _timestamped_url(base_url: str, *, timestamp_ms: int | None = None) -> str:
    selected_timestamp = (
        time.time_ns() // 1_000_000 if timestamp_ms is None else timestamp_ms
    )
    if isinstance(selected_timestamp, bool) or not isinstance(
        selected_timestamp, int
    ) or selected_timestamp < 0:
        raise ValueError("timestamp_ms 必须是非负整数")
    parts = urlsplit(base_url)
    query = [
        (key, value)
        for key, value in parse_qsl(parts.query, keep_blank_values=True)
        if key != "timestamp"
    ]
    query.append(("timestamp", str(selected_timestamp)))
    return urlunsplit(
        (parts.scheme, parts.netloc, parts.path, urlencode(query), parts.fragment)
    )


def _normalize_item(item_id: str, raw_source: Any) -> dict[str, Any]:
    validate_identifier(item_id, field="mini table 顶层 key")
    if not isinstance(raw_source, Mapping):
        raise ValueError(f"mini table 项必须是对象: {item_id}")
    missing_fields = [field for field in ITEM_FIELDS if field not in raw_source]
    if missing_fields:
        raise ValueError(f"{item_id} 缺少字段: {', '.join(missing_fields)}")

    storage_kind = _require_string(raw_source, "storageKind", item_id)
    category_type = _require_string(raw_source, "categoryType", item_id)
    result = {
        "name": clean_text(raw_source.get("name"), field=f"{item_id}.name"),
        "category": _category_name(storage_kind, category_type),
        "storageKind": storage_kind,
        "categoryType": category_type,
        "iconId": _require_string(
            raw_source, "iconId", item_id, allow_empty=True
        ),
        "rarity": _require_rarity(raw_source, item_id),
        "sortId1": _require_integer(raw_source, "sortId1", item_id),
        "sortId2": _require_integer(raw_source, "sortId2", item_id),
        "fluidType": _require_optional_string(raw_source, "fluidType", item_id),
        "fluid": _require_optional_string(raw_source, "fluid", item_id),
        "fullContainers": _require_string_list(
            raw_source, "fullContainers", item_id
        ),
        "emptyContainers": _require_string_list(
            raw_source, "emptyContainers", item_id
        ),
    }
    # zmdmap 那份额外带名字的文本 id；去重时按它分组才与上游的哈希去重等价。
    name_id = raw_source.get("nameId")
    if isinstance(name_id, int) and not isinstance(name_id, bool):
        result["nameId"] = name_id
    if _require_optional_boolean(raw_source, "regionRestricted", item_id):
        result["regionRestricted"] = True
    return result


def apply_item_blacklist(
    item_map: Mapping[str, Any],
    *,
    blacklist: Sequence[ItemBlacklistRule] | None = None,
) -> tuple[dict[str, dict[str, Any]], list[dict[str, Any]]]:
    """应用显式黑名单，并清理因此失去所有容器引用的流体条目。"""

    if not isinstance(item_map, Mapping):
        raise ValueError("item map 顶层必须是对象")
    items: dict[str, dict[str, Any]] = {}
    for item_id, raw_payload in item_map.items():
        if not isinstance(raw_payload, Mapping):
            raise ValueError(f"item map 项必须是对象: {item_id}")
        payload = dict(raw_payload)
        full_containers = payload.get("fullContainers", [])
        if not isinstance(full_containers, list) or not all(
            isinstance(container_id, str) and container_id
            for container_id in full_containers
        ):
            raise ValueError(f"{item_id}.fullContainers 必须是非空字符串数组")
        payload["fullContainers"] = list(full_containers)
        items[item_id] = payload

    active_blacklist = (
        load_item_blacklist() if blacklist is None else tuple(blacklist)
    )
    removed_ids: set[str] = set()
    removals: list[dict[str, Any]] = []
    for item_id, payload in items.items():
        # 分类条件用于约束黑名单作用域，避免上游复用 ID 后误删其他类型物品。
        if any(
            item_id in ids
            and payload.get("storageKind") == storage_kind
            and payload.get("categoryType") == category_type
            for storage_kind, category_type, ids in active_blacklist
        ):
            removed_ids.add(item_id)
            removals.append(
                {
                    "removedId": item_id,
                    "name": payload.get("name", ""),
                    "iconId": payload.get("iconId", ""),
                    "reason": "blacklist",
                }
            )
    while True:
        orphaned = []
        for item_id, payload in items.items():
            if item_id in removed_ids:
                continue
            remaining_containers = [
                container_id
                for container_id in payload["fullContainers"]
                if container_id not in removed_ids
            ]
            if len(remaining_containers) == len(payload["fullContainers"]):
                continue
            payload["fullContainers"] = remaining_containers
            if not remaining_containers:
                orphaned.append(item_id)
        if not orphaned:
            break
        for item_id in orphaned:
            removed_ids.add(item_id)
            payload = items[item_id]
            removals.append(
                {
                    "removedId": item_id,
                    "name": payload.get("name", ""),
                    "iconId": payload.get("iconId", ""),
                    "reason": "blacklist-orphan",
                }
            )

    removals.sort(key=lambda row: row["removedId"])
    return {
        item_id: payload
        for item_id, payload in items.items()
        if item_id not in removed_ids
    }, removals


def prepare_item_map(
    item_table: Mapping[str, Any],
    *,
    blacklist: Sequence[ItemBlacklistRule] | None = None,
) -> tuple[dict[str, dict[str, Any]], list[dict[str, Any]]]:
    """规范化 mini table，并按黑名单和引用感知规则去除条目。"""

    if not isinstance(item_table, Mapping):
        raise ValueError("item_mini_table.json 顶层必须是对象")
    normalized = {
        item_id: _normalize_item(item_id, raw_source)
        for item_id, raw_source in item_table.items()
    }
    filtered, removals = apply_item_blacklist(normalized, blacklist=blacklist)
    removed_ids: set[str] = set()
    referenced_ids = {
        container_id
        for payload in filtered.values()
        for container_id in payload["fullContainers"]
    }

    duplicate_groups: dict[tuple[Any, str], list[str]] = defaultdict(list)
    for item_id, payload in filtered.items():
        # 按名字的文本 id 分组（上游是拿名字哈希去重的，哈希即文本 id 的函数）；
        # 没有 nameId 的旧数据退回用名字本身。
        duplicate_groups[(payload.get("nameId", payload["name"]), payload["iconId"])].append(item_id)
    for (_name, icon_id), member_ids in duplicate_groups.items():
        if len(member_ids) < 2:
            continue
        ordered_ids = sorted(member_ids, key=lambda item_id: (len(item_id), item_id))
        referenced_members = [
            item_id for item_id in ordered_ids if item_id in referenced_ids
        ]
        kept_ids = set(referenced_members or ordered_ids[:1])
        kept_id = (referenced_members or ordered_ids)[0]
        reason = "fullContainers-reference" if referenced_members else "shortest-key"
        for item_id in ordered_ids:
            if item_id in kept_ids:
                continue
            removed_ids.add(item_id)
            removals.append(
                {
                    "removedId": item_id,
                    "keptId": kept_id,
                    "name": filtered[kept_id]["name"],
                    "iconId": icon_id,
                    "reason": reason,
                }
            )

    items = {
        item_id: payload
        for item_id, payload in filtered.items()
        if item_id not in removed_ids
    }
    removals.sort(key=lambda row: row["removedId"])
    return dict(sorted(items.items())), removals


def _normalized_weapon(
    item_id: str, raw_source: Mapping[str, Any]
) -> dict[str, Any]:
    names = raw_source.get("names")
    if not isinstance(names, Mapping):
        raise ValueError(f"{item_id}.names 必须是对象")
    return {
        "name": clean_text(names.get("CN"), field=f"{item_id}.names.CN"),
        "category": _category_name("ValuableDepot", "Weapon"),
        "storageKind": "ValuableDepot",
        "categoryType": "Weapon",
        "iconId": validate_identifier(
            raw_source.get("icon_id", item_id), field=f"{item_id}.icon_id"
        ),
        "rarity": _require_rarity(raw_source, item_id),
        "fluidType": None,
        "fluid": None,
        "fullContainers": [],
        "emptyContainers": [],
    }


def prepare_weapon_map(
    weapon_table: Mapping[str, Any],
    *,
    probe_icon: Callable[[str], bool] | None = None,
) -> dict[str, dict[str, Any]]:
    if not isinstance(weapon_table, Mapping):
        raise ValueError("weapons.json 顶层必须是对象")
    weapons: dict[str, dict[str, Any]] = {}
    for top_key, raw_source in weapon_table.items():
        validate_identifier(top_key, field="weapons.json 顶层 key")
        if not isinstance(raw_source, Mapping):
            raise ValueError(f"武器数据必须是对象: {top_key}")
        internal_id = validate_identifier(
            raw_source.get("internal_id"), field=f"{top_key}.internal_id"
        )
        selected_id = top_key
        if internal_id != top_key:
            if probe_icon is None:
                raise ValueError(
                    f"武器身份不一致且未提供图标探测器: {top_key} != {internal_id}"
                )
            downloadable = [
                candidate
                for candidate in (top_key, internal_id)
                if probe_icon(candidate)
            ]
            if len(downloadable) != 1:
                raise ValueError(
                    f"武器身份无法唯一解析: {top_key} != {internal_id}，"
                    f"可下载候选 {downloadable}"
                )
            selected_id = downloadable[0]
        if selected_id in weapons:
            raise ValueError(f"武器规范化后 ID 冲突: {selected_id}")
        weapons[selected_id] = _normalized_weapon(selected_id, raw_source)
    return dict(sorted(weapons.items()))


def merge_item_sources(
    items: Mapping[str, Mapping[str, Any]],
    weapons: Mapping[str, Mapping[str, Any]],
) -> dict[str, dict[str, Any]]:
    merged = {item_id: dict(payload) for item_id, payload in items.items()}
    for item_id, payload in weapons.items():
        if item_id in merged:
            raise ValueError(f"普通物品与武器 ID 冲突: {item_id}")
        merged[item_id] = dict(payload)
    for item_id, fixed in FIXED_ITEMS.items():
        if item_id in merged:
            raise ValueError(f"固定物品与远端物品 ID 冲突: {item_id}")
        merged[item_id] = {
            "name": fixed["name"],
            "names": fixed["names"],
            "category": fixed["category"],
            "storageKind": fixed["storageKind"],
            "categoryType": fixed["categoryType"],
            "iconId": fixed["iconId"],
            "rarity": fixed["rarity"],
            "fluidType": None,
            "fluid": None,
            "fullContainers": [],
            "emptyContainers": [],
        }
    return dict(sorted(merged.items()))


def build_download_jobs(
    items: Mapping[str, Mapping[str, Any]], image_root: Path, *, image_base_url: str = IMAGE_BASE_URL
) -> tuple[list[DownloadJob], list[dict[str, Any]]]:
    jobs_by_destination: dict[Path, DownloadJob] = {}
    missing_icons: list[dict[str, Any]] = []
    for item_id, payload in items.items():
        if not isinstance(payload, Mapping):
            raise ValueError(f"物品数据必须是对象: {item_id}")
        icon_id = _require_string(payload, "iconId", item_id, allow_empty=True)
        rarity = _require_rarity(payload, item_id)
        if not icon_id:
            missing_icons.append(
                {
                    "id": item_id,
                    "name": _require_string(payload, "name", item_id),
                    "rarity": rarity,
                }
            )
            continue
        validate_identifier(icon_id, field=f"{item_id}.iconId")
        destination = image_root / str(rarity) / f"{icon_id}.png"
        job = DownloadJob(
            icon_id=icon_id,
            rarity=rarity,
            url=f"{image_base_url}/{quote(icon_id, safe='')}.png@raw",
            destination=destination,
        )
        previous = jobs_by_destination.get(destination)
        if previous is not None and previous.url != job.url:
            raise ValueError(f"下载目标冲突: {destination}")
        jobs_by_destination[destination] = job
    jobs = sorted(
        jobs_by_destination.values(), key=lambda job: (job.rarity, job.icon_id)
    )
    missing_icons.sort(key=lambda row: row["id"])
    return jobs, missing_icons


def relocate_rarity_changed_icons(
    jobs: Sequence[DownloadJob], image_root: Path
) -> int:
    """在下载前迁移稀有度变更的缓存图标，避免重复下载并留下旧路径。"""
    destinations: dict[str, Path] = {}
    for job in jobs:
        previous = destinations.get(job.icon_id)
        if previous is not None and previous != job.destination:
            raise ValueError(f"同一 iconId 对应多个稀有度: {job.icon_id}")
        destinations[job.icon_id] = job.destination

    moved = 0
    for icon_id, destination in destinations.items():
        candidates = []
        for rarity_directory in rarity_directories(image_root):
            path = rarity_directory / f"{icon_id}.png"
            if path.is_file() and path != destination:
                candidates.append(path)
        if len(candidates) > 1:
            raise ValueError(f"同一 iconId 存在多个旧稀有度图标: {icon_id}")
        if not candidates:
            continue

        source = candidates[0]
        source_metadata = _metadata_path(source)
        destination_metadata = _metadata_path(destination)
        if destination.is_file():
            # 目标已存在时保留它，避免覆盖可能经过 CI 处理的图片。
            source.unlink()
            source_metadata.unlink(missing_ok=True)
            continue
        if not is_valid_png(source):
            continue
        destination.parent.mkdir(parents=True, exist_ok=True)
        source.replace(destination)
        if source_metadata.is_file():
            source_metadata.replace(destination_metadata)
        moved += 1
    return moved


def is_valid_png(path: Path) -> bool:
    if not path.is_file():
        return False
    try:
        validate_icon_png_bytes(path.read_bytes())
        return True
    except (OSError, ValueError):
        return False


def _metadata_path(destination: Path) -> Path:
    return destination.with_suffix(destination.suffix + ".meta.json")


def fetch(
    url: str,
    destination: Path,
    *,
    timeout: float = 60,
    validate_content: Callable[[bytes], Any] | None = None,
) -> dict[str, Any]:
    """来源地址未变化时使用条件请求；切换来源后重新下载并原子更新。"""

    destination.parent.mkdir(parents=True, exist_ok=True)
    metadata_path = _metadata_path(destination)
    metadata = (
        json.loads(metadata_path.read_text(encoding="utf-8"))
        if metadata_path.is_file()
        else {}
    )
    headers = {}
    if destination.is_file() and metadata.get("url") == url and (
        validate_content is None or is_valid_png(destination)
    ):
        if isinstance(metadata.get("etag"), str):
            headers["If-None-Match"] = metadata["etag"]
        if isinstance(metadata.get("lastModified"), str):
            headers["If-Modified-Since"] = metadata["lastModified"]
    response = request_with_retry(url, headers=headers, timeout=timeout)
    if response.status_code == 304 and destination.is_file():
        return metadata
    response.raise_for_status()
    data = response.content
    if validate_content is not None:
        validate_content(data)
    result: dict[str, Any] = {
        "url": url,
        "bytes": len(data),
        "sha256": hashlib.sha256(data).hexdigest(),
        "updatedAt": int(time.time()),
    }
    for source_key, output_key in (
        ("ETag", "etag"),
        ("Last-Modified", "lastModified"),
    ):
        if response.headers.get(source_key):
            result[output_key] = response.headers[source_key]

    temporary = destination.with_suffix(destination.suffix + ".part")
    try:
        temporary.write_bytes(data)
        temporary.replace(destination)
        metadata_path.write_text(
            json.dumps(result, ensure_ascii=False, indent=4) + "\n",
            encoding="utf-8",
        )
    finally:
        temporary.unlink(missing_ok=True)
    return result


def _download_icon(job: DownloadJob, timeout: float) -> str:
    previous = job.destination.read_bytes() if is_valid_png(job.destination) else None
    fetch(
        job.url,
        job.destination,
        timeout=timeout,
        validate_content=validate_icon_png_bytes,
    )
    return "skipped" if previous == job.destination.read_bytes() else "downloaded"


def download_images(
    jobs: Sequence[DownloadJob], *, workers: int, timeout: float
) -> dict[str, Any]:
    if workers < 1:
        raise ValueError("workers 必须至少为 1")
    if timeout <= 0:
        raise ValueError("timeout 必须大于 0")
    downloaded = 0
    skipped = 0
    failures: list[dict[str, Any]] = []
    unpublished: list[dict[str, Any]] = []
    with ThreadPoolExecutor(max_workers=workers) as executor:
        futures = {
            executor.submit(_download_icon, job, timeout): job for job in jobs
        }
        for future in as_completed(futures):
            job = futures[future]
            try:
                if future.result() == "downloaded":
                    downloaded += 1
                else:
                    skipped += 1
            except Exception as error:
                row = {
                    "iconId": job.icon_id,
                    "rarity": job.rarity,
                    "url": job.url,
                    "destination": str(job.destination),
                    "errorType": type(error).__name__,
                    "error": str(error),
                }
                # 404 是站点上没有这个图标（上游的缺口），不是下载失败：重试也不会出现。
                if isinstance(error, httpx.HTTPStatusError) and error.response.status_code == 404:
                    unpublished.append(row)
                else:
                    failures.append(row)
    failures.sort(key=lambda row: (row["rarity"], row["iconId"]))
    unpublished.sort(key=lambda row: (row["rarity"], row["iconId"]))
    return {
        "total": len(jobs),
        "downloaded": downloaded,
        "skipped": skipped,
        "failed": len(failures),
        "failures": failures,
        "unpublished": len(unpublished),
        "unpublishedIcons": unpublished,
    }


def _load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8-sig"))


def _write_json(path: Path, payload: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(payload, ensure_ascii=False, indent=4, sort_keys=True) + "\n",
        encoding="utf-8",
    )


def probe_icon_downloadable(icon_id: str, *, timeout: float) -> bool:
    validate_identifier(icon_id, field="待探测 iconId")
    url = f"{IMAGE_BASE_URL}/{quote(icon_id, safe='')}.png@raw"
    response = request_with_retry(url, timeout=timeout)
    if response.status_code == 404:
        return False
    response.raise_for_status()
    return response.content[: len(PNG_SIGNATURE)] == PNG_SIGNATURE


def load_item_table(data_dir: str | Path = ZMDMAP_DATA_DIR) -> dict[str, Any]:
    """zmdmap 的物品表。"""
    return read_payload(Path(data_dir) / ITEM_TABLE_NAME)


def load_weapon_table(data_dir: str | Path = ZMDMAP_DATA_DIR) -> dict[str, Any]:
    """zmdmap 的武器表。"""
    return read_payload(Path(data_dir) / WEAPON_TABLE_NAME)


def run(
    cache_root: str | Path,
    *,
    data_dir: str | Path = ZMDMAP_DATA_DIR,
    workers: int,
    timeout: float,
) -> dict[str, Any]:
    root = Path(cache_root)
    items, removals = prepare_item_map(
        load_item_table(data_dir),
        blacklist=load_item_blacklist(),
    )
    weapons = prepare_weapon_map(
        load_weapon_table(data_dir),
        probe_icon=lambda icon_id: probe_icon_downloadable(
            icon_id, timeout=timeout
        ),
    )
    merged = merge_item_sources(items, weapons)
    _write_json(root / "item.json", merged)
    jobs, missing_icons = build_download_jobs(merged, root / "images")
    relocated = relocate_rarity_changed_icons(jobs, root / "images")
    report = download_images(jobs, workers=workers, timeout=timeout)
    big_icon_ids = load_big_icon_ids()
    big_items = {item_id: item for item_id, item in merged.items() if item.get("iconId") in big_icon_ids}
    big_jobs, _ = build_download_jobs(big_items, root / "big_images", image_base_url=BIG_IMAGE_BASE_URL)
    if {job.icon_id for job in big_jobs} != big_icon_ids:
        raise ValueError("大图名单包含不在物品源数据中的 iconId")
    relocate_rarity_changed_icons(big_jobs, root / "big_images")
    big_report = download_images(big_jobs, workers=workers, timeout=timeout)
    blacklist_removals = [
        row for row in removals if row["reason"] == "blacklist"
    ]
    blacklist_orphans = [
        row for row in removals if row["reason"] == "blacklist-orphan"
    ]
    duplicate_removals = [
        row
        for row in removals
        if row["reason"] not in {"blacklist", "blacklist-orphan"}
    ]
    report.update(
        {
            "itemCount": len(merged),
            "weaponCount": len(weapons),
            "blacklistedCount": len(blacklist_removals),
            "blacklistedItems": blacklist_removals,
            "blacklistOrphanCount": len(blacklist_orphans),
            "blacklistOrphans": blacklist_orphans,
            "duplicateRemovedCount": len(duplicate_removals),
            "duplicateRemovals": duplicate_removals,
            "missingIconCount": len(missing_icons),
            "missingIconItems": missing_icons,
            "relocatedCount": relocated,
            "bigImages": big_report,
        }
    )
    _write_json(root / "download_report.json", report)
    return report


def main() -> int:
    parser = argparse.ArgumentParser(
        description="下载并整理 IconRecognition 离线资源"
    )
    parser.add_argument(
        "--cache-root",
        type=Path,
        default=DEFAULT_CACHE_ROOT,
    )
    parser.add_argument(
        "--data-dir",
        type=Path,
        default=ZMDMAP_DATA_DIR,
        help="zmdmap 数据目录（默认 tools/zmdmap/data）",
    )
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--timeout", type=float, default=60)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if args.dry_run:
        print(
            json.dumps(
                {
                    "itemTable": str(Path(args.data_dir) / ITEM_TABLE_NAME),
                    "weaponTable": str(Path(args.data_dir) / WEAPON_TABLE_NAME),
                    "imageBaseUrl": IMAGE_BASE_URL,
                    "bigImageBaseUrl": BIG_IMAGE_BASE_URL,
                },
                ensure_ascii=False,
                indent=4,
            )
        )
        return 0
    report = run(
        args.cache_root,
        data_dir=args.data_dir,
        workers=args.workers,
        timeout=args.timeout,
    )
    print(json.dumps(report, ensure_ascii=False, indent=4))
    if report["unpublished"]:
        print(
            f"[icon] {report['unpublished']} 个图标站点上没有（见 download_report.json 的"
            f" unpublishedIcons），已跳过",
            file=sys.stderr,
        )
    return 1 if report["failed"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
