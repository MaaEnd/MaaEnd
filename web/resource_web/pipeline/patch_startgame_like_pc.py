#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""照抄上游 PC 端「点完开始游戏之后」的链路，补进网页端覆盖层。

上游 PC 的关键结构（resource/pipeline/nodes.json）：
  __ScenePrivateLogin                      hub，max_hit 500，next 里带 [JumpBack] 弹窗
  __ScenePrivateLoginContinue              OCR「点击任意位置继续」→ Click，max_hit 10
  __ScenePrivateLoginContinueDisappeared   **inverse:true**，等这段文字消失
                                           next: [JumpBack]SceneWaitLoadingExit,
                                                 [JumpBack]月卡..., __ScenePrivateLoginSuccess,
                                                 [JumpBack]关闭弹窗
  __ScenePrivateLoginSuccess               And(InWorld) —— 成功终点

网页端照这个结构补：
  新增 WebTitleContinueDisappeared（inverse OCR，等标题文字消失）
  WebTitleContinue.next 增加它；WebLauncherStartButton 也指向它
  WebCloseRewards 的断链修到回 Entry
  WebLauncherStartButton 对齐 PC 的 max_hit 10 / post_delay 2000
"""
import json
import sys

P = sys.argv[1] if len(sys.argv) > 1 else \
    "/root/maaend/web/resource_web/pipeline/nodes.json"

d = json.load(open(P, encoding="utf-8"))
CONTINUE_WORDS = ["点击任意位置继续", "按下任意位置繼續", "点击任意处继续",
                  "Click to continue", "クリックして続ける", "아무 곳이나 눌러 계속하기"]

# 1) 新增：等「点击任意位置继续」消失（抄 __ScenePrivateLoginContinueDisappeared）
d["WebTitleContinueDisappeared"] = {
    "desc": "【网页端 · 启动器】等标题页「点击任意位置继续」消失（抄 __ScenePrivateLoginContinueDisappeared：inverse OCR）",
    "inverse": True,
    "recognition": {"type": "OCR", "param": {"expected": CONTINUE_WORDS,
                                             "roi": [-250, 200, 250, 400]}},
    "action": "DoNothing",
    "focus": {"Node.Recognition.Succeeded": "标题页文字已消失，等待进入游戏…"},
    "next": [
        "[JumpBack]SceneWaitLoadingExit",
        "[JumpBack]WebMonthlyCard",
        "[JumpBack]WebCloseRewards",
        "WebAlreadyInGame",
        "[JumpBack]WebSessionBusy",
    ],
    "timeout": 60000,
    "rate_limit": 0,
}

# 2) WebTitleContinue：点完之后先等「文字消失」，再回自己
d["WebTitleContinue"]["next"] = [
    "WebSessionBusy",
    "WebMonthlyCard",
    "WebTitleContinueDisappeared",
    "WebTitleContinue",
]

# 3) WebLauncherStartButton：对齐 PC 的 GameLauncherClickStartGame（max_hit 10 / post_delay 2000）
sb = d["WebLauncherStartButton"]
sb["max_hit"] = 10
sb["post_delay"] = 2000
sb["next"] = [
    "WebSessionBusy",
    "WebMonthlyCard",
    "WebTitleContinue",
    "WebTitleContinueDisappeared",
    "WebQueueDetected",
]

# 4) WebCloseRewards：修断链（原来 next=None，关掉弹窗后整条链就断了）
d["WebCloseRewards"]["next"] = ["WebStartGameEntry"]

# 5) WebWaitInGame：缩到 120s，超时回入口重来（pipeline 内自愈，不靠 python）
wg = d["WebWaitInGame"]
wg["timeout"] = 120000
wg["max_hit"] = 3
wg["next"] = ["WebStartGameEntry"]

# 6) WebAlreadyInGame：加文案
d["WebAlreadyInGame"]["focus"] = {"Node.Recognition.Succeeded": "已在大世界/菜单，跳过开始游戏"}

json.dump(d, open(P, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("节点数:", len(d))
for k in ["WebTitleContinue", "WebTitleContinueDisappeared",
          "WebLauncherStartButton", "WebCloseRewards", "WebWaitInGame"]:
    v = d[k]
    print("  %-30s next=%s max_hit=%s timeout=%s inverse=%s" %
          (k, v.get("next"), v.get("max_hit"), v.get("timeout"), v.get("inverse")))
