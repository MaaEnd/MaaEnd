from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal


ConnectionKind = Literal["win32", "adb", "playcover", "wlroots"]


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
class WlRootsConnectionConfig:
    """WlRoots (Linux Wayland) 录制所需的连接配置。

    wlr_socket_path 是合成器的 Wayland socket 完整路径, 例如
    ``/run/user/1000/wayland-0``。注意它不一定是 ``$WAYLAND_DISPLAY`` 指向的
    那个 socket —— 游戏通常跑在嵌套合成器 (如 gamescope) 上, 连错成桌面会话会
    截到桌面而不是游戏。
    """

    wlr_socket_path: str = ""


@dataclass(frozen=True)
class RecordingSessionConfig:
    """一次录制会话的完整连接配置。"""

    kind: ConnectionKind
    win32: Win32ConnectionConfig = field(default_factory=Win32ConnectionConfig)
    adb: AdbConnectionConfig = field(default_factory=AdbConnectionConfig)
    playcover: PlayCoverConnectionConfig = field(default_factory=PlayCoverConnectionConfig)
    wlroots: WlRootsConnectionConfig = field(default_factory=WlRootsConnectionConfig)

    def display_name(self) -> str:
        if self.kind == "adb":
            target = self.adb.address or "未选择设备"
            return f"ADB / {target}"
        elif self.kind == "playcover":
            return f"PlayCover / {self.playcover.uuid}"
        elif self.kind == "wlroots":
            return f"WlRoots / {self.wlroots.wlr_socket_path or '未指定 socket'}"
        return f"Win32 / {self.win32.window_title}"


def session_config_from_payload(payload: dict[str, Any]) -> RecordingSessionConfig:
    """前端连接面板的 JSON -> 录制会话配置。缺字段一律回退到各自默认值。

    住在这里而不是 serve.py: 提权录制子进程 (record_worker.py) 拿到的是同一份原始
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
    elif kind == "wlroots":
        wlroots = payload.get("wlroots") or {}
        return RecordingSessionConfig(
            kind="wlroots",
            wlroots=WlRootsConnectionConfig(
                wlr_socket_path=str(wlroots.get("wlr_socket_path", "") or ""),
            ),
        )
    win = payload.get("win32") or {}
    return RecordingSessionConfig(
        kind="win32",
        win32=Win32ConnectionConfig(window_title=str(win.get("window_title", "Endfield") or "Endfield")),
    )
