from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal


ConnectionKind = Literal["win32", "adb", "playcover", "linux"]


@dataclass(frozen=True)
class Win32ConnectionConfig:
    """Win32 录制所需的窗口连接配置。"""

    window_title: str = "Endfield"


@dataclass(frozen=True)
class AdbDeviceInfo:
    """ADB 设备发现结果。"""

    serial: str
    state: str
    model: str = ""
    device: str = ""
    transport_id: str = ""

    @property
    def address(self) -> str:
        return self.serial

    def display_name(self) -> str:
        details = []
        if self.model:
            details.append(self.model)
        if self.device:
            details.append(self.device)
        if self.transport_id:
            details.append(f"tid={self.transport_id}")

        suffix = f" ({', '.join(details)})" if details else ""
        state_suffix = "" if self.state == "device" else f" [{self.state}]"
        return f"{self.serial}{suffix}{state_suffix}"


@dataclass(frozen=True)
class AdbConnectionConfig:
    """ADB 录制所需的设备连接配置。"""

    adb_path: str = ""
    address: str = ""
    config: dict[str, Any] = field(default_factory=dict)


@dataclass(frozen=True)
class PlayCoverConnectionConfig:
    """PlayCover 录制所需的连接配置。"""

    address: str = "127.0.0.1:1717"
    uuid: str = "maa.playcover"


@dataclass(frozen=True)
class LinuxConnectionConfig:
    """Linux-Gamescope 录制所需的连接配置。

    由 `Toolkit.find_gamescope_instances()` 发现的一个 gamescope 实例得出:

    - `pw_node_id`: gamescope 在 PipeWire 会话中的截图节点 ID (即 GamescopeInstance 的
      `pipewire_node_id`)。LinuxController 用它直连该节点做截图 (screencap)。
    - `eis_socket_path`: libei (EIS) 输入 socket 路径 (即 GamescopeInstance 的
      `eis_socket_path`)。本工具录制时不投递按键, 但 LinuxController 仍需该字段。
    """

    pw_node_id: int = 0
    eis_socket_path: str = ""


@dataclass(frozen=True)
class RecordingSessionConfig:
    """一次录制会话的完整连接配置。"""

    kind: ConnectionKind
    win32: Win32ConnectionConfig = field(default_factory=Win32ConnectionConfig)
    adb: AdbConnectionConfig = field(default_factory=AdbConnectionConfig)
    playcover: PlayCoverConnectionConfig = field(default_factory=PlayCoverConnectionConfig)
    linux: LinuxConnectionConfig = field(default_factory=LinuxConnectionConfig)

    def display_name(self) -> str:
        if self.kind == "adb":
            target = self.adb.address or "未选择设备"
            return f"ADB / {target}"
        elif self.kind == "playcover":
            return f"PlayCover / {self.playcover.uuid}"
        elif self.kind == "linux":
            return f"Linux-Gamescope / 节点 {self.linux.pw_node_id or '未选择'}"
        return f"Win32 / {self.win32.window_title}"


def session_config_from_payload(payload: dict[str, Any]) -> RecordingSessionConfig:
    """前端连接面板的 JSON -> 录制会话配置。缺字段一律回退到各自默认值。

    住在这里而不是 serve.py: 提权会话子进程 (session_worker.py) 拿到的是同一份原始
    payload, 必须和后端走完全一样的解析。
    """
    kind = payload.get("kind", "win32")
    if kind == "adb":
        adb = payload.get("adb") or {}
        cfg = adb.get("config")
        return RecordingSessionConfig(
            kind="adb",
            adb=AdbConnectionConfig(
                adb_path=str(adb.get("adb_path", "") or ""),
                address=str(adb.get("address", "") or ""),
                config=cfg if isinstance(cfg, dict) else {},
            ),
        )
    elif kind == "playcover":
        playcover = payload.get("playcover") or {}
        return RecordingSessionConfig(
            kind="playcover",
            playcover=PlayCoverConnectionConfig(
                address=str(playcover.get("address", "127.0.0.1:1717") or "127.0.0.1:1717"),
                uuid=str(playcover.get("uuid", "maa.playcover") or "maa.playcover"),
            ),
        )
    elif kind == "linux":
        linux = payload.get("linux") or {}
        return RecordingSessionConfig(
            kind="linux",
            linux=LinuxConnectionConfig(
                pw_node_id=int(linux.get("pw_node_id", 0) or 0),
                eis_socket_path=str(linux.get("eis_socket_path", "") or ""),
            ),
        )
    win = payload.get("win32") or {}
    return RecordingSessionConfig(
        kind="win32",
        win32=Win32ConnectionConfig(window_title=str(win.get("window_title", "Endfield") or "Endfield")),
    )
