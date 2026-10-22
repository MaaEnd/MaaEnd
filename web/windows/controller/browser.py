"""浏览器后端：默认拉起系统浏览器的无头模式，也可以直连外部 CDP 端点。

后端边界（每种后端都会在日志和控制器 uuid 里带明确标识，排障时一眼看出走的是哪条路）：

| 后端标识        | 什么时候出现                              | 起停由谁负责 |
| --------------- | ----------------------------------------- | ------------ |
| `edge-headless` | Windows 上找到 Edge（默认）               | 本模块       |
| `chrome-headless` | 只找到 Chrome（Linux 服务器常见）       | 本模块       |
| `external`      | 设了 `MAAEND_WEB_CDP_ENDPOINT`            | 用户自己     |

两种运行模式（`MAAEND_WEB_MODE`，默认 `headless`）：

  * `headless` —— `--headless=new`，零窗口对象：任务栏/Alt+Tab 都不可能有。适合无人值守跑批。
  * `offscreen` —— 真窗口但放在屏幕外 + 四个反遮挡开关：页面仍报 `visible`（不自降帧）、
    启动后把前台还给原窗口、窗口挂 `WS_EX_TOOLWINDOW`（不进任务栏/Alt+Tab）。
    好处是**随时能把窗口挪回屏幕给人操作**（`Browser.setWindowBounds`，实测 11–12 ms），
    同一进程、页面不重载、登录态不丢；代价是渲染常驻（无头也一样常驻，实测内存/CPU 同级）。

选哪一档取决于"要不要能随时交给人操作"，不是性能问题：
  * 无头没有窗口对象，**切不出来**；想显示只能换进程（同 profile 重启 ≈ 1.1 s，且必须优雅关闭，
    强杀会让刚写的 localStorage 不落盘）。
  * 屏外真窗口能 2–12 ms 拉回屏幕，且不抢焦点（`setWindowBounds` 不激活窗口）。

环境变量：
  MAAEND_WEB_CDP_ENDPOINT  外部端点，`http://127.0.0.1:9338` 或 `127.0.0.1:9338`；设了就不拉起、也不关闭
  MAAEND_WEB_BROWSER       浏览器可执行文件路径，覆盖自动探测
  MAAEND_WEB_PROFILE       用户数据目录（登录态存在这里，默认在用户目录下）
  MAAEND_WEB_VISIBLE=1     调试用：不要无头，把窗口正常显示出来（排查登录/识图）
  MAAEND_WEB_KEEP_ALIVE=1  控制器退出时保留本模块拉起的浏览器
"""

from __future__ import annotations

import contextlib
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import time
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

ENV_ENDPOINT = "MAAEND_WEB_CDP_ENDPOINT"
ENV_BROWSER = "MAAEND_WEB_BROWSER"
ENV_PROFILE = "MAAEND_WEB_PROFILE"
ENV_VISIBLE = "MAAEND_WEB_VISIBLE"
ENV_KEEP_ALIVE = "MAAEND_WEB_KEEP_ALIVE"
ENV_MODE = "MAAEND_WEB_MODE"        # headless（默认）| offscreen | webview2
ENV_WEBVIEW2_HOST = "MAAEND_WEB_WEBVIEW2_HOST"   # 指定 MaaEndWebHost.exe 路径

MODES = ("headless", "offscreen", "webview2")

# 屏外模式必须的四个反遮挡开关：没有它们，窗口在屏幕外/被遮挡时页面会自认为 hidden
# （云游戏据此降帧或停推流），实测加上后屏外仍报 visible。
_ANTI_OCCLUSION = [
    "--disable-features=CalculateNativeWinOcclusion,WebContentsOcclusion,msWebOOUI,msPdfOOUI",
    "--disable-backgrounding-occluded-windows",
    "--disable-renderer-backgrounding",
    "--disable-background-timer-throttling",
]


def resolve_mode(value: str | None = None) -> str:
    """解析运行模式；非法值直接报错，不静默降级。"""
    m = (value if value is not None else os.environ.get(ENV_MODE, "headless")).strip().lower()
    if m not in MODES:
        raise ValueError(f"{ENV_MODE}={m!r} 不合法，只能是 {' / '.join(MODES)}")
    return m

# 无头模式下的窗口装饰补偿：Chromium 会从 --window-size 里扣掉这部分，
# 想拿到 1280x720 的 CSS 视口就要加上它（实测 Win11 / Edge 155 为 30x95）。
_WINDOW_CHROME = (30, 95)

_PIDFILE = "backend.json"


@dataclass
class Backend:
    """一个可用的 CDP 后端。"""

    kind: str  # edge / chrome
    host: str
    port: int
    mode: str = "headless"   # headless | offscreen | external 用 kind=external 表示
    version: str = ""
    exe: str = ""
    profile: str = ""
    owned: bool = False
    hwnd: int = 0            # offscreen 模式下浏览器顶层窗口句柄（Windows）
    control_port: int = 0    # webview2 模式下宿主的控制口
    proc: subprocess.Popen | None = field(default=None, repr=False)
    extra: dict[str, Any] = field(default_factory=dict)

    def describe(self) -> str:
        owner = "本进程拉起" if self.owned else "外部提供"
        name = self.kind if self.kind == "external" else (
            self.kind if self.kind == self.mode else f"{self.kind}-{self.mode}")
        bits = [f"backend={name}", f"{self.host}:{self.port}", self.version or "version=?", owner]
        if self.control_port:
            bits.append(f"control={self.control_port}")
        if self.hwnd:
            bits.append(f"hwnd={self.hwnd}")
        if self.exe:
            bits.append(f"exe={self.exe}")
        if self.profile:
            bits.append(f"profile={self.profile}")
        return " | ".join(bits)

    def alive(self) -> bool:
        return probe(self.host, self.port) is not None

    def close(self) -> None:
        """只关闭自己拉起的后端；外部端点一律不碰。"""
        if not self.owned:
            return
        if os.environ.get(ENV_KEEP_ALIVE) == "1":
            return
        if self.mode == "webview2" and self.control_port:
            # 先让宿主优雅退出：WebView2 的 profile（登录态）要它自己 flush
            with contextlib.suppress(Exception):
                host_cmd(self.control_port, {"cmd": "quit"}, timeout=5)
            deadline = time.time() + 10
            while time.time() < deadline and self.proc is not None and self.proc.poll() is None:
                time.sleep(0.1)
        if self.proc is not None:
            kill_tree(self.proc.pid)
            with contextlib.suppress(Exception):
                self.proc.wait(timeout=10)
        _remove_pidfile(self.profile)


# ---------- 探测 ----------


def probe(host: str, port: int, timeout: float = 1.5) -> dict | None:
    """端点活着就返回 /json/version，否则 None。"""
    try:
        with urllib.request.urlopen(f"http://{host}:{port}/json/version", timeout=timeout) as r:
            return json.load(r)
    except Exception:
        return None


def parse_endpoint(value: str) -> tuple[str, int]:
    """`http://127.0.0.1:9338/` / `127.0.0.1:9338` → (host, port)。"""
    v = value.strip().rstrip("/")
    if "://" in v:
        v = v.split("://", 1)[1]
    if "/" in v:
        v = v.split("/", 1)[0]
    host, _, port = v.rpartition(":")
    if not host:
        raise ValueError(f"MAAEND_WEB_CDP_ENDPOINT 解析失败：{value!r}（要 host:port）")
    return host, int(port)


# ---------- 浏览器定位 ----------


def _windows_browser_candidates() -> list[tuple[str, str]]:
    local = os.environ.get("LOCALAPPDATA", "")
    pf = os.environ.get("PROGRAMFILES", r"C:\Program Files")
    pf86 = os.environ.get("PROGRAMFILES(X86)", r"C:\Program Files (x86)")
    return [
        ("edge", rf"{pf86}\Microsoft\Edge\Application\msedge.exe"),
        ("edge", rf"{pf}\Microsoft\Edge\Application\msedge.exe"),
        ("chrome", rf"{pf}\Google\Chrome\Application\chrome.exe"),
        ("chrome", rf"{pf86}\Google\Chrome\Application\chrome.exe"),
        ("edge", rf"{local}\Microsoft\Edge\Application\msedge.exe"),
    ]


_NIX_BROWSER_NAMES = [
    ("chrome", "google-chrome"),
    ("chrome", "google-chrome-stable"),
    ("chrome", "chrome"),
    ("edge", "microsoft-edge"),
    ("edge", "microsoft-edge-stable"),
]


def find_browser(override: str | None = None) -> tuple[str, str]:
    """返回 (后端标识, 可执行文件路径)。找不到就抛 FileNotFoundError，绝不静默降级。"""
    override = override or os.environ.get(ENV_BROWSER)
    if override:
        kind = "chrome" if "chrome" in Path(override).name.lower() else "edge"
        if not Path(override).exists():
            raise FileNotFoundError(f"{ENV_BROWSER}={override} 指向的浏览器不存在")
        return kind, override

    if sys.platform == "win32":
        for kind, path in _windows_browser_candidates():
            if Path(path).exists():
                return kind, path
    else:
        for kind, name in _NIX_BROWSER_NAMES:
            path = shutil.which(name)
            if path:
                return kind, path

    raise FileNotFoundError(
        "找不到可用的 Chromium 系浏览器。Windows 需要 Microsoft Edge 或 Google Chrome；"
        f"Linux 建议安装 google-chrome（Chromium 不含 H.264 专有编解码，云游戏画面放不出来）；"
        f"也可以用 {ENV_BROWSER} 指定可执行文件路径。"
    )


def find_webview2_host(override: str | None = None) -> str:
    """定位 MaaEndWebHost.exe（WebView2 宿主）。找不到就报错，不静默降级。"""
    override = override or os.environ.get(ENV_WEBVIEW2_HOST)
    if override:
        if not Path(override).exists():
            raise FileNotFoundError(f"{ENV_WEBVIEW2_HOST}={override} 指向的宿主不存在")
        return override
    here = Path(__file__).resolve().parent
    for rel in (
        "../MaaEndWebHost/bin/Release/net10.0-windows/MaaEndWebHost.exe",
        "../MaaEndWebHost/bin/Release/net8.0-windows/MaaEndWebHost.exe",
        "../MaaEndWebHost/bin/Debug/net10.0-windows/MaaEndWebHost.exe",
    ):
        p = (here / rel).resolve()
        if p.exists():
            return str(p)
    raise FileNotFoundError(
        "找不到 MaaEndWebHost.exe。先构建一次："
        "`dotnet build -c Release web/windows/MaaEndWebHost`；"
        f"或用 {ENV_WEBVIEW2_HOST} 指定路径。"
    )


def free_port(host: str = "127.0.0.1") -> int:
    with socket.socket() as s:
        s.bind((host, 0))
        return int(s.getsockname()[1])


def host_cmd(port: int, payload: dict, timeout: float = 10.0) -> dict:
    """给 WebView2 宿主的控制口发一条 JSON 命令（一行一答）。"""
    with socket.create_connection(("127.0.0.1", int(port)), timeout=timeout) as s:
        s.sendall((json.dumps(payload) + "\n").encode("utf-8"))
        line = s.makefile("rb").readline()
    return json.loads(line or b"{}")


def default_profile() -> Path:
    env = os.environ.get(ENV_PROFILE)
    if env:
        return Path(env)
    if sys.platform == "win32":
        base = Path(os.environ.get("LOCALAPPDATA", Path.home() / "AppData" / "Local"))
        return base / "MaaEnd" / "web-profile"
    return Path.home() / ".local" / "share" / "maaend" / "web-profile"


# ---------- 进程管理 ----------


def _pid_alive(pid: int) -> bool:
    """Windows 上必须走 OpenProcess/GetExitCodeProcess：`os.kill(pid, 0)` 在 Windows 上没有
    "探测"语义，它会调 TerminateProcess，等于把目标进程杀掉（实测踩到，回归测试里有用例）。"""
    if pid <= 0:
        return False
    if sys.platform == "win32":
        import ctypes

        k32 = ctypes.WinDLL("kernel32", use_last_error=True)
        k32.OpenProcess.restype = ctypes.c_void_p
        # PROCESS_QUERY_LIMITED_INFORMATION；没有权限就认为"不是我们能处理的进程"
        handle = k32.OpenProcess(0x1000, False, int(pid))
        if not handle:
            return False
        try:
            code = ctypes.c_ulong(0)
            if not k32.GetExitCodeProcess(ctypes.c_void_p(handle), ctypes.byref(code)):
                return False
            return code.value == 259  # STILL_ACTIVE
        finally:
            k32.CloseHandle(ctypes.c_void_p(handle))
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    return True


def kill_tree(pid: int) -> None:
    """连同子进程一起结束（Chromium 是多进程架构，光杀主进程会留孤儿）。"""
    if sys.platform == "win32":
        with contextlib.suppress(Exception):
            subprocess.run(["taskkill", "/PID", str(pid), "/T", "/F"],
                           capture_output=True, timeout=15,
                           creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        return
    with contextlib.suppress(ProcessLookupError):
        os.killpg(os.getpgid(pid), signal.SIGTERM)
    deadline = time.time() + 8
    while time.time() < deadline:
        if not _pid_alive(pid):
            return
        time.sleep(0.2)
    with contextlib.suppress(ProcessLookupError):
        os.killpg(os.getpgid(pid), signal.SIGKILL)


# ---------- Windows 窗口操作（offscreen 模式用） ----------


def get_foreground() -> int:
    if sys.platform != "win32":
        return 0
    import ctypes

    return int(ctypes.windll.user32.GetForegroundWindow() or 0)


def restore_foreground(hwnd: int) -> bool:
    """把前台还给用户原来的窗口。

    新窗口有一次激活权，不还就是"抢焦点"。`SetForegroundWindow` 对后台进程常被系统拒绝，
    标准绕法是先 AttachThreadInput 到当前前台线程再调用。
    """
    if sys.platform != "win32" or not hwnd:
        return False
    import ctypes

    u32 = ctypes.windll.user32
    if u32.SetForegroundWindow(hwnd):
        return True
    with contextlib.suppress(Exception):
        cur = ctypes.windll.kernel32.GetCurrentThreadId()
        fg = u32.GetForegroundWindow()
        fg_thread = u32.GetWindowThreadProcessId(fg, None)
        u32.AttachThreadInput(cur, fg_thread, True)
        try:
            ok = bool(u32.SetForegroundWindow(hwnd))
        finally:
            u32.AttachThreadInput(cur, fg_thread, False)
        return ok
    return False


def fix_viewport(port: int, host: str, viewport: tuple[int, int], log=None) -> tuple[int, int]:
    """把 CSS 视口校正到目标尺寸。

    注意**不能**用 `Emulation.setDeviceMetricsOverride`（实测 screencast 26 → 15 fps），
    而是用 `Browser.setWindowBounds` 补偿窗口装饰；`--window-size` 会被 profile 里记住的
    窗口尺寸覆盖，所以这一步是必需的，不能只靠命令行参数。
    """
    try:
        from cdp_client import CDPClient
    except Exception:  # 允许只装 websocket-client 的最小环境跳过
        return (0, 0)
    client = CDPClient(port=port, host=host, timeout=10)
    try:
        client.connect()
        tw, th = viewport
        for _ in range(3):
            inner = client.eval("[innerWidth, innerHeight]") or [0, 0]
            if (inner[0], inner[1]) == (tw, th):
                break
            win = client("Browser.getWindowForTarget")
            bounds = win.get("bounds") or {}
            dw = int(bounds.get("width", inner[0]) - inner[0])
            dh = int(bounds.get("height", inner[1]) - inner[1])
            client("Browser.setWindowBounds", windowId=win["windowId"],
                   bounds={"width": tw + dw, "height": th + dh})
            time.sleep(0.25)
        inner = client.eval("[innerWidth, innerHeight]") or [0, 0]
        if log:
            log(f"[backend] viewport = {inner[0]}x{inner[1]}（目标 {tw}x{th}）")
        return (inner[0], inner[1])
    except Exception as e:
        if log:
            log(f"[backend] 视口校正失败（忽略）：{type(e).__name__}: {e}")
        return (0, 0)
    finally:
        with contextlib.suppress(Exception):
            client.close()


def find_browser_window(pid: int, timeout: float = 10.0) -> int:
    """找承载页面的那个顶层窗口（Chrome_WidgetWin_1 且宽度 > 600）。"""
    if sys.platform != "win32":
        return 0
    import ctypes
    import ctypes.wintypes as wt

    u32 = ctypes.WinDLL("user32", use_last_error=True)
    u32.GetWindowThreadProcessId.argtypes = [wt.HWND, ctypes.POINTER(wt.DWORD)]
    u32.GetWindowRect.argtypes = [wt.HWND, ctypes.POINTER(wt.RECT)]
    u32.GetClassNameW.argtypes = [wt.HWND, ctypes.c_wchar_p, ctypes.c_int]
    enum_proc = ctypes.WINFUNCTYPE(wt.BOOL, wt.HWND, wt.LPARAM)
    deadline = time.time() + timeout
    hits: list[int] = []
    while time.time() < deadline:
        hits.clear()

        def cb(h, _):
            owner = wt.DWORD()
            u32.GetWindowThreadProcessId(h, ctypes.byref(owner))
            if owner.value != pid:
                return True
            cls = ctypes.create_unicode_buffer(256)
            u32.GetClassNameW(h, cls, 256)
            rect = wt.RECT()
            u32.GetWindowRect(h, ctypes.byref(rect))
            if cls.value == "Chrome_WidgetWin_1" and (rect.right - rect.left) > 600:
                hits.append(int(h))
            return True

        u32.EnumWindows(enum_proc(cb), 0)
        if hits:
            return hits[0]
        time.sleep(0.2)
    return 0


def set_toolwindow(hwnd: int) -> bool:
    """挂 WS_EX_TOOLWINDOW：从任务栏与 Alt+Tab 里消失（实测 Edge 不会把它改回去）。"""
    if sys.platform != "win32" or not hwnd:
        return False
    import ctypes

    u32 = ctypes.windll.user32
    gwl_exstyle, ws_ex_toolwindow = -20, 0x80
    cur = u32.GetWindowLongW(hwnd, gwl_exstyle)
    u32.SetWindowLongW(hwnd, gwl_exstyle, cur | ws_ex_toolwindow)
    return bool(u32.GetWindowLongW(hwnd, gwl_exstyle) & ws_ex_toolwindow)


def _pidfile(profile: Path) -> Path:
    return profile / _PIDFILE


def _read_pidfile(profile: Path) -> dict | None:
    try:
        return json.loads(_pidfile(profile).read_text(encoding="utf-8"))
    except Exception:
        return None


def _remove_pidfile(profile: str | Path) -> None:
    with contextlib.suppress(Exception):
        _pidfile(Path(profile)).unlink(missing_ok=True)


def reap_stale(profile: Path, log) -> None:
    """清理上次崩溃留下的浏览器（否则它占着端口和 profile，新的起不来）。"""
    info = _read_pidfile(profile)
    if not info:
        return
    pid = int(info.get("pid") or 0)
    port = int(info.get("port") or 0)
    if _pid_alive(pid):
        log(f"[backend] 发现上次残留 pid={pid} port={port}，清理中")
        kill_tree(pid)
        deadline = time.time() + 8
        while time.time() < deadline and port and probe("127.0.0.1", port, timeout=0.5):
            time.sleep(0.2)
    _remove_pidfile(profile)


def _instances_using_profile(profile: Path) -> list[int]:
    """列出正用同一 profile 的浏览器进程。同 profile 二开只会把 URL 转发过去，不会给新窗口。"""
    try:
        if sys.platform == "win32":
            ps = ("Get-CimInstance Win32_Process | Where-Object { $_.Name -match 'msedge|chrome' -and "
                  f"$_.CommandLine -like '*{profile}*' }} | Select-Object -ExpandProperty ProcessId")
            out = subprocess.run(["powershell", "-NoProfile", "-Command", ps],
                                 capture_output=True, timeout=20,
                                 creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0)).stdout
        else:
            out = subprocess.run(["pgrep", "-f", f"user-data-dir={profile}"],
                                 capture_output=True, timeout=20).stdout
    except Exception:
        return []
    return [int(x) for x in out.decode("utf-8", "replace").split() if x.strip().isdigit()]


def read_devtools_port(profile: Path, timeout: float = 20.0) -> int:
    """`--remote-debugging-port=0` 时，Chromium 把实际端口写进 <profile>/DevToolsActivePort。"""
    path = profile / "DevToolsActivePort"
    with contextlib.suppress(Exception):
        path.unlink()
    deadline = time.time() + timeout
    while time.time() < deadline:
        with contextlib.suppress(Exception):
            first = path.read_text(encoding="utf-8").splitlines()[0].strip()
            if first.isdigit():
                return int(first)
        time.sleep(0.05)
    raise TimeoutError(f"{timeout:.0f}s 内没有等到 {path}（浏览器可能没起来）")


def _browser_args(url: str | None, port: int, profile: Path, viewport: tuple[int, int],
                  mode: str, on_screen: bool) -> list[str]:
    w, h = viewport
    sw, sh = (w + _WINDOW_CHROME[0], h + _WINDOW_CHROME[1]) if not on_screen else (w, h)
    # 刻意不传 --remote-allow-origins：我们的 CDP 客户端不带 Origin 头，用不到它；
    # 一旦放开，本机任意网页都能连上这个调试端口控制浏览器、读登录态。
    args: list[str] = []
    if mode == "headless":
        # 零窗口：不创建任何平台窗口，任务栏/Alt+Tab 都不可能看到它
        args.append("--headless=new")
        args += [
            "--disable-features=msWebOOUI,msPdfOOUI",
            # 无头页面同样可能被判定为后台，这几个开关保证 rAF / 定时器 / 合成不被降频
            "--disable-background-timer-throttling",
            "--disable-renderer-backgrounding",
            "--disable-backgrounding-occluded-windows",
        ]
    else:
        # 屏外真窗口：必须有反遮挡开关，否则页面会自认为 hidden（云游戏据此降帧）
        args += _ANTI_OCCLUSION
        args.append("--window-position=120,120" if on_screen else "--window-position=-32000,-32000")
    args += [
        f"--remote-debugging-port={port}",
        f"--user-data-dir={profile}",
        f"--window-size={sw},{sh}",
        # 强制 DPR=1：不加的话真窗口会继承系统缩放（150% 屏上实测 devicePixelRatio=1.5，
        # CSS 视口只能凑到 1281×721、截图 1279×720，永远对不齐模板基准）
        "--force-device-scale-factor=1",
        "--mute-audio",
        "--no-first-run",
        "--no-default-browser-check",
        "--autoplay-policy=no-user-gesture-required",
    ]
    if url:
        args.append(url)
    return args


def _ensure_webview2(viewport: tuple[int, int], url: str | None, port: int | None,
                     log, visible: bool, ready_timeout: float) -> Backend:
    """拉起 MaaEndWebHost.exe（WebView2 宿主）并等它的 CDP + 控制口就绪。"""
    exe = find_webview2_host()
    prof_env = os.environ.get("MAAEND_WEB_WEBVIEW2_PROFILE")
    profile = Path(prof_env) if prof_env else (default_profile().parent / "webview2-profile")
    profile.mkdir(parents=True, exist_ok=True)
    state_path = profile / "host-state.json"
    with contextlib.suppress(Exception):
        state_path.unlink()          # 上一轮的状态文件别拿来误判

    cdp_port = port or free_port()
    ctl_port = free_port()
    w, h = viewport
    args = [exe, "--cdp-port", str(cdp_port), "--control-port", str(ctl_port),
            "--profile", str(profile), f"--size={w}x{h}", "--state", str(state_path),
            "--block-pointer-lock"]
    if visible:
        args.append("--on-screen")
    if url:
        args += ["--url", url]

    log(f"[backend] 启动 webview2 宿主: {exe}")
    log(f"[backend] 参数 {' '.join(args[1:])}")
    proc = subprocess.Popen(args, cwd=str(profile),
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                            stdin=subprocess.DEVNULL,
                            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
    try:
        deadline = time.time() + ready_timeout
        ver = None
        while time.time() < deadline:
            if proc.poll() is not None:
                raise RuntimeError(
                    f"WebView2 宿主启动后立即退出（exit={proc.returncode}），"
                    f"日志见 {Path(exe).parent / 'host.log'}")
            ver = probe("127.0.0.1", cdp_port)
            if ver and state_path.exists():
                break
            time.sleep(0.15)
        if not ver:
            raise TimeoutError(f"{ready_timeout:.0f}s 内 WebView2 宿主的 CDP 端口 {cdp_port} 没就绪")
        status: dict = {}
        for _ in range(60):
            with contextlib.suppress(Exception):
                status = host_cmd(ctl_port, {"cmd": "status"}, timeout=2)
                if status.get("ok"):
                    break
            time.sleep(0.2)
        if not status.get("ok"):
            raise TimeoutError(f"WebView2 宿主的控制口 {ctl_port} 没响应（{status}）")
    except BaseException:
        kill_tree(proc.pid)
        raise

    backend = Backend(kind="webview2", mode="webview2", host="127.0.0.1", port=cdp_port,
                      version=ver.get("Browser", ""), exe=exe, profile=str(profile),
                      owned=True, proc=proc, control_port=ctl_port)
    log(f"[backend] 就绪 {backend.describe()}")
    return backend


def ensure_backend(viewport: tuple[int, int] = (1280, 720),
                   url: str | None = None,
                   port: int | None = None,
                   log=print,
                   ready_timeout: float = 25.0) -> Backend:
    """按后端边界拿到一个可用后端：外部端点优先，否则拉起无头浏览器。

    port 传 None 时用随机端口（`--remote-debugging-port=0` + DevToolsActivePort），
    避免多实例互相抢固定端口，也不给本机其它程序留下一个可猜的入口。
    """
    endpoint = os.environ.get(ENV_ENDPOINT)
    if endpoint:
        host, eport = parse_endpoint(endpoint)
        ver = probe(host, eport, timeout=3)
        if ver is None:
            raise ConnectionError(
                f"{ENV_ENDPOINT}={endpoint} 指定的端点连不上（/json/version 无响应）。"
                "请确认那个浏览器/宿主已经开着，或去掉该环境变量让本模块自己拉起无头浏览器。"
            )
        backend = Backend(kind="external", host=host, port=eport,
                          version=ver.get("Browser", ""), owned=False)
        log(f"[backend] {backend.describe()}")
        return backend

    kind, exe = find_browser()
    profile = default_profile()
    profile.mkdir(parents=True, exist_ok=True)
    reap_stale(profile, log)

    mode = resolve_mode()
    visible = os.environ.get(ENV_VISIBLE) == "1"
    if visible:
        # 可见调试必须真窗口，无头状态下没有窗口可显示
        mode = "offscreen"
        log(f"[backend] {ENV_VISIBLE}=1：调试模式，窗口会真的显示在屏幕上")
    log(f"[backend] 模式 = {mode}（{ENV_MODE}）")

    if mode == "webview2":
        return _ensure_webview2(viewport=viewport, url=url, port=port, log=log,
                                visible=visible, ready_timeout=ready_timeout)

    # 屏外真窗口会抢一次前台（新窗口有一次性激活权），先记下用户当前窗口，起来后还回去
    prev_fg = get_foreground() if (mode == "offscreen" and not visible) else 0

    args = [exe, *_browser_args(url, port or 0, profile, viewport, mode, on_screen=visible)]
    log_path = profile / "browser.log"
    log(f"[backend] 启动 {kind}: {exe}")
    log(f"[backend] 参数 {' '.join(args[1:])}")
    with open(log_path, "ab") as fh:
        fh.write(f"\n=== {time.strftime('%Y-%m-%d %H:%M:%S')} {' '.join(args)}\n".encode())
        fh.flush()
        proc = subprocess.Popen(
            args,
            cwd=str(profile),
            stdout=fh,
            stderr=subprocess.STDOUT,
            stdin=subprocess.DEVNULL,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
            start_new_session=sys.platform != "win32",
        )

    try:
        real_port = port or read_devtools_port(profile, timeout=ready_timeout)
        deadline = time.time() + ready_timeout
        ver = None
        while time.time() < deadline:
            if proc.poll() is not None:
                raise RuntimeError(
                    f"浏览器启动后立即退出（exit={proc.returncode}），日志见 {log_path}")
            ver = probe("127.0.0.1", real_port)
            if ver:
                break
            time.sleep(0.1)
        if not ver:
            others = _instances_using_profile(profile)
            hint = (f"；另有 {len(others)} 个浏览器进程在用同一 profile（pid={others[:5]}）——"
                    "同一 profile 只能有一个实例，先关掉它或改用 MAAEND_WEB_PROFILE") if others else ""
            raise TimeoutError(f"{ready_timeout:.0f}s 内 CDP 端口 {real_port} 没有就绪{hint}")
    except BaseException:
        kill_tree(proc.pid)
        raise

    backend = Backend(kind=kind, mode=mode, host="127.0.0.1", port=real_port,
                      version=ver.get("Browser", ""), exe=exe,
                      profile=str(profile), owned=True, proc=proc)

    if mode == "offscreen" and not visible:
        # 收尾两件事：把任务栏/Alt+Tab 里的条目去掉 + 把前台还给用户
        hwnd = find_browser_window(proc.pid)
        backend.hwnd = hwnd
        if hwnd:
            log(f"[backend] 窗口 hwnd={hwnd}，挂 WS_EX_TOOLWINDOW = {set_toolwindow(hwnd)}"
                f"（不进任务栏/Alt+Tab）")
        else:
            log("[backend] 没找到浏览器窗口（任务栏条目可能还在）")
        if prev_fg and restore_foreground(prev_fg):
            log(f"[backend] 前台已还给原窗口 {prev_fg}（避免抢焦点）")
        else:
            log("[backend] 前台还原失败（可能系统限制 SetForegroundWindow）")

    _pidfile(profile).write_text(json.dumps({
        "kind": kind, "mode": mode, "pid": proc.pid, "port": real_port, "exe": exe,
        "profile": str(profile), "started": time.strftime("%Y-%m-%d %H:%M:%S"),
    }, ensure_ascii=False, indent=2), encoding="utf-8")
    log(f"[backend] 就绪 {backend.describe()}")
    # `--window-size` 会被 profile 里记住的窗口尺寸覆盖，必须启动后用 CDP 校正一次
    fix_viewport(real_port, "127.0.0.1", viewport, log=log)
    return backend
