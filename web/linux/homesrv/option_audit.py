#!/usr/bin/env python3
"""选项展开基线检查（上游升级后的第一道防线）。

背景
----
2026-10-20 发现 AutoStockStaple「跑完但一件没买」：上游把购买清单选项加深了一层
（`AutoStockBuyDailyGoodsValleyIV` checkbox → switch → `…Items` checkbox），
而 `resolve_preset.py` 只展开两层，28 条 `attach` 被静默丢弃，任务照常返回成功。
同类风险还有 `input` 型选项（其 pipeline_override 挂在 option 根上）。

本脚本把「每个任务展开后有多少条覆盖、关键 attach 有多少条、覆盖内容的指纹」
记成基线，升级后对比：掉项或内容变化都会报出来。

用法
----
    python3 option_audit.py --save [--force]   # 建立/刷新基线（默认拒绝在有漂移时覆盖）
    python3 option_audit.py --check            # 与基线对比，有丢失/变化返回非 0
    python3 option_audit.py --check --json-out /path/drift.json
"""

import hashlib
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(HERE))  # web/linux

import resolve_preset as rp  # noqa: E402

BASELINE = os.path.join(HERE, "option_baseline.json")
CONFIG = rp.CONFIG


def _fingerprint(ov):
    """覆盖内容的指纹：键不变但值被改（例如 int 被写成字符串）也能发现。"""
    blob = json.dumps(ov, ensure_ascii=False, sort_keys=True)
    return hashlib.sha1(blob.encode("utf-8")).hexdigest()[:16]


def collect():
    """返回 {instance: {task: {n, attach, keys, hash}}}。"""
    cfg = json.load(open(CONFIG, encoding="utf-8-sig"))
    out = {}
    for inst in cfg.get("instances") or []:
        iid = inst.get("id")
        if not iid:
            continue
        try:
            _, plan = rp.load_preset(iid)
        except Exception as e:
            out[iid] = {"__error__": str(e)[:120]}
            continue
        per = {}
        for p in plan:
            ov = p.get("override") or {}
            attach = {
                k: len(v["attach"]) for k, v in ov.items() if isinstance(v, dict) and isinstance(v.get("attach"), dict)
            }
            per[p["name"]] = {
                "n": p.get("n_override", len(ov)),
                "attach": attach,
                "keys": sorted(ov.keys()),
                "hash": _fingerprint(ov),
                "notes": [
                    n for n in (p.get("notes") or []) if n.startswith(("MISSING", "UNKNOWN", "TRUNCATED", "ERROR"))
                ],
            }
        out[iid] = per
    return out


def compare(cur, base):
    """返回 (loss, changed, added)。loss=覆盖减少；changed=数量不变但内容变了。"""
    loss, changed, added = [], [], []
    for iid, per in cur.items():
        bper = base.get(iid) or {}
        if "__error__" in per:
            loss.append({"instance": iid, "task": "__error__", "detail": per["__error__"]})
            continue
        for task in sorted(set(bper) - set(per)):
            loss.append({"instance": iid, "task": task, "detail": "任务消失（上游删除/改名或被移出实例）"})
        for task, info in per.items():
            b = bper.get(task)
            if not b:
                added.append({"instance": iid, "task": task})
                continue
            if info["n"] < b["n"]:
                lost_keys = sorted(set(b.get("keys") or []) - set(info["keys"]))
                loss.append(
                    {
                        "instance": iid,
                        "task": task,
                        "detail": "覆盖 %d→%d，丢失键 %s" % (b["n"], info["n"], lost_keys[:5]),
                    }
                )
                continue
            for k, cnt in (b.get("attach") or {}).items():
                now = (info.get("attach") or {}).get(k, 0)
                if now < cnt:
                    loss.append({"instance": iid, "task": task, "detail": "attach %s %d→%d" % (k, cnt, now)})
            if b.get("hash") and info["n"] == b["n"] and info["hash"] != b["hash"]:
                changed.append({"instance": iid, "task": task, "detail": "覆盖键数相同但内容变了（检查类型/取值）"})
            if info["notes"]:
                changed.append({"instance": iid, "task": task, "detail": "解析告警: %s" % info["notes"][:3]})
    return loss, changed, added


def main():
    save = "--save" in sys.argv
    force = "--force" in sys.argv
    json_out = None
    if "--json-out" in sys.argv:
        json_out = sys.argv[sys.argv.index("--json-out") + 1]

    cur = collect()
    total = sum(len(v) for v in cur.values() if isinstance(v, dict))

    if save:
        if os.path.exists(BASELINE) and not force:
            base = json.load(open(BASELINE, encoding="utf-8")).get("data") or {}
            loss, changed, _ = compare(cur, base)
            if loss or changed:
                print(
                    "拒绝覆盖基线：相对旧基线有 %d 处丢失、%d 处变化，确认无误后加 --force" % (len(loss), len(changed))
                )
                for x in (loss + changed)[:8]:
                    print("  - %s/%s %s" % (x["instance"], x["task"], x["detail"]))
                return 1
        payload = {
            "saved_at": __import__("time").strftime("%Y-%m-%dT%H:%M:%S"),
            "tasks_version": _version(),
            "data": cur,
        }
        json.dump(payload, open(BASELINE, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
        print("基线已写入 %s（%d 个任务画像）" % (BASELINE, total))
        return 0

    if not os.path.exists(BASELINE):
        print("没有基线文件，先跑 --save")
        return 2
    base = json.load(open(BASELINE, encoding="utf-8")).get("data") or {}
    loss, changed, added = compare(cur, base)

    print("版本: %s  实例: %d  任务: %d" % (_version(), len(cur), total))
    for x in loss:
        print("  ✗ %-10s %-20s %s" % (x["instance"], x["task"], x["detail"]))
    for x in changed:
        print("  ! %-10s %-20s %s" % (x["instance"], x["task"], x["detail"]))
    for x in added:
        print("  ＋ %-10s %-20s 新任务（基线里没有）" % (x["instance"], x["task"]))

    if json_out:
        json.dump(
            {
                "at": __import__("time").strftime("%Y-%m-%dT%H:%M:%S"),
                "version": _version(),
                "loss": loss,
                "changed": changed,
                "added": added,
            },
            open(json_out, "w", encoding="utf-8"),
            ensure_ascii=False,
            indent=1,
        )

    if loss:
        print("结果: %d 处覆盖减少 —— 大概率是上游改了选项树，检查 resolve_preset.py" % len(loss))
        return 1
    if changed:
        print("结果: 无丢失，%d 处内容变化（需人工确认，无误后 --save --force）" % len(changed))
        return 1
    print("结果: 无覆盖项丢失、无内容变化")
    return 0


def _version():
    try:
        d = json.load(open(os.path.join(rp.ROOT, "interface.json"), encoding="utf-8-sig"))
        return d.get("version", "?")
    except Exception:
        return "?"


if __name__ == "__main__":
    sys.exit(main())
