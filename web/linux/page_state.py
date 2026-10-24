"""页面状态机：把「网页现在是什么状态」变成可查询的数据，再据此决策。

对应 MaaEnd#6746 的 ``main.cpp::prepare_host``：启动链不再散在 pipeline 节点里，
而是「求值 → 分支 → 动作」三步。Linux 侧用它替代 onboarding 那批节点。
"""

from __future__ import annotations

import json
import time
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any

import page_js

Log = Callable[[str], None]


def _noop(_: str) -> None:
    pass


@dataclass
class State:
    raw: dict[str, Any] = field(default_factory=dict)

    @property
    def page(self) -> str:
        return str(self.raw.get("page") or "unknown")

    @property
    def account(self) -> str:
        return str(self.raw.get("account") or "unknown")

    @property
    def video_ready(self) -> bool:
        return bool(self.raw.get("video_ready"))

    @property
    def ended(self) -> bool:
        return bool(self.raw.get("ended"))

    @property
    def startup_resource_failed(self) -> bool:
        return bool(self.raw.get("startup_resource_failed"))

    @property
    def document_epoch(self) -> float:
        try:
            return float(self.raw.get("document_epoch") or 0.0)
        except (TypeError, ValueError):
            return 0.0

    @property
    def viewport(self) -> tuple[int, int]:
        try:
            return int(self.raw.get("viewport_width") or 0), int(self.raw.get("viewport_height") or 0)
        except (TypeError, ValueError):
            return (0, 0)

    @property
    def video_size(self) -> tuple[int, int]:
        try:
            return int(self.raw.get("video_width") or 0), int(self.raw.get("video_height") or 0)
        except (TypeError, ValueError):
            return (0, 0)

    @property
    def start_target(self) -> tuple[float, float] | None:
        return _target(self.raw.get("start_target"))

    @property
    def busy_confirm_target(self) -> tuple[float, float] | None:
        return _target(self.raw.get("session_busy_confirm_target"))

    def __str__(self) -> str:  # 日志里一眼能看懂
        vw, vh = self.viewport
        bw, bh = self.video_size
        return ("page=%s account=%s video_ready=%s viewport=%dx%d video=%dx%d"
                % (self.page, self.account, self.video_ready, vw, vh, bw, bh))


def _target(v: Any) -> tuple[float, float] | None:
    if isinstance(v, dict) and "x" in v and "y" in v:
        try:
            return float(v["x"]), float(v["y"])
        except (TypeError, ValueError):
            return None
    return None


def inspect(sess) -> State:
    """求值一次页面状态。失败返回 page=unknown 的空状态，不抛。"""
    try:
        raw = sess.ev(page_js.PAGE_STATE_JS)
    except Exception:
        raw = None
    if not raw:
        return State({"page": "unknown"})
    try:
        d = json.loads(raw) if isinstance(raw, str) else raw
    except json.JSONDecodeError:
        return State({"page": "unknown"})
    return State(d if isinstance(d, dict) else {"page": "unknown"})


def click_css(sess, target: tuple[float, float]) -> bool:
    """按 CSS 坐标点一下（target 来自状态机，已经是 CSS px）。"""
    x, y = target
    try:
        sess.call("Input.dispatchMouseEvent", type="mousePressed", x=x, y=y,
                  button="left", buttons=1, clickCount=1, pointerType="mouse")
        time.sleep(0.03)
        sess.call("Input.dispatchMouseEvent", type="mouseReleased", x=x, y=y,
                  button="left", buttons=0, clickCount=1, pointerType="mouse")
        return True
    except Exception:
        return False


def prepare(
    sess,
    log: Log = _noop,
    timeout: float = 90.0,
    interval: float = 0.5,
    on_login: Callable[[], None] | None = None,
    on_startup_error: Callable[[State], None] | None = None,
) -> bool:
    """把页面推到「有解码帧」为止。返回 True 表示可以开始跑任务。

    分支与上游 ``prepare_host`` 一致：
      ended            → 会话已结束，失败
      startup_error    → 调 on_startup_error（默认什么都不做），等一轮再看
      login            → 用户操作不计时，deadline 顺延
      session_busy     → 点「知道了」
      landing          → 点「开始游戏」
      queue / starting → 等
      stream           → video_ready 就成功
    """
    deadline = time.time() + timeout
    login_announced = False
    last_page = ""
    epoch_seen = 0.0

    while True:
        st = inspect(sess)
        if st.page != last_page:
            log("页面状态: %s" % st)
            last_page = st.page

        # 登录是用户操作，不计时；其余状态一律受 deadline 约束（否则会在这里空转）。
        if st.page != "login" and st.account != "signed_out" and time.time() >= deadline:
            log("等待超时（%ds），最后状态：%s" % (int(timeout), st))
            return False

        if st.page == "ended":
            log("云会话已结束，需要在网页里重新开始游戏")
            return False

        if st.page == "startup_error":
            if st.document_epoch != epoch_seen:
                epoch_seen = st.document_epoch
                log("启动资源加载失败（SDK 未就绪），等待重试")
            if on_startup_error is not None:
                on_startup_error(st)
            time.sleep(interval)
            continue

        if st.page == "login" or st.account == "signed_out":
            if not login_announced:
                log("需要登录：请在网页里用任意方式登录，登录后会自动继续")
                login_announced = True
                if on_login is not None:
                    on_login()
            deadline = time.time() + timeout   # 用户操作不计时
            time.sleep(interval)
            continue

        if st.page == "session_busy":
            target = st.busy_confirm_target
            if target and click_css(sess, target):
                log("确认「上次游戏数据处理中」弹窗")
                time.sleep(interval)
            continue

        if st.page == "landing":
            target = st.start_target
            if target and click_css(sess, target):
                log("点击「开始游戏」@(%.0f, %.0f)" % target)
            else:
                log("看到「开始游戏」但点不到（被遮挡或禁用），等待")
            time.sleep(interval)
            continue

        if st.video_ready:
            log("已进入串流：%s" % st)
            return True

        if time.time() >= deadline:
            log("等待超时（%s），最后状态：%s" % ("%ds" % int(timeout), st))
            return False

        time.sleep(interval)
