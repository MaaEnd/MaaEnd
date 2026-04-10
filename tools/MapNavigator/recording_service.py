from __future__ import annotations

import json
import subprocess
import threading
import time
from dataclasses import dataclass
from typing import Callable

from connection_models import RecordingSessionConfig
from connectors import build_recording_connector
import key_listener
from model import ActionType, PathPoint, PathRecorder, normalize_zone_id
from runtime import AGENT_DIR, CPP_AGENT_EXE, MAAFW_BIN_DIR, MaaRuntime, get_agent_env


StatusCallback = Callable[[str, str], None]
FinishedCallback = Callable[[list[PathPoint]], None]
ErrorCallback = Callable[[str], None]
LocatorDetailCallback = Callable[[str], None]
ClipboardCallback = Callable[[str, str], None]
ForceWaypointCallback = Callable[[float, float, str], None]


@dataclass
class LivePosition:
    """实时位置快照。"""

    x: float = 0.0
    y: float = 0.0
    zone: str = ""
    valid: bool = False


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
        on_clipboard: ClipboardCallback | None = None,
        on_force_waypoint: ForceWaypointCallback | None = None,
    ) -> None:
        self._runtime = runtime
        self._on_status = on_status
        self._on_finished = on_finished
        self._on_error = on_error
        self._on_locator_detail = on_locator_detail
        self._on_clipboard = on_clipboard
        self._on_force_waypoint = on_force_waypoint

        self._recorder = PathRecorder()
        self._agent_process: subprocess.Popen[str] | None = None
        self._worker_thread: threading.Thread | None = None
        self._running_event = threading.Event()
        self._session_config: RecordingSessionConfig | None = None
        self._last_record_log_signature: tuple[object, ...] | None = None
        self._last_record_log_at = 0.0
        self._last_skip_log_signature: tuple[object, ...] | None = None
        self._last_skip_log_at = 0.0

        # 实时位置（录制线程写入，主线程读取；热键回调线程读取）
        self._live_position = LivePosition()
        self._position_lock = threading.Lock()

    @property
    def is_running(self) -> bool:
        return self._running_event.is_set()

    @property
    def live_position(self) -> LivePosition:
        """获取录制期间的实时位置快照"""
        with self._position_lock:
            return LivePosition(
                x=self._live_position.x,
                y=self._live_position.y,
                zone=self._live_position.zone,
                valid=self._live_position.valid,
            )

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
                f"● 正在录制轨迹 [{self._session_config.display_name()}] (G:复制坐标 X:强制打点)",
                "#ef4444",
            )

            self._register_hotkeys()
            key_listener.start()

            while self._running_event.is_set():
                tasker.post_task("MapLocateNode").wait()
                self._consume_latest_result(tasker)
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


    def _update_live_position(self, x: float, y: float, zone: str) -> None:
        """由录制线程调用，更新实时位置。"""
        with self._position_lock:
            self._live_position.x = x
            self._live_position.y = y
            self._live_position.zone = zone
            self._live_position.valid = True

    def _register_hotkeys(self) -> None:
        """注册 G/X 热键回调。回调在 pynput 监听线程中即时触发。"""
        key_listener.register("g", self._handle_copy_hotkey)
        key_listener.register("x", self._handle_pin_hotkey)

    def _handle_copy_hotkey(self) -> None:
        """G 键回调：立即读取最近一次已知坐标并复制到剪贴板。"""
        pos = self.live_position
        if not pos.valid:
            return
        coord_text = f"[{_compact_number(pos.x)}, {_compact_number(pos.y)}]"
        status_text = f"📋 已复制坐标: {coord_text}  (zone: {pos.zone})"
        self._emit_locator_detail(f"Hotkey [G]: copy coords -> {coord_text} zone={pos.zone}")
        if self._on_clipboard:
            self._on_clipboard(coord_text, status_text)

    def _handle_pin_hotkey(self) -> None:
        """X 键回调：立即在最近一次已知坐标处强制打一个严格到达点。"""
        pos = self.live_position
        if not pos.valid:
            return
        self._recorder.add_waypoint(pos.x, pos.y, int(ActionType.RUN), pos.zone, strict=True)
        coord_text = f"[{_compact_number(pos.x)}, {_compact_number(pos.y)}]"
        status_text = f"📌 已在当前位置强制打点: {coord_text}  (zone: {pos.zone})"
        self._emit_locator_detail(f"Hotkey [X]: force waypoint -> {coord_text} zone={pos.zone}")
        if self._on_force_waypoint:
            self._on_force_waypoint(pos.x, pos.y, pos.zone)

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

    def _emit_record_summary(self, detail: dict, zone_id: str) -> None:
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
            f"latencyMs={detail.get('latencyMs', '-')!r}"
        )

    def _consume_latest_result(self, tasker) -> None:
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

        self._update_live_position(float(x), float(y), zone_id)
        self._emit_record_summary(detail, zone_id=zone_id)
        self._recorder.update(float(x), float(y), int(ActionType.RUN), zone_id)

    def _shutdown_agent(self) -> None:
        key_listener.stop()
        if not self._agent_process:
            return
        self._agent_process.terminate()
        self._agent_process.wait()
        self._agent_process = None


def _compact_number(value: float) -> int | float:
    rounded = round(float(value), 2)
    if rounded.is_integer():
        return int(rounded)
    return rounded
