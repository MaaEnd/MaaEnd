import subprocess
import uuid
from pathlib import Path
from typing import TypedDict

from maa.agent_client import AgentClient
from maa.tasker import Tasker, TaskDetail
from maa.toolkit import Toolkit, DesktopWindow, AdbDevice
from maa.resource import Resource
from maa.controller import (
    Controller,
    Win32Controller,
    AdbController,
    MaaWin32ScreencapMethodEnum,
    MaaWin32InputMethodEnum,
)


class MapTrackerInferResult(TypedDict):
    loc_conf: float
    map_name: str
    rot: int
    rot_conf: float
    x: float
    y: float


class MaaInitializationError(Exception):
    pass


class MaaRuntimeError(Exception):
    pass


class MaaInterface:
    TARGET_WINDOW_NAME = "Endfield"
    TARGET_WINDOW_CLASS = "UnityWndClass"

    WORK_DIR = Path("./install")
    ASSET_DIR = Path("./") / "assets"
    AGENT_PATH = WORK_DIR / "agent" / "go-service.exe"

    def __init__(self):
        self.resource = Resource()
        self.tasker = Tasker()
        self.toolkit = Toolkit()
        self.toolkit.init_option(MaaInterface.WORK_DIR)
        self.controller: Controller | None = None
        self.agent_client: AgentClient | None = None
        self.agent_process: subprocess.Popen | None = None

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

    def init_controller(self):
        if self.controller is not None:
            return

        # Init controller
        try:
            window = self._find_win32_window()
            self.controller = Win32Controller(
                window.hwnd,
                screencap_method=MaaWin32ScreencapMethodEnum.Background,
                mouse_method=MaaWin32InputMethodEnum.PostMessageWithCursorPos,
                keyboard_method=MaaWin32InputMethodEnum.PostMessage,
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

        if not MaaInterface.ASSET_DIR.exists():
            raise MaaInitializationError(
                f"Asset directory not found at {MaaInterface.ASSET_DIR}"
            )

        # Init resource and tasker
        try:
            self.resource.post_bundle(self.ASSET_DIR / "resource").wait()
            self.tasker.set_log_dir(self.WORK_DIR / "logs")
            self.tasker.bind(self.resource, self.controller)
        except Exception as e:
            raise MaaInitializationError(
                "Failed to initialize resource or tasker"
            ) from e

    def init_agent(self):
        if self.agent_client is not None:
            return

        if not self.AGENT_PATH.exists():
            raise MaaInitializationError(
                f"Agent executable not found at {self.AGENT_PATH}"
            )

        agent_id = f"__MapTrackerEditorAgent-{uuid.uuid4()}"

        try:
            self.agent_process = subprocess.Popen(
                [self.AGENT_PATH, agent_id],
                cwd=MaaInterface.WORK_DIR,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            self.agent_client = AgentClient(agent_id)
            self.agent_client.bind(self.resource)
            self.agent_client.connect()
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

    def do_infer(self) -> MapTrackerInferResult:
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
                            "precision": 0.8,
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

        if task_detail.status.succeeded:
            best_result = task_detail.nodes[0].recognition.best_result
            if best_result:
                data = best_result.detail
                return MapTrackerInferResult(
                    loc_conf=data["locConf"],
                    map_name=data["mapName"],
                    rot=data["rot"],
                    rot_conf=data["rotConf"],
                    x=data["x"],
                    y=data["y"],
                )
            raise MaaRuntimeError("Inference succeeded but no result found")
        raise MaaRuntimeError(f"Inference failed")


# Testing
if __name__ == "__main__":
    maa_interface = MaaInterface()
    try:
        maa_interface.init_controller()
        maa_interface.init_agent()
        maa_interface.do_infer()
    finally:
        maa_interface.dispose_agent()
