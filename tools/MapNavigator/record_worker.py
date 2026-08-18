"""提权录制子进程: 只跑录制, 由 serve.py 用 runas 拉起并回连它的 127.0.0.1 端口。

热键要管理员权限, 但服务是长驻进程不能自己 runas 重启 (会另起一整套服务并丢掉页面
状态), 所以只把录制这一段拆出来提权。

协议 (NDJSON):
    -> {"type":"hello","token":...}       父进程校验后才继续
    <- {"type":"start","config":{...}}    前端原始 payload
    -> 录制事件, 与进程内录制推给前端的 JSON 完全一致
    <- {"type":"stop"}                    随后本进程发 finished
socket EOF 即退出 —— 父进程 kill 不掉更高完整性级别的进程, 只能靠这个避免孤儿。
"""

from __future__ import annotations

import argparse
import json
import socket
import sys
import threading
import traceback
from pathlib import Path

# 与 serve.py 同样的 bare-name import 约定: 本目录必须在 sys.path 最前。
# 提权子进程的工作目录不可靠 (ShellExecuteW 给的是全新环境), 不能指望 cwd。
_TOOL_DIR = Path(__file__).resolve().parent
if str(_TOOL_DIR) not in sys.path:
    sys.path.insert(0, str(_TOOL_DIR))

from clipboard import copy_to_clipboard  # noqa: E402
from connection_models import session_config_from_payload  # noqa: E402

CONNECT_TIMEOUT_SECONDS = 30.0
# 收到 stop 后等录制线程交出 finished 的宽限期 (一轮采样可能正卡在识别调用里)。
STOP_GRACE_SECONDS = 20.0
# 半关闭后等父进程收完并回关的上限。
LINGER_SECONDS = 5.0


class _Channel:
    """回连 socket 的 NDJSON 读写封装。

    send 会被录制线程和 pynput 热键线程并发调用, 故加锁。
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


def _log(channel: _Channel, message: str) -> None:
    """子进程没有可见控制台 (SW_HIDE), 日志只能回传给父进程。"""
    channel.send({"type": "log", "message": message})


class _ChannelStream:
    """把 print/traceback 按行转成 log 消息回传父进程。

    recording_service 全程用 print 打诊断, 子进程窗口是隐藏的, 不接过来就全丢了。
    """

    def __init__(self, channel: _Channel) -> None:
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


def run(host: str, port: int, token: str) -> int:
    sock = socket.create_connection((host, port), timeout=CONNECT_TIMEOUT_SECONDS)
    sock.settimeout(None)
    channel = _Channel(sock)
    stopped = threading.Event()
    reader: threading.Thread | None = None

    try:
        channel.send({"type": "hello", "token": token})
        sys.stdout = sys.stderr = _ChannelStream(channel)  # type: ignore[assignment]

        first = channel.recv()
        if first is None:
            return 1
        if first.get("type") != "start":
            channel.send({"type": "error", "message": f"协议错误: 期待 start, 收到 {first.get('type')!r}"})
            return 1

        from recording_service import LivePosition, RecordingService  # noqa: PLC0415
        from runtime import configure_runtime_env, load_maa_runtime  # noqa: PLC0415

        # 提权进程拿到的是全新环境块, 父进程 lifespan 里设的变量不会继承过来。
        configure_runtime_env()

        runtime = load_maa_runtime()
        if runtime is None:
            channel.send({"type": "error", "message": "maafw 运行时不可用, 无法录制 (缺少 maafw 依赖或初始化失败)。"})
            return 1

        def on_finished(points: list) -> None:
            channel.send({"type": "finished", "points": points})
            stopped.set()

        def on_error(message: str) -> None:
            channel.send({"type": "error", "message": message})
            stopped.set()

        def on_clipboard(coord: str, status: str) -> None:
            # 本进程已提权, 由它直接写剪贴板 (游戏持有焦点)。
            copy_to_clipboard(coord, log=lambda m: _log(channel, m))
            channel.send({"type": "toast", "coord": coord, "status": status})

        def on_live_position(position: LivePosition) -> None:
            channel.send(
                {
                    "type": "position",
                    "x": position.x,
                    "y": position.y,
                    "zone": position.zone,
                    "rot": position.rot,
                }
            )

        service = RecordingService(
            runtime=runtime,
            on_status=lambda text, color: channel.send({"type": "status", "text": text, "color": color}),
            on_finished=on_finished,
            on_error=on_error,
            on_locator_detail=lambda text: channel.send({"type": "locator", "text": text}),
            on_clipboard=on_clipboard,
            on_force_waypoint=lambda x, y, zone: channel.send(
                {"type": "force_waypoint", "x": x, "y": y, "zone": zone}
            ),
            on_live_position=on_live_position,
        )
        service.start(session_config_from_payload(first.get("config") or {}))

        # 读父进程指令直到 stop 或 EOF; EOF = 父进程没了, 必须停录制并退出。
        def read_parent() -> None:
            parent_gone = False
            try:
                while True:
                    msg = channel.recv()
                    if msg is None:
                        parent_gone = True
                        break
                    if msg.get("type") == "stop":
                        break
            finally:
                try:
                    service.stop()  # 非阻塞: 只清标志, 录制线程随后自己走 on_finished
                except Exception:  # noqa: BLE001
                    pass
                if parent_gone:
                    stopped.set()  # 已经没人收 finished 了, 直接退
                elif not stopped.wait(STOP_GRACE_SECONDS):
                    # 录制线程卡在采样调用里没能发出 finished, 不能无限等。
                    _log(channel, f"停止后 {STOP_GRACE_SECONDS:.0f}s 未收到 finished, 强制退出。")
                    channel.send({"type": "error", "message": "录制停止超时, 已强制结束子进程。"})
                    stopped.set()

        reader = threading.Thread(target=read_parent, daemon=True)
        reader.start()
        stopped.wait()
        return 0
    except Exception as exc:  # noqa: BLE001
        try:
            channel.send({"type": "error", "message": f"录制子进程异常: {exc}"})
            _log(channel, traceback.format_exc())
        except Exception:  # noqa: BLE001
            pass
        return 1
    finally:
        stream = sys.stdout
        if isinstance(stream, _ChannelStream):
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


def main() -> int:
    parser = argparse.ArgumentParser(description="MapNavigator 提权录制子进程 (由 serve.py 拉起)")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--token", required=True)
    args = parser.parse_args()
    return run(args.host, args.port, args.token)


if __name__ == "__main__":
    sys.exit(main())
