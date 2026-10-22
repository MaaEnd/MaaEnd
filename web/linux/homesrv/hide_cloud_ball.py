#!/usr/bin/env python3
"""隐藏云端网页的悬浮球（白色云朵图标）——支持新标签页。

背景：球是网页端自己的 DOM 覆盖层（容器 `e4MbkW` / 球体 `hbU_7F`），浮在 canvas 上，
默认停在左上角，会盖住游戏标题区（「活动中心」的「活」字就是被它遮掉）。
关键事实：点「开始游戏」后云端会**新开一个标签页**，一次性注入只管原标签，所以需要 watch。

用法：
    python3 hide_cloud_ball.py            # 对所有 endfield 页面注入一次
    python3 hide_cloud_ball.py --check    # 只报告当前状态
    python3 hide_cloud_ball.py --watch    # 常驻：每 2s 检查，新标签页自动注入（日常跑期间用）
"""

import contextlib
import json
import sys
import time
import urllib.request

import websocket  # type: ignore

DST = "http://127.0.0.1:9334"

INIT_JS = r"""
(() => {
  const CSS = '[class*="hbU_7F"],[class*="e4MbkW"]{display:none!important;visibility:hidden!important}';
  window.__hermesInitRan = (window.__hermesInitRan || 0) + 1;

  function injectStyle() {
    if (document.getElementById('hermes-hide-ball')) return;
    const s = document.createElement('style');
    s.id = 'hermes-hide-ball';
    s.textContent = CSS;
    (document.head || document.documentElement).appendChild(s);
  }

  function byClass() {
    let n = 0;
    for (const sel of ['[class*="hbU_7F"]', '[class*="e4MbkW"]']) {
      for (const e of document.querySelectorAll(sel)) {
        e.style.setProperty('display', 'none', 'important');
        n++;
      }
    }
    return n;
  }

  function byGeometry() {
    let n = 0;
    for (const e of document.querySelectorAll('div')) {
      const st = getComputedStyle(e);
      if (st.position !== 'fixed' && st.position !== 'absolute') continue;
      const r = e.getBoundingClientRect();
      if (r.width >= 20 && r.width <= 90 && r.height >= 20 && r.height <= 90 &&
          r.left >= -60 && r.left <= 120 && r.top >= -5 && r.top <= 150) {
        e.style.setProperty('display', 'none', 'important');
        n++;
      }
    }
    return n;
  }

  function apply() {
    injectStyle();
    let n = byClass();
    if (n === 0) n = byGeometry();
    if (n) window.__hermesBallHidden = Math.max(window.__hermesBallHidden || 0, n);
    return n;
  }

  window.__hermesApply = apply;
  apply();
  document.addEventListener('DOMContentLoaded', apply);
  // class 是后加的（React 先插节点再设 className），必须盯属性变化 + 定时兜底
  try {
    new MutationObserver(apply).observe(document.documentElement,
      {childList: true, subtree: true, attributes: true, attributeFilter: ['class', 'style']});
  } catch (e) {}
  setInterval(apply, 1000);
})();
"""

CHECK_JS = r"""
(() => {
  const cls = document.querySelectorAll('[class*="hbU_7F"],[class*="e4MbkW"]');
  let visible = 0, sample = null;
  for (const e of cls) {
    const st = getComputedStyle(e);
    const r = e.getBoundingClientRect();
    if (st.display !== 'none' && st.visibility !== 'hidden' && r.width > 0) {
      visible++;
      if (!sample) sample = {cls: String(e.className).slice(0, 40), x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width)};
    }
  }
  return JSON.stringify({matched: cls.length, visible,
                         initRan: window.__hermesInitRan || 0,
                         hiddenCounter: window.__hermesBallHidden || 0,
                         styleTag: !!document.getElementById('hermes-hide-ball'),
                         sample});
})()
"""


def targets():
    try:
        with urllib.request.urlopen(DST + "/json", timeout=5) as r:
            return json.load(r)
    except Exception:
        return []


def endfield_pages():
    return [t for t in targets() if t.get("type") == "page" and "endfield" in (t.get("url") or "")]


class CDP:
    def __init__(self, url):
        self.ws = websocket.create_connection(url, timeout=20, suppress_origin=True)
        self.i = 0

    def call(self, method, **params):
        self.i += 1
        self.ws.send(json.dumps({"id": self.i, "method": method, "params": params}))
        while True:
            msg = json.loads(self.ws.recv())
            if msg.get("id") == self.i:
                return msg

    def js(self, expr):
        r = self.call("Runtime.evaluate", expression=expr, returnByValue=True, awaitPromise=True)
        return r.get("result", {}).get("result", {}).get("value")

    def close(self):
        with contextlib.suppress(Exception):
            self.ws.close()


def apply_all(registered):
    out = []
    for t in endfield_pages():
        tid = t.get("id")
        try:
            c = CDP(t["webSocketDebuggerUrl"])
            if tid not in registered:
                c.call("Page.addScriptToEvaluateOnNewDocument", source=INIT_JS)
                registered.add(tid)
            c.js(INIT_JS)
            out.append((tid, t.get("url", "")[:50], c.js(CHECK_JS)))
            c.close()
        except Exception as e:
            out.append((tid, t.get("url", "")[:50], "ERR:%s" % str(e)[:60]))
    return out


def main():
    watch = "--watch" in sys.argv
    check = "--check" in sys.argv
    registered = set()

    if check and not watch:
        for t in endfield_pages():
            try:
                c = CDP(t["webSocketDebuggerUrl"])
                print("%s %s" % (t.get("url", "")[:60], c.js(CHECK_JS)))
                c.close()
            except Exception as e:
                print("ERR", e)
        return 0

    if watch:
        while True:
            for tid, url, res in apply_all(registered):
                if isinstance(res, str) and res.startswith("ERR"):
                    print("注入失败", tid, url, res, flush=True)
            time.sleep(2)

    for tid, url, res in apply_all(registered):
        print("已注入", tid, url, res)
    return 0


if __name__ == "__main__":
    sys.exit(main())
