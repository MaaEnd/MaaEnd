"""窗口样式的在线调整：无边框（只留页面画面）+ 任务栏/Alt+Tab 可见性。

针对**已经在跑**的浏览器窗口，改完立刻生效，不重启、不重载页面。
跨进程操作（脚本不是那个窗口的父进程）也没问题——窗口样式是桌面上共享的属性。

用法：

    # 只看页面画面：去掉标题栏和边框，并且不进任务栏/Alt+Tab
    python window_style.py --hwnd 4724442 --borderless --no-taskbar --fit 1280x720

    # 改回来：恢复标题栏与边框、回到任务栏
    python window_style.py --hwnd 4724442 --frame --taskbar --fit 1280x720

    # 只看现在是什么状态
    python window_style.py --hwnd 4724442

`--fit` 用于无边框后把**客户区**精确调到目标尺寸：去掉边框后客户区会突然变大
（样式变了、Chromium 会按新的客户区重排），页面就不再是标定过的 1280×720。

为什么用窗口样式而不是 `--app`：`--app=URL` 只是去掉工具栏和标签条，
标题栏还在；要"只剩画面"必须动 `WS_CAPTION`/`WS_THICKFRAME`。
"""

from __future__ import annotations

import argparse
import contextlib
import ctypes
import ctypes.wintypes as wt
import sys

user32 = ctypes.WinDLL("user32", use_last_error=True)

GWL_STYLE = -16
GWL_EXSTYLE = -20

WS_CAPTION = 0x00C00000
WS_THICKFRAME = 0x00040000
WS_SYSMENU = 0x00080000
WS_MINIMIZEBOX = 0x00020000
WS_MAXIMIZEBOX = 0x00010000

WS_EX_TOOLWINDOW = 0x00000080   # 不进任务栏 / Alt+Tab
WS_EX_APPWINDOW = 0x00040000    # 强制进任务栏

SWP_NOSIZE = 0x0001
SWP_NOMOVE = 0x0002
SWP_NOZORDER = 0x0004
SWP_NOACTIVATE = 0x0010
SWP_FRAMECHANGED = 0x0020

SW_HIDE = 0
SW_SHOWNOACTIVATE = 4

# 去掉/恢复边框时涉及的样式位
_FRAME_BITS = WS_CAPTION | WS_THICKFRAME | WS_SYSMENU | WS_MINIMIZEBOX | WS_MAXIMIZEBOX


def _dpi_aware() -> None:
    """让坐标是物理像素，避免在 150% 缩放的屏上被 Windows 虚拟化。"""
    try:
        user32.SetProcessDpiAwarenessContext(ctypes.c_void_p(-4))  # PER_MONITOR_AWARE_V2
    except Exception:
        with contextlib.suppress(Exception):
            ctypes.WinDLL("shcore").SetProcessDpiAwareness(2)


def _styles(hwnd: int) -> tuple[int, int]:
    return (user32.GetWindowLongW(wt.HWND(hwnd), GWL_STYLE) & 0xFFFFFFFF,
            user32.GetWindowLongW(wt.HWND(hwnd), GWL_EXSTYLE) & 0xFFFFFFFF)


def _apply(hwnd: int, style: int | None = None, exstyle: int | None = None) -> None:
    if style is not None:
        user32.SetWindowLongW(wt.HWND(hwnd), GWL_STYLE, style)
    if exstyle is not None:
        user32.SetWindowLongW(wt.HWND(hwnd), GWL_EXSTYLE, exstyle)
    # 必须带 FRAMECHANGED，Windows 才会重新计算非客户区
    user32.SetWindowPos(wt.HWND(hwnd), None, 0, 0, 0, 0,
                        SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE | SWP_FRAMECHANGED)


def find_main_window(pid: int) -> int:
    """按进程 id 找它的主窗口（可见、面积最大的顶层窗口）。"""
    found: list[tuple[int, int]] = []

    @ctypes.WINFUNCTYPE(wt.BOOL, wt.HWND, wt.LPARAM)
    def _cb(hwnd, _lparam):
        wpid = wt.DWORD()
        user32.GetWindowThreadProcessId(hwnd, ctypes.byref(wpid))
        if wpid.value == pid and user32.IsWindowVisible(hwnd):
            _, _, w, h = window_rect(int(hwnd))
            if w * h > 0:
                found.append((w * h, int(hwnd)))
        return True

    user32.EnumWindows(_cb, 0)
    return max(found)[1] if found else 0


def window_rect(hwnd: int) -> tuple[int, int, int, int]:
    r = wt.RECT()
    user32.GetWindowRect(wt.HWND(hwnd), ctypes.byref(r))
    return r.left, r.top, r.right - r.left, r.bottom - r.top


def client_rect(hwnd: int) -> tuple[int, int]:
    r = wt.RECT()
    user32.GetClientRect(wt.HWND(hwnd), ctypes.byref(r))
    return r.right - r.left, r.bottom - r.top


def set_borderless(hwnd: int, on: bool) -> dict:
    """on=True 去掉标题栏/边框（只剩页面画面）；on=False 恢复可拖动可关闭的普通窗口。"""
    style, _ = _styles(hwnd)
    new_style = style & ~_FRAME_BITS if on else style | _FRAME_BITS
    _apply(hwnd, style=new_style)
    return {"borderless": bool(not (new_style & WS_CAPTION)),
            "style": f"0x{new_style:08X}",
            "was": f"0x{style:08X}"}


def set_taskbar(hwnd: int, on: bool) -> dict:
    """on=True 回到任务栏/Alt+Tab；on=False 改成工具窗口（隐形）。"""
    _, exstyle = _styles(hwnd)
    new_ex = ((exstyle & ~WS_EX_TOOLWINDOW) | WS_EX_APPWINDOW if on
              else (exstyle & ~WS_EX_APPWINDOW) | WS_EX_TOOLWINDOW)
    _apply(hwnd, exstyle=new_ex)
    # 任务栏条目是窗口创建时登记的，改完要藏一下再显示才会刷新
    user32.ShowWindow(wt.HWND(hwnd), SW_HIDE)
    user32.ShowWindow(wt.HWND(hwnd), SW_SHOWNOACTIVATE)
    return {"in_taskbar": bool(new_ex & WS_EX_APPWINDOW),
            "exstyle": f"0x{new_ex:08X}",
            "was": f"0x{exstyle:08X}"}


def fit_client(hwnd: int, width: int, height: int, tolerance: int = 0) -> tuple[int, int]:
    """调整窗口外框，让客户区（= 页面视口）等于 width×height。返回最终客户区尺寸。"""
    for _ in range(4):
        _, _, w, h = window_rect(hwnd)
        cw, ch = client_rect(hwnd)
        dw, dh = width - cw, height - ch
        if abs(dw) <= tolerance and abs(dh) <= tolerance:
            break
        user32.SetWindowPos(wt.HWND(hwnd), None, 0, 0, w + dw, h + dh,
                            SWP_NOMOVE | SWP_NOZORDER | SWP_NOACTIVATE)
    return client_rect(hwnd)


def state(hwnd: int) -> dict:
    style, exstyle = _styles(hwnd)
    return {"hwnd": hwnd,
            "rect": window_rect(hwnd),
            "client": client_rect(hwnd),
            "style": f"0x{style:08X}",
            "exstyle": f"0x{exstyle:08X}",
            "borderless": not bool(style & WS_CAPTION),
            "in_taskbar": bool(exstyle & WS_EX_APPWINDOW),
            "toolwindow": bool(exstyle & WS_EX_TOOLWINDOW),
            "visible": bool(user32.IsWindowVisible(wt.HWND(hwnd)))}


def main() -> int:
    ap = argparse.ArgumentParser(description="在线调整窗口样式（无边框 / 任务栏可见性）")
    ap.add_argument("--hwnd", type=lambda v: int(v, 0), help="目标窗口句柄")
    ap.add_argument("--pid", type=int, help="按进程 id 自动找主窗口（与 --hwnd 二选一）")
    ap.add_argument("--borderless", action="store_true", help="去掉标题栏与边框")
    ap.add_argument("--frame", action="store_true", help="恢复标题栏与边框")
    ap.add_argument("--taskbar", action="store_true", help="回到任务栏/Alt+Tab")
    ap.add_argument("--no-taskbar", action="store_true", help="改用工具窗口（隐形）")
    ap.add_argument("--move", help="移到屏幕坐标，如 120,120")
    ap.add_argument("--fit", help="把客户区调到 WxH，如 1280x720")
    a = ap.parse_args()

    if sys.platform != "win32":
        print("仅 Windows 支持", file=sys.stderr)
        return 2

    _dpi_aware()
    hwnd = a.hwnd or (find_main_window(a.pid) if a.pid else 0)
    if not hwnd:
        print("找不到窗口：给 --hwnd 或 --pid", file=sys.stderr)
        return 2
    before = state(hwnd)
    if a.borderless:
        print("borderless:", set_borderless(hwnd, True))
    if a.frame:
        print("frame     :", set_borderless(hwnd, False))
    if a.taskbar:
        print("taskbar   :", set_taskbar(hwnd, True))
    if a.no_taskbar:
        print("no-taskbar:", set_taskbar(hwnd, False))
    if a.move:
        x, y = (int(v) for v in a.move.split(","))
        _, _, w, h = window_rect(hwnd)
        user32.SetWindowPos(wt.HWND(hwnd), None, x, y, w, h, SWP_NOZORDER | SWP_NOACTIVATE)
    if a.fit:
        fw, fh = (int(v) for v in a.fit.lower().split("x"))
        print("fit       :", fit_client(hwnd, fw, fh))

    print("before:", before)
    print("after :", state(hwnd))
    return 0


if __name__ == "__main__":
    sys.exit(main())
