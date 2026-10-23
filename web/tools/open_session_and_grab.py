#!/usr/bin/env python3
"""在 WSL 的 Chrome 里点「开始游戏」，进入云串流后抓原始画面。

打印：页面文本摘要、与画质相关的 localStorage 项、video/canvas 尺寸、
      并保存 canvas 原始帧 + CDP 全屏截图。
"""
import argparse
import base64
import json
import time
import urllib.request

import websocket

ap = argparse.ArgumentParser()
ap.add_argument("--port", type=int, default=9334)
ap.add_argument("--timeout", type=int, default=200)
ap.add_argument("--outdir", default="/mnt/d/maaend-dist")
a = ap.parse_args()

ts = json.loads(urllib.request.urlopen("http://127.0.0.1:%d/json/list" % a.port, timeout=6).read())
pg = next(t for t in ts if t.get("type") == "page")
ws = websocket.create_connection(pg["webSocketDebuggerUrl"], timeout=40, suppress_origin=True)
ws.settimeout(40)
_id = [0]


def call(method, **p):
    _id[0] += 1
    ws.send(json.dumps({"id": _id[0], "method": method, "params": p}))
    while True:
        r = json.loads(ws.recv())
        if r.get("id") == _id[0]:
            if "error" in r:
                raise RuntimeError(str(r["error"])[:160])
            return r.get("result", {})


def ev(expr, await_p=False):
    r = call("Runtime.evaluate", expression=expr, returnByValue=True, awaitPromise=await_p)
    return r.get("result", {}).get("value")


INFO = r"""
(() => {
  const q = {};
  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i);
    if (/qual|res|bit|fps|stream|clarity|hd|sd/i.test(k)) q[k] = (localStorage.getItem(k) || '').slice(0, 120);
  }
  const v = document.querySelector('video'), c = document.querySelector('canvas');
  return JSON.stringify({
    url: location.href,
    text: (document.body.innerText || '').replace(/\s+/g, ' ').slice(0, 260),
    qualityKeys: q,
    video: v ? {w: v.videoWidth, h: v.videoHeight, cw: v.clientWidth, ch: v.clientHeight, paused: v.paused} : null,
    canvas: c ? {w: c.width, h: c.height, cw: c.clientWidth, ch: c.clientHeight} : null
  });
})()
"""

print("=== 进入前状态 ===")
info = json.loads(ev(INFO) or "{}")
print(" url:", info.get("url"))
print(" 文本:", info.get("text"))
print(" 画质相关 localStorage:", json.dumps(info.get("qualityKeys"), ensure_ascii=False))
print(" video:", info.get("video"), " canvas:", info.get("canvas"))

if not (info.get("video") or info.get("canvas")):
    clicked = ev(r"""
    (() => {
      const cands = [...document.querySelectorAll('button,div,span,a')]
        .filter(e => { const t = (e.innerText || '').trim(); return t === '开始游戏' || t === '进入游戏' || t === '立即游戏'; });
      if (!cands.length) return 'not-found';
      cands[0].click();
      return 'clicked:' + cands.length;
    })()
    """)
    print("\n点击开始游戏:", clicked)

print("\n=== 轮询会话 ===")
t0 = time.time()
last = None
hit = False
while time.time() - t0 < a.timeout:
    try:
        raw = ev(INFO)
        d = json.loads(raw or "{}")
        v, c = d.get("video"), d.get("canvas")
        line = "video=%s canvas=%s" % (
            "%dx%d(css %dx%d)" % (v["w"], v["h"], v["cw"], v["ch"]) if v else "无",
            "%dx%d(css %dx%d)" % (c["w"], c["h"], c["cw"], c["ch"]) if c else "无")
        if line != last:
            print("[%3ds] %s" % (int(time.time() - t0), line), flush=True)
            last = line
        if v or c:
            hit = True
            break
    except Exception as e:
        print("  (取值失败 %s)" % str(e)[:50], flush=True)
    time.sleep(3)

if not hit:
    print("超时，仍未出现 video/canvas")
    ws.close()
    raise SystemExit(2)

time.sleep(5)
print("\n=== 抓帧 ===")
GRAB = r"""
(async () => {
  const v = document.querySelector('video'), c = v ? null : document.querySelector('canvas');
  if (!v && !c) return JSON.stringify({ok:false});
  const w = v ? (v.videoWidth || v.clientWidth) : c.width;
  const h = v ? (v.videoHeight || v.clientHeight) : c.height;
  const cv = document.createElement('canvas');
  cv.width = w; cv.height = h;
  cv.getContext('2d').drawImage(v || c, 0, 0, w, h);
  return JSON.stringify({ok:true, w:w, h:h, data:cv.toDataURL('image/png')});
})()
"""
g = json.loads(ev(GRAB, await_p=True) or "{}")
if g.get("ok"):
    raw = base64.b64decode(g["data"])
    p = a.outdir + "/wsl_raw.png"
    open(p, "wb").write(raw)
    print(" 原始帧: %s  %dx%d  %d 字节" % (p, g["w"], g["h"], len(raw)))
else:
    print(" canvas 抓取失败（可能是 OffscreenCanvas / 跨域）")

sh = call("Page.captureScreenshot", format="png")
raw = base64.b64decode(sh["data"])
p2 = a.outdir + "/wsl_shot.png"
open(p2, "wb").write(raw)
print(" 页面截图: %s  %d 字节" % (p2, len(raw)))

print("\n=== 会话信息 ===")
info = json.loads(ev(INFO) or "{}")
print(" video:", info.get("video"), " canvas:", info.get("canvas"))
print(" 画质相关 localStorage:", json.dumps(info.get("qualityKeys"), ensure_ascii=False))
ws.close()
