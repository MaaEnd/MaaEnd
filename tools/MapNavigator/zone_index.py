from __future__ import annotations

from model import PathPoint


class ZoneState:
    """负责当前区域导航与点索引映射。"""

    def __init__(self) -> None:
        self.zone_ids: list[str] = [""]
        self.current_zone_idx: int = 0

    def current_zone(self) -> str:
        if not self.zone_ids:
            return ""
        return self.zone_ids[self.current_zone_idx % len(self.zone_ids)]

    def point_indices(self, points: list[PathPoint]) -> list[int]:
        zone_id = self.current_zone()
        if not zone_id:
            return list(range(len(points)))
        return [index for index, point in enumerate(points) if point.get("zone") == zone_id]

    def current_points(self, points: list[PathPoint]) -> list[PathPoint]:
        return [points[index] for index in self.point_indices(points)]

    def rebuild(self, points: list[PathPoint]) -> None:
        previous_zone = self.current_zone()
        discovered = []
        for point in points:
            zone_id = point.get("zone", "")
            if zone_id and zone_id not in discovered:
                discovered.append(zone_id)

        self.zone_ids = discovered or [""]
        if previous_zone and previous_zone in self.zone_ids:
            self.current_zone_idx = self.zone_ids.index(previous_zone)
        else:
            self.current_zone_idx = 0

    def label_text(self) -> str:
        zone_id = self.current_zone()
        if zone_id:
            return f"区域 {self.current_zone_idx + 1}/{len(self.zone_ids)}: {zone_id}"
        return "— 无区域信息 —"

    def prev_zone(self) -> None:
        if not self.zone_ids:
            return
        self.current_zone_idx = (self.current_zone_idx - 1) % len(self.zone_ids)

    def next_zone(self) -> None:
        if not self.zone_ids:
            return
        self.current_zone_idx = (self.current_zone_idx + 1) % len(self.zone_ids)
