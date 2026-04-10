# /// script
# dependencies = [
#   "pillow",
#   "maafw",
#   "pynput",
# ]
# ///

from __future__ import annotations

import ctypes
import sys
import tkinter as tk

import key_listener
from app_tk import RouteEditorApp


def configure_windows_dpi() -> None:
    """开启 DPI 感知，避免高缩放下 UI 模糊。"""
    try:
        ctypes.windll.shcore.SetProcessDpiAwareness(1)
        return
    except Exception:
        pass

    try:
        ctypes.windll.user32.SetProcessDPIAware()
    except Exception:
        return


def main() -> None:
    # 检测并尝试获取全局按键监听所需的系统权限
    if not key_listener.ensure_privileges():
        sys.exit(0)

    configure_windows_dpi()
    root = tk.Tk()
    RouteEditorApp(root)
    root.mainloop()


if __name__ == "__main__":
    main()
