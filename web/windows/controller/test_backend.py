"""后端回归测试：不依赖 MaaFramework，只测 browser.py + cdp_client.py 的契约。

    cd web/windows/controller
    uv run --with websocket-client --with pillow --with numpy python test_backend.py

覆盖：
  1. 冷启动：无任何前置进程时能拉起后端，端口随机、版本可读
  2. 无窗口：整棵进程树下没有任何顶层窗口（"完全无头"的硬指标）
  3. 视口：CSS 视口精确等于目标值（窗口尺寸补偿是否生效）
  4. 取帧：screencap 形状/类型/非全黑
  5. pidfile：运行时存在，关闭后消失
  6. 残留清理：伪造一个"上次崩溃"的 pidfile，能杀掉并删掉
  7. external 后端：只管连、不管起停
  8. 收尸：关闭后端口不可连、进程树消失

只用标准库 + websocket-client + numpy + pillow，方便在 CI 里裸跑。
"""

from __future__ import annotations

import contextlib
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.request

import browser
import numpy as np
from cdp_client import CDPClient

VIEWPORT = (1280, 720)
RESULTS: list[tuple[str, bool, str]] = []


def case(name: str, ok: bool, detail: str = "") -> None:
    RESULTS.append((name, bool(ok), detail))
    print(f"{'PASS' if ok else 'FAIL'}  {name}{('  — ' + detail) if detail else ''}", flush=True)


def probe_ok(port: int) -> bool:
    with contextlib.suppress(Exception), \
            urllib.request.urlopen(f"http://127.0.0.1:{port}/json/version", timeout=1) as r:
        return bool(r.status == 200)
    return False


# ---------- Windows：枚举某进程树下的顶层窗口 ----------

def top_level_windows(pids: set[int]) -> list[tuple[str, tuple[int, int, int, int]]]:
    if sys.platform != "win32":
        return []
    import ctypes
    import ctypes.wintypes as wt

    u32 = ctypes.WinDLL("user32", use_last_error=True)
    u32.GetWindowThreadProcessId.argtypes = [wt.HWND, ctypes.POINTER(wt.DWORD)]
    u32.GetClassNameW.argtypes = [wt.HWND, ctypes.c_wchar_p, ctypes.c_int]
    u32.GetWindowTextW.argtypes = [wt.HWND, ctypes.c_wchar_p, ctypes.c_int]
    u32.GetWindowRect.argtypes = [wt.HWND, ctypes.POINTER(wt.RECT)]
    enump = ctypes.WINFUNCTYPE(wt.BOOL, wt.HWND, wt.LPARAM)
    found: list[tuple[str, tuple[int, int, int, int]]] = []

    def cb(h, _):
        pid = wt.DWORD()
        u32.GetWindowThreadProcessId(h, ctypes.byref(pid))
        if pid.value not in pids:
            return True
        cls = ctypes.create_unicode_buffer(256)
        u32.GetClassNameW(h, cls, 256)
        rect = wt.RECT()
        u32.GetWindowRect(h, ctypes.byref(rect))
        found.append((cls.value, (rect.left, rect.top, rect.right, rect.bottom)))
        return True

    u32.EnumWindows(enump(cb), 0)
    return found


def child_pids(pid: int) -> set[int]:
    """用 PowerShell 拿进程树（避免引入 psutil 依赖）。"""
    if sys.platform != "win32":
        return {pid}
    with contextlib.suppress(Exception):
        out = subprocess.run(
            ["powershell", "-NoProfile", "-Command",
             f"$ids=@{pid}; for($i=0;$i -lt 4;$i++){{"
             f"$ids += (Get-CimInstance Win32_Process | Where-Object {{$ids -contains $_.ParentProcessId}}).ProcessId }};"
             f"$ids -join ','"],
            capture_output=True, timeout=30,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0)).stdout.decode("utf-8", "replace").strip()
        return {int(x) for x in out.split(",") if x.strip().isdigit()}
    return {pid}


def offscreen_cases() -> None:
    """屏外模式：能随时拉回屏幕给人操作，且不打扰用户（不进任务栏、不抢焦点）。"""
    if sys.platform != "win32":
        print("SKIP  offscreen 用例（仅 Windows）")
        return
    import ctypes
    import ctypes.wintypes

    os.environ[browser.ENV_MODE] = "offscreen"
    os.environ[browser.ENV_PROFILE] = str(browser.default_profile().parent / "web-profile-offscreen-test")
    backend = None
    try:
        backend = browser.ensure_backend(viewport=VIEWPORT, url="about:blank")
        case("offscreen：模式与窗口句柄", backend.mode == "offscreen" and backend.hwnd != 0,
             f"mode={backend.mode} hwnd={backend.hwnd}")

        hwnd = backend.hwnd
        ex = ctypes.windll.user32.GetWindowLongW(hwnd, -20) & 0xFFFFFFFF
        case("offscreen：WS_EX_TOOLWINDOW 已挂（不进任务栏/Alt+Tab）", bool(ex & 0x80), hex(ex))

        client = CDPClient(port=backend.port, host=backend.host)
        client.connect()
        vis = client.eval("document.visibilityState")
        case("offscreen：屏幕外时页面仍 visible（反遮挡开关生效）", vis == "visible", str(vis))

        wid = client("Browser.getWindowForTarget")["windowId"]
        fg_before = int(ctypes.windll.user32.GetForegroundWindow() or 0)
        t0 = time.time()
        client("Browser.setWindowBounds", windowId=wid,
               bounds={"left": 120, "top": 120, "windowState": "normal"})
        dt = (time.time() - t0) * 1000
        time.sleep(0.5)
        rect = ctypes.wintypes.RECT()
        ctypes.windll.user32.GetWindowRect(hwnd, ctypes.byref(rect))
        on_screen = rect.left > -1000 and rect.top > -1000
        case("offscreen：能拉回屏幕（<200ms、不重载）", on_screen and dt < 200,
             f"{dt:.0f}ms rect=({rect.left},{rect.top})")
        fg_after = int(ctypes.windll.user32.GetForegroundWindow() or 0)
        case("offscreen：拉回屏幕不主动抢焦点", fg_after == fg_before,
             f"前后前台 {fg_before}->{fg_after}")

        client("Browser.setWindowBounds", windowId=wid, bounds={"left": -32000, "top": -32000})
        time.sleep(0.5)
        ctypes.windll.user32.GetWindowRect(hwnd, ctypes.byref(rect))
        vis2 = client.eval("document.visibilityState")
        case("offscreen：能挪回屏幕外继续跑（仍 visible）",
             rect.left < -10000 and vis2 == "visible", f"rect={rect.left} vis={vis2}")
        client.close()
    except Exception as e:
        case("offscreen：整体流程", False, f"{type(e).__name__}: {e}")
    finally:
        if backend is not None:
            port = backend.port
            with contextlib.suppress(Exception):
                backend.close()
            time.sleep(1.2)
            case("offscreen：关闭后端口不可连", not probe_ok(port), f"port={port}")
        os.environ.pop(browser.ENV_MODE, None)
        os.environ.pop(browser.ENV_PROFILE, None)
        with contextlib.suppress(OSError):
            shutil.rmtree(browser.default_profile().parent / "web-profile-offscreen-test", ignore_errors=True)



def webview2_cases() -> None:
    """WebView2 宿主：屏外真窗口 + 进程内停车（hide/resume）+ 控制口。"""
    if sys.platform != "win32":
        print("SKIP  webview2 用例（仅 Windows）")
        return
    try:
        browser.find_webview2_host()
    except FileNotFoundError as e:
        print(f"SKIP  webview2 用例（{e}）")
        return
    os.environ[browser.ENV_MODE] = "webview2"
    os.environ["MAAEND_WEB_WEBVIEW2_PROFILE"] = str(browser.default_profile().parent / "webview2-test")
    backend = None
    try:
        backend = browser.ensure_backend(viewport=VIEWPORT, url="about:blank")
        case("webview2：后端就绪（CDP + 控制口）",
             backend.mode == "webview2" and backend.control_port != 0,
             f"cdp={backend.port} control={backend.control_port}")

        st = browser.host_cmd(backend.control_port, {"cmd": "status"})
        case("webview2：默认在屏幕外（用户看不见）",
             st.get("x", 0) < -10000 and st.get("visible") is True, str(st))

        client = CDPClient(port=backend.port, host=backend.host)
        client.connect()
        vis = client.eval("document.visibilityState")
        png = client.screenshot_png()
        case("webview2：页面 visible 且能截图", vis == "visible" and len(png) > 1000,
             f"visibilityState={vis} png={len(png)}B")

        r = browser.host_cmd(backend.control_port, {"cmd": "show", "left": 150, "top": 150})
        case("webview2：show 立刻拉回屏幕（不重载）",
             r.get("ok") and r.get("x") == 150 and r.get("visible") is True, str(r))
        r2 = browser.host_cmd(backend.control_port, {"cmd": "park"})
        case("webview2：park 回屏幕外且仍在渲染",
             r2.get("ok") and r2.get("x", 0) < -10000 and r2.get("visible") is True, str(r2))

        h = browser.host_cmd(backend.control_port, {"cmd": "hide"})
        case("webview2：hide 真正停车（visible=False）",
             h.get("ok") and h.get("visible") is False, str(h))
        rv = browser.host_cmd(backend.control_port, {"cmd": "resume"})
        case("webview2：resume 能重新出画面", rv.get("ok") and rv.get("visible") is True, str(rv))
        client.close()
    except Exception as e:
        case("webview2：整体流程", False, f"{type(e).__name__}: {e}")
    finally:
        if backend is not None:
            port = backend.port
            with contextlib.suppress(Exception):
                backend.close()
            time.sleep(1.5)
            case("webview2：quit 后端口不可连（宿主已退出）", not probe_ok(port), f"port={port}")
        os.environ.pop(browser.ENV_MODE, None)
        os.environ.pop("MAAEND_WEB_WEBVIEW2_PROFILE", None)
        shutil.rmtree(browser.default_profile().parent / "webview2-test", ignore_errors=True)



def mode_override_cases() -> None:
    """显式 mode 参数要能压过环境变量——热切换（switch_mode）就靠这条。"""
    if sys.platform != "win32":
        print("SKIP  mode 覆盖用例（仅 Windows）")
        return
    os.environ[browser.ENV_MODE] = "offscreen"
    os.environ[browser.ENV_PROFILE] = str(browser.default_profile().parent / "web-profile-mode-test")
    b = None
    try:
        b = browser.ensure_backend(viewport=VIEWPORT, url="about:blank", mode="headless")
        case("switch：显式 mode=headless 压过 env=offscreen",
             b.mode == "headless" and b.hwnd == 0, f"env=offscreen 实际 mode={b.mode} hwnd={b.hwnd}")
    except Exception as e:
        case("switch：显式 mode 覆盖", False, f"{type(e).__name__}: {e}")
    finally:
        if b is not None:
            b.close()
        os.environ.pop(browser.ENV_MODE, None)
        os.environ.pop(browser.ENV_PROFILE, None)
        shutil.rmtree(browser.default_profile().parent / "web-profile-mode-test", ignore_errors=True)
    # 未知模式必须响亮报错，不能静默退回默认值
    try:
        browser.resolve_mode("nonsense")
        case("switch：未知模式报错", False, "没有抛异常")
    except ValueError as e:
        case("switch：未知模式报错", True, str(e)[:70])


def default_mode_case() -> None:
    """默认档必须是 offscreen（能随时交给人操作），纯无头要靠 env 显式要求。"""
    os.environ.pop(browser.ENV_MODE, None)
    case("默认档：不设环境变量时是 offscreen", browser.resolve_mode() == "offscreen",
         browser.resolve_mode())
    os.environ[browser.ENV_MODE] = "headless"
    case("默认档：MAAEND_WEB_MODE=headless 能要到纯无头", browser.resolve_mode() == "headless",
         browser.resolve_mode())
    os.environ.pop(browser.ENV_MODE, None)


def main() -> int:
    # ---- 1. 冷启动 + 2. 无窗口 + 3. 视口 ----
    # 这一段是无头的专项契约（零窗口、UA 里带 Headless），所以显式要 headless 模式，
    # 不跟着默认档走——否则改默认值时这些用例会静默失效。
    default_mode_case()
    t0 = time.time()
    backend = browser.ensure_backend(viewport=VIEWPORT, url="about:blank", mode="headless")
    ready = time.time() - t0
    case("冷启动：后端就绪", backend.port > 0 and backend.version != "",
         f"{backend.kind} {backend.version} {ready:.2f}s port={backend.port}")
    case("冷启动：端口随机（非固定 9338）", backend.port != 9338, f"port={backend.port}")

    if sys.platform == "win32":
        pids = child_pids(backend.proc.pid) if backend.proc else {0}
        wins = top_level_windows(pids)
        case("无窗口：进程树下顶层窗口数 = 0", len(wins) == 0,
             f"{len(wins)} 个 {wins[:3]}")

    client = CDPClient(port=backend.port, host=backend.host)
    client.connect()
    inner = client.eval("[innerWidth, innerHeight]") or [0, 0]
    case("视口：CSS 视口等于目标值", tuple(inner) == VIEWPORT, f"{inner} vs {list(VIEWPORT)}")
    ua = client.eval("navigator.userAgent") or ""
    case("UA：无头浏览器确实带 Headless（控制器负责修）", "Headless" in ua, ua[:60])

    # ---- 4. 取帧 ----
    png = client.screenshot_png()
    case("取帧：PNG 非空", len(png) > 1000, f"{len(png)} bytes")
    app = "data:text/html,<body style='margin:0;background:%23fff'>" \
          "<div style='width:300px;height:300px;background:%230a0'></div></body>"
    client("Page.navigate", url=app)
    time.sleep(1.0)
    img = client.screenshot_bgr()
    ok_shape = img.shape == (VIEWPORT[1], VIEWPORT[0], 3) and img.dtype == np.uint8
    case("取帧：BGR uint8 且尺寸匹配", ok_shape, f"shape={img.shape} dtype={img.dtype}")
    case("取帧：非全黑", float(img.mean()) > 5, f"mean={float(img.mean()):.1f}")

    # ---- 5. pidfile 生命周期 ----
    pidfile = browser._pidfile(__import__("pathlib").Path(backend.profile))
    case("pidfile：运行时存在", pidfile.exists(), str(pidfile))

    client.close()
    port = backend.port
    backend.close()
    time.sleep(1.5)
    case("收尸：关闭后端口不可连", not probe_ok(port), f"port={port}")
    case("pidfile：关闭后已删除", not pidfile.exists(), str(pidfile))

    # ---- 6. 残留清理 ----
    fake_profile = browser.default_profile().parent / "web-profile-stale-test"
    fake_profile.mkdir(parents=True, exist_ok=True)
    dummy = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(120)"],
                             creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
    (fake_profile / "backend.json").write_text(
        json.dumps({"kind": "edge-headless", "pid": dummy.pid, "port": 0,
                    "profile": str(fake_profile)}), encoding="utf-8")
    logs: list[str] = []
    browser.reap_stale(fake_profile, logs.append)
    time.sleep(1.0)
    dead = dummy.poll() is not None
    with contextlib.suppress(Exception):
        dummy.kill()
    case("残留清理：杀掉上次崩溃留下的进程并删 pidfile",
         dead and not (fake_profile / "backend.json").exists(), " | ".join(logs[:2]))
    with contextlib.suppress(OSError):
        fake_profile.rmdir()

    # ---- 7. external 后端 ----
    ext_profile = browser.default_profile().parent / "web-profile-ext-test"
    ext_src = subprocess.Popen([
        browser.find_browser()[1], "--headless=new", "--remote-debugging-port=9466",
        f"--user-data-dir={ext_profile}", "--window-size=1310,815", "--no-first-run",
        "--mute-audio", "about:blank",
    ], creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(80):
        if probe_ok(9466):
            break
        time.sleep(0.25)
    os.environ[browser.ENV_ENDPOINT] = "127.0.0.1:9466"
    try:
        ext = browser.ensure_backend(viewport=VIEWPORT)
        case("external：识别为外部端点", ext.kind == "external" and not ext.owned,
             f"{ext.kind} owned={ext.owned} port={ext.port}")
        ext.close()
        time.sleep(0.5)
        case("external：close 不影响外部浏览器", probe_ok(9466), "端点仍可用")
    finally:
        os.environ.pop(browser.ENV_ENDPOINT, None)
        with contextlib.suppress(Exception):
            subprocess.run(["taskkill", "/PID", str(ext_src.pid), "/T", "/F"],
                           capture_output=True, timeout=15,
                           creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        with contextlib.suppress(Exception):
            ext_src.wait(timeout=10)
        shutil.rmtree(ext_profile, ignore_errors=True)
        time.sleep(1.0)

    offscreen_cases()
    webview2_cases()
    mode_override_cases()

    failed = [r for r in RESULTS if not r[1]]
    print(f"\n=== {len(RESULTS) - len(failed)}/{len(RESULTS)} 通过 ===")
    for name, _, detail in failed:
        print(f"  FAIL {name} {detail}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
