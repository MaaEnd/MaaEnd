# 售卖物品

据点数据通过 zmdmap API 获取，存储在 `tools/pipeline-generate/data/` 目录。

`model.mjs` 统一维护据点命名、排序和国际化键；三个模板各自消费最小数据投影：

- `pipeline-data.mjs`：Win32 Pipeline 据点与识别框；
- `pipeline-adb-data.mjs`：ADB Pipeline 据点与识别框；
- `task-data.mjs`：Task 中的物品、保留数量和干员选项。

据点 `LocationId` 由 zmdmap 英文名称自动派生；只有存在实际 OCR 误识证据时才在 `model.mjs` 追加兼容候选。某个模板独有的参数留在对应投影文件中。

```shell
# 在仓库根目录运行（自动拉取最新数据并生成）
pnpm generate:SellProduct

# 仅更新数据文件
pnpm fetch:zmdmap

# 使用已缓存的数据补齐五语言据点和干员键
node tools/pipeline-generate/SellProduct/sync-locales.mjs

# 等价于在当前目录运行
npx @joebao/maa-pipeline-generate --config pipeline-config.json
npx @joebao/maa-pipeline-generate --config task-config.json
# 需要生成安卓端（ADB）专用流水线时使用
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

`pnpm generate:SellProduct` 会在渲染前根据 `settlement_trade.json` 按游戏据点顺序重排五语言 locale 的据点键，据点名始终覆盖为 zmdmap 当前官方译文，并补齐缺失的据点和干员键。

## 致谢

- 感谢 `zmdmap` 提供的数据
