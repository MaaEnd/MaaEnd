#!/usr/bin/env python3
"""改用「扩充官服路径」而不是新增资源条目。

实测：往 interface.json 的 resource 里新增一个条目（即使补了 label），MXU 前端会崩
（加载失败 / Object.entries requires that input parameter not be null or undefined）；
只加 task import 则正常。所以把三层网页端覆盖直接挂到「官服」的 path 上，
资源名保持唯一，MXU 侧不需要任何新条目。
"""
import json
import shutil

IFACE = "/root/maaend/interface.json"
CFG = "/root/maaend/config/mxu-MaaEnd.json"
PATHS = ["./resource", "./web/resource_web",
         "./web/linux/resource_web_linux", "./web/resource_web_homesrv"]

shutil.copy2(IFACE, IFACE + ".bak-pathmerge")
d = json.load(open(IFACE, encoding="utf-8-sig"))
d["resource"] = [r for r in d["resource"] if r.get("name") != "官服-网页端"]
for r in d["resource"]:
    if r.get("name") == "官服":
        r["path"] = PATHS
json.dump(d, open(IFACE, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("resource:", [(r["name"], len(r["path"])) for r in d["resource"]])

shutil.copy2(CFG, CFG + ".bak-pathmerge")
c = json.load(open(CFG, encoding="utf-8"))
for it in c["instances"]:
    if it.get("resourceName") in ("官服-网页端", ""):
        it["resourceName"] = "官服"
json.dump(c, open(CFG, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("resourceName:", sorted({it.get("resourceName") for it in c["instances"]}))
