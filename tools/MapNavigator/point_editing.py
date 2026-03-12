from __future__ import annotations

from typing import Protocol

from model import ACTION_NAMES, ActionType, PathPoint, set_point_actions


class CoordinateProjector(Protocol):
    def world_to_canvas(self, world_x: float, world_y: float) -> tuple[float, float]: ...


class PointEditingService:
    """点编辑领域逻辑：命中、插点、改动作、删点、拖拽。"""

    @staticmethod
    def hit_test(
        points: list[PathPoint],
        zone_indices: list[int],
        projector: CoordinateProjector,
        event_x: float,
        event_y: float,
        hit_radius: float = 12.0,
    ) -> int | None:
        best_index = None
        best_dist2 = hit_radius * hit_radius
        for index, global_idx in enumerate(zone_indices):
            point = points[global_idx]
            cx, cy = projector.world_to_canvas(point["x"], point["y"])
            dx = event_x - cx
            dy = event_y - cy
            dist2 = dx * dx + dy * dy
            if dist2 < best_dist2:
                best_dist2 = dist2
                best_index = index
        return best_index

    @staticmethod
    def action_name_to_type(action_name: str) -> int:
        for action_type, display_name in ACTION_NAMES.items():
            if display_name == action_name:
                return int(action_type)
        return int(ActionType.NONE)

    def insert_point(
        self,
        points: list[PathPoint],
        zone_indices: list[int],
        current_zone: str,
        action_name: str,
        strict_arrival: bool,
        world_x: float,
        world_y: float,
    ) -> None:
        action_type = self.action_name_to_type(action_name)
        new_point: PathPoint = {
            "x": round(world_x, 2),
            "y": round(world_y, 2),
            "action": action_type,
            "actions": [action_type],
            "zone": current_zone,
            "strict": strict_arrival,
        }

        if len(zone_indices) < 2:
            points.append(new_point)
            return

        best_segment = 0
        best_distance = float("inf")
        best_projection = 0.0
        for k in range(len(zone_indices) - 1):
            point_a = points[zone_indices[k]]
            point_b = points[zone_indices[k + 1]]
            distance, projection = self._dist_point_to_segment(
                world_x,
                world_y,
                point_a["x"],
                point_a["y"],
                point_b["x"],
                point_b["y"],
            )
            if distance < best_distance:
                best_distance = distance
                best_segment = k
                best_projection = projection

        is_last_segment = best_segment == len(zone_indices) - 2
        if is_last_segment and best_projection > 0.85:
            insert_pos = zone_indices[best_segment + 1] + 1
        else:
            insert_pos = zone_indices[best_segment + 1]
        points.insert(insert_pos, new_point)

    def apply_attributes(
        self,
        points: list[PathPoint],
        zone_indices: list[int],
        selected_idx: int | None,
        action_name: str,
        strict_arrival: bool,
    ) -> bool:
        if selected_idx is None or selected_idx >= len(zone_indices):
            return False

        global_idx = zone_indices[selected_idx]
        set_point_actions(points[global_idx], [self.action_name_to_type(action_name)])
        points[global_idx]["strict"] = strict_arrival
        return True

    @staticmethod
    def delete_selected(points: list[PathPoint], zone_indices: list[int], selected_idx: int | None) -> bool:
        if selected_idx is None or selected_idx >= len(zone_indices):
            return False

        global_idx = zone_indices[selected_idx]
        points.pop(global_idx)
        return True

    @staticmethod
    def move_selected(
        points: list[PathPoint],
        zone_indices: list[int],
        selected_idx: int | None,
        world_x: float,
        world_y: float,
    ) -> bool:
        if selected_idx is None or selected_idx >= len(zone_indices):
            return False

        global_idx = zone_indices[selected_idx]
        points[global_idx]["x"] = round(world_x, 2)
        points[global_idx]["y"] = round(world_y, 2)
        return True

    @staticmethod
    def _dist_point_to_segment(
        point_x: float,
        point_y: float,
        a_x: float,
        a_y: float,
        b_x: float,
        b_y: float,
    ) -> tuple[float, float]:
        vx, vy = b_x - a_x, b_y - a_y
        wx, wy = point_x - a_x, point_y - a_y
        vv = vx * vx + vy * vy
        if vv <= 1e-6:
            dx, dy = point_x - a_x, point_y - a_y
            return (dx * dx + dy * dy) ** 0.5, 0.0

        projection = (wx * vx + wy * vy) / vv
        projection = max(0.0, min(1.0, projection))
        cx, cy = a_x + projection * vx, a_y + projection * vy
        dx, dy = point_x - cx, point_y - cy
        return (dx * dx + dy * dy) ** 0.5, projection
