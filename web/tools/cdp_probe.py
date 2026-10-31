#!/usr/bin/env python3
"""在 CDP 控制器上验证：资源加载 → 绑定 → 单个识别节点。"""
import os
import sys
import time

DIST = "/root/maaend"
sys.path.insert(0, os.path.join(DIST, "web", "linux"))
sys.path.insert(0, os.path.join(DIST, "maafw"))
os.environ.setdefault("LD_LIBRARY_PATH", os.path.join(DIST, "maafw"))

from pathlib import Path  # noqa: E402

from cdp_controller import make_custom_controller  # noqa: E402
from maa.library import Library  # noqa: E402
from maa.resource import Resource  # noqa: E402
from maa.tasker import Tasker  # noqa: E402

Library.open(Path(os.path.join(DIST, "maafw")))

DIRS = [
    os.path.join(DIST, "resource"),
    os.path.join(DIST, "web", "resource_web"),
    os.path.join(DIST, "web", "linux", "resource_web_linux"),
    os.path.join(DIST, "web", "resource_web_homesrv"),
]

ctrl = make_custom_controller()
print("controller.connect() =", ctrl.connect())
print("video =", ctrl._vw, "x", ctrl._vh, " rect =", ctrl._rect)

res = Resource()
for d in DIRS:
    if not os.path.isdir(d):
        print("  跳过不存在的目录:", d)
        continue
    res.post_bundle(d).wait()
print("资源加载 loaded =", res.loaded)
if not res.loaded:
    raise SystemExit("资源加载失败")

from maa.define import LoggingLevelEnum  # noqa: E402

tasker = Tasker()
tasker.bind(res, ctrl)
tasker.set_stdout_level(LoggingLevelEnum.Info)
tasker.set_log_dir("/tmp/cdp-probe")

for entry in ("WebAlreadyInGame", "WebPageReady", "WebStartGameEntry"):
    t0 = time.time()
    job = tasker.post_task(entry)
    job.wait()
    st = job.status
    print("[%s] status=%s 用时 %.1fs" % (entry, st, time.time() - t0))
    time.sleep(0.5)

print("最后帧来源:", ctrl.last_source, "抓帧 %.0fms" % ctrl.last_ms)
