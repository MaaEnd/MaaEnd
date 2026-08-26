from __future__ import annotations

import json
import math
import re
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path
from typing import Any

from model import (
    ACTION_NAME_LOOKUP,
    ActionType,
    PathPoint,
    coerce_action_chain,
    coerce_action_type,
    coerce_strict_arrival,
    export_action_token,
    get_display_action,
    get_point_actions,
    normalize_path_points,
    normalize_zone_id,
    try_parse_action_type,
)


ZONE_HINT_KEYS = ("map_name", "mapName", "zone", "zone_id", "zoneId")
ACTION_KEYS = ("action", "action_type", "actionType", "type")
STRICT_KEYS = ("strict", "strict_arrival", "strictArrival")
TARGET_TIER_KEYS = ("target_tier", "targetTier")
CONTROL_ACTION_NAMES = {"HEADING", "ZONE"}
PROJECT_ROOT = Path(__file__).resolve().parents[2]
MAP_LOCATOR_DIR = PROJECT_ROOT / "assets" / "resource" / "image" / "MapLocator"


@dataclass(frozen=True)
class ImportedRoute:
    points: list[PathPoint]
    route_count: int
    source_has_zone_info: bool


@dataclass(frozen=True)
class ImportedAssertLocation:
    zone_id: str
    target: tuple[float, float, float, float]
    condition_count: int


def load_points_from_json_file(file_path: str | Path) -> ImportedRoute:
    data = load_jsonc(file_path)
    routes = discover_path_routes(data)
    if not routes:
        raise ValueError("未找到可识别的 path 数据")

    selected_route = max(routes, key=len)
    source_has_zone_info = any(normalize_zone_id(point.get("zone", "")) for point in selected_route)
    return ImportedRoute(
        points=normalize_path_points(selected_route),
        route_count=len(routes),
        source_has_zone_info=source_has_zone_info,
    )


def load_assert_location_from_json_file(file_path: str | Path) -> ImportedAssertLocation:
    data = load_jsonc(file_path)
    assert_locations = discover_assert_locations(data)
    if not assert_locations:
        raise ValueError("未找到可识别的 AssertLocation 数据")
    return assert_locations[0]


def export_path_nodes(points: list[PathPoint]) -> list[dict[str, Any] | list[int | float | str | bool]]:
    exported_nodes: list[dict[str, Any] | list[int | float | str | bool]] = []
    current_zone = ""

    for point in normalize_path_points(points):
        zone_id = normalize_zone_id(point.get("zone", ""))
        if zone_id and zone_id != current_zone:
            exported_nodes.append({"action": "ZONE", "zone_id": zone_id})
            current_zone = zone_id

        strict_arrival = coerce_strict_arrival(point.get("strict"), default=False)
        required = coerce_strict_arrival(point.get("required"), default=False)
        target_tier = normalize_zone_id(point.get("target_tier", ""))
        for action in get_point_actions(point):
            target = [_compact_number(point["x"]), _compact_number(point["y"])]
            if action == int(ActionType.NAVMESH):
                # The runtime parses NAVMESH from an object target and makes its arrival strict itself.
                navmesh_node: dict[str, Any] = {"action": "NAVMESH", "target": target}
                if target_tier:
                    navmesh_node["target_tier"] = target_tier
                if required:
                    navmesh_node["required"] = True
                exported_nodes.append(navmesh_node)
                continue

            if target_tier or required:
                positioned_node: dict[str, Any] = {
                    "action": export_action_token(action),
                    "target": target,
                }
                if target_tier:
                    positioned_node["target_tier"] = target_tier
                if strict_arrival:
                    positioned_node["strict"] = True
                if required:
                    positioned_node["required"] = True
                exported_nodes.append(positioned_node)
                continue

            node: list[int | float | str | bool] = [*target]
            if action != int(ActionType.RUN):
                node.append(export_action_token(action))
            if strict_arrival:
                node.append(True)
            exported_nodes.append(node)

    return exported_nodes


def export_assert_location_node(zone_id: str, target: tuple[float, float, float, float]) -> dict[str, Any]:
    normalized_zone_id = normalize_zone_id(zone_id)
    if not normalized_zone_id:
        raise ValueError("AssertLocation 缺少有效的 zone_id")

    x, y, w, h = target
    if w <= 0.0 or h <= 0.0:
        raise ValueError("AssertLocation 的 target 宽高必须大于 0")

    compact_target = [
        _compact_number(x),
        _compact_number(y),
        _compact_number(w),
        _compact_number(h),
    ]
    return {
        "NodeName": {
            "recognition": "Custom",
            "custom_recognition": "MapLocateAssertLocation",
            "custom_recognition_param": {
                "zone_id": normalized_zone_id,
                "target": compact_target,
            },
            "action": "DoNothing",
        }
    }


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


def discover_assert_locations(data: Any) -> list[ImportedAssertLocation]:
    assert_locations: list[ImportedAssertLocation] = []
    _walk_assert_location_node(data, assert_locations)
    return assert_locations


def split_route_into_segments(points: list[PathPoint]) -> list[tuple[int, int]]:
    if not points:
        return []
    if len(points) == 1:
        return [(0, 1)]

    break_indices: set[int] = set()
    distances: list[float] = []

    for idx in range(1, len(points)):
        prev_zone = normalize_zone_id(points[idx - 1].get("zone", ""))
        curr_zone = normalize_zone_id(points[idx].get("zone", ""))
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


def _walk_assert_location_node(node: Any, assert_locations: list[ImportedAssertLocation]) -> None:
    if isinstance(node, dict):
        parsed = _parse_assert_location_node(node)
        if parsed is not None:
            assert_locations.append(parsed)

        for value in node.values():
            _walk_assert_location_node(value, assert_locations)
        return

    if isinstance(node, list):
        for item in node:
            _walk_assert_location_node(item, assert_locations)


def _parse_assert_location_node(node: dict[str, Any]) -> ImportedAssertLocation | None:
    if node.get("custom_recognition") != "MapLocateAssertLocation":
        return None

    param = node.get("custom_recognition_param")
    if not isinstance(param, dict):
        return None
    return _parse_maplocate_assert_param(param)


def _parse_maplocate_assert_param(param: dict[str, Any]) -> ImportedAssertLocation | None:
    zone_id = normalize_zone_id(param.get("zone_id", ""))
    target = _parse_rect(param.get("target"))
    if not zone_id or target is None:
        return None
    return ImportedAssertLocation(zone_id, target, 1)


def _parse_rect(value: Any) -> tuple[float, float, float, float] | None:
    if not isinstance(value, list) or len(value) != 4:
        return None

    numbers: list[float] = []
    for item in value:
        number = _as_float(item)
        if number is None:
            return None
        numbers.append(number)
    if numbers[2] <= 0.0 or numbers[3] <= 0.0:
        return None
    return numbers[0], numbers[1], numbers[2], numbers[3]


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
def _load_available_zone_ids() -> tuple[str, ...]:
    zone_ids: set[str] = set()

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


def _looks_like_action_token(value: str) -> bool:
    token = value.strip()
    if not token:
        return False
    return bool(re.fullmatch(r"[A-Z_]+", token))


def _is_reserved_action_keyword(value: str) -> bool:
    token = value.strip().upper()
    if not token:
        return False
    if token in ACTION_NAME_LOOKUP:
        return True
    return token in CONTROL_ACTION_NAMES


def _try_parse_action_chain_value(value: Any) -> list[int] | None:
    if isinstance(value, list):
        parsed_actions: list[int] = []
        for item in value:
            parsed = try_parse_action_type(item)
            if parsed is None:
                return None
            parsed_actions.append(parsed)
        return coerce_action_chain(parsed_actions, default=int(ActionType.RUN))

    parsed = try_parse_action_type(value)
    if parsed is None:
        return None
    return coerce_action_chain(parsed, default=int(ActionType.RUN))


def _parse_point_dict(node: dict[str, Any], zone_hint: str) -> PathPoint | None:
    x = _as_float(node.get("x"))
    y = _as_float(node.get("y"))
    if x is None or y is None:
        # NAVMESH 路点写作 target: [x, y] (工具「复制 JSON 配置」的导出格式)。
        # 长度必须是 2：断言的 target 是 [x, y, w, h] 矩形，不是路点。
        target = node.get("target")
        if isinstance(target, list) and len(target) == 2:
            x = _as_float(target[0])
            y = _as_float(target[1])
    if x is None or y is None:
        return None

    zone = _resolve_zone_hint(node, zone_hint)
    actions = [int(ActionType.RUN)]
    if "actions" in node:
        parsed_actions = _try_parse_action_chain_value(node.get("actions"))
        if parsed_actions is None:
            return None
        actions = parsed_actions
    else:
        for key in ACTION_KEYS:
            if key not in node:
                continue
            parsed_actions = _try_parse_action_chain_value(node.get(key))
            if parsed_actions is None:
                return None
            actions = parsed_actions
            break

    point: PathPoint = {
        "x": round(x, 2),
        "y": round(y, 2),
        "action": get_display_action(actions),
        "actions": actions,
        "zone": zone,
        "strict": _resolve_strict_hint(node, False),
    }
    if coerce_strict_arrival(node.get("required"), default=False):
        point["required"] = True
    target_tier = _resolve_target_tier(node)
    if target_tier:
        point["target_tier"] = target_tier
    return point


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

        if isinstance(extra, str):
            if _looks_like_action_token(extra) or _is_reserved_action_keyword(extra):
                return None
            parsed_zone = normalize_zone_id(extra)
            if parsed_zone:
                zone = parsed_zone
                continue
            continue

        if isinstance(extra, list):
            parsed_actions = _try_parse_action_chain_value(extra)
            if parsed_actions is None:
                return None
            actions.extend(parsed_actions)
            continue

        return None

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
        zone_id = normalize_zone_id(value)
        if zone_id:
            return zone_id
    return fallback


def _resolve_strict_hint(node: dict[str, Any], fallback: bool) -> bool:
    for key in STRICT_KEYS:
        if key not in node:
            continue
        return coerce_strict_arrival(node.get(key), default=fallback)
    return fallback


def _resolve_target_tier(node: dict[str, Any]) -> str:
    for key in TARGET_TIER_KEYS:
        target_tier = normalize_zone_id(node.get(key, ""))
        if target_tier:
            return target_tier
    return ""


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
