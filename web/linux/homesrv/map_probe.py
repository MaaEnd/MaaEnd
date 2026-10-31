#!/usr/bin/env python3
"""地图探针：进游戏 → 打开武陵/首墩地图 → 把地图相关识别的命中情况和 OCR 原文打出来。

用法（在 daily_run 已 export 的环境里跑）：
    MAAEND_ROOT=$HOME/maaend MAAEND_WEB=$HOME/maaend/web \
    MAAEND_RES_DIRS="$HOME/maaend/resource:$HOME/maaend/web/resource_web:$HOME/maaend/web/linux/resource_web_linux" \
    MAA_AGENT_BIN=$HOME/maaend/agent/go-service MAA_AGENT_LIB=$HOME/maaend/maafw \
    python3 ~/maaend/web/linux/homesrv/map_probe.py
"""

import os
import sys
import time

WEB = os.environ.get("MAAEND_WEB") or os.path.expanduser("~/maaend/web")
sys.path.insert(0, os.path.join(WEB, "linux"))

from maa.controller import WlRootsController  # noqa: E402
from maa.pipeline import JAnd, JRecognitionType  # noqa: E402
from maa.resource import Resource  # noqa: E402
from maa.tasker import Tasker  # noqa: E402
from run_daily_linux import DIRS, SOCK, log, start_agent  # noqa: E402

PROBE_NODES = [
    "InMapAny",
    "InMapWulingMarkerStone",
    "__ScenePrivateMapAnyEnterMapOverview",
    "ProbeMapTitle",
    "ProbeMapTitleWide",
]


def main():
    res = Resource()
    for d in [x for x in DIRS if os.path.isdir(x)]:
        res.post_bundle(d).wait()
    log("资源加载: %s" % res.loaded)

    ctrl = WlRootsController(SOCK, use_win32_vk_code=True)
    ctrl.post_connection().wait()
    log("控制器: connected=%s" % ctrl.connected)

    tasker = Tasker()
    tasker.bind(res, ctrl)
    try:
        from maa.define import LoggingLevelEnum

        tasker.set_stdout_level(LoggingLevelEnum.Info)
        tasker.set_log_dir(os.path.expanduser("~/maaend-log"))
    except Exception as e:
        log("(日志设置失败: %s)" % e)

    client, _proc = start_agent(res, "Linux-Wlroots", "官服-网页端")
    log("agent: %s" % bool(client))

    def run_node(node, cap=180):
        jb = tasker.post_task(node)
        t0 = time.time()
        while not jb.done and time.time() - t0 < cap:
            time.sleep(0.5)
        d = jb.get()
        ok = bool(getattr(d, "succeeded", False))
        log("  %-46s -> %s %.0fs" % (node, ok, time.time() - t0))
        return ok

    def probe(img, node):
        j = tasker.post_recognition(JRecognitionType.And, JAnd(all_of=[node]), img)
        j.wait()
        d = j.get()
        nid = list(getattr(d, "node_id_list", None) or [])
        if not nid:
            log("  %-34s 未产生节点详情" % node)
            return
        rd = tasker.get_node_detail(int(nid[0])).recognition
        log("  %-34s hit=%s box=%s" % (node, getattr(rd, "hit", None), getattr(rd, "box", None)))
        det = getattr(rd, "detail", None)
        if isinstance(det, dict):
            alls = det.get("all") or []
            texts = []
            for a in alls if isinstance(alls, list) else []:
                if isinstance(a, dict) and a.get("text"):
                    texts.append("%s(%.2f)" % (a["text"], a.get("score", 0)))
                elif isinstance(a, list):
                    for b in a:
                        if isinstance(b, dict) and b.get("text"):
                            texts.append("%s(%.2f)" % (b["text"], b.get("score", 0)))
            if texts:
                log("      OCR: %s" % " | ".join(texts[:12]))

    log("== 1) 进游戏")
    run_node("WebStartGameEntry", cap=900)

    log("== 2) 尝试打开 武陵-首墩 地图")
    run_node("SceneEnterMapWulingMarkerStone", cap=180)

    log("== 3) 截图 + 识别")
    ctrl.post_screencap().wait()
    img = ctrl.cached_image
    log("  截图: %s" % (None if img is None else "ok"))
    for n in PROBE_NODES:
        try:
            probe(img, n)
        except Exception as e:
            log("  %-34s 探针失败: %s" % (n, e))

    try:
        png = os.path.expanduser("~/maaend-log/map_probe.png")
        img.save(png) if hasattr(img, "save") else None
        log("截图保存: %s" % png)
    except Exception as e:
        log("(截图保存失败: %s)" % e)

    log("完成")
    return 0


if __name__ == "__main__":
    sys.exit(main())
