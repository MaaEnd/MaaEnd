#!/usr/bin/env python3
"""把 zmdmap 的精简数据同步成基质筛选能直接用的那几份。

数据源在 ``tools/zmdmap/data/``（``uv run zmdmap fetch`` 下载，或 ``uv run zmdmap generate``
本地生成）：

- ``energy_point_gems.json``：构建期输入，由 ``build_locations.py`` 直接读，不落这里；
- ``weapons.json``：既当构建期输入（技能池、任务选项），也是**运行时资源**——Go 侧匹配引擎在
  运行时从资源目录读 ``assets/data/EssenceFilter/weapons_output.json``，所以这里把它复制过去。
"""

import json
from pathlib import Path
from typing import Any

from tools.zmdmap import read_payload

REPO_ROOT = Path(__file__).resolve().parents[2]
ZMDMAP_DATA_DIR = REPO_ROOT / "tools" / "zmdmap" / "data"
DATA_DIR = REPO_ROOT / "assets" / "data" / "EssenceFilter"
LANGS = ("CN", "TC", "EN", "JP", "KR")

WEAPONS_SOURCE = ZMDMAP_DATA_DIR / "weapons.json"
WEAPONS_RUNTIME = DATA_DIR / "weapons_output.json"


def _valid_weapons(data: Any) -> bool:
    return isinstance(data, dict) and bool(data) and all(
        isinstance(row, dict)
        and isinstance(row.get("names"), dict)
        and isinstance(row.get("skills"), dict)
        and all(isinstance(row["skills"].get(lang), list) for lang in LANGS)
        for row in data.values()
    )


def main() -> int:
    weapons = read_payload(WEAPONS_SOURCE)
    if not _valid_weapons(weapons):
        raise ValueError(f"{WEAPONS_SOURCE} 为空或结构无效")

    formatted = json.dumps(weapons, ensure_ascii=False, indent=4) + "\n"
    previous = WEAPONS_RUNTIME.read_text(encoding="utf-8") if WEAPONS_RUNTIME.is_file() else ""
    if previous == formatted:
        print(f"[EssenceFilter] {WEAPONS_RUNTIME.relative_to(REPO_ROOT)} 未变化")
        return 0

    WEAPONS_RUNTIME.parent.mkdir(parents=True, exist_ok=True)
    WEAPONS_RUNTIME.write_text(formatted, encoding="utf-8")
    print(f"[EssenceFilter] 已更新 {WEAPONS_RUNTIME.relative_to(REPO_ROOT)}（{len(weapons)} 把武器）")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
