"""Linux 侧冒烟：WlRootsController 能不能连上 sway、截到帧、点得动。

不碰 MaaEnd 资源，只验证控制器三个能力：连接 / 截图 / 点击。
截图会存成 wlr_shot.png，供离线评分复用。
"""

from __future__ import annotations

import os
import sys
import time

import numpy as np
from PIL import Image

SOCK = os.environ.get("WLR_SOCKET", "/tmp/xdg-run/wayland-1")
OUT = os.environ.get("SHOT_OUT", "/tmp/wlr_shot.png")


def main():
    from maa.controller import WlRootsController

    print("socket:", SOCK, "存在:", os.path.exists(SOCK))
    ctrl = WlRootsController(SOCK)

    j = ctrl.post_connection()
    j.wait()
    print("连接: succeeded=%s status=%s connected=%s" % (j.succeeded, j.status, ctrl.connected))
    if not ctrl.connected:
        return 2
    print("分辨率:", ctrl.resolution, "uuid:", ctrl.uuid)

    t0 = time.time()
    j2 = ctrl.post_screencap()
    j2.wait()
    arr = ctrl.cached_image
    if arr is None:
        print("[X] 截图返回空")
        return 3
    arr = np.asarray(arr)
    print("截图:", arr.shape, arr.dtype, "均值 %.1f  %.2fs" % (float(arr.mean()), time.time() - t0))
    rgb = arr[:, :, :3] if arr.ndim == 3 and arr.shape[2] in (3, 4) else arr
    if rgb.dtype != np.uint8:
        rgb = np.clip(rgb, 0, 255).astype(np.uint8)
    Image.fromarray(rgb).save(OUT)
    print("已存:", OUT)

    h, w = arr.shape[:2]
    t0 = time.time()
    j3 = ctrl.post_click(w // 2, h // 2)
    j3.wait()
    print("点击(%d,%d): succeeded=%s  %.2fs" % (w // 2, h // 2, j3.succeeded, time.time() - t0))
    return 0


if __name__ == "__main__":
    sys.exit(main())
