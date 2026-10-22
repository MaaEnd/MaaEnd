"""截图吞吐基准：静态页 vs 动画页 vs screencast（默认自己拉起无头浏览器）。"""
from __future__ import annotations

import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import browser
from cdp_client import CDPClient

ANIM = (
    "data:text/html,<html><body style='margin:0;background:%23123'>"
    "<div style='width:200px;height:200px;background:%23e33;animation:spin 1s linear infinite'></div>"
    "<style>@keyframes spin{to{transform:rotate(360deg)}}</style></body></html>"
)


def bench_capture(c: CDPClient, n: int = 8) -> None:
    c.screenshot_png()  # 预热
    ts = []
    for _ in range(n):
        t0 = time.time()
        c.screenshot_png()
        ts.append((time.time() - t0) * 1000)
    print("captureScreenshot ms:", [round(t) for t in ts],
          "中位", round(sorted(ts)[len(ts) // 2]))


def bench_screencast(c: CDPClient, seconds: float = 3.0) -> None:
    c("Page.enable")
    c("Page.startScreencast", format="jpeg", quality=70, maxWidth=1280, maxHeight=720, everyNthFrame=1)
    ws = c.ws
    assert ws is not None
    t_end = time.time() + seconds
    frames = 0
    sizes = []
    ws.settimeout(0.5)
    while time.time() < t_end:
        try:
            msg = json.loads(ws.recv())
        except Exception:
            continue
        if msg.get("method") == "Page.screencastFrame":
            frames += 1
            data = msg["params"].get("data", "")
            sizes.append(len(data))
            c("Page.screencastFrameAck", sessionId=msg["params"]["sessionId"])
    c("Page.stopScreencast")
    print(f"screencast: {frames} 帧 / {seconds}s ≈ {frames / seconds:.1f} fps，"
          f"平均 {int(sum(sizes)/max(len(sizes),1)/1024)} KB/帧")


def main() -> None:
    port = int(sys.argv[1]) if len(sys.argv) > 1 else None
    backend = browser.ensure_backend(url="about:blank", port=port)
    try:
        c = CDPClient(port=backend.port, host=backend.host)
        c.connect()
        print("backend:", backend.describe())
        print("target:", c.target_url)

        print("\n=== A. 静态页")
        bench_capture(c)

        print("\n=== B. 动画页（rAF/CSS 持续重绘）")
        c("Page.navigate", url=ANIM)
        time.sleep(1.5)
        bench_capture(c)

        print("\n=== C. screencast（持续推帧）")
        bench_screencast(c)
    finally:
        backend.close()


if __name__ == "__main__":
    main()
