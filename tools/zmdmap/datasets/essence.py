"""EssenceFilter 用的能量淤积点。

``tools/essence_filter/`` 的生成步骤从站点下载的 ``energy_point_gems.json`` 字段很多，但它们
真正读到的只有这里的几项（中文名字、体力消耗、两栏词条名），所以只从本地 TableCfg 取这些：

- ``pointName``：``WorldEnergyPointTable`` 的 ``gameName``；
- ``costStamina``：点自己的，没有就用 ``GameMechanicTable`` 的；
- ``secAttrTermNames`` / ``skillTermNames``：``WorldEnergyPointGroupTable`` 的词条 id 经
  ``GemTable.tagName`` 换成中文名，查不到就留 id 本身。

产物默认落 ``tools/zmdmap/data/energy_point_gems.json``（不需要投影），文件名与字段名沿用消费端
现在读的那套，所以那几个生成步骤一行都不用改。
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from ..utils.game_data import Record, as_text, logger, read_table_cfg, ref_text


def build_energy_points(table_cfg_dir: Path, json_data_dir: Path) -> list[dict[str, Any]]:
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
                "costStamina": point.get("costStamina")
                or (mechanic.get("costStamina") if mechanic else None),
                "secAttrTermNames": _term_names(table_cfg_dir, gems, group.get("secAttrTermIds")),
                "skillTermNames": _term_names(table_cfg_dir, gems, group.get("skillTermIds")),
            }
        )

    logger.info(f"能量淤积点 {len(rows)} 条")
    return rows


def _term_names(table_cfg_dir: Path, gems: Record, term_ids: Any) -> list[str]:
    """词条 id -> 中文名；查不到就留 id 本身。"""
    names: list[str] = []
    for term_id in term_ids or []:
        gem = gems.get(term_id)
        name = ref_text(table_cfg_dir, gem.get("tagName") if gem else None)
        names.append(name or term_id)
    return names
