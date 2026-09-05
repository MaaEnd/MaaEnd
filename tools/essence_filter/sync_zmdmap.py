#!/usr/bin/env python3
"""从 zmdmap 同步基质筛选的两份源数据。"""

from __future__ import annotations

import argparse
import json
import os
import time
from datetime import datetime, timezone
from email.utils import parsedate_to_datetime
from pathlib import Path
from typing import Any
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

import json5


VERSION_API = "https://api.zmdmap.com/api/v1/endfield/version"
# zmdmap 数据 CI 已把精简游戏数据发布到 assets.fz.wiki/output_maaend（旧地址
# assets.zmdmap.com/data/entity 已停用），下载时需带 ?ver=<version>。
DATA_BASE_URL = "https://assets.fz.wiki/output_maaend"
LANGS = ("CN", "TC", "EN", "JP", "KR")

REPO_ROOT = Path(__file__).resolve().parents[2]
DATA_DIR = REPO_ROOT / "assets" / "data" / "EssenceFilter"
TARGETS = {
    "energy_point_gems.json": DATA_DIR / "energy_point_gems.json",
    "weapons.json": DATA_DIR / "weapons_output.json",
}


def _url(url: str) -> str:
    sep = "&" if "?" in url else "?"
    return f"{url}{sep}{urlencode({'source': 'MaaEnd', 't': time.time_ns() // 1_000_000})}"


def _download_json(url: str) -> tuple[Any, str]:
    try:
        request = Request(_url(url), headers={"User-Agent": "MaaEnd/EssenceFilter"})
        with urlopen(request, timeout=60) as response:
            return json.loads(response.read()), response.headers.get("Last-Modified", "")
    except (OSError, ValueError) as error:
        print(f"[EssenceFilter] 跳过无效数据 {url}: {error}")
        return None, ""


def _latest_version() -> str | None:
    payload, _ = _download_json(VERSION_API)
    try:
        version = payload["data"]["list"][0]["version"]
    except (KeyError, IndexError, TypeError):
        print("[EssenceFilter] 跳过同步：版本接口没有有效版本")
        return None
    return version if isinstance(version, str) and version else None


def _valid_energy_points(data: Any) -> bool:
    return isinstance(data, list) and bool(data) and all(
        isinstance(row, dict)
        and isinstance(row.get("pointName"), str)
        and isinstance(row.get("secAttrTermNames"), list)
        and isinstance(row.get("skillTermNames"), list)
        for row in data
    )


def _valid_weapons(data: Any) -> bool:
    return isinstance(data, dict) and bool(data) and all(
        isinstance(row, dict)
        and isinstance(row.get("skills"), dict)
        and all(isinstance(row["skills"].get(lang), list) for lang in LANGS)
        for row in data.values()
    )


def _download_sources(version: str) -> tuple[dict[str, Any], datetime | None] | None:
    validators = {
        "energy_point_gems.json": _valid_energy_points,
        "weapons.json": _valid_weapons,
    }
    result: dict[str, Any] = {}
    last_modified: list[str] = []
    for filename, validator in validators.items():
        # output_maaend 使用查询参数 ?ver=<version> 指定数据版本（与 fetch-data.mjs 一致）。
        url = f"{DATA_BASE_URL}/{filename}?ver={quote(version, safe='')}"
        data, modified = _download_json(url)
        if not validator(data):
            print(f"[EssenceFilter] 跳过同步：{filename} 为空或结构无效")
            return None
        result[filename] = data
        last_modified.append(modified)
    try:
        modified_at = max(
            parsedate_to_datetime(value).astimezone(timezone.utc)
            for value in last_modified
        )
    except ValueError:
        print("[EssenceFilter] Last-Modified 缺失或无效，保留原数据日期")
        modified_at = None
    return result, modified_at


def _write_json(path: Path, data: Any) -> None:
    path.write_text(
        json.dumps(data, ensure_ascii=False, indent=4) + "\n", encoding="utf-8"
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--check-version",
        help="只检查指定版本的两份远端数据，不写入本地文件",
    )
    args = parser.parse_args()

    version = args.check_version or _latest_version()
    if not version:
        return 1 if args.check_version else 0

    print(f"[EssenceFilter] 检查 zmdmap {version}")
    downloaded = _download_sources(version)
    if downloaded is None:
        return 1 if args.check_version else 0
    sources, modified_at = downloaded
    data_version = modified_at.strftime("%Y-%m-%d %H:%M:%S UTC") if modified_at else ""
    if data_version:
        print(f"[EssenceFilter] 上游 Last-Modified: {data_version}")
    if args.check_version:
        print(f"[EssenceFilter] zmdmap {version} 数据有效")
        return 0

    changed = [
        filename
        for filename, path in TARGETS.items()
        if json.loads(path.read_text(encoding="utf-8")) != sources[filename]
    ]
    version_changed = False
    if data_version:
        config = json5.loads(
            (DATA_DIR / "matcher_config.json").read_text(encoding="utf-8-sig")
        )
        version_changed = config.get("data_version") != data_version

    for filename in changed:
        _write_json(TARGETS[filename], sources[filename])
        print(f"[EssenceFilter] 已更新 {TARGETS[filename].relative_to(REPO_ROOT)}")

    # 时间随本轮源数据传给生成步骤，全部生成成功后才写入 matcher_config。
    if github_output := os.environ.get("GITHUB_OUTPUT"):
        with Path(github_output).open("a", encoding="utf-8") as output:
            output.write(f"updated={str(bool(changed) or version_changed).lower()}\n")
            output.write(f"data_version={data_version}\n")
    if not changed and not version_changed:
        print("[EssenceFilter] 本地数据已是最新")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
