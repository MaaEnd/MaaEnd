from __future__ import annotations

import json
import os
import sys
from dataclasses import asdict, dataclass, field
from pathlib import Path

from connection_models import ConnectionKind


SETTINGS_DIR = Path.home() / ".maaend"
SETTINGS_PATH = SETTINGS_DIR / "mapnavigator.json"

CONNECTION_KINDS: tuple[ConnectionKind, ...] = ("win32", "adb", "playcover", "wlroots")


def default_wlroots_socket_path() -> str:
    """默认 Wayland socket 路径: ``$XDG_RUNTIME_DIR/wayland-0``。

    不跟 ``$WAYLAND_DISPLAY`` 走 —— 游戏通常跑在嵌套合成器 (如 gamescope) 上,
    桌面会话的 socket 才是 ``$WAYLAND_DISPLAY`` 指向的那个, 连错会截到桌面。
    """
    runtime_dir = os.environ.get("XDG_RUNTIME_DIR", "").strip()
    if runtime_dir:
        return os.path.join(runtime_dir, "wayland-0")
    uid = getattr(os, "getuid", lambda: 0)()
    return f"/run/user/{uid}/wayland-0"


def supported_connection_kinds() -> tuple[ConnectionKind, ...]:
    """当前系统真正能连上的方式：句柄只有 Windows 有，PlayCover 只有 macOS 有，
    WlRoots 只有 Linux 有，ADB 到处都有。"""
    if sys.platform == "win32":
        return ("win32", "adb")
    if sys.platform == "darwin":
        return ("playcover", "adb")
    if sys.platform.startswith("linux"):
        return ("wlroots", "adb")
    return ("adb",)


def default_connection_kind() -> ConnectionKind:
    return supported_connection_kinds()[0]


@dataclass
class MapNavigatorSettings:
    """MapNavigator GUI 本地用户设置。"""

    connection_kind: ConnectionKind = field(default_factory=default_connection_kind)
    adb_path: str = ""
    adb_address: str = ""
    win32_window_title: str = "Endfield"
    playcover_uuid: str = "maa.playcover"
    playcover_address: str = "127.0.0.1:1717"
    wlroots_socket_path: str = field(default_factory=default_wlroots_socket_path)
    recent_adb_targets: list[str] = field(default_factory=list)


class MapNavigatorSettingsStore:
    """将用户偏好保存到用户目录，避免污染仓库工作区。"""

    def __init__(self, path: Path = SETTINGS_PATH) -> None:
        self._path = path

    def load(self) -> MapNavigatorSettings:
        if not self._path.exists():
            return MapNavigatorSettings()

        try:
            payload = json.loads(self._path.read_text(encoding="utf-8"))
        except Exception:
            return MapNavigatorSettings()

        if not isinstance(payload, dict):
            return MapNavigatorSettings()

        defaults = MapNavigatorSettings()
        merged = {
            "connection_kind": payload.get("connection_kind", defaults.connection_kind),
            "adb_path": payload.get("adb_path", defaults.adb_path),
            "adb_address": payload.get("adb_address", defaults.adb_address),
            "win32_window_title": payload.get("win32_window_title", defaults.win32_window_title),
            "playcover_uuid": payload.get("playcover_uuid", defaults.playcover_uuid),
            "playcover_address": payload.get("playcover_address", defaults.playcover_address),
            "wlroots_socket_path": payload.get("wlroots_socket_path", defaults.wlroots_socket_path),
            "recent_adb_targets": payload.get("recent_adb_targets", defaults.recent_adb_targets),
        }
        if merged["connection_kind"] not in CONNECTION_KINDS:
            merged["connection_kind"] = defaults.connection_kind
        if not isinstance(merged["recent_adb_targets"], list):
            merged["recent_adb_targets"] = []
        merged["recent_adb_targets"] = [str(item) for item in merged["recent_adb_targets"] if str(item).strip()]
        if not str(merged["wlroots_socket_path"] or "").strip():
            merged["wlroots_socket_path"] = defaults.wlroots_socket_path
        return MapNavigatorSettings(**merged)

    def save(self, settings: MapNavigatorSettings) -> None:
        self._path.parent.mkdir(parents=True, exist_ok=True)
        self._path.write_text(json.dumps(asdict(settings), indent=4, ensure_ascii=False), encoding="utf-8")
