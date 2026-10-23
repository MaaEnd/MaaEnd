# 地图总览点击节点

从 [region3d_data.json](https://voice.fz.wiki/output_maaend/region3d_data.json) 下载各地区的总览点击坐标，生成 `assets/resource/pipeline/SceneManager/SceneMapOverview.json`。

```shell
pnpm generate:MapOverview
node tools/pipeline-generate/MapOverview/generate.mjs --no-download
node --test tools/pipeline-generate/MapOverview/generate.test.mjs
```

默认下载并生成，请求附带缓存刷新参数以获取最新发布内容；`--no-download` 使用已入库的 `clicks.json` 离线生成。

## 数据和命名

- 数据结构为 `region01: {name: "四号谷地", area: {map01_lv001: {polygon: [...], click: [455, 478]}}}`。生成器读取地区 `name` 和 `area` 内各地图的 `click`，不保留 polygon 或来源元数据。
- `clicks.json` 保留地区的 `name`、`area` 以及地图的 `click`；地图存在非空 `name` 时也保留，保证名称可以随点击数据一起离线使用，不维护手写地区映射。
- 地区名称和非空 `area` 必须存在，坐标必须是 1280×720 基准下的两个整数。若数据提供 `_meta.screen_size`，必须为 `[1280, 720]`；若地图提供 `status`，必须为 `ok`。不满足时直接报错。
- 节点名由双下划线开头的内部公共前缀、地区 key 和地图 key 拼接，例如 `__SceneMapOverviewClick_region01_map01_lv001`。
- 点击节点的 `next` 自动指向 `__SceneMapOverviewSuccess_地区key_地图key`，无需逐张地图登记。
- 点击节点的 `desc` 使用“在四号谷地总览界面，点击进入枢纽区地图”这样的描述，地区名来自远端地区 `name`。
- 地图名优先使用地图的可选 `name`，缺失时使用 `tools/zmdmap/data/teleport_anchors.json` 中的 `levels.<地图 key>.names.zh_cn`；名称表也没有记录时显示地图 key。该名称表由现有 zmdmap 工具链同步，无需手动补充。

## 流程边界

地区和地图均直接从下载数据中遍历，无需另行登记。生成器生成全部点击节点及其 `next`，并清理数据中已不存在的生成点击节点。

生成器扫描 `assets/resource/pipeline/` 下的 JSON/JSONC，检查对应名称的 Success 是否已存在。已有 Success 保留原文件、位置、内容和注释，由接入者维护；工具只补齐缺失的占位节点。

找不到 Success 时，工具在 `SceneMapOverview.json` 末尾追加带 `enabled: false` 的占位节点，避免空节点直接命中。占位描述为“在地图界面，成功进入四号谷地-枢纽区地图；待实现地图成功确认，实现后启用”。接入者可直接补全占位节点的识别逻辑并启用；若将实际 Success 放在其他文件，需要先删除原占位节点，并沿用相同节点名。

`SceneValleyIV.json` 和 `SceneWuling.json` 负责确认总览界面、切换地区标签，以及确认进入了目标地图；它们在地区切换后引用对应地图的单个生成点击节点。
