"""把 CDP 包装成 MaaFramework 的自定义控制器。

Maa 侧看到的是一台"会截图、会多指触摸"的设备；实际上画面来自 WebView2/Chrome，
输入走 CDP，不碰系统光标、不需要窗口在前台。

依赖：maafw、numpy、pillow、websocket-client

用法（与内置控制器一致）：

    from web_controller import WebController
    from maa.resource import Resource
    from maa.tasker import Tasker

    ctrl = WebController(port=9338)
    res = Resource()
    res.load("assets/resource", "assets/resource_web")
    tasker = Tasker()
    tasker.bind(res, ctrl)
    tasker.post_task("入口节点").wait()
"""

from __future__ import annotations

import contextlib
import os
import time
from typing import Any

import numpy as np
from cdp_client import CDPClient, CDPError
from maa.controller import CustomController

TOUCH_MODE_JS = r"""
(() => {
  const target = [...document.querySelectorAll('div,span,button,i,a')]
    .filter(el => (el.innerText || '').trim() === '触屏')
    .filter(el => !el.querySelector('div,span,button'))
    .map(el => ({ el, r: el.getBoundingClientRect() }))
    .filter(o => o.r.width > 0 && o.r.height > 0)
    .sort((a, b) => b.r.width - a.r.width)[0];
  if (!target) return null;
  return { x: target.r.x + target.r.width / 2, y: target.r.y + target.r.height / 2,
           cls: String(target.el.className).slice(0, 80) };
})()
"""


class WebController(CustomController):
    """CDP（Chrome / Edge / WebView2）控制器。"""

    def __init__(self, port: int = 9338, page_match: str | None = None,
                 viewport: tuple[int, int] = (1280, 720),
                 switch_touch_mode: bool = False,
                 screencast: bool = True,
                 log_path: str | None = None):
        super().__init__()
        self.port = port
        self.viewport = viewport
        self.switch_touch_mode = switch_touch_mode
        self.screencast = screencast
        self.client = CDPClient(port=port, page_match=page_match)
        self.log_path = log_path or os.path.join(os.path.dirname(os.path.abspath(__file__)), "web-controller.log")

    # ---------- 日志 ----------

    def _log(self, msg: str) -> None:
        line = f"[{time.strftime('%H:%M:%S')}] {msg}"
        with contextlib.suppress(OSError), open(self.log_path, "a", encoding="utf-8") as f:
            f.write(line + "\n")
        print(line, flush=True)

    # ---------- Maa 控制器接口 ----------

    def connect(self) -> bool:
        try:
            self.client.connect()
            v = self.client.version()
            self._log(f"connected: {v.get('Browser')} target={self.client.target_url[:90]}")
            w, h = self.viewport
            self.client.set_viewport(w, h)
            self.client.enable_touch(5)
            if self.screencast:
                self.client.start_screencast(max_w=w, max_h=h)
                self._log("screencast: on（本地缓存最新帧）")
            if self.switch_touch_mode:
                self._log(f"touch mode: {'on' if self.switch_touch_mode_on() else '未找到「触屏」按钮'}")
            return True
        except Exception as e:
            self._log(f"connect failed: {type(e).__name__}: {e}")
            return False

    def request_uuid(self) -> str:
        return f"web-cdp:{self.port}"

    def screencap(self) -> np.ndarray:
        return self.client.screenshot_bgr()

    def click(self, x: int, y: int) -> bool:
        self.client.tap(x, y, contact=0)
        return True

    def swipe(self, x1: int, y1: int, x2: int, y2: int, duration: int) -> bool:
        self.client.swipe(x1, y1, x2, y2, duration_ms=max(int(duration), 16), contact=0)
        return True

    def touch_down(self, contact: int, x: int, y: int, pressure: int) -> bool:
        self.client.touch_down(contact, x, y)
        return True

    def touch_move(self, contact: int, x: int, y: int, pressure: int) -> bool:
        self.client.touch_move(contact, x, y)
        return True

    def touch_up(self, contact: int) -> bool:
        self.client.touch_up(contact)
        return True

    def key_down(self, keycode: int) -> bool:
        self.client.key(keycode, down=True)
        return True

    def key_up(self, keycode: int) -> bool:
        self.client.key(keycode, down=False)
        return True

    def click_key(self, keycode: int) -> bool:
        self.client.key(keycode, down=True)
        time.sleep(0.03)
        self.client.key(keycode, down=False)
        return True

    def input_text(self, text: str) -> bool:
        self.client.text(text)
        return True

    def start_app(self, intent: str) -> bool:
        """云游戏不需要"启动 App"：页面已在宿主里加载，这里只保证 target 还在。"""
        try:
            self.client.ensure()
            return True
        except Exception:
            return False

    def stop_app(self, intent: str) -> bool:
        return True

    # ---------- 可选增强 ----------

    def scroll(self, dx: int, dy: int) -> bool:
        try:
            self.client("Input.dispatchMouseEvent", type="mouseWheel",
                        x=self.viewport[0] // 2, y=self.viewport[1] // 2,
                        deltaX=int(dx), deltaY=int(dy))
            return True
        except CDPError:
            return False

    def relative_move(self, dx: int, dy: int) -> bool:
        # 网页端没有相对鼠标模式；视角走 touch（contact=1）。
        return False

    def switch_touch_mode_on(self) -> bool:
        """点客户端工具栏里的「触屏」，切到触摸输入模式。"""
        try:
            hit: Any = self.client.eval(TOUCH_MODE_JS)
            if not hit:
                return False
            self.client.tap(hit["x"], hit["y"], contact=0, hold_ms=40)
            self._log(f"clicked 触屏 @({hit['x']:.0f},{hit['y']:.0f}) cls={hit.get('cls')}")
            time.sleep(0.8)
            return True
        except Exception as e:
            self._log(f"switch_touch_mode failed: {e}")
            return False
