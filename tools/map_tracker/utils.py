import sys
import os
import re
import math
import time
from dataclasses import dataclass, field
from typing import Any, Callable, Literal, NamedTuple

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
        """Image width in pixels."""
        return self._img.shape[1]

    @property
    def h(self):
        """Image height in pixels."""
        return self._img.shape[0]

    def get_image(self):
        """Return the underlying image buffer."""
        return self._img

    def get_text_size(self, text: str, font_scale: float, *, thickness: int):
        """Measure text size for current font settings."""
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

    def dashed_line(
        self,
        pt1: Point,
        pt2: Point,
        *,
        color: Color,
        thickness: int,
        dash: int = 8,
        gap: int = 6,
    ) -> None:
        x1, y1 = pt1
        x2, y2 = pt2
        dx = x2 - x1
        dy = y2 - y1
        dist = math.hypot(dx, dy)
        if dist < 1:
            return
        nx, ny = dx / dist, dy / dist
        pos = 0.0
        drawing = True
        while pos < dist:
            seg = dash if drawing else gap
            end_pos = min(pos + seg, dist)
            if drawing:
                sx = int(round(x1 + nx * pos))
                sy = int(round(y1 + ny * pos))
                ex = int(round(x1 + nx * end_pos))
                ey = int(round(y1 + ny * end_pos))
                cv2.line(self._img, (sx, sy), (ex, ey), self._to_bgr(color), thickness)
            pos = end_pos
            drawing = not drawing

    def arrow(
        self,
        pt1: Point,
        pt2: Point,
        *,
        color: Color,
        thickness: int,
        arrow_size: int = 12,
    ) -> None:
        """Draw a line with an arrowhead at pt2."""
        self.line(pt1, pt2, color=color, thickness=thickness)
        x1, y1 = pt1
        x2, y2 = pt2
        dx = x2 - x1
        dy = y2 - y1
        dist = math.hypot(dx, dy)
        if dist < 1:
            return
        nx, ny = dx / dist, dy / dist
        ax1 = int(round(x2 - arrow_size * (nx - ny * 0.5)))
        ay1 = int(round(y2 - arrow_size * (ny + nx * 0.5)))
        ax2 = int(round(x2 - arrow_size * (nx + ny * 0.5)))
        ay2 = int(round(y2 - arrow_size * (ny - nx * 0.5)))
        cv2.line(self._img, (x2, y2), (ax1, ay1), self._to_bgr(color), thickness)
        cv2.line(self._img, (x2, y2), (ax2, ay2), self._to_bgr(color), thickness)

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

    def get_view_coords(self, real_x: float, real_y: float) -> tuple[int, int]:
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

    def maybe_center_to(
        self, real_x: float, real_y: float, padding: float = 0.3
    ) -> None:
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


class Button:
    def __init__(
        self,
        rect: tuple[int, int, int, int],
        text: str,
        base_color: int,
        text_color: int = 0xFFFFFF,
        hotkey: int | tuple[int, ...] | None = None,
        on_click: Callable[[], None] | None = None,
        thickness: int = -1,
        font_scale: float = 0.5,
    ):
        self.rect = rect
        self.text = text
        self.base_color = base_color
        self.text_color = text_color
        self.hotkey = (
            hotkey if isinstance(hotkey, tuple) else ((hotkey,) if hotkey else ())
        )
        self.on_click = on_click
        self.thickness = thickness
        self.font_scale = font_scale

        self.hovered = False
        self.needs_render = True

    def _get_draw_color(self) -> int:
        if not self.hovered:
            return self.base_color
        r = (self.base_color >> 16) & 0xFF
        g = (self.base_color >> 8) & 0xFF
        b = self.base_color & 0xFF
        r = min(255, r + 40)
        g = min(255, g + 40)
        b = min(255, b + 40)
        return (r << 16) | (g << 8) | b

    def render(self, drawer: "Drawer", border_color: int = 0xB4B4B4):
        x1, y1, x2, y2 = self.rect
        color = self._get_draw_color()
        drawer.rect((x1, y1), (x2, y2), color=color, thickness=self.thickness)
        if border_color != -1:
            drawer.rect((x1, y1), (x2, y2), color=border_color, thickness=1)

        cx, cy = x1 + (x2 - x1) // 2, y1 + (y2 - y1) // 2 + 5
        drawer.text_centered(
            self.text, (cx, cy), self.font_scale, color=self.text_color, thickness=1
        )
        self.needs_render = False

    def handle_mouse(self, event, x: int, y: int) -> bool:
        x1, y1, x2, y2 = self.rect
        in_rect = x1 <= x <= x2 and y1 <= y <= y2

        if self.hovered != in_rect:
            self.hovered = in_rect
            self.needs_render = True

        if event == cv2.EVENT_LBUTTONDOWN and in_rect:
            if self.on_click:
                self.on_click()
            self.needs_render = True
            return True
        return False

    def handle_key(self, key: int) -> bool:
        if key in self.hotkey:
            if self.on_click:
                self.on_click()
            self.needs_render = True
            return True
        return False


class BasePage:
    def __init__(
        self, window_name: str = "App", window_w: int = 1280, window_h: int = 720
    ):
        self.window_name = window_name
        self.window_w = window_w
        self.window_h = window_h
        self.mouse_pos: tuple[int, int] = (-1, -1)
        self._frame_interval = 1.0 / 120.0
        self._last_render_ts = 0.0
        self._needs_render = True
        self.done = False
        self.stepper: Any = None
        self.buttons: list[Button] = []

    def render_page(self) -> None:
        # Any explicit render request should mark the page dirty.
        self._needs_render = True

    def _render(self, drawer: Drawer) -> None:
        pass

    def render(self) -> Any:
        now = time.monotonic()
        btn_needs_render = any(b.needs_render for b in self.buttons)
        if (
            self._needs_render
            or btn_needs_render
            or (now - self._last_render_ts >= self._frame_interval)
        ):
            self._last_render_ts = now
            self._needs_render = False
            drawer = Drawer.new(self.window_w, self.window_h)

            self._render(drawer)

            for btn in self.buttons:
                btn.render(drawer)

            return drawer.get_image()
        return None

    def on_enter(self, stepper: Any):
        """Attach to stepper and prepare the page for rendering."""
        self.stepper = stepper
        # PageStepper owns the real cv2 window; use its name to avoid resizing
        # a non-existent page-local window.
        if hasattr(stepper, "window_name"):
            self.window_name = stepper.window_name
        cv2.resizeWindow(self.window_name, self.window_w, self.window_h)
        self.render_page()

    def on_exit(self):
        """Lifecycle hook called when page leaves the stack."""
        pass

    def handle_mouse(self, event, x: int, y: int, flags, param):
        """Dispatch mouse input to buttons first, then page handler."""
        self.mouse_pos = (x, y)
        for btn in self.buttons:
            if btn.handle_mouse(event, x, y):
                self.render_page()
                return
        self._on_mouse(event, x, y, flags, param)

    def _on_mouse(self, event, x: int, y: int, flags, param) -> None:
        pass

    def handle_key(self, key: int):
        """Dispatch key input to buttons first, then page handler."""
        for btn in self.buttons:
            if btn.handle_key(key):
                self.render_page()
                return
        self._on_key(key)

    def _on_key(self, key: int) -> None:
        pass

    def handle_idle(self):
        """Execute idle hook for background updates."""
        self._on_idle()

    def _on_idle(self) -> None:
        pass


@dataclass
class StepData:
    """Data for a simplified wizard-style step."""

    step_id: str
    title: str
    data: dict[str, Any] = field(default_factory=dict)
    can_go_back: bool = True


class StepPage(BasePage):
    """A generic BasePage that provides standard Wizard UI (header/footer)."""

    WINDOW_W = 1280
    WINDOW_H = 720
    HEADER_H = 80
    FOOTER_H = 50

    @staticmethod
    def is_up_key(key: int) -> bool:
        return key in (82, 0x260000, 65362)

    @staticmethod
    def is_down_key(key: int) -> bool:
        return key in (84, 0x280000, 65364)

    def __init__(self, step_data: StepData):
        super().__init__("WizardStep", self.WINDOW_W, self.WINDOW_H)
        self.step_data = step_data

        if self.step_data.can_go_back:
            btn_w, btn_h = 120, 36
            btn_x1 = 20
            btn_y1 = self.WINDOW_H - self.FOOTER_H + (self.FOOTER_H - btn_h) // 2
            btn_x2, btn_y2 = btn_x1 + btn_w, btn_y1 + btn_h

            def on_back():
                if len(self.stepper.step_history) > 1:
                    self.stepper.pop_step()

            self.buttons.append(
                Button(
                    rect=(btn_x1, btn_y1, btn_x2, btn_y2),
                    text="< Back",
                    base_color=0x555566,
                    text_color=0xFFFFFF,
                    on_click=on_back,
                )
            )

    def on_enter(self, stepper: "PageStepper"):
        super().on_enter(stepper)

    def _render_header(self, drawer: Drawer) -> None:
        h = self.HEADER_H
        drawer.rect((0, 0), (self.WINDOW_W, h), color=0x0A0A14, thickness=-1)
        step_num = len(
            [p for p in self.stepper.step_history if isinstance(p, StepPage)]
        )
        drawer.text(f"Step {step_num}", (30, h - 35), 0.6, color=0x6688AA, thickness=1)
        drawer.text_centered(
            self.step_data.title,
            (self.WINDOW_W // 2, h - 20),
            0.9,
            color=0xFFFFFF,
            thickness=2,
        )
        drawer.line((0, h - 1), (self.WINDOW_W, h - 1), color=0x444455, thickness=2)

    def _render_footer(self, drawer: Drawer) -> None:
        y1 = self.WINDOW_H - self.FOOTER_H
        y2 = self.WINDOW_H
        drawer.rect((0, y1), (self.WINDOW_W, y2), color=0x0A0A14, thickness=-1)
        drawer.line((0, y1), (self.WINDOW_W, y1), color=0x444455, thickness=2)

    def _render(self, drawer: Drawer):
        drawer.rect(
            (0, 0),
            (self.WINDOW_W, self.WINDOW_H),
            color=0x14141E,
            thickness=-1,
        )
        self._render_header(drawer)
        self._render_content(drawer)
        self._render_footer(drawer)

    def _on_mouse(self, event, x, y, flags, param):
        self._handle_content_mouse(event, x, y, flags, param)

    def _on_key(self, key):
        self._handle_content_key(key)

    def _render_content(self, drawer: Drawer):
        pass

    def _handle_content_mouse(self, event, x, y, flags, param):
        pass

    def _handle_content_key(self, key):
        pass


class PageStepper:
    """Main application loop managing a stack of pages."""

    def __init__(self, window_name: str = "App"):
        self.window_name = window_name
        self.step_history: list[BasePage] = []
        self.done = False
        self.result: Any = None
        cv2.namedWindow(self.window_name)
        cv2.setMouseCallback(self.window_name, self._handle_mouse)

    @property
    def current_step(self) -> BasePage | None:
        """Return the active page on top of the stack."""
        return self.step_history[-1] if self.step_history else None

    def push_step(self, page: BasePage) -> None:
        """Push a new page and enter it."""
        if self.current_step:
            self.current_step.on_exit()
        self.step_history.append(page)
        page.on_enter(self)
        self.request_render()

    def pop_step(self) -> BasePage | None:
        """Pop current page when history allows and restore previous page."""
        if len(self.step_history) > 1:
            popped = self.step_history.pop()
            popped.on_exit()
            if self.current_step:
                self.current_step.on_enter(self)
            self.request_render()
            return popped
        return None

    def finish(self, result: Any = None) -> None:
        """Stop the loop and store final result."""
        self.result = result
        self.done = True

    def request_render(self):
        """Request current step to render on next loop tick."""
        if self.current_step:
            self.current_step.render_page()

    def _handle_mouse(self, event, x, y, flags, param):
        if self.current_step:
            self.current_step.handle_mouse(event, x, y, flags, param)

    def run(self) -> Any:
        """Run the main event loop until finished or window closed."""
        if not self.step_history:
            raise RuntimeError("No initial step provided.")

        self.request_render()

        while not self.done:
            if cv2.getWindowProperty(self.window_name, cv2.WND_PROP_VISIBLE) < 1:
                break

            page = self.current_step
            if not page:
                break

            page.handle_idle()

            rendered_img = page.render()
            if rendered_img is not None:
                cv2.imshow(self.window_name, rendered_img)

            key = cv2.waitKeyEx(1)
            if key == 27:  # ESC
                if len(self.step_history) > 1:
                    self.pop_step()
                else:
                    break
            elif key != -1:
                page.handle_key(key)

        cv2.destroyAllWindows()
        return self.result


class TextInputWidget:
    """Single-line text input widget.

    Holds text state and cursor blink.  Call ``handle_key`` for keyboard
    events and ``render`` to draw into a Drawer.
    """

    def __init__(self, placeholder: str = "", max_length: int = 200):
        self.text = ""
        self.placeholder = placeholder
        self.max_length = max_length
        self._cursor_blink_start = time.time()

    def clear(self) -> None:
        self.text = ""
        self._cursor_blink_start = time.time()

    def handle_key(self, key: int) -> bool:
        """Process a cv2.waitKey result.  Returns True if consumed."""
        if key == 8 or key == 127:  # Backspace / Del
            if self.text:
                self.text = self.text[:-1]
                self._cursor_blink_start = time.time()
            return True
        if 32 <= key <= 126:  # Printable ASCII
            if len(self.text) < self.max_length:
                self.text += chr(key)
                self._cursor_blink_start = time.time()
            return True
        return False

    def render(
        self,
        drawer: "Drawer",
        rect: tuple[int, int, int, int],
        *,
        focused: bool = True,
        font_scale: float = 0.55,
    ) -> None:
        x1, y1, x2, y2 = rect
        h = y2 - y1
        # Background
        drawer.rect((x1, y1), (x2, y2), color=0x0D0D1A, thickness=-1)
        # Border
        border_color = 0x4488FF if focused else 0x555566
        drawer.rect((x1, y1), (x2, y2), color=border_color, thickness=2)
        # Text content
        pad_x = 10
        text_y = y1 + h // 2 + 6
        cursor_visible = (
            focused and int((time.time() - self._cursor_blink_start) * 1.6) % 2 == 0
        )
        if self.text:
            display = self.text + ("|" if cursor_visible else "")
            drawer.text(
                display, (x1 + pad_x, text_y), font_scale, color=0xFFFFFF, thickness=1
            )
        else:
            display = "|" if cursor_visible else self.placeholder
            color = 0xFFFFFF if cursor_visible else 0x666677
            drawer.text(
                display, (x1 + pad_x, text_y), font_scale, color=color, thickness=1
            )


class ScrollableListWidget:
    """Scrollable list widget.

    Items are dicts with keys:
      - ``label``     : str  display text
      - ``sub_label`` : str  (optional) secondary text shown right-aligned
      - ``disabled``  : bool  grayed-out, not selectable
      - ``priority``  : bool  pinned to top, shown with highlight tint
      - ``data``      : any   caller payload

    Usage::

        widget.set_items([...])
        widget.navigate(-1)          # up
        widget.handle_click(x, y, rect)
        widget.render(drawer, rect)
    """

    def __init__(self, item_height: int = 38):
        self.items: list[dict] = []
        self.selected_idx: int = -1
        self.scroll_offset: int = 0
        self.item_height = item_height
        self._preview_generator: Callable[[dict], np.ndarray | None] | None = None
        self._last_list_x2: int | None = None

    def set_preview_generator(
        self, generator: Callable[[dict], np.ndarray | None] | None
    ) -> None:
        """Set or clear the preview image generator for selected item.

        When the generator returns a non-None image, the list is rendered as
        left list + right preview panel. Otherwise list keeps full width.
        """
        self._preview_generator = generator

    def set_items(self, items: list[dict], *, auto_select_first: bool = True) -> None:
        prev_selected_data = None
        if 0 <= self.selected_idx < len(self.items):
            prev_selected_data = self.items[self.selected_idx].get("data")

        self.items = items
        self.scroll_offset = 0
        self.selected_idx = -1

        # Keep selection if the same item still exists after filtering.
        if prev_selected_data is not None:
            for i, item in enumerate(items):
                if item.get("data") == prev_selected_data and not item.get("disabled"):
                    self.selected_idx = i
                    return

        # Initial population: optionally auto-select first enabled item.
        if auto_select_first:
            for i, item in enumerate(items):
                if not item.get("disabled"):
                    self.selected_idx = i
                    break

    def _enabled_indices(self) -> list[int]:
        return [i for i, item in enumerate(self.items) if not item.get("disabled")]

    def navigate(self, direction: int) -> None:
        """direction: -1 = up, +1 = down."""
        enabled = self._enabled_indices()
        if not enabled:
            return
        if self.selected_idx not in enabled:
            self.selected_idx = enabled[0]
            return
        curr_pos = enabled.index(self.selected_idx)
        new_pos = curr_pos + direction
        if 0 <= new_pos < len(enabled):
            self.selected_idx = enabled[new_pos]

    def handle_click(self, x: int, y: int, rect: tuple[int, int, int, int]) -> int:
        """Two-step click behavior.

        Returns item index (>=0) only when clicking an already-selected item
        (confirmation click). First click only updates selection and returns -1.
        """
        x1, y1, x2, y2 = rect
        list_x2 = self._last_list_x2 if self._last_list_x2 is not None else x2
        # Ignore clicks on scrollbar strip and preview area.
        content_x2 = max(x1, list_x2 - 6)
        if not (x1 <= x <= content_x2 and y1 <= y <= y2):
            return -1
        rel_y = y - y1
        idx = self.scroll_offset + rel_y // self.item_height
        if 0 <= idx < len(self.items) and not self.items[idx].get("disabled"):
            if self.selected_idx == idx:
                return idx
            self.selected_idx = idx
            return -1
        return -1

    def handle_wheel(
        self, x: int, y: int, flags: int, rect: tuple[int, int, int, int]
    ) -> bool:
        """Handle mouse wheel scrolling. Returns True if the event was consumed."""
        x1, y1, x2, y2 = rect
        if not (x1 <= x <= x2 and y1 <= y <= y2):
            return False

        # Calculate visible count
        h = y2 - y1
        visible = max(1, h // self.item_height)
        max_offset = max(0, len(self.items) - visible)

        # Scroll direction
        if flags > 0:  # Scroll up
            self.scroll_offset = max(0, self.scroll_offset - 1)
        else:  # Scroll down
            self.scroll_offset = min(max_offset, self.scroll_offset + 1)

        # Keep selection inside current viewport so _ensure_visible won't undo wheel scroll.
        if self.selected_idx >= 0:
            if self.selected_idx < self.scroll_offset:
                self.selected_idx = self.scroll_offset
            elif self.selected_idx >= self.scroll_offset + visible:
                self.selected_idx = min(
                    len(self.items) - 1,
                    self.scroll_offset + visible - 1,
                )

        return True

    def _ensure_visible(self, visible_count: int) -> None:
        if self.selected_idx < 0:
            return
        if self.selected_idx < self.scroll_offset:
            self.scroll_offset = self.selected_idx
        elif self.selected_idx >= self.scroll_offset + visible_count:
            self.scroll_offset = self.selected_idx - visible_count + 1

    def render(
        self,
        drawer: "Drawer",
        rect: tuple[int, int, int, int],
        *,
        font_scale: float = 0.45,
    ) -> None:
        x1, y1, x2, y2 = rect
        preview_img: np.ndarray | None = None
        if self._preview_generator is not None and 0 <= self.selected_idx < len(
            self.items
        ):
            try:
                preview_img = self._preview_generator(self.items[self.selected_idx])
            except Exception:
                preview_img = None

        list_x2 = x2
        if preview_img is not None:
            list_x2 = x1 + max(1, (x2 - x1) // 2)
        self._last_list_x2 = list_x2

        h = y2 - y1
        visible = max(1, h // self.item_height)
        self._ensure_visible(visible)
        for i in range(visible):
            item_idx = self.scroll_offset + i
            if item_idx >= len(self.items):
                break
            item = self.items[item_idx]
            iy1 = y1 + i * self.item_height
            iy2 = iy1 + self.item_height
            disabled = item.get("disabled", False)
            priority = item.get("priority", False)
            selected = item_idx == self.selected_idx

            # Background
            if selected:
                bg_color = 0x1E4A90
            elif priority:
                bg_color = 0x111828
            else:
                bg_color = 0x0A0A14
            drawer.rect((x1, iy1), (list_x2, iy2), color=bg_color, thickness=-1)

            # Label
            label = item.get("label", "")
            text_color = (
                0x666677 if disabled else (0xFFFFFF if not priority else 0xAADDFF)
            )
            drawer.text(
                label, (x1 + 12, iy2 - 10), font_scale, color=text_color, thickness=1
            )

            # Sub-label (right-aligned)
            sub = item.get("sub_label", "")
            if sub:
                sub_color = 0x445566 if disabled else 0x6688AA
                sub_size = drawer.get_text_size(sub, font_scale, thickness=1)
                drawer.text(
                    sub,
                    (list_x2 - sub_size[0] - 12, iy2 - 10),
                    font_scale,
                    color=sub_color,
                    thickness=1,
                )

            # Divider
            drawer.line((x1, iy2 - 1), (list_x2, iy2 - 1), color=0x222233, thickness=1)

        # Scrollbar
        total = len(self.items)
        if total > visible and total > 0:
            bar_x = list_x2 - 6
            bar_y1 = y1
            bar_y2 = y2
            bar_h = bar_y2 - bar_y1
            thumb_h = max(20, bar_h * visible // total)
            thumb_y = bar_y1 + (bar_h - thumb_h) * self.scroll_offset // max(
                1, total - visible
            )
            drawer.rect(
                (bar_x, bar_y1), (list_x2, bar_y2), color=0x111122, thickness=-1
            )
            drawer.rect(
                (bar_x, thumb_y),
                (list_x2, thumb_y + thumb_h),
                color=0x445566,
                thickness=-1,
            )

        if preview_img is not None:
            px1 = list_x2 + 1
            px2 = x2
            py1 = y1
            py2 = y2

            drawer.rect((px1, py1), (px2, py2), color=0x07070D, thickness=-1)
            drawer.rect((px1, py1), (px2, py2), color=0x223044, thickness=1)

            img = preview_img
            if img.ndim == 2:
                img = cv2.cvtColor(img, cv2.COLOR_GRAY2BGR)

            ph = max(1, py2 - py1 - 16)
            pw = max(1, px2 - px1 - 16)
            ih, iw = img.shape[:2]
            scale = min(pw / max(1, iw), ph / max(1, ih))
            nw = max(1, int(iw * scale))
            nh = max(1, int(ih * scale))
            ox = px1 + (px2 - px1 - nw) // 2
            oy = py1 + (py2 - py1 - nh) // 2
            drawer.paste(
                img,
                (ox, oy),
                scale_w=nw,
                scale_h=nh,
                with_alpha=(img.ndim == 3 and img.shape[2] == 4),
            )
