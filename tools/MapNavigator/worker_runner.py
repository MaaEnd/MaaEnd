"""提权子进程的骨架: 回连父进程的 NDJSON 通道 + 整套启动收尾流程。

连接、握手、日志回传、停止宽限、半关闭收尾对录制与试跑完全一样, 两者的差异全部来自
session_modes.MODES。提权本身由 serve.py 的 ElevatedWorkerBridge 负责, 子进程侧不碰。
"""

from __future__ import annotations

import argparse
import json
import socket
import sys
import threading
import traceback

from session_modes import MODES, SessionMode

CONNECT_TIMEOUT_SECONDS = 30.0
# 收到 stop 后等服务交出终结消息的宽限期 (一轮采样可能正卡在识别调用里)。
STOP_GRACE_SECONDS = 20.0
# 半关闭后等父进程收完并回关的上限。
LINGER_SECONDS = 5.0


class Channel:
    """回连 socket 的 NDJSON 读写封装。

    send 会被工作线程和 pynput 热键线程并发调用, 故加锁。
    """

    def __init__(self, sock: socket.socket) -> None:
        self._sock = sock
        self._send_lock = threading.Lock()
        self._recv_buffer = b""
        self._closed = False

    def send(self, payload: dict) -> None:
        line = (json.dumps(payload, ensure_ascii=False) + "\n").encode("utf-8")
        with self._send_lock:
            if self._closed:
                return
            try:
                self._sock.sendall(line)
            except OSError:
                self._closed = True

    def recv(self) -> dict | None:
        """读下一条消息; 返回 None 表示对端关闭 (父进程没了 -> 本进程该退出)。"""
        while True:
            newline = self._recv_buffer.find(b"\n")
            if newline >= 0:
                line = self._recv_buffer[:newline]
                self._recv_buffer = self._recv_buffer[newline + 1:]
                if not line.strip():
                    continue
                try:
                    msg = json.loads(line.decode("utf-8"))
                except (ValueError, UnicodeDecodeError):
                    continue
                return msg if isinstance(msg, dict) else {}
            try:
                chunk = self._sock.recv(65536)
            except OSError:
                return None
            if not chunk:
                return None
            self._recv_buffer += chunk

    def half_close(self) -> None:
        """只发 FIN 不关 socket。直接 close 在 Windows 上会退化成 RST, 把对端收到但
        还没读的数据一起冲掉 (实测 4/14 的会话因此丢了最后那条 finished/error)。
        """
        with self._send_lock:
            self._closed = True
            try:
                self._sock.shutdown(socket.SHUT_WR)
            except OSError:
                pass

    def drain(self, linger: float) -> None:
        """半关闭后等对端读完回关。只在没有 reader 线程占着读端时调。"""
        try:
            self._sock.settimeout(linger)
            while self._sock.recv(65536):
                pass
        except OSError:
            pass

    def close(self) -> None:
        self._closed = True
        try:
            self._sock.close()
        except OSError:
            pass


class ChannelStream:
    """把 print/traceback 按行转成 log 消息回传父进程。

    子进程窗口是隐藏的 (SW_HIDE), 服务层全程用 print 打诊断, 不接过来就全丢了。
    """

    def __init__(self, channel: Channel) -> None:
        self._channel = channel
        self._buffer = ""
        self._lock = threading.Lock()

    def write(self, text: str) -> int:
        with self._lock:
            self._buffer += text
            lines = self._buffer.split("\n")
            self._buffer = lines.pop()
            pending = [line for line in lines if line.strip()]
        for line in pending:
            self._channel.send({"type": "log", "message": line})
        return len(text)

    def flush(self) -> None:
        with self._lock:
            line, self._buffer = self._buffer, ""
        if line.strip():
            self._channel.send({"type": "log", "message": line})

    def isatty(self) -> bool:
        return False


def log(channel: Channel, message: str) -> None:
    """子进程没有可见控制台 (SW_HIDE), 日志只能回传给父进程。"""
    channel.send({"type": "log", "message": message})


def run_worker(host: str, port: int, token: str, *, mode: SessionMode) -> int:
    """回连父进程 -> 起服务 -> 转发指令直到 stop 或 EOF -> 收尾。

    服务由 mode.build 建好并启动, 它发出的每条消息都经 emit 回传, 其中终结类型顺带唤醒
    主线程收场。指令原样交给服务分发, 与进程内会话走的是同一份分发表。
    """
    label = mode.label
    sock = socket.create_connection((host, port), timeout=CONNECT_TIMEOUT_SECONDS)
    sock.settimeout(None)
    channel = Channel(sock)
    stopped = threading.Event()
    reader: threading.Thread | None = None

    try:
        channel.send({"type": "hello", "token": token})
        sys.stdout = sys.stderr = ChannelStream(channel)  # type: ignore[assignment]

        first = channel.recv()
        if first is None:
            return 1
        if first.get("type") != "start":
            channel.send({"type": "error", "message": f"协议错误: 期待 start, 收到 {first.get('type')!r}"})
            return 1

        from runtime import configure_runtime_env, load_maa_runtime  # noqa: PLC0415

        # 提权进程拿到的是全新环境块, 父进程 lifespan 里设的变量不会继承过来。
        configure_runtime_env()

        runtime = load_maa_runtime()
        if runtime is None:
            channel.send(
                {"type": "error", "message": f"maafw 运行时不可用, 无法{label} (缺少 maafw 依赖或初始化失败)。"}
            )
            return 1

        def emit(payload: dict) -> None:
            channel.send(payload)
            if payload.get("type") in mode.terminal_types:
                stopped.set()

        service = mode.build(runtime, emit, lambda message: log(channel, message), first)

        # 读父进程指令直到 stop 或 EOF; EOF = 父进程没了, 必须停掉服务并退出。
        def read_parent() -> None:
            parent_gone = False
            try:
                while True:
                    msg = channel.recv()
                    if msg is None:
                        parent_gone = True
                        break
                    if not service.apply_client_message(msg):
                        break
            finally:
                try:
                    service.stop()
                except Exception:  # noqa: BLE001
                    pass
                if parent_gone or mode.stop_is_blocking:
                    stopped.set()  # 已经没人收终结消息, 或服务已经收完尾
                elif not stopped.wait(STOP_GRACE_SECONDS):
                    # 服务线程卡在调用里没能发出终结消息, 不能无限等。
                    log(channel, f"停止后 {STOP_GRACE_SECONDS:.0f}s 未收尾, 强制退出。")
                    channel.send({"type": "error", "message": f"{label}停止超时, 已强制结束子进程。"})
                    stopped.set()

        reader = threading.Thread(target=read_parent, daemon=True)
        reader.start()
        stopped.wait()
        return 0
    except Exception as exc:  # noqa: BLE001
        try:
            channel.send({"type": "error", "message": f"{label}子进程异常: {exc}"})
            log(channel, traceback.format_exc())
        except Exception:  # noqa: BLE001
            pass
        return 1
    finally:
        stream = sys.stdout
        if isinstance(stream, ChannelStream):
            stream.flush()
            sys.stdout = sys.__stdout__
            sys.stderr = sys.__stderr__
        # 半关闭 -> 等父进程收完最后一条消息后回关, 再真正 close (见 half_close)。
        channel.half_close()
        if reader is not None and reader.is_alive():
            reader.join(LINGER_SECONDS)  # reader 占着读端, 由它读到 EOF
        else:
            channel.drain(LINGER_SECONDS)
        channel.close()


def worker_main() -> int:
    parser = argparse.ArgumentParser(description="MapNavigator 提权会话子进程 (由 serve.py 拉起)")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--token", required=True)
    parser.add_argument("--mode", required=True, choices=sorted(MODES))
    args = parser.parse_args()
    return run_worker(args.host, args.port, args.token, mode=MODES[args.mode])
