"""干员培养（``operator_growth.json``）：等级经验、突破节点、技能升级与天赋节点，全部取自 TableCfg。

一份数据回答「把某个干员从当前状态练到目标状态要多少材料」：

- ``level_up``：等级 → 升到下一级所需的经验与折金票（``exp`` / ``gold``，等级 90 是满级、不写）。
- ``operators``：每个干员的五语名字、稀有度、职业、属性、武器类型，以及四组培养节点：

  - ``break_nodes``：突破（``charBreak20`` / ``40`` / ``60`` / ``70``），带 ``break_stage`` 与
    ``equip_tier_limit``（该阶段解锁的装备档位）；
  - ``equip_break_nodes``：装备档位突破，只花折金票；
  - ``skills``：四组技能（普攻 / 战技 / 连携 / 大招）各自的逐级消耗，``level`` 是升到的目标等级，
    ``gold`` 是折金票，``items`` 是素材（10 级起是专精，``item_char_skill_specialize_*``）；
  - ``talent_nodes``：除突破与装备以外的天赋节点（潜能、被动、基建技能），``node_type`` 保留原值：
    3 潜能 / 4 被动 / 5 基建。

- ``items``：上面出现过的素材，带五语名字、稀有度与图标 id；折金票 ``item_gold`` 也在里面。

节点里的素材统一写成「素材 id → 数量」，折金票按素材 id ``item_gold`` 原样留在 ``items`` 里，
消费端要单独统计货币时不必再认别的字段。
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from ..utils.game_data import as_text, logger, read_table_cfg
from .base import Dataset, DatasetError

# 技能组的排列顺序（游戏里就是这么排的）；表里没有的组名排在后面按名字排。
SKILL_ORDER = ("NormalAttack", "NormalSkill", "ComboSkill", "UltimateSkill")

# ``charBreakCostMap`` 里的节点类型：1 突破，2 装备档位突破。
NODE_TYPE_BREAK = 1
NODE_TYPE_EQUIP_BREAK = 2

#: 满级（最后一个等级）在表里是 ``-1`` 的哨兵行，不参与累加。
MAX_LEVEL_SENTINEL = -1


class OperatorGrowth(Dataset):
    """干员培养素材需求（``operator_growth.json``）。"""

    #: 只读 TableCfg，不要求 JsonData 目录。
    requires_json_data = False

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        """等级曲线 + 各干员的突破/技能/天赋消耗 + 素材表。"""
        characters = read_table_cfg(table_cfg_dir, "CharacterTable")
        growth = read_table_cfg(table_cfg_dir, "CharGrowthTable")
        level_up_table = read_table_cfg(table_cfg_dir, "CharLevelUpTable")
        item_table = read_table_cfg(table_cfg_dir, "ItemTable")

        level_up = cls._level_up(level_up_table)
        used_items: set[str] = set()
        operators: dict[str, Any] = {}

        for char_id in sorted(growth):
            entry = growth[char_id]
            character = characters.get(char_id)
            if character is None:
                raise DatasetError(f"CharacterTable 缺少干员 {char_id}")

            operators[char_id] = {
                "names": cls.localized_names(character.name, table_cfg_dir, f"干员 {char_id}"),
                "rarity": character.rarity,
                "profession": character.profession,
                "char_type": as_text(character.charTypeId),
                "weapon_type": character.weaponType,
                "break_nodes": cls._break_nodes(entry, used_items),
                "equip_break_nodes": cls._equip_break_nodes(entry, used_items),
                "skills": cls._skills(entry, used_items),
                "talent_nodes": cls._talent_nodes(entry, used_items),
            }

        items = cls._items(table_cfg_dir, item_table, used_items)
        logger.info(f"Built operator growth: {len(operators)} operators, {len(items)} items")
        return {"level_up": level_up, "operators": operators, "items": items}

    @staticmethod
    def _level_up(level_up_table: Any) -> dict[str, dict[str, int]]:
        """``level -> {exp, gold}``：升到下一级的消耗，哨兵行丢掉。"""
        level_up: dict[str, dict[str, int]] = {}
        for level in sorted(level_up_table, key=int):
            row = level_up_table[level]
            exp = row.get("exp", 0)
            gold = row.get("gold", 0)
            if exp == MAX_LEVEL_SENTINEL or gold < 0:
                continue
            level_up[level] = {"exp": exp, "gold": gold}
        if not level_up:
            raise DatasetError("CharLevelUpTable 里没有可用的等级")
        return level_up

    @staticmethod
    def _items_of(node: Any, used: set[str]) -> dict[str, int]:
        """节点 ``requiredItem`` / ``itemBundle`` -> 「素材 id -> 数量」。"""
        items: dict[str, int] = {}
        for required in node.get("requiredItem") or node.get("itemBundle") or []:
            item_id = as_text(required.get("id"))
            count = required.get("count", 0)
            if not item_id or not isinstance(count, int) or count <= 0:
                continue
            items[item_id] = items.get(item_id, 0) + count
            used.add(item_id)
        return items

    @classmethod
    def _break_nodes(cls, entry: Any, used: set[str]) -> list[dict[str, Any]]:
        """突破节点，按 ``break_stage`` 排序。"""
        nodes = [
            {
                "node_id": as_text(node.get("nodeId")),
                "break_stage": node.get("breakStage", 0),
                "equip_tier_limit": node.get("equipTierLimit", 0),
                "items": cls._items_of(node, used),
            }
            for node in entry.get("charBreakCostMap", {}).values()
            if node.get("nodeType") == NODE_TYPE_BREAK
        ]
        return sorted(nodes, key=lambda node: node["break_stage"])

    @classmethod
    def _equip_break_nodes(cls, entry: Any, used: set[str]) -> list[dict[str, Any]]:
        """装备档位突破，按 ``break_stage`` 排序。"""
        nodes = [
            {
                "node_id": as_text(node.get("nodeId")),
                "break_stage": node.get("breakStage", 0),
                "equip_tier_limit": node.get("equipTierLimit", 0),
                "items": cls._items_of(node, used),
            }
            for node in entry.get("charBreakCostMap", {}).values()
            if node.get("nodeType") == NODE_TYPE_EQUIP_BREAK
        ]
        return sorted(nodes, key=lambda node: node["break_stage"])

    @classmethod
    def _skills(cls, entry: Any, used: set[str]) -> list[dict[str, Any]]:
        """四组技能的逐级消耗，按游戏里的顺序排。"""
        groups: dict[str, list[dict[str, Any]]] = {}
        for row in entry.get("skillLevelUp") or []:
            group_id = as_text(row.get("skillGroupId"))
            if not group_id:
                continue
            items = cls._items_of(row, used)
            groups.setdefault(group_id, []).append(
                {
                    "level": row.get("level", 0),
                    "gold": row.get("goldCost", 0),
                    "items": items,
                }
            )
        for levels in groups.values():
            levels.sort(key=lambda level: level["level"])

        def order(group_id: str) -> tuple[int, str]:
            name = group_id.rsplit("_", 1)[-1]
            return (SKILL_ORDER.index(name) if name in SKILL_ORDER else len(SKILL_ORDER), group_id)

        return [{"group_id": group_id, "levels": groups[group_id]} for group_id in sorted(groups, key=order)]

    @classmethod
    def _talent_nodes(cls, entry: Any, used: set[str]) -> list[dict[str, Any]]:
        """天赋节点（潜能 / 被动 / 基建），突破与装备节点不重复收。"""
        nodes = [
            {
                "node_id": as_text(node.get("nodeId")),
                "node_type": node.get("nodeType", 0),
                "items": cls._items_of(node, used),
            }
            for node in entry.get("talentNodeMap", {}).values()
            if node.get("nodeType") not in (NODE_TYPE_BREAK, NODE_TYPE_EQUIP_BREAK)
        ]
        return sorted(nodes, key=lambda node: node["node_id"])

    @classmethod
    def _items(cls, table_cfg_dir: Path, item_table: Any, used: set[str]) -> dict[str, dict[str, Any]]:
        """素材表：五语名字、稀有度、图标。"""
        items: dict[str, dict[str, Any]] = {}
        for item_id in sorted(used):
            entry = item_table.get(item_id)
            if entry is None:
                raise DatasetError(f"ItemTable 里没有素材 {item_id}")
            items[item_id] = {
                "names": cls.localized_names(entry.name, table_cfg_dir, f"素材 {item_id}"),
                "rarity": entry.rarity,
                "icon": as_text(entry.iconId),
            }
        return items
