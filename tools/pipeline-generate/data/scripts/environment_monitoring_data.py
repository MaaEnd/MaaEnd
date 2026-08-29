from __future__ import annotations

from typing import Any

from tablecfg_utils import (
    DATA_DIR,
    LOCALE_TABLES,
    assert_record,
    build_locale_tables,
    build_localized_names,
    run_cli,
    sorted_entries,
)

TABLE_NAMES = (
    "KiteStationEntrustTasksTable.json",
    "KiteStationLevelTable.json",
    *LOCALE_TABLES,
)
OUTPUT_PATH = DATA_DIR / "environment_monitoring.json"


def build_environment_monitoring_data(tables: dict[str, Any]) -> dict[str, Any]:
    entrust_table = assert_record(
        tables["KiteStationEntrustTasksTable.json"], "KiteStationEntrustTasksTable"
    )
    level_table = assert_record(
        tables["KiteStationLevelTable.json"], "KiteStationLevelTable"
    )
    locale_tables = build_locale_tables(tables)

    terminals = {}
    for terminal_id, entrust_value in sorted_entries(entrust_table):
        entrust_entry = assert_record(entrust_value, f"监测终端 {terminal_id} 委托")
        level_entry = assert_record(
            level_table.get(terminal_id), f"监测终端 {terminal_id}"
        )
        mission_list = assert_record(
            entrust_entry.get("list"), f"监测终端 {terminal_id} list"
        )
        missions = []
        for mission_value in sorted(
            mission_list.values(),
            key=lambda value: assert_record(
                value, f"监测终端 {terminal_id} 观察点"
            ).get("entrustIdx"),
        ):
            mission = assert_record(mission_value, f"监测终端 {terminal_id} 观察点")
            if mission.get("kiteStation") != terminal_id:
                raise ValueError(
                    f"观察点 {mission.get('missionId')} 引用了错误的监测终端 {mission.get('kiteStation')}"
                )
            mission_id = mission.get("missionId")
            if not mission_id:
                raise ValueError(f"监测终端 {terminal_id} 的观察点缺少 missionId")
            missions.append(
                {
                    "mission_id": mission_id,
                    "entrust_index": mission.get("entrustIdx"),
                    "names": build_localized_names(
                        mission.get("name"), locale_tables, f"观察点 {mission_id}"
                    ),
                    "shot_target_names": build_localized_names(
                        mission.get("shotTargetName"),
                        locale_tables,
                        f"观察点 {mission_id} 拍照目标",
                    ),
                }
            )
        if not missions:
            raise ValueError(f"监测终端 {terminal_id} 没有观察点")
        terminals[terminal_id] = {
            "id": terminal_id,
            "names": build_localized_names(
                level_entry.get("name"), locale_tables, f"监测终端 {terminal_id}"
            ),
            "missions": missions,
        }

    return {"terminals": terminals}


def main() -> int:
    return run_cli(
        label="EnvironmentMonitoring",
        description="从 BeyondTableCfg 仓库生成环境监测数据",
        table_names=TABLE_NAMES,
        output_path=OUTPUT_PATH,
        build_data=build_environment_monitoring_data,
    )


if __name__ == "__main__":
    raise SystemExit(main())
