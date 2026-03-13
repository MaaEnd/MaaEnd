import json
import os
from datetime import datetime, timezone

from .core_utils import MapName

SERVICE_LOG_FILE = "install/debug/go-service.log"


def unique_map_key(name: str) -> str:
    """Normalize map name for semantic comparison."""
    try:
        parsed = MapName.parse(name)
        if parsed.map_type == "tier":
            if not parsed.tier_suffix:
                return f"{parsed.map_type}:{parsed.map_id}:{parsed.map_level_id}"
            return (
                f"{parsed.map_type}:{parsed.map_id}:"
                f"{parsed.map_level_id}:{parsed.tier_suffix}"
            )
        return f"{parsed.map_type}:{parsed.map_id}:{parsed.map_level_id}"
    except ValueError:
        basename = os.path.basename(name.replace("\\", "/"))
        stem, _ = os.path.splitext(basename)
        return stem.lower()


class LocationService:
    """Read locations from a jsonl service log."""

    MESSAGE_KEYWORDS = ("Map tracking inference completed",)

    def __init__(self, log_file: str = SERVICE_LOG_FILE):
        self.log_file = log_file
        self._offset = 0
        self._buffer = b""
        self._last_map_key: str | None = None
        self._last_start_time = 0.0

    def _is_target_message(self, message: str | None) -> bool:
        if not message:
            return False
        return any(key in message for key in self.MESSAGE_KEYWORDS)

    def _parse_timestamp(self, value) -> float | None:
        if isinstance(value, (int, float)):
            return float(value)
        if isinstance(value, str):
            try:
                return float(value)
            except ValueError:
                pass
            try:
                if value.endswith("Z"):
                    value = value[:-1] + "+00:00"
                parsed = datetime.fromisoformat(value)
                if parsed.tzinfo is None:
                    parsed = parsed.replace(tzinfo=timezone.utc)
                return parsed.timestamp()
            except ValueError:
                return None
        return None

    def _parse_location_line(self, line: str, expected_map_name: str) -> dict | None:
        try:
            data_obj = json.loads(line)
        except Exception:
            return None
        if not isinstance(data_obj, dict):
            return None
        if not self._is_target_message(data_obj.get("message") or data_obj.get("msg")):
            return None

        log_map_name = data_obj.get("MapName")
        if not log_map_name:
            return None
        if unique_map_key(log_map_name) != unique_map_key(expected_map_name):
            return None

        x = data_obj.get("X")
        y = data_obj.get("Y")
        if x is None or y is None:
            return None
        try:
            x = float(x)
            y = float(y)
        except (TypeError, ValueError):
            return None

        ts = None
        for key in ("time", "timestamp", "ts"):
            if key in data_obj:
                ts = self._parse_timestamp(data_obj.get(key))
                if ts is not None:
                    break

        return {
            "x": x,
            "y": y,
            "timestamp": ts,
            "raw": data_obj,
        }

    def get_locations(self, expected_map_name: str, start_time: float) -> list[dict]:
        if not os.path.exists(self.log_file):
            return []

        map_key = unique_map_key(expected_map_name)
        if self._last_map_key != map_key or start_time < self._last_start_time:
            self._offset = 0
            self._buffer = b""
        self._last_map_key = map_key
        self._last_start_time = start_time

        results: list[dict] = []
        try:
            with open(self.log_file, "rb") as f:
                f.seek(0, os.SEEK_END)
                end_pos = f.tell()
                if end_pos < self._offset:
                    self._offset = 0
                    self._buffer = b""
                if end_pos > self._offset:
                    f.seek(self._offset, os.SEEK_SET)
                    data = f.read(end_pos - self._offset)
                    self._offset = end_pos
                    if data:
                        self._buffer += data

            if self._buffer:
                lines = self._buffer.split(b"\n")
                self._buffer = lines[-1]
                for raw in lines[:-1]:
                    if not raw:
                        continue
                    line = raw.decode("utf-8", errors="ignore")
                    if not line.strip():
                        continue
                    record = self._parse_location_line(line, expected_map_name)
                    if record is None:
                        continue
                    ts = record.get("timestamp")
                    if ts is None or ts < start_time:
                        continue
                    results.append(record)
        except Exception:
            return []

        results.sort(key=lambda item: item.get("timestamp") or 0.0)
        deduped: list[dict] = []
        last_xy: tuple[float, float] | None = None
        for item in results:
            x = item.get("x")
            y = item.get("y")
            if x is None or y is None:
                continue
            xy = (round(x, 1), round(y, 1))
            if last_xy == xy:
                continue
            deduped.append(item)
            last_xy = xy
        return deduped
