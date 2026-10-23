#!/usr/bin/env python3
"""照安卓云的做法：web 层只留「点开始游戏」+ 云 UI 覆盖，进入游戏交给 base 的 __ScenePrivateLogin 链。

依据 resource_cloud_adb 的设计：云专有节点只有 2 个
  __ScenePrivateCloudBackPill      面板右上角「← 返回」胶囊
  __ScenePrivateCloudClickToContinue 底部「点击屏幕继续」
游戏的登录/继续/月卡/维护/成功判定全部复用平台链（安卓=ADB 链，我们=base 的 __ScenePrivateLogin*）。
理由：点完「开始游戏」之后，云串流里跑的是**真正的游戏客户端**，UI 与 PC 端一致，
所以 base 的 login 链（LoginExit 模板 / Continue / ContinueDisappeared / MonthlyPass / Success）本该直接可用。

本脚本把 web 链改成：
  WebStartGameEntry → WebLauncherStartButton → __ScenePrivateLogin（base）→ … → __ScenePrivateLoginSuccess
保留 WebSessionBusy（"上次数据正在处理"）与 WebQueueDetected（排队）作为网页端特有前置。
"""
import json
import sys

P = sys.argv[1] if len(sys.argv) > 1 else "/root/maaend/web/resource_web/pipeline/nodes.json"
d = json.load(open(P, encoding="utf-8"))

# 入口：先处理网页端特有两件事，再点开始游戏，再交给 base 登录链
d["WebStartGameEntry"]["next"] = [
    "WebSessionBusy",
    "WebMonthlyCard",
    "WebAlreadyInGame",
    "WebLauncherStartButton",
    "__ScenePrivateLogin",
]

# 点完开始游戏 → 交给 base 的登录链（它自带 Continue / ContinueDisappeared / 月卡 / 维护 / Success）
d["WebLauncherStartButton"]["next"] = [
    "__ScenePrivateLogin",
    "WebSessionBusy",
    "WebMonthlyCard",
    "WebQueueDetected",
]

# 排队中：继续等，同时仍可命中登录链
d["WebQueueDetected"]["next"] = [
    "__ScenePrivateLogin",
    "WebSessionBusy",
    "WebMonthlyCard",
    "WebQueueDetected",
]

# 领奖弹窗关掉后回入口（避免断链）
d["WebCloseRewards"]["next"] = ["WebStartGameEntry"]

json.dump(d, open(P, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("已改：")
for k in ["WebStartGameEntry", "WebLauncherStartButton", "WebQueueDetected", "WebCloseRewards"]:
    print("  %-26s next=%s" % (k, d[k].get("next")))
print("节点数:", len(d))
