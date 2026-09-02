"""录制与试跑两种游戏会话的差异表: 服务怎么建、哪些消息算终结、降级提示怎么写。

进程内端点 (web/serve.py) 与提权子进程 (session_worker.py) 共用这一张表, 两者的唯一
区别是 emit 把消息推给 WebSocket 还是写进 NDJSON 通道。服务本身只认回调, 不知道自己
跑在哪一侧。
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable


Emit = Callable[[dict], None]
Log = Callable[[str], None]


def _build_record(runtime: Any, emit: Emit, log: Log, start: dict) -> Any:
    # 延迟导入: 纯编辑用户不开会话, 不该为此拉起 maafw 相关模块。
    from clipboard import copy_to_clipboard  # noqa: PLC0415
    from connection_models import session_config_from_payload  # noqa: PLC0415
    from recording_service import LivePosition, RecordingService  # noqa: PLC0415

    def on_clipboard(coord: str, status: str) -> None:
        # 游戏持有焦点, 由跑录制的这个进程直接写系统剪贴板。
        copy_to_clipboard(coord, log=log)
        emit({"type": "toast", "coord": coord, "status": status})

    def on_live_position(position: LivePosition) -> None:
        emit(
            {
                "type": "position",
                "x": position.x,
                "y": position.y,
                "zone": position.zone,
                "rot": position.rot,
            }
        )

    service = RecordingService(
        runtime=runtime,
        on_status=lambda text, color: emit({"type": "status", "text": text, "color": color}),
        on_finished=lambda points: emit({"type": "finished", "points": points}),
        on_error=lambda message: emit({"type": "error", "message": message}),
        on_locator_detail=lambda text: emit({"type": "locator", "text": text}),
        on_clipboard=on_clipboard,
        on_force_waypoint=lambda x, y, zone: emit(
            {"type": "force_waypoint", "x": x, "y": y, "zone": zone}
        ),
        on_live_position=on_live_position,
    )
    service.start(session_config_from_payload(start.get("config") or {}))
    return service


def _build_navtest(runtime: Any, emit: Emit, log: Log, start: dict) -> Any:
    from connection_models import session_config_from_payload  # noqa: PLC0415
    from navtest_service import NavTestService  # noqa: PLC0415

    service = NavTestService(
        runtime=runtime,
        on_status=lambda text, color: emit({"type": "status", "text": text, "color": color}),
        on_ready=lambda: emit({"type": "ready"}),
        on_armed=lambda count, kind: emit({"type": "armed", "count": count, "kind": kind}),
        on_run_state=lambda running: emit({"type": "run_state", "running": running}),
        on_finished=lambda ok, reason, kind: emit(
            {"type": "finished", "ok": ok, "reason": reason, "kind": kind}
        ),
        on_position=lambda position: emit(position),
        on_error=lambda message: emit({"type": "error", "message": message}),
        on_closed=lambda: emit({"type": "session_over"}),
    )
    # 先装载后启动: 连上游戏即开跑, 「开始试跑」这一下就走完连接 + 起步。
    service.arm(
        start.get("path") or [],
        exported=bool(start.get("exported")),
        zip_enabled=bool(start.get("zip")),
        assert_target=start.get("assert_target"),
    )
    service.start(session_config_from_payload(start.get("config") or {}))
    return service


@dataclass(frozen=True)
class SessionMode:
    name: str
    label: str
    build: Callable[[Any, Emit, Log, dict], Any]
    # 让本次会话就此收场的消息类型。录制一出结果即完, 试跑的 finished 只是一轮跑完,
    # 要等它自己 (F4) 或端点把会话收掉才发 session_over。
    terminal_types: tuple[str, ...]
    # 提权失败时除 status 外再补一条的通道, 前端据此把提示钉在对应面板上。
    degraded_type: str
    degraded_note: str
    eof_message: str
    # stop() 返回即已收尾 (无须再等终结消息)。
    stop_is_blocking: bool


MODES: dict[str, SessionMode] = {
    "record": SessionMode(
        name="record",
        label="录制",
        build=_build_record,
        terminal_types=("finished", "error"),
        degraded_type="locator",
        degraded_note="已退回本进程录制: 游戏窗口在前台时 G/X 热键可能收不到按键。",
        eof_message="提权录制子进程意外退出 (未返回录制结果)。",
        stop_is_blocking=False,
    ),
    "navtest": SessionMode(
        name="navtest",
        label="试跑",
        build=_build_navtest,
        terminal_types=("error", "session_over"),
        degraded_type="hotkey_degraded",
        degraded_note=(
            "已退回本进程试跑: 游戏窗口在前台时 F3/F4 可能收不到按键, 终止请改用面板上的按钮。"
        ),
        eof_message="提权试跑子进程意外退出。",
        stop_is_blocking=True,
    ),
}
