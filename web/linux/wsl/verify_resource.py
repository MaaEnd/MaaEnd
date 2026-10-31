#!/usr/bin/env python3
"""验证资源能不能按 MXU 的那 5 个路径加载成功（Linux 控制器会额外追加 resource_linux）"""
import os
import sys

sys.path.insert(0, "/root/maaend/web/linux")
os.environ.setdefault("LD_LIBRARY_PATH", "/root/maaend/maafw")

from maa.resource import Resource

PATHS = [
    "/root/maaend/resource",
    "/root/maaend/resource_linux",
    "/root/maaend/web/resource_web",
    "/root/maaend/web/linux/resource_web_linux",
    "/root/maaend/web/resource_web_homesrv",
]

res = Resource()
for p in PATHS:
    ok_dir = os.path.isdir(p)
    job = res.post_bundle(p).wait()
    print("%-46s 目录存在=%-5s loaded=%s" % (p, ok_dir, job.succeeded))
print("整体 loaded =", res.loaded)
