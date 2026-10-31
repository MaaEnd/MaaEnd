#!/usr/bin/env python3
"""自动接受 Chrome 的 JS 原生对话框（beforeunload 的 "Leave site?" 等）。

云游戏页面注册了 beforeunload，脚本一旦触发导航/关闭，Chrome 会弹出
"Leave site? Changes you made may not be saved."，它是**浏览器原生 UI**，
模板/OCR 都碰不到，还会遮住标题页导致「点击任意位置继续」点不到
（2026-10-23 实测：链路已走到 __ScenePrivateLogin，画面被这个框压住）。

做法：CDP 里 Page.enable 后监听 Page.javascriptDialogOpening，立刻 handleJavaScriptDialog(accept=true)。
常驻进程，跟 Chrome 同生命周期（chrome_homesrv.sh 里 setsid 拉起）。
"""
import argparse
import json
import time
import urllib.request

import websocket


def dialogs(port):
    base = "http://127.0.0.1:%d" % port
    ts = json.loads(urllib.request.urlopen(base + "/json/list", timeout=6).read())
    return [t for t in ts if t.get("type") == "page"]


def attach(ws_url, port):
    ws = websocket.create_connection(ws_url, timeout=30, suppress_origin=True)
    ws.send(json.dumps({"id": 1, "method": "Page.enable"}))
    while True:
        raw = ws.recv()
        if not raw:
            break
        try:
            msg = json.loads(raw)
        except Exception:
            continue
        if msg.get("method") == "Page.javascriptDialogOpening":
            print("[i] 自动接受对话框: %s" % (msg.get("params", {}).get("message", "")[:60]), flush=True)
            ws.send(json.dumps({"id": 99, "method": "Page.handleJavaScriptDialog",
                                "params": {"accept": True}}))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=9334)
    a = ap.parse_args()
    print("[i] 等待 Chrome CDP :%d 出现…" % a.port, flush=True)
    for _ in range(120):
        try:
            pages = dialogs(a.port)
            if pages:
                break
        except Exception:
            pass
        time.sleep(1)
    else:
        print("[X] 等不到 CDP", flush=True)
        return 1
    print("[i] 已接上 CDP，开始监听对话框", flush=True)
    while True:
        try:
            for p in dialogs(a.port):
                attach(p["webSocketDebuggerUrl"], a.port)
        except Exception:
            time.sleep(3)


if __name__ == "__main__":
    raise SystemExit(main())
