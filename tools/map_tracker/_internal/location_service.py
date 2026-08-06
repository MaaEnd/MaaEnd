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

    def _dispose_maa(self) -> None:
        maa = self._maa_interface
        self._maa_interface = None
        if maa is None:
            return
        try:
            maa.dispose_agent()
        except Exception:
            pass

    def _ensure_agent(self) -> None:
        """Ensures agent is ready for fixed-image recognition (no live controller)."""
        maa = self._maa_interface
        if maa is not None and maa.agent_client is not None:
            return

        self._dispose_maa()
        maa = MaaInterface()
        try:
            maa.init_agent()
        except Exception:
            try:
                maa.dispose_agent()
            except Exception:
                pass
            raise
        self._maa_interface = maa

    def _ensure_controller(self) -> None:
        """Ensures live controller + agent for screencap-based location tools."""
        maa = self._maa_interface
        if (
            maa is not None
            and maa.controller is not None
            and maa.agent_client is not None
        ):
            return

        self._dispose_maa()
        maa = MaaInterface()
        try:
            maa.init_controller()
            maa.init_agent()
        except Exception:
            try:
                maa.dispose_agent()
            except Exception:
                pass
            raise
        self._maa_interface = maa

    def infer_once(self, expected_map_name: str) -> MapTrackerInferResult:
        self._ensure_controller()
        with self._infer_lock:
            result = self._maa_interface.do_infer(precision=0.9)
        if not self._is_map_match(result["map_name"], expected_map_name):
            raise ValueError(
                f"Location map mismatch, expected '{expected_map_name}', got '{result['map_name']}'"
            )
        return result

    def infer_on_image(
        self,
        image,
        *,
        precision: float = 0.7,
        allowed_modes: int = 3,
    ) -> MapTrackerInferResult:
        if not 0.1 <= precision <= 1.0:
            raise ValueError("Precision must be between 0.1 and 1.0")
        self._ensure_agent()
        with self._infer_lock:
            return self._maa_interface.do_infer_on_image(
                image,
                precision=precision,
                allowed_modes=allowed_modes,
            )

    def run_goal(self, map_name: str, x: float, y: float) -> None:
        self._ensure_controller()
        with self._infer_lock:
            self._maa_interface.do_goal(map_name, x, y)

    def cleanup(self) -> None:
        # Do not wait on the infer lock: shutdown must kill the agent even if a
        # recognition is still running (that call will fail after process death).
        self._dispose_maa()
