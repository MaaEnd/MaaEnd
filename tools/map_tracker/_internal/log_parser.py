from __future__ import annotations

import json
from dataclasses import asdict, dataclass
from datetime import datetime


@dataclass(slots=True)
class TrackPoint:
    map_name: str
    x: float
    y: float
    rot: float
    timestamp: float | None
    loc_conf: float
    rot_conf: float

    def to_dict(self) -> dict:
        return asdict(self)


@dataclass(slots=True)
class TrackPeriod:
    period_id: int
    points: list[TrackPoint]
    start_timestamp: float | None
    end_timestamp: float | None

    def to_dict(self) -> dict:
        return {
            "period_id": self.period_id,
            "points": [point.to_dict() for point in self.points],
            "start_timestamp": self.start_timestamp,
            "end_timestamp": self.end_timestamp,
        }


def _parse_iso_timestamp(value: object) -> float | None:
    if not isinstance(value, str):
        return None
    try:
        return datetime.fromisoformat(value).timestamp()
    except ValueError:
        return None


def _extract_json(line: str) -> dict | None:
    raw = line.strip()
    if not raw:
        return None
    candidates = [raw] if raw.startswith("{") and raw.endswith("}") else []
    left, right = raw.find("{"), raw.rfind("}")
    if left >= 0 and right > left and raw[left : right + 1] not in candidates:
        candidates.append(raw[left : right + 1])
    for candidate in candidates:
        try:
            value = json.loads(candidate)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            return value
    return None


def parse_track_points(text: str) -> tuple[list[TrackPoint], list[dict]]:
    """Parses FastSearchHit records and returns recoverable line warnings."""
    points: list[TrackPoint] = []
    warnings: list[dict] = []
    for line_number, line in enumerate(text.splitlines(), start=1):
        value = _extract_json(line)
        if value is None or value.get("InferMode") != "FastSearchHit":
            continue
        if not all(key in value for key in ("MapName", "X", "Y")):
            continue
        try:
            point = TrackPoint(
                map_name=str(value["MapName"]),
                x=round(float(value["X"]), 3),
                y=round(float(value["Y"]), 3),
                rot=round(float(value.get("Rot", 0.0) or 0.0), 3),
                timestamp=_parse_iso_timestamp(value.get("time")),
                loc_conf=float(value.get("LocConf", 0.0) or 0.0),
                rot_conf=float(value.get("RotConf", 0.0) or 0.0),
            )
        except (KeyError, TypeError, ValueError) as exc:
            warnings.append({"line": line_number, "message": str(exc)})
            continue
        points.append(point)
    return points, warnings


def split_track_periods(
    points: list[TrackPoint], gap_seconds: float = 10.0
) -> list[TrackPeriod]:
    if not points:
        return []
    chunks: list[list[TrackPoint]] = [[points[0]]]
    for point in points[1:]:
        previous = chunks[-1][-1]
        should_split = (
            previous.timestamp is not None
            and point.timestamp is not None
            and point.timestamp - previous.timestamp > gap_seconds
        )
        if should_split:
            chunks.append([])
        chunks[-1].append(point)

    periods: list[TrackPeriod] = []
    for period_id, chunk in enumerate(chunks):
        timestamps = [point.timestamp for point in chunk if point.timestamp is not None]
        periods.append(
            TrackPeriod(
                period_id=period_id,
                points=chunk,
                start_timestamp=timestamps[0] if timestamps else None,
                end_timestamp=timestamps[-1] if timestamps else None,
            )
        )
    periods.sort(
        key=lambda period: (
            period.start_timestamp
            if period.start_timestamp is not None
            else float("-inf"),
            period.period_id,
        ),
        reverse=True,
    )
    return periods
