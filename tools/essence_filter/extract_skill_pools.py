#!/usr/bin/env python3
"""
从 weapons_output.json 提取 skill_pools（slot1/slot2/slot3）。

- 从每个武器的 skills 按位置归入 slot：idx0=slot1, idx1=slot2(若 len=3) 或 slot3(若 len=2), idx2=slot3
- 技能名取「基名」：按 · / : / ： / [ 分割，取第一段（统一处理不同格式如 ·小、: xxx、[S] 等）
- 五语（cn/tc/en/jp/kr）直接从 weapons_output 的 skills.CN/TC/EN/JP/KR 同位置提取，不再依赖 i18n
- 每 slot 用 set 去重；id 分配规则：若存在已有的 skill_pools 文件，已出现的 (slot, cn) 保留原 id，
  新增技能在该 slot 内分配「当前最大 id + 1」，保证同一技能 id 不随新增/删除其他技能而变化。
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Dict, List, Set, Tuple

LANGS = ("CN", "TC", "EN", "JP", "KR")

DEFAULT_MATCHER_CONFIG = Path("assets/data/EssenceFilter/matcher_config.json")


def load_suffix_stopwords(config_path: Path) -> Dict[str, List[str]]:
    """返回 { "CN": [...], "TC": [...], ... }，从 matcher_config 读取；支持多语种对象或旧版仅 CN 数组。"""
    default: Dict[str, List[str]] = {
        "CN": ["提升", "提高", "强化", "增幅", "效果", "效率", "伤害", "倍率"],
        "TC": ["提升", "提高", "強化", "增幅", "效果", "效率", "傷害", "倍率"],
        "EN": [" Boost", " Up", " DMG", " Increase", " Efficiency"],
        "JP": ["UP", "アップ", "ダメージ", "効率", "ブースト"],
        "KR": [" 증가", " 부스트", " 피해", " 효율", " 강도"],
    }
    if not config_path.exists():
        return default
    with config_path.open("r", encoding="utf-8") as f:
        data = json.load(f)
    stopwords = data.get("suffixStopwords")
    if isinstance(stopwords, dict):
        return {
            lang: list(stopwords.get(lang, default.get(lang, []))) for lang in LANGS
        }
    if isinstance(stopwords, list):
        result = {**default, "CN": list(stopwords)}
        return result
    return default


def strip_suffix_stopwords(text: str, stopwords: List[str]) -> str:
    """从末尾反复去掉停用词，如 '力量提升' -> '力量'。

    - 若整词就是停用词（如 '効率'）则不剥掉，避免 slot3 等基名被清空。
    - 兼容 stopwords 写法差异（如 ' DMG' 与 'DMG'）。
    - 对 ASCII 词尾增加词边界保护，避免误删如 'Setup' 里的 'Up'。
    """
    s = text.strip()
    normalized = [w.strip() for w in stopwords if isinstance(w, str) and w.strip()]
    changed = True
    while changed and s:
        changed = False
        for w in normalized:
            if not s.endswith(w) or len(s) <= len(w):
                continue

            prefix = s[: -len(w)]
            if w.isascii() and w.isalnum():
                prev = prefix[-1]
                if prev.isascii() and prev.isalnum():
                    continue

            s = prefix.rstrip()
            changed = True
            break
    return s


def base_skill_name(raw: str, lang: str, lang_stopwords: Dict[str, List[str]]) -> str:
    """取基名：按 · / ・ / : / ： / [ 分割，取第一段。例如：
    - '力量提升·小' -> '力量提升'
    - '压制·应急强化' -> '压制'
    - 'Strength Boost [S]' -> 'Strength Boost'
    - 'Assault: Armament Prep' -> 'Assault'
    - 'メイン能力UP・大' -> 'メイン能力UP'
    再按各语言的 suffixStopwords 去掉尾缀。
    """
    s = raw.strip()
    for sep in ("·", "・", ":", "：", "["):
        if sep in s:
            s = s.split(sep)[0].strip()
            break
    stopwords = lang_stopwords.get(lang, [])
    if stopwords:
        s = strip_suffix_stopwords(s, stopwords)
    return s


def extract_skills_by_slot(
    weapons_data: Dict,
    lang_stopwords: Dict[str, List[str]],
) -> Tuple[
    Set[str],
    Set[str],
    Set[str],
    Dict[Tuple[str, str], Dict[str, str]],
]:
    """从 weapons_output 提取 slot 集合，以及 (slot_key, base_cn) -> {cn, tc, en, jp, kr} 的翻译映射。"""
    slot1: Set[str] = set()
    slot2: Set[str] = set()
    slot3: Set[str] = set()
    # (slot_key, base_cn) -> {cn, tc, en, jp, kr}，优先保留已有条目（首次出现）
    translations: Dict[Tuple[str, str], Dict[str, str]] = {}

    for _wpn_id, wpn in weapons_data.items():
        if not isinstance(wpn, dict):
            continue
        skills_obj = wpn.get("skills")
        if not isinstance(skills_obj, dict):
            continue
        cn_list = skills_obj.get("CN")
        if not isinstance(cn_list, list):
            continue

        for i, cn_raw in enumerate(cn_list):
            if not isinstance(cn_raw, str):
                continue
            base_cn = base_skill_name(cn_raw, "CN", lang_stopwords)
            if not base_cn:
                continue

            if i == 0:
                slot_key = "slot1"
            elif i == 1:
                slot_key = "slot2" if len(cn_list) == 3 else "slot3"
            else:
                slot_key = "slot3"

            if slot_key == "slot1":
                slot1.add(base_cn)
            elif slot_key == "slot2":
                slot2.add(base_cn)
            else:
                slot3.add(base_cn)

            key = (slot_key, base_cn)
            if key not in translations:
                row: Dict[str, str] = {
                    "cn": base_cn,
                    "tc": "",
                    "en": "",
                    "jp": "",
                    "kr": "",
                }
                for lang in LANGS:
                    lang_list = skills_obj.get(lang)
                    if isinstance(lang_list, list) and i < len(lang_list):
                        val = lang_list[i]
                        if isinstance(val, str):
                            row[lang.lower()] = base_skill_name(
                                val, lang, lang_stopwords
                            )
                translations[key] = row

    return slot1, slot2, slot3, translations


def load_existing_ids(path: Path) -> Tuple[Dict[Tuple[str, str], int], Dict[str, int]]:
    """从已有 skill_pools.json 加载 (slot_key, cn) -> id，以及每 slot 的 max_id。若文件不存在或格式不对则返回空。"""
    existing_cn_to_id: Dict[Tuple[str, str], int] = {}
    max_id_by_slot: Dict[str, int] = {}
    if not path.exists():
        return existing_cn_to_id, max_id_by_slot
    try:
        with path.open("r", encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, json.JSONDecodeError):
        return existing_cn_to_id, max_id_by_slot
    if not isinstance(data, dict):
        return existing_cn_to_id, max_id_by_slot
    for slot_key in ("slot1", "slot2", "slot3"):
        lst = data.get(slot_key)
        if not isinstance(lst, list):
            continue
        slot_max = 0
        for item in lst:
            if not isinstance(item, dict):
                continue
            cn = item.get("cn")
            id_val = item.get("id")
            if isinstance(cn, str) and isinstance(id_val, int):
                existing_cn_to_id[(slot_key, cn)] = id_val
                slot_max = max(slot_max, id_val)
        max_id_by_slot[slot_key] = slot_max
    return existing_cn_to_id, max_id_by_slot


def build_skill_pools(
    slot1_set: Set[str],
    slot2_set: Set[str],
    slot3_set: Set[str],
    translations: Dict[Tuple[str, str], Dict[str, str]],
    existing_cn_to_id: Dict[Tuple[str, str], int] | None = None,
    max_id_by_slot: Dict[str, int] | None = None,
) -> Dict[str, List[Dict]]:
    """赋稳定 id：已有 (slot, cn) 沿用旧 id，新增技能用该 slot 的 max_id+1；最终按 id 排序输出。"""
    existing = existing_cn_to_id or {}
    max_ids = max_id_by_slot or {}
    out: Dict[str, List[Dict]] = {}

    for key, s in (("slot1", slot1_set), ("slot2", slot2_set), ("slot3", slot3_set)):
        next_id = (max_ids.get(key) or 0) + 1
        entries: List[Dict] = []
        for base_cn in sorted(s):
            t = translations.get(
                (key, base_cn), {"cn": base_cn, "tc": "", "en": "", "jp": "", "kr": ""}
            )
            eid = existing.get((key, base_cn))
            if eid is None:
                eid = next_id
                next_id += 1
            entries.append(
                {
                    "cn": t["cn"],
                    "tc": t.get("tc", ""),
                    "en": t.get("en", ""),
                    "jp": t.get("jp", ""),
                    "kr": t.get("kr", ""),
                    "id": eid,
                }
            )
        entries.sort(key=lambda x: x["id"])
        out[key] = entries

    return out


def write_skill_pools(path: Path, data: Dict) -> None:
    with path.open("w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=4)


def main() -> int:
    parser = argparse.ArgumentParser(
        description="从 weapons_output.json 提取 skill_pools（五语来自同文件）"
    )
    parser.add_argument(
        "--input",
        type=Path,
        default=Path("assets/data/EssenceFilter/weapons_output.json"),
        help="weapons_output.json 路径",
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=Path("assets/data/EssenceFilter/skill_pools.json"),
        help="输出的 skill_pools.json 路径",
    )
    parser.add_argument(
        "--base-dir",
        type=Path,
        default=Path.cwd(),
        help="仓库根目录",
    )
    args = parser.parse_args()

    base_dir = args.base_dir.resolve()
    input_path = args.input if args.input.is_absolute() else base_dir / args.input
    output_path = args.output if args.output.is_absolute() else base_dir / args.output

    if not input_path.exists():
        print(f"[ERROR] 输入文件不存在: {input_path}", file=sys.stderr)
        return 1

    with input_path.open("r", encoding="utf-8") as f:
        weapons_data = json.load(f)

    if not isinstance(weapons_data, dict):
        print("[ERROR] weapons_output 应为 JSON object", file=sys.stderr)
        return 1

    matcher_config_path = base_dir / DEFAULT_MATCHER_CONFIG
    lang_stopwords = load_suffix_stopwords(matcher_config_path)

    slot1_set, slot2_set, slot3_set, translations = extract_skills_by_slot(
        weapons_data, lang_stopwords
    )
    existing_cn_to_id, max_id_by_slot = load_existing_ids(output_path)
    pools = build_skill_pools(
        slot1_set,
        slot2_set,
        slot3_set,
        translations,
        existing_cn_to_id=existing_cn_to_id,
        max_id_by_slot=max_id_by_slot,
    )

    output_path.parent.mkdir(parents=True, exist_ok=True)
    write_skill_pools(output_path, pools)
    print(f"[INFO] 已写入: {output_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
