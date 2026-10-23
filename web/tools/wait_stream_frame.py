#!/usr/bin/env python3
"""等 <video> 真正出帧（videoWidth>0 且 readyState>=2），再抓原始帧。

关键输出：云串流实际给的分辨率（videoWidth x videoHeight）。
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
ap.add_argument("--outdir", default="/mnt/d/maaend-dist")
a = ap.parse_args()

ts = json.loads(urllib.request.urlopen("http://127.0.0.1:%d/json/list" % a.port, timeout=6).read())
pg = next(t for t in ts if t.get("type") == "page")
ws = websocket.create_connection(pg["webSocketDebuggerUrl"], timeout=60, suppress_origin=True)
ws.settimeout(60)
_i = [0]


def call(method, **p):
    _i[0] += 1
    ws.send(json.dumps({"id": _i[0], "method": method, "params": p}))
    while True:
        r = json.loads(ws.recv())
        if r.get("id") == _i[0]:
            if "error" in r:
                raise RuntimeError(str(r["error"])[:200])
            return r.get("result", {})


def ev(expr, await_p=False):
    r = call("Runtime.evaluate", expression=expr, returnByValue=True, awaitPromise=await_p)
    res = r.get("result", {})
    if res.get("subtype") == "error":
        raise RuntimeError(str(res.get("description"))[:160])
    return res.get("value")


STATE = r"""
(() => {
  const v = document.querySelector('video');
  if (!v) return JSON.stringify({has:false});
  return JSON.stringify({
    has:true, w:v.videoWidth, h:v.videoHeight,
    cw:v.clientWidth, ch:v.clientHeight,
    rs:v.readyState, ns:v.networkState, paused:v.paused,
    t:Math.round(v.currentTime*100)/100,
    src:(v.currentSrc||'').slice(0,50),
    frames:(v.webkitDecodedFrameCount||null)
  });
})()
"""

print("=== 等出帧 ===")
t0 = time.time()
last = None
ok = False
while time.time() - t0 < a.timeout:
    try:
        s = json.loads(ev(STATE) or "{}")
        if not s.get("has"):
            line = "无 video 元素"
        else:
            line = "videoWxH=%dx%d css=%dx%d ready=%s paused=%s t=%s frames=%s" % (
                s["w"], s["h"], s["cw"], s["ch"], s["rs"], s["paused"], s["t"], s.get("frames"))
        if line != last:
            print("[%3ds] %s" % (int(time.time() - t0), line), flush=True)
            last = line
        if s.get("has") and s.get("w", 0) > 0 and s.get("rs", 0) >= 2:
            ok = True
            break
    except Exception as e:
        print("  (失败 %s)" % str(e)[:60], flush=True)
    time.sleep(2)

if not ok:
    print("超时：video 始终没出帧")
    ws.close()
    raise SystemExit(2)

time.sleep(2)
JS_GRAB = r"""
(() => {
  const v = document.querySelector('video');
  const w = v.videoWidth, h = v.videoHeight;
  const cv = document.createElement('canvas');
  cv.width = w; cv.height = h;
  cv.getContext('2d').drawImage(v, 0, 0, w, h);
  const s = cv.toDataURL('image/png');
  return JSON.stringify({w:w, h:h, len:s.length, data:s});
})()
"""
print("\n=== 抓原始帧 ===")
try:
    g = json.loads(ev(JS_GRAB))
    b64 = g["data"].split(",", 1)[1]
    raw = base64.b64decode(b64 + "=" * (-len(b64) % 4))
    p = a.outdir + "/wsl_raw.png"
    open(p, "wb").write(raw)
    print(" ✅ 原始帧 %s  %dx%d  %d 字节" % (p, g["w"], g["h"], len(raw)))
except Exception as e:
    print(" 原始帧失败:", str(e)[:120])

try:
    sh = call("Page.captureScreenshot", format="png")
    b64 = sh["data"]
    raw = base64.b64decode(b64 + "=" * (-len(b64) % 4))
    p2 = a.outdir + "/wsl_shot.png"
    open(p2, "wb").write(raw)
    print(" ✅ 页面截图 %s  %d 字节" % (p2, len(raw)))
except Exception as e:
    print(" 截图失败:", str(e)[:120])

try:
    s = json.loads(ev(STATE) or "{}")
    print("\n会话: %dx%d  css %dx%d" % (s["w"], s["h"], s["cw"], s["ch"]))
except Exception:
    pass
ws.close()
