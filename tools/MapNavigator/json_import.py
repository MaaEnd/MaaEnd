from __future__ import annotations

import copy
import json
import math
import re
import struct
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path
from typing import Any

from model import (
    ActionType,
    PathPoint,
    coerce_action_chain,
    coerce_action_type,
    coerce_strict_arrival,
    export_action_token,
    get_display_action,
    get_point_actions,
    normalize_path_points,
    try_parse_action_type,
)


ZONE_HINT_KEYS = ("map_name", "mapName", "zone", "zone_id", "zoneId")
ACTION_KEYS = ("action", "action_type", "actionType", "type")
STRICT_KEYS = ("strict", "strict_arrival", "strictArrival")
CONTROL_ACTION_NAMES = {"HEADING", "ZONE"}
PROJECT_ROOT = Path(__file__).resolve().parents[2]
MAP_TRACKER_MAP_DIR = PROJECT_ROOT / "assets" / "resource" / "image" / "MapTracker" / "map"
MAP_TRACKER_BBOX_PATH = MAP_TRACKER_MAP_DIR / "map_bbox.json"
MAP_LOCATOR_DIR = PROJECT_ROOT / "assets" / "resource" / "image" / "MapLocator"


@dataclass(frozen=True)
class ImportedRoute:
    points: list[PathPoint]
    route_count: int
    source_has_zone_info: bool


def load_points_from_json_file(file_path: str | Path, apply_zone_inference: bool = True) -> ImportedRoute:
    data = load_jsonc(file_path)
    routes = discover_path_routes(data)
    if not routes:
        raise ValueError("未找到可识别的 path 数据")

    selected_route = max(routes, key=len)
    source_has_zone_info = any(str(point.get("zone", "") or "") for point in selected_route)
    if apply_zone_inference:
        selected_route = infer_missing_zones(selected_route)
    return ImportedRoute(
        points=normalize_path_points(selected_route),
        route_count=len(routes),
        source_has_zone_info=source_has_zone_info,
    )


def export_path_nodes(points: list[PathPoint]) -> list[dict[str, Any] | list[int | float | str | bool]]:
    exported_nodes: list[dict[str, Any] | list[int | float | str | bool]] = []
    current_zone = ""

    for point in normalize_path_points(points):
        zone_id = str(point.get("zone", "") or "")
        if zone_id and zone_id != current_zone:
            exported_nodes.append({"action": "ZONE", "zone_id": zone_id})
            current_zone = zone_id

        strict_arrival = coerce_strict_arrival(point.get("strict"), default=False)
        for action in get_point_actions(point):
            node: list[int | float | str | bool] = [_compact_number(point["x"]), _compact_number(point["y"])]
            if action != int(ActionType.RUN):
                node.append(export_action_token(action))
            if strict_arrival:
                node.append(True)
            exported_nodes.append(node)

    return exported_nodes


def load_jsonc(file_path: str | Path) -> Any:
    text = Path(file_path).read_text(encoding="utf-8")
    sanitized = strip_json_comments(text)
    sanitized = strip_trailing_commas(sanitized)
    try:
        return json.loads(sanitized)
    except json.JSONDecodeError as exc:
        raise ValueError(f"JSON 解析失败: 第 {exc.lineno} 行，第 {exc.colno} 列") from exc


def discover_path_routes(data: Any) -> list[list[PathPoint]]:
    routes: list[list[PathPoint]] = []
    _walk_json_node(data, routes, zone_hint="")
    return routes


def infer_missing_zones(points: list[PathPoint]) -> list[PathPoint]:
    if not points:
        return points
    if all(point.get("zone") for point in points):
        return points

    candidates = _load_map_candidates()
    if not candidates:
        return points

    inferred = [copy.deepcopy(point) for point in points]
    point_matches: list[list[str]] = []
    route_scores: dict[str, int] = {}

    for point in inferred:
        explicit_zone = str(point.get("zone", "") or "")
        if explicit_zone:
            route_scores[explicit_zone] = route_scores.get(explicit_zone, 0) + 100
            point_matches.append([explicit_zone])
            continue

        matches = _match_point_to_zones(point["x"], point["y"], candidates)
        point_matches.append(matches)
        for rank, zone_name in enumerate(matches[:5]):
            route_scores[zone_name] = route_scores.get(zone_name, 0) + max(1, 5 - rank)

    primary_zone = max(route_scores.items(), key=lambda item: item[1])[0] if route_scores else ""
    primary_hits = sum(1 for matches in point_matches if primary_zone and primary_zone in matches)

    if primary_zone and primary_hits >= max(2, len(inferred) // 2):
        for idx, point in enumerate(inferred):
            if point.get("zone"):
                continue
            if not point_matches[idx] or primary_zone in point_matches[idx]:
                point["zone"] = primary_zone

    for idx, point in enumerate(inferred):
        if point.get("zone"):
            continue
        matches = point_matches[idx]
        if not matches:
            continue
        if primary_zone and primary_zone in matches and primary_hits >= max(2, len(inferred) // 2):
            point["zone"] = primary_zone
        else:
            point["zone"] = matches[0]

    _fill_unknown_zones(inferred, fallback_zone=primary_zone)
    return inferred


def split_route_into_segments(points: list[PathPoint]) -> list[tuple[int, int]]:
    if not points:
        return []
    if len(points) == 1:
        return [(0, 1)]

    break_indices: set[int] = set()
    distances: list[float] = []

    for idx in range(1, len(points)):
        prev_zone = str(points[idx - 1].get("zone", "") or "")
        curr_zone = str(points[idx].get("zone", "") or "")
        if prev_zone and curr_zone and prev_zone != curr_zone:
            break_indices.add(idx)

        prev_action = coerce_action_type(points[idx - 1].get("action"), default=int(ActionType.RUN))
        curr_action = coerce_action_type(points[idx].get("action"), default=int(ActionType.RUN))
        if prev_action == int(ActionType.PORTAL) and curr_action == int(ActionType.PORTAL):
            break_indices.add(idx)

        dx = points[idx]["x"] - points[idx - 1]["x"]
        dy = points[idx]["y"] - points[idx - 1]["y"]
        distances.append(math.hypot(dx, dy))

    positive_distances = sorted(distance for distance in distances if distance > 0.0)
    if positive_distances:
        median_distance = positive_distances[len(positive_distances) // 2]
        gap_threshold = max(120.0, median_distance * 5.0)
        for idx, distance in enumerate(distances, start=1):
            if distance > gap_threshold:
                break_indices.add(idx)

    segments: list[tuple[int, int]] = []
    start = 0
    for end in sorted(break_indices):
        if end > start:
            segments.append((start, end))
            start = end
    if start < len(points):
        segments.append((start, len(points)))
    return segments


def list_available_zone_ids() -> list[str]:
    return sorted(_load_available_zone_ids())


def _walk_json_node(node: Any, routes: list[list[PathPoint]], zone_hint: str) -> None:
    if isinstance(node, dict):
        local_zone = _resolve_zone_hint(node, zone_hint)

        path_value = node.get("path")
        if path_value is not None:
            route = _parse_route(path_value, local_zone)
            if route:
                routes.append(route)

        for key, value in node.items():
            if key == "path" and path_value is not None:
                continue
            _walk_json_node(value, routes, local_zone)
        return

    if isinstance(node, list):
        route = _parse_route(node, zone_hint)
        if route:
            routes.append(route)
            return

        for item in node:
            _walk_json_node(item, routes, zone_hint)


def _parse_route(node: Any, zone_hint: str) -> list[PathPoint] | None:
    if not isinstance(node, list) or not node:
        return None

    points: list[PathPoint] = []
    current_zone = zone_hint
    for item in node:
        zone_declaration = _parse_zone_declaration(item)
        if zone_declaration is not None:
            current_zone = zone_declaration
            continue

        point = _parse_point(item, current_zone)
        if point is None:
            if _is_skippable_control_node(item):
                continue
            return None
        if point["zone"]:
            current_zone = point["zone"]
        points.append(point)
    return points


@lru_cache(maxsize=1)
def _load_map_candidates() -> list[dict[str, float | str]]:
    bbox_data: dict[str, list[float]] = {}
    if MAP_TRACKER_BBOX_PATH.exists():
        try:
            raw = json.loads(MAP_TRACKER_BBOX_PATH.read_text(encoding="utf-8"))
            if isinstance(raw, dict):
                bbox_data = raw
        except Exception:
            bbox_data = {}

    candidates: list[dict[str, float | str]] = []
    if not MAP_TRACKER_MAP_DIR.exists():
        return candidates

    for image_path in MAP_TRACKER_MAP_DIR.glob("*.png"):
        size = _read_png_size(image_path)
        if size is None:
            continue

        width, height = size
        zone_name = image_path.stem
        rect = bbox_data.get(zone_name, [0, 0, width, height])
        if not isinstance(rect, list) or len(rect) != 4:
            rect = [0, 0, width, height]

        x1, y1, x2, y2 = [float(value) for value in rect]
        if x2 <= x1 or y2 <= y1:
            x1, y1, x2, y2 = 0.0, 0.0, float(width), float(height)

        candidates.append(
            {
                "zone": zone_name,
                "bbox_x1": x1,
                "bbox_y1": y1,
                "bbox_x2": x2,
                "bbox_y2": y2,
                "bbox_area": (x2 - x1) * (y2 - y1),
                "img_width": float(width),
                "img_height": float(height),
                "img_area": float(width * height),
            }
        )

    return candidates


@lru_cache(maxsize=1)
def _load_available_zone_ids() -> tuple[str, ...]:
    zone_ids: set[str] = {str(candidate["zone"]) for candidate in _load_map_candidates()}

    if MAP_LOCATOR_DIR.exists():
        for image_path in MAP_LOCATOR_DIR.rglob("*.png"):
            zone_ids.update(_map_locator_zone_ids(image_path))

    zone_ids.discard("")
    return tuple(zone_ids)


def _map_locator_zone_ids(image_path: Path) -> tuple[str, ...]:
    stem = image_path.stem
    parent_name = image_path.parent.name

    if image_path.name == "Base.png":
        return (f"{parent_name}_Base",)

    level_match = re.match(r"^Lv(\d+)Tier(.+)$", stem)
    if level_match:
        return (f"{parent_name}_L{int(level_match.group(1))}_{level_match.group(2)}",)

    return (stem,)


def _read_png_size(image_path: Path) -> tuple[int, int] | None:
    try:
        with image_path.open("rb") as file:
            header = file.read(24)
        if len(header) < 24 or header[:8] != b"\x89PNG\r\n\x1a\n":
            return None
        width, height = struct.unpack(">II", header[16:24])
        return width, height
    except Exception:
        return None


def _match_point_to_zones(point_x: float, point_y: float, candidates: list[dict[str, float | str]]) -> list[str]:
    bbox_matches: list[tuple[float, str]] = []
    image_matches: list[tuple[float, str]] = []

    for candidate in candidates:
        zone_name = str(candidate["zone"])
        if (
            float(candidate["bbox_x1"]) <= point_x <= float(candidate["bbox_x2"])
            and float(candidate["bbox_y1"]) <= point_y <= float(candidate["bbox_y2"])
        ):
            bbox_matches.append((float(candidate["bbox_area"]), zone_name))
            continue

        if 0.0 <= point_x <= float(candidate["img_width"]) and 0.0 <= point_y <= float(candidate["img_height"]):
            image_matches.append((float(candidate["img_area"]), zone_name))

    if bbox_matches:
        return [zone_name for _area, zone_name in sorted(bbox_matches, key=lambda item: item[0])]
    if image_matches:
        return [zone_name for _area, zone_name in sorted(image_matches, key=lambda item: item[0])]
    return []


def _fill_unknown_zones(points: list[PathPoint], fallback_zone: str) -> None:
    if not points:
        return

    known_indices = [idx for idx, point in enumerate(points) if point.get("zone")]
    if not known_indices:
        if fallback_zone:
            for point in points:
                point["zone"] = fallback_zone
        return

    for idx, point in enumerate(points):
        if point.get("zone"):
            continue

        prev_zone = ""
        next_zone = ""

        for prev_idx in range(idx - 1, -1, -1):
            zone_name = str(points[prev_idx].get("zone", "") or "")
            if zone_name:
                prev_zone = zone_name
                break

        for next_idx in range(idx + 1, len(points)):
            zone_name = str(points[next_idx].get("zone", "") or "")
            if zone_name:
                next_zone = zone_name
                break

        if prev_zone and prev_zone == next_zone:
            point["zone"] = prev_zone
        elif prev_zone:
            point["zone"] = prev_zone
        elif next_zone:
            point["zone"] = next_zone
        elif fallback_zone:
            point["zone"] = fallback_zone


def _parse_point(node: Any, zone_hint: str) -> PathPoint | None:
    if isinstance(node, dict):
        return _parse_point_dict(node, zone_hint)
    if isinstance(node, list):
        return _parse_point_list(node, zone_hint)
    return None


def _parse_zone_declaration(node: Any) -> str | None:
    if not isinstance(node, dict):
        return None
    if _read_action_name(node) != "ZONE":
        return None

    zone_id = _resolve_zone_hint(node, "")
    return zone_id or None


def _is_skippable_control_node(node: Any) -> bool:
    return isinstance(node, dict) and _read_action_name(node) in CONTROL_ACTION_NAMES


def _read_action_name(node: dict[str, Any]) -> str:
    for key in ACTION_KEYS:
        value = node.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip().upper()
    return ""


def _parse_action_chain_value(value: Any) -> list[int]:
    return coerce_action_chain(value, default=int(ActionType.RUN))


def _parse_point_dict(node: dict[str, Any], zone_hint: str) -> PathPoint | None:
    x = _as_float(node.get("x"))
    y = _as_float(node.get("y"))
    if x is None or y is None:
        return None

    zone = _resolve_zone_hint(node, zone_hint)
    actions = [int(ActionType.RUN)]
    if "actions" in node:
        actions = _parse_action_chain_value(node.get("actions"))
    else:
        for key in ACTION_KEYS:
            if key not in node:
                continue
            actions = _parse_action_chain_value(node.get(key))
            break

    return {
        "x": round(x, 2),
        "y": round(y, 2),
        "action": get_display_action(actions),
        "actions": actions,
        "zone": zone,
        "strict": _resolve_strict_hint(node, False),
    }


def _parse_point_list(node: list[Any], zone_hint: str) -> PathPoint | None:
    if len(node) < 2:
        return None

    x = _as_float(node[0])
    y = _as_float(node[1])
    if x is None or y is None:
        return None

    actions: list[int] = []
    zone = zone_hint
    strict_arrival = False

    for extra in node[2:]:
        if isinstance(extra, bool):
            strict_arrival = extra
            continue
        if isinstance(extra, str):
            lowered = extra.strip().lower()
            if lowered in {"true", "false"}:
                strict_arrival = coerce_strict_arrival(extra, default=strict_arrival)
                continue

        parsed_action = try_parse_action_type(extra)
        if parsed_action is not None:
            actions.append(parsed_action)
            continue

        if isinstance(extra, str) and extra.strip():
            zone = extra.strip()

        if isinstance(extra, list):
            actions.extend(_parse_action_chain_value(extra))

    actions = coerce_action_chain(actions, default=int(ActionType.RUN))

    return {
        "x": round(x, 2),
        "y": round(y, 2),
        "action": get_display_action(actions),
        "actions": actions,
        "zone": zone,
        "strict": strict_arrival,
    }


def _resolve_zone_hint(node: dict[str, Any], fallback: str) -> str:
    for key in ZONE_HINT_KEYS:
        value = node.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return fallback


def _resolve_strict_hint(node: dict[str, Any], fallback: bool) -> bool:
    for key in STRICT_KEYS:
        if key not in node:
            continue
        return coerce_strict_arrival(node.get(key), default=fallback)
    return fallback


def _as_float(value: Any) -> float | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        text = value.strip()
        if not text:
            return None
        try:
            return float(text)
        except ValueError:
            return None
    return None


def _compact_number(value: float) -> int | float:
    rounded = round(float(value), 2)
    if rounded.is_integer():
        return int(rounded)
    return rounded


def strip_json_comments(text: str) -> str:
    out: list[str] = []
    idx = 0
    in_string = False
    quote_char = ""

    while idx < len(text):
        ch = text[idx]
        nxt = text[idx + 1] if idx + 1 < len(text) else ""

        if in_string:
            out.append(ch)
            if ch == "\\" and idx + 1 < len(text):
                idx += 1
                out.append(text[idx])
            elif ch == quote_char:
                in_string = False
            idx += 1
            continue

        if ch in ('"', "'"):
            in_string = True
            quote_char = ch
            out.append(ch)
            idx += 1
            continue

        if ch == "/" and nxt == "/":
            idx += 2
            while idx < len(text) and text[idx] not in "\r\n":
                idx += 1
            continue

        if ch == "/" and nxt == "*":
            idx += 2
            while idx + 1 < len(text) and not (text[idx] == "*" and text[idx + 1] == "/"):
                idx += 1
            idx += 2
            continue

        out.append(ch)
        idx += 1

    return "".join(out)


def strip_trailing_commas(text: str) -> str:
    out: list[str] = []
    idx = 0
    in_string = False
    quote_char = ""

    while idx < len(text):
        ch = text[idx]

        if in_string:
            out.append(ch)
            if ch == "\\" and idx + 1 < len(text):
                idx += 1
                out.append(text[idx])
            elif ch == quote_char:
                in_string = False
            idx += 1
            continue

        if ch in ('"', "'"):
            in_string = True
            quote_char = ch
            out.append(ch)
            idx += 1
            continue

        if ch == ",":
            lookahead = idx + 1
            while lookahead < len(text) and text[lookahead].isspace():
                lookahead += 1
            if lookahead < len(text) and text[lookahead] in "}]":
                idx += 1
                continue

        out.append(ch)
        idx += 1

    return "".join(out)
