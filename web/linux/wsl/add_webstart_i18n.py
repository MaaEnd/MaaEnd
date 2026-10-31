#!/usr/bin/env python3
"""把 WebStartGame 的 i18n 文案写进 locales/interface/<lang>.json。

MXU 的界面文案全部走 `$task.X.label` 形式的键；任务文件里写死中文会让前端在
解析描述时拿到 undefined（表现为「加载失败 / Object.entries requires that input
parameter not be null or undefined」）。所以必须补键。
"""
import json
import os
import shutil

LOC = "/root/maaend/locales/interface"
TEXTS = {
    "zh_cn": ("▶️开始游戏（云·终末地网页版）",
              "在云·终末地页面点「开始游戏」→ 标题页「点击任意位置继续」，直到进入大世界/菜单；已在游戏内会自动跳过。"),
    "zh_tw": ("▶️開始遊戲（雲·終末地網頁版）",
              "在雲·終末地頁面點「開始遊戲」→ 標題頁「點擊任意位置繼續」，直到進入大世界/選單；已在遊戲內會自動跳過。"),
    "en_us": ("▶️ Start Game (Cloud Endfield Web)",
              "Click Start Game on the cloud page, then click anywhere on the title screen, until in world/menu. Skipped if already in game."),
    "ja_jp": ("▶️ゲーム開始（クラウド・エンドフィールド Web）",
              "クラウドページで「ゲーム開始」を押し、タイトル画面をクリックしてワールド／メニューに入る。既にゲーム内ならスキップ。"),
    "ko_kr": ("▶️게임 시작 (클라우드 엔드필드 웹)",
              "클라우드 페이지에서 게임 시작을 누르고 타이틀 화면을 클릭해 월드/메뉴로 진입. 이미 게임 중이면 건너뜀."),
}

for lang, (label, desc) in TEXTS.items():
    p = os.path.join(LOC, lang + ".json")
    if not os.path.exists(p):
        print("%-6s 缺失，跳过" % lang)
        continue
    shutil.copy2(p, p + ".bak-webstart")
    d = json.load(open(p, encoding="utf-8"))
    d["task.WebStartGame.label"] = label
    d["task.WebStartGame.description"] = desc
    json.dump(d, open(p, "w", encoding="utf-8"), ensure_ascii=False, indent=2)
    print("%-6s 已写入（键数 %d）" % (lang, len(d)))
