#!/usr/bin/env python3
"""修 MXU 在本机 WSL 上跑网页端的两处硬伤：

1) interface.json 里只有「官服」一个资源（只有 ./resource），
   网页端覆盖层（web/resource_web、linux/resource_web_linux、resource_web_homesrv）
   根本不在资源列表里 → MXU 加载不到 Web* 节点。
2) 每个实例的 savedDevice.wlrSocketPath 是 wayland-0（WSLg 的 Weston，没有
   wlr_screencopy）→ 自动连接必失败。应指向 sway 的 wayland-1。
"""
import json
import shutil

IFACE = "/root/maaend/interface.json"
CFG = "/root/maaend/config/mxu-MaaEnd.json"
RES_NAME = "官服-网页端"
PATHS = ["./resource", "./web/resource_web",
         "./web/linux/resource_web_linux", "./web/resource_web_homesrv"]

# --- 1) interface.json ---
shutil.copy2(IFACE, IFACE + ".bak-westart")
d = json.load(open(IFACE, encoding="utf-8-sig"))
res = d.setdefault("resource", [])
names = [r.get("name") for r in res]
if RES_NAME in names:
    res[names.index(RES_NAME)]["path"] = PATHS
else:
    res.append({"name": RES_NAME, "path": PATHS})
json.dump(d, open(IFACE, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("interface.json 资源:", [(r["name"], len(r["path"])) for r in res])

# --- 2) mxu-MaaEnd.json ---
shutil.copy2(CFG, CFG + ".bak-wlr")
c = json.load(open(CFG, encoding="utf-8"))
n_sock = n_res = 0
for it in c.get("instances", []):
    sd = it.get("savedDevice")
    if isinstance(sd, dict) and "wlrSocketPath" in sd and sd["wlrSocketPath"] != "wayland-1":
        sd["wlrSocketPath"] = "wayland-1"
        n_sock += 1
    if it.get("resourceName") != RES_NAME:
        it["resourceName"] = RES_NAME
        n_res += 1

json.dump(c, open(CFG, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("已改 socket 的实例: %d，已改 resourceName 的实例: %d" % (n_sock, n_res))
for it in c["instances"]:
    sd = it.get("savedDevice") or {}
    print("  %-9s %-22s socket=%-10s resource=%s" %
          (it["id"], it["name"], sd.get("wlrSocketPath", "-"), it.get("resourceName")))
