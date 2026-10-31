#!/usr/bin/env python3
"""中和损坏的 resource_linux 层。

背景：MXU 用 Linux 控制器时会自动给资源列表追加 `resource_linux`。
v2.35.0-beta.2 的这份 resource_linux 只有 10 个节点，其中 4 个的 next 指向
base 里不存在的节点（_AutoEcoFarmTargeInCenter / _AutoEcoFarmSwipeLoopCommon /
_AutoEcoFarmMoveToTargetErr / _AutoEcoFarmJumpBack），PipelineChecker 整体校验失败
→ MXU 报「资源加载失败」，任务根本起不来。

处理：把指向不存在节点的 next 项剔掉；若某节点因此变成空 next，就删掉该节点。
原始文件备份为 nodes.json.bak-orig。
"""
import json
import shutil

RL = "/root/maaend/resource_linux/pipeline/nodes.json"
BASE = "/root/maaend/resource/pipeline/nodes.json"

base = set(json.load(open(BASE, encoding="utf-8")))
d = json.load(open(RL, encoding="utf-8"))
shutil.copy2(RL, RL + ".bak-fix")

known = base | set(d)
dropped_ref, dropped_node = {}, []
for _ in range(20):
    known = base | set(d)
    changed = False
    for k in list(d):
        nxt = d[k].get("next")
        if not isinstance(nxt, list):
            continue
        keep = [n for n in nxt if not isinstance(n, str) or n.startswith("[") or n in known]
        if len(keep) != len(nxt):
            dropped_ref.setdefault(k, [])
            dropped_ref[k] += [n for n in nxt if n not in keep]
            changed = True
            if keep:
                d[k]["next"] = keep
            else:
                dropped_node.append(k)
                del d[k]
    if not changed:
        break

json.dump(d, open(RL, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("剔除的悬空引用:", dropped_ref)
print("因 next 全空而删除的节点:", dropped_node or "无")
print("剩余节点:", list(d))
