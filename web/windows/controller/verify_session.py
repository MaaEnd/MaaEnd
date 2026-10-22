"""真实会话验收：等你扫码登录后，自动量四件只有真机才能回答的事。

1. 画面是不是真的串流起来了（video/canvas 尺寸、帧数）
2. 「触屏」按钮在不在、能不能点亮（视角转向依赖它）
3. 触屏拖动是否产生**相对位移**（拖动前后帧差），也就是能不能转视角
4. 真实画面下的取帧延迟（连续 10 帧）与 `visibilityState`

用法（默认 webview2 模式，窗口会摆到屏幕上让你扫码）：

    uv run --with maafw --with websocket-client --with pillow --with numpy \
        python verify_session.py                # 等你登录，最多 10 分钟
    python verify_session.py --mode offscreen   # 换成 Edge 屏外窗口
    python verify_session.py --skip-login       # 已经登录过（profile 里有登录态）

登录完成判定：出现有尺寸的 `video`/`canvas`，或者页面不再显示「手机号/验证码」。
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

SHOTS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "shots")

PROBE_JS = """(() => {
  const txt = document.body.innerText || '';
  const v = document.querySelector('video');
  const cvs = [...document.querySelectorAll('canvas')]
      .filter(c => c.width > 320 && c.height > 180);
  const clickables = [...document.querySelectorAll('button,div,span,a')];
  const findText = (t) => clickables.some(e => (e.innerText || '').trim() === t);
  return {
    url: location.href,
    login: txt.includes('验证码') && txt.includes('手机号'),
    textLen: txt.length,
    video: v ? {rs: v.readyState, w: v.videoWidth, h: v.videoHeight,
                paused: v.paused, t: Math.round(v.currentTime * 10) / 10} : null,
    canvas: cvs.length,
    canvasSize: cvs.length ? [cvs[0].width, cvs[0].height] : null,
    touchButton: findText('触屏'),
    startButton: findText('开始游戏'),
  };
})()"""


def save_bgr(arr: np.ndarray, path: str) -> None:
    Image.fromarray(arr[:, :, ::-1]).save(path)


def frame_diff(a: np.ndarray, b: np.ndarray) -> dict:
    if a.shape != b.shape:
        return {"error": f"{a.shape} != {b.shape}"}
    ga, gb = a.mean(axis=2), b.mean(axis=2)
    d = np.abs(ga - gb)
    return {"mean_abs": round(float(d.mean()), 2),
            "changed_pct": round(float((d > 25).mean() * 100), 1)}


def is_streaming(st: dict) -> bool:
    v = st.get("video")
    if v and (v.get("w") or 0) > 0:
        return True
    cs = st.get("canvasSize")
    return bool(st.get("canvas")) and bool(cs) and cs[0] > 640 and cs[1] > 360


def wait_for_login(ctrl: WebController, timeout: float) -> tuple[bool, dict]:
    t0 = time.time()
    last = {}
    while time.time() - t0 < timeout:
        try:
            last = ctrl.cdp.eval(PROBE_JS) or {}
        except Exception as e:
            print(f"[{time.strftime('%H:%M:%S')}] 读取页面失败：{type(e).__name__}: {e}")
            time.sleep(3)
            continue
        print(f"[{time.strftime('%H:%M:%S')}] 登录页={last.get('login')} "
              f"video={last.get('video')} canvas={last.get('canvas')} "
              f"触屏={last.get('touchButton')} 开始={last.get('startButton')}", flush=True)
        if is_streaming(last):
            return True, last
        time.sleep(5)
    return False, last


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--mode", default="webview2", choices=["webview2", "offscreen", "headless"])
    ap.add_argument("--url", default=None)
    ap.add_argument("--at", default="150,90")
    ap.add_argument("--timeout", type=float, default=600, help="等你登录的最长秒数")
    ap.add_argument("--skip-login", action="store_true", help="已经登录过，跳过等待")
    a = ap.parse_args()
    os.environ["MAAEND_WEB_MODE"] = a.mode
    os.makedirs(SHOTS, exist_ok=True)
    left, top = (int(v) for v in a.at.split(","))

    ctrl = WebController(url=a.url)
    results: list[tuple[str, bool, str]] = []
    try:
        if not ctrl.connect():
            print("connect 失败，看上面日志")
            return 2
        if a.mode != "headless":
            ctrl.pop_to_screen(left, top)

        if a.skip_login:
            ok, st = True, ctrl.cdp.eval(PROBE_JS) or {}
        else:
            print(f"窗口已摆到屏幕 ({left},{top})：请扫码 / 登录（最多等 {a.timeout:.0f} 秒）", flush=True)
            ok, st = wait_for_login(ctrl, a.timeout)
        results.append(("进入串流画面", ok, str({k: st.get(k) for k in ("url", "video", "canvas", "canvasSize")})))

        img0 = ctrl.screencap()
        save_bgr(img0, os.path.join(SHOTS, "session-01-baseline.png"))

        # 触屏模式：视角转向依赖它
        if st.get("touchButton"):
            ok2 = bool(ctrl.switch_touch_mode_on())
            results.append(("「触屏」按钮可点亮", ok2, ""))
        else:
            results.append(("「触屏」按钮可点亮", False, "页面上没找到文字为「触屏」的元素"))

        # 触屏拖动 = 转视角？用 contact=1 拖一次，比较帧差
        h, w = img0.shape[:2]
        cx, cy = w // 2, h // 2
        ctrl.cdp.swipe(cx + 200, cy, cx - 200, cy, duration_ms=400, contact=1)
        time.sleep(0.8)
        img1 = ctrl.screencap()
        save_bgr(img1, os.path.join(SHOTS, "session-02-after-drag.png"))
        d = frame_diff(img0, img1)
        moved = d.get("changed_pct", 0) >= 8
        results.append(("触屏拖动产生相对位移（能转视角）", moved, str(d)))

        # 真实画面取帧延迟
        stamps = []
        for _ in range(10):
            ctrl.screencap()
            stamps.append(time.time())
        gaps = [(stamps[i + 1] - stamps[i]) * 1000 for i in range(len(stamps) - 1)]
        avg = sum(gaps) / len(gaps)
        results.append(("取帧延迟 < 30ms", avg < 30,
                        f"平均 {avg:.1f} ms（{[round(g) for g in gaps]}）"))

        vis = ctrl.cdp.eval("[document.visibilityState, document.hasFocus()]")
        results.append(("页面 visible", vis[0] == "visible", str(vis)))

        print("\n=== 真实会话验收 ===")
        for name, okk, detail in results:
            print(f"{'PASS' if okk else 'FAIL'}  {name}  {detail}")
        bad = [r for r in results if not r[1]]
        print(f"\n{len(results) - len(bad)}/{len(results)} 通过；截图在 {SHOTS}")
        return 0 if not bad else 1
    finally:
        ctrl.close()


if __name__ == "__main__":
    sys.exit(main())
