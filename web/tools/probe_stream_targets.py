#!/usr/bin/env python3
"""全量排查：所有 CDP target 里的 video/canvas/iframe，并尝试抓原始帧。"""
import base64
import json
import urllib.request

import websocket

PORT = 9334
ts = json.loads(urllib.request.urlopen("http://127.0.0.1:%d/json/list" % PORT, timeout=6).read())
print("=== targets ===")
for t in ts:
    print("  %-10s %-60s %s" % (t.get("type"), (t.get("url") or "")[:60], (t.get("title") or "")[:24]))

EXPR = r"""
(() => {
  const out = {videos: [], canvases: [], iframes: []};
  document.querySelectorAll('video').forEach(v => out.videos.push({
    w: v.videoWidth, h: v.videoHeight, cssW: v.clientWidth, cssH: v.clientHeight,
    paused: v.paused, t: v.currentTime, src: (v.currentSrc || '').slice(0, 60)
  }));
  document.querySelectorAll('canvas').forEach(c => out.canvases.push({
    w: c.width, h: c.height, cssW: c.clientWidth, cssH: c.clientHeight
  }));
  document.querySelectorAll('iframe').forEach(f => out.iframes.push((f.src || '').slice(0, 80)));
  out.url = location.href;
  return JSON.stringify(out);
})()
"""

GRAB = r"""
(async () => {
  const v = document.querySelector('video');
  const c = v ? null : document.querySelector('canvas');
  const src = v || c;
  if (!src) return JSON.stringify({ok:false});
  const w = v ? (v.videoWidth || v.clientWidth) : c.width;
  const h = v ? (v.videoHeight || v.clientHeight) : c.height;
  const cv = document.createElement('canvas');
  cv.width = w; cv.height = h;
  if (v) cv.getContext('2d').drawImage(v, 0, 0, w, h);
  else cv.getContext('2d').drawImage(c, 0, 0, w, h);
  return JSON.stringify({ok:true, w:w, h:h, data:cv.toDataURL('image/png')});
})()
"""

saved = False
for t in ts:
    if t.get("type") != "page":
        continue
    url = t.get("url") or ""
    print("\n=== 检查:", url[:70])
    try:
        ws = websocket.create_connection(t["webSocketDebuggerUrl"], timeout=20, suppress_origin=True)
    except Exception as e:
        print("  连不上:", str(e)[:60])
        continue
    i = 0

    def call(method, _ws=ws, **p):
        global i
        i += 1
        _ws.send(json.dumps({"id": i, "method": method, "params": p}))
        while True:
            r = json.loads(_ws.recv())
            if r.get("id") == i:
                return r.get("result", {})

    try:
        r = call("Runtime.evaluate", expression=EXPR, returnByValue=True)
        info = json.loads(r.get("result", {}).get("value") or "{}")
        print("  video:", info.get("videos"))
        print("  canvas:", info.get("canvases"))
        print("  iframe:", info.get("iframes"))
        if (info.get("videos") or info.get("canvases")) and not saved:
            g = call("Runtime.evaluate", expression=GRAB, awaitPromise=True, returnByValue=True)
            d = json.loads(g.get("result", {}).get("value") or "{}")
            if d.get("ok"):
                raw = base64.b64decode(d["data"])
                open("/mnt/d/maaend-dist/raw_frame.png", "wb").write(raw)
                print("  ✅ 已保存 raw_frame.png  %dx%d  %d 字节" % (d["w"], d["h"], len(raw)))
                saved = True
    except Exception as e:
        print("  取值失败:", str(e)[:80])
    finally:
        ws.close()

if not saved:
    print("\n所有 target 里都没抓到 video/canvas")
