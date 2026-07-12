from __future__ import annotations

from dataclasses import dataclass
import json
from functools import lru_cache
from pathlib import Path
import struct
import zlib

from model import ActionType, PathPoint, get_point_actions, normalize_zone_id, set_point_actions


PROJECT_ROOT = Path(__file__).resolve().parents[2]
MAP_LOCATOR_DIR = PROJECT_ROOT / "assets" / "resource" / "image" / "MapLocator"
MAP_TRACKER_MAP_DIR = PROJECT_ROOT / "assets" / "resource" / "image" / "MapTracker" / "map"
MAP_TRACKER_COORDINATE_TRANSFORMS_PATH = MAP_LOCATOR_DIR / "maptracker_coordinate_transforms.json"
MAP_TRACKER_EFFECTIVE_LUMINANCE_THRESHOLD = 80.0


@dataclass(frozen=True)
class MapTrackerCoordinateTransform:
    map_name: str
    zone_id: str
    offset_x: float
    offset_y: float
    scale_x: float
    scale_y: float
    parent_map_name: str = ""
    source_bbox: tuple[float, float, float, float] | None = None


def _source_bbox_from_json(item: dict[str, object]) -> tuple[float, float, float, float] | None:
    source_bbox = item.get("source_bbox")
    if source_bbox is None:
        return None

    left, top, right, bottom = source_bbox
    return float(left), float(top), float(right), float(bottom)


def _load_maptracker_coordinate_transforms() -> dict[str, MapTrackerCoordinateTransform]:
    data = json.loads(MAP_TRACKER_COORDINATE_TRANSFORMS_PATH.read_text(encoding="utf-8"))
    transforms: dict[str, MapTrackerCoordinateTransform] = {}
    for item in data["transforms"]:
        transform = MapTrackerCoordinateTransform(
            map_name=str(item["map_name"]),
            zone_id=str(item["zone_id"]),
            offset_x=float(item["offset_x"]),
            offset_y=float(item["offset_y"]),
            scale_x=float(item["scale_x"]),
            scale_y=float(item["scale_y"]),
            parent_map_name=str(item.get("parent_map_name", "")),
            source_bbox=_source_bbox_from_json(item),
        )
        transforms[transform.map_name] = transform
    return transforms


MAP_TRACKER_COORDINATE_TRANSFORMS = _load_maptracker_coordinate_transforms()

_zone_to_base_map_names: dict[str, set[str]] = {}
for transform in MAP_TRACKER_COORDINATE_TRANSFORMS.values():
    if transform.parent_map_name:
        continue
    _zone_to_base_map_names.setdefault(transform.zone_id, set()).add(transform.map_name)

MAP_TRACKER_BASE_ZONE_TO_MAP_NAME = {
    zone_id: next(iter(map_names))
    for zone_id, map_names in _zone_to_base_map_names.items()
    if len(map_names) == 1
}


def maptracker_base_map_name_from_zone(zone_id: object) -> str:
    return MAP_TRACKER_BASE_ZONE_TO_MAP_NAME.get(normalize_zone_id(zone_id), "")


def find_maptracker_transform(map_name: object) -> MapTrackerCoordinateTransform | None:
    normalized_map_name = normalize_zone_id(map_name)
    if not normalized_map_name:
        return None
    return MAP_TRACKER_COORDINATE_TRANSFORMS.get(normalized_map_name)


def _contains_source_point(transform: MapTrackerCoordinateTransform, x: float, y: float) -> bool:
    if transform.source_bbox is None:
        return False
    left, top, right, bottom = transform.source_bbox
    return left <= x <= right and top <= y <= bottom


def find_maptracker_point_transform(map_name: object, x: float, y: float) -> MapTrackerCoordinateTransform | None:
    transform = find_maptracker_transform(map_name)
    if transform is None:
        return None
    if transform.parent_map_name:
        return transform

    child_candidates = [
        candidate
        for candidate in MAP_TRACKER_COORDINATE_TRANSFORMS.values()
        if candidate.parent_map_name == transform.map_name
        and _contains_source_point(candidate, x, y)
        and _is_effective_tier_point(candidate, x, y)
    ]
    if not child_candidates:
        return transform

    return min(
        child_candidates,
        key=lambda candidate: (
            (candidate.source_bbox[2] - candidate.source_bbox[0]) * (candidate.source_bbox[3] - candidate.source_bbox[1])
            if candidate.source_bbox is not None
            else float("inf")
        ),
    )


def convert_maptracker_xy(x: float, y: float, transform: MapTrackerCoordinateTransform) -> tuple[float, float]:
    return (
        round(transform.offset_x + float(x) * transform.scale_x, 2),
        round(transform.offset_y + float(y) * transform.scale_y, 2),
    )


def convert_maptracker_rect(
    map_name: object,
    target: tuple[float, float, float, float] | list[float],
) -> tuple[str, tuple[float, float, float, float]] | None:
    if len(target) != 4:
        return None

    center_x = float(target[0]) + float(target[2]) / 2.0
    center_y = float(target[1]) + float(target[3]) / 2.0
    transform = find_maptracker_point_transform(map_name, center_x, center_y)
    if transform is None:
        return None

    x, y = convert_maptracker_xy(float(target[0]), float(target[1]), transform)
    width = round(float(target[2]) * transform.scale_x, 2)
    height = round(float(target[3]) * transform.scale_y, 2)
    return transform.zone_id, (x, y, width, height)


def convert_maptracker_point(point: PathPoint, route_map_name: str = "") -> tuple[PathPoint, bool]:
    point_zone = normalize_zone_id(point.get("zone", ""))
    transform = find_maptracker_point_transform(point_zone, float(point["x"]), float(point["y"]))
    if transform is not None and route_map_name and transform.map_name == route_map_name:
        transform = find_maptracker_point_transform(route_map_name, float(point["x"]), float(point["y"]))
    if transform is None:
        return point, False

    converted = dict(point)
    converted["x"], converted["y"] = convert_maptracker_xy(float(point["x"]), float(point["y"]), transform)
    converted["zone"] = transform.zone_id
    return converted, True


def convert_maptracker_points_to_mapnavigator(points: list[PathPoint]) -> tuple[list[PathPoint], int]:
    converted_points: list[PathPoint] = []
    converted_count = 0
    route_map_name = _infer_route_maptracker_map_name(points)
    for point in points:
        converted_point, converted = convert_maptracker_point(point, route_map_name=route_map_name)
        converted_points.append(converted_point)
        if converted:
            converted_count += 1
    apply_maptracker_portal_semantics(converted_points)
    return converted_points, converted_count


def apply_maptracker_portal_semantics(points: list[PathPoint]) -> None:
    previous_index: int | None = None
    for index, point in enumerate(points):
        current_zone = normalize_zone_id(point.get("zone", ""))
        if previous_index is not None:
            previous_zone = normalize_zone_id(points[previous_index].get("zone", ""))
            if previous_zone and current_zone and previous_zone != current_zone:
                _set_auto_portal(points[previous_index])
                _set_auto_portal(point)
        previous_index = index


def _infer_route_maptracker_map_name(points: list[PathPoint]) -> str:
    counts: dict[str, int] = {}
    for point in points:
        zone_name = normalize_zone_id(point.get("zone", ""))
        transform = find_maptracker_transform(zone_name)
        if transform is None:
            continue
        root_map_name = transform.parent_map_name or transform.map_name
        counts[root_map_name] = counts.get(root_map_name, 0) + 1
    if not counts:
        return ""
    return max(counts.items(), key=lambda item: item[1])[0]


def _is_effective_tier_point(transform: MapTrackerCoordinateTransform, x: float, y: float) -> bool:
    luminance = _maptracker_luminance_at(transform.map_name, x, y)
    return luminance is not None and luminance >= MAP_TRACKER_EFFECTIVE_LUMINANCE_THRESHOLD


@lru_cache(maxsize=None)
def _load_maptracker_png(map_name: str) -> tuple[int, int, list[bytes], int, int, list[tuple[int, int, int]] | None] | None:
    image_path = MAP_TRACKER_MAP_DIR / f"{map_name}.png"
    if not image_path.exists():
        return None

    data = image_path.read_bytes()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        return None

    pos = 8
    width = height = color_type = 0
    bit_depth = 0
    idat = bytearray()
    palette: list[tuple[int, int, int]] | None = None
    while pos + 8 <= len(data):
        length = struct.unpack(">I", data[pos : pos + 4])[0]
        chunk_type = data[pos + 4 : pos + 8]
        chunk = data[pos + 8 : pos + 8 + length]
        pos += 12 + length
        if chunk_type == b"IHDR":
            width, height, bit_depth, color_type, _compression, _filter, interlace = struct.unpack(">IIBBBBB", chunk)
            if bit_depth != 8 or color_type not in {0, 2, 3, 4, 6} or interlace != 0:
                return None
        elif chunk_type == b"PLTE":
            palette = [tuple(chunk[index : index + 3]) for index in range(0, len(chunk), 3)]
        elif chunk_type == b"IDAT":
            idat.extend(chunk)
        elif chunk_type == b"IEND":
            break

    if width <= 0 or height <= 0:
        return None

    channels_by_color_type = {
        0: 1,
        2: 3,
        3: 1,
        4: 2,
        6: 4,
    }
    channels = channels_by_color_type[color_type]
    stride = width * channels
    try:
        raw = zlib.decompress(bytes(idat))
    except zlib.error:
        return None

    rows: list[bytes] = []
    previous = [0] * stride
    index = 0
    for _row_index in range(height):
        if index >= len(raw):
            return None
        filter_type = raw[index]
        index += 1
        scanline = list(raw[index : index + stride])
        index += stride
        if len(scanline) != stride:
            return None

        row = [0] * stride
        for offset, value in enumerate(scanline):
            left = row[offset - channels] if offset >= channels else 0
            up = previous[offset]
            up_left = previous[offset - channels] if offset >= channels else 0
            if filter_type == 0:
                restored = value
            elif filter_type == 1:
                restored = value + left
            elif filter_type == 2:
                restored = value + up
            elif filter_type == 3:
                restored = value + ((left + up) // 2)
            elif filter_type == 4:
                predictor = left + up - up_left
                dist_left = abs(predictor - left)
                dist_up = abs(predictor - up)
                dist_up_left = abs(predictor - up_left)
                predicted = left if dist_left <= dist_up and dist_left <= dist_up_left else up if dist_up <= dist_up_left else up_left
                restored = value + predicted
            else:
                return None
            row[offset] = restored & 0xFF
        rows.append(bytes(row))
        previous = row
    return width, height, rows, channels, color_type, palette


def _maptracker_luminance_at(map_name: str, x: float, y: float) -> float | None:
    image = _load_maptracker_png(map_name)
    if image is None:
        return None
    width, height, rows, channels, color_type, palette = image
    pixel_x = int(round(x))
    pixel_y = int(round(y))
    if pixel_x < 0 or pixel_x >= width or pixel_y < 0 or pixel_y >= height:
        return None
    offset = pixel_x * channels
    row = rows[pixel_y]
    if color_type == 0:
        return float(row[offset])
    if color_type == 2 or color_type == 6:
        return (row[offset] + row[offset + 1] + row[offset + 2]) / 3.0
    if color_type == 4:
        return float(row[offset])
    if color_type == 3 and palette is not None:
        color_index = row[offset]
        if color_index >= len(palette):
            return None
        red, green, blue = palette[color_index]
        return (red + green + blue) / 3.0
    return None


def _set_auto_portal(point: PathPoint) -> None:
    if bool(point.get("suppress_auto_portal")):
        return
    set_point_actions(point, [int(ActionType.PORTAL)])
    point["auto_portal"] = True
