import subprocess
import sys
import uuid
from pathlib import Path
from typing import TypedDict

import numpy as np

from maa.agent_client import AgentClient
from maa.tasker import Tasker, TaskDetail
from maa.pipeline import JRecognitionType, JCustomRecognition
from maa.toolkit import Toolkit, DesktopWindow, AdbDevice
from maa.resource import Resource
from maa.controller import (
    Controller,
    Win32Controller,
    AdbController,
    CustomController,
    MaaWin32ScreencapMethodEnum,
    MaaWin32InputMethodEnum,
)


class MapTrackerInferResult(TypedDict):
    infer_mode: str
    infer_time_ms: int
    loc_conf: float
    loc_time_ms: int
    map_name: str
    rot: int
    rot_conf: float
    rot_time_ms: int
    x: float
    y: float


class MaaInitializationError(Exception):
    pass


class MaaRuntimeError(Exception):
    pass


class _OfflineController(CustomController):
    """Provides a no-op controller so Tasker can run fixed-image recognition."""

    def __init__(self):
        super().__init__()
        self._connected = False

    def connect(self) -> bool:
        self._connected = True
        return True

    def connected(self) -> bool:
        return self._connected

    def request_uuid(self) -> str:
        return "MapTrackerOfflineController"

    def start_app(self, intent: str) -> bool:
        return True

    def stop_app(self, intent: str) -> bool:
        return True

    def screencap(self) -> np.ndarray:
        return np.zeros((1, 1, 3), dtype=np.uint8)

    def click(self, x: int, y: int) -> bool:
        return True

    def swipe(self, x1: int, y1: int, x2: int, y2: int, duration: int) -> bool:
        return True

    def touch_down(self, contact: int, x: int, y: int, pressure: int) -> bool:
        return True

    def touch_move(self, contact: int, x: int, y: int, pressure: int) -> bool:
        return True

    def touch_up(self, contact: int) -> bool:
        return True

    def click_key(self, keycode: int) -> bool:
        return True

    def input_text(self, text: str) -> bool:
        return True

    def key_down(self, keycode: int) -> bool:
        return True

    def key_up(self, keycode: int) -> bool:
        return True


class MaaInterface:
    TARGET_WINDOW_NAME = "Endfield"
    TARGET_WINDOW_CLASS = "UnityWndClass"

    WORK_DIR = Path("./install")
    ASSET_DIR = Path("./") / "assets"
    AGENT_PATH = (
        WORK_DIR
        / "agent"
        / ("go-service.exe" if sys.platform == "win32" else "go-service")
    )

    def __init__(self):
        self.resource = Resource()
        self.tasker = Tasker()
        self.toolkit = Toolkit()
        self.toolkit.init_option(MaaInterface.WORK_DIR)
        self.controller: Controller | None = None
        self._offline_controller: Controller | None = None
        self.agent_client: AgentClient | None = None
        self.agent_process: subprocess.Popen | None = None
        self._resource_initialized = False

    def _find_win32_window(self) -> DesktopWindow:
        # Win32
        def _calc_window_likelihood(window: DesktopWindow):
            return (
                int(window.class_name == MaaInterface.TARGET_WINDOW_CLASS) * 1
                + int(window.window_name == MaaInterface.TARGET_WINDOW_NAME) * 2
            )

        # Find window
        try:
            all_windows = sorted(
                self.toolkit.find_desktop_windows(), key=lambda w: w.hwnd
            )
            all_windows_likelihood = list(map(_calc_window_likelihood, all_windows))
            max_likelihood = max(all_windows_likelihood)
        except Exception as e:
            raise MaaInitializationError("Failed to fetch window list") from e

        if max_likelihood == 0:
            raise MaaInitializationError("No target window found")
        return all_windows[all_windows_likelihood.index(max_likelihood)]

    def _find_adb_device(self) -> AdbDevice:
        all_adb = self.toolkit.find_adb_devices()
        if not all_adb:
            raise MaaInitializationError("No adb device found")
        return all_adb[0]

    def init_resource(self):
        if self._resource_initialized:
            return

        if not MaaInterface.ASSET_DIR.exists():
            raise MaaInitializationError(
                f"Asset directory not found at {MaaInterface.ASSET_DIR}"
            )

        try:
            self.resource.post_bundle(self.ASSET_DIR / "resource").wait()
            self.tasker.set_log_dir(self.WORK_DIR / "logs")
        except Exception as e:
            raise MaaInitializationError("Failed to initialize resource") from e
        self._resource_initialized = True

    def _bind_tasker(self, controller: Controller) -> None:
        try:
            self.init_resource()
            self.tasker.bind(self.resource, controller)
        except Exception as e:
            raise MaaInitializationError("Failed to initialize tasker") from e

    def init_offline_controller(self):
        if self._offline_controller is not None:
            return

        try:
            self._offline_controller = _OfflineController()
            self._offline_controller.post_connection().wait()
            self._bind_tasker(self._offline_controller)
        except Exception as e:
            raise MaaInitializationError(
                "Failed to initialize offline controller"
            ) from e

    def init_controller(self):
        if self.controller is not None:
            return

        # Init controller
        try:
            window = self._find_win32_window()
            self.controller = Win32Controller(
                window.hwnd,
                screencap_method=MaaWin32ScreencapMethodEnum.FramePool,
                mouse_method=MaaWin32InputMethodEnum.Seize,
                keyboard_method=MaaWin32InputMethodEnum.Seize,
            )
        except Exception as e_win:
            try:
                adb = self._find_adb_device()
                self.controller = AdbController(adb.adb_path, adb.address)
            except Exception as e_adb:
                raise MaaInitializationError(
                    "Failed to initialize any available controller: "
                    f"win32={e_win!r}; adb={e_adb!r}"
                )

        try:
            self.controller.post_connection().wait()
        except Exception as e:
            raise MaaInitializationError("Failed to connect controller") from e

        self._bind_tasker(self.controller)

    def init_agent(self):
        if self.agent_client is not None:
            return

        if not self.AGENT_PATH.exists():
            raise MaaInitializationError(
                f"Agent executable not found at {self.AGENT_PATH}"
            )

        if not self.tasker.inited:
            self.init_offline_controller()
        agent_id = f"__MapTrackerEditorAgent-{uuid.uuid4()}"

        try:
            self.agent_process = subprocess.Popen(
                [self.AGENT_PATH.resolve(), agent_id],
                cwd=MaaInterface.WORK_DIR,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            self.agent_client = AgentClient(agent_id)
            self.agent_client.bind(self.resource)
            self.agent_client.set_timeout(60 * 1000)
            if not self.agent_client.connect():
                if self.agent_process.poll() is not None:
                    raise MaaInitializationError(
                        f"Agent exited early with code {self.agent_process.returncode}"
                    )
                raise MaaInitializationError("Agent client failed to connect")
        except MaaInitializationError:
            raise
        except Exception as e:
            raise MaaInitializationError(f"Failed to start agent: {e}") from e

    def dispose_agent(self):
        if self.agent_client is not None:
            try:
                if self.agent_client.connected:
                    self.agent_client.disconnect()
            except Exception:
                pass
            finally:
                self.agent_client = None

        process = self.agent_process
        self.agent_process = None
        if process is None:
            return

        try:
            if process.poll() is not None:
                return
            process.terminate()
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                process.kill()
                try:
                    process.wait(timeout=3)
                except Exception:
                    pass
        except Exception:
            pass

    def capture_screen(self) -> np.ndarray:
        if self.controller is None:
            raise MaaRuntimeError("Controller not initialized")

        self.controller.post_screencap().wait()
        image = self.controller.cached_image
        if image is None:
            raise MaaRuntimeError("Screencap succeeded but no cached image found")
        return image

    @staticmethod
    def _parse_infer_detail(task_detail: TaskDetail | None) -> MapTrackerInferResult:
        """Extracts the MapTrackerInfer result from a recognition task detail."""
        if task_detail is None:
            raise MaaRuntimeError("Inference failed: task detail not found")
        if not task_detail.status.succeeded:
            raise MaaRuntimeError("Inference failed")
        best_result = task_detail.nodes[0].recognition.best_result
        if not best_result:
            raise MaaRuntimeError("Inference succeeded but no result found")
        data = best_result.detail
        return MapTrackerInferResult(
            infer_mode=data["inferMode"],
            infer_time_ms=data["inferTimeMs"],
            loc_conf=data["locConf"],
            loc_time_ms=data["locTimeMs"],
            map_name=data["mapName"],
            rot=data["rot"],
            rot_conf=data["rotConf"],
            rot_time_ms=data["rotTimeMs"],
            x=data["x"],
            y=data["y"],
        )

    @staticmethod
    def _build_infer_param(precision: float, allowed_modes: int) -> JCustomRecognition:
        """Builds the MapTrackerInfer recognition parameter."""
        return JCustomRecognition(
            custom_recognition="MapTrackerInfer",
            custom_recognition_param={
                "map_name_regex": ".*",
                "precision": precision,
                "allowed_modes": allowed_modes,
            },
        )

    def do_infer(
        self, *, precision: float, allowed_modes: int = 3
    ) -> MapTrackerInferResult:
        if self.controller is None:
            raise MaaRuntimeError("Controller not initialized")
        if self.agent_client is None:
            raise MaaRuntimeError("Agent client not initialized")

        ENTRY_NAME = "__MapTrackerEditorInternalMapTrackerInfer"

        pipeline = {
            ENTRY_NAME: {
                "recognition": {
                    "type": "Custom",
                    "param": {
                        "custom_recognition": "MapTrackerInfer",
                        "custom_recognition_param": {
                            "map_name_regex": ".*",
                            "precision": precision,
                            "allowed_modes": allowed_modes,
                        },
                    },
                },
                "pre_delay": 0,
                "post_delay": 0,
                "timeout": 3000,
            }
        }

        task_detail: TaskDetail = (
            self.tasker.post_task(ENTRY_NAME, pipeline).wait().get()
        )
        return self._parse_infer_detail(task_detail)

    def do_infer_on_image(
        self, image: np.ndarray, *, precision: float, allowed_modes: int = 3
    ) -> MapTrackerInferResult:
        """Runs MapTrackerInfer against a fixed image instead of a live screencap."""
        if self.agent_client is None:
            raise MaaRuntimeError("Agent client not initialized")

        task_detail: TaskDetail = (
            self.tasker.post_recognition(
                JRecognitionType.Custom,
                self._build_infer_param(precision, allowed_modes),
                image,
            )
            .wait()
            .get()
        )
        return self._parse_infer_detail(task_detail)

    def do_goal(
        self,
        map_name: str,
        x: float,
        y: float,
        *,
        move_params: dict | None = None,
    ) -> None:
        """Run MapTrackerGoal to navigate to the given coordinate."""
        if self.controller is None:
            raise MaaRuntimeError("Controller not initialized")
        if self.agent_client is None:
            raise MaaRuntimeError("Agent client not initialized")

        if move_params is None:
            move_params = {
                "arrival_timeout": 30000,
                "no_ensure_initial_movement_state": True,
            }

        ENTRY_NAME = f"__MapTrackerEditorInternalMapTrackerGoal"
        pipeline = {
            ENTRY_NAME: {
                "action": {
                    "type": "Custom",
                    "param": {
                        "custom_action": "MapTrackerGoal",
                        "custom_action_param": {
                            "map_name": map_name,
                            "target": [x, y],
                            **move_params,
                        },
                    },
                },
                "pre_delay": 0,
                "post_delay": 0,
            }
        }

        task_detail: TaskDetail = (
            self.tasker.post_task(ENTRY_NAME, pipeline).wait().get()
        )

        if not task_detail.status.succeeded:
            raise MaaRuntimeError("Goal action failed")
