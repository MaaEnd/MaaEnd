"""升级后自检：按 run_daily 的顺序加载四层资源，确认覆盖层与 base 关键节点都在。"""

import os
import sys

from maa.resource import Resource

DIST = os.environ.get("MAAEND_DIST", os.path.expanduser("~/maaend"))
WEB = os.environ.get("MAAEND_WEB", os.path.join(DIST, "web"))
DIRS = [
    os.path.join(DIST, "resource"),
    os.path.join(WEB, "resource_web"),
    os.path.join(WEB, "linux", "resource_web_linux"),
    os.path.join(WEB, "resource_web_homesrv"),
]
CHECK = [
    "WebStartGameEntry",
    "WebMonthlyCard",
    "WebCloseRewards",
    "WebSessionBusy",
    "WebAlreadyInGame",
    "WebLauncherStartButton",
    "InWorld",
    "InMenuList",
    "CheckMenuListColor",
    "__ScenePrivateEndVisit",
    "__ScenePrivateWorldEnterMenuList",
    "CloseRewardsButton",
    "CloseRewardsButtonText",
    "VisitFriendsQuickVisitNext",
    "AutoStockpileEnaureElasticClicked",
]

r = Resource()
for d in DIRS:
    ok = r.post_bundle(d).wait()
    print("load %-28s -> %s" % (d.rsplit("/", 1)[-1], ok))

missing = []
for n in CHECK:
    nd = r.get_node_data(n)
    ok = bool(nd)
    if not ok:
        missing.append(n)
    print("  %-40s %s" % (n, "OK" if ok else "缺失"))

print("\n结果: %d/%d 存在" % (len(CHECK) - len(missing), len(CHECK)))
sys.exit(0 if not missing else 3)
