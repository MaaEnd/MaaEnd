"""按当前相机、Canvas 和完整父链，把按钮四角投到屏幕上。"""

import math
from dataclasses import dataclass
from typing import TYPE_CHECKING

from .errors import GenerationError
from .geometry import (
    Matrix4,
    Point,
    mat4_inverse_rigid,
    mat4_mul,
    perspective_project,
    transform_point,
)

if TYPE_CHECKING:
    from .layout import Chain


@dataclass(frozen=True, slots=True)
class CameraSetup:
    world_matrix: Matrix4
    fov_deg: float
    near_clip: float | None
    orthographic: bool
    lens_shift: Point
    # 平台样式应用之后登记 Canvas 的世界 Z。放在相机上，因为它是同一次投影决定的另一半。
    canvas_world_z: float = 0.0
    screen_height: float = 720.0

    @property
    def focal_y(self) -> float:
        """深度为 1 处、每世界单位对应多少像素。

        视口高度减半则同 FOV 下这个值也减半；Canvas 的**缩放**是另一回事，不受它影响。
        """
        return focal_length_y(self.fov_deg, self.screen_height)


def focal_length_y(fov_deg: float, screen_height: float = 720.0) -> float:
    """整高视口下每世界单位对应的像素数。"""
    if not 0.0 < fov_deg < 180.0:
        raise GenerationError(f"fov {fov_deg!r} 不是合法的视角")
    return screen_height / (2.0 * math.tan(math.radians(fov_deg / 2.0)))


def camera_view(camera: CameraSetup) -> Matrix4:
    if camera.orthographic:
        raise GenerationError("不支持正交相机")
    if camera.lens_shift != (0.0, 0.0):
        raise GenerationError(f"不支持镜头偏移 {camera.lens_shift!r}（只支持完整视口）")
    return mat4_inverse_rigid(camera.world_matrix)


def projected_quad(chain: Chain, camera: CameraSetup, *, screen: Point) -> tuple[Point, Point, Point, Point]:
    if not chain.frames:
        raise GenerationError("变换链为空")
    # Canvas 缩放已经写进真实节点，这里沿局部矩阵连乘，避免 Canvas 的平移被重复缩放。
    world = camera_view(camera)
    for frame in chain.frames:
        world = mat4_mul(world, frame.local_matrix)
    quad: list[Point] = []
    focal = camera.focal_y
    for corner in chain.local_corners:
        camera_point = transform_point(world, corner)
        if camera.near_clip is not None and camera_point[2] < camera.near_clip:
            raise GenerationError(f"角落穿过了近裁剪面（z={camera_point[2]!r} < {camera.near_clip!r}）")
        u, v, _ = perspective_project(camera_point, screen=screen, focal_y=focal)
        quad.append((u, v))
    return quad[0], quad[1], quad[2], quad[3]
