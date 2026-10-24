"""在 Linux 上跑 MaaEnd 的 WebStartGame 管线（不走 MXU，纯 MaaFw Python 绑定）。

资源直接用 Windows 那份（已打过 resource_web 补丁），控制器用 WlRoots（sway）。
用法:
  python3 run_web_linux.py [entry] [--timeout 秒]
默认 entry=WebStartGameEntry。
"""

from __future__ import annotations

import argparse
import contextlib
import os
import sys
import time

RES_DIRS = [
    "/mnt/d/桌面/常用/旧机改造服务器/MaaEnd适配/../MaaEnd适配",  # 占位，下面覆盖
]
DIST = "/mnt/d/jiaoben/MaaEnd-win-x86_64-v2.3.0"
WEB = "/mnt/d/桌面/常用/旧机改造服务器/MaaEnd适配/web"
DEFAULT_DIRS = [
    os.path.join(DIST, "resource"),
    os.path.join(DIST, "resource_web"),
    # Linux 专属覆盖层：把 Windows 帧里抠的模板换成 Linux 帧里抠的（字体光栅化不同，0.947 -> 1.000）
    os.path.join(WEB, "linux", "resource_web_linux"),
]
SOCK = os.environ.get("WLR_SOCKET", "/tmp/xdg-run/wayland-1")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("entry", nargs="?", default="WebStartGameEntry")
    ap.add_argument("--timeout", type=float, default=600.0)
    ap.add_argument("--resource", action="append", default=None)
    a = ap.parse_args()

    from maa.controller import WlRootsController
    from maa.resource import Resource
    from maa.tasker import Tasker

    dirs = a.resource or DEFAULT_DIRS
    for d in dirs:
        if not os.path.isdir(d):
            print("[X] 资源目录不存在:", d)
            return 2

    print("== 加载资源 ==")
    res = Resource()
    for d in dirs:
        t0 = time.time()
        job = res.post_bundle(d)
        job.wait()
        print("  %-46s loaded=%s  %.1fs" % (os.path.basename(d), res.loaded, time.time() - t0))
    if not res.loaded:
        print("[X] 资源加载失败")
        return 3

    print("== 连接控制器 ==")
    ctrl = WlRootsController(SOCK)
    ctrl.post_connection().wait()
    print("  connected=%s" % ctrl.connected)
    if not ctrl.connected:
        return 4

    tasker = Tasker()
    tasker.bind(res, ctrl)
    print("  tasker.inited=%s" % tasker.inited)

    print("== 提交任务 %s ==" % a.entry)
    t0 = time.time()
    job = tasker.post_task(a.entry)
    deadline = time.time() + a.timeout
    last = None
    while time.time() < deadline:
        if job.done:
            break
        node = None
        with contextlib.suppress(Exception):
            node = tasker.get_latest_node()
        if node and node != last:
            print("  [%5.1fs] 节点: %s" % (time.time() - t0, node))
            last = node
        time.sleep(0.5)

    print("== 结果 ==")
    print("  done=%s succeeded=%s status=%s 用时 %.1fs" % (job.done, job.succeeded, job.status, time.time() - t0))
    if not job.done:
        print("  [X] 超时")
        with contextlib.suppress(Exception):
            tasker.post_stop().wait()
        return 5
    return 0 if job.succeeded else 6


if __name__ == "__main__":
    sys.exit(main())
