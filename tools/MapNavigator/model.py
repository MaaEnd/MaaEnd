from __future__ import annotations

import ctypes
import math
import re
from enum import IntEnum
from pathlib import Path
from typing import TypedDict


class PathPoint(TypedDict):
    """路径点统一结构，录制与导出都复用该格式。"""

    x: float
    y: float
    action: int
    actions: list[int]
    zone: str
    strict: bool


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


def _normalize_action_chain(actions: list[int]) -> list[int]:
    non_run_actions = [action for action in actions if action != int(ActionType.RUN)]
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
        normalized.append(
            {
                "x": round(float(point["x"]), 2),
                "y": round(float(point["y"]), 2),
                "action": get_display_action(action_chain),
                "actions": action_chain,
                "zone": str(point.get("zone", "") or ""),
                "strict": coerce_strict_arrival(point.get("strict"), default=False),
            }
        )

    for idx in range(len(normalized) - 1):
        current_zone = normalized[idx]["zone"]
        next_zone = normalized[idx + 1]["zone"]
        if current_zone and next_zone and current_zone != next_zone:
            if get_point_actions(normalized[idx]) == [int(ActionType.RUN)]:
                set_point_actions(normalized[idx], [int(ActionType.PORTAL)])
            if get_point_actions(normalized[idx + 1]) == [int(ActionType.RUN)]:
                set_point_actions(normalized[idx + 1], [int(ActionType.PORTAL)])

    merged: list[PathPoint] = []
    for point in normalized:
        if (
            merged
            and merged[-1]["x"] == point["x"]
            and merged[-1]["y"] == point["y"]
            and merged[-1]["zone"] == point["zone"]
            and merged[-1]["strict"] == point["strict"]
        ):
            set_point_actions(merged[-1], get_point_actions(merged[-1]) + get_point_actions(point))
            continue
        merged.append(point)

    return merged


def is_key_pressed(vk_code: int) -> bool:
    """读取 Windows 按键状态；在非 Windows 或调用失败时返回 False。"""
    try:
        return (ctypes.windll.user32.GetAsyncKeyState(vk_code) & 0x8000) != 0
    except Exception:
        return False


def perpendicular_distance(point: PathPoint, line_start: PathPoint, line_end: PathPoint) -> float:
    """计算点到线段方向向量对应直线的垂距（RDP 需要）。"""
    dx = line_end["x"] - line_start["x"]
    dy = line_end["y"] - line_start["y"]
    magnitude = math.hypot(dx, dy)
    if magnitude > 0.0:
        dx /= magnitude
        dy /= magnitude

    pvx = point["x"] - line_start["x"]
    pvy = point["y"] - line_start["y"]
    projection = pvx * dx + pvy * dy
    dsx = projection * dx
    dsy = projection * dy
    return math.hypot(pvx - dsx, pvy - dsy)


def _rdp_recursive(
    points: list[PathPoint],
    start_idx: int,
    end_idx: int,
    epsilon: float,
    out: list[PathPoint],
) -> None:
    max_dist = 0.0
    index = start_idx
    for i in range(start_idx + 1, end_idx):
        dist = perpendicular_distance(points[i], points[start_idx], points[end_idx])
        if dist > max_dist:
            max_dist = dist
            index = i

    if max_dist > epsilon:
        _rdp_recursive(points, start_idx, index, epsilon, out)
        if out:
            out.pop()
        _rdp_recursive(points, index, end_idx, epsilon, out)
        return

    out.append(points[start_idx])
    out.append(points[end_idx])


def apply_constrained_rdp(path: list[PathPoint], epsilon: float = 1.5) -> list[PathPoint]:
    """
    在非 RUN 动作点处强制保留锚点，再在锚点区间内应用 RDP。
    该函数保留为公共能力，供调试或外部脚本复用。
    """
    if len(path) < 3:
        return path

    anchor_indices = [0]
    for i in range(1, len(path) - 1):
        if path[i]["action"] != ActionType.RUN:
            anchor_indices.append(i)
    anchor_indices.append(len(path) - 1)

    result: list[PathPoint] = []
    for i in range(len(anchor_indices) - 1):
        segment_out: list[PathPoint] = []
        _rdp_recursive(path, anchor_indices[i], anchor_indices[i + 1], epsilon, segment_out)
        for point in segment_out:
            if not result or (result[-1]["x"] != point["x"] or result[-1]["y"] != point["y"]):
                result.append(point)
    return result


def simplify_path(path: list[PathPoint], density: int) -> list[PathPoint]:
    """
    根据密度参数压缩轨迹点，同时保留动作点与关键转向信息。

    处理流程：
    1. 把 density (0-100) 映射为 RDP 阈值和最大段长。
    2. 标记锚点：起终点、非 RUN 点、同区域内的明显转角点。
    3. 在锚点区间内执行 RDP。
    4. 对超长线段做等距插值，保证导航稳定性。
    """
    if len(path) < 2:
        return path

    epsilon = 8.0 - (7.2 * density / 100.0)
    max_seg_len = 120.0 - (95.0 * density / 100.0)
    turn_threshold = 20.0

    anchor_indices = {0, len(path) - 1}
    for i in range(1, len(path) - 1):
        if path[i]["action"] != ActionType.RUN:
            anchor_indices.add(i)
            continue

        prev_point, curr_point, next_point = path[i - 1], path[i], path[i + 1]
        if prev_point["zone"] == curr_point["zone"] == next_point["zone"]:
            d1x = curr_point["x"] - prev_point["x"]
            d1y = curr_point["y"] - prev_point["y"]
            d2x = next_point["x"] - curr_point["x"]
            d2y = next_point["y"] - curr_point["y"]
            mag1 = math.hypot(d1x, d1y)
            mag2 = math.hypot(d2x, d2y)
            if mag1 > 0.1 and mag2 > 0.1:
                dot = (d1x * d2x + d1y * d2y) / (mag1 * mag2)
                angle = math.degrees(math.acos(max(-1.0, min(1.0, dot))))
                if angle > turn_threshold:
                    anchor_indices.add(i)

    sorted_anchors = sorted(anchor_indices)

    rdp_points: list[PathPoint] = []
    for i in range(len(sorted_anchors) - 1):
        segment_out: list[PathPoint] = []
        _rdp_recursive(path, sorted_anchors[i], sorted_anchors[i + 1], epsilon, segment_out)
        for point in segment_out:
            if not rdp_points or (rdp_points[-1]["x"] != point["x"] or rdp_points[-1]["y"] != point["y"]):
                rdp_points.append(point)

    result: list[PathPoint] = []
    for i in range(len(rdp_points) - 1):
        p1 = rdp_points[i]
        p2 = rdp_points[i + 1]
        result.append(p1)

        if p1["zone"] != p2["zone"]:
            continue

        dist = math.hypot(p2["x"] - p1["x"], p2["y"] - p1["y"])
        if dist <= max_seg_len:
            continue

        num_segments = math.ceil(dist / max_seg_len)
        for j in range(1, num_segments):
            ratio = j / num_segments
            result.append(
                {
                    "x": round(p1["x"] + (p2["x"] - p1["x"]) * ratio, 2),
                    "y": round(p1["y"] + (p2["y"] - p1["y"]) * ratio, 2),
                    "action": ActionType.RUN,
                    "actions": [int(ActionType.RUN)],
                    "zone": p1["zone"],
                    "strict": False,
                }
            )

    result.append(rdp_points[-1])
    return normalize_path_points(result)


class PathRecorder:
    """录制阶段的数据累积器，负责基础去抖和动作/区域切换保留。"""

    def __init__(self) -> None:
        self.recorded_path: list[PathPoint] = []

    def add_waypoint(self, x: float, y: float, action: int, zone_id: str = "") -> None:
        self.recorded_path.append(
            {
                "x": round(x, 2),
                "y": round(y, 2),
                "action": action,
                "actions": [int(action)],
                "zone": zone_id,
                "strict": False,
            }
        )

    def update(self, current_x: float, current_y: float, current_action: int, zone_id: str = "") -> None:
        if not self.recorded_path:
            self.add_waypoint(current_x, current_y, current_action, zone_id)
            return

        last_wp = self.recorded_path[-1]
        dx = current_x - last_wp["x"]
        dy = current_y - last_wp["y"]
        dist = math.hypot(dx, dy)

        # 保留尽量完整的原始轨迹，仅过滤亚像素级噪声。
        if dist > 0.5 or current_action != last_wp["action"] or zone_id != last_wp["zone"]:
            self.add_waypoint(current_x, current_y, current_action, zone_id)


def resolve_zone_image(zone_id: str, map_image_dir: Path) -> Path | None:
    """
    将 zone_id 映射到地图文件路径。

    支持以下命名模式：
    - MapLocator: Region_L{level}_{tier} -> MapLocator/Region/Lv{level:03d}Tier{tier}.png
    - MapLocator: Region_Base -> MapLocator/Region/Base.png
    - MapTracker: map01_lv001(_tier_114).png
    - 回退扫描：MapLocator 任意子目录下 `{zone_id}.png`
    """
    if not zone_id or zone_id == "None":
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

    tracker_candidate = map_tracker_dir / f"{zone_id}.png"
    if tracker_candidate.exists():
        return tracker_candidate

    level_match = re.match(r"^(\w+?)_L(\d+)_(\d+)$", zone_id)
    if level_match:
        region, level, tier = level_match.group(1), int(level_match.group(2)), level_match.group(3)
        candidate = map_locator_dir / region / f"Lv{level:03d}Tier{tier}.png"
        if candidate.exists():
            return candidate

    base_match = re.match(r"^(\w+?)_Base$", zone_id)
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
        candidate = sub_dir / f"{zone_id}.png"
        if candidate.exists():
            return candidate

    return None
