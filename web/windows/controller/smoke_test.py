"""冒烟测试：不依赖 Maa 任务，直接验证"控制器 → 后端 → CDP"这条链路。

默认会自己拉起无头浏览器（不需要事先开任何东西）：

    uv run --with maafw --with websocket-client --with pillow python smoke_test.py

连外部端点（比如你自己开的浏览器）：

    MAAEND_WEB_CDP_ENDPOINT=127.0.0.1:9338 python smoke_test.py

测"无头 ↔ 可交互"的热切换（无头跑批，需要人时切出来操作，用完切回去）：

    python smoke_test.py --switch
"""

from __future__ import annotations

import argparse
import os
import sys
import time

import numpy as np
from PIL import Image

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from web_controller import WebController


def save_bgr(arr: np.ndarray, path: str) -> None:
    Image.fromarray(arr[:, :, ::-1]).save(path)


def frame_diff(a: np.ndarray, b: np.ndarray) -> dict:
    if a.shape != b.shape:
        return {"error": f"shape {a.shape} != {b.shape}"}
    ga = a.mean(axis=2)
    gb = b.mean(axis=2)
    d = np.abs(ga - gb)
    return {"mean_abs": round(float(d.mean()), 2),
            "changed_pct": round(float((d > 25).mean() * 100), 1)}


# 用来验证"切换后端不丢登录态"的标记（写进 localStorage，随 profile 落盘）
MARKER_JS_SET = "localStorage.setItem('maaend_switch_probe', '%s')"
MARKER_JS_GET = "localStorage.getItem('maaend_switch_probe')"


def run_switch(ctrl: WebController, out: str) -> list[tuple[str, bool, str]]:
    """无头 → offscreen（人在场操作）→ 无头。每一步都验：截图、可见性、uuid、登录态。"""
    res: list[tuple[str, bool, str]] = []
    token = f"t{int(time.time())}"
    ctrl.cdp.eval(MARKER_JS_SET % token)

    def state(label: str, expect_visible_page: bool) -> None:
        assert ctrl.backend is not None
        t0 = time.time()
        img = ctrl.screencap()
        dt = (time.time() - t0) * 1000
        vis = ctrl.cdp.eval("[document.visibilityState, localStorage.getItem('maaend_switch_probe')]")
        save_bgr(img, os.path.join(out, f"switch-{label}.png"))
        ok = (img.shape == (720, 1280, 3) and img.dtype == np.uint8
              and vis[0] == "visible" and vis[1] == token)
        res.append((label, ok, f"mode={ctrl.backend.mode} uuid={ctrl.request_uuid()} "
                               f"visibilityState={vis[0]} marker={vis[1]} screencap={dt:.0f}ms "
                               f"shape={img.shape}"))
        # 无头模式下不该有窗口句柄；有窗口的两种模式应该有
        want_hwnd = ctrl.backend.mode in ("offscreen", "webview2")
        res.append((f"{label}:窗口", bool(ctrl.backend.hwnd) == want_hwnd,
                    f"hwnd={ctrl.backend.hwnd}（期望 {'有' if want_hwnd else '无'}）"))

    state("1-headless", True)
    t0 = time.time()
    ok = ctrl.promote()
    res.append(("2-promote", ok, f"headless -> offscreen 耗时 {(time.time() - t0) * 1000:.0f} ms，"
                                 f"uuid={ctrl.request_uuid()}"))
    state("2-offscren", True)
    if ctrl.backend is not None:
        win = ctrl.cdp("Browser.getWindowForTarget")
        b = win["bounds"]
        res.append(("2-窗口在屏幕内", b["left"] > -10000 and b["top"] > -10000, str(b)))
    t0 = time.time()
    ok = ctrl.demote()
    res.append(("3-demote", ok, f"offscreen -> headless 耗时 {(time.time() - t0) * 1000:.0f} ms，"
                                f"uuid={ctrl.request_uuid()}"))
    state("3-headless", True)
    ctrl.cdp.eval("localStorage.removeItem('maaend_switch_probe')")
    return res


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=None, help="固定调试端口；默认随机")
    ap.add_argument("--url", default=None, help="起始地址；默认云·终末地网页端")
    ap.add_argument("--page-match", default=None)
    ap.add_argument("--out", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "shots"))
    ap.add_argument("--touch-mode", action="store_true", help="尝试点「触屏」按钮")
    ap.add_argument("--switch", action="store_true",
                    help="测热切换：headless → offscreen（人在场）→ headless")
    a = ap.parse_args()
    os.makedirs(a.out, exist_ok=True)

    ctrl = WebController(port=a.port, url=a.url, page_match=a.page_match,
                         switch_touch_mode=a.touch_mode)
    try:
        if not ctrl.connect():
            print("connect 失败（看上面的日志：后端选择 / 浏览器路径 / 登录提示都会写出来）")
            return 2
        assert ctrl.backend is not None

        print(f"backend = {ctrl.backend.describe()}")
        print(f"uuid    = {ctrl.request_uuid()}")
        print(f"mode    = {ctrl.backend.mode}")
        print(f"target  = {ctrl.cdp.target_url}")
        print(f"viewport = {ctrl.cdp.eval('[innerWidth, innerHeight, devicePixelRatio]')}")
        print(f"page     = {ctrl.cdp.eval('[document.title, document.visibilityState]')}")

        if a.switch:
            print("\n=== 热切换：headless → offscreen → headless")
            rows = run_switch(ctrl, a.out)
            for label, ok, detail in rows:
                print(f"{'PASS' if ok else 'FAIL'}  {label:<20} {detail}")
            bad = [r for r in rows if not r[1]]
            print(f"\n切换用例 {len(rows) - len(bad)}/{len(rows)} 通过")
            return 0 if not bad else 1

        t0 = time.time()
        img = ctrl.screencap()
        dt = (time.time() - t0) * 1000
        print(f"screencap -> shape={img.shape} dtype={img.dtype} 耗时 {dt:.0f} ms")
        save_bgr(img, os.path.join(a.out, "01-baseline.png"))

        h, w = img.shape[:2]
        cx, cy = w // 2, h // 2

        print("=== 点击中心")
        ctrl.click(cx, cy)
        time.sleep(0.6)
        img2 = ctrl.screencap()
        save_bgr(img2, os.path.join(a.out, "02-after-click.png"))
        print("click diff:", frame_diff(img, img2))

        print("=== 触摸拖动（contact=1，模拟视角）")
        ctrl.cdp.swipe(cx + 160, cy, cx - 160, cy, duration_ms=250, contact=1)
        time.sleep(0.6)
        img3 = ctrl.screencap()
        save_bgr(img3, os.path.join(a.out, "03-after-drag.png"))
        print("drag diff:", frame_diff(img2, img3))

        print("=== 连续 5 帧间隔（判断帧率）")
        stamps = []
        for _ in range(5):
            ctrl.screencap()
            stamps.append(time.time())
        gaps = [(stamps[i + 1] - stamps[i]) * 1000 for i in range(4)]
        print("每帧耗时 ms:", [round(g) for g in gaps], "≈", round(1000 / (sum(gaps) / len(gaps))), "fps")

        print(f"\n截图已存: {a.out}")
        return 0
    finally:
        ctrl.close()   # 自己拉起的浏览器会被回收；外部端点不会被动
        print(f"closed; 后端={'已回收' if ctrl.backend is None else '为外部端点，未关闭'}")


if __name__ == "__main__":
    sys.exit(main())
