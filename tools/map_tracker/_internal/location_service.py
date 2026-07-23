import re
import threading

from .core_utils import MapName, unique_map_key
from .maa_interface import MaaInterface, MapTrackerInferResult


class LocationService:
    """Location service with integrated MAA lifecycle for single-shot infer and goal."""

    def __init__(self) -> None:
        self._maa_interface: MaaInterface | None = None
        self._infer_lock = threading.Lock()

    @staticmethod
    def _main_map_key(name: str) -> str:
        try:
            parsed = MapName.parse(name)
            return f"{parsed.map_id}:{parsed.map_level_id}"
        except ValueError:
            stem = re.sub(r"^.*[\\/]", "", name)
            stem = re.sub(r"\.[^.]+$", "", stem)
            stem = re.sub(r"_tier_\w+$", "", stem, flags=re.IGNORECASE)
            return stem.lower()

    def _is_map_match(self, inferred_map_name: str, expected_map_name: str) -> bool:
        if unique_map_key(inferred_map_name) == unique_map_key(expected_map_name):
            return True
        return self._main_map_key(inferred_map_name) == self._main_map_key(
            expected_map_name
        )

    def _ensure_initialized(self) -> None:
        if self._maa_interface is not None:
            return
        self._maa_interface = MaaInterface()
        self._maa_interface.init_controller()
        self._maa_interface.init_agent()

    def infer_once(self, expected_map_name: str) -> MapTrackerInferResult:
        self._ensure_initialized()
        with self._infer_lock:
            result = self._maa_interface.do_infer(precision=0.9)
        if not self._is_map_match(result["map_name"], expected_map_name):
            raise ValueError(
                f"Location map mismatch, expected '{expected_map_name}', got '{result['map_name']}'"
            )
        return result

    def run_goal(self, map_name: str, x: float, y: float) -> None:
        self._ensure_initialized()
        with self._infer_lock:
            self._maa_interface.do_goal(map_name, x, y)

    def cleanup(self) -> None:
        if self._maa_interface is not None:
            try:
                self._maa_interface.dispose_agent()
            except Exception:
                pass
            self._maa_interface = None
