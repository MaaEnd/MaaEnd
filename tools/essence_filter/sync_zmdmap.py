#!/usr/bin/env python3
"""从 zmdmap 同步基质筛选的两份源数据。"""

from __future__ import annotations

import argparse
import json
import time
from pathlib import Path
from typing import Any
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen


VERSION_API = "https://api.zmdmap.com/api/v1/endfield/version"
DATA_BASE_URL = "https://assets.zmdmap.com/data/entity"
LANGS = ("CN", "TC", "EN", "JP", "KR")

REPO_ROOT = Path(__file__).resolve().parents[2]
DATA_DIR = REPO_ROOT / "assets" / "data" / "EssenceFilter"
TARGETS = {
    "energy_point_gems.json": DATA_DIR / "energy_point_gems.json",
    "weapons.json": DATA_DIR / "weapons_output.json",
}


def _url(url: str) -> str:
    return f"{url}?{urlencode({'source': 'MaaEnd', 't': time.time_ns() // 1_000_000})}"


def _download_json(url: str) -> Any | None:
    try:
        request = Request(_url(url), headers={"User-Agent": "MaaEnd/EssenceFilter"})
        with urlopen(request, timeout=60) as response:
            return json.loads(response.read())
    except (OSError, ValueError) as error:
        print(f"[EssenceFilter] 跳过无效数据 {url}: {error}")
        return None


def _latest_version() -> str | None:
    payload = _download_json(VERSION_API)
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


def _download_sources(version: str) -> dict[str, Any] | None:
    validators = {
        "energy_point_gems.json": _valid_energy_points,
        "weapons.json": _valid_weapons,
    }
    result: dict[str, Any] = {}
    for filename, validator in validators.items():
        url = f"{DATA_BASE_URL}/{quote(version, safe='')}/{filename}"
        data = _download_json(url)
        if not validator(data):
            print(f"[EssenceFilter] 跳过同步：{filename} 为空或结构无效")
            return None
        result[filename] = data
    return result


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
    sources = _download_sources(version)
    if sources is None:
        return 1 if args.check_version else 0
    if args.check_version:
        print(f"[EssenceFilter] zmdmap {version} 数据有效")
        return 0

    changed = [
        filename
        for filename, path in TARGETS.items()
        if json.loads(path.read_text(encoding="utf-8")) != sources[filename]
    ]
    if not changed:
        print("[EssenceFilter] 本地数据已是最新")
        return 0

    for filename in changed:
        _write_json(TARGETS[filename], sources[filename])
        print(f"[EssenceFilter] 已更新 {TARGETS[filename].relative_to(REPO_ROOT)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
