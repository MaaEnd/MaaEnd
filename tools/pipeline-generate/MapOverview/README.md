# 地图总览点击节点

从 [region3d_data.json](https://voice.fz.wiki/output_maaend/region3d_data.json) 下载各地区的总览点击坐标，生成 `assets/resource/pipeline/SceneManager/SceneMapOverview.json`。

```shell
pnpm generate:MapOverview
node tools/pipeline-generate/MapOverview/generate.mjs --no-download
node --test tools/pipeline-generate/MapOverview/generate.test.mjs
```

默认下载并生成；`--no-download` 使用已入库的 `clicks.json` 离线生成。

## 数据和命名

- 当前远端结构以各地图的 `center_px` 提供点击点，生成器仅提取这些坐标到 `clicks.json`，不保存 `area`、polygon 或来源元数据。
- 坐标必须是 1280×720 基准下的两个整数，且地图 `status` 为 `ok`；不满足时直接报错。
- 节点名由双下划线开头的内部公共前缀、地区 key 和地图 key 拼接，例如 `__SceneMapOverviewClick_region01_map01_lv001`。
- 点击节点的 `next` 自动指向 `__SceneMapOverviewSuccess_地区key_地图key`，无需逐张地图登记。
- `desc` 使用“在地图总览对应区域中，点击进入枢纽区地图”这样的描述；在线优先使用远端同一份数据中的地图 `name`，无需维护地区名称映射。
- 离线生成或远端名称缺失时，地图名使用 `tools/zmdmap/data/teleport_anchors.json` 中的 `levels.<地图 key>.names.zh_cn`；名称表也没有记录时显示地图 key。该名称表由现有 zmdmap 工具链同步，无需手动补充。

## 流程边界

地区和地图均直接从下载数据中遍历，无需另行登记。生成器生成全部点击节点及其 `next`，并清理数据中已不存在的生成点击节点；`clicks.json` 仍只保存点击坐标。

生成器扫描 `assets/resource/pipeline/` 下的 JSON/JSONC，检查对应名称的 Success 是否已存在。现有 Success 保留原文件、位置和识别逻辑，仅统一名称，由原维护者继续维护；工具不生成或修改其识别实现。

找不到 Success 时，工具在 `SceneMapOverview.json` 末尾追加带 `enabled: false` 的占位节点，避免空节点直接命中。手写 Success 及其注释会在后续生成时保留。接入者可直接补全占位节点的识别逻辑并启用；若将实际 Success 放在其他文件，需要先删除原占位节点，并沿用相同节点名。

`SceneValleyIV.json` 和 `SceneWuling.json` 仍负责确认总览界面、切换地区标签，以及确认进入了目标地图；它们在地区切换后引用生成的点击节点。旧的 OCR 点击和坐标 fallback 均已删除，合并为这一个点击动作；总览入口和后续传送流程保持原有实现。
