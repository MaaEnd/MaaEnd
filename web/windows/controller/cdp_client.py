"""最小 CDP 客户端（同步调用 + 后台读取线程 + 多指触摸 + screencast 帧缓存）。

只依赖 websocket-client + numpy + pillow，不依赖 Selenium/Playwright。
Chrome / Edge / WebView2 通用：开了 `--remote-debugging-port` 的都能连。

两种取帧方式：
  * `screenshot_bgr()`            —— Page.captureScreenshot，按需拉取
  * `start_screencast()` + 同上    —— 页面持续推帧，本地缓存最新一帧（静态页面也能 0 延迟取图）

实测（WebView2 155 / 1280x720）：
  静态页 captureScreenshot ≈ 244 ms/帧；动画页 ≈ 41 ms/帧；screencast ≈ 89 fps 推流。
"""

from __future__ import annotations

import base64
import contextlib
import io
import json
import threading
import time
import urllib.request
from collections.abc import Callable
from typing import Any

import numpy as np
import websocket
from PIL import Image


class CDPError(RuntimeError):
    """CDP 返回了 error 字段。"""


class CDPClient:
    def __init__(self, port: int = 9338, page_match: str | None = None,
                 host: str = "127.0.0.1", timeout: float = 20.0):
        self.host = host
        self.port = port
        self.page_match = page_match
        self.timeout = timeout
        self.ws: websocket.WebSocket | None = None
        self.target_url: str = ""

        self._id = 0
        self._lock = threading.Lock()
        self._pending: dict[int, tuple[threading.Event, list]] = {}
        self._handlers: dict[str, Callable[[dict], None]] = {}
        self._reader: threading.Thread | None = None
        self._closed = False

        self._touches: dict[int, tuple[float, float]] = {}
        self._frame_b64: str | None = None
        self._frame_ts: float = 0.0
        self._screencast = False

    # ---------- 连接 ----------

    def _http_json(self, path: str) -> Any:
        url = f"http://{self.host}:{self.port}{path}"
        with urllib.request.urlopen(url, timeout=5) as r:
            return json.load(r)

    def targets(self) -> list[dict]:
        return self._http_json("/json")

    def version(self) -> dict:
        return self._http_json("/json/version")

    def _pick_target(self) -> dict | None:
        pages = [t for t in self.targets()
                 if t.get("type") == "page"
                 and not t.get("url", "").startswith(("chrome://", "devtools://", "edge://"))]
        if not pages:
            return None
        if self.page_match:
            for t in pages:
                if self.page_match in t.get("url", ""):
                    return t
        for t in pages:
            if t.get("url"):
                return t
        return pages[0]

    def connect(self, retries: int = 40, interval: float = 0.5) -> bool:
        """连到页面 target；端口刚起来时页面可能还没建好，故带重试。"""
        last: Exception | None = None
        for _ in range(retries):
            try:
                t = self._pick_target()
                if not t:
                    time.sleep(interval)
                    continue
                # suppress_origin=True：不发 Origin 头。浏览器对带 Origin 的连接要求
                # --remote-allow-origins（等于向本机任意网页开放调试口），不发就不需要。
                self.ws = websocket.create_connection(t["webSocketDebuggerUrl"],
                                                      timeout=self.timeout, max_size=None,
                                                      suppress_origin=True)
                self.target_url = t.get("url", "")
                self._closed = False
                self._reader = threading.Thread(target=self._read_loop, name="cdp-reader", daemon=True)
                self._reader.start()
                return True
            except Exception as e:
                last = e
                time.sleep(interval)
        raise CDPError(f"无法连接 CDP {self.host}:{self.port}: {last}")

    def close(self) -> None:
        self._closed = True
        try:
            if self.ws:
                self.ws.close()
        except Exception:
            pass
        self.ws = None

    def ensure(self) -> None:
        """页面跳转/新开标签页后旧 WS 会失效，这里做一次轻量重连。"""
        if self.ws is None or self._closed:
            self.close()
            self.connect()

    # ---------- 收发 ----------

    def _read_loop(self) -> None:
        ws = self.ws
        if ws is None:
            return
        while not self._closed:
            try:
                raw = ws.recv()
            except Exception:
                if self._closed:
                    return
                time.sleep(0.02)
                continue
            if not raw:
                return
            try:
                msg = json.loads(raw)
            except Exception:
                continue
            mid = msg.get("id")
            if mid is not None:
                slot = self._pending.pop(mid, None)
                if slot:
                    slot[1].append(msg)
                    slot[0].set()
                continue
            fn = self._handlers.get(msg.get("method", ""))
            if fn:
                with contextlib.suppress(Exception):
                    fn(msg.get("params") or {})

    def on(self, method: str, fn: Callable[[dict], None]) -> None:
        self._handlers[method] = fn

    def _next_id(self) -> int:
        with self._lock:
            self._id += 1
            return self._id

    def __call__(self, method: str, **params) -> dict:
        self.ensure()
        ws = self.ws
        if ws is None:
            raise CDPError(f"{method}: 未连接 CDP")
        mid = self._next_id()
        ev = threading.Event()
        box: list = []
        self._pending[mid] = (ev, box)
        ws.send(json.dumps({"id": mid, "method": method, "params": params}))
        if not ev.wait(self.timeout):
            self._pending.pop(mid, None)
            raise CDPError(f"{method}: 超时 {self.timeout}s")
        msg = box[0]
        if "error" in msg:
            raise CDPError(f"{method}: {msg['error']}")
        return msg.get("result", {})

    def _fire(self, method: str, **params) -> None:
        """发出去但不等回复（用于 screencastFrameAck，避免在读取线程里自锁）。"""
        ws = self.ws
        if ws is None:
            return
        ws.send(json.dumps({"id": self._next_id(), "method": method, "params": params}))

    def eval(self, expr: str, await_promise: bool = False) -> Any:
        r = self("Runtime.evaluate", expression=expr, returnByValue=True, awaitPromise=await_promise)
        res = r.get("result", {})
        if res.get("subtype") == "error":
            raise CDPError(res.get("description", "JS error"))
        return res.get("value")

    # ---------- 视图 ----------

    def set_viewport(self, width: int, height: int, dpr: float = 1.0, mobile: bool = False) -> None:
        self("Emulation.setDeviceMetricsOverride", width=width, height=height,
             deviceScaleFactor=dpr, mobile=mobile)

    def clear_viewport(self) -> None:
        self("Emulation.clearDeviceMetricsOverride")

    def enable_touch(self, points: int = 5) -> None:
        self("Emulation.setTouchEmulationEnabled", enabled=True, maxTouchPoints=points)

    # ---------- 截图 ----------

    def screenshot_png(self, clip: dict | None = None, scale: float | None = None) -> bytes:
        params: dict[str, Any] = {"format": "png", "captureBeyondViewport": False}
        if clip:
            c = dict(clip)
            if scale:
                c["scale"] = scale
            params["clip"] = c
        r = self("Page.captureScreenshot", **params)
        return base64.b64decode(r["data"])

    def start_screencast(self, quality: int = 75, max_w: int = 1280, max_h: int = 720) -> None:
        if self._screencast:
            return
        self.on("Page.screencastFrame", self._on_frame)
        self("Page.enable")
        self("Page.startScreencast", format="jpeg", quality=quality,
             maxWidth=max_w, maxHeight=max_h, everyNthFrame=1)
        self._screencast = True

    def stop_screencast(self) -> None:
        if not self._screencast:
            return
        with contextlib.suppress(CDPError):
            self("Page.stopScreencast")
        self._screencast = False

    def _on_frame(self, p: dict) -> None:
        self._frame_b64 = p.get("data")
        self._frame_ts = time.time()
        sid = p.get("sessionId")
        if sid is not None:
            self._fire("Page.screencastFrameAck", sessionId=sid)

    def screenshot_bgr(self, clip: dict | None = None, use_cache: bool = True) -> np.ndarray:
        """返回 (H, W, 3) uint8 BGR，符合 Maa ImageBuffer 约定。"""
        png: bytes
        if use_cache and self._screencast and self._frame_b64 and (time.time() - self._frame_ts) < 2.0:
            png = base64.b64decode(self._frame_b64)
        else:
            png = self.screenshot_png(clip=clip)
        img = Image.open(io.BytesIO(png)).convert("RGB")
        rgb = np.asarray(img, dtype=np.uint8)
        return np.ascontiguousarray(rgb[:, :, ::-1])

    # ---------- 触摸（多指）----------

    def _points(self) -> list[dict]:
        return [{"x": x, "y": y, "id": cid, "force": 1}
                for cid, (x, y) in sorted(self._touches.items())]

    def touch_down(self, contact: int, x: float, y: float) -> None:
        self._touches[contact] = (x, y)
        self("Input.dispatchTouchEvent", type="touchStart", touchPoints=self._points())

    def touch_move(self, contact: int, x: float, y: float) -> None:
        if contact not in self._touches:
            return
        self._touches[contact] = (x, y)
        self("Input.dispatchTouchEvent", type="touchMove", touchPoints=self._points())

    def touch_up(self, contact: int) -> None:
        if contact not in self._touches:
            return
        self._touches.pop(contact, None)
        self("Input.dispatchTouchEvent", type="touchEnd", touchPoints=self._points())

    def tap(self, x: float, y: float, contact: int = 0, hold_ms: int = 30) -> None:
        self.touch_down(contact, x, y)
        if hold_ms > 0:
            time.sleep(hold_ms / 1000)
        self.touch_up(contact)

    def swipe(self, x1: float, y1: float, x2: float, y2: float,
              duration_ms: int = 300, contact: int = 0, step_ms: int = 16) -> None:
        steps = max(2, int(duration_ms / step_ms))
        self.touch_down(contact, x1, y1)
        for i in range(1, steps + 1):
            t = i / steps
            self.touch_move(contact, x1 + (x2 - x1) * t, y1 + (y2 - y1) * t)
            time.sleep(step_ms / 1000)
        self.touch_up(contact)

    # ---------- 键盘 ----------

    def key(self, code: int, down: bool) -> None:
        self("Input.dispatchKeyEvent",
             type="keyDown" if down else "keyUp",
             windowsVirtualKeyCode=int(code),
             nativeVirtualKeyCode=int(code))

    def text(self, s: str) -> None:
        self("Input.insertText", text=s)
