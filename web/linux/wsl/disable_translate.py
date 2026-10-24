#!/usr/bin/env python3
"""关掉 profile 里的翻译功能（Chrome 155 的翻译气泡会盖住右上角 ROI）。
Chrome 运行时改 Preferences 会被回写，所以必须先停 Chrome。"""
import json
import os
import shutil
import sys

PROFILE = sys.argv[1] if len(sys.argv) > 1 else "/root/chrome-profile"
PREF = os.path.join(PROFILE, "Default", "Preferences")

if not os.path.exists(PREF):
    raise SystemExit("找不到 %s" % PREF)

shutil.copy2(PREF, PREF + ".bak-translate")
d = json.load(open(PREF, encoding="utf-8"))

tr = d.setdefault("translate", {})
tr["enabled"] = False
tr.pop("translate_recent_target", None)
d.setdefault("intl", {}).pop("accept_languages", None)

json.dump(d, open(PREF, "w", encoding="utf-8"), ensure_ascii=False)
print("已关闭翻译：translate.enabled=%s" % tr.get("enabled"))
