from __future__ import annotations

from dataclasses import dataclass

from model import PathPoint, normalize_zone_id


@dataclass(frozen=True)
class ZoneSegment:
    zone_id: str
    start_idx: int
    end_idx: int

    def point_indices(self) -> list[int]:
        return list(range(self.start_idx, self.end_idx))


class ZoneState:
    """负责当前连续区域片段的导航与点索引映射。"""

    def __init__(self) -> None:
        self.segments: list[ZoneSegment] = [ZoneSegment("", 0, 0)]
        self.current_segment_idx: int = 0

    def _current_segment(self) -> ZoneSegment:
        if not self.segments:
            return ZoneSegment("", 0, 0)
        return self.segments[self.current_segment_idx % len(self.segments)]

    def current_zone(self) -> str:
        return self._current_segment().zone_id

    def point_indices(self, points: list[PathPoint]) -> list[int]:
        segment = self._current_segment()
        if not segment.zone_id:
            return list(range(len(points)))
        end_idx = min(segment.end_idx, len(points))
        start_idx = min(segment.start_idx, end_idx)
        return list(range(start_idx, end_idx))

    def current_points(self, points: list[PathPoint]) -> list[PathPoint]:
        return [points[index] for index in self.point_indices(points)]

    def rebuild(self, points: list[PathPoint]) -> None:
        previous_segment = self._current_segment()
        rebuilt_segments: list[ZoneSegment] = []

        segment_start: int | None = None
        segment_zone = ""
        for idx, point in enumerate(points):
            zone_id = normalize_zone_id(point.get("zone", ""))
            if not zone_id:
                if segment_start is not None:
                    rebuilt_segments.append(ZoneSegment(segment_zone, segment_start, idx))
                    segment_start = None
                    segment_zone = ""
                continue

            if segment_start is None:
                segment_start = idx
                segment_zone = zone_id
                continue

            if zone_id != segment_zone:
                rebuilt_segments.append(ZoneSegment(segment_zone, segment_start, idx))
                segment_start = idx
                segment_zone = zone_id

        if segment_start is not None:
            rebuilt_segments.append(ZoneSegment(segment_zone, segment_start, len(points)))

        self.segments = rebuilt_segments or [ZoneSegment("", 0, len(points))]

        matched_idx = None
        if previous_segment.zone_id:
            for idx, segment in enumerate(self.segments):
                if (
                    segment.zone_id == previous_segment.zone_id
                    and segment.start_idx <= previous_segment.start_idx < segment.end_idx
                ):
                    matched_idx = idx
                    break

        self.current_segment_idx = 0 if matched_idx is None else matched_idx

    def label_text(self) -> str:
        segment = self._current_segment()
        if segment.zone_id:
            return f"片段 {self.current_segment_idx + 1}/{len(self.segments)}: {segment.zone_id}"
        return "— 无区域信息 —"

    def prev_zone(self) -> None:
        if not self.segments:
            return
        self.current_segment_idx = (self.current_segment_idx - 1) % len(self.segments)

    def next_zone(self) -> None:
        if not self.segments:
            return
        self.current_segment_idx = (self.current_segment_idx + 1) % len(self.segments)
