"""实机试跑: 把编辑器当前页签里的东西直接放到游戏里跑一遍。

路线交给 MapNavigateAction 走, 断言框交给 MapLocateAssertLocation 认一次 —— 两者都是
把复制出去的那一份节点原样跑, 不加额外判定。Agent / 控制器 / Tasker 的起法与录制完全
相同。连上游戏即跑首轮, 会话随后常驻, 改完按 F3 就能重跑, 不必重连。
"""

from __future__ import annotations

import re
import threading
import time
from pathlib import Path
from typing import Any, Callable

from agent_session import AgentSession
from connection_models import RecordingSessionConfig
from connectors import build_recording_connector
from json_import import export_assert_location_node, export_path_nodes
import key_listener
from runtime import MaaRuntime


StatusCallback = Callable[[str, str], None]
PhaseCallback = Callable[[str, str], None]
ReadyCallback = Callable[[], None]
ArmedCallback = Callable[[int, str], None]
RunStateCallback = Callable[[bool], None]
FinishedCallback = Callable[[bool, str, str], None]
PositionCallback = Callable[[dict[str, Any]], None]
ErrorCallback = Callable[[str], None]
ClosedCallback = Callable[[], None]

# 试跑节点。DirectHit 让识别恒成立, 三项延迟归零 —— 按下 F3 到真正起步之间不额外等。
NODE_NAME = "MapNavigatorDebugRunNode"
# 断言另起一个节点名: 两种形状的 override 落在同一个键上时, 合并与替换的语义不该去赌。
ASSERT_NODE_NAME = "MapNavigatorDebugAssertNode"

HOTKEY_RUN = "f3"
HOTKEY_ABORT = "f4"


class NavTestService:
    """一次试跑会话: 起 Agent、连游戏、监听 F3/F4、按需重复跑同一条路线。"""

    RUN_POLL_INTERVAL_SECONDS = 0.2
    POSITION_OBSERVER_READY_TIMEOUT_SECONDS = 2.0
    # 关会话时等工作线程收尾的上限 (要覆盖一轮 post_task 从 post_stop 中返回的时间)。
    SHUTDOWN_JOIN_TIMEOUT_SECONDS = 20.0

    def __init__(
        self,
        runtime: MaaRuntime,
        on_status: StatusCallback,
        on_phase: PhaseCallback,
        on_ready: ReadyCallback,
        on_armed: ArmedCallback,
        on_run_state: RunStateCallback,
        on_finished: FinishedCallback,
        on_position: PositionCallback,
        on_error: ErrorCallback,
        on_closed: ClosedCallback,
    ) -> None:
        self._on_status = on_status
        self._on_phase = on_phase
        self._on_ready = on_ready
        self._on_armed = on_armed
        self._on_run_state = on_run_state
        self._on_finished = on_finished
        self._on_position = on_position
        self._on_error = on_error
        self._on_closed = on_closed

        self._runtime = runtime
        self._session = AgentSession(runtime)
        self._worker_thread: threading.Thread | None = None
        self._session_config: RecordingSessionConfig | None = None

        self._alive = threading.Event()
        self._run_request = threading.Event()
        self._running = threading.Event()
        self._abort_requested = False

        self._state_lock = threading.Lock()
        self._armed_path: list[Any] = []
        self._armed_kind = "route"
        self._armed_zip = False
        self._tasker: Any = None
        self._resource: Any = None
        self._position_thread: threading.Thread | None = None
        self._position_stop = threading.Event()
        self._position_ready = threading.Event()
        self._navigation_phase_reported = False

    _POSITION_RE = re.compile(
        r"MapLocator \[status=0\].*?\[position\.zoneId=(.*?)\] "
        r"\[position\.x=([-+0-9.eE]+)\] \[position\.y=([-+0-9.eE]+)\].*?"
        r"\[position\.angle=([-+0-9.eE]+)\]"
    )
    _NAVMESH_PLANNED_RE = re.compile(r"NAVMESH (?:route planned|generated path)\.")
    _WAITING_FOR_GPS_RE = re.compile(r"Waiting for first valid GPS signal\.\.\.")
    _INITIAL_FIX_RE = re.compile(r"Initial Pos fixed:")
    _AGENT_ABORT_RE = re.compile(r"Process aborted\.")

    @staticmethod
    def _position_log_path() -> Path:
        return (
            Path(__file__).resolve().parents[2]
            / "install"
            / "agent"
            / "debug"
            / "cpp-algo"
            / "debug"
            / "maafw.log"
        )

    def _start_position_observer(self) -> None:
        self._stop_position_observer()
        self._position_stop.clear()
        self._position_ready.clear()
        self._position_thread = threading.Thread(
            target=self._position_observer_loop,
            args=(self._position_log_path(),),
            name="MapNavigatorLivePosition",
            daemon=True,
        )
        try:
            self._position_thread.start()
        except RuntimeError as exc:
            self._position_thread = None
            self._position_ready.set()
            self._on_status(f"实时寻路位置观察启动失败: {exc}", "#f59e0b")
            return
        if not self._position_ready.wait(self.POSITION_OBSERVER_READY_TIMEOUT_SECONDS):
            self._on_status("实时寻路位置观察启动超时, 本轮不显示实时轨迹。", "#f59e0b")
            self._stop_position_observer()

    def _stop_position_observer(self) -> None:
        self._position_stop.set()
        self._position_ready.set()
        thread = self._position_thread
        if thread is not None and thread.is_alive():
            thread.join(1.0)
        self._position_thread = None

    def _position_observer_loop(self, path: Path) -> None:
        offset = 0
        try:
            with path.open("r", encoding="utf-8", errors="replace") as stream:
                stream.seek(0, 2)
                offset = stream.tell()
                self._position_ready.set()
                while not self._position_stop.is_set():
                    stream.seek(offset)
                    line = stream.readline()
                    if not line:
                        time.sleep(0.04)
                        continue
                    offset = stream.tell()
                    if self._WAITING_FOR_GPS_RE.search(line):
                        self._on_phase("locating", "正在定位起点…")
                    elif self._INITIAL_FIX_RE.search(line):
                        self._on_phase("planning", "已定位起点，正在规划路线…")
                    elif self._AGENT_ABORT_RE.search(line):
                        self._abort_requested = True
                        self._on_phase("error", "导航服务异常退出，本轮试跑已中断")
                        self._on_status("导航服务在规划过程中异常退出，请查看导航日志。", "#ef4444")
                    if not self._navigation_phase_reported and self._NAVMESH_PLANNED_RE.search(line):
                        self._navigation_phase_reported = True
                        self._on_phase("navigating", "规划完成，正在执行路线…")
                    match = self._POSITION_RE.search(line)
                    if not match:
                        continue
                    zone, x, y, rot = match.groups()
                    try:
                        self._on_position(
                            {
                                "type": "position",
                                "x": float(x),
                                "y": float(y),
                                "zone": zone,
                                "rot": float(rot) % 360.0,
                            }
                        )
                    except (ValueError, TypeError):
                        continue
        except OSError as exc:
            self._on_status(f"实时寻路位置观察不可用: {exc}", "#f59e0b")
        finally:
            self._position_ready.set()

    @property
    def is_alive(self) -> bool:
        return self._alive.is_set()

    def start(self, session_config: RecordingSessionConfig) -> None:
        if self.is_alive:
            return
        self._session_config = session_config
        self._alive.set()
        self._run_request.clear()
        self._running.clear()
        self._worker_thread = threading.Thread(target=self._run, daemon=True)
        self._worker_thread.start()

    def arm(
        self,
        points: list[Any],
        *,
        exported: bool = False,
        zip_enabled: bool = False,
        assert_target: dict | None = None,
    ) -> None:
        """装载待跑的东西: 有断言框就装框, 否则装线。F3 跑的就是这一份。

        每次调用整份替换 (含 kind), 切页签重新装载时不会留下上一种形状的残留。
        编辑器路点在这里就地导出, 只有这一处口径, 进程内会话与提权子进程都经过它。
        A* 路线依赖 tier 变换与显示底图, 这些只有前端有, 故送来的已是 pipeline 节点。
        """
        if assert_target is not None:
            nodes = self._export_assert(assert_target)
            if nodes is None:
                return
            kind = "assert"
        elif exported:
            nodes, kind = list(points), "route"
        else:
            try:
                nodes = export_path_nodes(points)
            except Exception as exc:  # noqa: BLE001
                self._on_status(f"路线导出失败, 未装载: {exc}", "#ef4444")
                return
            kind = "route"
        with self._state_lock:
            self._armed_path = nodes
            self._armed_kind = kind
            self._armed_zip = bool(zip_enabled and kind == "route")
        self._on_armed(len(nodes), kind)

    def _export_assert(self, assert_target: dict) -> list[Any] | None:
        """断言框就地导出成 MapLocateAssertLocation 节点 —— 与「复制配置」同一个口径。"""
        try:
            target = tuple(float(v) for v in (assert_target.get("target") or ()))
            node = export_assert_location_node(str(assert_target.get("zone_id") or ""), target)
        except Exception as exc:  # noqa: BLE001
            self._on_status(f"断言导出失败, 未装载: {exc}", "#ef4444")
            return None
        # 导出的是 {节点名: 节点体}, 跑的时候只要节点体。
        return [next(iter(node.values()))]

    def apply_client_message(self, msg: dict) -> bool:
        """处理一条前端消息, 返回 False 表示对方要求结束会话。

        进程内端点与提权子进程收到的消息形状完全一样, 故分发表只写这一份。
        """
        kind = msg.get("type")
        if kind in ("arm", "run"):
            points = msg.get("path")
            assert_target = msg.get("assert_target")
            if isinstance(assert_target, dict):
                self.arm([], assert_target=assert_target)
            elif isinstance(points, list):
                self.arm(
                    points,
                    exported=bool(msg.get("exported")),
                    zip_enabled=bool(msg.get("zip")),
                )
            if kind == "run":
                self.trigger_run()
        elif kind == "abort":
            self.abort()
        elif kind == "stop":
            return False
        return True

    def trigger_run(self) -> None:
        """请求跑一轮 (F3 热键 / 前端按钮)。忙碌或未装载时直接忽略。"""
        if not self.is_alive:
            return
        if self._running.is_set():
            self._on_status("试跑正在进行中, 按 F4 可立即终止。", "#f59e0b")
            return
        with self._state_lock:
            armed = bool(self._armed_path)
        if not armed:
            self._on_status("尚未装载: 请先在编辑器里画出路线或断言框。", "#ef4444")
            return
        self._run_request.set()

    def abort(self) -> None:
        """立即终止本轮试跑 (F4 热键 / 前端按钮)。

        post_stop 会被 cpp 端的 should_stop 读到, 状态机在停止路径上收掉扫描与移动,
        故按下即松手, 不会留着方向键按住不放。
        """
        if not self._running.is_set():
            self._on_status("当前没有正在进行的试跑。", "#64748b")
            return
        self._abort_requested = True
        self._on_phase("stopping", "正在终止试跑…")
        self._on_status("⏹ 正在终止试跑…", "#f59e0b")
        with self._state_lock:
            tasker = self._tasker
        if tasker is not None:
            tasker.post_stop()

    def hotkey_abort(self) -> None:
        """F4: 在跑就停这一轮, 已经停着就把会话整个收掉 —— 与面板上那个按钮同一件事。"""
        if self._running.is_set():
            self.abort()
            return
        self._on_status("试跑会话已结束。", "#64748b")
        self.request_shutdown()

    def request_shutdown(self) -> None:
        """请求收场并立即返回: 工作线程醒来后自己把 Agent 与热键收干净。

        热键回调跑在监听线程上, 在那里等收尾会把监听线程按在正被拆掉的东西上。
        """
        # _alive 先落: 工作线程醒来后据此直接退出循环, 不会再起新一轮。
        self._alive.clear()
        self._run_request.set()  # 把工作线程从等待里叫醒

    def stop(self) -> None:
        """结束会话: 终止在跑的一轮, 等 Agent 与热键收干净再返回。"""
        if self._running.is_set():
            self.abort()
        self.request_shutdown()
        thread = self._worker_thread
        if thread is not None and thread.is_alive():
            thread.join(self.SHUTDOWN_JOIN_TIMEOUT_SECONDS)
        self._worker_thread = None

    def _run(self) -> None:
        try:
            if self._session_config is None:
                raise RuntimeError("试跑会话配置缺失。")

            self._on_phase("connecting", "正在启动导航服务并连接游戏…")
            self._session.open(
                build_recording_connector(self._runtime, self._session_config),
                agent_name="MapNavigateAgent",
            )
            tasker = self._session.tasker
            resource = self._session.resource

            with self._state_lock:
                self._tasker = tasker
                self._resource = resource

            self._register_hotkeys()
            key_listener.start()

            self._on_ready()
            self._on_phase("ready", "已连接游戏，正在装载试跑路线…")
            self._on_status(
                f"● 已连接游戏, 按 F3 重跑 / F4 立即终止 [{self._session_config.display_name()}]",
                "#3b82f6",
            )

            # 连游戏是「开始试跑」按下才发生的, 装好的线直接开跑, 不必再补一次 F3。
            with self._state_lock:
                armed = bool(self._armed_path)
            if armed:
                self._run_request.set()

            while self._alive.is_set():
                if not self._run_request.wait(self.RUN_POLL_INTERVAL_SECONDS):
                    continue
                self._run_request.clear()
                if not self._alive.is_set():
                    break
                self._run_once(tasker, resource)
        except Exception as exc:
            print(f"Error in navtest session: {exc}")
            import traceback

            traceback.print_exc()
            self._on_phase("error", "试跑会话启动失败")
            self._on_error(str(exc))
        finally:
            self._alive.clear()
            self._running.clear()
            self._shutdown_agent()
            self._session_config = None
            # 会话可以由 F4 自行收场, 端点不看这条就会一直挂着占住独占锁。
            self._on_closed()

    def _run_once(self, tasker: Any, resource: Any) -> None:
        with self._state_lock:
            path = list(self._armed_path)
            kind = self._armed_kind
            zip_enabled = self._armed_zip
        if not path:
            return
        if tasker.stopping or tasker.running:
            # 上一轮的终止还没落地, 此时 post 出去的任务会被立刻收掉, 表现成「按了 F3 但人不动」。
            self._on_status("上一轮还在收尾, 稍候再按 F3。", "#f59e0b")
            return

        self._abort_requested = False
        self._running.set()
        self._navigation_phase_reported = False
        self._on_run_state(True)
        if kind == "assert":
            # 跑的就是导出的那个节点原样: 识别命中即通过, 不另加判定。
            self._on_phase("preparing", "正在准备定位断言…")
            self._on_status("● 断言中 —— 按 F4 立即终止", "#ef4444")
            node_name = ASSERT_NODE_NAME
            override = {node_name: path[0]}
        else:
            self._on_phase("preparing", "正在准备路线试跑…")
            self._on_status("● 试跑中 —— 按 F4 立即终止", "#ef4444")
            node_name = NODE_NAME
            custom_action_param: dict[str, Any] = {"path": path}
            if zip_enabled:
                custom_action_param["zip"] = True
            override = {
                node_name: {
                    "recognition": "DirectHit",
                    "action": "Custom",
                    "custom_action": "MapNavigateAction",
                    # 必须是 dict: maafw 会对整个 override 做一次 json.dumps, 先序列化会双重编码。
                    "custom_action_param": custom_action_param,
                    "pre_delay": 0,
                    "post_delay": 0,
                }
            }

        resource.override_pipeline(override)

        if kind == "route":
            self._start_position_observer()
            self._on_phase("starting", "正在启动导航器…")
        else:
            self._on_phase("locating", "正在定位并检查断言区域…")
        try:
            job = tasker.post_task(node_name).wait()
            succeeded = bool(job.succeeded)
        finally:
            self._stop_position_observer()
        self._on_phase("finishing", "正在收尾本轮试跑…")
        self._running.clear()
        self._on_run_state(False)

        if self._abort_requested:
            self._on_phase("idle", "本轮试跑已终止，可按 F3 重新开始")
            self._on_finished(False, "aborted", kind)
            return
        completed_text = (
            "断言检查完成" if kind == "assert" and succeeded else
            "断言检查未通过" if kind == "assert" else
            "路线试跑完成" if succeeded else
            "路线试跑失败"
        )
        self._on_phase("finished" if succeeded else "failed", completed_text)
        self._on_finished(succeeded, "ok" if succeeded else "failed", kind)

    def _register_hotkeys(self) -> None:
        key_listener.register(HOTKEY_RUN, self.trigger_run)
        key_listener.register(HOTKEY_ABORT, self.hotkey_abort)

    def _shutdown_agent(self) -> None:
        self._stop_position_observer()
        key_listener.stop()
        with self._state_lock:
            self._tasker = None
            self._resource = None
        self._session.close()
