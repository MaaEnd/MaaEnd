"""MaaEnd 精简游戏数据 substep: delivery_destinations.json.

从 TableCfg + GameplayConfig 生成送货目的地
（普通收货 NPC / 回收站）及其所属仓储节点。

只发布游戏世界坐标 x/y/z：仓储节点与回收站的 y 取自 ``LevelMapMark.pos.y``，
普通收货 NPC 的取自 ``NpcProxyTable.position.y``，都原样交给 MaaEnd 当
NAVMESH 终点的 ``target_deck_y``；底图像素由 MaaEnd 侧 ``fetch-data.mjs`` 按 BaseNav
pack 的 zone 表投影，这里不做任何换算。
"""

import re
from typing import Any

from local_context import (
    DomainDepotTargetType,
    I18nText,
    LanguageCode,
    MapMarkBasicData,
    RecycleBinData,
    RunContext,
    load_json_dict,
    logger,
)
from coords import COORD_TEXT, COORD_Y_NOTE, read_xyz

# MaaEnd 侧硬编码的 5 种语言（zh_cn/zh_tw/en_us/ja_jp/ko_kr），
# 对应 I18nTextTable_{CN,TC,EN,JP,KR}.json 后缀。
MAAEND_LOCALES: tuple[tuple[str, LanguageCode], ...] = (
    ("zh_cn", LanguageCode.ZH_CN),
    ("zh_tw", LanguageCode.ZH_TW),
    ("en_us", LanguageCode.EN_US),
    ("ja_jp", LanguageCode.JA_JP),
    ("ko_kr", LanguageCode.KO_KR),
)

# 富文本标签（如 <@qu.key>…</>）剥除。
RICH_TEXT_TAG = re.compile(r"<[^<>]*>")

# DomainDepotDeliverTargetTable.entityType 枚举 -> 输出 kind。
ENTITY_TYPE_KINDS = {
    DomainDepotTargetType.NpcProxy.value: "npc",
    DomainDepotTargetType.RecycleBin.value: "recycle_bin",
}

# 仓储节点/资源回收站在 LevelData 关卡文件里的交互 ID 与系统组件名
# （Issue #9 朝向来源）。
_DEPOT_INTERACTIVE_ID = "int_system_domain_depot"
_DEPOT_SYSTEM_COMPONENT = "DomainDepotSystemComponent"

_RECYCLE_INTERACTIVE_ID = "int_doodad_core_recycle"
_RECYCLE_SYSTEM_COMPONENT = "RecycleBinSystemComponent"

class DeliveryDestinationError(ValueError):
    """delivery_destinations 数据组装错误。"""

def _build_localized_names(
    ref: I18nText,
    ctx: RunContext,
    label: str,
) -> dict[str, str]:
    """Resolve an I18nText id into the 5-language dict MaaEnd expects."""
    if ref.text:
        return {locale: ref.text for locale, _ in MAAEND_LOCALES}
    text_id = ref.id
    if not text_id:
        raise DeliveryDestinationError(f"{label} 缺少 i18n id")

    names: dict[str, str] = {}
    for locale, lang_code in MAAEND_LOCALES:
        text = ctx.i18n.get_localized_text(text_id, lang_code)
        if not text:
            raise DeliveryDestinationError(f"{label} 在 {locale} 中缺少文本 {text_id}")
        names[locale] = text
    return names

def strip_rich_text(names: dict[str, str], label: str) -> dict[str, str]:
    """剥掉每个语言的富文本标签，剥完后为空则报错。"""
    stripped: dict[str, str] = {}
    for locale, text in names.items():
        value = RICH_TEXT_TAG.sub("", text).strip()
        if not value:
            raise DeliveryDestinationError(f"{label} 在 {locale} 中为空")
        stripped[locale] = value
    return stripped

def _build_mark_index(
    level_map_mark: dict[str, list[Any]],
) -> dict[str, MapMarkBasicData]:
    """LevelMapMark（{level: [mark...]}）-> {markInstId: basicData}。"""
    index: dict[str, MapMarkBasicData] = {}
    for level_key in sorted(level_map_mark):
        for offset, mark in enumerate(level_map_mark[level_key]):
            basic = mark.basicData
            mark_inst_id = basic.markInstId
            if not mark_inst_id:
                continue
            if mark_inst_id in index:
                raise DeliveryDestinationError(f"标记 {mark_inst_id} 在 LevelMapMark 中重复")
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
            raise DeliveryDestinationError(f"仓储节点 {depot_id} 缺少 refLevelId")
        if level_id in depot_by_level:
            raise DeliveryDestinationError(f"层级 {level_id} 对应多个仓储节点")
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

def _load_depot_pose(
    ctx: RunContext, depot_by_level: dict[str, dict[str, Any]]
) -> dict[str, dict[str, Any]]:
    """为每个仓储节点读取 LevelData 交互实体，计算朝向 yaw。

    返回 ``{depot_id: {yaw}}``。yaw 取交互实体 rotation.y；关卡目录或实体缺失时按
    yaw=0 处理（不影响其它字段）。
    """
    poses: dict[str, dict[str, Any]] = {}

    for level_id, depot_info in depot_by_level.items():
        depot_id = depot_info["id"]
        pose = _find_entity_pose(
            ctx,
            level_id,
            _DEPOT_INTERACTIVE_ID,
            _DEPOT_SYSTEM_COMPONENT,
            depot_id,
        )
        if pose is None:
            # 遍历完整个关卡目录仍未找到对应实体。
            logger.warning(f"仓储节点 {depot_id} 在 {level_id} 的 LevelData 中未找到朝向实体")
        else:
            poses[depot_id] = pose
    return poses

def _load_recycle_pose(
    ctx: RunContext, recycle_targets: list[tuple[str, str]]
) -> dict[str, dict[str, Any]]:
    """为每个资源回收站目的地读取 LevelData 交互实体，计算朝向 yaw。

    入参为 ``[(level_id, target_id), ...]``（每个回收站目的地所在关卡及其 targetId），
    返回 ``{target_id: {yaw}}``。yaw 取交互实体 rotation.y；关卡目录或实体缺失时按
    yaw=0 处理。
    """
    poses: dict[str, dict[str, Any]] = {}

    for level_id, target_id in recycle_targets:
        pose = _find_entity_pose(
            ctx,
            level_id,
            _RECYCLE_INTERACTIVE_ID,
            _RECYCLE_SYSTEM_COMPONENT,
            target_id,
        )
        if pose is None:
            logger.warning(f"资源回收站 {target_id} 在 {level_id} 的 LevelData 中未找到朝向实体")
        else:
            poses[target_id] = pose
    return poses

def _reverse_yaw(yaw: float) -> float:
    """将交互实体朝向翻转为角色正面接近方向。"""
    return round((yaw + 180.0) % 360.0, 9)

def _find_entity_pose(
    ctx: RunContext,
    level_id: str,
    interactive_id: str,
    system_component: str,
    target_id: str,
) -> dict[str, Any] | None:
    """在指定关卡的 LevelData 中查找交互实体并计算朝向 yaw。

    按 ``entityDataIdKey == interactive_id`` 且 ``system_component.system_inst_key ==
    target_id`` 匹配；找到即返回，关卡目录缺失或未匹配到实体时返回 None。
    """
    level_dir = ctx.json_data.base_path / "LevelData" / level_id
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

def _build_depots(
    ctx: RunContext,
    depot_table: dict[str, Any],
    mark_index: dict[str, MapMarkBasicData],
    depot_pose: dict[str, dict[str, Any]],
) -> list[dict[str, Any]]:
    """生成仓储节点列表。"""
    depots: list[dict[str, Any]] = []
    for depot_id, entry in sorted(depot_table.items()):
        level_id = entry.refLevelId
        if level_id.count("_") != 1:
            raise DeliveryDestinationError(
                f"仓储节点 {depot_id} 的 refLevelId {level_id!r} 不是 mapXX_lvYYY"
            )
        map_id = level_id.split("_")[0]

        basic = mark_index.get(depot_id)
        if basic is None:
            raise DeliveryDestinationError(f"仓储节点 {depot_id} 在 LevelMapMark 中没有同名标记")
        x, y, z = read_xyz(basic.pos, f"LevelMapMark[{depot_id}].pos")

        pose = depot_pose.get(depot_id, {})
        depots.append(
            {
                "id": depot_id,
                "name": _build_localized_names(entry.depotName, ctx, f"仓储节点 {depot_id}"),
                "map": map_id,
                "x": x,
                "y": round(y, 3),
                "z": z,
                "yaw": pose.get("yaw", 0.0),
            }
        )
    if not depots:
        raise DeliveryDestinationError("DomainDepotTable 里一个仓储节点都没有")
    return depots

def _build_delivery_destinations(
    ctx: RunContext,
    deliver_target_table: dict[str, Any],
    buyer_table: dict[str, Any],
    recycle_bin_table: dict[str, RecycleBinData],
    level_desc_table: dict[str, Any],
    npc_positions: dict[str, Any],
    mark_index: dict[str, MapMarkBasicData],
    depot_by_level: dict[str, dict[str, Any]],
) -> list[dict[str, Any]]:
    """组装送货目的地列表。"""
    # 资源回收站目的地需要像仓储节点一样输出朝向；先收集其
    # (level, targetId)，统一扫描 LevelData 计算 pose，再在循环里按需输出。
    recycle_targets = [
        (entry.level, entry.targetId)
        for entry in deliver_target_table.values()
        if entry.entityType == DomainDepotTargetType.RecycleBin
    ]
    recycle_pose = _load_recycle_pose(ctx, recycle_targets)

    destinations: list[dict[str, Any]] = []
    for target_id, entry in sorted(deliver_target_table.items()):
        label = f"送货目的地 {target_id}"

        level_id = entry.level
        if level_id.count("_") != 1:
            raise DeliveryDestinationError(f"{label} 的 level {level_id!r} 不是 mapXX_lvYYY")
        map_id = level_id.split("_")[0]

        entity_key = entry.targetId
        if not entity_key:
            raise DeliveryDestinationError(f"{label} 缺少 targetId")

        kind = ENTITY_TYPE_KINDS.get(entry.entityType.value)
        if kind is None:
            raise DeliveryDestinationError(f"{label} 的 entityType {entry.entityType!r} 不是已知枚举值")

        if entry.entityType == DomainDepotTargetType.NpcProxy:
            npc = npc_positions.get(entity_key)
            if npc is None:
                raise DeliveryDestinationError(f"{label} 在 NpcProxyTable 中的 {entity_key} 不存在")
            x, y, z = read_xyz(npc.position, f"NpcProxyTable[{entity_key}].position")
            # 普通收货 NPC 同样保留朝向：读取 NpcProxyTable 的 rotation.y。
            pose: dict[str, Any] = {"yaw": float(npc.rotation.get("y", 0.0) or 0.0)}
            serial_id: int | None = None
        else:
            basic = mark_index.get(entity_key)
            if basic is None:
                raise DeliveryDestinationError(f"{label} 在 LevelMapMark 中没有 {entity_key}")
            x, y, z = read_xyz(basic.pos, f"LevelMapMark[{entity_key}].pos")
            pose = recycle_pose.get(entity_key, {})
            recycle_bin = recycle_bin_table.get(entity_key)
            if recycle_bin is None:
                raise DeliveryDestinationError(f"{label} 对应的资源回收站 {entity_key} 不存在")
            if recycle_bin.levelId != level_id:
                raise DeliveryDestinationError(
                    f"资源回收站 {entity_key} 的层级与{label}的层级 {level_id} 不一致"
                )
            serial_id = recycle_bin.serialId
            if serial_id <= 0:
                raise DeliveryDestinationError(f"资源回收站 {entity_key} 的 serialId {serial_id!r} 无效")

        buyer_id = target_id.replace("deliver_target", "buyer", 1)
        buyer = buyer_table.get(buyer_id)
        if buyer is None:
            raise DeliveryDestinationError(f"{label} 对应的收货人 {buyer_id} 不存在")
        if buyer.level != level_id:
            raise DeliveryDestinationError(f"收货人 {buyer_id} 的层级与{label}的层级 {level_id} 不一致")

        level_desc = level_desc_table.get(level_id)
        if level_desc is None:
            raise DeliveryDestinationError(f"{label} 对应的关卡 {level_id} 不存在")
        area = _build_localized_names(level_desc.showName, ctx, f"关卡 {level_id}")

        mission_ref = entry.missionObjDesc
        if mission_ref is None:
            raise DeliveryDestinationError(f"{label} 缺少任务目标文案")
        depot = depot_by_level.get(level_id)

        dest_entry: dict[str, Any] = {
            "id": target_id,
            "kind": kind,
            **({"serial_id": serial_id} if serial_id is not None else {}),
            "name": _build_localized_names(buyer.buyerName, ctx, f"收货人 {buyer_id}"),
            "mission": strip_rich_text(
                _build_localized_names(mission_ref, ctx, f"{label}任务目标"),
                f"{label}任务目标",
            ),
            "area": area,
            "depot_id": depot["id"] if depot is not None else "",
            "map": map_id,
            "x": x,
            "y": round(y, 3),
            "z": z,
        }
        # 回收站交互实体的 rotation.y 与实际正面方向相反，需要翻转 180 度；普通 NPC 直接使用 rotation.y。
        raw_yaw = pose.get("yaw")
        dest_entry["yaw"] = (
            _reverse_yaw(float(raw_yaw))
            if kind == "recycle_bin" and raw_yaw is not None
            else float(raw_yaw or 0.0)
        )
        destinations.append(dest_entry)

    if not destinations:
        raise DeliveryDestinationError("DomainDepotDeliverTargetTable 里一个送货点都没有")
    return destinations

def _check_unique_names(destinations: list[dict[str, Any]]) -> None:
    """每个语言的 name 不允许重复（普通收货任务的 OCR 歧义检查）。"""
    for locale in destinations[0]["name"]:
        values = [item["name"][locale] for item in destinations]
        if len(set(values)) != len(values):
            dupes = sorted({value for value in values if values.count(value) > 1})
            raise DeliveryDestinationError(f"{locale} 的 name 有重复：{dupes}")

def build_delivery_destinations(ctx: RunContext) -> dict[str, Any]:
    """Data chain: DomainDepotDeliverTargetTable -> delivery points; the
    point's position comes from NpcProxyTable (npc) or LevelMapMark
    (recycle_bin), names/areas from DomainDepotBuyerTable/LevelDescTable;
    DomainDepotTable -> depots + level linkage. 坐标按游戏世界坐标发布，
    底图像素由 MaaEnd 侧投影。
    """
    tcfg = ctx.table_cfg
    deliver_target_table = tcfg.DomainDepotDeliverTargetTable
    buyer_table = tcfg.DomainDepotBuyerTable
    depot_table = tcfg.DomainDepotTable
    recycle_bin_table = tcfg.RecycleBinTable
    level_desc_table = tcfg.LevelDescTable

    npc_positions = ctx.json_data.NpcProxyTable.dataTable
    mark_index = _build_mark_index(ctx.json_data.LevelMapMark)

    depot_by_level = _build_depots_by_level(depot_table)
    depot_pose = _load_depot_pose(ctx, depot_by_level)
    depots = _build_depots(ctx, depot_table, mark_index, depot_pose)
    destinations = _build_delivery_destinations(
        ctx,
        deliver_target_table,
        buyer_table,
        recycle_bin_table,
        level_desc_table,
        npc_positions,
        mark_index,
        depot_by_level,
    )
    _check_unique_names(destinations)

    logger.info(
        f"Built maaend delivery_destinations: {len(depots)} depots, {len(destinations)} destinations"
    )
    return {
        "text": {
            "name": (
                "取自 DomainDepotBuyerTable.buyerName；普通收货任务用作 OCR 目标，"
                "回收站任务仅保留为数据标识"
            ),
            "mission": (
                "委托目标文案，已剥掉 <@qu.key>…</> 富文本标签；回收站任务用作 OCR 目标，"
                "同区域重复时保持歧义失败"
            ),
            "area": "任务详情页显示的关卡名，取自 LevelDescTable.showName",
            "serial_id": "资源回收站在当前区域内显示的序号，取自 RecycleBinTable.serialId",
            "depot_id": "该终点所属仓储节点 ID；没有同层仓储节点时为空字符串",
            "depots": (
                "仓储节点坐标，按 DomainDepotTable.id 精确关联 LevelMapMark.markInstId；"
                "y 为世界高度，取自 LevelMapMark.pos.y，用作 NAVMESH 终点的 target_deck_y；"
                "yaw 为朝向（绕 Y 轴欧拉角，度），取自 LevelData 交互实体 rotation.y，缺省为 0"
            ),
            "destinations": (
                "普通收货 NPC 的 y 取自 NpcProxyTable.position.y，资源回收站的 y 取自 LevelMapMark.pos.y，"
                "均用作 NAVMESH 终点的 target_deck_y；"
                "普通收货 NPC 的 yaw 取自 NpcProxyTable.rotation.y；"
                "资源回收站的 yaw 取自 LevelData 交互实体 rotation.y 并翻转 180 度，缺省为 0"
            ),
        },
        "coord": f"{COORD_TEXT}；{COORD_Y_NOTE}",
        "ocr_key": "name",
        "depot_count": len(depots),
        "count": len(destinations),
        "depots": depots,
        "destinations": destinations,
    }
