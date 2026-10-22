"""验证 MaaEndWebHost 的全局热键：Ctrl+Alt+H 藏/显、任务栏切换、Ctrl+Alt+Q 退出。

不放进 test_backend.py：它会**真的按全局热键**（keybd_event），
在别人正在用电脑时可能撞上别的程序的热键。单独跑：

    uv run python test_hotkeys.py
"""

from __future__ import annotations

import contextlib
import ctypes
import json
import os
import socket
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import browser
import window_style

EXE = browser.find_webview2_host()
PROF = os.path.join(os.environ["LOCALAPPDATA"], "MaaEnd", "webview2-hotkey-test")
STATE = os.path.join(PROF, "host-state.json")
HOSTLOG = os.path.join(PROF, "test-host.log")

u32 = ctypes.windll.user32
KEYUP, KEYDOWN = 0x2, 0x0
VK = {"ctrl": 0x11, "alt": 0x12, "h": 0x48, "q": 0x51, "t": 0x54, "b": 0x42, "g": 0x47}


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def ctl(port: int, payload: dict) -> dict:
    with socket.create_connection(("127.0.0.1", port), timeout=10) as s:
        s.sendall((json.dumps(payload) + "\n").encode())
        return json.loads(s.makefile().readline())


def hotkey(*keys: str) -> None:
    for k in keys:
        u32.keybd_event(VK[k], 0, KEYDOWN, 0)
    time.sleep(0.05)
    for k in reversed(keys):
        u32.keybd_event(VK[k], 0, KEYUP, 0)
    time.sleep(0.6)


def main() -> int:
    cdp_port, ctl_port = free_port(), free_port()
    proc = subprocess.Popen(
        [EXE, f"--cdp-port={cdp_port}", f"--control-port={ctl_port}", f"--profile={PROF}",
         "--size=1280x720", "--on-screen", "--url=https://example.com", f"--log={HOSTLOG}"],
        creationflags=0x08000000, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        for _ in range(200):
            if os.path.exists(STATE):
                break
            time.sleep(0.2)
        else:
            print("FAIL 宿主没起来")
            return 1
        time.sleep(2)

        print("启动:", ctl(ctl_port, {"cmd": "status"}))

        # 热键是系统级独占资源：另一个实例（比如正在跑的演示窗口）占着就会全注册失败，
        # 这时不是本用例的错，直接 SKIP 并提示，别把真失败和"被占用"混在一起。
        text = ""
        with contextlib.suppress(OSError), open(HOSTLOG, encoding="utf-8", errors="replace") as fh:
            text = fh.read()
        if "[hotkey]" in text and "藏 / 显" not in text:
            print("SKIP  热键都被别的实例占用了（先关掉正在跑的宿主再测）")
            return 0
        hotkey("ctrl", "alt", "h")
        hidden = ctl(ctl_port, {"cmd": "status"})["visible"] is False
        print(f"{'PASS' if hidden else 'FAIL'}  Ctrl+Alt+H 藏")
        hotkey("ctrl", "alt", "h")
        shown = ctl(ctl_port, {"cmd": "status"})["visible"] is True
        print(f"{'PASS' if shown else 'FAIL'}  Ctrl+Alt+H 再按回来")

        hwnd = window_style.find_main_window(proc.pid)
        before = window_style.state(hwnd)
        print(f"     任务栏切换前 exstyle={before['exstyle']} in_taskbar={before['in_taskbar']}")
        key = None
        for k in ("t", "b", "g"):          # Ctrl+Alt+T 常被占用，宿主会换备选
            hotkey("ctrl", "alt", k)
            st = window_style.state(hwnd)
            if st["in_taskbar"]:
                key = k
                print(f"PASS  Ctrl+Alt+{k.upper()} -> exstyle={st['exstyle']} in_taskbar=True")
                break
        if key is None:
            print("FAIL  任务栏热键都没生效")
        else:
            hotkey("ctrl", "alt", key)
            st = window_style.state(hwnd)
            print(f"{'PASS' if not st['in_taskbar'] else 'FAIL'}  再按一次回到 toolwindow"
                  f"（exstyle={st['exstyle']}）")

        hotkey("ctrl", "alt", "q")
        try:
            proc.wait(timeout=15)
            print(f"PASS  Ctrl+Alt+Q 优雅退出，exit={proc.returncode}")
        except subprocess.TimeoutExpired:
            print("FAIL  Ctrl+Alt+Q 没生效")
            return 1
        return 0
    finally:
        if proc.poll() is None:
            proc.kill()
        with contextlib.suppress(OSError):
            os.unlink(STATE)


if __name__ == "__main__":
    sys.exit(main())
