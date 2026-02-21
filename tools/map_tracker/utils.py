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
Color = tuple[int, int, int]


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
                bg_color,
                -1,
            )
        cv2.putText(self._img, text, pos, self._font_face, font_scale, color, thickness)

    def text_centered(
        self, text: str, pos: Point, font_scale: float, *, color: Color, thickness: int
    ):
        text_size = self.get_text_size(text, font_scale, thickness=thickness)
        x = pos[0] - text_size[0] // 2
        self.text(text, (x, pos[1]), font_scale, color=color, thickness=thickness)

    def rect(self, pt1: Point, pt2: Point, *, color: Color, thickness: int):
        cv2.rectangle(self._img, pt1, pt2, color, thickness)

    def circle(self, center: Point, radius: int, *, color: Color, thickness: int):
        cv2.circle(self._img, center, radius, color, thickness)

    def line(self, pt1: Point, pt2: Point, *, color: Color, thickness: int):
        cv2.line(self._img, pt1, pt2, color, thickness)

    @staticmethod
    def new(w: int, h: int, **kwargs) -> "Drawer":
        img = np.zeros((h, w, 3), dtype=np.uint8)
        return Drawer(img, **kwargs)
