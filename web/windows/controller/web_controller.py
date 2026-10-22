"""把 CDP 包装成 MaaFramework 的自定义控制器。

Maa 侧看到的是一台"会截图、会多指触摸"的设备；实际上画面来自浏览器（默认无头 Edge），
输入走 CDP，不碰系统光标、不创建任何窗口。

后端由 `browser.py` 决定，并会在日志与 uuid 里带标识（`edge-headless` / `chrome-headless` / `external`）。

依赖：maafw、numpy、pillow、websocket-client

用法（与内置控制器一致）：

    from web_controller import WebController
    from maa.resource import Resource
    from maa.tasker import Tasker

    ctrl = WebController()          # 自动拉起无头浏览器 + 随机调试端口
    res = Resource()
    res.load("assets/resource", "assets/resource_web")
    tasker = Tasker()
    tasker.bind(res, ctrl)
    tasker.post_task("入口节点").wait()
    ctrl.close()
"""

from __future__ import annotations

import atexit
import contextlib
import os
import sys
import time
from typing import Any

import browser
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

# 页面就绪探测：有没有在播的视频 + 像不像登录页
PAGE_PROBE_JS = r"""
(() => {
  const v = document.querySelector('video');
  const text = (document.body ? document.body.innerText : '').slice(0, 4000);
  const title = document.title || '';
  const hit = /登录|扫码|二维码|账号|同意|sign\s?in|log\s?in|login/i;
  return {
    href: location.href, title,
    video: v ? { w: v.videoWidth, h: v.videoHeight, paused: v.paused } : null,
    login_hint: hit.test(title) || hit.test(text),
  };
})()
"""

DEFAULT_URL = "https://endfield.hypergryph.com/cloud"

# 护栏：云页面上的「全屏」按钮会调 requestFullscreen，指针锁会 requestPointerLock——
# 两者都可能把用户的桌面顶掉。这里直接把它们废掉（可用 MAAEND_WEB_ALLOW_FULLSCREEN=1 关掉护栏）。
GUARD_JS = r"""
(() => {
  try {
    const block = () => Promise.reject(new Error('blocked by MaaEnd web backend'));
    const P = Element.prototype, H = HTMLElement.prototype;
    if (P.requestFullscreen) P.requestFullscreen = block;
    if (P.webkitRequestFullscreen) P.webkitRequestFullscreen = block;
    if (P.requestPointerLock) P.requestPointerLock = block;
    if (H.requestPointerLock) H.requestPointerLock = block;
  } catch (e) {}
})();
"""


class WebController(CustomController):
    """CDP 控制器：把无头浏览器伪装成一台可截图、可多指触摸的设备。"""

    def __init__(self, port: int | None = None, page_match: str | None = None,
                 viewport: tuple[int, int] = (1280, 720),
                 switch_touch_mode: bool = False,
                 screencast: bool = True,
                 log_path: str | None = None,
                 url: str | None = None,
                 strict_login: bool | None = None):
        super().__init__()
        self.port = port
        self.page_match = page_match
        self.viewport = viewport
        self.switch_touch_mode = switch_touch_mode
        self.screencast = screencast
        self.url = url or os.environ.get("MAAEND_WEB_URL") or DEFAULT_URL
        self.strict_login = (os.environ.get("MAAEND_WEB_STRICT") == "1") if strict_login is None else strict_login
        self.log_path = log_path or os.path.join(os.path.dirname(os.path.abspath(__file__)), "web-controller.log")
        self.backend: browser.Backend | None = None
        self.client: CDPClient | None = None
        self._closed = False
        atexit.register(self.close)

    # ---------- 日志 ----------

    def _log(self, msg: str) -> None:
        line = f"[{time.strftime('%H:%M:%S')}] {msg}"
        with contextlib.suppress(OSError), open(self.log_path, "a", encoding="utf-8") as f:
            f.write(line + "\n")
        print(line, flush=True)

    @property
    def cdp(self) -> CDPClient:
        """已连接的 CDP 客户端；未连接时直接报错，避免 None 上的属性访问。"""
        if self.client is None:
            raise CDPError("控制器未连接：先调用 connect()")
        return self.client

    # ---------- Maa 控制器接口 ----------

    def connect(self) -> bool:
        """拉起/连接后端 → 连 CDP → 校正视口 → 开触屏与 screencast → 探测页面状态。"""
        self._closed = False
        try:
            self.backend = browser.ensure_backend(viewport=self.viewport, url=self.url, port=self.port,
                                                  log=self._log)
            self.client = CDPClient(port=self.backend.port, host=self.backend.host,
                                    page_match=self.page_match)
            self.cdp.connect()
            self._log(f"connected: {self.backend.version} target={self.cdp.target_url[:90]}")
        except Exception as e:
            self._log(f"connect failed: {type(e).__name__}: {e}")
            self._teardown()
            return False

        try:
            # 1) 护栏：拦掉全屏/指针锁，避免误点后顶掉用户桌面
            self._inject_guards()
            # 2) 先修 UA：无头 UA 里带 HeadlessChrome，官网会弹"当前浏览器可能影响游戏体验"
            #    并劝用户去下载客户端（实测点开登录弹窗后必现）。抹掉后警告消失。
            self._sanitize_ua()
            # 3) 视口：不要用 Emulation.setDeviceMetricsOverride 强制视口——实测它会把
            #    screencast 从 ~26 fps 砍到 ~15 fps。改用窗口尺寸拿到精确的 CSS 视口。
            self._fix_viewport()
            self.cdp.enable_touch(5)
            if self.screencast:
                w, h = self.viewport
                self.cdp.start_screencast(max_w=w, max_h=h)
                self._log("screencast: on（本地缓存最新帧，静态页回落 captureScreenshot）")
            if self.switch_touch_mode:
                self._log(f"touch mode: {'on' if self.switch_touch_mode_on() else '未找到「触屏」按钮'}")
            return self._check_page()
        except Exception as e:
            self._log(f"connect failed(2): {type(e).__name__}: {e}")
            self._teardown()
            return False

    def request_uuid(self) -> str:
        """uuid 里带后端标识（含模式），MXU/日志里一眼能看出走的是哪条路。"""
        if self.backend is None:
            return "web-cdp:pending"
        if not self.backend.owned:
            return f"web-cdp:external:{self.backend.port}"
        if self.backend.kind == self.backend.mode:
            return f"web-cdp:{self.backend.kind}"
        return f"web-cdp:{self.backend.kind}-{self.backend.mode}"

    def screencap(self) -> np.ndarray:
        if self.client is None:
            raise CDPError("screencap 前必须先 connect")
        return self.cdp.screenshot_bgr()

    def click(self, x: int, y: int) -> bool:
        self.cdp.tap(x, y, contact=0)
        return True

    def swipe(self, x1: int, y1: int, x2: int, y2: int, duration: int) -> bool:
        self.cdp.swipe(x1, y1, x2, y2, duration_ms=max(int(duration), 16), contact=0)
        return True

    def touch_down(self, contact: int, x: int, y: int, pressure: int) -> bool:
        self.cdp.touch_down(contact, x, y)
        return True

    def touch_move(self, contact: int, x: int, y: int, pressure: int) -> bool:
        self.cdp.touch_move(contact, x, y)
        return True

    def touch_up(self, contact: int) -> bool:
        self.cdp.touch_up(contact)
        return True

    def key_down(self, keycode: int) -> bool:
        self.cdp.key(keycode, down=True)
        return True

    def key_up(self, keycode: int) -> bool:
        self.cdp.key(keycode, down=False)
        return True

    def click_key(self, keycode: int) -> bool:
        self.cdp.key(keycode, down=True)
        time.sleep(0.03)
        self.cdp.key(keycode, down=False)
        return True

    def input_text(self, text: str) -> bool:
        self.cdp.text(text)
        return True

    def start_app(self, intent: str) -> bool:
        """云游戏不需要"启动 App"：页面已在浏览器里加载，这里只保证 target 还在。"""
        try:
            self.cdp.ensure()
            return True
        except Exception:
            return False

    def stop_app(self, intent: str) -> bool:
        return True

    def inactive(self) -> bool:
        """Maa 在控制器空闲时调用。浏览器不在这里关——任务之间还要复用它。"""
        self._log("controller inactive（空闲，浏览器保持运行）")
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

    # ---------- 前台 / 后台切换（仅 offscreen 模式） ----------

    def pop_to_screen(self, left: int = 120, top: int = 120) -> bool:
        """把窗口挪到屏幕内交给用户操作：同进程、不重载、不丢登录态、不抢焦点。"""
        if self.backend is None or not self.backend.owned or self.backend.mode not in ("offscreen", "webview2"):
            self._log("pop_to_screen：只有 offscreen / webview2 模式支持（headless 没有窗口可切）")
            return False
        try:
            if self.backend.mode == "webview2":
                r = browser.host_cmd(self.backend.control_port,
                                     {"cmd": "show", "left": left, "top": top})
                self._log(f"pop_to_screen(宿主)：{r}")
                return bool(r.get("ok"))
            win = self.cdp("Browser.getWindowForTarget")
            self.cdp("Browser.setWindowBounds", windowId=win["windowId"],
                     bounds={"left": left, "top": top, "windowState": "normal"})
            st = self.cdp.eval("document.visibilityState")
            self._log(f"pop_to_screen：窗口 -> ({left},{top})，页面 visibilityState={st}")
            return True
        except Exception as e:
            self._log(f"pop_to_screen 失败：{type(e).__name__}: {e}")
            return False

    def park_offscreen(self) -> bool:
        """把窗口挪回屏幕外继续无人值守（不动焦点、页面仍 visible）。"""
        if self.backend is None or not self.backend.owned or self.backend.mode not in ("offscreen", "webview2"):
            self._log("park_offscreen：只有 offscreen / webview2 模式支持")
            return False
        try:
            if self.backend.mode == "webview2":
                r = browser.host_cmd(self.backend.control_port, {"cmd": "park"})
                self._log(f"park_offscreen(宿主)：{r}")
                return bool(r.get("ok"))
            win = self.cdp("Browser.getWindowForTarget")
            self.cdp("Browser.setWindowBounds", windowId=win["windowId"],
                     bounds={"left": -32000, "top": -32000})
            st = self.cdp.eval("document.visibilityState")
            self._log(f"park_offscreen：窗口 -> 屏幕外，页面 visibilityState={st}")
            return True
        except Exception as e:
            self._log(f"park_offscreen 失败：{type(e).__name__}: {e}")
            return False

    def sleep_host(self) -> bool:
        """真正停车（只有 webview2 支持）：宿主不再渲染、省 GPU，代价是自动化也跑不了。"""
        if self.backend is None or self.backend.mode != "webview2":
            self._log("sleep_host：只有 webview2 模式支持（系统浏览器没有停车原语）")
            return False
        try:
            r = browser.host_cmd(self.backend.control_port, {"cmd": "hide"})
            self._log(f"sleep_host：{r}")
            return bool(r.get("ok"))
        except Exception as e:
            self._log(f"sleep_host 失败：{type(e).__name__}: {e}")
            return False

    def resume_host(self) -> bool:
        if self.backend is None or self.backend.mode != "webview2":
            self._log("resume_host：只有 webview2 模式支持")
            return False
        try:
            r = browser.host_cmd(self.backend.control_port, {"cmd": "resume"})
            self._log(f"resume_host：{r}")
            return bool(r.get("ok"))
        except Exception as e:
            self._log(f"resume_host 失败：{type(e).__name__}: {e}")
            return False

    def switch_touch_mode_on(self) -> bool:
        """点客户端工具栏里的「触屏」，切到触摸输入模式。"""
        try:
            hit: Any = self.cdp.eval(TOUCH_MODE_JS)
            if not hit:
                return False
            self.cdp.tap(hit["x"], hit["y"], contact=0, hold_ms=40)
            self._log(f"clicked 触屏 @({hit['x']:.0f},{hit['y']:.0f}) cls={hit.get('cls')}")
            time.sleep(0.8)
            return True
        except Exception as e:
            self._log(f"switch_touch_mode failed: {e}")
            return False

    # ---------- 视口 / 页面状态 / 收尸 ----------

    def _inject_guards(self) -> None:
        """拦掉全屏与指针锁：被误点的全屏按钮不该顶掉用户的桌面。"""
        if os.environ.get("MAAEND_WEB_ALLOW_FULLSCREEN") == "1":
            self._log("MAAEND_WEB_ALLOW_FULLSCREEN=1：不注入全屏/指针锁护栏")
            return
        with contextlib.suppress(Exception):
            self.cdp("Page.addScriptToEvaluateOnNewDocument", source=GUARD_JS)
        with contextlib.suppress(Exception):
            self.cdp.eval(GUARD_JS)
        self._log("已注入护栏：禁 requestFullscreen / requestPointerLock")

    def _sanitize_ua(self) -> None:
        """把 `HeadlessChrome/x` 换成 `Chrome/x`，并同步 client hints。

        无头浏览器的 UA 会让云游戏官网判定"浏览器可能影响游戏体验"，弹出劝退提示；
        抹掉之后官网按普通 Edge 对待（实测）。外部端点不动——那是用户自己的浏览器。
        """
        if self.backend is not None and not self.backend.owned:
            return
        if os.environ.get("MAAEND_WEB_KEEP_UA") == "1":
            self._log("MAAEND_WEB_KEEP_UA=1：保留原始无头 UA")
            return
        ua = self.cdp.eval("navigator.userAgent") or ""
        if "Headless" not in ua:
            return
        clean = ua.replace("HeadlessChrome", "Chrome")
        major = "155"
        for token in ("Edg/", "Chrome/"):
            if token in clean:
                with contextlib.suppress(ValueError):
                    major = clean.split(token, 1)[1].split(".", 1)[0]
                break
        brands = [{"brand": "Microsoft Edge", "version": major},
                  {"brand": "Chromium", "version": major},
                  {"brand": "Not:A-Brand", "version": "99"}]
        full = [{"brand": b["brand"], "version": f"{major}.0.0.0"} for b in brands[:2]]
        full.append({"brand": "Not:A-Brand", "version": "99.0.0.0"})
        win = "Windows" in clean
        self.cdp("Network.enable")
        self.cdp("Network.setUserAgentOverride",
                 userAgent=clean,
                 acceptLanguage="zh-CN,zh;q=0.9,en;q=0.8",
                 platform="Win32" if win else "Linux x86_64",
                 userAgentMetadata={"brands": brands, "fullVersionList": full,
                                    "platform": "Windows" if win else "Linux",
                                    "platformVersion": "15.0.0", "architecture": "x86",
                                    "model": "", "mobile": False, "bitness": "64", "wow64": False})
        self._log(f"UA 已修正：{clean[:80]}…")
        # 首屏是在旧 UA 下加载的，重载一次让页面（含风控脚本）按新 UA 走
        self.cdp("Page.reload")
        deadline = time.time() + 15
        while time.time() < deadline:
            with contextlib.suppress(Exception):
                if self.cdp.eval("document.readyState") == "complete":
                    break
            time.sleep(0.15)

    def _fix_viewport(self) -> None:
        """用窗口尺寸把 CSS 视口校正到目标值（而不是 setDeviceMetricsOverride，那个会砍帧）。"""
        tw, th = self.viewport
        if self.backend is not None and self.backend.mode == "webview2":
            # 宿主的 ClientSize 就是 1280x720（且强制 DPR=1），不需要再改窗口
            inner = self.cdp.eval("[innerWidth, innerHeight]") or [0, 0]
            self._log(f"viewport = {inner[0]}x{inner[1]}（目标 {tw}x{th}；webview2 由宿主定尺寸）")
            return
        for _ in range(3):
            inner = self.cdp.eval("[innerWidth, innerHeight]") or [0, 0]
            if (inner[0], inner[1]) == (tw, th):
                self._log(f"viewport = {inner[0]}x{inner[1]}（精确匹配）")
                return
            try:
                win = self.client("Browser.getWindowForTarget")
                wid = win.get("windowId")
                bounds = win.get("bounds") or {}
                dw = int(bounds.get("width", inner[0]) - inner[0])
                dh = int(bounds.get("height", inner[1]) - inner[1])
                self.client("Browser.setWindowBounds", windowId=wid,
                            bounds={"width": tw + dw, "height": th + dh})
                time.sleep(0.3)
            except Exception as e:
                self._log(f"viewport 校正不可用（{type(e).__name__}: {e}），沿用浏览器实际视口")
                break
        inner = self.cdp.eval("[innerWidth, innerHeight]") or [0, 0]
        self._log(f"viewport = {inner[0]}x{inner[1]}（目标 {tw}x{th}；Maa 侧按 display 缩放换算）")

    def _diag_dir(self) -> str:
        base = os.environ.get("MAAEND_WEB_LOG_DIR")
        if not base and self.backend and self.backend.profile:
            base = os.path.join(self.backend.profile, "diagnostics")
        base = base or os.path.join(os.path.dirname(os.path.abspath(__file__)), "diagnostics")
        with contextlib.suppress(OSError):
            os.makedirs(base, exist_ok=True)
        return base

    def _check_page(self) -> bool:
        """页面就绪判断。登录/风控页会把截图落盘并给出人话提示，避免用户对着"识别失败"猜谜。"""
        try:
            info: dict = self.cdp.eval(PAGE_PROBE_JS) or {}
        except Exception as e:
            self._log(f"页面探测失败（忽略）：{type(e).__name__}: {e}")
            return True

        video = info.get("video") or {}
        playing = bool(video.get("w"))
        self._log(f"page: title={info.get('title')!r} video={video or None} "
                  f"login_hint={info.get('login_hint')}")

        if playing:
            return True

        shot = os.path.join(self._diag_dir(), f"page-{time.strftime('%Y%m%d-%H%M%S')}.png")
        with contextlib.suppress(Exception), open(shot, "wb") as f:
            f.write(self.cdp.screenshot_png())
        if info.get("login_hint"):
            self._log(f"看起来是登录/风控页（没有画面）。截图已存 {shot}；"
                      f"请先扫码登录，登录态会留在 profile 里，之后不用再登。")
            if self.strict_login:
                return False
        else:
            self._log(f"页面还没有视频画面（可能仍在加载）。截图已存 {shot}")
        return True

    def _teardown(self) -> None:
        if self.screencast and self.client is not None:
            with contextlib.suppress(Exception):
                self.cdp.stop_screencast()
        if self.client is not None:
            with contextlib.suppress(Exception):
                self.cdp.close()
            self.client = None
        if self.backend is not None:
            with contextlib.suppress(Exception):
                self.backend.close()
            self.backend = None

    def close(self) -> None:
        """关闭 CDP 连接，并回收本进程拉起的浏览器（外部端点不动）。"""
        if self._closed:
            return
        self._closed = True
        self._teardown()
        self._log("closed")


if __name__ == "__main__":  # 手动连一次，方便排查
    out = sys.argv[1] if len(sys.argv) > 1 else "."
    c = WebController(log_path=os.path.join(out, "web-controller.log"))
    ok = c.connect()
    print("connect =", ok, "| uuid =", c.request_uuid())
    if ok:
        img = c.screencap()
        print("screencap =", img.shape, img.dtype)
    c.close()
