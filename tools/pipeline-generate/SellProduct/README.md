# 售卖物品

据点数据通过 zmdmap API 获取，存储在 `tools/pipeline-generate/data/` 目录。

`data.mjs` 统一维护据点数据和生成参数，`sell-data.mjs` 从中投影区域售卖入口与区域内据点列表。

```shell
# 在仓库根目录运行（自动拉取最新数据并生成）
pnpm generate:SellProduct

# 仅更新数据文件
pnpm fetch:zmdmap

# 使用已缓存的数据补齐五语言据点和干员键
node tools/pipeline-generate/SellProduct/sync-locales.mjs

# 等价于在当前目录运行
npx @joebao/maa-pipeline-generate --config pipeline-config.json
npx @joebao/maa-pipeline-generate --config sell-config.json
npx @joebao/maa-pipeline-generate --config task-config.json
# 需要生成安卓端（ADB）专用流水线时使用
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

`pnpm generate:SellProduct` 会在渲染前根据 `settlement_trade.json` 自动补齐五语言 locale 中缺失的据点和干员键；已有文案保持不变。

## 致谢

- 感谢 `zmdmap` 提供的数据
