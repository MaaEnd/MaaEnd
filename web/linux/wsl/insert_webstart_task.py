#!/usr/bin/env python3
"""把 WebStartGame 插到网页端预设第 1 位（id 用与 MXU 同风格 7 位随机串）。

上一次用 "webstartgame" 当 id，MXU 启动后把整条丢掉了；这次用同风格 id 再试。
"""
import json
import shutil

CFG = "/root/maaend/config/mxu-MaaEnd.json"
WEB_ON = {"hmuddz8", "hyasuml"}
CTRL = ["Linux-Wlroots", "Linux-ScreenCast", "Linux-Gamescope"]

shutil.copy2(CFG, CFG + ".bak-insert2")
c = json.load(open(CFG, encoding="utf-8"))
for it in c["instances"]:
    if it["id"] not in WEB_ON:
        continue
    tasks = [t for t in (it.get("tasks") or []) if t.get("taskName") != "WebStartGame"]
    tasks.insert(0, {
        "id": "webst01",
        "taskName": "WebStartGame",
        "enabled": True,
        "enabledByController": dict.fromkeys(CTRL, True),
    })
    it["tasks"] = tasks
    print("%-9s 任务数=%d 首项=%s id=%s" % (it["id"], len(tasks), tasks[0]["taskName"], tasks[0]["id"]))
json.dump(c, open(CFG, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
