"""MaaEnd 精简游戏数据 substep: environment_monitoring.json.

从 TableCfg 生成监测终端、观察点与五语言名称。
"""

from pathlib import Path
from typing import Any

from ..constants import MAAEND_LOCALES
from ..utils.game_data import Record, localized_text, logger, read_table_cfg

def build_environment_monitoring(table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
    """Data chain: KiteStationEntrustTasksTable -> per-terminal missions
    (entrustIdx/kiteStation/missionId/name/shotTargetName),
    KiteStationLevelTable -> terminal display name."""
    entrust_table = read_table_cfg(table_cfg_dir, "KiteStationEntrustTasksTable")
    level_table = read_table_cfg(table_cfg_dir, "KiteStationLevelTable")

    terminals: dict[str, Any] = {}

    for terminal_id in sorted(entrust_table.keys()):
        entrust_entry = entrust_table[terminal_id]
        level_entry = level_table.get(terminal_id)
        if level_entry is None:
            raise ValueError(f"监测终端 {terminal_id} 缺少 KiteStationLevelTable 配置")

        mission_list = entrust_entry.list
        if not mission_list:
            raise ValueError(f"监测终端 {terminal_id} 没有观察点")

        missions: list[dict[str, Any]] = []
        for mission_value in sorted(
            mission_list.values(),
            key=lambda value: value.entrustIdx,
        ):
            if mission_value.kiteStation is not None and mission_value.kiteStation != terminal_id:
                raise ValueError(
                    f"观察点 {mission_value.missionId} 引用了错误的监测终端 "
                    + f"{mission_value.kiteStation}"
                )
            mission_id = mission_value.missionId
            if not mission_id:
                raise ValueError(f"监测终端 {terminal_id} 的观察点缺少 missionId")

            missions.append(
                {
                    "mission_id": mission_id,
                    "entrust_index": mission_value.entrustIdx,
                    "names": _build_localized_names(
                        mission_value.name, table_cfg_dir, f"观察点 {mission_id}"
                    ),
                    "shot_target_names": _build_localized_names(
                        mission_value.shotTargetName,
                        table_cfg_dir,
                        f"观察点 {mission_id} 拍照目标",
                    ),
                }
            )

        if not missions:
            raise ValueError(f"监测终端 {terminal_id} 没有观察点")

        terminals[terminal_id] = {
            "id": terminal_id,
            "names": _build_localized_names(
                level_entry.name, table_cfg_dir, f"监测终端 {terminal_id}"
            ),
            "missions": missions,
        }

    logger.info(f"Built maaend environment_monitoring ({len(terminals)} terminals)")
    return {"terminals": terminals}

def _build_localized_names(
    ref: Record,
    table_cfg_dir: Path,
    label: str,
) -> dict[str, str]:
    """把 ``ref`` 里的文本 id 解析成 MaaEnd 需要的五语言字典。"""
    if ref.text:
        return {locale: ref.text for locale, _ in MAAEND_LOCALES}
    text_id = ref.id
    if not text_id:
        raise ValueError(f"{label} 缺少 i18n id")

    names: dict[str, str] = {}
    for locale, lang_code in MAAEND_LOCALES:
        text = localized_text(table_cfg_dir, text_id, lang_code)
        if not text:
            raise ValueError(f"{label} 在 {locale} 中缺少文本 {text_id}")
        names[locale] = text
    return names
