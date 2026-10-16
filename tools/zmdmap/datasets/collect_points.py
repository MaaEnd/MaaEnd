"""AutoCollect 采集路线的数据源（``collect_points.json``）。

``WorldEntityRegistry`` 中 ``detailId`` 以 ``int_doodad_`` 开头的可交互采集物，全量输出。

只发布游戏世界坐标 x/z：投影参数属于 BaseNav pack，换算由 MaaEnd 侧的投影步骤
（``tools/zmdmap/project.py``）完成，所以这里不做任何底图筛选。
"""

from pathlib import Path
from typing import Any

from ..utils.game_data import logger, read_gameplay_config
from ..utils.coords import read_xz
from ..utils.levels import level_of, map_of_level
from .base import Dataset, DatasetError

DOODAD_PREFIX = "int_doodad_"

class CollectPoints(Dataset):
    """AutoCollect 采集路线的采集物（``collect_points.json``）。"""

    needs_projection = True
    projection_containers = ("points",)

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        """Data chain: WorldEntityRegistry -> 采集物，LevelBasicInfoTable 提供 ID 前缀
        到关卡的对照。

        全部可交互采集物都收，按实体 ID 数值升序。
        """
        brief_infos = read_gameplay_config(json_data_dir, "WorldEntityRegistry").worldEntityBriefInfos
        if not brief_infos:
            raise DatasetError("WorldEntityRegistry 缺少 worldEntityBriefInfos")
        levels = cls.level_index(json_data_dir)

        points: list[dict[str, Any]] = []
        for entity_id in sorted(brief_infos, key=lambda key: (len(key), key)):
            entity = brief_infos[entity_id]
            if not entity.detailId.startswith(DOODAD_PREFIX):
                continue
            level_id = level_of(levels, entity_id)
            if level_id is None:
                continue

            x, z = read_xz(entity.position, f"世界实体 {entity_id}.position")
            points.append(
                {
                    "id": entity_id,
                    "detail_id": entity.detailId,
                    "map": map_of_level(level_id),
                    "x": x,
                    "z": z,
                }
            )
        if not points:
            raise DatasetError("WorldEntityRegistry 里一个可交互采集物都没有")

        logger.info(f"Built maaend collect_points: {len(points)} points")
        return {"points": points}
