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
        self._last_request_seq = 0
        self._hq_timer: str | None = None

        self.view_offset_x = 0.0
        self.view_offset_y = 0.0
        self.view_scale = 1.0

        self.last_params: tuple[
            str | None, float | None, float | None, float | None, bool | None, float | None
        ] = (
            None,
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

    def reset_view(self, clear_cache: bool = False) -> None:
        if self._hq_timer:
            self.root.after_cancel(self._hq_timer)
            self._hq_timer = None
        self._last_request_seq += 1
        if clear_cache:
            self.full_map_cache.clear()
        self._clear_bg()

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

    def request_render(self, zone_id: str, fast: bool = True, margin_fraction: float = 0.0) -> None:
        """
        请求异步渲染：
        - `fast=True` 使用 Nearest 采样，优先保证拖拽流畅。
        - `fast=False` 使用 Lanczos 采样，作为视觉补帧。
        - `margin_fraction` 在可视区四周多渲染一圈底图（按可视区比例），
          供拖拽时画布整体位移滑入，避免露出空白边。
        """
        if self._hq_timer:
            self.root.after_cancel(self._hq_timer)
            self._hq_timer = None

        request_time = time.time()
        self._last_request_time = request_time
        self._last_request_seq += 1
        request_seq = self._last_request_seq

        canvas_width = self.canvas.winfo_width()
        canvas_height = self.canvas.winfo_height()
        if canvas_width <= 1 or canvas_height <= 1:
            return

        params = (zone_id, self.view_scale, self.view_offset_x, self.view_offset_y, fast, margin_fraction)
        if params == self.last_params:
            return

        viewport = (self.view_scale, self.view_offset_x, self.view_offset_y)
        self.executor.submit(
            self._async_render,
            zone_id,
            canvas_width,
            canvas_height,
            fast,
            request_time,
            request_seq,
            viewport,
            margin_fraction,
        )
        if fast:
            self._hq_timer = self.root.after(
                150, lambda: self.request_render(zone_id, fast=False, margin_fraction=margin_fraction)
            )

    def _async_render(
        self,
        zone_id: str,
        canvas_width: int,
        canvas_height: int,
        fast: bool,
        request_time: float,
        request_seq: int,
        viewport: tuple[float, float, float],
        margin_fraction: float = 0.0,
    ) -> None:
        if request_seq != self._last_request_seq:
            return

        image = self._get_map_pil(zone_id)
        if not image:
            self.root.after(0, self._clear_bg_if_current, request_seq, request_time, viewport)
            return

        view_scale, view_offset_x, view_offset_y = viewport
        margin_x = canvas_width * margin_fraction / view_scale
        margin_y = canvas_height * margin_fraction / view_scale
        x0 = 0 / view_scale - view_offset_x - margin_x
        y0 = 0 / view_scale - view_offset_y - margin_y
        x1 = canvas_width / view_scale - view_offset_x + margin_x
        y1 = canvas_height / view_scale - view_offset_y + margin_y

        image_width, image_height = image.size
        left = max(0, int(x0))
        top = max(0, int(y0))
        right = min(image_width, int(x1) + 1)
        bottom = min(image_height, int(y1) + 1)
        if right <= left or bottom <= top:
            self.root.after(0, self._clear_bg_if_current, request_seq, request_time, viewport)
            return

        cropped = image.crop((left, top, right, bottom))
        target_width = int((right - left) * view_scale)
        target_height = int((bottom - top) * view_scale)
        if target_width <= 0 or target_height <= 0:
            return

        resample = Image.Resampling.NEAREST if fast else Image.Resampling.LANCZOS
        resized = cropped.resize((target_width, target_height), resample)
        canvas_x = (left + view_offset_x) * view_scale
        canvas_y = (top + view_offset_y) * view_scale

        self.root.after(
            0,
            self._apply_render_result,
            resized,
            canvas_x,
            canvas_y,
            zone_id,
            request_time,
            request_seq,
            viewport,
            fast,
            margin_fraction,
        )

    def _apply_render_result(
        self,
        pil_image: Image.Image,
        canvas_x: float,
        canvas_y: float,
        zone_id: str,
        request_time: float,
        request_seq: int,
        viewport: tuple[float, float, float],
        fast: bool,
        margin_fraction: float = 0.0,
    ) -> None:
        if request_seq != self._last_request_seq or request_time < self._last_request_time:
            return
        if viewport != (self.view_scale, self.view_offset_x, self.view_offset_y):
            return

        self.render_photo = ImageTk.PhotoImage(pil_image)
        if self.bg_image_id is None:
            self.bg_image_id = self.canvas.create_image(canvas_x, canvas_y, image=self.render_photo, anchor="nw")
        else:
            self.canvas.itemconfig(self.bg_image_id, image=self.render_photo)
            self.canvas.coords(self.bg_image_id, canvas_x, canvas_y)

        self.canvas.tag_lower(self.bg_image_id)
        self.last_params = (zone_id, viewport[0], viewport[1], viewport[2], fast, margin_fraction)

    def _clear_bg(self) -> None:
        if self.bg_image_id is not None:
            self.canvas.delete(self.bg_image_id)
            self.bg_image_id = None
        self.render_photo = None
        self.last_params = (None, None, None, None, None, None)

    def _clear_bg_if_current(
        self,
        request_seq: int,
        request_time: float,
        viewport: tuple[float, float, float],
    ) -> None:
        if request_seq != self._last_request_seq or request_time < self._last_request_time:
            return
        if viewport != (self.view_scale, self.view_offset_x, self.view_offset_y):
            return
        self._clear_bg()
