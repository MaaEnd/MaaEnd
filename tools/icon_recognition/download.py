"""下载并整理 IconRecognition 的物品、武器、多语言和图标资源。"""

from __future__ import annotations

import argparse
from collections import defaultdict
from collections.abc import Callable, Mapping, Sequence
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
import hashlib
import json
from pathlib import Path
import struct
import time
from typing import Any
from urllib.error import HTTPError
from urllib.parse import parse_qsl, quote, urlencode, urlsplit, urlunsplit
from urllib.request import Request, urlopen

from fixed_items import FIXED_ITEMS

from text import clean_text, validate_identifier


ITEM_TABLE_URL = "https://assets.fz.wiki/output_beyondmap/item_mini_table.json"
WEAPON_TABLE_URL = "https://assets.fz.wiki/output_maaend/weapons.json"
IMAGE_BASE_URL = "https://assets.fz.wiki/output_image/itemicon"
LANG_URL = "https://assets.fz.wiki/output_beyondmap/i18n/{locale}/lang.json"
LOCALES = {
    "zh-CN": "CN",
    "zh-TW": "TC",
    "en-US": "EN",
    "ja-JP": "JP",
    "ko-KR": "KR",
}
PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"
DEFAULT_CACHE_ROOT = Path("tools/icon_recognition/.cache/downloads")
DEFAULT_BLACKLIST_PATH = Path(__file__).with_name("blacklist.json")


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
    payload = json.loads(source_path.read_text(encoding="utf-8-sig"))
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
    if _require_optional_boolean(raw_source, "regionRestricted", item_id):
        result["regionRestricted"] = True
    return result


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
    active_blacklist = (
        load_item_blacklist() if blacklist is None else tuple(blacklist)
    )
    removed_ids: set[str] = set()
    removals: list[dict[str, Any]] = []
    for item_id, payload in normalized.items():
        # 分类条件用于约束黑名单作用域，避免上游复用 ID 后误删其他类型物品。
        if any(
            item_id in ids
            and payload["storageKind"] == storage_kind
            and payload["categoryType"] == category_type
            for storage_kind, category_type, ids in active_blacklist
        ):
            removed_ids.add(item_id)
            removals.append(
                {
                    "removedId": item_id,
                    "name": payload["name"],
                    "iconId": payload["iconId"],
                    "reason": "blacklist",
                }
            )
    while True:
        orphaned = []
        for item_id, payload in normalized.items():
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
            payload = normalized[item_id]
            removals.append(
                {
                    "removedId": item_id,
                    "name": payload["name"],
                    "iconId": payload["iconId"],
                    "reason": "blacklist-orphan",
                }
            )

    referenced_ids = {
        container_id
        for item_id, payload in normalized.items()
        if item_id not in removed_ids
        for container_id in payload["fullContainers"]
    }
    icon_groups: dict[str, list[str]] = defaultdict(list)
    for item_id, payload in normalized.items():
        if item_id not in removed_ids and payload["iconId"]:
            icon_groups[payload["iconId"]].append(item_id)
    for icon_id, member_ids in icon_groups.items():
        if len(member_ids) < 2:
            continue
        regular_ids = [
            item_id
            for item_id in member_ids
            if not normalized[item_id]["name"].startswith("模拟")
        ]
        if not regular_ids:
            continue
        kept_id = min(regular_ids, key=lambda item_id: (len(item_id), item_id))
        for item_id in member_ids:
            if not normalized[item_id]["name"].startswith("模拟"):
                continue
            removed_ids.add(item_id)
            removals.append(
                {
                    "removedId": item_id,
                    "keptId": kept_id,
                    "name": normalized[item_id]["name"],
                    "iconId": icon_id,
                    "reason": "simulated-duplicate-icon",
                }
            )

    duplicate_groups: dict[tuple[str, str], list[str]] = defaultdict(list)
    for item_id, payload in normalized.items():
        if item_id not in removed_ids:
            duplicate_groups[(payload["name"], payload["iconId"])].append(item_id)
    for (name, icon_id), member_ids in duplicate_groups.items():
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
                    "name": name,
                    "iconId": icon_id,
                    "reason": reason,
                }
            )

    items = {
        item_id: payload
        for item_id, payload in normalized.items()
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
        "iconId": item_id,
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
            "name": fixed["i18nKey"],
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
    items: Mapping[str, Mapping[str, Any]], image_root: Path
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
            url=f"{IMAGE_BASE_URL}/{quote(icon_id, safe='')}.png@raw",
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
        candidates = [
            path
            for path in image_root.glob(f"*/{icon_id}.png")
            if path.is_file() and path != destination
        ]
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


def fetch(url: str, destination: Path, *, timeout: float = 60) -> dict[str, Any]:
    """使用 ETag/Last-Modified 条件请求原子更新单个远端文件。"""

    destination.parent.mkdir(parents=True, exist_ok=True)
    metadata_path = _metadata_path(destination)
    metadata = (
        json.loads(metadata_path.read_text(encoding="utf-8"))
        if metadata_path.is_file()
        else {}
    )
    headers = {"User-Agent": "MaaEnd-IconRecognition/1.0"}
    if destination.is_file():
        if isinstance(metadata.get("etag"), str):
            headers["If-None-Match"] = metadata["etag"]
        if isinstance(metadata.get("lastModified"), str):
            headers["If-Modified-Since"] = metadata["lastModified"]
    request = Request(url, headers=headers)
    try:
        with urlopen(request, timeout=timeout) as response:
            data = response.read()
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
    except HTTPError as error:
        if error.code == 304 and destination.is_file():
            return metadata
        raise

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
    if is_valid_png(job.destination):
        return "skipped"
    job.destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = job.destination.with_suffix(job.destination.suffix + ".part")
    request = Request(
        job.url, headers={"User-Agent": "MaaEnd-IconRecognition/1.0"}
    )
    try:
        with urlopen(request, timeout=timeout) as response:
            content = response.read()
        validate_icon_png_bytes(content)
        temporary.write_bytes(content)
        temporary.replace(job.destination)
        return "downloaded"
    finally:
        temporary.unlink(missing_ok=True)


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
                failures.append(
                    {
                        "iconId": job.icon_id,
                        "rarity": job.rarity,
                        "url": job.url,
                        "destination": str(job.destination),
                        "errorType": type(error).__name__,
                        "error": str(error),
                    }
                )
    failures.sort(key=lambda row: (row["rarity"], row["iconId"]))
    return {
        "total": len(jobs),
        "downloaded": downloaded,
        "skipped": skipped,
        "failed": len(failures),
        "failures": failures,
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
    request = Request(
        url, headers={"User-Agent": "MaaEnd-IconRecognition/1.0"}
    )
    try:
        with urlopen(request, timeout=timeout) as response:
            return response.read(len(PNG_SIGNATURE)) == PNG_SIGNATURE
    except HTTPError as error:
        if error.code == 404:
            return False
        raise


def download_sources(
    cache_root: str | Path,
    *,
    timeout: float = 60,
    dry_run: bool = False,
) -> dict[str, str]:
    root = Path(cache_root)
    timestamp_ms = time.time_ns() // 1_000_000
    jobs = {
        "item_mini_table.json": _timestamped_url(
            ITEM_TABLE_URL, timestamp_ms=timestamp_ms
        ),
        "weapons.json": _timestamped_url(
            WEAPON_TABLE_URL, timestamp_ms=timestamp_ms
        ),
    }
    jobs.update(
        {
            f"lang_{locale}.json": _timestamped_url(
                LANG_URL.format(locale=locale),
                timestamp_ms=timestamp_ms,
            )
            for locale in LOCALES
        }
    )
    if dry_run:
        return jobs
    for filename, url in jobs.items():
        fetch(url, root / filename, timeout=timeout)
    _write_json(root / "source_download_report.json", jobs)
    return jobs


def run(
    cache_root: str | Path,
    *,
    workers: int,
    timeout: float,
) -> dict[str, Any]:
    root = Path(cache_root)
    download_sources(root, timeout=timeout)
    items, removals = prepare_item_map(
        _load_json(root / "item_mini_table.json"),
        blacklist=load_item_blacklist(),
    )
    weapons = prepare_weapon_map(
        _load_json(root / "weapons.json"),
        probe_icon=lambda icon_id: probe_icon_downloadable(
            icon_id, timeout=timeout
        ),
    )
    merged = merge_item_sources(items, weapons)
    _write_json(root / "item.json", merged)
    jobs, missing_icons = build_download_jobs(merged, root / "images")
    relocated = relocate_rarity_changed_icons(jobs, root / "images")
    report = download_images(jobs, workers=workers, timeout=timeout)
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
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--timeout", type=float, default=60)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if args.dry_run:
        print(
            json.dumps(
                download_sources(
                    args.cache_root, timeout=args.timeout, dry_run=True
                ),
                ensure_ascii=False,
                indent=4,
            )
        )
        return 0
    report = run(
        args.cache_root, workers=args.workers, timeout=args.timeout
    )
    print(json.dumps(report, ensure_ascii=False, indent=4))
    return 1 if report["failed"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
