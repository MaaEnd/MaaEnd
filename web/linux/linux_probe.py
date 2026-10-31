#!/usr/bin/env python3
"""Linux 侧离线节点评分：给一张截图，用 MaaFramework 自己的识别器算命中与分数。

为什么不用自写相关系数：框架的模板分与朴素 NCC 不是一回事（实测同一帧 NCC 0.9913、
框架给 0.8949），阈值判定必须用框架本体，否则会自欺欺人。

用法：
  python3 linux_probe.py --frame shot.png --node WebLauncherStartButton \
      --dirs /opt/MaaEnd/resource,/opt/MaaEnd/resource_linux,/opt/MaaEnd/resource_web
  python3 linux_probe.py --frame shot.png --node InWorld,WebQueueDetected --dirs ...

依赖：pip3 install --user MaaFw pillow numpy
"""

import argparse
import os
import sys

try:
    import numpy as np
    from PIL import Image
except Exception as e:
    print("[X] 需要 pillow + numpy：pip3 install --user pillow numpy")
    print("   ", e)
    sys.exit(2)

try:
    from maa.controller import CustomController
    from maa.pipeline import JAnd, JRecognitionType
    from maa.resource import Resource
    from maa.tasker import Tasker
except Exception as e:
    print("[X] 需要 MaaFw：pip3 install --user MaaFw")
    print("   ", e)
    sys.exit(2)


class NullController(CustomController):
    """只满足 Tasker 需要一个控制器的约束；识别用的是显式传入的图片。"""

    def connect(self):
        return True

    def connected(self):
        return True

    def request_uuid(self):
        return "linux-probe"

    def screencap(self):
        return np.zeros((720, 1280, 3), dtype=np.uint8)

    def click(self, x, y):
        return True

    def swipe(self, x1, y1, x2, y2, duration):
        return True

    def touch_down(self, contact, x, y, pressure):
        return True

    def touch_move(self, contact, x, y, pressure):
        return True

    def touch_up(self, contact):
        return True

    def click_key(self, keycode):
        return True

    def input_text(self, text):
        return True

    def key_down(self, keycode):
        return True

    def key_up(self, keycode):
        return True

    def start_app(self, intent):
        return False

    def stop_app(self, intent):
        return False


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--frame", required=True)
    ap.add_argument("--node", required=True, help="逗号分隔的节点名")
    ap.add_argument("--dirs", required=True, help="逗号分隔的资源目录（resource,resource_linux,resource_web）")
    ap.add_argument("--min-score", type=float, default=0.0, help="低于该分数的结果不打印（默认全打）")
    ap.add_argument(
        "--short-side",
        type=int,
        default=720,
        help="先把短边缩放到该值（默认 720，对应 MaaFramework 的 display_short_side）；0=不缩放",
    )
    a = ap.parse_args()

    dirs = [d for d in a.dirs.split(",") if d.strip()]
    for d in dirs:
        if not os.path.isdir(d):
            print("[X] 资源目录不存在: %s" % d)
            return 2

    img = Image.open(a.frame).convert("RGB")
    if a.short_side and min(img.size) != a.short_side:
        k = a.short_side / float(min(img.size))
        new = (max(1, round(img.size[0] * k)), max(1, round(img.size[1] * k)))
        img = img.resize(new, Image.BILINEAR)
        print("  缩放 %d->短边%d：%s" % (a.short_side, a.short_side, new))
    arr = np.ascontiguousarray(np.asarray(img, dtype=np.uint8)[:, :, ::-1])  # BGR
    print("帧: %s  %dx%d" % (a.frame, img.size[0], img.size[1]))

    res = Resource()
    for d in dirs:
        j = res.post_bundle(d)
        j.wait()
        if not j.succeeded:
            print("[X] 资源加载失败: %s" % d)
            return 3

    tasker = Tasker()
    tasker.bind(res, NullController())
    if not tasker.inited:
        print("[X] Tasker 初始化失败")
        return 4

    rc = 0
    for name in [n.strip() for n in a.node.split(",") if n.strip()]:
        job = tasker.post_recognition(JRecognitionType.And, JAnd(all_of=[name]), arr)
        job.wait()
        r = job.get()
        node = (getattr(r, "nodes", None) or [None])[0]
        rd = getattr(node, "recognition", None) if node else None
        if rd is None:
            print("  %-26s 无法解析（节点不存在？）" % name)
            rc = 5
            continue
        hit = bool(getattr(rd, "hit", None))
        best = _best(rd)
        if best and a.min_score and best[0] < a.min_score:
            continue
        extra = ""
        algo = str(getattr(_unwrap(rd), "algorithm", ""))
        if "OCR" in algo:
            txt = _best_text(rd)
            if txt:
                extra = "  text=%r" % txt
        print(
            "  %-26s hit=%-5s score=%s box=%s%s"
            % (name, hit, ("%.4f" % best[0]) if best else "-", best[1] if best else "-", extra)
        )
    return rc


def _unwrap(rd):
    """钻进 And/Or 包装，拿到真正干活的 RecognitionDetail。"""
    cur = rd
    for _ in range(4):
        br = getattr(cur, "best_result", None)
        subs = getattr(br, "sub_results", None) if br is not None else None
        if subs:
            cur = subs[0]
            continue
        break
    return cur


def _best_text(rd):
    cur = _unwrap(rd)
    raw = getattr(cur, "raw_detail", None)
    if isinstance(raw, dict):
        one = raw.get("best") or (raw.get("all") or [None])[0]
        if isinstance(one, dict):
            return one.get("text")
    return None


def _best(rd):
    """从 (可能是 And 包装的) 识别详情里取 (最高分, box)。

    And/Or 的 best_result 是 *RecognitionResult（带 sub_results），
    真正的分数在 sub_results[0].all_results[0].score 里。
    """
    cur = rd
    for _ in range(4):
        br = getattr(cur, "best_result", None)
        subs = getattr(br, "sub_results", None) if br is not None else None
        if subs:
            cur = subs[0]
            continue
        break
    for attr in ("all_results", "filtered_results"):
        arrs = getattr(cur, attr, None) or []
        best = None
        for item in arrs:
            s = getattr(item, "score", None)
            if s is None and isinstance(item, dict):
                s = item.get("score")
            b = getattr(item, "box", None)
            if b is None and isinstance(item, dict):
                b = item.get("box")
            if s is not None and (best is None or s > best[0]):
                best = (s, b)
        if best:
            return best
    raw = getattr(cur, "raw_detail", None)
    if isinstance(raw, dict):
        one = raw.get("best") or (raw.get("all") or [None])[0]
        if isinstance(one, dict) and "score" in one:
            return (one["score"], one.get("box"))
    return None


if __name__ == "__main__":
    sys.exit(main())
