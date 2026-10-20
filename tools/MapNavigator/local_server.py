"""本机 Web 工具的启动：只监听 127.0.0.1，选端口、宣告地址、就绪后开浏览器，再把已绑定的 socket 交给 uvicorn。

MapNavigator 与其他基于同一套地图前端的工具共用。端口的唯一 owner 是这里：由这里 bind、由这里宣告
URL，入口脚本不自行猜端口 —— 否则端口顺延后浏览器会开到错误的地址。
"""

from __future__ import annotations

import logging
import os
import socket
import threading
import time
import webbrowser
from collections.abc import Callable, Iterable
from typing import Any

from starlette.datastructures import MutableHeaders

# 只绑 127.0.0.1 —— 后端会 spawn 进程 / 连 ADB / 载 maafw, 绝不暴露到局域网。
LISTEN_HOST = "127.0.0.1"
PORT_SCAN_LIMIT = 20  # 首选端口被占用时, 顺延试探的端口个数

Log = Callable[[str], None]


def preferred_port(env_name: str, default: int, log: Log) -> int:
    """首选端口: 环境变量覆盖 (非法值回退默认)。占用时仍会顺延, 见 bind_listen_socket。"""
    env_port = os.environ.get(env_name)
    if not env_port:
        return default
    try:
        port = int(env_port)
    except ValueError:
        log(f"{env_name} 非法 ({env_port!r}), 回退 {default}")
        return default
    if not 1 <= port <= 65535:
        log(f"{env_name} 越界 ({port}), 回退 {default}")
        return default
    return port


def _new_listen_socket() -> socket.socket:
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    if os.name != "nt":
        # POSIX: 允许复用 TIME_WAIT 残留 (快速重启不必换端口); bind 仍会拒绝正在监听的端口,
        # 所以冲突检测不受影响。Windows 语义相反 —— SO_REUSEADDR 允许抢占别人已绑的端口,
        # 会让下面的占用探测失效 (两个服务同绑一个口), 故 Windows 一律不设。
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    return sock


def bind_listen_socket(host: str, preferred: int, log: Log) -> tuple[socket.socket, int]:
    """绑定一个可用端口, 返回 (已绑定的 socket, 实际端口)。

    从 preferred 起顺延试 PORT_SCAN_LIMIT 个端口; 全被占用则退到端口 0 (内核分配),
    使工具在任何情况下都能起来, 而不是像 uvicorn.run 那样直接 EADDRINUSE 退出。

    返回的 socket 直接交给 uvicorn (`Server.run(sockets=[sock])`), 因此「探测到的端口」
    与「实际服务的端口」是同一个 socket —— 不存在「探测完到真正 bind 之间被别人抢走」的竞态。
    """
    last_error: OSError | None = None
    for offset in range(PORT_SCAN_LIMIT):
        port = preferred + offset
        if port > 65535:
            break
        sock = _new_listen_socket()
        try:
            sock.bind((host, port))
        except OSError as exc:
            sock.close()
            last_error = exc
            continue
        if offset:
            log(f"端口 {preferred} 已被占用, 顺延到 {port}")
        return sock, port

    sock = _new_listen_socket()
    try:
        sock.bind((host, 0))
    except OSError as exc:
        sock.close()
        raise RuntimeError(f"无法绑定任何端口 (最后错误: {last_error or exc})") from exc
    port = sock.getsockname()[1]
    log(f"端口 {preferred} 起连续 {PORT_SCAN_LIMIT} 个均被占用, 改用系统分配的 {port}")
    return sock, port


def open_browser_when_ready(host: str, port: int, log: Log, path: str = "/", timeout: float = 30.0) -> None:
    """等服务真正开始 accept 再开浏览器 (取代固定 sleep: 慢机器上不会开出「无法连接」页)。"""
    url = f"http://{host}:{port}{path}"
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.5):
                break
        except OSError:
            time.sleep(0.2)
    else:
        log(f"等待服务就绪超时 ({timeout:.0f}s), 仍尝试打开 {url}")
    webbrowser.open(url)


class NoStoreMiddleware:
    """给所有 HTTP 响应打 Cache-Control: no-store。

    这是给别的开发者用的工具, 底图/数据包/前端 JS 都可能被就地替换。浏览器默认对没有
    Cache-Control 的 FileResponse 走启发式缓存, 会拿旧底图旧代码却毫无提示 —— 非本模块
    开发者根本察觉不到。localhost 每次重取代价为零, 宁可不缓存也不让任何人踩到隐形旧图。

    纯 ASGI 实现: 只在 http.response.start 时补一个响应头, 不缓冲 FileResponse 响应体
    (BaseHTTPMiddleware 会缓冲, 破坏大文件与 range), 也不碰 websocket。
    """

    def __init__(self, app: Any) -> None:
        self.app = app

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return

        async def send_no_store(message: Any) -> None:
            if message["type"] == "http.response.start":
                MutableHeaders(scope=message)["Cache-Control"] = "no-store"
            await send(message)

        await self.app(scope, receive, send_no_store)


class _QuietAccessLog(logging.Filter):
    """访问日志只留失败的请求; 轮询类端点连失败也不留。"""

    def __init__(self, silent: Iterable[str]) -> None:
        super().__init__()
        self.silent = tuple(silent)

    def filter(self, record: logging.LogRecord) -> bool:
        msg = record.getMessage()
        if any(prefix in msg for prefix in self.silent):
            return False

        status_code = None
        if record.args and len(record.args) >= 5:
            try:
                status_code = int(record.args[4])
            except (ValueError, TypeError, IndexError):
                pass

        if status_code is not None:
            if 200 <= status_code < 400:
                return False
        else:
            for ok_code in (" 200 ", " 204 ", " 304 ", " 200 OK", " 304 Not Modified"):
                if ok_code in msg:
                    return False

        return True


def serve_local(
    app: Any,
    *,
    port: int,
    log: Log,
    open_browser: bool = True,
    silent_requests: Iterable[str] = ("GET /favicon.ico",),
) -> None:
    """在 127.0.0.1 上从 port 起找一个可用端口跑 app, 阻塞到服务退出。"""
    import uvicorn

    logging.getLogger("uvicorn.access").addFilter(_QuietAccessLog(silent_requests))

    listen_socket, listen_port = bind_listen_socket(LISTEN_HOST, port, log)
    log(f"服务地址: http://{LISTEN_HOST}:{listen_port}/")

    if open_browser:
        threading.Thread(
            target=open_browser_when_ready,
            args=(LISTEN_HOST, listen_port, log),
            daemon=True,
        ).start()

    # 交出已绑定的 socket (而非让 uvicorn 自己 bind), 端口即为上面宣告给浏览器的那个。
    try:
        uvicorn.Server(uvicorn.Config(app, host=LISTEN_HOST, port=listen_port)).run(sockets=[listen_socket])
    except KeyboardInterrupt:
        # Python 3.14 的 asyncio.run 会在 Uvicorn 完成 shutdown 后重新抛出 KeyboardInterrupt。
        pass
