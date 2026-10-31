"""从 MaaEnd 发行版里解出「任务名 -> entry + 默认选项覆盖」。

为什么需要它：MXU 在提交任务时会把用户选的选项解析成 pipeline_override；
Linux 上没有 GUI，只能自己按 interface.json 的 option/cases 复现这一步。
"""

from __future__ import annotations

import glob
import json
import os
import re
import sys


def load_jsonc(path):
    s = open(path, "rb").read().decode("utf-8-sig")
    try:
        return json.loads(s)
    except json.JSONDecodeError:
        pass
    # 去块注释 -> 再去行首/行尾 // 注释
    s = re.sub(r"/\*.*?\*/", "", s, flags=re.S)
    s = re.sub(r"(?m)^\s*//.*$", "", s)
    s = re.sub(r'(?m)([^:"])\s*//[^"\n]*$', r"\1", s)
    s = re.sub(r",(\s*[}\]])", r"\1", s)
    return json.loads(s)


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else r"D:\jiaoben\MaaEnd-win-x86_64-v2.3.0"
    iface = load_jsonc(os.path.join(root, "interface.json"))

    files = []
    for pat in iface.get("import") or []:
        p = pat.replace("\\", "/")
        if not os.path.isabs(p):
            p = os.path.join(root, p)
        files += sorted(glob.glob(p))

    tasks, options = [], {}
    for f in files:
        try:
            d = load_jsonc(f)
        except Exception as e:
            print("  ! %s 解析失败: %s" % (os.path.basename(f), e))
            continue
        for k in ("task", "tasks"):
            if isinstance(d.get(k), list):
                tasks += [t for t in d[k] if isinstance(t, dict)]
        if isinstance(d.get("option"), dict):
            options.update(d["option"])

    print("任务数 %d   选项组 %d   （来源 %d 个文件）" % (len(tasks), len(options), len(files)))
    print()
    print("%-3s %-24s %-34s %s" % ("#", "任务名", "entry", "选项"))
    for i, t in enumerate(tasks, 1):
        print("%-3d %-24s %-34s %s" % (i, t.get("name"), t.get("entry"), ",".join(t.get("option") or []) or "-"))

    # 解析每个选项的默认 case
    print("\n=== 选项默认值 ===")
    for key, o in options.items():
        cases = o.get("cases") or []
        names = [c.get("name") for c in cases]
        dflt = o.get("default_case") or o.get("default")
        if dflt is None and cases:
            dflt = cases[0].get("name")
        print("  %-30s default=%-16s cases=%s" % (key, str(dflt), names))


if __name__ == "__main__":
    main()
