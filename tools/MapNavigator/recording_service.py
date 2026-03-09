from __future__ import annotations

import ctypes
import json
import subprocess
import threading
import time
from typing import Callable

from model import ActionType, PathPoint, PathRecorder, is_key_pressed
from runtime import AGENT_DIR, CPP_AGENT_EXE, MAAFW_BIN_DIR, MaaRuntime


StatusCallback = Callable[[str, str], None]
FinishedCallback = Callable[[list[PathPoint]], None]
ErrorCallback = Callable[[str], None]


class RecordingService:
    """
    负责 Maa Agent 生命周期与轨迹采集循环。

    UI 层只需要调用 `start/stop` 并消费回调，不再感知具体 maafw 细节。
    """

    POLL_INTERVAL_SECONDS = 0.04
    AGENT_BOOT_WAIT_SECONDS = 2.0

    def __init__(
        self,
        runtime: MaaRuntime,
        on_status: StatusCallback,
        on_finished: FinishedCallback,
        on_error: ErrorCallback,
    ) -> None:
        self._runtime = runtime
        self._on_status = on_status
        self._on_finished = on_finished
        self._on_error = on_error

        self._recorder = PathRecorder()
        self._agent_process: subprocess.Popen[str] | None = None
        self._worker_thread: threading.Thread | None = None
        self._running_event = threading.Event()

    @property
    def is_running(self) -> bool:
        return self._running_event.is_set()

    def start(self) -> None:
        if self.is_running:
            return

        self._recorder = PathRecorder()
        self._running_event.set()
        self._worker_thread = threading.Thread(target=self._run, daemon=True)
        self._worker_thread.start()

    def stop(self) -> None:
        self._running_event.clear()

    def _run(self) -> None:
        try:
            agent_id = f"MapLocatorAgent_{int(time.time())}"
            self._agent_process = subprocess.Popen([str(CPP_AGENT_EXE), agent_id], cwd=str(AGENT_DIR))
            time.sleep(self.AGENT_BOOT_WAIT_SECONDS)
            self._open_runtime_library()

            hwnd = self._find_game_window()
            if not hwnd:
                raise RuntimeError("未找到游戏窗口，请确保游戏已运行且未被最小化。")

            controller = self._runtime.Win32Controller(hWnd=hwnd)
            controller.post_connection().wait()

            resource = self._runtime.Resource()
            client = self._runtime.AgentClient(identifier=agent_id)
            client.bind(resource)
            client.connect()
            if not client.connected:
                raise RuntimeError("Agent 连接失败。")

            resource.override_pipeline(
                {"MapLocateNode": {"recognition": "Custom", "custom_recognition": "MapLocateRecognition"}}
            )

            tasker = self._runtime.Tasker()
            tasker.bind(resource, controller)
            if not tasker.inited:
                raise RuntimeError("Tasker 初始化失败。")

            self._on_status("● 正在录制轨迹... (Space:跳跃 F:交互 Shift:分层)", "#ef4444")

            while self._running_event.is_set():
                action = self._read_action_from_keyboard()
                tasker.post_task("MapLocateNode").wait()
                self._consume_latest_result(tasker, action)
                time.sleep(self.POLL_INTERVAL_SECONDS)

            self._on_finished(self._recorder.recorded_path)
        except Exception as exc:
            self._on_error(str(exc))
        finally:
            self._running_event.clear()
            self._shutdown_agent()

    def _open_runtime_library(self) -> None:
        try:
            self._runtime.Library.open(MAAFW_BIN_DIR)
        except Exception:
            # 兼容重复初始化场景，不影响后续流程。
            return

    @staticmethod
    def _find_game_window() -> int:
        enum_windows = ctypes.windll.user32.EnumWindows
        enum_windows_proc = ctypes.WINFUNCTYPE(
            ctypes.c_bool,
            ctypes.POINTER(ctypes.c_int),
            ctypes.POINTER(ctypes.c_int),
        )
        get_window_text = ctypes.windll.user32.GetWindowTextW
        is_window_visible = ctypes.windll.user32.IsWindowVisible
        get_class_name = ctypes.windll.user32.GetClassNameW

        result = [0]

        def foreach(hwnd, _l_param):
            if not is_window_visible(hwnd):
                return True

            title_buffer = ctypes.create_unicode_buffer(512)
            get_window_text(hwnd, title_buffer, 512)
            title = title_buffer.value

            class_buffer = ctypes.create_unicode_buffer(512)
            get_class_name(hwnd, class_buffer, 512)
            class_name = class_buffer.value

            if not title:
                return True

            title_match = any(keyword in title for keyword in ("Endfield", "EndField", "终末地"))
            class_match = (
                "Unity" in class_name
                or "Unreal" in class_name
                or "Windows.UI.Core.CoreWindow" not in class_name
            )
            if title_match and class_match:
                result[0] = hwnd
                return False
            return True

        enum_windows(enum_windows_proc(foreach), 0)
        return result[0]

    @staticmethod
    def _read_action_from_keyboard() -> ActionType:
        if is_key_pressed(0x20):
            return ActionType.JUMP
        if is_key_pressed(0x46):
            return ActionType.INTERACT
        if is_key_pressed(0x10) or is_key_pressed(0x02):
            return ActionType.SPRINT
        return ActionType.RUN

    def _consume_latest_result(self, tasker, action: ActionType) -> None:
        node = tasker.get_latest_node("MapLocateNode")
        if not node or not node.recognition or not node.recognition.best_result:
            return

        detail = node.recognition.best_result.detail
        if isinstance(detail, str):
            detail = json.loads(detail)

        if not isinstance(detail, dict) or detail.get("status") != 0:
            return

        self._recorder.update(
            detail.get("x", 0),
            detail.get("y", 0),
            int(action),
            detail.get("mapName", ""),
        )

    def _shutdown_agent(self) -> None:
        if not self._agent_process:
            return
        self._agent_process.terminate()
        self._agent_process.wait()
        self._agent_process = None
