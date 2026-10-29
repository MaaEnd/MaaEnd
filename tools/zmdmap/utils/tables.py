"""游戏表格的结构定义与类型化解码。

TableCfg / JsonData 里的一行就是一个 JSON 对象，字段名是 camelCase。这里用 :mod:`msgspec` 的
``Struct`` 把「我们真正读的那几个字段」声明出来：解码时字段名对得上就直接落到带类型的属性上，不必
再靠 ``dict.get`` 加一堆运行时判断，写错字段名这种问题在静态检查阶段就能发现。

两条约定：

- **只声明用得到的字段**。msgspec 默认忽略未知字段，所以整张表有几十个字段也只解出这几项；
  要用的字段不在 ``Struct`` 里就去补，不要退回去用 ``dict``。
- **缺字段与 ``null`` 都要能读**。游戏表里 ``{"id": …, "text": null}`` 很常见，所以每个字段都有默认值，
  类型也照数据里真实出现的形态写（比如 ``str | int`` 的枚举）。
"""

from pathlib import Path
from typing import Any

import msgspec

from .errors import LocalDataError


class TextRef(msgspec.Struct, frozen=True):
    """表里的文本引用：``{"id": 123, "text": null}``；``text`` 是上游偶尔内联的中文。"""

    id: int = 0
    """文本 id；去 I18nTextTable_<语言>.json 里查文字用的就是它"""
    text: str | None = None
    """上游偶尔内联的中文原文，多数时候是 null"""


class ItemStack(msgspec.Struct, frozen=True, rename="camel"):
    """「物品 id + 数量」：奖励、材料消耗都是这个形状。"""

    id: str = ""
    """物品 id，也就是 ItemTable 的行 key"""
    count: int = 0
    """数量"""


class Position3(msgspec.Struct, frozen=True, rename="camel"):
    """世界坐标 x/y/z（y 是世界高度）。"""

    x: float = 0.0
    """东西向坐标"""
    y: float = 0.0
    """世界高度（不是底图上的 y）"""
    z: float = 0.0
    """南北向坐标"""


# ── 物品 ────────────────────────────────────────────────────────────────────


class ItemRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ItemTable`` 的一行。"""

    name: TextRef = TextRef()
    """物品名"""
    icon_id: str = ""
    """图标键，对应 assets/resource/image/UI/Item/<iconId>.png"""
    rarity: int = 0
    """稀有度，1–6 星"""
    type: str = ""
    """物品类型，如 Material / Equip / FormulaUnlock / WeaponGem"""
    showing_type: str = ""
    """背包页签，对应 ItemShowingTypeTable 的 type；None 表示不进背包"""
    valuable_tab_type: str = ""
    """仓库页签，对应 ValuableDepot 的 type"""
    sort_id1: int = 0
    """页签内的排序"""
    sort_id2: int = 0
    """次级排序，sortId1 相同时再比它"""


class InnItemRow(msgspec.Struct, frozen=True, rename="camel"):
    """``InnDishTable`` / ``InnDrinkTable`` 的一行：客栈菜品与饮品。"""

    id: str = ""
    """对应 ItemTable 的物品 id"""


class ItemShowingTypeRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ItemShowingTypeTable`` 的一行：背包页签。"""

    type: str = ""
    """页签 key，物品用 showingType 引用它"""
    name: TextRef = TextRef()
    """页签显示名"""


class ValuableDepotRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ValuableDepot`` 的一行：仓库页签。"""

    type: str = ""
    """页签 key，物品用 valuableTabType 引用它，如 Weapon / CommercialItem"""
    name: TextRef = TextRef()
    """页签显示名"""
    is_hidden: bool = False
    """是否隐藏页签"""


class GasRow(msgspec.Struct, frozen=True, rename="camel"):
    """``GasTable`` 的一行：气体与它的满/空气罐。"""

    full_gas_jar_items: list[str] = msgspec.field(default_factory=list[str])
    """装满这种气体的罐子，填物品 id"""
    empty_gas_jar_items: list[str] = msgspec.field(default_factory=list[str])
    """空罐子的物品 id"""


class LiquidRow(msgspec.Struct, frozen=True, rename="camel"):
    """``LiquidTable`` 的一行：液体与它的满/空瓶子。"""

    full_bottle_items: list[str] = msgspec.field(default_factory=list[str])
    """装满这种液体的瓶子，填物品 id"""
    empty_bottle_items: list[str] = msgspec.field(default_factory=list[str])
    """空瓶子的物品 id"""


class FactoryBuildingRow(msgspec.Struct, frozen=True, rename="camel"):
    """``FactoryBuildingTable`` 的一行（只看是否限定地区）。"""

    recommend_domains: list[str] = msgspec.field(default_factory=list[str])
    """推荐放置的地区；非空说明这个建筑只在部分地区有"""


class FactoryBuildingItemReverseRow(msgspec.Struct, frozen=True, rename="camel"):
    """``FactoryBuildingItemReverseTable`` 的一行：建筑 -> 产出的物品。

    这个字段有的版本是单个字符串、有的版本是数组，两种都要认。
    """

    item_id: str | list[str] = ""
    """这个建筑产出的物品 id"""


# ── 能量淤积点 ──────────────────────────────────────────────────────────────


class WorldEnergyPointRow(msgspec.Struct, frozen=True, rename="camel"):
    """``WorldEnergyPointTable`` 的一行。"""

    game_name: TextRef = TextRef()
    """淤积点的名字"""
    level_id: str = ""
    """所在关卡 id，当地区标识用（一个地区一个）"""
    cost_stamina: int = 0
    """打一次消耗的体力"""
    game_group_id: str = ""
    """词条分组，查 WorldEnergyPointGroupTable"""
    game_mechanics_id: str = ""
    """机制 id；点自己没写体力时拿它去 GameMechanicTable 找"""


class WorldEnergyPointGroupRow(msgspec.Struct, frozen=True, rename="camel"):
    """``WorldEnergyPointGroupTable`` 的一行：两组词条 id。"""

    sec_attr_term_ids: list[str] = msgspec.field(default_factory=list[str])
    """副属性词条 id，去 GemTable.tagName 换中文名"""
    skill_term_ids: list[str] = msgspec.field(default_factory=list[str])
    """技能词条 id，同样查 GemTable"""


class GameMechanicRow(msgspec.Struct, frozen=True, rename="camel"):
    """``GameMechanicTable`` 的一行（只取体力消耗兜底值）。"""

    cost_stamina: int = 0
    """这个机制消耗的体力（淤积点自己没写时用）"""


class GemRow(msgspec.Struct, frozen=True, rename="camel"):
    """``GemTable`` 的一行（词条名）。"""

    tag_name: TextRef = TextRef()
    """词条名"""


# ── 武器与技能 ──────────────────────────────────────────────────────────────


class WeaponBasicRow(msgspec.Struct, frozen=True, rename="camel"):
    """``WeaponBasicTable`` 的一行：已上线武器。"""

    weapon_type: str = ""
    """武器类型，如 Sword / Claymores / Pistol / Wand / Lance"""
    rarity: int = 0
    """稀有度，3–6 星"""
    weapon_skill_list: list[str] = msgspec.field(default_factory=list[str])
    """这把武器的技能 id，查 SkillPatchTable 拿技能名"""
    eng_name: TextRef = TextRef()
    """英文名；ItemTable 里没有这把武器时用它当名字"""


class ForesightWeaponRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ForesightWeaponTable`` 的一行：还没上线的前瞻武器。"""

    weapon_id: str = ""
    """真 id（行 key 不一定可靠，有的 dump 按数字排序拿 id 当 key）"""
    name: TextRef = TextRef()
    """武器名"""
    icon_id: str = ""
    """图标键；前瞻武器还没有 ItemTable 行，图标就用它"""
    wpn_type: str = ""
    """武器类型，作用同 WeaponBasicTable.weaponType"""
    rarity: int = 0
    """稀有度，与正式武器同样的星级"""
    sk_ids: list[str] = msgspec.field(default_factory=list[str])
    """技能 id"""
    sk_names: list[TextRef] = msgspec.field(default_factory=list[TextRef])
    """与 sk_ids 一一对应的技能名；这些技能还没进 SkillPatchTable"""


class SkillPatchBundle(msgspec.Struct, frozen=True, rename="camel"):
    """``SkillPatchTable`` 行里 ``SkillPatchDataBundle`` 的一项（只取技能名）。"""

    skill_name: TextRef = TextRef()
    """技能名"""


class SkillPatchRow(msgspec.Struct, frozen=True):
    """``SkillPatchTable`` 的一行；这个字段名本身就是 PascalCase，不能靠 ``rename``。"""

    skill_patch_data_bundle: list[SkillPatchBundle] = msgspec.field(
        default_factory=list[SkillPatchBundle], name="SkillPatchDataBundle"
    )
    """技能数据列表，取第一项的名字"""


# ── 环境监测 ────────────────────────────────────────────────────────────────


class KiteStationMission(msgspec.Struct, frozen=True, rename="camel"):
    """``KiteStationEntrustTasksTable`` 里的一个观察点。"""

    mission_id: str = ""
    """观察点 id，MaaEnd 的路线配置按它对上"""
    entrust_idx: int = 0
    """终端里的序号，用来排序"""
    kite_station: str | None = None
    """所属监测终端 id；有的观察点不写"""
    name: TextRef = TextRef()
    """观察点名"""
    shot_target_name: TextRef = TextRef()
    """要拍的目标名"""


class KiteStationEntrustRow(msgspec.Struct, frozen=True, rename="camel"):
    """``KiteStationEntrustTasksTable`` 的一行：一个监测终端下的观察点。"""

    missions: dict[str, KiteStationMission] = msgspec.field(default_factory=dict[str, KiteStationMission], name="list")
    """这个终端下的观察点，key 是表里的行 key"""


class KiteStationLevelRow(msgspec.Struct, frozen=True, rename="camel"):
    """``KiteStationLevelTable`` 的一行（只取终端显示名）。"""

    name: TextRef = TextRef()
    """终端显示名"""


# ── 干员与据点 ──────────────────────────────────────────────────────────────


class CharacterRow(msgspec.Struct, frozen=True, rename="camel"):
    """``CharacterTable`` 的一行。"""

    name: TextRef = TextRef()
    """干员名"""
    rarity: int = 0
    """稀有度，4–6 星"""
    profession: str = ""
    """职业，如 GUARD / CASTER / DEFENDER"""
    char_type_id: str = ""
    """伤害类型，如 Physical / Fire / Cryst"""
    weapon_type: str = ""
    """能用的武器类型，如 Sword / Wand"""


class CharacterTagRow(msgspec.Struct, frozen=True, rename="camel"):
    """``CharacterTagTable`` 的一行：干员身上的标签。"""

    bloc_tag_id: str = ""
    """职业或阵营标签"""
    expert_tag_ids: list[str] = msgspec.field(default_factory=list[str])
    """专精标签，据点特性按这些标签挑干员"""
    hobby_tag_ids: list[str] = msgspec.field(default_factory=list[str])
    """爱好标签"""


class SettlementTradeItem(msgspec.Struct, frozen=True, rename="camel"):
    """``SettlementBasicDataTable`` 里一个能卖的物品。"""

    item_id: str = ""
    """卖出去的物品 id"""
    reward_money_count: int = 0
    """卖一个得多少据点货币"""
    activity_id: str = ""
    """限定活动 id，非活动时是空串"""


class SettlementLevel(msgspec.Struct, frozen=True, rename="camel"):
    """``SettlementBasicDataTable`` 里一级繁荣度。"""

    settlement_trade_item_map: dict[str, SettlementTradeItem] = msgspec.field(
        default_factory=dict[str, SettlementTradeItem]
    )
    """这一级能卖的物品，key 是物品 id"""


class SettlementRow(msgspec.Struct, frozen=True, rename="camel"):
    """``SettlementBasicDataTable`` 的一行：一个据点。"""

    domain_id: str = ""
    """所在地区"""
    settlement_name: TextRef = TextRef()
    """据点名"""
    want_tag_id_group: list[str] = msgspec.field(default_factory=list[str])
    """这个据点的特性 id，查 SettlementTagTable"""
    settlement_level_map: dict[str, SettlementLevel] = msgspec.field(default_factory=dict[str, SettlementLevel])
    """繁荣度等级（字符串数字）-> 各级能卖的物品"""


class SettlementTagRow(msgspec.Struct, frozen=True, rename="camel"):
    """``SettlementTagTable`` 的一行：据点特性与它增强的三项收益。"""

    enhance_char_tag_id: list[str] = msgspec.field(default_factory=list[str])
    """需要哪些干员标签才能触发这个特性"""
    enhance_exp_profit_rate: int = 0
    """经验收益加成；0 表示没有这项"""
    enhance_money_profit_rate: int = 0
    """货币收益加成"""
    enhance_money_produce_speed_rate: int = 0
    """货币生产速度加成"""


# ── 地图标记与关卡地图 ──────────────────────────────────────────────────────


class LevelMarkBasic(msgspec.Struct, frozen=True, rename="camel"):
    """``LevelMapMark`` 里一个标记的 ``basicData``。"""

    template_id: str = ""
    """标记模板 id，营地是 mark_sp_campfire"""
    mark_inst_id: str = ""
    """标记实例 id，营地拿它当传送锚点 id"""
    pos: Position3 = Position3()
    """世界坐标"""
    tier_map: int = 0
    """所在层级；取地下层名时按它查"""


class LevelMarkDetailed(msgspec.Struct, frozen=True, rename="camel"):
    """``LevelMapMark`` 里一个标记的 ``detailedData``（只有营地有传送 id）。"""

    teleport_validation_id: str | None = None
    """传送到这个点用的锚点 id；只有营地有"""


class LevelMark(msgspec.Struct, frozen=True, rename="camel"):
    """``LevelMapMark`` 里的一个标记。"""

    basic_data: LevelMarkBasic = LevelMarkBasic()
    """位置、模板这些基本信息"""
    detailed_data: LevelMarkDetailed = LevelMarkDetailed()
    """模板自己的附加数据"""


class TextPhase(msgspec.Struct, frozen=True, rename="camel"):
    """地名标签按任务推进换的那几段文本。"""

    text_id: str = ""
    """这一段对应的文本 key"""


class LevelMapElement(msgspec.Struct, frozen=True, rename="camel"):
    """``UILevelMapLoadConfig`` 里 ``staticElements`` 的一个元素（只用地名标签）。"""

    type: int = 0
    """元素类型；2 是地名标签"""
    position: Position3 = Position3()
    """世界坐标，用来找离营地最近的标签"""
    text_id: str = ""
    """默认的文本 key"""
    text_phases: list[TextPhase] = msgspec.field(default_factory=list[TextPhase])
    """任务推进后换的文本，按顺序取最后一段"""


class LevelMap(msgspec.Struct, frozen=True, rename="camel"):
    """``UILevelMapLoadConfig/<level_id>.json``。"""

    tier_names: dict[str, str] = msgspec.field(default_factory=dict[str, str])
    """层级 id（字符串数字）-> 该层的名字文本 key"""
    static_elements: dict[str, LevelMapElement] = msgspec.field(default_factory=dict[str, LevelMapElement])
    """静态元素，key 是元素 id"""


class LevelDescRow(msgspec.Struct, frozen=True, rename="camel"):
    """``LevelDescTable`` 的一行（只取显示名）。"""

    show_name: TextRef = TextRef()
    """关卡显示名"""


# ── 采集物与关卡 ────────────────────────────────────────────────────────────


class WorldEntityBrief(msgspec.Struct, frozen=True, rename="camel"):
    """``WorldEntityRegistry`` 里 ``worldEntityBriefInfos`` 的一条。"""

    detail_id: str | None = None
    """实体模板 id；可交互采集物以 int_doodad_ 开头，没有的这里是 null"""
    position: Position3 = Position3()
    """世界坐标"""


class WorldEntityRegistry(msgspec.Struct, frozen=True, rename="camel"):
    """``WorldEntityRegistry``（只取实体简表）。"""

    world_entity_brief_infos: dict[str, WorldEntityBrief] = msgspec.field(default_factory=dict[str, WorldEntityBrief])
    """实体 id -> 简表"""


class LevelBasicInfoRow(msgspec.Struct, frozen=True, rename="camel"):
    """``LevelBasicInfoTable`` 的一行：一个关卡。"""

    id_num: int = 0
    """关卡编号；实体 id 除以 1e8 就是它"""


class DoodadRow(msgspec.Struct, frozen=True, rename="camel"):
    """``DoodadGeneralTable`` 的一行：一种可交互物。"""

    item_id: str = ""
    """通用产物物品 id；写了就不用去翻奖励表"""
    pickable_reward_id: list[str] = msgspec.field(default_factory=list[str])
    """拾取掉落的奖励 id"""
    breaked_drop_reward_id: list[str] = msgspec.field(default_factory=list[str])
    """打碎掉落的奖励 id"""
    is_pickable: bool = False
    """能不能拾取"""
    is_breakable: bool = False
    """能不能打碎"""
    label: TextRef = TextRef()
    """采集物自己的名字；为空就退回产物名"""


# ── 送货目的地 ──────────────────────────────────────────────────────────────


class DeliverTargetRow(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDepotDeliverTargetTable`` 的一行：一个送货点。"""

    level: str = ""
    """所在关卡 id"""
    target_id: str = ""
    """目标实体的 key：NPC 的 key 或回收站的 key"""
    entity_type: str = ""
    """目标类型，NpcProxy 或 RecycleBin"""
    mission_obj_desc: TextRef = TextRef()
    """任务目标文案"""


class BuyerRow(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDepotBuyerTable`` 的一行：收货人。"""

    level: str = ""
    """所在关卡 id"""
    buyer_name: TextRef = TextRef()
    """收货人名字"""


class RecycleBinRow(msgspec.Struct, frozen=True, rename="camel"):
    """``RecycleBinTable`` 的一行：资源回收站。"""

    level_id: str = ""
    """所在关卡 id"""
    serial_id: int = 0
    """回收站序号，区分同一个关卡里的几个回收站"""


class NpcProxyRow(msgspec.Struct, frozen=True, rename="camel"):
    """``NpcProxyTable`` 里 ``dataTable`` 的一条：坐标与朝向。"""

    position: Position3 = Position3()
    """世界坐标"""
    rotation: Position3 = Position3()
    """朝向（欧拉角），只用 y 当水平朝向"""


class NpcProxyTable(msgspec.Struct, frozen=True, rename="camel"):
    """``NpcProxyTable``（只取 ``dataTable``）。"""

    data_table: dict[str, NpcProxyRow] = msgspec.field(default_factory=dict[str, NpcProxyRow])
    """NPC key -> 坐标与朝向"""


class ComponentValue(msgspec.Struct, frozen=True, rename="camel"):
    """组件参数里的一个值。

    参数自己带 ``type`` 说该读哪一项：``Float`` / ``Int`` 之类的读 ``valueBit64``，``String`` 读
    ``valueString``。我们只用到字符串那一项，整数也声明出来，免得上游换了类型时这里静默变空。
    """

    value_string: str | None = None
    """字符串取值（``type`` 为 ``String`` 时用它）"""
    value_bit64: int | None = None
    """整数或位掩码取值（``type`` 为 ``Float`` / ``Int`` 时用它）"""


class ComponentValueSet(msgspec.Struct, frozen=True, rename="camel"):
    """组件的某个参数可以带一串值；要用到的参数都只取第一项。"""

    value_array: list[ComponentValue] = msgspec.field(default_factory=list[ComponentValue], name="valueArray")
    """取值列表"""


class ComponentProperty(msgspec.Struct, frozen=True, rename="camel"):
    """交互实体的一个组件参数：``[{"key": "system_inst_key", "value": {...}}]`` 里的一项。"""

    key: str = ""
    """参数名，如 ``system_inst_key`` / ``doodad_id``"""
    value: ComponentValueSet = ComponentValueSet()
    """取值"""


class LevelInteractive(msgspec.Struct, frozen=True, rename="camel"):
    """``LevelData`` 里 ``interactives`` 的一项。"""

    entity_data_id_key: str = ""
    """实体模板 key，如 int_system_domain_depot"""
    rotation: Position3 = Position3()
    """朝向，只用 y 当水平朝向"""
    component_properties: dict[str, list[ComponentProperty]] = msgspec.field(
        default_factory=dict[str, list[ComponentProperty]]
    )
    """组件名 -> 这个组件的参数列表（如 ``NpcProxy`` / ``RecycleBin``）"""


class LevelData(msgspec.Struct, frozen=True, rename="camel"):
    """``LevelData/<level_id>/*.json``（只取交互实体）。"""

    interactives: list[LevelInteractive] = msgspec.field(default_factory=list[LevelInteractive])
    """这份关卡文件里的交互实体"""


class InteractiveTemplateVariant(msgspec.Struct, frozen=True, rename="camel"):
    """交互模板的一个变体：它默认参数里被改掉的那几项。"""

    diff_properties: list[ComponentProperty] = msgspec.field(default_factory=list[ComponentProperty])
    """改掉的默认参数，采集物的 ``doodad_id`` 在这里"""


class InteractiveTemplate(msgspec.Struct, frozen=True, rename="camel"):
    """``Interactive/InteractiveData/data_*.json``：交互模板表。

    一份文件里按 ``templateVariant`` 分若干变体，每个变体各自改掉一批默认参数；采集物的 ``doodad_id``
    就在变体的 ``diffProperties`` 里。
    """

    id: str = ""
    """文件自己的模板 id，如 int_doodad_common"""
    template_variant: dict[str, InteractiveTemplateVariant] = msgspec.field(
        default_factory=dict[str, InteractiveTemplateVariant]
    )
    """变体 id -> 这个变体的参数覆盖"""


# ── 运送委托 ────────────────────────────────────────────────────────────────


class FactoryItemRow(msgspec.Struct, frozen=True, rename="camel"):
    """``FactoryItemTable`` 的一行：能装箱的物品与它能去的地方。"""

    deliver_item_type_list: list[str] = msgspec.field(default_factory=list[str])
    """能装的运送类型，与仓储节点等级上的类型求交集"""
    transfer_domain_ids: list[str] = msgspec.field(default_factory=list[str])
    """能转运到的地区"""


class DomainDepotRow(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDepotTable`` 的一行：仓储节点。"""

    domain_depot_id: str = ""
    """仓储节点 id，正常与行 key 一致"""
    domain_id: str = ""
    """所在地区"""
    ref_level_id: str = ""
    """所在关卡；送货目的地靠「关卡 -> 节点」对应"""
    depot_name: TextRef = TextRef()
    """节点名"""


class DomainDepotLevel(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDepotLevelTable`` 里的一级。"""

    level: int = 0
    """等级"""
    deliver_item_type_list: list[str] = msgspec.field(default_factory=list[str])
    """这一级能装的运送类型"""
    deliver_pack_type_list: list[str] = msgspec.field(default_factory=list[str])
    """这一级能用的货箱大小"""


class DomainDepotLevelRow(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDepotLevelTable`` 的一行：一个仓储节点的各级配置。"""

    level_list: dict[str, DomainDepotLevel] = msgspec.field(default_factory=dict[str, DomainDepotLevel])
    """等级（字符串数字）-> 该等级的配置"""


class DomainDepotPackValue(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDepotPackValueTable`` 中一种货物与货箱的价值上限。"""

    deliver_item_type: str = ""
    """货物类型，与等级配置和货物价格系数对应"""
    deliver_pack_type: str = ""
    """货箱大小，与等级配置对应"""
    max_limit: int = 0
    """装箱物品总价值上限，不是买家报价"""


class DomainDepotPackValueRow(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDepotPackValueTable`` 的一行：一个仓储节点的货箱价值配置。"""

    pack_value_list: list[DomainDepotPackValue] = msgspec.field(default_factory=list[DomainDepotPackValue])
    """按货物类型和货箱大小区分的价值配置"""


class DomainDepotDeliverItemTypeRow(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDepotDeliverItemTypeTable`` 的一行：货物类型的价格系数。"""

    deliver_item_type: str = ""
    """货物类型，如 NaturalResource / Industry / Misc"""
    price_factor: float = 0.0
    """物品总价值换算基础报价时使用的系数"""


# ── 干员培养 ────────────────────────────────────────────────────────────────


class GrowthNode(msgspec.Struct, frozen=True, rename="camel"):
    """``CharGrowthTable`` 里的一个培养节点。

    ``node_type`` 有两种写法：1–5 的整数，或名字（``CharBreak`` 等）；两种都收，统一由
    :func:`tools.zmdmap.datasets.operator_growth.node_type` 转成数字再比较。
    """

    node_id: str = ""
    """节点 id，如 charBreak20"""
    node_type: str | int = 0
    """节点类型：1 突破 / 2 装备档位突破 / 3 潜能 / 4 被动 / 5 基建，有的版本写名字"""
    break_stage: int = 0
    """突破阶段"""
    equip_tier_limit: int = 0
    """这一阶段解锁的装备档位"""
    required_item: list[ItemStack] = msgspec.field(default_factory=list[ItemStack], name="requiredItem")
    """消耗的素材"""


class SkillLevelUp(msgspec.Struct, frozen=True, rename="camel"):
    """``CharGrowthTable`` 里 ``skillLevelUp`` 的一项：某组技能升到某一级。"""

    skill_group_id: str = ""
    """技能组 id，如 chr_0002_endminm_NormalSkill"""
    level: int = 0
    """升到的目标等级"""
    gold_cost: int = 0
    """折金票消耗"""
    item_bundle: list[ItemStack] = msgspec.field(default_factory=list[ItemStack])
    """素材消耗"""


class CharGrowthRow(msgspec.Struct, frozen=True, rename="camel"):
    """``CharGrowthTable`` 的一行：一个干员的全部培养节点。"""

    char_break_cost_map: dict[str, GrowthNode] = msgspec.field(default_factory=dict[str, GrowthNode])
    """突破与装备档位节点，key 是节点 id"""
    talent_node_map: dict[str, GrowthNode] = msgspec.field(default_factory=dict[str, GrowthNode])
    """天赋节点（潜能 / 被动 / 基建），key 是节点 id"""
    skill_level_up: list[SkillLevelUp] = msgspec.field(default_factory=list[SkillLevelUp])
    """技能逐级消耗"""


class CharLevelUpRow(msgspec.Struct, frozen=True, rename="camel"):
    """``CharLevelUpTable`` 的一行：升到下一级的经验与折金票。"""

    exp: int = 0
    """升到下一级需要的经验"""
    gold: int = 0
    """升到下一级需要的折金票"""


# ── 奖励与商店 ──────────────────────────────────────────────────────────────


class RewardRow(msgspec.Struct, frozen=True, rename="camel"):
    """``RewardTable`` 的一行。"""

    item_bundles: list[ItemStack] = msgspec.field(default_factory=list[ItemStack])
    """必掉的物品"""
    prob_item_bundles: list[ItemStack] = msgspec.field(default_factory=list[ItemStack])
    """概率掉落的物品，形状与 item_bundles 一样"""


class DomainRow(msgspec.Struct, frozen=True, rename="camel"):
    """``DomainDataTable`` 的一行：地区。"""

    domain_shop_group_id: str = ""
    """这个地区的商店组，查 ShopGroupTable"""
    domain_name: TextRef = TextRef()
    """地区名"""
    domain_depot_offer_price_ratio: float = 0.0
    """运送委托基础报价的地区倍率"""
    sort_id: int = 0
    """排序"""


class ShopGroupRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ShopGroupTable`` 的一行：一个商店组下的商店。"""

    shop_ids: list[str] = msgspec.field(default_factory=list[str])
    """组里的商店 id，查 ShopTable"""


class ShopRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ShopTable`` 的一行。

    ``shop_refresh_type`` 这类枚举既可能是名字也可能是个整数（``None`` / ``0``），两种都收。
    """

    shop_id: str = ""
    """商店 id"""
    shop_refresh_type: str | int = 0
    """刷新方式；None 或 0 表示不刷新的常驻货架"""
    shop_goods_ids: list[str] = msgspec.field(default_factory=list[str])
    """货架上的商品 id，查 ShopGoodsTable"""


class ShopGoodsTagRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ShopGoodsTagTable`` 的一行：商品分类。"""

    tag_name: TextRef = TextRef()
    """分类名"""
    sort_id: int = 0
    """排序"""


class ChannelLevel(msgspec.Struct, frozen=True, rename="camel"):
    """``ShopChannelDevelopmentTable`` 里渠道等级的一项。"""

    new_goods_list: list[str] = msgspec.field(default_factory=list[str])
    """这一级新增的商品 id"""
    new_goods_num: list[int] = msgspec.field(default_factory=list[int])
    """与 new_goods_list 一一对应的解锁数量"""
    version_start: str = ""
    """起始版本，如 v0d8d0"""
    time_id: str = ""
    """时间 id，多数是空串"""


class ShopChannelRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ShopChannelDevelopmentTable`` 的一行：渠道各等级新增的供货。"""

    channel_level_map: dict[str, ChannelLevel] = msgspec.field(default_factory=dict[str, ChannelLevel])
    """渠道等级（字符串数字）-> 这一级的供货"""


class UnlockCondition(msgspec.Struct, frozen=True, rename="camel"):
    """``ShopGoodsTable`` 里的一个解锁条件。"""

    condition_id: str = ""
    """条件 id，如 condition_shop_activity_contingency_contract_0_4_1_1"""
    desc: TextRef = TextRef()
    """条件说明"""


class ShopGoodsRow(msgspec.Struct, frozen=True, rename="camel"):
    """``ShopGoodsTable`` 的一行。

    ``limit_count_refresh_type`` / ``shop_refresh_type`` 这类枚举在有的版本里是字符串、有的是整数，
    所以两边都收；判断时把名字和整数一起列上（如 ``("Weekly", 2)``）。整数是枚举序号，0 不一定是
    ``None``：``limit_count_refresh_type`` 的 0 就是 ``Forever``。
    """

    shop_id: str = ""
    """所属商店"""
    reward_id: str = ""
    """这个商品发的奖励，查 RewardTable"""
    goods_tag_id: str = ""
    """商品分类，查 ShopGoodsTagTable"""
    money_id: str = ""
    """用哪种货币，查 ItemTable"""
    price: int | float = 0
    """单价；表里有整数也有小数，所以声明成 ``int | float``，免得被统一转成小数
    （转成 ``20.0`` 会让产物与仓库里的 ``20`` 差一个字符）"""
    cn_discount: int | float = 1
    """国服折扣率，1 表示原价"""
    limit_count_refresh_type: str | int = 0
    """限购的刷新方式，如 Forever / Daily / Weekly / ByTimeId；整数形态里 0 是 Forever、2 是 Weekly"""
    limit_count: int = 0
    """限购数量"""
    sort_id: int = 0
    """排序"""
    unlock_conditions: list[UnlockCondition] = msgspec.field(default_factory=list[UnlockCondition])
    """解锁条件；原样写进产物，不给产物换形状"""


# ── 读取 ────────────────────────────────────────────────────────────────────


def _decode(raw: bytes, model: Any, label: str) -> Any:
    try:
        return msgspec.json.decode(raw, type=model)
    except msgspec.DecodeError as error:
        raise LocalDataError(f"{label} 的结构与预期不符：{error}") from error


def _read_bytes(path: Path, label: str) -> bytes:
    try:
        return path.read_bytes()
    except OSError as error:
        raise LocalDataError(f"缺少 {label} 文件：{path}") from error


# 缓存手工维护，不用 ``functools.cache``：那个装饰器把函数变成 ``Callable[..., T]``，调用处拿到的
# 类型会退化成 ``Any``，本文件存在的意义（让调用处知道每张表是什么结构）就没了。
_table_cache: dict[tuple[Path, str, Any], Any] = {}
_config_cache: dict[tuple[Path, str, Any], Any] = {}
_level_cache: dict[tuple[Path, str, Any], Any] = {}


def load_table[T](table_cfg_dir: Path, name: str, model: type[T]) -> dict[str, T]:
    """``<TableCfg>/<name>.json`` 类型化解码：``{行 id: model}``。

    结果按（目录、表名、模型）缓存 —— 一张表常被几份数据读，解码一次就够。
    """
    key = (table_cfg_dir, name, model)
    cached = _table_cache.get(key)
    if cached is None:
        raw = _read_bytes(table_cfg_dir / f"{name}.json", "TableCfg")
        cached = _decode(raw, dict[str, model], f"{name}.json")
        _table_cache[key] = cached
    return cached


def load_config[T](json_data_dir: Path, name: str, model: type[T]) -> T:
    """``<JsonData>/GameplayConfig/<name>.json`` 类型化解码。"""
    key = (json_data_dir, name, model)
    cached = _config_cache.get(key)
    if cached is None:
        raw = _read_bytes(json_data_dir / "GameplayConfig" / f"{name}.json", "GameplayConfig")
        cached = _decode(raw, model, f"{name}.json")
        _config_cache[key] = cached
    return cached


def load_level[T](json_data_dir: Path, level_id: str, model: type[T]) -> T:
    """``<JsonData>/UILevelMapLoadConfig/<level_id>.json`` 类型化解码。"""
    key = (json_data_dir, level_id, model)
    cached = _level_cache.get(key)
    if cached is None:
        raw = _read_bytes(json_data_dir / "UILevelMapLoadConfig" / f"{level_id}.json", "UILevelMapLoadConfig")
        cached = _decode(raw, model, f"{level_id}.json")
        _level_cache[key] = cached
    return cached


def load_interactive_data(path: Path) -> InteractiveTemplate:
    """``<JsonData>/Interactive/InteractiveData/data_*.json`` 类型化解码。"""
    key = (path, "interactive", InteractiveTemplate)
    cached = _level_cache.get(key)
    if cached is None:
        raw = _read_bytes(path, "InteractiveData")
        cached = _decode(raw, InteractiveTemplate, path.name)
        _level_cache[key] = cached
    return cached


def load_level_data(path: Path) -> LevelData:
    """``LevelData`` 下的一个关卡文件；每份文件只读一次，不需要缓存。"""
    raw = _read_bytes(path, "LevelData")
    return _decode(raw, LevelData, path.name)
