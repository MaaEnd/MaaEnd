# 售卖产品生成器

从 zmdmap 精简游戏数据生成 SellProduct 任务的 Pipeline、Task 选项与部署数据。任务流程、选品/干员规则和缓存语义见[开发手册 - SellProduct](../../../docs/zh_cn/developers/tasks/sell-product-maintain.md)，本文只讲生成器本身。

## 数据流

```text
zmdmap 数据 CI：TableCfg → data/scripts/sell_product_data.py → 发布精简游戏数据 sell_product.json
MaaEnd：fetch-data.mjs → data/sell_product.json
                      → 本目录生成器 → assets/ 下的生成产物（不要手改）
```

## 文件职责

- `model.mjs`：共享模型，统一定义据点/地区、多语言键和稳定顺序。据点 `LocationId` 由游戏数据英文名称派生；只有存在实际 OCR 误识证据时才追加识别候选。地区顺序在这里定义，生成器按 `sellProductRegionsNewestFirst`（新地区优先）输出主循环遍历列表，地区内据点保持模型顺序。
- 每个模板对应一组「投影 + 模板 + 配置」三件套，投影只提供该模板需要的最小数据；某个模板独有的参数留在对应投影文件，不要塞进共享模型：

| 投影 | 生成产物 |
| ----------------------- | ------------------------------------------------------------- |
| `pipeline-data.mjs` | Win32 据点 Pipeline（`{Region}/{Location}.json`） |
| `pipeline-adb-data.mjs` | ADB 据点 Pipeline |
| `sell-data.mjs` | 地区售卖入口（含地区据点锚点 `SellProductIn{Region}Outpost`） |
| `loop-data.mjs` | 主循环 `Loop.json` 的地区遍历列表 |
| `session-data.mjs` | `OperatorSession.json` 的据点注册链 |
| `task-data.mjs` | `assets/tasks/SellProduct.json` 的任务选项 |

- `selection-data.mjs`：把售卖产品精简游戏数据预计算为 `assets/data/SellProduct/selection_data.json`（Go 运行时数据）；活动物品临时排除项集中在这里，原始数据移除活动物品后应清理并重新生成。
- `sync-locales.mjs`：同步五语言 locale——按游戏据点顺序重排据点键、据点名始终覆盖为当前官方译文、补齐缺失的据点/干员/物品键（中文名与既有键相同的货品复用旧键）。

## 命令

```shell
# 在仓库根目录运行（同步 zmdmap 精简游戏数据并完整生成）
pnpm generate:SellProduct

# 仅同步 zmdmap 精简游戏数据
pnpm fetch:zmdmap

# 使用已缓存的数据补齐五语言据点、干员键和缺失的物品键
node tools/pipeline-generate/SellProduct/sync-locales.mjs

# 使用已缓存的数据生成部署所需的最小选品数据
node tools/pipeline-generate/SellProduct/selection-data.mjs

# 等价于在当前目录运行
pnpm exec maa-pipeline-generate --config pipeline-config.json
pnpm exec maa-pipeline-generate --config sell-config.json
pnpm exec maa-pipeline-generate --config session-config.json
pnpm exec maa-pipeline-generate --config task-config.json
# 需要生成安卓端（ADB）专用流水线时使用
pnpm exec maa-pipeline-generate --config pipeline-adb-config.json
```

## 维护注意

- 不要手改生成产物；修改对应模板或数据投影后重新生成。
- 新增货品通常只需更新售卖产品数据，`sync-locales.mjs` 会自动补齐五语言 `item.*` 键。
- 新增据点后检查生成的地区 `next`、SceneManager 入口及 Win/ADB 两套产物。
- 新增地区时先在两套资源包的 `SellProduct/` 下手动创建地区子目录再运行生成（生成器只创建 `outputDir`）。
- 提交前运行 `node --test` 本目录的三个 `*.test.mjs`。
