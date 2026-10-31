#!/usr/bin/env python3
"""修 MXU 白屏 / 「加载失败 Object.entries requires that input parameter not be null or undefined」。

根因：我们往 interface.json 的 resource 里塞了「官服-网页端」，但没给 `label`。
官方两个资源条目都是 {"name","label":"$resource.X.label","path"...} 的形状，
MXU 前端拿 label 去查语言包，查不到就是 undefined → Object.entries(undefined) 崩掉整个界面。
"""
import json
import os
import shutil

IFACE = "/root/maaend/interface.json"
LOC = "/root/maaend/locales/interface"
RES = "官服-网页端"
KEY = "resource.CNWeb.label"
TEXTS = {
    "zh_cn": "官服-网页端（云·终末地）",
    "zh_tw": "官服-網頁端（雲·終末地）",
    "en_us": "CN Server - Web (Cloud Endfield)",
    "ja_jp": "CNサーバー・Web（クラウド・エンドフィールド）",
    "ko_kr": "CN 서버 - 웹 (클라우드 엔드필드)",
}

shutil.copy2(IFACE, IFACE + ".bak-reslabel")
d = json.load(open(IFACE, encoding="utf-8-sig"))
for r in d["resource"]:
    if r.get("name") == RES:
        r.setdefault("label", "$" + KEY)
        print("资源条目:", json.dumps(r, ensure_ascii=False)[:200])
json.dump(d, open(IFACE, "w", encoding="utf-8"), ensure_ascii=False, indent=2)

for lang, txt in TEXTS.items():
    p = os.path.join(LOC, lang + ".json")
    j = json.load(open(p, encoding="utf-8"))
    j[KEY] = txt
    json.dump(j, open(p, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
print("已补 label 与 5 个语言包的键:", KEY)
