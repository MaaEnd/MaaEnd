"""送货目的地（``delivery_destinations.json``）：普通收货 NPC 与回收站，以及它们所属的仓储节点。

数据来自 TableCfg + GameplayConfig。

只发布游戏世界坐标 x/y/z：仓储节点与回收站的 y 取自 ``LevelMapMark.pos.y``，
普通收货 NPC 的取自 ``NpcProxyTable.position.y``，都原样交给 MaaEnd 当
NAVMESH 终点的 ``target_deck_y``；``yaw`` 是朝向（度）：仓储与回收站取自 LevelData 交互实体的
``rotation.y``（回收站再翻转 180 度），普通收货 NPC 取自 ``NpcProxyTable.rotation.y``，缺省为 0。
底图像素由 MaaEnd 侧的入库步骤（``tools/zmdmap/project.py``）按 BaseNav pack 的 zone 表算出，
这里不做任何换算。
"""

import re
from pathlib import Path
from typing import Any

from ..utils.game_data import (
    Record,
    load_json_dict,
    logger,
    read_gameplay_config,
    read_table_cfg,
)
from ..utils.coords import read_xyz
from .base import Dataset, DatasetError

# 富文本标签（如 <@qu.key>…</>）剥除。
RICH_TEXT_TAG = re.compile(r"<[^<>]*>")

# DomainDepotDeliverTargetTable.entityType 枚举 -> 输出 kind。
ENTITY_TYPE_KINDS = {
    "NpcProxy": "npc",
    "RecycleBin": "recycle_bin",
}

# 仓储节点/资源回收站在 LevelData 关卡文件里的交互 ID 与系统组件名
# （Issue #9 朝向来源）。
_DEPOT_INTERACTIVE_ID = "int_system_domain_depot"
_DEPOT_SYSTEM_COMPONENT = "DomainDepotSystemComponent"

_RECYCLE_INTERACTIVE_ID = "int_doodad_core_recycle"
_RECYCLE_SYSTEM_COMPONENT = "RecycleBinSystemComponent"

def _strip_rich_text(names: dict[str, str], label: str) -> dict[str, str]:
    """剥掉每个语言的富文本标签，剥完后为空则报错。"""
    stripped: dict[str, str] = {}
    for locale, text in names.items():
        value = RICH_TEXT_TAG.sub("", text).strip()
        if not value:
            raise DatasetError(f"{label} 在 {locale} 中为空")
        stripped[locale] = value
    return stripped

def _build_mark_index(
    level_map_mark: dict[str, list[Any]],
) -> dict[str, Record]:
    """LevelMapMark（{level: [mark...]}）-> {markInstId: basicData}。"""
    index: dict[str, Record] = {}
    for level_key in sorted(level_map_mark):
        for offset, mark in enumerate(level_map_mark[level_key]):
            basic = mark.basicData
            mark_inst_id = basic.markInstId
            if not mark_inst_id:
                continue
            if mark_inst_id in index:
                raise DatasetError(f"标记 {mark_inst_id} 在 LevelMapMark 中重复")
            index[mark_inst_id] = basic
    return index

def _build_depots_by_level(
    depot_table: dict[str, Any],
) -> dict[str, dict[str, Any]]:
    """DomainDepotTable -> {refLevelId: {id}}，用于「关卡 -> 仓储节点」。"""
    depot_by_level: dict[str, dict[str, Any]] = {}
    for depot_id, entry in sorted(depot_table.items()):
        level_id = entry.refLevelId
        if not level_id:
            raise DatasetError(f"仓储节点 {depot_id} 缺少 refLevelId")
        if level_id in depot_by_level:
            raise DatasetError(f"层级 {level_id} 对应多个仓储节点")
        depot_by_level[level_id] = {"id": depot_id}
    return depot_by_level

def _extract_system_instance_id(entity: dict[str, Any], system_component: str) -> str | None:
    """从 LevelData 交互实体的 componentProperties 中取 system_inst_key。"""
    component_properties = entity.get("componentProperties") or {}
    prop_list = component_properties.get(system_component)
    if not prop_list:
        return None
    for item in prop_list:
        if not isinstance(item, dict):
            continue
        if item.get("key") == "system_inst_key":
            arr = (item.get("value") or {}).get("valueArray", [])
            if arr and isinstance(arr[0], dict):
                return str(arr[0].get("valueString", ""))
    return None

def _load_poses(
    json_data_dir: Path,
    targets: list[tuple[str, str]],
    interactive_id: str,
    system_component: str,
    label: str,
) -> dict[str, dict[str, Any]]:
    """为每个 ``(level_id, target_id)`` 读取 LevelData 交互实体，计算朝向 yaw。

    返回 ``{target_id: {yaw}}``。yaw 取交互实体 rotation.y；关卡目录或实体缺失时整条跳过，
    由调用方按 yaw=0 处理（不影响其它字段）。
    """
    poses: dict[str, dict[str, Any]] = {}

    for level_id, target_id in targets:
        pose = _find_entity_pose(json_data_dir, level_id, interactive_id, system_component, target_id)
        if pose is None:
            # 遍历完整个关卡目录仍未找到对应实体。
            logger.warning(f"{label} {target_id} 在 {level_id} 的 LevelData 中未找到朝向实体")
        else:
            poses[target_id] = pose
    return poses

def _find_entity_pose(
    json_data_dir: Path,
    level_id: str,
    interactive_id: str,
    system_component: str,
    target_id: str,
) -> dict[str, Any] | None:
    """在指定关卡的 LevelData 中查找交互实体并计算朝向 yaw。

    按 ``entityDataIdKey == interactive_id`` 且 ``system_component.system_inst_key ==
    target_id`` 匹配；找到即返回，关卡目录缺失或未匹配到实体时返回 None。
    """
    level_dir = json_data_dir / "LevelData" / level_id
    if not level_dir.is_dir():
        logger.warning(f"LevelData 目录不存在: {level_dir}")
        return None
    for json_file in sorted(level_dir.glob("*.json")):
        try:
            data = load_json_dict(json_file)
        except Exception as e:
            logger.warning(f"读取关卡文件失败 {json_file}: {e}")
            continue
        for entity in data.get("interactives", []):
            if not isinstance(entity, dict):
                continue
            if entity.get("entityDataIdKey") != interactive_id:
                continue
            if _extract_system_instance_id(entity, system_component) != target_id:
                continue

            rotation = entity.get("rotation") or {}
            yaw = float(rotation.get("y", 0.0) or 0.0)

            return {"yaw": yaw}
    return None

def _check_unique_names(destinations: list[dict[str, Any]]) -> None:
    """每个语言的 name 不允许重复（普通收货任务的 OCR 歧义检查）。"""
    for locale in destinations[0]["name"]:
        values = [item["name"][locale] for item in destinations]
        if len(set(values)) != len(values):
            dupes = sorted({value for value in values if values.count(value) > 1})
            raise DatasetError(f"{locale} 的 name 有重复：{dupes}")

class DeliveryDestinations(Dataset):
    """送货目的地（普通收货 NPC / 回收站）及其所属仓储节点（``delivery_destinations.json``）。"""

    needs_projection = True
    projection_containers = ("depots", "destinations")

    @classmethod
    def build(cls, table_cfg_dir: Path, json_data_dir: Path) -> dict[str, Any]:
        """Data chain: DomainDepotDeliverTargetTable -> delivery points; the
        point's position comes from NpcProxyTable (npc) or LevelMapMark
        (recycle_bin), names/areas from DomainDepotBuyerTable/LevelDescTable;
        DomainDepotTable -> depots + level linkage. 坐标按游戏世界坐标发布，
        底图像素由 MaaEnd 侧投影。
        """
        deliver_target_table = read_table_cfg(table_cfg_dir, "DomainDepotDeliverTargetTable")
        buyer_table = read_table_cfg(table_cfg_dir, "DomainDepotBuyerTable")
        depot_table = read_table_cfg(table_cfg_dir, "DomainDepotTable")
        recycle_bin_table = read_table_cfg(table_cfg_dir, "RecycleBinTable")
        level_desc_table = read_table_cfg(table_cfg_dir, "LevelDescTable")

        npc_positions = read_gameplay_config(json_data_dir, "NpcProxyTable").dataTable
        mark_index = _build_mark_index(read_gameplay_config(json_data_dir, "LevelMapMark"))
        # 关卡 -> 仓储节点，两半都要用（送货目的地要记自己属于哪个节点）。
        depot_by_level = _build_depots_by_level(depot_table)

        # ── 仓储节点 ────────────────────────────────────────────────
        depot_pose = _load_poses(
            json_data_dir,
            [(level_id, info["id"]) for level_id, info in depot_by_level.items()],
            _DEPOT_INTERACTIVE_ID,
            _DEPOT_SYSTEM_COMPONENT,
            "仓储节点",
        )
        depots: list[dict[str, Any]] = []
        for depot_id, entry in sorted(depot_table.items()):
            level_id = entry.refLevelId
            if level_id.count("_") != 1:
                raise DatasetError(
                    f"仓储节点 {depot_id} 的 refLevelId {level_id!r} 不是 mapXX_lvYYY"
                )
            map_id = level_id.split("_")[0]

            basic = mark_index.get(depot_id)
            if basic is None:
                raise DatasetError(f"仓储节点 {depot_id} 在 LevelMapMark 中没有同名标记")
            x, y, z = read_xyz(basic.pos, f"LevelMapMark[{depot_id}].pos")

            pose = depot_pose.get(depot_id, {})
            depots.append(
                {
                    "id": depot_id,
                    "name": Dataset.localized_names(entry.depotName, table_cfg_dir, f"仓储节点 {depot_id}"),
                    "map": map_id,
                    "x": x,
                    "y": round(y, 3),
                    "z": z,
                    "yaw": pose.get("yaw", 0.0),
                }
            )
        if not depots:
            raise DatasetError("DomainDepotTable 里一个仓储节点都没有")

        # ── 送货目的地 ───────────────────────────────────────────────
        recycle_pose = _load_poses(
            json_data_dir,
            [
                (entry.level, entry.targetId)
                for entry in deliver_target_table.values()
                if entry.entityType == "RecycleBin"
            ],
            _RECYCLE_INTERACTIVE_ID,
            _RECYCLE_SYSTEM_COMPONENT,
            "资源回收站",
        )

        destinations: list[dict[str, Any]] = []
        for target_id, entry in sorted(deliver_target_table.items()):
            label = f"送货目的地 {target_id}"

            level_id = entry.level
            if level_id.count("_") != 1:
                raise DatasetError(f"{label} 的 level {level_id!r} 不是 mapXX_lvYYY")
            map_id = level_id.split("_")[0]

            entity_key = entry.targetId
            if not entity_key:
                raise DatasetError(f"{label} 缺少 targetId")

            kind = ENTITY_TYPE_KINDS.get(entry.entityType)
            if kind is None:
                raise DatasetError(f"{label} 的 entityType {entry.entityType!r} 不是已知枚举值")

            if entry.entityType == "NpcProxy":
                npc = npc_positions.get(entity_key)
                if npc is None:
                    raise DatasetError(f"{label} 在 NpcProxyTable 中的 {entity_key} 不存在")
                x, y, z = read_xyz(npc.position, f"NpcProxyTable[{entity_key}].position")
                # 普通收货 NPC 同样保留朝向：读取 NpcProxyTable 的 rotation.y。
                pose: dict[str, Any] = {"yaw": float(npc.rotation.get("y", 0.0) or 0.0)}
                serial_id: int | None = None
            else:
                basic = mark_index.get(entity_key)
                if basic is None:
                    raise DatasetError(f"{label} 在 LevelMapMark 中没有 {entity_key}")
                x, y, z = read_xyz(basic.pos, f"LevelMapMark[{entity_key}].pos")
                pose = recycle_pose.get(entity_key, {})
                recycle_bin = recycle_bin_table.get(entity_key)
                if recycle_bin is None:
                    raise DatasetError(f"{label} 对应的资源回收站 {entity_key} 不存在")
                if recycle_bin.levelId != level_id:
                    raise DatasetError(
                        f"资源回收站 {entity_key} 的层级与{label}的层级 {level_id} 不一致"
                    )
                serial_id = recycle_bin.serialId
                if serial_id <= 0:
                    raise DatasetError(f"资源回收站 {entity_key} 的 serialId {serial_id!r} 无效")

            buyer_id = target_id.replace("deliver_target", "buyer", 1)
            buyer = buyer_table.get(buyer_id)
            if buyer is None:
                raise DatasetError(f"{label} 对应的收货人 {buyer_id} 不存在")
            if buyer.level != level_id:
                raise DatasetError(f"收货人 {buyer_id} 的层级与{label}的层级 {level_id} 不一致")

            level_desc = level_desc_table.get(level_id)
            if level_desc is None:
                raise DatasetError(f"{label} 对应的关卡 {level_id} 不存在")
            area = Dataset.localized_names(level_desc.showName, table_cfg_dir, f"关卡 {level_id}")

            mission_ref = entry.missionObjDesc
            if mission_ref is None:
                raise DatasetError(f"{label} 缺少任务目标文案")
            depot = depot_by_level.get(level_id)

            dest_entry: dict[str, Any] = {
                "id": target_id,
                "kind": kind,
                **({"serial_id": serial_id} if serial_id is not None else {}),
                "name": Dataset.localized_names(buyer.buyerName, table_cfg_dir, f"收货人 {buyer_id}"),
                "mission": _strip_rich_text(
                    Dataset.localized_names(mission_ref, table_cfg_dir, f"{label}任务目标"),
                    f"{label}任务目标",
                ),
                "area": area,
                "depot_id": depot["id"] if depot is not None else "",
                "map": map_id,
                "x": x,
                "y": round(y, 3),
                "z": z,
            }
            # 回收站交互实体的 rotation.y 与实际正面方向相反，需要翻转 180 度；普通 NPC 直接使用 rotation.y，
            # 实体没读到（raw_yaw 为 None）时按 0 处理。
            raw_yaw = pose.get("yaw")
            dest_entry["yaw"] = (
                round((float(raw_yaw) + 180.0) % 360.0, 9)
                if kind == "recycle_bin" and raw_yaw is not None
                else float(raw_yaw or 0.0)
            )
            destinations.append(dest_entry)

        if not destinations:
            raise DatasetError("DomainDepotDeliverTargetTable 里一个送货点都没有")

        _check_unique_names(destinations)

        logger.info(
            f"Built maaend delivery_destinations: {len(depots)} depots, {len(destinations)} destinations"
        )
        return {
            "depots": depots,
            "destinations": destinations,
        }
