"""武器表：EssenceFilter 与 IconRecognition 共用的那份精简武器数据。

消费端各自要的字段不一样，合起来是这些（其余一概不生成）：

| 字段 | 谁要 |
| --- | --- |
| ``internal_id`` / ``icon_id`` / ``rarity`` / ``names{五语}`` | IconRecognition 的图标目录与五语名称 |
| ``rarity`` / ``weapon_type`` / ``skills{五语}`` / ``names{五语}`` | EssenceFilter 的技能池与任务选项 |

数据来自 ``WeaponBasicTable`` + ``ForesightWeaponTable`` + ``ItemTable`` + ``SkillPatchTable`` +
``WikiEntryDataReverseTable``：前者是已上线的武器，第二张是还没上线的前瞻武器（同一把武器上线后会出现在
``WeaponBasicTable`` 里，所以要先按 id、再按五语名字去重）。输出里不含 ``icon_path``、``foresight``、
``skill_ids`` 与其余语言的字段：消费端没有读它们。

**已上线的武器以图鉴为准**：``WikiEntryDataReverseTable``（物品 id → 图鉴条目）里查不到的整条跳过。
游戏里偶尔会留下玩家拿不到的内部副本——「宏愿」就有 ``wpn_sword_0021`` 与 ``wpn_sword_0021_ink``
两条同名、同图标、同技能的数据，后者没有图鉴条目、不在武器池、也没有任何获取途径，照收只会让基质刷取的
待刷列表多出一个一模一样的选项。

前瞻武器还没上线，自然也没有图鉴条目，走单独一条路：字段要求五语齐全（名字与三个技能名都不为空），
缺一处就整条跳过。``ForesightWeaponTable`` 里出现过名字与技能名文本 id 全为 0 的空行（望乡、落草就是），
那种行收进来只会得到空名字。
"""

from typing import Any

from ..utils.config import UPSTREAM_LOCALES
from ..utils.game_data import text_id_of
from ..utils.inputs import TableCfg
from ..utils.log import logger
from ..utils.tables import (
    ForesightWeaponRow,
    ItemRow,
    SkillPatchRow,
    TextRef,
    WeaponBasicRow,
)
from .base import Dataset


class Weapons(Dataset):
    """EssenceFilter 与 IconRecognition 共用的武器表（``weapons.json``）。"""

    @classmethod
    def build(cls, table_cfg: TableCfg) -> dict[str, dict[str, Any]]:
        """武器：稀有度、类型、图标 id 与五语名字/技能名。

        已上线的武器要求有图鉴条目；前瞻武器不要求（还没上线），但要求名字与三个技能名五语齐全。
        """
        weapons = table_cfg.table("WeaponBasicTable", WeaponBasicRow)
        items = table_cfg.table("ItemTable", ItemRow)
        patches = table_cfg.table("SkillPatchTable", SkillPatchRow)
        foresight = table_cfg.table("ForesightWeaponTable", ForesightWeaponRow)
        wiki_items = table_cfg.table("WikiEntryDataReverseTable", str)

        skill_name_ids = _skill_name_ids(patches)
        output: dict[str, dict[str, Any]] = {}
        released: set[str] = set()
        seen_released: set[str] = set()

        for weapon_id, weapon in weapons.items():
            seen_released.add(weapon_id)
            if weapon_id not in wiki_items:
                # 图鉴里没有 = 玩家见不到：通常是同名内部副本，收进来只会多一个分不清的选项。
                logger.warning(f"武器 {weapon_id} 在图鉴里没有条目，跳过")
                continue
            item = items.get(weapon_id)
            output[weapon_id] = _weapon_entry(
                table_cfg,
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
            if _already_released(table_cfg, weapon_id, row.name, output, released, seen_released):
                logger.warning(f"前瞻武器 {row_key}（{weapon_id}）已经发布过，跳过")
                continue
            skill_ids = list(row.sk_ids)
            missing = _missing_foresight_texts(table_cfg, skill_ids, row.name, list(row.sk_names))
            if missing:
                logger.warning(f"前瞻武器 {row_key}（{weapon_id}）缺{'、'.join(missing)}，跳过")
                continue
            output[weapon_id] = _weapon_entry(
                table_cfg,
                weapon_id=weapon_id,
                # 前瞻武器还没有 ItemTable 行，自家 iconId 就是图标键。
                icon_id=row.icon_id or weapon_id,
                name_ref=row.name,
                weapon_type=row.wpn_type,
                rarity=row.rarity,
                skill_ids=skill_ids,
                skill_name_ids=skill_name_ids,
                row_skill_names=_row_skill_names(table_cfg, skill_ids, row.sk_names),
            )
            logger.info(f"前瞻武器 {weapon_id} 入库")

        logger.info(f"武器 {len(output)} 把")
        return output


def _already_released(
    table_cfg: TableCfg,
    weapon_id: str,
    name_ref: TextRef | None,
    output: dict[str, dict[str, Any]],
    released: set[str],
    seen_released: set[str],
) -> bool:
    """前瞻武器是否已经作为正式武器发布：先认 id，再逐语比对名字。

    ``seen_released`` 是 ``WeaponBasicTable`` 里的全部 id（含因为没图鉴条目被跳过的），
    ``released`` 只放真正写进输出的那些 id，名字比对只可能命中它们。

    id 通常会跟着升级保留，但不保证；重新给过 id 的武器仍然指向同一段游戏文本，所以名字全中
    也算同一把（只中一部分算翻译差异，不算重复）。
    """
    if weapon_id in seen_released:
        return True

    known = {suffix: text for suffix, text in table_cfg.ref_texts(name_ref).items() if text}
    if not known:
        return False
    for other_id in released:
        names = output[other_id]["names"]
        if all(names.get(suffix) == text for suffix, text in known.items()):
            return True
    return False


def _missing_foresight_texts(
    table_cfg: TableCfg,
    skill_ids: list[str],
    name_ref: TextRef | None,
    skill_refs: list[TextRef],
) -> list[str]:
    """前瞻武器缺哪些文本：名字与三个技能名都要求五语齐全，缺一处就整条跳过。

    前瞻武器没有 ``ItemTable`` 行，名字与技能名只能取 ``ForesightWeaponTable`` 自带的那一份；
    表里有文本 id 全为 0 的空行，那种行收进来只会得到空名字。
    """
    if len(skill_ids) != 3 or len(skill_refs) != 3:
        return ["三个技能名"]
    missing: list[str] = []
    if not all(table_cfg.ref_texts(name_ref).values()):
        missing.append("名字")
    for index, ref in enumerate(skill_refs, start=1):
        if not all(table_cfg.ref_texts(ref).values()):
            missing.append(f"技能 {index} 名称")
    return missing


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
    table_cfg: TableCfg,
    skill_ids: list[str],
    skill_refs: list[TextRef],
) -> dict[str, dict[str, str]]:
    """前瞻武器自带的 ``skNames``：它的专属技能还没进 ``SkillPatchTable``。"""
    result: dict[str, dict[str, str]] = {}
    for skill_id, ref in zip(skill_ids, skill_refs, strict=False):
        texts = table_cfg.ref_texts(ref)
        if any(texts.values()):
            result[skill_id] = texts
    return result


def _weapon_entry(
    table_cfg: TableCfg,
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
            names = table_cfg.texts(text_id) if text_id else {}
        for suffix in skills:
            skills[suffix].append(names.get(suffix) or skill_id)
    return {
        "internal_id": weapon_id,
        "icon_id": icon_id or None,
        "rarity": rarity,
        "weapon_type": weapon_type,
        "names": table_cfg.ref_texts(name_ref),
        "skills": skills,
    }
