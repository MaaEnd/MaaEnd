"""浏览器后端：默认拉起系统浏览器的无头模式，也可以直连外部 CDP 端点。

后端边界（每种后端都会在日志和控制器 uuid 里带明确标识，排障时一眼看出走的是哪条路）：

| 后端标识        | 什么时候出现                              | 起停由谁负责 |
| --------------- | ----------------------------------------- | ------------ |
| `edge-headless` | Windows 上找到 Edge（默认）               | 本模块       |
| `chrome-headless` | 只找到 Chrome（Linux 服务器常见）       | 本模块       |
| `external`      | 设了 `MAAEND_WEB_CDP_ENDPOINT`            | 用户自己     |

选 `--headless=new` 而不是"屏外窗口"的理由（实测）：
  * 屏外窗口（`--window-position=-32000,-32000`）会让页面 `document.visibilityState == "hidden"`，
    云游戏客户端普遍据此降帧或暂停推流；无头反而报告 `visible` + `hasFocus`。
  * 屏外窗口会在任务栏留一个条目、进 Alt+Tab 列表；无头一个窗口对象都没有。
  * 两者取帧性能相同（screencast 约 30 fps、captureScreenshot 约 50 ms），所以没必要留窗口。

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

# 无头模式下的窗口装饰补偿：Chromium 会从 --window-size 里扣掉这部分，
# 想拿到 1280x720 的 CSS 视口就要加上它（实测 Win11 / Edge 155 为 30x95）。
_WINDOW_CHROME = (30, 95)

_PIDFILE = "backend.json"


@dataclass
class Backend:
    """一个可用的 CDP 后端。"""

    kind: str  # edge-headless / chrome-headless / external
    host: str
    port: int
    version: str = ""
    exe: str = ""
    profile: str = ""
    owned: bool = False
    proc: subprocess.Popen | None = field(default=None, repr=False)
    extra: dict[str, Any] = field(default_factory=dict)

    def describe(self) -> str:
        owner = "本进程拉起" if self.owned else "外部提供"
        bits = [f"backend={self.kind}", f"{self.host}:{self.port}", self.version or "version=?", owner]
        if self.exe:
            bits.append(f"exe={self.exe}")
        if self.profile:
            bits.append(f"profile={self.profile}")
        return " | ".join(bits)

    def alive(self) -> bool:
        return probe(self.host, self.port) is not None

    def close(self) -> None:
        """只关闭自己拉起的浏览器；外部端点一律不碰。"""
        if not self.owned:
            return
        if os.environ.get(ENV_KEEP_ALIVE) == "1":
            return
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
        ("edge-headless", rf"{pf86}\Microsoft\Edge\Application\msedge.exe"),
        ("edge-headless", rf"{pf}\Microsoft\Edge\Application\msedge.exe"),
        ("chrome-headless", rf"{pf}\Google\Chrome\Application\chrome.exe"),
        ("chrome-headless", rf"{pf86}\Google\Chrome\Application\chrome.exe"),
        ("edge-headless", rf"{local}\Microsoft\Edge\Application\msedge.exe"),
    ]


_NIX_BROWSER_NAMES = [
    ("chrome-headless", "google-chrome"),
    ("chrome-headless", "google-chrome-stable"),
    ("chrome-headless", "chrome"),
    ("edge-headless", "microsoft-edge"),
    ("edge-headless", "microsoft-edge-stable"),
]


def find_browser(override: str | None = None) -> tuple[str, str]:
    """返回 (后端标识, 可执行文件路径)。找不到就抛 FileNotFoundError，绝不静默降级。"""
    override = override or os.environ.get(ENV_BROWSER)
    if override:
        kind = "chrome-headless" if "chrome" in Path(override).name.lower() else "edge-headless"
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


def _browser_args(exe_kind: str, url: str | None, port: int, profile: Path,
                  viewport: tuple[int, int], visible: bool) -> list[str]:
    w, h = viewport
    sw, sh = (w + _WINDOW_CHROME[0], h + _WINDOW_CHROME[1]) if not visible else (w, h)
    # 刻意不传 --remote-allow-origins：我们的 CDP 客户端不带 Origin 头，用不到它；
    # 一旦放开，本机任意网页都能连上这个调试端口控制浏览器、读登录态。
    args: list[str] = []
    if not visible:
        # 无头：不创建任何平台窗口，任务栏/Alt+Tab 都不可能看到它
        args.append("--headless=new")
    args += [
        f"--remote-debugging-port={port}",
        f"--user-data-dir={profile}",
        f"--window-size={sw},{sh}",
        "--mute-audio",
        "--no-first-run",
        "--no-default-browser-check",
        "--autoplay-policy=no-user-gesture-required",
        "--disable-features=msWebOOUI,msPdfOOUI",
        # 无头页面同样可能被判定为后台，这几个开关保证 rAF / 定时器 / 合成不被降频
        "--disable-background-timer-throttling",
        "--disable-renderer-backgrounding",
        "--disable-backgrounding-occluded-windows",
    ]
    if url:
        args.append(url)
    return args


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

    visible = os.environ.get(ENV_VISIBLE) == "1"
    if visible:
        log(f"[backend] {ENV_VISIBLE}=1：调试模式，窗口会真的显示在屏幕上")

    args = [exe, *_browser_args(kind, url, port or 0, profile, viewport, visible)]
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
            raise TimeoutError(f"{ready_timeout:.0f}s 内 CDP 端口 {real_port} 没有就绪")
    except BaseException:
        kill_tree(proc.pid)
        raise

    backend = Backend(kind=kind, host="127.0.0.1", port=real_port,
                      version=ver.get("Browser", ""), exe=exe,
                      profile=str(profile), owned=True, proc=proc)
    _pidfile(profile).write_text(json.dumps({
        "kind": kind, "pid": proc.pid, "port": real_port, "exe": exe,
        "profile": str(profile), "started": time.strftime("%Y-%m-%d %H:%M:%S"),
    }, ensure_ascii=False, indent=2), encoding="utf-8")
    log(f"[backend] 就绪 {backend.describe()}")
    return backend
