"""从当版表、Lua 字面量和 prefab 引用建立地区输入，不执行 Lua。"""

import re
from dataclasses import replace
from typing import TYPE_CHECKING

from .errors import InputError
from .layout import find_component, region_settings, resolve_reference
from .lua import format_template, literal_string, strip_comments
from .registry import (
    EntryAnimationBinding,
    RegionRegistration,
    RegionRegistry,
)
from .unity import dict_entries, narrow_dict, narrow_list

if TYPE_CHECKING:
    from .provider import LocalDataProvider
    from .unity import UnityDocument, UnityNode

MAP_CONST = "Const/MapConst.lua"
UI_CONST = "Const/UIConst.lua"
PANEL_CONFIG = "UI/Panels/PanelConfig.lua"
REGION_CTRL = "UI/Panels/RegionMap3D/RegionMap3DCtrl.lua"


def _logical(path: str) -> str:
    """把 ``Assets/...`` 归一成 ``assets/...`` 逻辑路径，并挡掉跳出资源根的那种写法。"""
    path = path.replace("\\", "/").lstrip("/").lower()
    if not path.startswith("assets/") or any(part in {"", ".", ".."} for part in path.split("/")):
        raise InputError(f"资源引用不合法：{path!r}")
    return path


def _clip_path(reference: object) -> str:
    path = narrow_dict(reference).get("$ref")
    if not isinstance(path, str):
        raise InputError(f"动画必须是一个导出的资源引用：{reference!r}")
    path = _logical(path).removesuffix(".json")
    return path if path.endswith(".anim") else path + ".anim"


def _reference(document: UnityDocument, node: UnityNode, key: str) -> UnityNode:
    component = find_component(node, "LuaReference")
    values = dict_entries(component.get("refDict")) if component else {}
    target = resolve_reference(document, values.get(key), what=f"{node.path}.{key}")
    if target is None:
        raise InputError(f"{node.path}：LuaReference '{key}' 解不开")
    return target


def discover_registry(provider: LocalDataProvider) -> RegionRegistry:
    """地区由 DomainDataTable 枚举；每个地区下的子地图仍由 RegionMapSetting._cfg 枚举。"""
    map_template = literal_string(provider.get_lua(MAP_CONST), "UI_DOMAIN_MAP_PATH")
    animation_template = literal_string(provider.get_lua(REGION_CTRL), "SWITCH_ANIMATION_IN_FORMAT")
    ui_source = provider.get_lua(UI_CONST)
    constants: dict[str, str] = {}
    for key in (
        "UI_ASSETS_PATH",
        "UI_PANEL_PREFAB_FORMAT",
        "UI_PC_PANEL_PREFAB_FORMAT",
        "UI_NODE_PREFAB_PATH",
        "UI_PANEL_PREFAB_PATH",
        "UI_PC_PANEL_PREFAB_PATH",
    ):
        constants[key] = literal_string(ui_source, key, constants)
    config = strip_comments(provider.get_lua(PANEL_CONFIG))
    blocks = re.findall(r"\bRegionMap3D\s*=\s*\{([^{}]*)\}", config)
    if len(blocks) != 1:
        raise InputError("PanelConfig 里的 RegionMap3D 应当只有一段扁平配置")
    folder = literal_string(blocks[0], "folder")
    panel_name = (
        literal_string(blocks[0], "name") if re.search(r"^\s*name\s*=", blocks[0], re.MULTILINE) else "RegionMap3D"
    )
    panel_path = _logical(format_template(constants["UI_PANEL_PREFAB_PATH"], folder, panel_name))
    pc_path = _logical(format_template(constants["UI_PC_PANEL_PREFAB_PATH"], folder, panel_name))
    if not provider.missing_asset_paths([pc_path]):
        panel_path = pc_path
    panel = provider.get_asset(panel_path)
    domain_root = _reference(panel, panel.root, "domainRoot")
    domain_reference = find_component(domain_root, "LuaReference")
    mount_keys: set[str] = set(dict_entries(domain_reference.get("refDict"))) if domain_reference else set()
    wrapper = find_component(panel.root, "UIAnimationWrapper")
    if wrapper is None:
        raise InputError("地区面板 prefab 上没有 UIAnimationWrapper")
    panel_entry = _clip_path(wrapper.get("_animationIn"))
    camera_path = provider.find_prefab("UICamera", "Camera")
    ui_node_path = _logical(constants["UI_NODE_PREFAB_PATH"])

    regions: list[RegionRegistration] = []
    seen: set[str] = set()
    domains = narrow_dict(provider.get_table("DomainDataTable"))
    if not domains:
        raise InputError("DomainDataTable 是空的")
    for domain_id, raw in sorted(domains.items()):
        domain = narrow_dict(raw)
        domain_map = domain.get("domainMap")
        # domainMap 为空的不算地区总览入口；不看 levelGroup，也不猜解锁状态。
        if domain_map in (None, ""):
            continue
        if not isinstance(domain_map, str):
            raise InputError(f"{domain_id}：domainMap 应当是字符串")
        region_id = domain_map.lower()
        if region_id in seen:
            raise InputError(f"domainMap 重复：{domain_map}")
        seen.add(region_id)
        name_id = narrow_dict(domain.get("domainName")).get("id")
        if name_id is None or not str(name_id).isdecimal():
            raise InputError(f"{domain_id}：domainName.id 缺失或不是数字")
        mount = _reference(panel, domain_root, region_id)
        animation_node = _reference(panel, mount, "animationWrapper")
        animation = find_component(animation_node, "Animation")
        clip_name = format_template(animation_template, region_id)
        clips: set[str] = set()
        for reference in narrow_list(animation.get("m_Animations")) if animation else []:
            path = _clip_path(reference)
            if narrow_dict(provider.get_asset_data(path)).get("m_Name") == clip_name:
                clips.add(path)
        if len(clips) != 1:
            raise InputError(f"{region_id}：绑定的动画 clip 应当恰好有一个叫 {clip_name!r} 的")
        region = RegionRegistration(
            region_id=region_id,
            name_id=str(name_id),
            region_prefab=_logical(format_template(map_template, domain_map)),
            mount_keys=("domainRoot", region_id, "transform"),
            entry_animations=(
                EntryAnimationBinding(
                    next(iter(clips)),
                    animation_node.path.removeprefix(panel.root.path).lstrip("/"),
                ),
            ),
        )
        _, settings = region_settings(provider.get_asset(region.region_prefab), region)
        paths: dict[str, str] = {}
        for setting in settings:
            ui = setting.ui_path
            paths[setting.level_id] = (
                _logical(ui)
                if ui.lower().lstrip("/").startswith("assets/")
                else provider.find_prefab(ui.removesuffix(".prefab"), "UIRegionMap3DUICell")
            )
        regions.append(replace(region, ui_paths=paths))
    if not regions:
        raise InputError("DomainDataTable 里没有地区总览入口")
    unknown = mount_keys - seen - {"transform"}
    if unknown:
        raise InputError(f"domainRoot 上有 DomainDataTable 里没有的挂载点：{sorted(unknown)}")
    return RegionRegistry(
        regions=tuple(regions),
        camera_path=camera_path,
        ui_node_path=ui_node_path,
        panel_path=panel_path,
        panel_entry_animation=panel_entry,
    )
