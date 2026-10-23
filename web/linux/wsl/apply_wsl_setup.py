#!/usr/bin/env python3
"""一键把 WSL 上的网页端装配恢复到位（幂等，可反复跑）。

背景：MXU 自带更新（会把 base 换到新版本）会重写 interface.json、resource/ ——
我们的四层资源路径与 task import 全部被清掉，网页端节点随之"消失"。
所以每次 MXU 更新后、每次测试前，都跑一遍这个脚本。

做四件事：
1) interface.json：resource 里「官服」的 path 扩成 4 层；import 追加 web/tasks/WebStartGame.json
2) config：网页端实例 savedDevice.wlrSocketPath=wayland-1、resourceName=官服；
   「快速日常」恢复成原 9 项、全部 enabled、enabledByController.Linux-Wlroots=true
3) locales：补 task.WebStartGame.* / resource.CNWeb.label 文案
4) 打印自检结果
"""
import json
import os
import shutil

ROOT = "/root/maaend"
IFACE = os.path.join(ROOT, "interface.json")
CFG = os.path.join(ROOT, "config", "mxu-MaaEnd.json")
LOC = os.path.join(ROOT, "locales", "interface")
PATHS = ["./resource", "./web/resource_web",
         "./web/linux/resource_web_linux", "./web/resource_web_homesrv"]
IMPORT_ITEMS = ["web/tasks/WebStartGame.json"]
IMPORT_ITEM = IMPORT_ITEMS[0]
WEB_ON = {"hmuddz8", "hyasuml"}
DAILY9 = ["WebStartGame", "VisitFriends", "DijiangRewards", "CreditShoppingN2",
          "AutoStockpile", "AutoStockStaple", "SellProduct", "DailyRewards", "DeliveryJobs"]
CTRL = ["Win32-Front", "Linux-Wlroots", "Linux-ScreenCast", "Linux-Gamescope"]

# 0) agent/maafw 符号链接：MXU 更新/解包会把它冲掉，go-service 就加载不到 libMaaFramework
AGENT_LINK = os.path.join(ROOT, "agent", "maafw")
try:
    if os.path.islink(AGENT_LINK) or not os.path.exists(AGENT_LINK):
        os.remove(AGENT_LINK) if os.path.lexists(AGENT_LINK) else None
        os.symlink(os.path.join(ROOT, "maafw"), AGENT_LINK)
except OSError as e:
    print("(symlink 修复失败: %s)" % e)

# 1) interface.json
d = json.load(open(IFACE, encoding="utf-8-sig"))
shutil.copy2(IFACE, IFACE + ".bak-apply")
for r in d["resource"]:
    if r.get("name") == "官服":
        r["path"] = PATHS
d["resource"] = [r for r in d["resource"] if r.get("name") != "官服-网页端"]
imp = d.setdefault("import", [])
# 清掉历史遗留的 pretask 路径（已改为普通任务）
imp = [x for x in imp if "pretasks/" + "WebLaunchBrowser" not in x]
for _it in IMPORT_ITEMS:
    if _it not in imp:
        imp.append(_it)
json.dump(d, open(IFACE, "w", encoding="utf-8"), ensure_ascii=False, indent=2)

# 2) config
c = json.load(open(CFG, encoding="utf-8"))
shutil.copy2(CFG, CFG + ".bak-apply")
for it in c["instances"]:
    if it["id"] not in WEB_ON:
        continue
    it["resourceName"] = "官服"
    sd = it.setdefault("savedDevice", {})
    if isinstance(sd, dict):
        sd["wlrSocketPath"] = "wayland-1"
    it["controllerName"] = "Linux-Wlroots"
    # 已有任务：打开 Linux 控制器开关
    for t in it.get("tasks") or []:
        ec = dict(t.get("enabledByController") or {})
        ec["Linux-Wlroots"] = True
        t["enabledByController"] = ec
        t["enabled"] = True
    if it["id"] == "hmuddz8":
        by_name = {t["taskName"]: t for t in (it.get("tasks") or [])}
        tasks = []
        for i, name in enumerate(DAILY9):
            t = by_name.get(name) or {"id": "rest%02d" % i, "taskName": name,
                                      "enabled": True, "optionValues": {}}
            t["enabled"] = True
            ec = dict(t.get("enabledByController") or {})
            for k in CTRL:
                ec.setdefault(k, True)
            ec["Linux-Wlroots"] = True
            t["enabledByController"] = ec
            t.setdefault("optionValues", {})
            tasks.append(t)
        # 保留用户在预设里额外加的任务（不在 DAILY9 里的排在最后）
        for t in it.get("tasks") or []:
            if t["taskName"] not in DAILY9:
                tasks.append(t)
        it["tasks"] = tasks
json.dump(c, open(CFG, "w", encoding="utf-8"), ensure_ascii=False, indent=2)

# 3) locales
TEXTS = {
    "zh_cn": ("▶️开始游戏（云·终末地网页版）",
              "在云·终末地页面点「开始游戏」→ 标题页「点击任意位置继续」，直到进入大世界/菜单；已在游戏内会自动跳过。"),
    "zh_tw": ("▶️開始遊戲（雲·終末地網頁版）",
              "在雲·終末地頁面點「開始遊戲」→ 標題頁「點擊任意位置繼續」，直到進入大世界/選單；已在遊戲內會自動跳過。"),
    "en_us": ("▶️ Start Game (Cloud Endfield Web)",
              "Click Start Game on the cloud page, then click anywhere on the title screen, until in world/menu."),
    "ja_jp": ("▶️ゲーム開始（クラウド・エンドフィールド Web）",
              "クラウドページで「ゲーム開始」→タイトル画面をクリックしてワールド／メニューへ。"),
    "ko_kr": ("▶️게임 시작 (클라우드 엔드필드 웹)",
              "클라우드 페이지에서 게임 시작 → 타이틀 화면 클릭으로 월드/메뉴 진입."),
}
for lang, (lb, ds) in TEXTS.items():
    p = os.path.join(LOC, lang + ".json")
    if not os.path.exists(p):
        continue
    j = json.load(open(p, encoding="utf-8"))
    j["task.WebStartGame.label"] = lb
    j["task.WebStartGame.description"] = ds
    j["task.WebLaunchBrowser.label"] = "▶️启动网页端（sway + Chrome）"
    j["task.WebLaunchBrowser.description"] = "起 WSL 里的合成器与 Chrome，并等页面就绪；已在跑会直接跳过。"
    json.dump(j, open(p, "w", encoding="utf-8"), ensure_ascii=False, indent=2)

# 4) 自检
d = json.load(open(IFACE, encoding="utf-8"))
print("interface version:", d.get("version"))
print("官服 path 层数:", [len(r["path"]) for r in d["resource"] if r["name"] == "官服"])
print("import 含 web 任务:", [x for x in IMPORT_ITEMS if x in d["import"]])
c = json.load(open(CFG, encoding="utf-8"))
it = next(x for x in c["instances"] if x["id"] == "hmuddz8")
print("快速日常:", [(t["taskName"], t["enabled"]) for t in it["tasks"]])
print("socket:", it["savedDevice"].get("wlrSocketPath"), "| resource:", it["resourceName"])
