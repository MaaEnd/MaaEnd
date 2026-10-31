"""EssenceFilter 用的能量淤积点。

``tools/essence_filter/`` 的生成步骤真正读到的只有这里的几项（中文名字、地区标识、体力消耗、
两栏词条名），所以只从本地 TableCfg 取这些：

- ``pointName``：``WorldEnergyPointTable`` 的 ``gameName``；
- ``levelId``：``WorldEnergyPointTable`` 的 ``levelId``（地区标识，一个地区一个，且随游戏数据稳定）；
- ``costStamina``：点自己的，没有就用 ``GameMechanicTable`` 的；
- ``secAttrTermNames`` / ``skillTermNames``：``WorldEnergyPointGroupTable`` 的词条 id 经
  ``GemTable.tagName`` 换成中文名，查不到就留 id 本身。

产物默认落 ``tools/zmdmap/data/energy_point_gems.json``（不需要投影），字段名沿用消费端读的那套。
顶层是 ``{"version": …, "sha256": …, "gems": [...]}``：元数据字段和数据放在同一个文件里，所以数据本体
挂在 ``gems`` 下；消费端用 :func:`tools.zmdmap.read_payload` 读，不必自己跳过元数据。
"""

from typing import Any

from ..utils.inputs import TableCfg
from ..utils.log import logger
from ..utils.tables import (
    GameMechanicRow,
    GemRow,
    WorldEnergyPointGroupRow,
    WorldEnergyPointRow,
)
from .base import Dataset, DatasetError


class EnergyPointGems(Dataset):
    """EssenceFilter 用的能量淤积点（``energy_point_gems.json``）。"""

    @classmethod
    def build(cls, table_cfg: TableCfg) -> dict[str, Any]:
        """能量淤积点：名字、体力消耗与两组词条名（中文）。"""
        points = table_cfg.table("WorldEnergyPointTable", WorldEnergyPointRow)
        groups = table_cfg.table("WorldEnergyPointGroupTable", WorldEnergyPointGroupRow)
        mechanics = table_cfg.table("GameMechanicTable", GameMechanicRow)
        gems = table_cfg.table("GemTable", GemRow)

        rows: list[dict[str, Any]] = []
        for point_id in sorted(points):
            point = points[point_id]
            group = groups.get(point.game_group_id)
            if group is None:
                raise DatasetError(f"能量淤积点 {point_id} 引用了未知分组 {point.game_group_id!r}")
            mechanics_id = point.game_mechanics_id or point_id
            mechanic = mechanics.get(mechanics_id)
            rows.append(
                {
                    "pointName": table_cfg.ref_text(point.game_name) or mechanics_id,
                    "levelId": point.level_id,
                    "costStamina": point.cost_stamina or (mechanic.cost_stamina if mechanic else None),
                    "secAttrTermNames": _term_names(table_cfg, gems, group.sec_attr_term_ids),
                    "skillTermNames": _term_names(table_cfg, gems, group.skill_term_ids),
                }
            )

        logger.info(f"能量淤积点 {len(rows)} 条")
        return {"gems": rows}


def _term_names(table_cfg: TableCfg, gems: dict[str, GemRow], term_ids: list[str]) -> list[str]:
    """词条 id -> 中文名；查不到就留 id 本身。"""
    names: list[str] = []
    for term_id in term_ids:
        gem = gems.get(term_id)
        name = table_cfg.ref_text(gem.tag_name if gem else None)
        names.append(name or term_id)
    return names
