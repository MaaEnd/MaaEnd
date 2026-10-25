"""基质刷取路线的激发点（``energy_points.json``）。

每个地区一个激发点，按关卡排序：

- ``id``：激发点 ID；
- ``level`` / ``map``：所在关卡与底图；
- ``x`` / ``y`` / ``z``：世界坐标，其中 ``y`` 是世界高度。

某个地区找不到激发点或找到不止一个时报错。
"""

from typing import Any

from ..utils.game_data import level_of, map_of_level, read_xyz
from ..utils.inputs import JsonData, TableCfg
from ..utils.log import logger
from ..utils.tables import WorldEnergyPointRow, WorldEntityRegistry
from .base import Dataset, DatasetError

TRIGGER_DETAIL_ID = "int_system_world_energy_point"


class EnergyPoints(Dataset):
    """基质刷取路线的激发点（``energy_points.json``）。"""

    needs_projection = True
    projection_containers = ("points",)

    @classmethod
    def build(cls, table_cfg: TableCfg, json_data: JsonData) -> dict[str, Any]:
        """有能量淤积点的地区各取一个激发点。"""
        levels_with_points = {
            point.level_id
            for point in table_cfg.table("WorldEnergyPointTable", WorldEnergyPointRow).values()
            if point.level_id
        }
        brief_infos = json_data.config("WorldEntityRegistry", WorldEntityRegistry).world_entity_brief_infos
        levels = json_data.level_index()

        triggers: dict[str, list[tuple[str, Any]]] = {}
        for entity_id, entity in brief_infos.items():
            if entity.detail_id != TRIGGER_DETAIL_ID:
                continue
            level_id = level_of(levels, entity_id)
            if level_id is None:
                raise DatasetError(f"激发点 {entity_id} 定位不到关卡")
            triggers.setdefault(level_id, []).append((entity_id, entity))

        points: list[dict[str, Any]] = []
        for level_id in sorted(levels_with_points):
            found = triggers.get(level_id, [])
            if len(found) != 1:
                ids = ", ".join(entity_id for entity_id, _ in found) or "无"
                raise DatasetError(f"地区 {level_id} 应有且只有一个激发点，实际：{ids}")
            entity_id, entity = found[0]
            x, y, z = read_xyz(entity.position)
            points.append(
                {
                    "id": entity_id,
                    "level": level_id,
                    "map": map_of_level(level_id),
                    "x": x,
                    "y": round(y, 3),
                    "z": z,
                }
            )

        logger.info(f"Built maaend energy_points: {len(points)} points")
        return {"points": points}
