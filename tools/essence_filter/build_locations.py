#!/usr/bin/env python3
"""
从 energy_point_gems.json 解析 location 词条并映射到当前 skill_pools.json，写出 locations.json。

- 输入的 energy_point_gems.json 中，slot2 来自 secAttrTermNames（如「攻击提升」），
  slot3 来自 skillTermNames（如「强攻」）。
- 仅保留 costStamina > 0（重度）词条，并按 pointName 去重（同地点多 worldLevel 只取一条）。
- 输出：每个 location 含 name、slot2_ids/slot3_ids（当前 skill_pools 的 id）、slot2/slot3（完整条目）。
"""

from __future__ import annotations

import argparse
import json
import unicodedata
from pathlib import Path
from typing import Any, Dict, List

DEFAULT_ENERGY_POINTS = Path("assets/data/EssenceFilter/energy_point_gems.json")
DEFAULT_SKILL_POOLS = Path("assets/data/EssenceFilter/skill_pools.json")
DEFAULT_OUTPUT = Path("assets/data/EssenceFilter/locations.json")

# slot2 中文后缀，用于从 energy_point_gems 的「攻击提升」等得到基名；只保留通用后缀，按长度从长到短
# 不用「充能效率提升」整段，否则「终结技充能效率提升」会变成「终结技」
SLOT2_CN_SUFFIXES = (
    "伤害提升",
    "效率提升",
    "强度提升",
    "提升",
)


# 用于匹配的简繁归一（仅影响 slot2 基名匹配）
_TC_TO_SC = str.maketrans("強藝", "强艺")

# slot2 基名歧义：
# - strip 強度提升 会得到「源石技艺」，对应 pool 的「源石技艺强度」
# - strip 效率提升 会得到「终结技」，对应 pool 的「终结技充能」
SLOT2_STEM_ALIAS: Dict[str, str] = {
    "源石技艺": "源石技艺强度",
    "终结技": "终结技充能",
}


def _norm(s: str) -> str:
    return unicodedata.normalize("NFC", (s or "").strip())


def _norm_key(s: str) -> str:
    """归一化用于比对的键（简繁统一）。"""
    return _norm(s).translate(_TC_TO_SC)


def _slot2_chinese_stem(chinese: str) -> str:
    s = _norm(chinese)
    # 先尝试带「强」的后缀，再尝试带「強」的，保证都能截出基名
    for suf in SLOT2_CN_SUFFIXES:
        if s.endswith(suf):
            return _norm_key(s[: -len(suf)])
        suf_tc = suf.replace("强", "強")
        if suf_tc != suf and s.endswith(suf_tc):
            return _norm_key(s[: -len(suf_tc)])
    return _norm_key(s)


def main() -> int:
    root = Path(__file__).resolve().parent.parent.parent
    parser = argparse.ArgumentParser(
        description="Parse energy_point_gems and map to current skill_pools, then write locations.json"
    )
    parser.add_argument("--energy-points", type=Path, default=root / DEFAULT_ENERGY_POINTS)
    parser.add_argument("--skill-pools", type=Path, default=root / DEFAULT_SKILL_POOLS)
    parser.add_argument("-o", "--output", type=Path, default=root / DEFAULT_OUTPUT)
    parser.add_argument("--debug", action="store_true", help="打印映射与未匹配的键")
    args = parser.parse_args()

    with args.energy_points.open("r", encoding="utf-8") as f:
        energy_points = json.load(f)
    with args.skill_pools.open("r", encoding="utf-8") as f:
        pools = json.load(f)

    pool_slot2 = pools.get("slot2") or []
    pool_slot3 = pools.get("slot3") or []

    if not isinstance(energy_points, list):
        raise ValueError("energy_point_gems.json 应为 JSON array")

    pool2_by_cn = {_norm_key(e.get("cn") or ""): e for e in pool_slot2}
    pool3_by_cn = {_norm_key(e.get("cn") or ""): e for e in pool_slot3}

    out_locations: List[Dict[str, Any]] = []
    seen_names: set[str] = set()

    for loc in energy_points:
        if not isinstance(loc, dict):
            continue
        name = _norm(loc.get("pointName") or "")
        if not name:
            continue

        # 只保留重度词条（掉落基质）
        if not isinstance(loc.get("costStamina"), int) or int(loc["costStamina"]) <= 0:
            continue
        # 同地点不同 worldLevel 词条池一致，只保留第一条
        if name in seen_names:
            continue
        seen_names.add(name)

        sec_terms = loc.get("secAttrTermNames") or []
        skill_terms = loc.get("skillTermNames") or []
        if not isinstance(sec_terms, list) or not isinstance(skill_terms, list):
            raise ValueError(f"location {name!r}: secAttrTermNames 或 skillTermNames 不是数组")

        slot2_entries: List[Dict[str, Any]] = []
        slot3_entries: List[Dict[str, Any]] = []

        missing_slot2: List[str] = []
        for raw in sec_terms:
            if not isinstance(raw, str):
                continue
            stem = _slot2_chinese_stem(raw)
            alias = SLOT2_STEM_ALIAS.get(stem, stem)
            matched = pool2_by_cn.get(alias)
            if matched is None:
                missing_slot2.append(raw)
                continue
            slot2_entries.append(matched)

        missing_slot3: List[str] = []
        for raw in skill_terms:
            if not isinstance(raw, str):
                continue
            key = _norm_key(raw)
            matched = pool3_by_cn.get(key)
            if matched is None:
                missing_slot3.append(raw)
                continue
            slot3_entries.append(matched)

        if missing_slot2 or missing_slot3:
            if args.debug:
                if missing_slot2:
                    print(f"[slot2] {name!r} 未匹配: {missing_slot2}")
                if missing_slot3:
                    print(f"[slot3] {name!r} 未匹配: {missing_slot3}")
            raise ValueError(
                f"location {name!r}: 无法映射 slot2 {missing_slot2} 或 slot3 {missing_slot3} 到 skill_pools"
            )

        slot2_ids = [int(e["id"]) for e in slot2_entries]
        slot3_ids = [int(e["id"]) for e in slot3_entries]

        out_locations.append({
            "name": name,
            "slot2_ids": slot2_ids,
            "slot3_ids": slot3_ids,
            "slot2": slot2_entries,
            "slot3": slot3_entries,
        })

    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("w", encoding="utf-8") as f:
        json.dump(out_locations, f, ensure_ascii=False, indent=4)

    print(f"Wrote {len(out_locations)} locations to {args.output}")
    return 0


if __name__ == "__main__":
    import sys
    sys.exit(main())
