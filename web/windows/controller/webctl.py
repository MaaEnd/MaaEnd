"""对**已经在跑**的后端做窗口操作：显示 / 藏起来 / 关闭 / 任务栏 / 无边框。

三类动作分开，别混：

| 命令 | 语义 | 自动化还能跑吗 |
|---|---|---|
| `show` | 窗口摆到屏幕上，用户看得见、能点 | ✅ |
| `park` / `hide` | 挪回屏幕外（看不见但仍渲染） | ✅ |
| `close` | 优雅关闭后端（先 flush profile，保住登录态） | ❌ |

实例怎么找：给 `--profile <用户数据目录>`（读 `backend.json` 或 `host-state.json`），
或者直接给 `--port <CDP 端口>` / `--control-port <宿主控制口>`。

    # Edge 后端
    python webctl.py --profile "%LOCALAPPDATA%\\MaaEnd\\web-profile" status
    python webctl.py --profile "%LOCALAPPDATA%\\MaaEnd\\web-profile" hide
    python webctl.py --profile "%LOCALAPPDATA%\\MaaEnd\\web-profile" close

    # WebView2 宿主
    python webctl.py --profile "%LOCALAPPDATA%\\MaaEnd\\webview2-demo" show --at 120,120
    python webctl.py --profile "%LOCALAPPDATA%\\MaaEnd\\webview2-demo" taskbar on
    python webctl.py --profile "%LOCALAPPDATA%\\MaaEnd\\webview2-demo" close

`hide` 对 Edge 后端就是"挪到屏幕外"——浏览器没有"停车"原语，真停车只有 WebView2 宿主能给。
"""

from __future__ import annotations

import argparse
import contextlib
import json
import os
import sys
import time
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import browser  # 同目录：允许 `python webctl.py` 直接跑

BROWSER_STATE = "backend.json"
HOST_STATE = "host-state.json"


def find_instance(profile: str | None, port: int | None, control_port: int | None) -> dict:
    info: dict = {}
    if profile:
        prof = Path(os.path.expandvars(profile)).expanduser()
        for name in (HOST_STATE, BROWSER_STATE):
            f = prof / name
            if f.exists():
                info.update(json.loads(f.read_text(encoding="utf-8")))
                info["state_file"] = str(f)
                info["profile"] = str(prof)
                break
    if port:
        info["port"] = port
        info.setdefault("kind", "browser")
    if control_port:
        info["control_port"] = control_port
        info["kind"] = "webview2"
    if not info:
        raise SystemExit("找不到实例：给 --profile（含 backend.json / host-state.json）"
                         "或 --port / --control-port")
    return info


def is_webview2(info: dict) -> bool:
    return bool(info.get("control_port")) and (info.get("kind") == "webview2"
                                               or "cdp_port" in info)


def _cdp(info: dict):
    """连 CDP；没装 websocket-client 或连不上就抛异常，调用方自己回落。"""
    from cdp_client import CDPClient
    port = int(info.get("port") or info.get("cdp_port") or 0)
    c = CDPClient(port=port, host="127.0.0.1")
    c.connect()
    return c


def _ws(hwnd: int):
    import window_style
    return window_style


def _hwnd(info: dict) -> int:
    pid = int(info.get("pid") or 0)
    if not pid:
        return 0
    with contextlib.suppress(Exception):
        import window_style
        return window_style.find_main_window(pid)
    return 0


def status(info: dict) -> dict:
    out = {"instance": {k: info.get(k) for k in
                        ("kind", "mode", "pid", "port", "cdp_port", "control_port",
                         "profile", "state_file")}}
    if is_webview2(info):
        with contextlib.suppress(Exception):
            out["host"] = browser.host_cmd(int(info["control_port"]), {"cmd": "status"})
    else:
        with contextlib.suppress(Exception):
            c = _cdp(info)
            win = c("Browser.getWindowForTarget")
            out["window"] = win.get("bounds")
            out["page"] = c.eval("[document.visibilityState, location.href]")
            c.close()
    hwnd = _hwnd(info)
    if hwnd:
        with contextlib.suppress(Exception):
            import window_style
            out["style"] = window_style.state(hwnd)
    return out


def show(info: dict, at: tuple[int, int]) -> dict:
    if is_webview2(info):
        return browser.host_cmd(int(info["control_port"]),
                                {"cmd": "show", "left": at[0], "top": at[1]})
    try:
        c = _cdp(info)
    except Exception as e:                      # 没装 websocket-client / 端口不通
        hwnd = _hwnd(info)
        if hwnd:
            ws = _ws(hwnd)
            return {"ok": True, "via": f"win32（CDP 不可用：{type(e).__name__}）",
                    "rect": ws.move(hwnd, *at)}
        return {"ok": False, "error": f"CDP 不可用且找不到窗口：{e}"}
    try:
        win = c("Browser.getWindowForTarget")
        c("Browser.setWindowBounds", windowId=win["windowId"],
          bounds={"left": at[0], "top": at[1], "windowState": "normal"})
        return {"ok": True, "via": "cdp", "bounds": c("Browser.getWindowForTarget").get("bounds")}
    finally:
        c.close()


def hide(info: dict) -> dict:
    """看不见但仍渲染：WebView2 用 hide（真停车），Edge 挪到屏幕外（渲染继续）。"""
    if is_webview2(info):
        return browser.host_cmd(int(info["control_port"]), {"cmd": "hide"})
    try:
        c = _cdp(info)
    except Exception as e:
        hwnd = _hwnd(info)
        if hwnd:
            ws = _ws(hwnd)
            return {"ok": True, "via": f"win32（CDP 不可用：{type(e).__name__}）",
                    "rect": ws.move(hwnd, -32000, -32000)}
        return {"ok": False, "error": f"CDP 不可用且找不到窗口：{e}"}
    try:
        win = c("Browser.getWindowForTarget")
        c("Browser.setWindowBounds", windowId=win["windowId"],
          bounds={"left": -32000, "top": -32000})
        return {"ok": True, "visibilityState": c.eval("document.visibilityState")}
    finally:
        c.close()


def close(info: dict, timeout: float = 15.0) -> dict:
    pid = int(info.get("pid") or 0)
    if is_webview2(info):
        r = browser.host_cmd(int(info["control_port"]), {"cmd": "quit"})
        deadline = time.time() + timeout
        while time.time() < deadline and pid and browser._pid_alive(pid):
            time.sleep(0.2)
        if pid and browser._pid_alive(pid):
            browser.kill_tree(pid)
            return {"ok": True, "closed": "forced", **r}
        return {"ok": True, "closed": "graceful", **r}
    graceful = False
    with contextlib.suppress(Exception):
        c = _cdp(info)
        c("Browser.close")          # 让 Chromium 自己把 profile 刷盘
        c.close()
        graceful = True
    deadline = time.time() + timeout
    while time.time() < deadline and pid and browser._pid_alive(pid):
        time.sleep(0.2)
    forced = bool(pid and browser._pid_alive(pid))
    if forced:
        browser.kill_tree(pid)
    if info.get("profile"):
        browser._remove_pidfile(info["profile"])
    return {"ok": True, "closed": "forced" if (forced or not graceful) else "graceful",
            "note": "" if graceful else "CDP 不可用，走的强杀；登录态可能丢",
            }


def main() -> int:
    ap = argparse.ArgumentParser(description="对一个已在跑的后端做窗口操作")
    ap.add_argument("--profile", help="用户数据目录（含 backend.json / host-state.json）")
    ap.add_argument("--port", type=int, help="CDP 端口")
    ap.add_argument("--control-port", type=int, help="WebView2 宿主的控制口")
    ap.add_argument("--at", default="120,120", help="show 的位置，默认 120,120")
    ap.add_argument("--fit", help="show 之后把客户区调到 WxH（无边框窗口用）")
    ap.add_argument("cmd", choices=["status", "show", "hide", "park", "close",
                                   "taskbar", "borderless"])
    ap.add_argument("arg", nargs="?", help="taskbar/borderless 的 on|off")
    a = ap.parse_args()

    info = find_instance(a.profile, a.port, a.control_port)
    at = tuple(int(v) for v in a.at.split(","))  # type: ignore[assignment]

    if a.cmd == "status":
        print(json.dumps(status(info), ensure_ascii=False, indent=2))
        return 0
    if a.cmd == "show":
        r = show(info, at=at)  # type: ignore[arg-type]
        if a.fit:
            hwnd = _hwnd(info)
            if hwnd:
                import window_style
                w, h = (int(v) for v in a.fit.lower().split("x"))
                r["fit"] = window_style.fit_client(hwnd, w, h)
        print(json.dumps(r, ensure_ascii=False))
        return 0
    if a.cmd in ("hide", "park"):
        print(json.dumps(hide(info), ensure_ascii=False))
        return 0
    if a.cmd == "close":
        print(json.dumps(close(info), ensure_ascii=False))
        return 0

    on = (a.arg or "on").lower() in ("on", "1", "true", "yes")
    hwnd = _hwnd(info)
    if not hwnd:
        print("找不到窗口（实例可能已经在屏幕外/已退出）", file=sys.stderr)
        return 2
    import window_style
    if a.cmd == "taskbar":
        print(json.dumps(window_style.set_taskbar(hwnd, on), ensure_ascii=False))
    else:
        r = window_style.set_borderless(hwnd, on)
        if on and a.fit:
            w, h = (int(v) for v in a.fit.lower().split("x"))
            window_style.fit_client(hwnd, w, h)
        print(json.dumps(r, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
