# /// script
# dependencies = [
#   "pillow",
#   "maafw",
# ]
# ///

from __future__ import annotations

import ctypes
import tkinter as tk

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
    configure_windows_dpi()
    root = tk.Tk()
    RouteEditorApp(root)
    root.mainloop()


if __name__ == "__main__":
    main()
