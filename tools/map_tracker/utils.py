import sys
import os
import re
import math
import time
from typing import Literal, NamedTuple

_R = "\033[31m"
_G = "\033[32m"
_Y = "\033[33m"
_C = "\033[36m"
_A = "\033[90m"
_0 = "\033[0m"

try:
    import numpy as np
except ImportError:
    print(f"{_R}Cannot import 'numpy'!{_0}")
    print(f"  Please run 'pip install numpy' first.")
    sys.exit(1)

try:
    import cv2
except ImportError:
    print(f"{_R}Cannot import 'opencv-python'!{_0}")
    print(f"  Please run 'pip install opencv-python' first.")
    sys.exit(1)


Point = tuple[int, int]
Color = int  # 0xRRGGBB


MapType = Literal["normal", "tier", "base", "dung"]


class MapName:
    """Parser for MapTracker map names.

    Supports parsing map file path or file name, with or without extension.
    Raises ValueError if the input does not match a known map naming format.
    """

    __slots__ = (
        "_map_id",
        "_map_level_id",
        "_map_type",
        "_tile_x",
        "_tile_y",
        "_tier_suffix",
    )

    def __init__(
        self,
        map_id: str,
        map_level_id: str,
        map_type: MapType,
        tile_x: int | None = None,
        tile_y: int | None = None,
        tier_suffix: str | None = None,
    ):
        self._map_id = map_id
        self._map_level_id = map_level_id
        self._map_type = map_type
        self._tile_x = tile_x
        self._tile_y = tile_y
        self._tier_suffix = tier_suffix

    @property
    def map_id(self) -> str:
        return self._map_id

    @property
    def map_level_id(self) -> str:
        return self._map_level_id

    @property
    def map_type(self) -> MapType:
        return self._map_type

    @property
    def tile_x(self) -> int | None:
        return self._tile_x

    @property
    def tile_y(self) -> int | None:
        return self._tile_y

    @property
    def tier_suffix(self) -> str | None:
        return self._tier_suffix

    @property
    def map_full_name(self) -> str:
        if self._map_type == "tier":
            if not self._tier_suffix:
                raise ValueError("tier map requires tier suffix")
            return f"{self._map_id}_{self._map_level_id}_tier_{self._tier_suffix}.png"
        return f"{self._map_id}_{self._map_level_id}.png"

    @staticmethod
    def parse(name_or_path: str, is_tile: bool = False) -> "MapName":
        if not isinstance(name_or_path, str):
            raise ValueError("map name must be a string")

        raw = name_or_path.strip()
        if raw == "":
            raise ValueError("map name cannot be empty")

        # Compatible with both '/' and '\\' separators.
        basename = os.path.basename(raw.replace("\\", "/"))
        stem, _ = os.path.splitext(basename)
        name = stem.lower()

        tile_m = re.match(
            r"^(?P<kind>map|base|dung)(?P<map>\d+)_lv(?P<lv>\d+)_(?P<x>\d+)_(?P<y>\d+)(?:_tier_(?P<tier>[a-z0-9_]+))?$",
            name,
        )
        merged_m = re.match(
            r"^(?P<kind>map|base|dung)(?P<map>\d+)_lv(?P<lv>\d+)(?:_tier_(?P<tier>[a-z0-9_]+))?$",
            name,
        )

        if is_tile:
            if not tile_m:
                raise ValueError(f"expected tile map name format: {name_or_path}")
            m = tile_m
        else:
            if not merged_m:
                raise ValueError(f"expected non-tile map name format: {name_or_path}")
            m = merged_m

        kind = m.group("kind")
        map_id = f"{kind}{m.group('map')}"
        map_level_id = f"lv{m.group('lv')}"
        map_type: MapType
        tier_suffix = m.group("tier")
        if tier_suffix is not None:
            map_type = "tier"
        elif kind == "map":
            map_type = "normal"
        elif kind == "base":
            map_type = "base"
        else:
            map_type = "dung"
        tile_x = int(m.group("x")) if is_tile else None
        tile_y = int(m.group("y")) if is_tile else None
        return MapName(
            map_id=map_id,
            map_level_id=map_level_id,
            map_type=map_type,
            tile_x=tile_x,
            tile_y=tile_y,
            tier_suffix=tier_suffix,
        )


class Drawer:
    def __init__(self, img: cv2.Mat, font_face: int = cv2.FONT_HERSHEY_SIMPLEX):
        self._img = img
        self._font_face = font_face

    @property
    def w(self):
        return self._img.shape[1]

    @property
    def h(self):
        return self._img.shape[0]

    def get_image(self):
        return self._img

    def get_text_size(self, text: str, font_scale: float, *, thickness: int):
        return cv2.getTextSize(text, self._font_face, font_scale, thickness)[0]

    @staticmethod
    def _to_bgr(color: Color) -> tuple[int, int, int]:
        r = (color >> 16) & 0xFF
        g = (color >> 8) & 0xFF
        b = color & 0xFF
        return (b, g, r)

    def text(
        self,
        text: str,
        pos: Point,
        font_scale: float,
        *,
        color: Color,
        thickness: int,
        bg_color: Color | None = None,
        bg_padding: int = 5,
    ):
        if bg_color is not None:
            text_size = self.get_text_size(text, font_scale, thickness=thickness)
            cv2.rectangle(
                self._img,
                (pos[0] - bg_padding, pos[1] - text_size[1] - bg_padding),
                (pos[0] + text_size[0] + bg_padding, pos[1] + bg_padding),
                self._to_bgr(bg_color),
                -1,
            )
        cv2.putText(
            self._img,
            text,
            pos,
            self._font_face,
            font_scale,
            self._to_bgr(color),
            thickness,
        )

    def text_centered(
        self, text: str, pos: Point, font_scale: float, *, color: Color, thickness: int
    ):
        text_size = self.get_text_size(text, font_scale, thickness=thickness)
        x = pos[0] - text_size[0] // 2
        self.text(
            text, (int(x), int(pos[1])), font_scale, color=color, thickness=thickness
        )

    def rect(self, pt1: Point, pt2: Point, *, color: Color, thickness: int):
        cv2.rectangle(self._img, pt1, pt2, self._to_bgr(color), thickness)

    def circle(self, center: Point, radius: int, *, color: Color, thickness: int):
        cv2.circle(self._img, center, radius, self._to_bgr(color), thickness)

    def line(self, pt1: Point, pt2: Point, *, color: Color, thickness: int):
        cv2.line(self._img, pt1, pt2, self._to_bgr(color), thickness)

    def mask(self, pt1: Point, pt2: Point, *, color: Color, alpha: float) -> None:
        x1, y1 = pt1
        x2, y2 = pt2
        if x1 == x2 or y1 == y2:
            return
        if x1 > x2:
            x1, x2 = x2, x1
        if y1 > y2:
            y1, y2 = y2, y1
        h, w = self._img.shape[:2]
        x1 = max(0, min(w, x1))
        x2 = max(0, min(w, x2))
        y1 = max(0, min(h, y1))
        y2 = max(0, min(h, y2))
        if x2 <= x1 or y2 <= y1:
            return

        region = self._img[y1:y2, x1:x2]
        overlay = np.empty_like(region)
        overlay[:, :] = self._to_bgr(color)
        cv2.addWeighted(region, 1 - alpha, overlay, alpha, 0, dst=region)

    def paste(
        self,
        img: np.ndarray,
        pos: Point,
        *,
        scale_w: int | None = None,
        scale_h: int | None = None,
        with_alpha: bool = False,
    ) -> None:
        # Scale if needed
        if scale_w is not None or scale_h is not None:
            h, w = img.shape[:2]
            new_w = scale_w if scale_w is not None else w
            new_h = scale_h if scale_h is not None else h
            img = cv2.resize(img, (new_w, new_h), interpolation=cv2.INTER_LINEAR)

        x, y = pos
        fh, fw = img.shape[:2]
        bh, bw = self._img.shape[:2]

        # Clamp to canvas bounds
        x0, y0 = max(0, x), max(0, y)
        x1, y1 = min(bw, x + fw), min(bh, y + fh)

        if x1 <= x0 or y1 <= y0:
            return

        # Extract regions
        target_bg = self._img[y0:y1, x0:x1]
        fx0, fy0 = x0 - x, y0 - y
        fx1, fy1 = fx0 + (x1 - x0), fy0 + (y1 - y0)
        target_fg = img[fy0:fy1, fx0:fx1]

        if with_alpha and img.shape[2] == 4:
            # Alpha blending when alpha channel exists
            alpha_fg = target_fg[:, :, 3:4].astype(np.float32) / 255.0
            alpha_bg = (
                target_bg[:, :, 3:4].astype(np.float32) / 255.0
                if target_bg.shape[2] == 4
                else np.ones_like(alpha_fg)
            )

            out_alpha = alpha_fg + alpha_bg * (1.0 - alpha_fg)
            mask = out_alpha > 0
            res_rgb = np.zeros_like(target_bg[:, :, :3], dtype=np.float32)

            rgb_fg = target_fg[:, :, :3].astype(np.float32)
            rgb_bg = target_bg[:, :, :3].astype(np.float32)

            m_idx = mask[:, :, 0]
            res_rgb[m_idx] = (
                rgb_fg[m_idx] * alpha_fg[m_idx]
                + rgb_bg[m_idx] * alpha_bg[m_idx] * (1.0 - alpha_fg[m_idx])
            ) / out_alpha[m_idx]

            res_bgra = np.zeros_like(target_bg, dtype=np.uint8)
            res_bgra[:, :, :3] = np.clip(res_rgb, 0, 255).astype(np.uint8)
            if target_bg.shape[2] == 4:
                res_bgra[:, :, 3:4] = np.clip(out_alpha * 255.0, 0, 255).astype(
                    np.uint8
                )

            self._img[y0:y1, x0:x1] = res_bgra
        else:
            # Simple paste without alpha blending
            self._img[y0:y1, x0:x1] = target_fg

    @staticmethod
    def new(w: int, h: int, **kwargs) -> "Drawer":
        img = np.zeros((h, w, 3), dtype=np.uint8)
        return Drawer(img, **kwargs)


class ViewportManager:
    ZOOM_STEP = 1.14514

    def __init__(
        self,
        vw: int,
        vh: int,
        *,
        zoom: float = 1.0,
        min_zoom: float = 0.5,
        max_zoom: float = 10.0,
        vx: float = 0.0,
        vy: float = 0.0,
    ):
        self._vw = vw
        self._vh = vh
        self._zoom = zoom
        self._min_zoom = min_zoom
        self._max_zoom = max_zoom
        self._vx = vx
        self._vy = vy

    @property
    def zoom(self) -> float:
        return self._zoom

    @zoom.setter
    def zoom(self, value: float) -> None:
        self._zoom = max(self._min_zoom, min(self._max_zoom, value))

    def get_real_coords(self, view_x: int, view_y: int) -> tuple[float, float]:
        rx = view_x / self._zoom + self._vx
        ry = view_y / self._zoom + self._vy
        return rx, ry

    def get_view_coords(self, real_x: int, real_y: int) -> tuple[int, int]:
        vx = round((real_x - self._vx) * self._zoom)
        vy = round((real_y - self._vy) * self._zoom)
        return vx, vy

    def zoom_in(self) -> None:
        self.zoom = self._zoom * self.ZOOM_STEP

    def zoom_out(self) -> None:
        self.zoom = self._zoom / self.ZOOM_STEP

    def set_view_origin(self, vx: float, vy: float) -> None:
        self._vx = vx
        self._vy = vy

    def pan_by(self, dx: float, dy: float) -> None:
        self._vx += dx
        self._vy += dy

    def maybe_center_to(self, real_x: int, real_y: int, padding: float = 0.3) -> None:
        padding = max(0.0, min(0.49, padding))
        view_w = self._vw / self._zoom
        view_h = self._vh / self._zoom
        pad_w = view_w * padding
        pad_h = view_h * padding
        left = self._vx + pad_w
        right = self._vx + view_w - pad_w
        top = self._vy + pad_h
        bottom = self._vy + view_h - pad_h
        if left <= real_x <= right and top <= real_y <= bottom:
            return
        self._vx = real_x - view_w / 2.0
        self._vy = real_y - view_h / 2.0

    def fit_to(self, real_points: list[Point], padding: float = 0.3) -> None:
        if not real_points:
            return
        min_x = min(p[0] for p in real_points)
        max_x = max(p[0] for p in real_points)
        min_y = min(p[1] for p in real_points)
        max_y = max(p[1] for p in real_points)
        span_x = max(1.0, float(max_x - min_x))
        span_y = max(1.0, float(max_y - min_y))

        padding = max(0.0, min(0.49, padding))
        fit_w = max(1.0, self._vw * (1.0 - 2.0 * padding))
        fit_h = max(1.0, self._vh * (1.0 - 2.0 * padding))
        target_zoom = min(fit_w / span_x, fit_h / span_y)
        self.zoom = target_zoom

        view_w = self._vw / self._zoom
        view_h = self._vh / self._zoom
        center_x = (min_x + max_x) / 2.0
        center_y = (min_y + max_y) / 2.0
        self._vx = center_x - view_w / 2.0
        self._vy = center_y - view_h / 2.0


class Layer:
    def __init__(self, view: ViewportManager):
        self.view = view

    def render(self, drawer: "Drawer") -> None:
        return None


class MapImageLayer(Layer):
    """Renders a background map image with viewport zoom/pan support."""

    def __init__(self, view: ViewportManager, img: np.ndarray):
        super().__init__(view)
        self._img = img
        self._scaled_img: np.ndarray | None = None
        self._scaled_zoom: float | None = None

    def render(self, drawer: Drawer) -> None:
        zoom = self.view.zoom
        if self._scaled_img is None or self._scaled_zoom != zoom:
            scaled_w = max(1, int(self._img.shape[1] * zoom))
            scaled_h = max(1, int(self._img.shape[0] * zoom))
            self._scaled_img = cv2.resize(
                self._img, (scaled_w, scaled_h), interpolation=cv2.INTER_AREA
            )
            self._scaled_zoom = zoom

        scaled_img = self._scaled_img
        if scaled_img is None:
            return

        scaled_h, scaled_w = scaled_img.shape[:2]
        src_x1 = int(round(self.view._vx * zoom))
        src_y1 = int(round(self.view._vy * zoom))
        dst_x = max(0, -src_x1)
        dst_y = max(0, -src_y1)
        src_x1 = max(0, src_x1)
        src_y1 = max(0, src_y1)
        src_x2 = min(scaled_w, src_x1 + drawer.w - dst_x)
        src_y2 = min(scaled_h, src_y1 + drawer.h - dst_y)
        copy_w = src_x2 - src_x1
        copy_h = src_y2 - src_y1
        if copy_w > 0 and copy_h > 0:
            drawer.get_image()[dst_y : dst_y + copy_h, dst_x : dst_x + copy_w] = (
                scaled_img[src_y1:src_y2, src_x1:src_x2]
            )


class StatusRecord(NamedTuple):
    """Generic status bar record."""

    timestamp: float
    color: Color
    message: str


class BasePage:
    """Base class for map-based editing pages.

    Provides: viewport/zoom/pan, FPS-limited rendering, status bar, sidebar
    scaffold, and a cv2 main-loop skeleton.

    Subclasses override:
      - ``_render_content(drawer)``  – draw layers on top of the map image.
      - ``_render_ui(drawer)``       – draw UI panels (status bar, sidebar, …).
      - ``_render_sidebar(drawer)``  – sidebar content (calls ``_render_sidebar_bg``).
      - ``_on_mouse(event, x, y, mx, my, flags)`` – custom mouse handling.
      - ``_on_key(key)``             – custom keyboard handling.
      - ``_on_idle()``               – called each loop iteration for background tasks.
    """

    SIDEBAR_W: int = 240
    STATUS_BAR_H: int = 32

    def __init__(
        self,
        map_path: str,
        window_name: str,
        window_w: int = 1280,
        window_h: int = 720,
        welcome_msg: str = "Welcome!",
    ) -> None:
        self.img = cv2.imread(map_path)
        if self.img is None:
            raise ValueError(f"Cannot load map image: {map_path}")
        self.window_w = window_w
        self.window_h = window_h
        self.window_name = window_name
        self.view = ViewportManager(
            window_w, window_h, zoom=1.0, min_zoom=0.5, max_zoom=10.0
        )
        self._map_layer = MapImageLayer(self.view, self.img)
        self._status = StatusRecord(0.0, 0xFFFFFF, welcome_msg)
        self.mouse_pos: tuple[int, int] = (-1, -1)
        self._frame_interval = 1.0 / 120.0
        self._last_render_ts = 0.0
        self.panning = False
        self.pan_start: tuple[int, int] = (0, 0)
        self.done = False

    def _update_status(self, color: Color, message: str) -> None:
        self._status = StatusRecord(time.time(), color, message)

    @staticmethod
    def _hit_button(x: int, y: int, rect) -> bool:
        if rect is None:
            return False
        x1, y1, x2, y2 = rect
        return x1 <= x <= x2 and y1 <= y <= y2

    def render_page(self, *, force: bool = False) -> None:
        now = time.monotonic()
        if force or now - self._last_render_ts >= self._frame_interval:
            self._last_render_ts = now
            self._render()

    def _render(self) -> None:
        drawer = Drawer.new(self.window_w, self.window_h)
        self._map_layer.render(drawer)
        self._render_content(drawer)
        # Crosshair
        drawer.line(
            (self.mouse_pos[0], 0),
            (self.mouse_pos[0], self.window_h),
            color=0xFFFF00,
            thickness=1,
        )
        drawer.line(
            (0, self.mouse_pos[1]),
            (self.window_w, self.mouse_pos[1]),
            color=0xFFFF00,
            thickness=1,
        )
        self._render_ui(drawer)
        cv2.imshow(self.window_name, drawer.get_image())

    def _render_content(self, drawer: Drawer) -> None:
        """Override to draw custom layers/content on top of the map."""

    def _render_ui(self, drawer: Drawer) -> None:
        """Override to draw UI panels. Default: status bar + sidebar."""
        self._render_status_bar(drawer)
        self._render_sidebar(drawer)

    def _render_status_bar(self, drawer: Drawer) -> None:
        x1 = self.SIDEBAR_W
        x2 = self.window_w
        y2 = self.window_h
        y1 = max(0, y2 - self.STATUS_BAR_H)
        drawer.rect((x1, y1), (x2, y2), color=0x000000, thickness=-1)
        if self._status:
            drawer.text(
                self._status.message,
                (x1 + 10, y2 - 10),
                0.45,
                color=self._status.color,
                thickness=1,
            )

    def _render_sidebar(self, drawer: Drawer) -> None:
        """Override to draw sidebar content. Default draws background only."""
        self._render_sidebar_bg(drawer)

    def _render_sidebar_bg(self, drawer: Drawer) -> None:
        sw = self.SIDEBAR_W
        h = self.window_h
        drawer.rect((0, 0), (sw, h), color=0x000000, thickness=-1)
        drawer.line((sw - 1, 0), (sw - 1, h), color=0xFFFFFF, thickness=1)

    def _handle_mouse(self, event, x: int, y: int, flags, param) -> None:
        self.mouse_pos = (x, y)
        mx, my = self.view.get_real_coords(x, y)

        if event == cv2.EVENT_MOUSEWHEEL:
            if flags > 0:
                self.view.zoom_in()
            else:
                self.view.zoom_out()
            self.view.set_view_origin(mx - x / self.view.zoom, my - y / self.view.zoom)
            self.render_page()
            return

        if event == cv2.EVENT_RBUTTONDOWN:
            if x >= self.SIDEBAR_W:
                self.panning = True
                self.pan_start = (x, y)
            return

        if event == cv2.EVENT_RBUTTONUP:
            self.panning = False
            return

        if event == cv2.EVENT_MOUSEMOVE and self.panning:
            dx = (x - self.pan_start[0]) / self.view.zoom
            dy = (y - self.pan_start[1]) / self.view.zoom
            self.view.pan_by(-dx, -dy)
            self.pan_start = (x, y)
            self.render_page()
            return

        self._on_mouse(event, x, y, mx, my, flags)
        self.render_page()

    def _on_mouse(self, event, x: int, y: int, mx: int, my: int, flags) -> None:
        """Override to handle custom mouse events (excludes scroll and right-drag pan)."""

    def _on_key(self, key: int) -> None:
        """Override to handle keyboard input."""

    def _on_idle(self) -> None:
        """Override for background tasks called each loop iteration."""

    def run(self) -> None:
        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)
        self.render_page(force=True)
        while not self.done:
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break
            key = cv2.waitKey(1) & 0xFF
            if key != 0xFF:
                if key == 27:  # ESC
                    self.done = True
                else:
                    self._on_key(key)
            self._on_idle()
        cv2.destroyAllWindows()


class SelectMapPage:
    """Map selection page."""

    def __init__(self, map_dir: str = "assets/resource/image/MapTracker/map"):
        self.map_dir = map_dir
        self.map_files = self._load_and_sort_maps()
        self.rows, self.cols = 2, 5
        self.nav_height = 90
        self.window_w, self.window_h = 1280, 720
        self.cell_size = min(
            self.window_w // self.cols, (self.window_h - self.nav_height) // self.rows
        )
        self.page_size = self.rows * self.cols
        self.window_name = "MapTracker Tool - Map Selector"

        self.current_page = 0
        self.cached_page = -1
        self.cached_img = None
        self.selected_index = -1
        self.total_pages = math.ceil(len(self.map_files) / self.page_size)

    def _load_and_sort_maps(self) -> list[str]:
        map_files = [f for f in os.listdir(self.map_dir) if f.endswith(".png")]
        if not map_files:
            return []

        def natural_sort_key(s: str) -> list[str | int]:
            return [
                int(text) if text.isdigit() else text.lower()
                for text in re.split("([0-9]+)", s)
            ]

        map_files.sort(key=lambda x: (len(x), natural_sort_key(x)))
        return map_files

    def _render_page(self):
        if self.cached_page == self.current_page:
            return self.cached_img
        drawer: Drawer = Drawer.new(self.window_w, self.window_h)
        start_idx = self.current_page * self.page_size
        end_idx = min(start_idx + self.page_size, len(self.map_files))

        # Content area height (excluding bottom navigation)
        content_h = self.window_h - self.nav_height
        content_w = self.window_w

        # Calculate horizontal and vertical spacing (space-between)
        if self.cols > 1:
            gap_x = int((content_w - self.cols * self.cell_size) / (self.cols - 1))
        else:
            gap_x = 0
        if self.rows > 1:
            gap_y = int((content_h - self.rows * self.cell_size) / (self.rows - 1))
        else:
            gap_y = 0

        # Draw map previews in space-between layout
        for i in range(start_idx, end_idx):
            idx_in_page = i - start_idx
            r = idx_in_page // self.cols
            c = idx_in_page % self.cols

            cell_x = int(c * (self.cell_size + gap_x))
            cell_y = int(r * (self.cell_size + gap_y))

            path = os.path.join(self.map_dir, self.map_files[i])
            img = cv2.imread(path)
            if img is not None:
                h, w = img.shape[:2]
                # Calculate scaling to maintain aspect ratio, fit image completely into cell
                scale = min(self.cell_size / w, self.cell_size / h)
                new_w = max(1, int(w * scale))
                new_h = max(1, int(h * scale))
                resized = cv2.resize(img, (new_w, new_h))
                # Center the image within the cell
                x1 = cell_x
                y1 = cell_y
                x2 = x1 + self.cell_size
                y2 = y1 + self.cell_size
                # Calculate placement offset
                dx = (self.cell_size - new_w) // 2
                dy = (self.cell_size - new_h) // 2
                dest_x1 = x1 + dx
                dest_y1 = y1 + dy
                dest_x2 = dest_x1 + new_w
                dest_y2 = dest_y1 + new_h
                # Boundary clipping (to prevent exceeding content area)
                dest_x2 = min(self.window_w, dest_x2)
                dest_y2 = min(content_h, dest_y2)
                src_x2 = dest_x2 - dest_x1
                src_y2 = dest_y2 - dest_y1
                if src_x2 > 0 and src_y2 > 0:
                    drawer._img[
                        dest_y1 : dest_y1 + src_y2, dest_x1 : dest_x1 + src_x2
                    ] = resized[0:src_y2, 0:src_x2]

                # Label (bottom)
                label = self.map_files[i]
                drawer.rect(
                    (x1, y1 + self.cell_size - 30),
                    (x1 + self.cell_size, y1 + self.cell_size),
                    color=0x000000,
                    thickness=-1,
                )
                drawer.text_centered(
                    label,
                    (x1 + self.cell_size // 2, y1 + self.cell_size - 10),
                    0.4,
                    color=0xFFFFFF,
                    thickness=1,
                )

        # Bottom navigation bar
        drawer.line(
            (0, content_h),
            (self.window_w, content_h),
            color=0x808080,
            thickness=2,
        )

        # Top navigation prompt text
        drawer.text_centered(
            "Please click a map to continue",
            (drawer.w // 2, content_h + 30),
            0.7,
            color=0xFFFFFF,
            thickness=1,
        )

        # Left arrow
        drawer.text(
            "< PREV",
            (150, self.window_h - 20),
            0.6,
            color=0x44DD66 if self.current_page > 0 else 0x808080,
            thickness=2,
        )

        # Middle page info
        page_text = f"Page {self.current_page + 1} / {self.total_pages}"
        drawer.text_centered(
            page_text,
            (drawer.w // 2, self.window_h - 20),
            0.5,
            color=0xFFFFFF,
            thickness=1,
        )

        # Right arrow
        drawer.text(
            "NEXT >",
            (self.window_w - 200, self.window_h - 20),
            0.6,
            color=0x44DD66 if self.current_page < self.total_pages - 1 else 0x808080,
            thickness=2,
        )

        self.cached_img = drawer.get_image()
        self.cached_page = self.current_page
        return self.cached_img

    def _handle_mouse(self, event, x, y, flags, param):
        if event == cv2.EVENT_LBUTTONDOWN:
            # Content area height (excluding bottom navigation)
            content_h = self.window_h - self.nav_height
            if y < content_h:
                # Use layout calculation to determine which cell the click falls into
                if self.cols > 1:
                    gap_x = int(
                        (self.window_w - self.cols * self.cell_size) / (self.cols - 1)
                    )
                else:
                    gap_x = 0
                if self.rows > 1:
                    gap_y = int(
                        (content_h - self.rows * self.cell_size) / (self.rows - 1)
                    )
                else:
                    gap_y = 0

                found = False
                for r in range(self.rows):
                    for c in range(self.cols):
                        cell_x = int(c * (self.cell_size + gap_x))
                        cell_y = int(r * (self.cell_size + gap_y))
                        if (
                            x >= cell_x
                            and x < cell_x + self.cell_size
                            and y >= cell_y
                            and y < cell_y + self.cell_size
                        ):
                            idx = self.current_page * self.page_size + r * self.cols + c
                            if idx < len(self.map_files):
                                self.selected_index = idx
                                found = True
                                break
                    if found:
                        break
            else:
                # Bottom navigation
                if x < self.window_w // 3:
                    if self.current_page > 0:
                        self.current_page -= 1
                elif x > 2 * self.window_w // 3:
                    if self.current_page < self.total_pages - 1:
                        self.current_page += 1

    def run(self):
        if not self.map_files:
            print(f"{_R}Error: No map files found in {self.map_dir}{_0}")
            print(
                "  Please ensure the current working directory of this program is correct!"
            )
            return None

        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)

        while True:
            cv2.imshow(self.window_name, self._render_page())

            if self.selected_index != -1:
                break
            key = cv2.waitKey(30) & 0xFF
            if key == 27:  # ESC
                break
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break

        cv2.destroyAllWindows()
        if self.selected_index != -1:
            return self.map_files[self.selected_index]
        return None
