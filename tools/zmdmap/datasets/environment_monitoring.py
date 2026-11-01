"""环境监测（``environment_monitoring.json``）：监测终端、观察点与五语言名称，都取自 TableCfg。"""

from typing import Any

from ..utils.inputs import TableCfg
from ..utils.log import logger
from ..utils.tables import KiteStationEntrustRow, KiteStationLevelRow
from .base import Dataset, DatasetError


class EnvironmentMonitoring(Dataset):
    """监测终端、观察点与五语言名称（``environment_monitoring.json``）。"""

    @classmethod
    def build(cls, table_cfg: TableCfg) -> dict[str, Any]:
        """监测终端 -> 它下面的观察点，以及观察点与终端的五语名字。

        同一个终端里的观察点按 ``entrustIdx`` 排序；观察点写了 ``kiteStation`` 就必须跟所在终端一致，
        写错直接报错。
        """
        entrust_table = table_cfg.table("KiteStationEntrustTasksTable", KiteStationEntrustRow)
        level_table = table_cfg.table("KiteStationLevelTable", KiteStationLevelRow)

        terminals: dict[str, Any] = {}

        for terminal_id in sorted(entrust_table.keys()):
            entrust_entry = entrust_table[terminal_id]
            level_entry = level_table.get(terminal_id)
            if level_entry is None:
                raise DatasetError(f"监测终端 {terminal_id} 缺少 KiteStationLevelTable 配置")

            mission_list = entrust_entry.missions
            if not mission_list:
                raise DatasetError(f"监测终端 {terminal_id} 没有观察点")

            missions: list[dict[str, Any]] = []
            for mission_value in sorted(
                mission_list.values(),
                key=lambda value: value.entrust_idx,
            ):
                if mission_value.kite_station is not None and mission_value.kite_station != terminal_id:
                    raise DatasetError(
                        f"观察点 {mission_value.mission_id} 引用了错误的监测终端 " + f"{mission_value.kite_station}"
                    )
                mission_id = mission_value.mission_id
                if not mission_id:
                    raise DatasetError(f"监测终端 {terminal_id} 的观察点缺少 missionId")

                missions.append(
                    {
                        "mission_id": mission_id,
                        "entrust_index": mission_value.entrust_idx,
                        "names": cls.localized_names(mission_value.name, table_cfg, f"观察点 {mission_id}"),
                        "shot_target_names": cls.localized_names(
                            mission_value.shot_target_name,
                            table_cfg,
                            f"观察点 {mission_id} 拍照目标",
                        ),
                    }
                )

            if not missions:
                raise DatasetError(f"监测终端 {terminal_id} 没有观察点")

            terminals[terminal_id] = {
                "id": terminal_id,
                "names": cls.localized_names(level_entry.name, table_cfg, f"监测终端 {terminal_id}"),
                "missions": missions,
            }

        logger.info(f"Built maaend environment_monitoring ({len(terminals)} terminals)")
        return {"terminals": terminals}
