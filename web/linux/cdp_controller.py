#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""CdpController —— 用 CDP 直接读云串流 <video> 原生帧，并用 CDP 注入鼠标/键盘。

为什么需要它：合成器截图那条链路是 1080p →（浏览器放大）1440p →（MaaFw 缩小）720p，
两次重采样后锐度只有原生帧方案的 ~43%（实测 501 vs 1174）。
本控制器改为：<video> 原生 1920x1080 → JPEG → Python 解码 → AREA(BOX) 降到 1280x720。

实测要点（详见 web/docs/cdp-native-capture.md）：
- 降采样必须放在 Python（JS 的 drawImage 只有 BOX 的 77~79%）；
- 抓帧尺寸固定 1280x720：上游 650 个模板都在这个空间，MaaFw 不做模板缩放；
- 单次抓帧约 190ms，用后台线程按 ~6fps 预热，动作后强制取新帧。

自测：
    python3 cdp_controller.py --selftest
    python3 cdp_controller.py --click 640 360
    python3 cdp_controller.py --key 27
"""
from __future__ import annotations

import argparse
import base64
import io
import json
import os
import sys
import threading
import time
import urllib.request

import numpy as np
import websocket
from PIL import Image

CAP_W, CAP_H = 1280, 720
DEFAULT_PORT = 9334

# Win32 虚拟键码 → (key, code, text)
VK = {
    8: ("Backspace", "Backspace", None),
    9: ("Tab", "Tab", None),
    13: ("Enter", "Enter", "\r"),
    16: ("Shift", "ShiftLeft", None),
    17: ("Control", "ControlLeft", None),
    18: ("Alt", "AltLeft", None),
    19: ("Pause", "Pause", None),
    20: ("CapsLock", "CapsLock", None),
    27: ("Escape", "Escape", None),
    32: (" ", "Space", " "),
    33: ("PageUp", "PageUp", None),
    34: ("PageDown", "PageDown", None),
    35: ("End", "End", None),
    36: ("Home", "Home", None),
    37: ("ArrowLeft", "ArrowLeft", None),
    38: ("ArrowUp", "ArrowUp", None),
    39: ("ArrowRight", "ArrowRight", None),
    40: ("ArrowDown", "ArrowDown", None),
    45: ("Insert", "Insert", None),
    46: ("Delete", "Delete", None),
}
for _i in range(10):          # 48..57 → 0..9
    VK[48 + _i] = (str(_i), "Digit%d" % _i, str(_i))
for _i in range(26):          # 65..90 → A..Z
    _c = chr(65 + _i)
    VK[65 + _i] = (_c.lower(), "Key" + _c, _c.lower())
for _i in range(1, 13):       # 112..123 → F1..F12
    VK[111 + _i] = ("F%d" % _i, "F%d" % _i, None)


def _decode_data_url(s: str) -> bytes:
    b64 = s.split(",", 1)[1]
    return base64.b64decode(b64 + "=" * (-len(b64) % 4))


class _Sess:
    """一条 CDP websocket 连接。抓帧线程与输入通道各用一条，互不阻塞。"""

    def __init__(self, url: str):
        self.ws = websocket.create_connection(url, timeout=40, suppress_origin=True)
        self.ws.settimeout(40)
        self._id = 0
        self.lock = threading.Lock()

    def call(self, method: str, **params):
        with self.lock:
            self._id += 1
            self.ws.send(json.dumps({"id": self._id, "method": method, "params": params}))
            while True:
                r = json.loads(self.ws.recv())
                if r.get("id") == self._id:
                    if "error" in r:
                        raise RuntimeError("%s: %s" % (method, str(r["error"])[:160]))
                    return r.get("result", {})

    def ev(self, expr: str, await_p: bool = False):
        r = self.call("Runtime.evaluate", expression=expr, returnByValue=True,
                      awaitPromise=await_p)
        res = r.get("result", {})
        if res.get("subtype") == "error":
            raise RuntimeError(str(res.get("description"))[:200])
        return res.get("value")

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


class CdpController:
    """鸭子类型实现 maa.controller.CustomController 的回调集合。

    make_custom_controller() 会在运行时与真的 CustomController 组合成子类。
    """

    JS_RECT = r"""
    (() => { const v=document.querySelector('video');
      if(!v) return '';
      const r=v.getBoundingClientRect();
      return JSON.stringify({l:r.left,t:r.top,w:r.width,h:r.height,
                             vw:v.videoWidth,vh:v.videoHeight}); })()
    """

    JS_FRAME = r"""
    (() => {
      const v = document.querySelector('video');
      if (!v || !v.videoWidth) return '';
      const c = document.createElement('canvas');
      c.width = v.videoWidth; c.height = v.videoHeight;
      c.getContext('2d').drawImage(v, 0, 0, c.width, c.height);
      return c.toDataURL('image/jpeg', 0.78);
    })()
    """

    def __init__(self, port: int = DEFAULT_PORT, url_hint: str = "endfield",
                 fps: float = 6.0, max_age_ms: int = 400):
        self.port = port
        self.url_hint = url_hint
        self.fps = fps
        self.max_age_ms = max_age_ms
        self._cap = None            # _Sess：抓帧线程专用
        self._io = None             # _Sess：输入/查询
        self._rect = None           # (left, top, width, height) CSS px
        self._vw = self._vh = 0
        self._latest = None
        self._latest_t = 0.0
        self._dirty = True
        self._stop = threading.Event()
        self._worker = None
        self.last_source = ""       # "video" | "page"
        self.last_ms = 0.0
        self._last_xy = (CAP_W // 2, CAP_H // 2)

    # ---------------- 连接 ----------------
    def _target_url(self) -> str:
        ts = json.loads(urllib.request.urlopen(
            "http://127.0.0.1:%d/json/list" % self.port, timeout=6).read())
        pages = [t for t in ts if t.get("type") == "page"]
        if self.url_hint:
            hit = [t for t in pages if self.url_hint in (t.get("url") or "")]
            pages = hit or pages
        if not pages:
            raise RuntimeError("没有 page target")
        return pages[0]["webSocketDebuggerUrl"]

    def connect(self) -> bool:
        try:
            url = self._target_url()
            self._cap = _Sess(url)
            self._io = _Sess(url)
            self._refresh_rect()
            self._start_worker()
            return True
        except Exception:
            return False

    def _refresh_rect(self):
        v = self._io.ev(self.JS_RECT)
        if not v:
            self._rect = None
            return False
        d = json.loads(v)
        self._rect = (d["l"], d["t"], d["w"], d["h"])
        self._vw, self._vh = d["vw"], d["vh"]
        return True

    def _start_worker(self):
        if self._worker and self._worker.is_alive():
            return
        self._stop.clear()

        def loop():
            period = 1.0 / max(1.0, self.fps)
            while not self._stop.is_set():
                t0 = time.time()
                try:
                    f = self._grab(self._cap)
                    self._latest = f
                    self._latest_t = time.time()
                    self._dirty = False
                except Exception:
                    try:                       # 连接可能被页面刷新断掉
                        self._cap = _Sess(self._target_url())
                    except Exception:
                        time.sleep(0.5)
                dt = time.time() - t0
                if dt < period:
                    time.sleep(period - dt)

        self._worker = threading.Thread(target=loop, name="cdp-cap", daemon=True)
        self._worker.start()

    # ---------------- 抓帧 ----------------
    def _grab(self, sess: _Sess) -> np.ndarray:
        t0 = time.time()
        url = None
        try:
            url = sess.ev(self.JS_FRAME)
        except Exception:
            url = None
        if url:
            self.last_source = "video"
            raw = _decode_data_url(url)
        else:                                   # 不在会话里 / 只有 DOM 弹窗 → 页面截图兜底
            self.last_source = "page"
            sh = sess.call("Page.captureScreenshot", format="jpeg", quality=88)
            raw = _decode_data_url("data:," + sh["data"])
        img = Image.open(io.BytesIO(raw)).convert("RGB")
        if img.size != (CAP_W, CAP_H):
            img = img.resize((CAP_W, CAP_H), Image.BOX)   # AREA：降采样最锐利
        self.last_ms = (time.time() - t0) * 1000
        return np.asarray(img)[:, :, ::-1].copy()          # MaaFw 要 BGR

    def screencap(self) -> np.ndarray:
        age = (time.time() - self._latest_t) * 1000
        if not self._dirty and self._latest is not None and age < self.max_age_ms:
            return self._latest.copy()          # 预抓线程已备好，直接给
        frame = self._grab(self._cap)           # 动作后/过期 → 同步取新帧（约 190ms）
        self._latest, self._latest_t = frame, time.time()
        self._dirty = False
        return frame.copy()

    def invalidate(self):
        """外部改了页面状态（JS 点了按钮等）后调用，强制下一帧重抓。"""
        self._dirty = True

    # ---------------- 坐标 / 输入 ----------------
    def _to_css(self, x: int, y: int):
        """1280x720 抓帧坐标 → 页面 CSS 坐标。"""
        if self._rect is None:
            self._refresh_rect()
        if self._rect is None:
            return x, y
        l, t, w, h = self._rect
        return l + x * w / CAP_W, t + y * h / CAP_H

    def _mouse(self, kind: str, x: int, y: int, buttons: int = 1, clicks: int = 0):
        self._last_xy = (x, y)
        cx, cy = self._to_css(x, y)
        self._io.call("Input.dispatchMouseEvent", type=kind, x=cx, y=cy,
                      button="left", buttons=buttons, clickCount=clicks,
                      pointerType="mouse")
        self._dirty = True

    def click(self, x: int, y: int) -> bool:
        try:
            self._mouse("mousePressed", x, y, buttons=1, clicks=1)
            time.sleep(0.03)
            self._mouse("mouseReleased", x, y, buttons=0, clicks=1)
            return True
        except Exception:
            return False

    def touch_down(self, contact: int, x: int, y: int, pressure: int) -> bool:
        try:
            self._mouse("mousePressed", x, y, buttons=1, clicks=1)
            return True
        except Exception:
            return False

    def touch_move(self, contact: int, x: int, y: int, pressure: int) -> bool:
        try:
            self._mouse("mouseMoved", x, y, buttons=1)
            return True
        except Exception:
            return False

    def touch_up(self, contact: int) -> bool:
        try:
            self._mouse("mouseReleased", self._last_xy[0], self._last_xy[1],
                        buttons=0, clicks=1)
            return True
        except Exception:
            return False

    def swipe(self, x1: int, y1: int, x2: int, y2: int, duration: int) -> bool:
        try:
            self._mouse("mousePressed", x1, y1, buttons=1, clicks=1)
            steps = max(6, min(30, max(1, duration) // 16))
            for i in range(1, steps + 1):
                self._mouse("mouseMoved",
                            int(x1 + (x2 - x1) * i / steps),
                            int(y1 + (y2 - y1) * i / steps), buttons=1)
                time.sleep(max(0.004, duration / steps / 1000.0))
            self._mouse("mouseReleased", x2, y2, buttons=0, clicks=1)
            return True
        except Exception:
            return False

    def _key(self, kind: str, keycode: int) -> bool:
        key, code, text = VK.get(keycode, ("", "", None))
        p = {"type": kind, "windowsVirtualKeyCode": keycode,
             "nativeVirtualKeyCode": keycode, "code": code, "key": key}
        if kind == "rawKeyDown" and text:
            p["text"] = text
        try:
            self._io.call("Input.dispatchKeyEvent", **p)
            self._dirty = True
            return True
        except Exception:
            return False

    def key_down(self, keycode: int) -> bool:
        return self._key("rawKeyDown", keycode)

    def key_up(self, keycode: int) -> bool:
        return self._key("keyUp", keycode)

    def input_text(self, text: str) -> bool:
        try:
            self._io.call("Input.insertText", text=text)
            self._dirty = True
            return True
        except Exception:
            return False

    def scroll(self, dx: int, dy: int) -> bool:
        try:
            cx, cy = self._to_css(CAP_W // 2, CAP_H // 2)
            self._io.call("Input.dispatchMouseEvent", type="mouseWheel",
                          x=cx, y=cy, deltaX=dx, deltaY=dy)
            self._dirty = True
            return True
        except Exception:
            return False

    # ---------------- 杂项 ----------------
    def request_uuid(self) -> str:
        return "cdp-cloud-%d" % int(time.time())

    def get_custom_info(self) -> dict:
        return {"source": self.last_source, "cap": [CAP_W, CAP_H],
                "video": [self._vw, self._vh]}

    def shell(self, cmd: str, timeout: int = 2000):
        return None

    def inactive(self) -> bool:
        return False

    def close(self):
        self._stop.set()
        for s in (self._cap, self._io):
            if s:
                s.close()


def make_custom_controller(**kw):
    """构造真正可交给 MaaFw 的 CustomController 实例。"""
    from maa.controller import CustomController

    class _Cdp(CdpController, CustomController):
        def __init__(self, **kwargs):
            CdpController.__init__(self, **kwargs)
            CustomController.__init__(self)

    return _Cdp(**kw)


# ------------------------------------------------------------------ 自测
def _main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=DEFAULT_PORT)
    ap.add_argument("--selftest", action="store_true")
    ap.add_argument("--out", default="/mnt/d/maaend-dist/cdp_cap.png")
    ap.add_argument("--click", nargs=2, type=int)
    ap.add_argument("--key", type=int)
    ap.add_argument("--repeat", type=int, default=4)
    a = ap.parse_args()

    c = CdpController(port=a.port)
    print("connect =", c.connect())
    print("video   =", c._vw, "x", c._vh, " rect =", c._rect)
    time.sleep(1.5)                      # 让预抓线程填一帧

    if a.selftest:
        for i in range(a.repeat):
            t0 = time.time()
            f = c.screencap()
            ms = (time.time() - t0) * 1000
            Image.fromarray(f[:, :, ::-1]).save(a.out)
            g = f.astype(np.float32).mean(axis=2)
            lap = float(((-4 * g[1:-1, 1:-1] + g[:-2, 1:-1] + g[2:, 1:-1]
                          + g[1:-1, :-2] + g[1:-1, 2:]) ** 2).mean())
            print("  #%d %s 来源=%s screencap=%.0fms 抓帧=%.0fms 锐度=%.1f mean=%.1f"
                  % (i + 1, f.shape, c.last_source, ms, c.last_ms, lap, f.mean()))
    if a.click:
        print("click", a.click, "->", c.click(*a.click), "css=", c._to_css(*a.click))
    if a.key is not None:
        print("key", a.key, "down=", c.key_down(a.key), "up=", c.key_up(a.key))
    c.close()


if __name__ == "__main__":
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    _main()
