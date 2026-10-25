"""直接生成 region -> {name, area: map_id -> {polygon, click}}。"""

from copy import deepcopy
from dataclasses import dataclass, replace
from typing import TYPE_CHECKING, Any

from .binary_rules import (
    RECT_TRANSFORM_IMPACT_SCALE,
    STYLE_TARGET_RECT_TRANSFORM,
    canvas_scale,
    initialize_panel,
)
from .errors import GenerationError, LayoutError, Region3dError
from .geometry import (
    Point,
    Quaternion,
    Vector3,
    quat_normalized,
    transform_point,
)
from .layout import (
    UI_CELL_TYPE,
    Chain,
    Hop,
    RegionSetting,
    ancestors,
    compose_chain,
    find_component,
    find_mount,
    find_nodes_with_component,
    node_at_path,
    node_transform,
    region_settings,
    resolve_chain,
    resolve_reference,
)
from .lua import extract_fov
from .options import GenerationOptions
from .projection import (
    CameraSetup,
    camera_view,
    projected_quad,
)
from .provider import LocalDataProvider
from .screen import derive_clicks
from .unity import dict_entries, narrow_dict, narrow_list

if TYPE_CHECKING:
    from .registry import RegionRegistration, RegionRegistry
    from .unity import UnityDocument, UnityNode

STYLE_CONTROLLER_TYPE = "UIStateController"
STYLE_COMPONENT_TYPE = "UIStyleByState"


# ── 快照 ───────────────────────────────────────────────────────────────────


@dataclass(slots=True)
class PanelStyle:
    scale: Vector3
    target_path: str


@dataclass(slots=True)
class SnapshotContext:
    """与输出尺寸无关、整份数据共用的那一半。"""

    provider: LocalDataProvider
    screen: Point
    panel: UnityDocument
    ui_node: UnityDocument
    canvas_root: UnityNode
    panel_root: UnityNode
    camera_setup: CameraSetup


@dataclass(slots=True)
class LevelCandidate:
    level_id: str
    quad: tuple[Point, Point, Point, Point]


# ── 相机与面板 ─────────────────────────────────────────────────────────────


def _camera_setup(document: UnityDocument, fov_deg: float) -> CameraSetup:
    nodes = find_nodes_with_component(document, "Camera")
    if len(nodes) != 1:
        raise GenerationError("相机 prefab 里应当恰好有一个 Camera 组件")
    node = nodes[0]
    component = find_component(node, "Camera")
    assert component is not None
    raw = component.data
    viewport = narrow_dict(raw.get("m_NormalizedViewPortRect"))
    if viewport and any(float(viewport.get(k, v)) != v for k, v in (("x", 0), ("y", 0), ("width", 1), ("height", 1))):
        raise GenerationError("不支持局部视口")
    shift = narrow_dict(raw.get("m_LensShift"))
    frames, _ = compose_chain([(document, n) for n in ancestors(document, node)])
    return CameraSetup(
        world_matrix=frames[-1].world_matrix,
        fov_deg=fov_deg,
        near_clip=float(raw.get("near clip plane", raw.get("nearClipPlane", 0.3))),
        orthographic=bool(raw.get("orthographic", raw.get("m_Orthographic", False))),
        lens_shift=(float(shift.get("x", 0)), float(shift.get("y", 0))),
    )


def _canvas_world_z(panel: UnityDocument, panel_root: UnityNode) -> float:
    # 调用方必须传入登记的 Canvas 所属文档与节点，不能传独立面板根。
    frames, _ = compose_chain([(panel, n) for n in ancestors(panel, panel_root)])
    return transform_point(frames[-1].world_matrix, (0.0, 0.0, 0.0))[2]


def _region_names(provider: LocalDataProvider, registry: RegionRegistry) -> dict[str, str]:
    texts = narrow_dict(provider.get_table("I18nTextTable_CN"))
    names: dict[str, str] = {}
    for region in registry.regions:
        name = texts.get(region.name_id)
        if not isinstance(name, str) or not name.strip():
            raise GenerationError(f"{region.region_id}：缺少地区名字文本 id {region.name_id}")
        names[region.region_id] = name
    return names


def _panel_style(panel: UnityDocument, state: str) -> PanelStyle:
    matches: dict[str, PanelStyle] = {}
    for node in find_nodes_with_component(panel, STYLE_COMPONENT_TYPE):
        for style in node.components_of(STYLE_COMPONENT_TYPE):
            for config in narrow_list(style.get("styleConfigs")):
                for binding in narrow_list(narrow_dict(config).get("stateConfigs")):
                    binding = narrow_dict(binding)
                    if binding.get("stateName") != state:
                        continue
                    ref = narrow_dict(binding.get("stateController"))
                    controller = panel.components.get(str(ref.get("$path_id")))
                    if controller is None or controller.type_name != STYLE_CONTROLLER_TYPE:
                        raise GenerationError(f"{state} 状态对应的 {STYLE_CONTROLLER_TYPE} 解不开")
                    for entry in narrow_list(controller.get("states")):
                        entry = narrow_dict(entry)
                        if entry.get("stateName") != state:
                            continue
                        for desc in narrow_list(entry.get("descriptions")):
                            desc = narrow_dict(desc)
                            if (
                                desc.get("targetType") != STYLE_TARGET_RECT_TRANSFORM
                                or desc.get("rectTransformImpactType") != RECT_TRANSFORM_IMPACT_SCALE
                            ):
                                continue
                            target = resolve_reference(panel, desc.get("rectTransform"), what="样式作用目标")
                            scale = narrow_dict(desc.get("rectTransformScale"))
                            if target is None or not scale:
                                raise GenerationError(f"{state} 状态的缩放目标不完整")
                            matches[target.path] = PanelStyle(
                                (float(scale["x"]), float(scale["y"]), float(scale["z"])),
                                target.path,
                            )
    if len(matches) != 1:
        raise GenerationError(f"面板 prefab 里没有唯一的 '{state}' 缩放状态")
    return next(iter(matches.values()))


# ── 关卡按钮 ───────────────────────────────────────────────────────────────


def resolve_level_button(ui_document: UnityDocument, level_id: str) -> UnityNode:
    """``UIRegionMap3DUICell.btn`` -> UIButton 自己的 GameObject -> 它的 RectTransform。

    不取 ``_cfg.uiRect``、不取 UI 根尺寸、不取标题范围、不取悬停效果，也不取 MeshCollider。
    """
    matched: dict[str, UnityNode] = {}
    for cell_node in find_nodes_with_component(ui_document, UI_CELL_TYPE):
        cell = find_component(cell_node, UI_CELL_TYPE)
        if cell is None:  # pragma: no cover - 上面已经保证有组件
            continue
        level_field = cell.get("levelId", cell.get("level_id", cell.get("level")))
        if level_field is not None and str(level_field) != level_id:
            continue
        btn_node = resolve_reference(ui_document, cell.get("btn"), what=f"{level_id}：cell.btn")
        if btn_node is None:
            raise LayoutError(f"{level_id}：UIRegionMap3DUICell.btn 没设置")
        _check_button_supported(ui_document, find_component(btn_node, "UIButton"), level_id)
        matched[btn_node.game_object_path_id] = btn_node
    if not matched:
        raise LayoutError(f"{level_id}：它的 UI prefab 里没有带 btn 引用的 {UI_CELL_TYPE}")
    if len(matched) > 1:
        raise LayoutError(f"{level_id}：有 {len(matched)} 个不同的 btn 目标，绑定不唯一")
    return next(iter(matched.values()))


def _check_button_supported(document: UnityDocument, button: Any, level_id: str) -> None:
    """只接受「UIButton 的 target Graphic 就是它自己那个节点、且没有任何命中过滤」的按钮。"""
    if button is None:
        raise LayoutError(f"{level_id}：btn 引用上没有 UIButton")
    ref = narrow_dict(button.get("m_TargetGraphic"))
    graphic = document.components.get(str(ref.get("$path_id")))
    graphic_node = resolve_reference(document, ref, what=f"{level_id}：m_TargetGraphic")
    owner = resolve_reference(document, {"$path_id": button.path_id}, what="UIButton 所属节点")
    if graphic is None or graphic_node is None or owner is not graphic_node:
        raise LayoutError(f"{level_id}：target Graphic 的几何解不开或不是按钮自己")
    for source in (button, graphic):
        padding = narrow_dict(source.get("m_RaycastPadding"))
        if any(abs(float(value)) > 1e-6 for value in padding.values()):
            raise LayoutError(f"{level_id}：不支持非零的 m_RaycastPadding")
    if not graphic.get("m_RaycastTarget", graphic.get("raycastTarget", 1)):
        raise LayoutError(f"{level_id}：target Graphic 不接收射线")
    sprite = narrow_dict(graphic.get("m_Sprite"))
    if sprite and sprite.get("$path_id") not in (None, 0, "0"):
        raise LayoutError(f"{level_id}：不支持按 Sprite 形状判定命中")
    if graphic.get("m_UseSpriteMesh", 0) or graphic.get("m_AlphaHitTestMinimumThreshold", 0):
        raise LayoutError(f"{level_id}：不支持按 Sprite 网格或 alpha 判定命中")
    for node in ancestors(document, graphic_node):
        for component in node.components:
            if component.type_name == "CanvasGroup":
                if component.get("m_Enabled", 1) and (
                    not component.get("m_BlocksRaycasts", True) or not component.get("m_Interactable", True)
                ):
                    raise LayoutError(f"{level_id}：CanvasGroup 挡住了点击")
                continue
            if component.type_name in {"Mask", "RectMask2D"} or component.type_name.endswith("RaycastFilter"):
                raise LayoutError(f"{level_id}：不支持 {component.type_name} 这类命中过滤")


def _ui_hops(document: UnityDocument, btn_node: UnityNode) -> list[Hop]:
    """关卡 UI 从实例根到按钮的每一跳，根在前。

    顺序很重要：面板链止于地区 prefab，关卡 UI 接在它下面，所以必须从这份实例自己最上层的节点开始算。
    """
    return [(document, node) for node in ancestors(document, btn_node)]


def _panel_hops(snapshot: SnapshotContext, mount_node: UnityNode) -> list[Hop]:
    return [
        *((snapshot.ui_node, node) for node in ancestors(snapshot.ui_node, snapshot.canvas_root)),
        *((snapshot.panel, node) for node in ancestors(snapshot.panel, mount_node)),
    ]


@dataclass(slots=True)
class ProjectionContext:
    snapshot: SnapshotContext
    panel_hops: list[Hop]

    def project(self, *, level_id: str, chain: Chain) -> LevelCandidate:
        return LevelCandidate(
            level_id=level_id,
            quad=projected_quad(chain, self.snapshot.camera_setup, screen=self.snapshot.screen),
        )


def _level_candidates(
    ctx: ProjectionContext,
    region: RegionRegistration,
    region_document: UnityDocument,
    ui_root_path: str,
    settings: list[RegionSetting],
    ui_cache: dict[str, UnityDocument],
) -> list[LevelCandidate]:
    out: list[LevelCandidate] = []
    for setting in settings:
        ui_path = region.ui_paths.get(setting.level_id)
        if not ui_path:
            raise GenerationError(f"{setting.level_id}：没有对应的 UI 资源")
        document = ui_cache.get(ui_path)
        if document is None:
            document = ctx.snapshot.provider.get_asset(ui_path)
            ui_cache[ui_path] = document
        button = resolve_level_button(document, setting.level_id)
        ui_parent = next((n for n in region_document.games.values() if n.path == ui_root_path), None)
        if ui_parent is None:
            raise LayoutError(f"{region.region_id}：uiRoot '{ui_root_path}' 找不到")
        region_hops: list[Hop] = [(region_document, node) for node in ancestors(region_document, ui_parent)]
        hops = [*ctx.panel_hops, *region_hops, *_ui_hops(document, button)]
        out.append(ctx.project(level_id=setting.level_id, chain=resolve_chain(hops, button)))
    return out


# ── 写入变换与入场动画 ─────────────────────────────────────────────────────


def _write_transform(
    document: UnityDocument,
    node: UnityNode,
    *,
    position: Vector3 | None = None,
    rotation: Quaternion | None = None,
    scale: Vector3 | None = None,
    raw: dict[str, Any] | None = None,
) -> None:
    transform = node_transform(document, node)
    if position is not None:
        transform.position = position
        transform.raw["m_LocalPosition"] = dict(zip("xyz", transform.position, strict=True))
    if rotation is not None:
        transform.rotation = quat_normalized(rotation)
    if scale is not None:
        transform.scale = scale
    if raw:
        transform.raw.update(raw)


def _apply_entry(provider: LocalDataProvider, asset: str, document: UnityDocument, root: UnityNode) -> None:
    """把入场动画的终值写到它绑定的节点上（覆盖 prefab 里的 TRS，不做相加）。"""
    curves = provider.get_animation(asset)
    for curve in curves:
        prop = curve.property
        if not prop.startswith(
            (
                "m_LocalPosition",
                "m_LocalScale",
                "m_LocalRotation",
                "localEulerAngles",
                "m_LocalEuler",
                "m_Anchored",
                "m_SizeDelta",
                "m_Anchor",
                "m_Pivot",
                "m_Offset",
            )
        ):
            # alpha 这类非几何轨道也会绑到动态 UI 实例上；本基线不据此推断解锁与可见性。
            continue
        value = curve.end_value()
        if value is None:
            continue
        path = root.path + ("/" + curve.path if curve.path else "")
        target = next((n for n in document.games.values() if n.path == path), None)
        if target is None:
            # 公共 clip 里也有绑到动态模型装饰上的轨道，它们不在按钮的父链上。
            if "/ModelNode/" in path:
                continue
            raise GenerationError(f"{asset}：动画绑定 '{path}' 找不到对应节点")
        transform = node_transform(document, target)
        if prop == "m_LocalRotation" and isinstance(value, tuple):
            _write_transform(document, target, rotation=value)
        elif prop.startswith(("m_LocalPosition.", "m_LocalScale.")):
            field, axis = prop.split(".")
            if not isinstance(value, (int, float)):
                raise GenerationError(f"{asset}：transform 的值不是数字")
            values = list(transform.position if field == "m_LocalPosition" else transform.scale)
            values["xyz".index(axis)] = float(value)
            if field == "m_LocalPosition":
                if transform.is_rect and axis != "z":
                    raise GenerationError(f"{asset}：Rect 的局部 XY 被动画改了，需要先换算布局")
                _write_transform(document, target, position=(values[0], values[1], values[2]))
            else:
                _write_transform(document, target, scale=(values[0], values[1], values[2]))
        elif prop.startswith(
            (
                "localEulerAngles",
                "m_LocalEuler",
                "m_LocalRotation.",
                "m_Anchored",
                "m_SizeDelta",
                "m_Anchor",
                "m_Pivot",
                "m_Offset",
            )
        ):
            raise GenerationError(f"{asset}：动画改的是 '{prop}'，需要单独写换算规则")


# ── 生成 ───────────────────────────────────────────────────────────────────


def build_snapshot(
    provider: LocalDataProvider, registry: RegionRegistry, options: GenerationOptions
) -> SnapshotContext:
    fov = extract_fov(provider)
    panel = deepcopy(provider.get_asset(registry.panel_path))
    ui_node = deepcopy(provider.get_asset(registry.ui_node_path))
    camera = provider.get_asset(registry.camera_path)
    root_ref = find_component(ui_node.root, "LuaReference")
    if root_ref is None:
        raise GenerationError("uinode prefab 的根上没有 LuaReference")
    canvas_root = resolve_reference(
        ui_node, dict_entries(root_ref.get("refDict")).get("worldUIRoot"), what="worldUIRoot Canvas"
    )
    if canvas_root is None:
        raise GenerationError("uinode 的 worldUIRoot Canvas 引用解不开")
    helper = find_component(canvas_root, "UICanvasScaleHelper")
    if helper is None or helper.get("isSingleCanvas") != 1:
        raise GenerationError("WorldUIRoot Canvas 的登记方式不支持")
    if helper.get("_useCustomStandardResolution", 0):
        raise GenerationError("自定义 Canvas 参考分辨率需要单独的换算规则")
    panel_root = panel.root
    initialize_panel(panel, ui_node, canvas_root)
    camera_setup = replace(
        _camera_setup(camera, fov),
        canvas_world_z=_canvas_world_z(ui_node, canvas_root),
        screen_height=float(options.reference_screen[1]),
    )
    _ = camera_view(camera_setup)
    return SnapshotContext(
        provider=provider,
        screen=(float(options.reference_screen[0]), float(options.reference_screen[1])),
        panel=panel,
        ui_node=ui_node,
        canvas_root=canvas_root,
        panel_root=panel_root,
        camera_setup=camera_setup,
    )


def generate(
    provider: LocalDataProvider, registry: RegionRegistry, options: GenerationOptions | None = None
) -> dict[str, Any]:
    """直接生成消费端要的 JSON；任一地区失败时不返回部分结果。"""
    if options is None:
        options = GenerationOptions()
    if not registry.regions:
        raise GenerationError("没有要生成的地区")
    names = _region_names(provider, registry)
    snapshot = build_snapshot(provider, registry, options)
    style = _panel_style(snapshot.panel, options.panel_style_state)
    canvas = canvas_scale(canvas_world_z=snapshot.camera_setup.canvas_world_z, fov_deg=snapshot.camera_setup.fov_deg)
    if canvas.canvas_scale <= 0:
        raise GenerationError("登记的 Canvas 世界深度为 0")
    _write_transform(
        snapshot.ui_node,
        snapshot.canvas_root,
        scale=(canvas.canvas_scale, canvas.canvas_scale, canvas.canvas_scale),
        raw={"m_SizeDelta": dict(zip("xy", canvas.reference_size, strict=True))},
    )
    target = next(n for n in snapshot.panel.games.values() if n.path == style.target_path)
    _write_transform(snapshot.panel, target, scale=style.scale)
    _apply_entry(provider, registry.panel_entry_animation, snapshot.panel, snapshot.panel_root)
    output: dict[str, Any] = {}
    ui_cache: dict[str, UnityDocument] = {}
    for region in registry.regions:
        try:
            region_document = deepcopy(provider.get_asset(region.region_prefab))
            mount_node = find_mount(snapshot.panel, region)
            for binding in region.entry_animations:
                animation_root = node_at_path(snapshot.panel, binding.root_path) if binding.root_path else mount_node
                _apply_entry(provider, binding.asset, snapshot.panel, animation_root)
            ui_root_path, settings = region_settings(region_document, region)
            context = ProjectionContext(snapshot, _panel_hops(snapshot, mount_node))
            levels = _level_candidates(context, region, region_document, ui_root_path, settings, ui_cache)
            polygons = {level.level_id: list(level.quad) for level in levels}
            clicks = derive_clicks(
                polygons,
                screen=snapshot.screen,
                min_clearance=options.min_click_clearance_px,
            )
            output[region.region_id] = {
                "name": names[region.region_id],
                "area": {
                    level_id: {"polygon": [list(p) for p in polygon], "click": list(clicks[level_id])}
                    for level_id, polygon in polygons.items()
                },
            }
        except (Region3dError, ValueError) as error:
            raise GenerationError(f"{region.region_id}：{error}") from error
    return output
