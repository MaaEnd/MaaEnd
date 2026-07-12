from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal


ConnectionKind = Literal["win32", "adb"]


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
class RecordingSessionConfig:
    """一次录制会话的完整连接配置。"""

    kind: ConnectionKind
    win32: Win32ConnectionConfig = field(default_factory=Win32ConnectionConfig)
    adb: AdbConnectionConfig = field(default_factory=AdbConnectionConfig)

    def display_name(self) -> str:
        if self.kind == "adb":
            target = self.adb.address or "未选择设备"
            return f"ADB / {target}"
        return f"Win32 / {self.win32.window_title}"
