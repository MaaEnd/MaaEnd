"""把云游戏页面重置回启动页（强制，绕开 beforeunload）。

背景：
    云游戏页面注册了 beforeunload，`Page.navigate` / 关标签都会触发 Chrome 原生
    「Leave site? / 离开此网站？」确认框。那是**浏览器 UI**，不是网页 DOM：
    - 用 OCR/TemplateMatch 识别不到，游戏内所有识别随之失效；
    - `Page.handleJavaScriptDialog` 对它无效（实测点了仍在）。

做法：
    用 CDP 的 HTTP 端点 /json/new 先开一个新标签到目标 URL，再用 /json/close 关掉旧标签。
    两条都是强制操作，不经过渲染进程的 beforeunload，所以不会弹框。

用法: python3 reset_page.py [--port 9334] [--url https://...] [--wait 12]
"""

from __future__ import annotations

import argparse
import json
import time
import urllib.parse
import urllib.request


def _get(url, method="GET", timeout=10):
    req = urllib.request.Request(url, method=method)
    return urllib.request.urlopen(req, timeout=timeout).read().decode("utf-8", "replace")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=9334)
    ap.add_argument("--url", default="https://endfield.hypergryph.com/cloud")
    ap.add_argument("--wait", type=float, default=14.0)
    a = ap.parse_args()

    base = "http://127.0.0.1:%d" % a.port
    try:
        ts = json.loads(_get(base + "/json/list", timeout=6))
    except Exception as e:
        print("[X] CDP 不可达: %s" % str(e)[:60])
        return 2

    pages = [t for t in ts if t.get("type") == "page"]
    old_id = pages[0]["id"] if pages else None
    old_url = pages[0].get("url", "") if pages else ""

    # 1) 新开一个标签直接到目标页
    new = None
    for method in ("PUT", "GET"):
        try:
            new = json.loads(_get(base + "/json/new?" + urllib.parse.quote(a.url, safe=":/.?=&%"), method=method))
            break
        except Exception:
            continue
    if not new:
        print("[X] 新建标签失败")
        return 3
    print("  新标签已建: %s" % (new.get("id") or "")[:12])

    # 2) 强制关掉旧标签（不走 beforeunload，不会弹 Leave site?）
    if old_id and old_id != new.get("id"):
        try:
            _get(base + "/json/close/" + old_id)
            print("  旧标签已关闭（原 URL: %s）" % old_url[:60])
        except Exception as e:
            print("  (关旧标签失败: %s)" % str(e)[:50])

    time.sleep(a.wait)

    # 3) 复查：现在剩几个 page、URL 是什么
    try:
        ts2 = json.loads(_get(base + "/json/list", timeout=6))
        left = [(t.get("id", "")[:8], t.get("url", "")[:60]) for t in ts2 if t.get("type") == "page"]
        print("  当前标签 %d 个: %s" % (len(left), left))
    except Exception:
        pass
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
