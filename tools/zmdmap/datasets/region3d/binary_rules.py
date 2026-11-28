"""客户端固定运行规则：标准分辨率、画布缩放与面板初始化。

这些数值和公式决定地区总览面板从 3D 世界到 720p 屏幕的换算；运行行为随版本变化时同步更新。
"""

import math
from dataclasses import dataclass
from typing import TYPE_CHECKING

from .geometry import quat_mul, quat_normalized
from .layout import ancestors, node_transform

if TYPE_CHECKING:
    from .geometry import Point
    from .unity import UnityDocument, UnityNode

# 客户端标准分辨率的宽高，与 PC 端使用的缩放系数：参考尺寸 = 标准尺寸 × 系数。
STANDARD_UI_RESOLUTION = (1920.0, 1080.0)
PC_RESOLUTION_SCALE = 1.25

# 面板样式状态里的两个筛选值：targetType=4 表示作用目标是 RectTransform，
# rectTransformImpactType=3 表示影响方式是缩放（匹配后读 rectTransform / rectTransformScale）。
STYLE_TARGET_RECT_TRANSFORM = 4
RECT_TRANSFORM_IMPACT_SCALE = 3


@dataclass(frozen=True, slots=True)
class CanvasScale:
    canvas_scale: float
    reference_size: Point


def canvas_scale(*, canvas_world_z: float, fov_deg: float) -> CanvasScale:
    """按登记 Canvas 的世界高度和 FOV 算画布缩放。

    世界高度 = 2 * |Z| * tan(FOV / 2)，缩放 = 世界高度 / 参考高度；Z 来自 uinode.prefab 的
    worldUIRoot 引用，FOV 来自 PhaseConfig.RegionMap。
    """
    reference = (
        STANDARD_UI_RESOLUTION[0] * PC_RESOLUTION_SCALE,
        STANDARD_UI_RESOLUTION[1] * PC_RESOLUTION_SCALE,
    )
    world_height = 2.0 * abs(canvas_world_z) * math.tan(math.radians(fov_deg / 2.0))
    return CanvasScale(world_height / reference[1], reference)


def initialize_panel(panel: UnityDocument, ui_node: UnityDocument, canvas_root: UnityNode) -> None:
    """把地区总览面板的根节点复位成铺满屏幕、无旋转的初始状态。

    位置归零、缩放归一、stretch anchors、居中 pivot、尺寸与偏移清零；局部旋转取父链世界旋转的逆四元数，
    抵消父节点带来的朝向。
    """
    transform = node_transform(panel, panel.root)
    transform.position = (0.0, 0.0, 0.0)
    transform.scale = (1.0, 1.0, 1.0)
    transform.raw.update(
        {
            "m_LocalPosition": {"x": 0, "y": 0, "z": 0},
            "m_AnchorMin": {"x": 0, "y": 0},
            "m_AnchorMax": {"x": 1, "y": 1},
            "m_Pivot": {"x": 0.5, "y": 0.5},
            "m_SizeDelta": {"x": 0, "y": 0},
            "m_AnchoredPosition": {"x": 0, "y": 0},
        }
    )
    rotation = (0.0, 0.0, 0.0, 1.0)
    for node in ancestors(ui_node, canvas_root):
        rotation = quat_mul(rotation, quat_normalized(node_transform(ui_node, node).rotation))
    transform.rotation = quat_normalized((-rotation[0], -rotation[1], -rotation[2], rotation[3]))
