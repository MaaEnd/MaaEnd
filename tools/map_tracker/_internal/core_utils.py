import sys
import os
import re
import math
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
        thickness: int | None = None,
        bg_color: Color | None = None,
        bg_padding: int = 5,
    ):
        if thickness is None:
            thickness = max(1, int(round(font_scale * 2)))
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
        self,
        text: str,
        pos: Point,
        font_scale: float,
        *,
        color: Color,
        thickness: int | None = None,
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
