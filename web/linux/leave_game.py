"""收尾用：让页面离开云游戏，通知服务端正常结束会话，再关进程。

直接 pkill Chrome 会让云端认为会话没退干净，下一次点「开始游戏」会弹
「您上次游戏的数据正在火速处理中，请稍后重试」。
"""

from __future__ import annotations

import argparse
import contextlib
import json
import time
import urllib.request

import websocket


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=9334)
    ap.add_argument("--url", default="about:blank")
    ap.add_argument("--wait", type=float, default=4.0)
    a = ap.parse_args()

    try:
        ts = json.loads(urllib.request.urlopen("http://127.0.0.1:%d/json/list" % a.port, timeout=5).read().decode())
    except Exception as e:
        print("  (CDP 不可达，跳过优雅退出: %s)" % str(e)[:60])
        return 0

    pages = [t for t in ts if t.get("type") == "page"]
    if not pages:
        print("  (无页面，跳过)")
        return 0

    ws = websocket.create_connection(pages[0]["webSocketDebuggerUrl"], timeout=30, suppress_origin=True)
    i = 0

    def call(m, **p):
        nonlocal i
        i += 1
        ws.send(json.dumps({"id": i, "method": m, "params": p}))
        while True:
            r = json.loads(ws.recv())
            if r.get("id") == i:
                return r.get("result", {})

    def send(m, **p):
        """只发不收：串流页在拆除时常常不回响应，等就会超时。"""
        nonlocal i
        i += 1
        ws.send(json.dumps({"id": i, "method": m, "params": p}))

    try:
        send("Page.navigate", url=a.url)
        print("  已导航到 %s（不等响应），等服务端结束会话 %.0fs" % (a.url, a.wait))
        time.sleep(a.wait)
    except Exception as e:
        print("  (导航发送失败: %s) —— 仍等待 %.0fs 让服务端超时回收" % (str(e)[:60], a.wait))
        time.sleep(a.wait)
    finally:
        with contextlib.suppress(Exception):
            ws.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
