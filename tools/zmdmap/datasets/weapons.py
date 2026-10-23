"""武器表：EssenceFilter 与 IconRecognition 共用的那份精简武器数据。

消费端各自要的字段不一样，合起来是这些（其余一概不生成）：

| 字段 | 谁要 |
| --- | --- |
| ``internal_id`` / ``icon_id`` / ``rarity`` / ``names{五语}`` | IconRecognition 的图标目录与五语名称 |
| ``rarity`` / ``weapon_type`` / ``skills{五语}`` / ``names{五语}`` | EssenceFilter 的技能池与任务选项 |

数据来自 ``WeaponBasicTable`` + ``ForesightWeaponTable`` + ``ItemTable`` + ``SkillPatchTable``：
前者是已上线的武器，后者是还没上线的前瞻武器（同一把武器上线后会出现在 ``WeaponBasicTable`` 里，
所以要先按 id、再按五语名字去重）。输出里不含 ``icon_path``、``foresight``、``skill_ids`` 与其余语言的
字段：消费端没有读它们。
"""

from pathlib import Path
from typing import Any

from ..utils.config import UPSTREAM_LOCALES
from ..utils.game_data import ref_texts, text_id_of, texts_of
from ..utils.log import logger
from ..utils.tables import (
    ForesightWeaponRow,
    ItemRow,
    SkillPatchRow,
    TextRef,
    WeaponBasicRow,
    load_table,
)
from .base import Dataset


class Weapons(Dataset):
    """EssenceFilter 与 IconRecognition 共用的武器表（``weapons.json``）。"""

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, dict[str, Any]]:
        """武器：稀有度、类型、图标 id 与五语名字/技能名（含还没上线的前瞻武器）。"""
        weapons = load_table(table_cfg_dir, "WeaponBasicTable", WeaponBasicRow)
        items = load_table(table_cfg_dir, "ItemTable", ItemRow)
        patches = load_table(table_cfg_dir, "SkillPatchTable", SkillPatchRow)
        foresight = load_table(table_cfg_dir, "ForesightWeaponTable", ForesightWeaponRow)

        skill_name_ids = _skill_name_ids(patches)
        output: dict[str, dict[str, Any]] = {}
        released: set[str] = set()

        for weapon_id, weapon in weapons.items():
            item = items.get(weapon_id)
            output[weapon_id] = _weapon_entry(
                table_cfg_dir,
                weapon_id=weapon_id,
                icon_id=item.icon_id if item else "",
                name_ref=item.name if item else weapon.eng_name,
                weapon_type=weapon.weapon_type,
                rarity=weapon.rarity,
                skill_ids=weapon.weapon_skill_list,
                skill_name_ids=skill_name_ids,
            )
            released.add(weapon_id)

        for row_key, row in foresight.items():
            # ``weaponId`` 才是真 id，行 key 不可靠（有的 dump 整表按数字排序 id 做 key）。
            weapon_id = row.weapon_id or row_key
            if _already_released(table_cfg_dir, weapon_id, row.name, output, released):
                logger.warning(f"前瞻武器 {row_key}（{weapon_id}）已经发布过，跳过")
                continue
            skill_ids = list(row.sk_ids)
            output[weapon_id] = _weapon_entry(
                table_cfg_dir,
                weapon_id=weapon_id,
                # 前瞻武器还没有 ItemTable 行，自家 iconId 就是图标键。
                icon_id=row.icon_id or weapon_id,
                name_ref=row.name,
                weapon_type=row.wpn_type,
                rarity=row.rarity,
                skill_ids=skill_ids,
                skill_name_ids=skill_name_ids,
                row_skill_names=_row_skill_names(table_cfg_dir, skill_ids, row.sk_names),
            )
            logger.info(f"前瞻武器 {weapon_id} 入库")

        logger.info(f"武器 {len(output)} 把")
        return output


def _already_released(
    table_cfg_dir: Path,
    weapon_id: str,
    name_ref: TextRef | None,
    output: dict[str, dict[str, Any]],
    released: set[str],
) -> bool:
    """前瞻武器是否已经作为正式武器发布：先认 id，再逐语比对名字。

    id 通常会跟着升级保留，但不保证；重新给过 id 的武器仍然指向同一段游戏文本，所以名字全中
    也算同一把（只中一部分算翻译差异，不算重复）。
    """
    if weapon_id in released:
        return True

    known = {suffix: text for suffix, text in ref_texts(table_cfg_dir, name_ref).items() if text}
    if not known:
        return False
    for other_id in released:
        names = output[other_id]["names"]
        if all(names.get(suffix) == text for suffix, text in known.items()):
            return True
    return False


def _skill_name_ids(patches: dict[str, SkillPatchRow]) -> dict[str, int]:
    """技能 id -> 技能名文本 id（取 ``SkillPatchDataBundle`` 第一级的名字）。"""
    mapping: dict[str, int] = {}
    for skill_id, data in patches.items():
        bundle = data.skill_patch_data_bundle
        if not bundle:
            continue
        text_id = text_id_of(bundle[0].skill_name)
        if text_id:
            mapping[skill_id] = text_id
    return mapping


def _row_skill_names(
    table_cfg_dir: Path,
    skill_ids: list[str],
    skill_refs: list[TextRef],
) -> dict[str, dict[str, str]]:
    """前瞻武器自带的 ``skNames``：它的专属技能还没进 ``SkillPatchTable``。"""
    result: dict[str, dict[str, str]] = {}
    for skill_id, ref in zip(skill_ids, skill_refs, strict=False):
        texts = ref_texts(table_cfg_dir, ref)
        if any(texts.values()):
            result[skill_id] = texts
    return result


def _weapon_entry(
    table_cfg_dir: Path,
    *,
    weapon_id: str,
    icon_id: str,
    name_ref: TextRef | None,
    weapon_type: str,
    rarity: int,
    skill_ids: list[str],
    skill_name_ids: dict[str, int],
    row_skill_names: dict[str, dict[str, str]] | None = None,
) -> dict[str, Any]:
    """一把武器的条目；图标 id 缺了就退回武器 id，技能名查不到就退回技能 id。"""
    overrides = row_skill_names or {}
    skills: dict[str, list[str]] = {suffix: [] for suffix, _ in UPSTREAM_LOCALES}
    for skill_id in skill_ids:
        names = overrides.get(skill_id)
        if names is None:
            text_id = skill_name_ids.get(skill_id)
            names = texts_of(table_cfg_dir, text_id) if text_id else {}
        for suffix in skills:
            skills[suffix].append(names.get(suffix) or skill_id)
    return {
        "internal_id": weapon_id,
        "icon_id": icon_id or None,
        "rarity": rarity,
        "weapon_type": weapon_type,
        "names": ref_texts(table_cfg_dir, name_ref),
        "skills": skills,
    }
