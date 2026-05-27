import os
import warnings
from functools import lru_cache
from typing import Final

import cv2

SPRITE_PATH: Final[str] = os.path.join(os.path.dirname(__file__), "sprite_sheet.png")

SPRITE_LAYOUT: Final[dict[str, tuple[int, int, int, int]]] = {
    "Move": (0, 0, 64, 64),
    "AssertLocation": (64, 0, 64, 64),
    "Upload": (0, 64, 64, 64),
    "JSON": (64, 64, 64, 64),
    "Map": (128, 0, 64, 64),
    "Layer": (0, 128, 64, 64),
    "Undo": (128, 64, 64, 64),
    "Redo": (64, 128, 64, 64),
}

_SPRITE_CACHE: cv2.typing.MatLike | None = None


def _load_sprite() -> cv2.typing.MatLike | None:
    global _SPRITE_CACHE
    if _SPRITE_CACHE is None:
        if not os.path.isfile(SPRITE_PATH):
            warnings.warn(f"Sprite sheet not found: {SPRITE_PATH}")
        else:
            _SPRITE_CACHE = cv2.imread(SPRITE_PATH, cv2.IMREAD_UNCHANGED)
            if _SPRITE_CACHE is None:
                warnings.warn(f"Failed to load sprite sheet: {SPRITE_PATH}")
    return _SPRITE_CACHE


@lru_cache(maxsize=16)
def get_sprite_image(
    sprite_name: str, size: tuple[int, int] | None
) -> cv2.typing.MatLike | None:
    sprite = _load_sprite()
    if sprite is None:
        warnings.warn("Sprite sheet is unavailable")
        return None

    rect = SPRITE_LAYOUT.get(sprite_name)
    if rect is None:
        warnings.warn(f"Sprite name not found: {sprite_name}")
        return None

    x, y, w, h = rect
    if w <= 0 or h <= 0:
        warnings.warn(f"Sprite rect is invalid: {sprite_name} {rect}")
        return None

    sprite_h, sprite_w = sprite.shape[:2]
    if x < 0 or y < 0 or x + w > sprite_w or y + h > sprite_h:
        warnings.warn(
            f"Sprite rect out of bounds: {sprite_name} {rect} (sheet {sprite_w}x{sprite_h})"
        )
        return None

    sprite_slice = sprite[y : y + h, x : x + w].copy()
    if size is not None:
        target_w, target_h = size
        if target_w <= 0 or target_h <= 0:
            warnings.warn(f"Sprite size is invalid: {size}")
            return None
        sprite_slice = cv2.resize(
            sprite_slice, (target_w, target_h), interpolation=cv2.INTER_AREA
        )
    return sprite_slice
