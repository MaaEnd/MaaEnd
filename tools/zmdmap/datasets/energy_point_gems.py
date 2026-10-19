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

from pathlib import Path
from typing import Any

from ..utils.game_data import Record, as_text, logger, read_table_cfg, ref_text
from .base import Dataset


class EnergyPointGems(Dataset):
    """EssenceFilter 用的能量淤积点（``energy_point_gems.json``）。"""

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        """能量淤积点：名字、体力消耗与两组词条名（中文）。"""
        points = read_table_cfg(table_cfg_dir, "WorldEnergyPointTable")
        groups = read_table_cfg(table_cfg_dir, "WorldEnergyPointGroupTable")
        mechanics = read_table_cfg(table_cfg_dir, "GameMechanicTable")
        gems = read_table_cfg(table_cfg_dir, "GemTable")

        rows: list[dict[str, Any]] = []
        for point_id in sorted(points):
            point = points[point_id]
            group_id = as_text(point.get("gameGroupId"))
            group = groups.get(group_id)
            if group is None:
                raise ValueError(f"能量淤积点 {point_id} 引用了未知分组 {group_id!r}")
            mechanics_id = as_text(point.get("gameMechanicsId")) or point_id
            mechanic = mechanics.get(mechanics_id)
            rows.append(
                {
                    "pointName": ref_text(table_cfg_dir, point.get("gameName")) or mechanics_id,
                    "levelId": as_text(point.get("levelId")),
                    "costStamina": point.get("costStamina")
                    or (mechanic.get("costStamina") if mechanic else None),
                    "secAttrTermNames": _term_names(table_cfg_dir, gems, group.get("secAttrTermIds")),
                    "skillTermNames": _term_names(table_cfg_dir, gems, group.get("skillTermIds")),
                }
            )

        logger.info(f"能量淤积点 {len(rows)} 条")
        return {"gems": rows}


def _term_names(table_cfg_dir: Path, gems: Record, term_ids: Any) -> list[str]:
    """词条 id -> 中文名；查不到就留 id 本身。"""
    names: list[str] = []
    for term_id in term_ids or []:
        gem = gems.get(term_id)
        name = ref_text(table_cfg_dir, gem.get("tagName") if gem else None)
        names.append(name or term_id)
    return names
