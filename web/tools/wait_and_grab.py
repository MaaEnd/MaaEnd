#!/usr/bin/env python3
"""等用户点进会话，然后抓一张全分辨率 PNG（同时报告 video/canvas 情况）。

用法：python3 wait_and_grab.py [--timeout 240] [--out /mnt/d/maaend-dist/session_hires.png]
判据：页面里出现 video 或 canvas 即视为已进入串流会话；等到后再稳定 3 秒抓图。
"""
import argparse
import base64
import json
import time
import urllib.request

import websocket

ap = argparse.ArgumentParser()
ap.add_argument("--port", type=int, default=9334)
ap.add_argument("--timeout", type=int, default=240)
ap.add_argument("--out", default="/mnt/d/maaend-dist/session_hires.png")
a = ap.parse_args()

PROBE = r"""
(() => {
  const v = document.querySelectorAll('video');
  const c = document.querySelectorAll('canvas');
  const vv = v[0], cc = c[0];
  return JSON.stringify({
    url: location.href,
    nVideo: v.length, nCanvas: c.length,
    vw: vv ? (vv.videoWidth||0) : 0, vh: vv ? (vv.videoHeight||0) : 0,
    cw: cc ? cc.width : 0, ch: cc ? cc.height : 0
  });
})()
"""

t0 = time.time()
last = ""
while time.time() - t0 < a.timeout:
    try:
        ts = json.loads(urllib.request.urlopen("http://127.0.0.1:%d/json/list" % a.port, timeout=5).read())
        pg = next(t for t in ts if t.get("type") == "page")
        ws = websocket.create_connection(pg["webSocketDebuggerUrl"], timeout=15, suppress_origin=True)
        ws.send(json.dumps({"id": 1, "method": "Runtime.evaluate",
                            "params": {"expression": PROBE, "returnByValue": True}}))
        info = None
        while True:
            r = json.loads(ws.recv())
            if r.get("id") == 1:
                info = json.loads(r.get("result", {}).get("result", {}).get("value") or "{}")
                break
        if info:
            line = "video=%d canvas=%d videoWxH=%dx%d canvasWxH=%dx%d" % (
                info["nVideo"], info["nCanvas"], info["vw"], info["vh"], info["cw"], info["ch"])
            if line != last:
                print("[%3ds] %s" % (int(time.time() - t0), line), flush=True)
                last = line
            if info["nVideo"] or info["nCanvas"]:
                time.sleep(3)
                ws.send(json.dumps({"id": 2, "method": "Page.captureScreenshot",
                                    "params": {"format": "png", "captureBeyondViewport": False}}))
                while True:
                    r = json.loads(ws.recv())
                    if r.get("id") == 2:
                        data = r["result"]["data"]
                        break
                raw = base64.b64decode(data)
                open(a.out, "wb").write(raw)
                print("✅ 已保存 %s  %d 字节" % (a.out, len(raw)), flush=True)
                ws.close()
                raise SystemExit(0)
        ws.close()
    except SystemExit:
        raise
    except Exception as e:
        print("(轮询失败: %s)" % str(e)[:60], flush=True)
    time.sleep(3)

print("超时：%.0f 秒内没等到会话" % a.timeout, flush=True)
