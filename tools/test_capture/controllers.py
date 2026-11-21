"""按 interface.json 的定义创建控制器。"""

# pyright: reportMissingTypeStubs=false

from __future__ import annotations

import os
import re
import shutil
from pathlib import Path
from typing import Any, Protocol, cast

from .flow import FlowError
from .runtime import controller_entry
from .types import Controller


class AdbDevice(Protocol):
    """maa 的 AdbDevice。"""

    adb_path: str
    address: str


class DesktopWindow(Protocol):
    """maa 的 DesktopWindow。"""

    hwnd: int
    class_name: str
    window_name: str


def find_adb() -> str | None:
    """按 PATH、ANDROID_HOME、ANDROID_SDK_ROOT 的顺序找 adb。"""
    found = shutil.which("adb")
    if found:
        return found
    for key in ("ANDROID_HOME", "ANDROID_SDK_ROOT", "ANDROID_SDK"):
        root = os.environ.get(key)
        if not root:
            continue
        candidate = Path(root) / "platform-tools" / ("adb.exe" if os.name == "nt" else "adb")
        if candidate.is_file():
            return str(candidate)
    return None


def pick_adb_device(adb_path: str | None, serial: str | None) -> tuple[str, str]:
    """返回 (adb 可执行文件, 设备地址)。"""
    from maa.toolkit import Toolkit

    devices: list[AdbDevice] = cast("list[AdbDevice]", Toolkit.find_adb_devices(adb_path))
    resolved_adb = adb_path or (devices[0].adb_path if devices else None) or find_adb()
    if resolved_adb is None:
        raise FlowError("找不到 adb，请用 --adb-path 指定，或设置 ANDROID_HOME")
    if serial:
        return resolved_adb, serial
    if not devices:
        raise FlowError("没有找到可用的安卓设备，请先启动模拟器或用 --adb-serial 指定地址")
    if len(devices) > 1:
        addresses = [device.address for device in devices]
        raise FlowError(f"找到多个设备，请用 --adb-serial 指定一个：{addresses}")
    return devices[0].adb_path, devices[0].address


def find_window(win32_config: dict[str, Any]) -> int:
    from maa.toolkit import Toolkit

    class_regex = str(win32_config.get("class_regex") or "")
    window_regex = str(win32_config.get("window_regex") or "")
    windows: list[DesktopWindow] = cast("list[DesktopWindow]", Toolkit.find_desktop_windows())
    matched = [
        window
        for window in windows
        if (not class_regex or re.search(class_regex, window.class_name))
        and (not window_regex or re.search(window_regex, window.window_name))
    ]
    if not matched:
        raise FlowError(f"没有找到窗口（class_regex={class_regex!r} window_regex={window_regex!r}），请先打开游戏")
    if len(matched) > 1:
        titles = [f"{window.class_name} / {window.window_name}" for window in matched]
        raise FlowError(f"匹配到多个窗口，无法确定用哪一个：{titles}")
    return matched[0].hwnd


def create_controller(
    interface: dict[str, Any],
    name: str,
    adb_path: str | None,
    adb_serial: str | None,
) -> tuple[Controller, str]:
    """返回 (控制器, 用于日志的说明)。"""
    entry = controller_entry(interface, name)
    kind = entry.get("type")
    if kind == "Adb":
        from maa.controller import AdbController

        resolved_adb, address = pick_adb_device(adb_path, adb_serial)
        return cast(Controller, AdbController(resolved_adb, address)), f"ADB {address}"
    if kind == "Win32":
        from maa.controller import Win32Controller
        from maa.define import MaaWin32InputMethodEnum, MaaWin32ScreencapMethodEnum

        config: dict[str, Any] = entry.get("win32") or {}
        screencap = MaaWin32ScreencapMethodEnum[str(config.get("screencap") or "ScreenDC")]
        mouse = MaaWin32InputMethodEnum[str(config.get("mouse") or "Seize")]
        keyboard = MaaWin32InputMethodEnum[str(config.get("keyboard") or "Seize")]
        hwnd = find_window(config)
        controller = cast(Controller, Win32Controller(hwnd, screencap, mouse, keyboard))
        return controller, f"Win32 窗口句柄 {hwnd}"
    raise FlowError(f"控制器 {name} 的类型是 {kind}，这个脚本目前只支持 Adb 和 Win32")
