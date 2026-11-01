# 地图总览点击节点

使用统一的 zmdmap 数据源获取各地区的总览点击坐标，生成 `assets/resource/pipeline/SceneManager/SceneMapOverview.json`。

```shell
pnpm generate:MapOverview
pnpm fetch:zmdmap --only region3d --only teleport_anchors
node tools/pipeline-generate/MapOverview/generate.mjs
node --test tools/pipeline-generate/MapOverview/generate.test.mjs
```

`pnpm generate:MapOverview` 会先由 zmdmap 下载并校验 `region3d`，再运行生成器。单独运行生成器时使用已有的
`tools/zmdmap/data/region3d.json`，不会自行请求远端或维护副本。

## 数据和命名

- 数据结构为 `region01: {name: "四号谷地", area: {map01_lv001: {polygon: [...], click: [455, 478]}}}`。生成器读取地区 `name` 和 `area` 内各地图的 `click`，不保留 `polygon`。
- `tools/zmdmap/data/region3d.json` 是唯一数据来源，文件头的 `version`、`sha256` 由 zmdmap 管理；生成器只读取地区的 `name`、`area` 以及地图的 `click`。
- 地区名称和非空 `area` 必须存在，坐标必须是 1280×720 基准下的两个整数。不满足时直接报错。
- 总览入口节点按 `__ScenePrivateMapOverview_地区key_EnterMap_地图key` 命名，例如 `__ScenePrivateMapOverview_region01_EnterMap_map01_lv001`，保留“从该地区总览进入目标地图”的语义。
- 点击节点的 `next` 自动指向 `__ScenePrivateMapAnyEnterMap_地区key_地图key_Success`，表示已从任意地图界面进入目标地图，无需逐张地图登记。
- 点击节点的 `desc` 使用“在四号谷地总览界面，点击进入枢纽区地图”这样的描述，地区名来自远端地区 `name`。
- 地图名使用 `tools/zmdmap/data/teleport_anchors.json` 中的 `levels.<地图 key>.names.zh_cn`，名称表没有记录时显示地图 key。该名称表由现有 zmdmap 工具链同步，无需手动补充。

## 流程边界

地区和地图均直接从下载数据中遍历，无需另行登记。生成器生成全部点击节点及其 `next`，并清理数据中已不存在的生成点击节点。

删除点击节点前，生成器检查其他 Pipeline 和本文件保留的手写节点的 `next`、`on_error` 静态引用（包括 `[JumpBack]`）。若仍有引用，则报告节点及引用位置，并在写入前终止，保持 `clicks.json` 和 Pipeline 原样；确认地图已移除时，应先同步调整调用流程，再运行生成器。

生成器扫描 `assets/resource/pipeline/` 下的 JSON/JSONC，检查对应名称的 Success 是否已存在。已有 Success 保留原文件、位置、内容和注释，由接入者维护；工具只补齐缺失的占位节点。

找不到 Success 时，工具在 `SceneMapOverview.json` 末尾追加带 `enabled: false` 的占位节点，避免空节点直接命中。占位描述为“在地图界面，成功进入四号谷地-枢纽区地图；待实现地图成功确认，实现后启用”。接入者可直接补全占位节点的识别逻辑并启用；若将实际 Success 放在其他文件，需要先删除原占位节点，并沿用相同节点名。

`SceneValleyIV.json` 和 `SceneWuling.json` 负责确认总览界面、切换地区标签，以及确认进入了目标地图。四号谷地入口在地区切换后引用对应地图的生成点击节点；武陵入口优先通过 OCR 识别地图名称并点击，未命中时使用生成坐标兜底，两条路径都接入同一 Success。试验园区和应龙关的 OCR 引用因识别不稳定保持注释禁用，直接使用生成坐标。OCR 节点及入口引用由接入者维护，生成器不修改。
