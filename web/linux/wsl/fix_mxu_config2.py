#!/usr/bin/env python3
"""只对网页端预设生效：resourceName=官服-网页端、savedDevice.wlrSocketPath=wayland-1。
其它实例从备份恢复原始 resourceName / savedDevice。
"""
import json

CFG = "/root/maaend/config/mxu-MaaEnd.json"
BAK = CFG + ".bak-wlr"
WEB_ON = {"hmuddz8", "hyasuml"}

cur = json.load(open(CFG, encoding="utf-8"))
old = {it["id"]: it for it in json.load(open(BAK, encoding="utf-8"))["instances"]}

for it in cur["instances"]:
    o = old.get(it["id"], {})
    if it["id"] in WEB_ON:
        it["resourceName"] = "官服-网页端"
        sd = it.setdefault("savedDevice", {})
        if isinstance(sd, dict):
            sd["wlrSocketPath"] = "wayland-1"
    else:
        if "resourceName" in o:
            it["resourceName"] = o["resourceName"]
        else:
            it.pop("resourceName", None)
        if "savedDevice" in o:
            it["savedDevice"] = o["savedDevice"]
        else:
            it.pop("savedDevice", None)

json.dump(cur, open(CFG, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
for it in cur["instances"]:
    sd = it.get("savedDevice") or {}
    print("  %-9s %-22s socket=%-10s resource=%-12s" %
          (it["id"], it["name"], sd.get("wlrSocketPath", "-"), it.get("resourceName")))
