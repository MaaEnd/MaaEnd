"""把 prefab 的层级、RectTransform 布局和父子链算成矩阵。

父链是按当前快照里真实的引用一层层走出来的，不假设固定形状::

    按钮 -> UI 实例根与中间节点 -> MapArea -> 地区 UINode
        -> 地区 prefab 根 -> 面板挂载点 -> MoveRoot -> ZoomRoot
        -> RegionRoot -> 面板根 -> WorldUIRoot -> 全局 UINode -> 世界

每一跳都是导出文件里真实存在的 transform。两件容易算错的事在这里明确处理：

- RectTransform 的 ``m_LocalPosition`` **不是**它屏幕上的位置，锚点和尺寸差才决定它落在哪。所以
  :func:`resolve_node_layout` 按 (位置, 尺寸) 一对往下传，父节点的尺寸和 pivot 顺着链传下去。
- ``m_LocalPosition.z`` 仍然有用（投影用的深度就是它），不会被布局算出来的 XY 顶掉。

算不出来就报 :class:`LayoutError`，不给默认值：拿一个零尺寸的 RectTransform 顶上缺失的父节点，会得到
看起来合理、其实全错的像素坐标。
"""

from dataclasses import dataclass
from typing import TYPE_CHECKING, Any

from .errors import LayoutError
from .geometry import (
    IDENTITY4,
    Matrix4,
    Point,
    Vector3,
    mat4_mul,
    quat_normalized,
    trs_matrix,
)
from .unity import (
    TransformData,
    UnityComponent,
    UnityDocument,
    UnityNode,
    as_reference,
    dict_entries,
    narrow_dict,
)

if TYPE_CHECKING:
    from .registry import RegionRegistration

REGION_SETTING_TYPE = "RegionMapSetting"
UI_CELL_TYPE = "UIRegionMap3DUICell"


@dataclass(slots=True)
class RectInfo:
    """一个 RectTransform 在父节点坐标系里解出来的矩形。"""

    width: float
    height: float
    pivot: Point
    anchor_min: Point
    anchor_max: Point
    size_delta: Point
    anchored_position: Point
    local_z: float


@dataclass(frozen=True, slots=True)
class ParentLayout:
    """节点锚点所依据的那块矩形（也就是父节点的）。

    ``is_rect=False`` 表示父节点是普通 Transform：锚点按零尺寸算，anchoredPosition 和 sizeDelta 照样生效，
    父节点的完整 TRS 也照样留在矩阵链里。
    """

    size: Point
    pivot: Point
    is_rect: bool = True


@dataclass(frozen=True, slots=True)
class RegionSetting:
    """地区 prefab 里的一张子地图：关卡 id 和它的 UI prefab 引用。"""

    level_id: str
    ui_path: str  # 逻辑路径，或地区 level3d 目录下的裸名字


@dataclass(slots=True)
class NodeFrame:
    """链条上一跳自己的局部矩阵，以及累加到世界坐标的矩阵。"""

    local_matrix: Matrix4
    world_matrix: Matrix4


@dataclass(frozen=True, slots=True)
class Chain:
    """一条完整变换链加上按钮自己的本地四角。"""

    frames: list[NodeFrame]
    local_corners: list[Vector3]


#: 链条的一跳：在哪份文档里、是哪个节点。
type Hop = tuple[UnityDocument, UnityNode]


# ── 查找 ───────────────────────────────────────────────────────────────────


def node_transform(document: UnityDocument, node: UnityNode) -> TransformData:
    transform = document.transforms.get(node.transform_path_id)
    if transform is None:
        raise LayoutError(f"{document.source_path.name}:{node.path or node.name} 没有 transform")
    return transform


def component_owner(document: UnityDocument, component_id: str) -> UnityNode | None:
    for node in document.games.values():
        if any(comp.path_id == component_id for comp in node.components):
            return node
    return None


def resolve_reference(document: UnityDocument, value: Any, *, what: str) -> UnityNode | None:
    """把 ``{"$path_id": n}`` 解成持有它的节点，无论这个 id 落在 GameObject、Transform 还是组件里。"""
    reference = as_reference(value)
    if reference is None:
        return None
    if reference.path_id in document.games:
        return document.games[reference.path_id]
    if reference.path_id in document.transforms:
        target = reference.path_id
        for node in document.games.values():
            if node.transform_path_id == target:
                return node
        raise LayoutError(f"{what}：transform {target} 不属于任何节点")
    if reference.path_id in document.components:
        owner = component_owner(document, reference.path_id)
        if owner is None:
            raise LayoutError(f"{what}：组件 {reference.path_id} 不属于任何节点")
        return owner
    return None


def ancestors(document: UnityDocument, node: UnityNode) -> list[UnityNode]:
    """从文档根到 *node* 的路径（根在前）。"""
    parents: dict[str, UnityNode] = {}
    for candidate in document.games.values():
        for child in candidate.children:
            parents[child.game_object_path_id] = candidate
    path: list[UnityNode] = [node]
    current = node
    seen: set[str] = set()
    while True:
        seen.add(current.game_object_path_id)
        parent = parents.get(current.game_object_path_id)
        if parent is None or parent.game_object_path_id in seen:
            break
        path.append(parent)
        current = parent
    return list(reversed(path))


def node_at_path(document: UnityDocument, path: str) -> UnityNode:
    current = document.root
    for part in [p for p in path.split("/") if p]:
        match = next((child for child in current.children if child.name == part), None)
        if match is None:
            raise LayoutError(f"{document.source_path.name}：路径 '{path}' 上没有节点")
        current = match
    return current


def find_component(node: UnityNode, type_name: str) -> UnityComponent | None:
    return node.component(type_name)


def find_nodes_with_component(document: UnityDocument, type_name: str) -> list[UnityNode]:
    return [node for node in document.games.values() if node.component(type_name) is not None]


# ── RectTransform 布局 ─────────────────────────────────────────────────────


def rect_info(transform: TransformData) -> RectInfo:
    raw = transform.raw

    def point2(value: Any, default: Point) -> Point:
        source = narrow_dict(value)
        return (float(source.get("x", default[0])), float(source.get("y", default[1])))

    return RectInfo(
        width=0.0,
        height=0.0,
        pivot=point2(raw.get("m_Pivot"), (0.5, 0.5)),
        anchor_min=point2(raw.get("m_AnchorMin"), (0.5, 0.5)),
        anchor_max=point2(raw.get("m_AnchorMax"), (0.5, 0.5)),
        size_delta=point2(raw.get("m_SizeDelta"), (0.0, 0.0)),
        anchored_position=point2(raw.get("m_AnchoredPosition"), (0.0, 0.0)),
        local_z=transform.position[2],
    )


def resolve_node_layout(document: UnityDocument, node: UnityNode, parent: ParentLayout) -> tuple[ParentLayout, Matrix4]:
    """给定父节点的矩形，解出这一跳的尺寸和它自己的局部矩阵。

    返回的矩阵是这一跳自己的 TRS（父矩阵由调用方乘），矩阵里带的位置是**布局**位置（锚点 + 尺寸差 +
    anchoredPosition），不是导出文件里的 ``m_LocalPosition.xy``——锚定过的 RectTransform 里它总是 0。
    """
    transform = node_transform(document, node)
    scale = transform.scale
    rotation = quat_normalized(transform.rotation)
    if not transform.is_rect:
        return (
            ParentLayout((0.0, 0.0), (0.5, 0.5), is_rect=False),
            trs_matrix(transform.position, rotation, scale),
        )

    rect = rect_info(transform)
    parent_size = parent.size if parent.is_rect else (0.0, 0.0)
    width = (rect.anchor_max[0] - rect.anchor_min[0]) * parent_size[0] + rect.size_delta[0]
    height = (rect.anchor_max[1] - rect.anchor_min[1]) * parent_size[1] + rect.size_delta[1]
    # 父 Rect 尺寸为零不会消掉 anchoredPosition 和固定的 sizeDelta。
    xy = layout_position(rect, parent)
    matrix = trs_matrix((xy[0], xy[1], rect.local_z), rotation, scale)
    return ParentLayout((width, height), rect.pivot), matrix


def layout_position(rect: RectInfo, parent: ParentLayout) -> Point:
    """节点落在父矩形里的位置。"""
    parent_size = parent.size if parent.is_rect else (0.0, 0.0)
    parent_pivot = parent.pivot if parent.is_rect else (0.5, 0.5)
    x = (
        -parent_pivot[0] + rect.anchor_min[0] + (rect.anchor_max[0] - rect.anchor_min[0]) * rect.pivot[0]
    ) * parent_size[0] + rect.anchored_position[0]
    y = (
        -parent_pivot[1] + rect.anchor_min[1] + (rect.anchor_max[1] - rect.anchor_min[1]) * rect.pivot[1]
    ) * parent_size[1] + rect.anchored_position[1]
    return (x, y)


def local_corners(layout: ParentLayout) -> list[Vector3]:
    """一块已解出的矩形在它自己坐标系里的四角（pivot 在原点），左上优先。

    依次是 (左,上) (右,上) (右,下) (左,下)。
    """
    left, right = -layout.pivot[0] * layout.size[0], (1 - layout.pivot[0]) * layout.size[0]
    bottom, top = -layout.pivot[1] * layout.size[1], (1 - layout.pivot[1]) * layout.size[1]
    return [
        (left, top, 0.0),
        (right, top, 0.0),
        (right, bottom, 0.0),
        (left, bottom, 0.0),
    ]


# ── 地区设置与挂载点 ───────────────────────────────────────────────────────


def region_settings(document: UnityDocument, region: RegionRegistration) -> tuple[str, list[RegionSetting]]:
    """从地区 prefab 的 ``RegionMapSetting._cfg`` 读出 ``(uiRoot 路径, 子地图列表)``。

    按 id 排序枚举，既不硬编码，也不去猜 ``regionNN`` 的编号范围。
    """
    settings_nodes = find_nodes_with_component(document, REGION_SETTING_TYPE)
    if not settings_nodes:
        raise LayoutError(f"{region.region_id}：地区 prefab 里没有 {REGION_SETTING_TYPE} 组件")
    setting_node = settings_nodes[0]
    component = find_component(setting_node, REGION_SETTING_TYPE)
    if component is None:  # pragma: no cover - 上面已经保证存在
        raise LayoutError(f"{region.region_id}：地区设置组件不见了")

    cfg = dict_entries(component.get("_cfg", {}))
    entries: list[RegionSetting] = []
    for level_id in sorted(cfg):
        entry = narrow_dict(cfg[level_id])
        ui: Any = entry.get("_ui")
        if not isinstance(ui, str) or not ui:
            raise LayoutError(f"{region.region_id}：_cfg[{level_id}] 没有 _ui 的 prefab 引用")
        entries.append(RegionSetting(level_id=str(level_id), ui_path=ui))
    if not entries:
        raise LayoutError(f"{region.region_id}：_cfg 是空的")

    ui_root_node = resolve_reference(
        document, component.get("uiRoot"), what=f"{region.region_id}：RegionMapSetting.uiRoot"
    )
    if ui_root_node is None:
        raise LayoutError(f"{region.region_id}：RegionMapSetting.uiRoot 没设置，MapArea 的父节点无从确定")
    return ui_root_node.path, entries


def find_mount(panel: UnityDocument, region: RegionRegistration) -> UnityNode:
    """顺着面板上的 LuaReference 找到这个地区实例化的位置。"""
    target = panel.root
    for key in region.mount_keys:
        component = find_component(target, "LuaReference")
        entries = dict_entries(component.get("refDict")) if component else {}
        resolved = resolve_reference(panel, entries.get(key), what=f"{region.region_id}：挂载键 '{key}'")
        if resolved is None:
            raise LayoutError(f"{region.region_id}：挂载引用 '{key}' 解不开")
        target = resolved
    return target


# ── 拼链 ───────────────────────────────────────────────────────────────────


def compose_chain(hops: list[Hop]) -> tuple[list[NodeFrame], ParentLayout]:
    """把一串跳（根在前）乘成世界矩阵，顺带返回链条末端解出来的矩形。

    每跳都按完整局部 TRS 相乘；Canvas 的运行时 setter 由调用方先写进对应节点。普通 Transform 一样继承父
    缩放，Rect 的布局平移只算一次。
    """
    frames: list[NodeFrame] = []
    world = IDENTITY4
    layout = ParentLayout((0.0, 0.0), (0.5, 0.5), is_rect=False)
    for document, node in hops:
        layout, matrix = resolve_node_layout(document, node, layout)
        world = mat4_mul(world, matrix)
        frames.append(NodeFrame(local_matrix=matrix, world_matrix=world))
    return frames, layout


def resolve_chain(hops: list[Hop], btn: UnityNode) -> Chain:
    """一串跳的矩阵，加上按钮自己的本地四角。"""
    if not hops:
        raise LayoutError("变换链为空")
    document, node = hops[-1]
    if node.game_object_path_id != btn.game_object_path_id:
        raise LayoutError(f"链尾 '{node.path}' 不是按钮 '{btn.path}'")
    if not node_transform(document, node).is_rect:
        raise LayoutError(f"按钮 '{node.path}' 没有 RectTransform")
    frames, tip = compose_chain(hops)
    return Chain(frames=frames, local_corners=local_corners(tip))
