#!/usr/bin/env python3
"""直接抓云串流 <video> 的原始帧（绕开合成器缩放的母版来源）。

用法：
  python3 grab_video_frame.py [--out /mnt/d/maaend-dist/raw_frame.png] [--port 9334]

输出：
  - PNG 文件（原始分辨率，不含浏览器 UI）
  - 控制台打印 videoWidth/videoHeight、元素 CSS 尺寸、是否暂停、当前时间
"""
import argparse
import base64
import json
import urllib.request

import websocket

ap = argparse.ArgumentParser()
ap.add_argument("--port", type=int, default=9334)
ap.add_argument("--out", default="/mnt/d/maaend-dist/raw_frame.png")
a = ap.parse_args()

ts = json.loads(urllib.request.urlopen("http://127.0.0.1:%d/json/list" % a.port, timeout=6).read())
pages = [t for t in ts if t.get("type") == "page"]
if not pages:
    raise SystemExit("没有 page target")
pg = pages[0]
ws = websocket.create_connection(pg["webSocketDebuggerUrl"], timeout=30, suppress_origin=True)
i = 0


def call(method, **params):
    global i
    i += 1
    ws.send(json.dumps({"id": i, "method": method, "params": params}))
    while True:
        r = json.loads(ws.recv())
        if r.get("id") == i:
            if "error" in r:
                raise RuntimeError(r["error"])
            return r.get("result", {})


EXPR = r"""
(async () => {
  const v = document.querySelector('video');
  if (!v) {
    const c = document.querySelector('canvas');
    if (!c) return JSON.stringify({ok:false, why:'no video/canvas'});
    const url = c.toDataURL('image/png');
    return JSON.stringify({ok:true, kind:'canvas', w:c.width, h:c.height, data:url});
  }
  const w = v.videoWidth || v.clientWidth, h = v.videoHeight || v.clientHeight;
  const cv = document.createElement('canvas');
  cv.width = w; cv.height = h;
  cv.getContext('2d').drawImage(v, 0, 0, w, h);
  const url = cv.toDataURL('image/png');
  return JSON.stringify({
    ok:true, kind:'video', w:w, h:h,
    cssW:v.clientWidth, cssH:v.clientHeight,
    paused:v.paused, t:v.currentTime, muted:v.muted,
    data:url
  });
})()
"""

r = call("Runtime.evaluate", expression=EXPR, awaitPromise=True, returnByValue=True)
val = r.get("result", {}).get("value")
if not val:
    print("取值为空:", json.dumps(r)[:300])
    raise SystemExit(1)
d = json.loads(val)
if not d.get("ok"):
    print("失败:", d.get("why"))
    raise SystemExit(2)

b64 = d.pop("data", "")
raw = base64.b64decode(b64)
open(a.out, "wb").write(raw)
print("已保存:", a.out, len(raw), "字节")
for k in ("kind", "w", "h", "cssW", "cssH", "paused", "t", "muted"):
    if k in d:
        print("  %-6s %s" % (k, d[k]))
ws.close()
