#!/usr/bin/env python3
"""把安卓云那套云 UI 节点接进我们的启动链。

resource_cloud_adb 里云专有的两个节点，我们已经在 web 层抄了一份：
  __ScenePrivateCloudClickToContinue  底部「点击屏幕继续」（云串流切场景时会挡住游戏）
  __ScenePrivateCloudBackPill         面板右上角「← 返回」胶囊
它们和「开始游戏」是同一类东西：云平台 UI，不属于游戏本体，必须由云层处理。

接法：
  WebStartGameEntry.next 与 WebLauncherStartButton.next 都加上 ClickToContinue，
  这样点完开始游戏、云串流缓冲时提示一出现就会被点掉，不会卡在那一屏。
"""
import json
import sys

P = sys.argv[1] if len(sys.argv) > 1 else "/root/maaend/web/resource_web/pipeline/nodes.json"
d = json.load(open(P, encoding="utf-8"))

for k in ("__ScenePrivateCloudClickToContinue", "__ScenePrivateCloudBackPill"):
    v = d.get(k)
    print("== %s\n   %s" % (k, json.dumps(v, ensure_ascii=False)[:300] if v else "<缺失>"))

def add_next(node, items):
    nxt = list(d[node].get("next") or [])
    for it in items:
        if it not in nxt:
            nxt.insert(0, it)
    d[node]["next"] = nxt

add_next("WebStartGameEntry", ["__ScenePrivateCloudClickToContinue"])
add_next("WebLauncherStartButton", ["__ScenePrivateCloudClickToContinue"])

json.dump(d, open(P, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
for k in ("WebStartGameEntry", "WebLauncherStartButton"):
    print("%-24s next=%s" % (k, d[k]["next"]))
