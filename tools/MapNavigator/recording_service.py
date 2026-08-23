from __future__ import annotations

import json
import math
import threading
import time
from dataclasses import dataclass
from typing import Callable

from agent_session import AgentSession
from connection_models import RecordingSessionConfig
from connectors import build_recording_connector
import key_listener
from model import ActionType, PathPoint, PathRecorder, normalize_zone_id
from runtime import MaaRuntime


StatusCallback = Callable[[str, str], None]
FinishedCallback = Callable[[list[PathPoint]], None]
ErrorCallback = Callable[[str], None]
LocatorDetailCallback = Callable[[str], None]
ClipboardCallback = Callable[[str, str], None]
ForceWaypointCallback = Callable[[float, float, str], None]


@dataclass(frozen=True)
class LivePosition:
    """MapLocator 最近一次有效的位置与朝向快照。"""

    x: float = 0.0
    y: float = 0.0
    zone: str = ""
    rot: float | None = None
    valid: bool = False


LivePositionCallback = Callable[[LivePosition], None]


def parse_live_position(detail: dict) -> LivePosition | None:
    """把成功的 MapLocator detail 转成规范化实时快照。"""
    if detail.get("status") != 0:
        return None

    zone_id = normalize_zone_id(detail.get("mapName", ""))
    x = detail.get("x")
    y = detail.get("y")
    if not zone_id or not _is_finite_number(x) or not _is_finite_number(y):
        return None

    raw_rot = detail.get("rot")
    rot = _normalize_heading(float(raw_rot)) if _is_finite_number(raw_rot) else None
    return LivePosition(x=float(x), y=float(y), zone=zone_id, rot=rot, valid=True)


class RecordingService:
    """
    负责 Maa Agent 生命周期与轨迹采集循环。

    UI 层只需要调用 `start/stop` 并消费回调，不再感知具体 maafw 细节。
    """

    POLL_INTERVAL_SECONDS = 0.04
    LIVE_POSITION_EMIT_INTERVAL_SECONDS = 0.1

    def __init__(
        self,
        runtime: MaaRuntime,
        on_status: StatusCallback,
        on_finished: FinishedCallback,
        on_error: ErrorCallback,
        on_locator_detail: LocatorDetailCallback | None = None,
        on_clipboard: ClipboardCallback | None = None,
        on_force_waypoint: ForceWaypointCallback | None = None,
        on_live_position: LivePositionCallback | None = None,
    ) -> None:
        self._on_status = on_status
        self._on_finished = on_finished
        self._on_error = on_error
        self._on_locator_detail = on_locator_detail
        self._on_clipboard = on_clipboard
        self._on_force_waypoint = on_force_waypoint
        self._on_live_position = on_live_position

        self._recorder = PathRecorder()
        self._runtime = runtime
        self._session = AgentSession(runtime)
        self._worker_thread: threading.Thread | None = None
        self._running_event = threading.Event()
        self._session_config: RecordingSessionConfig | None = None
        self._last_record_log_signature: tuple[object, ...] | None = None
        self._last_record_log_at = 0.0
        self._last_skip_log_signature: tuple[object, ...] | None = None
        self._last_skip_log_at = 0.0
        self._last_live_position_emit_at = 0.0
        # 首个有效定位到达前处于「预热」态（黄点提示），到达后才切到「正在录制」（红点）。
        self._first_fix_emitted = False

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
                rot=self._live_position.rot,
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
        self._last_live_position_emit_at = 0.0
        self._first_fix_emitted = False
        with self._position_lock:
            self._live_position = LivePosition()
        self._running_event.set()
        self._worker_thread = threading.Thread(target=self._run, daemon=True)
        self._worker_thread.start()

    def apply_client_message(self, msg: dict) -> bool:
        """处理一条前端消息, 返回 False 表示对方要求结束会话。录制期间只认 stop。"""
        return msg.get("type") != "stop"

    def stop(self) -> None:
        self._running_event.clear()

    def _run(self) -> None:
        try:
            if self._session_config is None:
                raise RuntimeError("录制会话配置缺失。")

            self._session.open(
                build_recording_connector(self._runtime, self._session_config),
                agent_name="MapLocatorAgent",
                pipeline_override={
                    "MapLocateNode": {"recognition": "Custom", "custom_recognition": "MapLocateRecognition"}
                },
            )
            tasker = self._session.tasker

            # 预热态：识别引擎已就绪但尚未拿到首个定位，提示用户先别动，避免开头几步被吞。
            self._on_status(
                f"● 定位预热中，请保持静止…定位成功后自动开始录制 [{self._session_config.display_name()}]",
                "#f59e0b",
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

    def _update_live_position(self, position: LivePosition) -> None:
        """由录制线程调用，更新快照并按固定频率推送给 UI。"""
        with self._position_lock:
            self._live_position = position

        now = time.monotonic()
        if self._on_live_position and (
            self._last_live_position_emit_at == 0.0
            or now - self._last_live_position_emit_at >= self.LIVE_POSITION_EMIT_INTERVAL_SECONDS
        ):
            self._last_live_position_emit_at = now
            self._on_live_position(position)

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
            f"rot={detail.get('rot', '-')!r} "
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

        position = parse_live_position(detail)
        if position is None:
            self._emit_skip_summary(detail, reason="invalid_zone_or_xy")
            return

        self._update_live_position(position)
        if not self._first_fix_emitted:
            # 首个有效定位到达：从「预热」切到「正在录制」，红点提示已真正开始记录。
            self._first_fix_emitted = True
            self._on_status(
                f"● 正在录制轨迹 [{self._session_config.display_name()}] (G:复制坐标 X:强制打点)",
                "#ef4444",
            )
        self._emit_record_summary(detail, zone_id=position.zone)
        self._recorder.update(position.x, position.y, int(ActionType.RUN), position.zone)

    def _shutdown_agent(self) -> None:
        key_listener.stop()
        self._session.close()


def _compact_number(value: float) -> int | float:
    rounded = round(float(value), 2)
    if rounded.is_integer():
        return int(rounded)
    return rounded


def _is_finite_number(value: object) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(float(value))


def _normalize_heading(value: float) -> float:
    return value % 360.0
