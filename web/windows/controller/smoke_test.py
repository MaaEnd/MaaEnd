"""冒烟测试：不依赖 Maa 任务，直接验证"控制器 → CDP → 宿主"这条链路。

    uv run --with maafw --with websocket-client --with pillow python smoke_test.py --port 9338
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


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=9338)
    ap.add_argument("--page-match", default=None)
    ap.add_argument("--out", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "shots"))
    ap.add_argument("--touch-mode", action="store_true", help="尝试点「触屏」按钮")
    a = ap.parse_args()
    os.makedirs(a.out, exist_ok=True)

    print(f"=== 目标 CDP {a.port}")
    ctrl = WebController(port=a.port, page_match=a.page_match, switch_touch_mode=a.touch_mode)
    if not ctrl.connect():
        print("connect 失败")
        return 2

    print(f"target = {ctrl.client.target_url}")
    video_js = (
        "(()=>{const v=document.getElementById('WelinkGameVideo');"
        "return v?{rs:v.readyState,w:v.videoWidth,h:v.videoHeight,paused:v.paused}:null})()"
    )
    print(f"video  = {ctrl.client.eval(video_js)}")
    print(f"viewport = {ctrl.client.eval('[innerWidth, innerHeight, devicePixelRatio]')}")

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
    ctrl.client.swipe(cx + 160, cy, cx - 160, cy, duration_ms=250, contact=1)
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


if __name__ == "__main__":
    sys.exit(main())
