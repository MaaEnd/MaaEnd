import os
import queue
import re
import threading
from typing import TypeAlias

from .core_utils import MapName
from .maa_interface import MaaInterface, MapTrackerInferResult, MaaRuntimeError

QueueItem: TypeAlias = MapTrackerInferResult | Exception


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
    """Unified location service with integrated MAA lifecycle and async inference."""

    def __init__(self, *, inference_interval: float = 0.3, queue_size: int = 5):
        self._maa_interface: MaaInterface | None = None
        self._thread: threading.Thread | None = None
        self._stop_event = threading.Event()
        self._infer_lock = threading.Lock()
        self._is_recording = False
        self._expected_map_name: str | None = None
        self._inference_interval = inference_interval
        self._queue: queue.Queue[QueueItem] = queue.Queue(maxsize=queue_size)

    @property
    def is_recording(self) -> bool:
        return self._is_recording

    @property
    def result_queue(self) -> queue.Queue[QueueItem]:
        return self._queue

    @staticmethod
    def _main_map_key(name: str) -> str:
        try:
            parsed = MapName.parse(name)
            return f"{parsed.map_id}:{parsed.map_level_id}"
        except ValueError:
            stem = os.path.splitext(os.path.basename(name.replace("\\", "/")))[0]
            stem = re.sub(r"_tier_\w+$", "", stem, flags=re.IGNORECASE)
            return stem.lower()

    def _is_map_match(self, inferred_map_name: str, expected_map_name: str) -> bool:
        if unique_map_key(inferred_map_name) == unique_map_key(expected_map_name):
            return True
        return self._main_map_key(inferred_map_name) == self._main_map_key(
            expected_map_name
        )

    def _push(self, item: QueueItem) -> None:
        try:
            self._queue.put_nowait(item)
        except queue.Full:
            try:
                self._queue.get_nowait()
                self._queue.put_nowait(item)
            except (queue.Empty, queue.Full):
                pass

    def _clear_queue(self) -> None:
        while True:
            try:
                self._queue.get_nowait()
            except queue.Empty:
                break

    def _ensure_initialized(self) -> None:
        if self._maa_interface is not None:
            return
        self._maa_interface = MaaInterface()
        self._maa_interface.init_controller()
        self._maa_interface.init_agent()

    def _loop(self) -> None:
        while not self._stop_event.is_set():
            if not self._is_recording or self._expected_map_name is None:
                self._stop_event.wait(0.1)
                continue
            try:
                with self._infer_lock:
                    result = self._maa_interface.do_infer()
                if not self._is_map_match(result["map_name"], self._expected_map_name):
                    raise ValueError(
                        f"Location map mismatch, expected '{self._expected_map_name}', got '{result['map_name']}'"
                    )
                self._push(result)
            except MaaRuntimeError as e:
                self._push(e)
            except Exception as e:
                self._push(e)
            self._stop_event.wait(self._inference_interval)

    def start_recording(self, expected_map_name: str) -> bool:
        if self._is_recording:
            return True
        try:
            self._ensure_initialized()
        except Exception as e:
            self._push(e)
            return False
        self._expected_map_name = expected_map_name
        self._clear_queue()
        self._is_recording = True
        self._stop_event.clear()
        if self._thread is None or not self._thread.is_alive():
            self._thread = threading.Thread(
                target=self._loop, name="LocationServiceThread", daemon=True
            )
            self._thread.start()
        return True

    def stop_recording(self) -> None:
        self._is_recording = False

    def toggle_recording(self, expected_map_name: str) -> bool:
        if self._is_recording:
            self.stop_recording()
            return False
        return self.start_recording(expected_map_name)

    def infer_once(self, expected_map_name: str) -> MapTrackerInferResult:
        self._ensure_initialized()
        with self._infer_lock:
            result = self._maa_interface.do_infer()
        if not self._is_map_match(result["map_name"], expected_map_name):
            raise ValueError(
                f"Location map mismatch, expected '{expected_map_name}', got '{result['map_name']}'"
            )
        return result

    def cleanup(self) -> None:
        self._is_recording = False
        self._stop_event.set()
        if self._thread is not None and self._thread.is_alive():
            self._thread.join(timeout=3.0)
        self._thread = None
        while not self._queue.empty():
            try:
                self._queue.get_nowait()
            except queue.Empty:
                break
        if self._maa_interface is not None:
            try:
                self._maa_interface.dispose_agent()
            except Exception:
                pass
            self._maa_interface = None
