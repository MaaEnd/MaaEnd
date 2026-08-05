import sys
import os
import re
from pathlib import Path
from typing import Literal

_R = "\033[31m"
_G = "\033[32m"
_Y = "\033[33m"
_C = "\033[36m"
_A = "\033[90m"
_B = "\033[34m"
_0 = "\033[0m"

try:
    import numpy as np
except ImportError:
    print(f"{_R}Cannot import 'numpy'!{_0}")
    print("  Please run 'pip install numpy' first.")
    sys.exit(1)

try:
    import cv2
except ImportError:
    print(f"{_R}Cannot import 'opencv-python'!{_0}")
    print("  Please run 'pip install opencv-python' first.")
    sys.exit(1)


MapType = Literal["normal", "tier", "base", "dung"]

REPO_ROOT = Path(__file__).resolve().parents[3]
MAP_DIR = REPO_ROOT / "assets" / "resource" / "image" / "MapTracker" / "map"
PIPELINE_DIR = REPO_ROOT / "assets" / "resource" / "pipeline"
NAVMESH_DIR = REPO_ROOT / "assets" / "data" / "MapTrackerNavMesh"
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

        m = re.match(
            r"^(?P<map_id>[a-z]+\d*)_(?P<map_level_id>[a-z]+\d+)(?:_(?P<x>\d+)_(?P<y>\d+))?(?:_tier_(?P<tier>[a-z0-9_]+))?$",
            name,
        )
        if not m:
            raise ValueError(f"unrecognized map name format: {name_or_path}")

        map_id = m.group("map_id")
        map_level_id = m.group("map_level_id")
        tier_suffix = m.group("tier")

        if is_tile:
            if m.group("x") is None or m.group("y") is None:
                raise ValueError(
                    f"expected tile map name format (with _X_Y): {name_or_path}"
                )
            tile_x = int(m.group("x"))
            tile_y = int(m.group("y"))
        else:
            if m.group("x") is not None or m.group("y") is not None:
                raise ValueError(
                    f"expected non-tile map name format (without _X_Y): {name_or_path}"
                )
            tile_x = None
            tile_y = None

        map_type: MapType
        if tier_suffix is not None:
            map_type = "tier"
        elif map_id.startswith("base"):
            map_type = "base"
        elif map_id.startswith("dung"):
            map_type = "dung"
        else:
            map_type = "normal"
        return MapName(
            map_id=map_id,
            map_level_id=map_level_id,
            map_type=map_type,
            tile_x=tile_x,
            tile_y=tile_y,
            tier_suffix=tier_suffix,
        )


def unique_map_key(name: str) -> str:
    """Returns a stable key for comparing MapTracker map names."""
    try:
        parsed = MapName.parse(name)
        parts = [parsed.map_type, parsed.map_id, parsed.map_level_id]
        if parsed.map_type == "tier" and parsed.tier_suffix:
            parts.append(parsed.tier_suffix)
        return ":".join(parts)
    except ValueError:
        basename = os.path.basename(name.replace("\\", "/"))
        return os.path.splitext(basename)[0].lower()


def find_map_file(name: str, map_dir: str | os.PathLike[str] = MAP_DIR) -> str | None:
    """Finds a map filename by exact or semantic map name."""
    directory = Path(map_dir)
    if not directory.is_dir():
        return None
    files = sorted(path.name for path in directory.iterdir() if path.is_file())
    basename = os.path.basename(str(name).replace("\\", "/"))
    if basename in files:
        return basename
    target_key = unique_map_key(basename)
    return next(
        (file_name for file_name in files if unique_map_key(file_name) == target_key),
        None,
    )


def alpha_composite(
    background: np.ndarray,
    foreground: np.ndarray,
    position: tuple[int, int],
) -> None:
    """Composites a BGRA foreground onto a BGR or BGRA background in place."""
    if foreground.ndim != 3 or foreground.shape[2] != 4:
        raise ValueError("foreground must be a BGRA image")
    if background.ndim != 3 or background.shape[2] not in (3, 4):
        raise ValueError("background must be a BGR or BGRA image")

    x, y = position
    foreground_height, foreground_width = foreground.shape[:2]
    background_height, background_width = background.shape[:2]
    x0, y0 = max(0, x), max(0, y)
    x1 = min(background_width, x + foreground_width)
    y1 = min(background_height, y + foreground_height)
    if x1 <= x0 or y1 <= y0:
        return

    foreground_x0, foreground_y0 = x0 - x, y0 - y
    foreground_x1 = foreground_x0 + x1 - x0
    foreground_y1 = foreground_y0 + y1 - y0
    source = foreground[
        foreground_y0:foreground_y1,
        foreground_x0:foreground_x1,
    ]
    target = background[y0:y1, x0:x1]

    source_alpha = source[:, :, 3:4].astype(np.float32) / 255.0
    target_alpha = (
        target[:, :, 3:4].astype(np.float32) / 255.0
        if target.shape[2] == 4
        else np.ones_like(source_alpha)
    )
    output_alpha = source_alpha + target_alpha * (1.0 - source_alpha)
    non_transparent = output_alpha[:, :, 0] > 0
    output_rgb = np.zeros_like(target[:, :, :3], dtype=np.float32)
    output_rgb[non_transparent] = (
        source[:, :, :3][non_transparent] * source_alpha[non_transparent]
        + target[:, :, :3][non_transparent]
        * target_alpha[non_transparent]
        * (1.0 - source_alpha[non_transparent])
    ) / output_alpha[non_transparent]

    target[:, :, :3] = np.clip(output_rgb, 0, 255).astype(np.uint8)
    if target.shape[2] == 4:
        target[:, :, 3:4] = np.clip(output_alpha * 255.0, 0, 255).astype(np.uint8)
