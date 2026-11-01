# AutoEssence 区域路线生成器

基质刷取地区的寻路节点由本目录生成，产物是 `assets/resource/pipeline/AutoEssence/RegionNodes/<区域 key>.json`；`routes.json` 里标了 `manual` 的地区仍使用手写节点。

```shell
pnpm generate:AutoEssence
```

命令先由 `sync-routes.mjs` 同步 `routes.json`，再由 `run-all.mjs` 按 `region-template.jsonc` 生成节点。

## 生成的节点

每个地区三个节点，节点名与锚点保持运行时约定：

| 节点 | 作用 |
| --- | --- |
| `GotoTriggerPointSetAnchor_<key>` | 由 Go 侧启用，设置 `AutoEssenceMoveToTriggerPoint` 与 `AutoEssenceMoveToTriggerPointMaybeTeleport` 两个锚点 |
| `GotoTriggerPointMaybeTeleport_<key>` | 传送到起点营地后前往激发点，连续命中上限 3 次，计数在 `AutoEssenceDispatcher` 清零 |
| `GotoTriggerPointMove_<key>` | 从当前位置前往激发点并交互，失败时转去传送重走 |

前往激发点的路线：

1. 首点 `ZONE` 声明地区的 Base 区域，由生成器插入；
2. 一个 `NAVMESH` 点直达激发点，带 `target_deck_y`（激发点的世界高度），在上下重叠的可走面中选中激发点所在的一层；
3. 末尾一个 `INTERACT` 点落在激发点上，严格到达，行进中认到交互提示即停车，确认后交回外层流程选择交互项。

## routes.json

`regions` 每个地区一条，`level` 与 `name` 由 `sync-routes.mjs` 按数据刷新，新地区自动补条目。其余字段由人工填写：

| 字段 | 说明 |
| --- | --- |
| `manual` | 为 `true` 时该地区仍使用手写的 `RegionNodes/<key>.json`，生成器不产出、只检查文件存在；不能与 `anchor` / `path` 同时使用 |
| `anchor` | 起点营地的入口节点名，须是该地图上已登记的营地。缺省取离激发点直线最近的营地 |
| `path` | 替换自动生成的 `NAVMESH` 段的路点数组；首点 `ZONE` 与末尾的 `INTERACT` 仍由生成器处理，不要写进来 |

直线最近的营地可能与激发点不连通（隔水、隔断崖或处于不同楼层），此时在 MapNavigator 中从候选营地对激发点做 `NAVMESH` 预览，取能到达且步行最短的营地写入 `anchor`。

祀的画之前的 12 个地区目前标了 `manual`，沿用手写路线；改为生成时去掉 `manual`，按上述规则补 `anchor` 或 `path`，并经实机确认。

## 新增地区

1. 在 `locations.go` 的 `locationLevelToKey` 与 `locationLabelKeys` 中登记区域 key 与地区文案 key；
2. 在五种语言的 `assets/locales/interface/*.json` 中补上地区名；
3. 运行 `pnpm generate:AutoEssence` 生成区域节点，再运行 `tools/essence_filter/build_autoessence_tasks.py` 生成任务选项，它同时把新地区补进 `AutoEssenceDispatcher` 的传送节点清零列表。

数据里出现、但 `locationLevelToKey` 尚未登记的地区不生成，生成器打印一行提示列出该地区。生成的路线需经实机运行确认。
