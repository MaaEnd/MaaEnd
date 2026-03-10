from __future__ import annotations

import time
import tkinter as tk
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from PIL import Image, ImageTk

from model import resolve_zone_image


class MapRenderer:
    """
    地图底图渲染器。

    采用“快速预览 + 延时高清补帧”的策略，并只处理当前可见区域，
    以降低大图缩放/平移时的卡顿。
    """

    def __init__(self, canvas: tk.Canvas, root: tk.Tk, map_image_dir: Path) -> None:
        self.canvas = canvas
        self.root = root
        self.map_image_dir = map_image_dir

        self.full_map_cache: dict[str, Image.Image] = {}
        self.render_photo: ImageTk.PhotoImage | None = None
        self.bg_image_id: int | None = None

        self.executor = ThreadPoolExecutor(max_workers=1)
        self._last_request_time = 0.0
        self._hq_timer: str | None = None

        self.view_offset_x = 0.0
        self.view_offset_y = 0.0
        self.view_scale = 1.0

        self.last_params: tuple[str | None, float | None, float | None, float | None, bool | None] = (
            None,
            None,
            None,
            None,
            None,
        )

    def set_viewport(self, scale: float, off_x: float, off_y: float) -> None:
        self.view_scale = scale
        self.view_offset_x = off_x
        self.view_offset_y = off_y

    def world_to_canvas(self, world_x: float, world_y: float) -> tuple[float, float]:
        canvas_x = (world_x + self.view_offset_x) * self.view_scale
        canvas_y = (world_y + self.view_offset_y) * self.view_scale
        return canvas_x, canvas_y

    def canvas_to_world(self, canvas_x: float, canvas_y: float) -> tuple[float, float]:
        world_x = canvas_x / self.view_scale - self.view_offset_x
        world_y = canvas_y / self.view_scale - self.view_offset_y
        return world_x, world_y

    def _get_map_pil(self, zone_id: str) -> Image.Image | None:
        if not zone_id or zone_id == "None":
            return None
        if zone_id in self.full_map_cache:
            return self.full_map_cache[zone_id]

        image_path = resolve_zone_image(zone_id, self.map_image_dir)
        if not image_path or not image_path.exists():
            return None

        try:
            image = Image.open(image_path)
            self.full_map_cache[zone_id] = image
            return image
        except Exception as exc:
            print(f"Failed to load map image for {zone_id} at {image_path}: {exc}")
            return None

    def request_render(self, zone_id: str, fast: bool = True) -> None:
        """
        请求异步渲染：
        - `fast=True` 使用 Nearest 采样，优先保证拖拽流畅。
        - `fast=False` 使用 Lanczos 采样，作为视觉补帧。
        """
        if self._hq_timer:
            self.root.after_cancel(self._hq_timer)
            self._hq_timer = None

        request_time = time.time()
        self._last_request_time = request_time

        canvas_width = self.canvas.winfo_width()
        canvas_height = self.canvas.winfo_height()
        if canvas_width <= 1 or canvas_height <= 1:
            return

        params = (zone_id, self.view_scale, self.view_offset_x, self.view_offset_y, fast)
        if params == self.last_params:
            return

        self.executor.submit(self._async_render, zone_id, canvas_width, canvas_height, fast, request_time)
        if fast:
            self._hq_timer = self.root.after(150, lambda: self.request_render(zone_id, fast=False))

    def _async_render(
        self,
        zone_id: str,
        canvas_width: int,
        canvas_height: int,
        fast: bool,
        request_time: float,
    ) -> None:
        if request_time < self._last_request_time and fast:
            return

        image = self._get_map_pil(zone_id)
        if not image:
            self.root.after(0, self._clear_bg)
            return

        x0, y0 = self.canvas_to_world(0, 0)
        x1, y1 = self.canvas_to_world(canvas_width, canvas_height)

        image_width, image_height = image.size
        left = max(0, int(x0))
        top = max(0, int(y0))
        right = min(image_width, int(x1) + 1)
        bottom = min(image_height, int(y1) + 1)
        if right <= left or bottom <= top:
            self.root.after(0, self._clear_bg)
            return

        cropped = image.crop((left, top, right, bottom))
        target_width = int((right - left) * self.view_scale)
        target_height = int((bottom - top) * self.view_scale)
        if target_width <= 0 or target_height <= 0:
            return

        resample = Image.Resampling.NEAREST if fast else Image.Resampling.LANCZOS
        resized = cropped.resize((target_width, target_height), resample)
        canvas_x, canvas_y = self.world_to_canvas(left, top)

        self.root.after(
            0,
            self._apply_render_result,
            resized,
            canvas_x,
            canvas_y,
            zone_id,
            request_time,
            fast,
        )

    def _apply_render_result(
        self,
        pil_image: Image.Image,
        canvas_x: float,
        canvas_y: float,
        zone_id: str,
        request_time: float,
        fast: bool,
    ) -> None:
        if request_time < self._last_request_time and fast:
            return

        self.render_photo = ImageTk.PhotoImage(pil_image)
        if self.bg_image_id is None:
            self.bg_image_id = self.canvas.create_image(canvas_x, canvas_y, image=self.render_photo, anchor="nw")
        else:
            self.canvas.itemconfig(self.bg_image_id, image=self.render_photo)
            self.canvas.coords(self.bg_image_id, canvas_x, canvas_y)

        self.canvas.tag_lower(self.bg_image_id)
        self.last_params = (zone_id, self.view_scale, self.view_offset_x, self.view_offset_y, fast)

    def _clear_bg(self) -> None:
        if self.bg_image_id is not None:
            self.canvas.delete(self.bg_image_id)
            self.bg_image_id = None
        self.render_photo = None
        self.last_params = (None, None, None, None, None)
