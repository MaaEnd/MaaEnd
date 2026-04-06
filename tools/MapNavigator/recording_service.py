from __future__ import annotations

import json
import subprocess
import threading
import time
from typing import Callable

from connection_models import RecordingSessionConfig
from connectors import build_recording_connector
from model import ActionType, PathPoint, PathRecorder, is_key_pressed, normalize_zone_id
from runtime import AGENT_DIR, CPP_AGENT_EXE, MAAFW_BIN_DIR, MaaRuntime, get_agent_env


StatusCallback = Callable[[str, str], None]
FinishedCallback = Callable[[list[PathPoint]], None]
ErrorCallback = Callable[[str], None]
LocatorDetailCallback = Callable[[str], None]


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
        on_locator_detail: LocatorDetailCallback | None = None,
    ) -> None:
        self._runtime = runtime
        self._on_status = on_status
        self._on_finished = on_finished
        self._on_error = on_error
        self._on_locator_detail = on_locator_detail

        self._recorder = PathRecorder()
        self._agent_process: subprocess.Popen[str] | None = None
        self._worker_thread: threading.Thread | None = None
        self._running_event = threading.Event()
        self._session_config: RecordingSessionConfig | None = None
        self._last_record_log_signature: tuple[object, ...] | None = None
        self._last_record_log_at = 0.0
        self._last_skip_log_signature: tuple[object, ...] | None = None
        self._last_skip_log_at = 0.0

    @property
    def is_running(self) -> bool:
        return self._running_event.is_set()

    def start(self, session_config: RecordingSessionConfig) -> None:
        if self.is_running:
            return

        self._session_config = session_config
        self._recorder = PathRecorder()
        self._last_record_log_signature = None
        self._last_record_log_at = 0.0
        self._last_skip_log_signature = None
        self._last_skip_log_at = 0.0
        self._running_event.set()
        self._worker_thread = threading.Thread(target=self._run, daemon=True)
        self._worker_thread.start()

    def stop(self) -> None:
        self._running_event.clear()

    def _run(self) -> None:
        try:
            if self._session_config is None:
                raise RuntimeError("录制会话配置缺失。")

            agent_id = f"MapLocatorAgent_{int(time.time())}"
            if not CPP_AGENT_EXE.exists():
                raise FileNotFoundError(f"找不到 Agent 可执行文件: {CPP_AGENT_EXE}")

            print(f"Starting Agent process: {CPP_AGENT_EXE} {agent_id}")
            env = get_agent_env()
            self._agent_process = subprocess.Popen([str(CPP_AGENT_EXE), agent_id], cwd=str(AGENT_DIR), env=env)
            
            print(f"Waiting {self.AGENT_BOOT_WAIT_SECONDS}s for Agent to boot...")
            time.sleep(self.AGENT_BOOT_WAIT_SECONDS)
            if self._agent_process.poll() is not None:
                ret_code = self._agent_process.returncode
                raise RuntimeError(f"Agent 启动失败，进程已退出，返回码: {ret_code}")

            print("Opening runtime library...")
            self._open_runtime_library()

            print("Connecting controller...")
            connector = build_recording_connector(self._runtime, self._session_config)
            controller = connector.connect()
            print("Controller connected.")

            print("Connecting AgentClient...")
            resource = self._runtime.Resource()
            connector.attach_resource(resource)
            client = self._runtime.AgentClient(identifier=agent_id)
            client.bind(resource)
            client.connect()
            if not client.connected:
                raise RuntimeError("Agent 连接失败。")
            print("AgentClient connected.")

            resource.override_pipeline(
                {"MapLocateNode": {"recognition": "Custom", "custom_recognition": "MapLocateRecognition"}}
            )

            print("Initializing Tasker...")
            tasker = self._runtime.Tasker()
            tasker.bind(resource, controller)
            if not tasker.inited:
                raise RuntimeError("Tasker 初始化失败。")
            print("Tasker initialized.")

            self._on_status(
                f"● 正在录制轨迹 [{self._session_config.display_name()}] (Space:跳跃 F:交互 Shift:分层)",
                "#ef4444",
            )

            while self._running_event.is_set():
                action = self._read_action_from_keyboard()
                tasker.post_task("MapLocateNode").wait()
                self._consume_latest_result(tasker, action)
                time.sleep(self.POLL_INTERVAL_SECONDS)

            self._on_finished(self._recorder.recorded_path)
        except Exception as exc:
            print(f"Error in recording cycle: {exc}")
            import traceback
            traceback.print_exc()
            self._on_error(str(exc))
        finally:
            self._running_event.clear()
            self._shutdown_agent()
            self._session_config = None

    def _open_runtime_library(self) -> None:
        try:
            self._runtime.Library.open(MAAFW_BIN_DIR)
        except Exception as exc:
            # 兼容重复初始化场景，不影响后续流程。
            print(f"Opening runtime library at {MAAFW_BIN_DIR}... Error: {exc}")
            return

    @staticmethod
    def _read_action_from_keyboard() -> ActionType:
        if is_key_pressed(0x20):
            return ActionType.JUMP
        if is_key_pressed(0x46):
            return ActionType.INTERACT
        if is_key_pressed(0x10) or is_key_pressed(0x02):
            return ActionType.SPRINT
        return ActionType.RUN

    def _emit_locator_detail(self, text: str) -> None:
        timestamp = time.strftime("%H:%M:%S")
        full_text = f"[{timestamp}] {text}"
        print(full_text, flush=True)
        if self._on_locator_detail:
            self._on_locator_detail(full_text)

    def _emit_skip_summary(self, detail: dict, reason: str) -> None:
        now = time.monotonic()
        signature = (
            detail.get("status"),
            detail.get("message", ""),
            detail.get("mapName", ""),
            reason,
        )
        if signature == self._last_skip_log_signature and now - self._last_skip_log_at < 1.5:
            return

        self._last_skip_log_signature = signature
        self._last_skip_log_at = now
        self._emit_locator_detail(
            "Locator skip: "
            f"reason={reason} "
            f"status={detail.get('status')} "
            f"map={detail.get('mapName', '')!r} "
            f"msg={detail.get('message', '')!r} "
            f"x={detail.get('x', '-')!r} "
            f"y={detail.get('y', '-')!r}"
        )

    def _emit_record_summary(self, detail: dict, zone_id: str, action: ActionType) -> None:
        now = time.monotonic()
        signature = (zone_id, detail.get("status"))
        if signature == self._last_record_log_signature and now - self._last_record_log_at < 0.5:
            return

        self._last_record_log_signature = signature
        self._last_record_log_at = now
        self._emit_locator_detail(
            "Locator ok: "
            f"zone={zone_id} "
            f"x={detail.get('x', '-')!r} "
            f"y={detail.get('y', '-')!r} "
            f"conf={detail.get('locConf', '-')!r} "
            f"latencyMs={detail.get('latencyMs', '-')!r} "
            f"action={action.name}"
        )

    def _consume_latest_result(self, tasker, action: ActionType) -> None:
        node = tasker.get_latest_node("MapLocateNode")
        if not node or not node.recognition or not node.recognition.best_result:
            return

        detail = node.recognition.best_result.detail
        if isinstance(detail, str):
            try:
                detail = json.loads(detail)
            except json.JSONDecodeError:
                self._emit_locator_detail("Locator skip: reason=detail_parse_failed")
                return

        if not isinstance(detail, dict):
            return

        if detail.get("status") != 0:
            self._emit_skip_summary(detail, reason="status")
            return

        zone_id = normalize_zone_id(detail.get("mapName", ""))
        x = detail.get("x")
        y = detail.get("y")
        if not zone_id or not isinstance(x, (int, float)) or not isinstance(y, (int, float)):
            self._emit_skip_summary(detail, reason="invalid_zone_or_xy")
            return

        self._emit_record_summary(detail, zone_id=zone_id, action=action)
        self._recorder.update(float(x), float(y), int(action), zone_id)

    def _shutdown_agent(self) -> None:
        if not self._agent_process:
            return
        self._agent_process.terminate()
        self._agent_process.wait()
        self._agent_process = None
