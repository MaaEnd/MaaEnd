import sys

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
