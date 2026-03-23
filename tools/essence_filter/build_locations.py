#!/usr/bin/env python3
"""
将 weapons_data.json 的 locations（slot2_ids / slot3_ids 为 weapons_data skill_pools 的 id）
映射到当前 skill_pools.json 的 id 与多语种名称，写出 locations.json。

- weapons_data.skill_pools 与 skill_pools.json 的 id 可能不一致，通过中文名匹配。
- 输出：每个 location 含 name、slot2_ids/slot3_ids（当前 skill_pools 的 id）、slot2/slot3（完整条目）。
"""

from __future__ import annotations

import argparse
import json
import unicodedata
from pathlib import Path
from typing import Any, Dict, List, Tuple

DEFAULT_WEAPONS_DATA = Path("assets/data/EssenceFilter/weapons_data.json")
DEFAULT_SKILL_POOLS = Path("assets/data/EssenceFilter/skill_pools.json")
DEFAULT_OUTPUT = Path("assets/data/EssenceFilter/locations.json")

# slot2 中文后缀，用于从 weapons_data 的「攻击提升」等得到基名；只保留通用后缀，按长度从长到短
# 不用「充能效率提升」整段，否则「终结技充能效率提升」会变成「终结技」
SLOT2_CN_SUFFIXES = (
    "伤害提升",
    "效率提升",
    "强度提升",
    "提升",
)


# 用于匹配的简繁归一（仅影响 slot2 基名匹配）
_TC_TO_SC = str.maketrans("強藝", "强艺")

# slot2 基名歧义：strip 強度提升 会得到「源石技艺」，对应 pool 的「源石技艺强度」
SLOT2_STEM_ALIAS: Dict[str, str] = {"源石技艺": "源石技艺强度"}


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


def build_wd_id_to_pool_id(
    wd_slot: List[Dict[str, Any]],
    pool_slot: List[Dict[str, Any]],
    slot_name: str,
    use_stem: bool = False,
    debug: bool = False,
) -> Dict[int, int]:
    """weapons_data 的 slot id -> skill_pools 的 slot id。"""
    pool_cn_to_id: Dict[str, int] = {}
    for ent in pool_slot:
        c = _norm_key(ent.get("cn") or "")
        if c:
            pool_cn_to_id[c] = int(ent["id"])

    wd_id_to_pool_id: Dict[int, int] = {}
    for ent in wd_slot:
        raw = _norm(ent.get("chinese") or "")
        c = _slot2_chinese_stem(raw) if use_stem else _norm_key(raw)
        pool_id = pool_cn_to_id.get(c)
        if pool_id is None and use_stem:
            pool_id = pool_cn_to_id.get(SLOT2_STEM_ALIAS.get(c, c))
        if pool_id is None:
            for pe in pool_slot:
                pc = _norm_key(pe.get("cn") or "")
                if pc and pc == c:
                    pool_id = int(pe["id"])
                    break
        if pool_id is not None:
            wd_id_to_pool_id[int(ent["id"])] = pool_id
        elif debug:
            print(f"[{slot_name}] wd id {ent['id']} stem {c!r} not in pool (sample: {list(pool_cn_to_id.keys())[:2]!r})")
    return wd_id_to_pool_id


def main() -> int:
    root = Path(__file__).resolve().parent.parent.parent
    parser = argparse.ArgumentParser(description="Map locations to current skill_pools and write locations.json")
    parser.add_argument("--weapons-data", type=Path, default=root / DEFAULT_WEAPONS_DATA)
    parser.add_argument("--skill-pools", type=Path, default=root / DEFAULT_SKILL_POOLS)
    parser.add_argument("-o", "--output", type=Path, default=root / DEFAULT_OUTPUT)
    parser.add_argument("--debug", action="store_true", help="打印映射与未匹配的键")
    args = parser.parse_args()

    with args.weapons_data.open("r", encoding="utf-8") as f:
        wd = json.load(f)
    with args.skill_pools.open("r", encoding="utf-8") as f:
        pools = json.load(f)

    skill_pools_wd = wd.get("skill_pools") or {}
    wd_slot2 = skill_pools_wd.get("slot2") or []
    wd_slot3 = skill_pools_wd.get("slot3") or []
    pool_slot2 = pools.get("slot2") or []
    pool_slot3 = pools.get("slot3") or []

    wd_to_pool_slot2 = build_wd_id_to_pool_id(
        wd_slot2, pool_slot2, "slot2", use_stem=True, debug=args.debug
    )
    wd_to_pool_slot3 = build_wd_id_to_pool_id(
        wd_slot3, pool_slot3, "slot3", use_stem=False, debug=args.debug
    )

    pool2_by_id = {int(e["id"]): e for e in pool_slot2}
    pool3_by_id = {int(e["id"]): e for e in pool_slot3}

    locations_raw = wd.get("locations") or []
    out_locations: List[Dict[str, Any]] = []

    for loc in locations_raw:
        name = loc.get("name") or ""
        slot2_ids_wd = loc.get("slot2_ids") or []
        slot3_ids_wd = loc.get("slot3_ids") or []

        slot2_ids = [wd_to_pool_slot2.get(i) for i in slot2_ids_wd]
        slot3_ids = [wd_to_pool_slot3.get(i) for i in slot3_ids_wd]
        if None in slot2_ids or None in slot3_ids:
            missing2 = [slot2_ids_wd[i] for i, p in enumerate(slot2_ids) if p is None]
            missing3 = [slot3_ids_wd[i] for i, p in enumerate(slot3_ids) if p is None]
            raise ValueError(f"location {name!r}: 无法映射 slot2_ids {missing2} 或 slot3_ids {missing3} 到 skill_pools")

        slot2_entries = [pool2_by_id[i] for i in slot2_ids]
        slot3_entries = [pool3_by_id[i] for i in slot3_ids]

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
