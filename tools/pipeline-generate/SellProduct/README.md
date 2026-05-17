# 售卖物品

据点数据通过 zmdmap API 获取，存储在 `tools/pipeline-generate/data/` 目录。

```shell
# 在仓库根目录运行（自动拉取最新数据并生成）
pnpm generate:SellProduct

# 仅更新数据文件
pnpm fetch:zmdmap

# 等价于在当前目录运行
npx @joebao/maa-pipeline-generate --config pipeline-config.json
npx @joebao/maa-pipeline-generate --config task-config.json
# 需要生成安卓端（ADB）专用流水线时使用
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

## 致谢

- 感谢 `zmdmap` 提供的数据
