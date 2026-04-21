from __future__ import annotations

import math
import re
from enum import IntEnum
from pathlib import Path
from typing import NotRequired, TypedDict


class PathPoint(TypedDict):
    """路径点统一结构，录制与导出都复用该格式。"""

    x: float
    y: float
    action: int
    actions: list[int]
    zone: str
    strict: bool
    auto_portal: NotRequired[bool]
    suppress_auto_portal: NotRequired[bool]


class ActionType(IntEnum):
    """轨迹点动作类型。"""

    NONE = -1
    RUN = 0
    SPRINT = 1
    JUMP = 2
    FIGHT = 3
    INTERACT = 4
    PORTAL = 5
    TRANSFER = 6


ACTION_COLORS: dict[int, str] = {
    ActionType.NONE: "#64748b",
    ActionType.RUN: "#3498db",
    ActionType.SPRINT: "#e67e22",
    ActionType.JUMP: "#e74c3c",
    ActionType.FIGHT: "#9b59b6",
    ActionType.INTERACT: "#2ecc71",
    ActionType.PORTAL: "#facc15",
    ActionType.TRANSFER: "#fb7185",
}

ACTION_NAMES: dict[int, str] = {
    ActionType.NONE: "None",
    ActionType.RUN: "Run",
    ActionType.SPRINT: "Sprint",
    ActionType.JUMP: "Jump",
    ActionType.FIGHT: "Fight",
    ActionType.INTERACT: "Interact",
    ActionType.PORTAL: "Portal",
    ActionType.TRANSFER: "Transfer",
}

ACTION_TOKENS: dict[int, str] = {
    ActionType.RUN: "RUN",
    ActionType.SPRINT: "SPRINT",
    ActionType.JUMP: "JUMP",
    ActionType.FIGHT: "FIGHT",
    ActionType.INTERACT: "INTERACT",
    ActionType.PORTAL: "PORTAL",
    ActionType.TRANSFER: "TRANSFER",
}

ACTION_NAME_LOOKUP: dict[str, int] = {
    "NONE": int(ActionType.NONE),
    "RUN": int(ActionType.RUN),
    "SPRINT": int(ActionType.SPRINT),
    "JUMP": int(ActionType.JUMP),
    "FIGHT": int(ActionType.FIGHT),
    "INTERACT": int(ActionType.INTERACT),
    "PORTAL": int(ActionType.PORTAL),
    "TRANSFER": int(ActionType.TRANSFER),
}
ACTION_MENU_TYPES: tuple[ActionType, ...] = (
    ActionType.RUN,
    ActionType.SPRINT,
    ActionType.JUMP,
    ActionType.FIGHT,
    ActionType.INTERACT,
    ActionType.PORTAL,
    ActionType.TRANSFER,
)
ACTION_MENU_NAMES: tuple[str, ...] = tuple(ACTION_NAMES[action_type] for action_type in ACTION_MENU_TYPES)
INVALID_ZONE_IDS = {"NONE", "NULL", "N/A"}


def _normalize_action_chain(actions: list[int]) -> list[int]:
    non_run_actions = [action for action in actions if action not in (int(ActionType.NONE), int(ActionType.RUN))]
    if non_run_actions:
        return non_run_actions
    return [int(ActionType.RUN)]


def try_parse_action_type(value: object) -> int | None:
    """宽松解析动作值，兼容数字、枚举名和 UI 展示名。"""
    if isinstance(value, ActionType):
        return int(value)
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        try:
            return int(ActionType(value))
        except ValueError:
            return None
    if isinstance(value, float) and value.is_integer():
        return try_parse_action_type(int(value))
    if not isinstance(value, str):
        return None

    text = value.strip()
    if not text:
        return None
    if re.fullmatch(r"-?\d+", text):
        return try_parse_action_type(int(text))

    upper_text = text.upper()
    if upper_text in ACTION_NAME_LOOKUP:
        return ACTION_NAME_LOOKUP[upper_text]

    for action_type, display_name in ACTION_NAMES.items():
        if display_name.upper() == upper_text:
            return int(action_type)
    return None


def coerce_action_type(value: object, default: int = int(ActionType.RUN)) -> int:
    parsed = try_parse_action_type(value)
    return default if parsed is None else parsed


def coerce_action_chain(value: object, default: int = int(ActionType.RUN)) -> list[int]:
    if isinstance(value, (list, tuple)):
        actions = [parsed for item in value if (parsed := try_parse_action_type(item)) is not None]
        return _normalize_action_chain(actions or [default])

    return _normalize_action_chain([coerce_action_type(value, default=default)])


def normalize_zone_id(value: object, default: str = "") -> str:
    if not isinstance(value, str):
        return default

    zone_id = value.strip()
    if not zone_id:
        return default
    if zone_id.upper() in INVALID_ZONE_IDS:
        return default
    return zone_id


def get_point_actions(point: PathPoint) -> list[int]:
    fallback_action = coerce_action_type(point.get("action"), default=int(ActionType.RUN))
    return coerce_action_chain(point.get("actions"), default=fallback_action)


def get_display_action(actions: list[int]) -> int:
    normalized_actions = _normalize_action_chain(actions)
    return normalized_actions[-1]


def set_point_actions(point: PathPoint, actions: list[int]) -> None:
    normalized_actions = coerce_action_chain(actions, default=int(ActionType.RUN))
    point["actions"] = normalized_actions
    point["action"] = get_display_action(normalized_actions)


def set_manual_point_actions(point: PathPoint, actions: list[int]) -> None:
    set_point_actions(point, actions)
    point.pop("auto_portal", None)
    if get_point_actions(point) == [int(ActionType.RUN)]:
        point["suppress_auto_portal"] = True
    else:
        point.pop("suppress_auto_portal", None)


def coerce_strict_arrival(value: object, default: bool = False) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, int):
        if value in (0, 1):
            return bool(value)
        return default
    if isinstance(value, float) and value.is_integer():
        return coerce_strict_arrival(int(value), default=default)
    if not isinstance(value, str):
        return default

    text = value.strip().lower()
    if text in {"true", "1", "yes", "y", "on"}:
        return True
    if text in {"false", "0", "no", "n", "off"}:
        return False
    return default


def export_action_token(value: object) -> str:
    return ACTION_TOKENS.get(coerce_action_type(value), "RUN")


def _sync_portal_flags(point: PathPoint) -> None:
    if bool(point.get("auto_portal")) and get_point_actions(point) == [int(ActionType.PORTAL)]:
        point["auto_portal"] = True
    else:
        point.pop("auto_portal", None)

    if bool(point.get("suppress_auto_portal")) and get_point_actions(point) == [int(ActionType.RUN)]:
        point["suppress_auto_portal"] = True
    else:
        point.pop("suppress_auto_portal", None)


def normalize_path_points(points: list[PathPoint]) -> list[PathPoint]:
    """
    统一清洗轨迹点，并自动在跨区域边界补 PORTAL 动作。

    已存在的 PORTAL 点会被保留；边界点会被强制标成 PORTAL。
    """
    normalized: list[PathPoint] = []
    for point in points:
        action_chain = coerce_action_chain(
            point.get("actions"),
            default=coerce_action_type(point.get("action"), default=int(ActionType.RUN)),
        )
        normalized_point: PathPoint = {
            "x": round(float(point["x"]), 2),
            "y": round(float(point["y"]), 2),
            "action": get_display_action(action_chain),
            "actions": action_chain,
            "zone": normalize_zone_id(point.get("zone", "")),
            "strict": coerce_strict_arrival(point.get("strict"), default=False),
        }
        if bool(point.get("auto_portal")):
            normalized_point["auto_portal"] = True
        if bool(point.get("suppress_auto_portal")):
            normalized_point["suppress_auto_portal"] = True
        _sync_portal_flags(normalized_point)
        normalized.append(normalized_point)

    boundary_indices: set[int] = set()
    for idx in range(len(normalized) - 1):
        current_zone = normalized[idx]["zone"]
        next_zone = normalized[idx + 1]["zone"]
        if current_zone and next_zone and current_zone != next_zone:
            boundary_indices.add(idx)
            boundary_indices.add(idx + 1)

    for idx, point in enumerate(normalized):
        actions = get_point_actions(point)
        is_boundary_point = idx in boundary_indices

        if is_boundary_point:
            if bool(point.get("suppress_auto_portal")):
                point.pop("auto_portal", None)
                _sync_portal_flags(point)
                continue

            if actions == [int(ActionType.RUN)]:
                set_point_actions(point, [int(ActionType.PORTAL)])
                point["auto_portal"] = True
            elif actions != [int(ActionType.PORTAL)]:
                point.pop("auto_portal", None)
            _sync_portal_flags(point)
            continue

        if bool(point.get("auto_portal")) and actions == [int(ActionType.PORTAL)]:
            set_point_actions(point, [int(ActionType.RUN)])
        point.pop("auto_portal", None)
        point.pop("suppress_auto_portal", None)

    merged: list[PathPoint] = []
    for point in normalized:
        if (
            merged
            and merged[-1]["x"] == point["x"]
            and merged[-1]["y"] == point["y"]
            and merged[-1]["zone"] == point["zone"]
            and merged[-1]["strict"] == point["strict"]
        ):
            merged_auto_portal = bool(merged[-1].get("auto_portal")) or bool(point.get("auto_portal"))
            merged_suppressed = bool(merged[-1].get("suppress_auto_portal")) or bool(point.get("suppress_auto_portal"))
            set_point_actions(merged[-1], get_point_actions(merged[-1]) + get_point_actions(point))
            if merged_auto_portal:
                merged[-1]["auto_portal"] = True
            if merged_suppressed:
                merged[-1]["suppress_auto_portal"] = True
            _sync_portal_flags(merged[-1])
            continue
        merged.append(point)

    return merged


class PathRecorder:
    """录制阶段的数据累积器，负责基础去抖和动作/区域切换保留。"""

    def __init__(self) -> None:
        self.recorded_path: list[PathPoint] = []

    def add_waypoint(self, x: float, y: float, action: int, zone_id: str = "", strict: bool = False) -> None:
        zone_name = normalize_zone_id(zone_id)
        if not zone_name:
            return
        self.recorded_path.append(
            {
                "x": round(x, 2),
                "y": round(y, 2),
                "action": action,
                "actions": [int(action)],
                "zone": zone_name,
                "strict": strict,
            }
        )

    def update(self, current_x: float, current_y: float, current_action: int, zone_id: str = "") -> None:
        zone_name = normalize_zone_id(zone_id)
        if not zone_name:
            return

        if not self.recorded_path:
            self.add_waypoint(current_x, current_y, current_action, zone_name)
            return

        last_wp = self.recorded_path[-1]
        dx = current_x - last_wp["x"]
        dy = current_y - last_wp["y"]
        dist = math.hypot(dx, dy)

        # 保留尽量完整的原始轨迹，仅过滤亚像素级噪声。
        if dist > 0.5 or current_action != last_wp["action"] or zone_name != last_wp["zone"]:
            self.add_waypoint(current_x, current_y, current_action, zone_name)


def resolve_zone_image(zone_id: str, map_image_dir: Path) -> Path | None:
    """
    将 zone_id 映射到地图文件路径。

    支持以下命名模式：
    - MapLocator: Region_L{level}_{tier} -> MapLocator/Region/Lv{level:03d}Tier{tier}.png
    - MapLocator: Region_Base -> MapLocator/Region/Base.png
    - MapTracker: map01_lv001(_tier_114).png
    - 回退扫描：MapLocator 任意子目录下 `{zone_id}.png`
    """
    normalized_zone_id = normalize_zone_id(zone_id)
    if not normalized_zone_id:
        return None
    if not map_image_dir.exists():
        return None

    if map_image_dir.name.lower() == "maplocator":
        map_locator_dir = map_image_dir
    else:
        map_locator_dir = map_image_dir / "MapLocator"

    if map_image_dir.name.lower() == "map" and map_image_dir.parent.name.lower() == "maptracker":
        map_tracker_dir = map_image_dir
    else:
        map_tracker_dir = map_image_dir / "MapTracker" / "map"

    tracker_candidate = map_tracker_dir / f"{normalized_zone_id}.png"
    if tracker_candidate.exists():
        return tracker_candidate

    level_match = re.match(r"^(\w+?)_L(\d+)_(\d+)$", normalized_zone_id)
    if level_match:
        region, level, tier = level_match.group(1), int(level_match.group(2)), level_match.group(3)
        candidate = map_locator_dir / region / f"Lv{level:03d}Tier{tier}.png"
        if candidate.exists():
            return candidate

    base_match = re.match(r"^(\w+?)_Base$", normalized_zone_id)
    if base_match:
        region = base_match.group(1)
        candidate = map_locator_dir / region / "Base.png"
        if candidate.exists():
            return candidate

    if not map_locator_dir.exists():
        return None

    for sub_dir in map_locator_dir.iterdir():
        if not sub_dir.is_dir():
            continue
        candidate = sub_dir / f"{normalized_zone_id}.png"
        if candidate.exists():
            return candidate

    return None
