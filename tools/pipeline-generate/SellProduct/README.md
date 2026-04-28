# 售卖物品

据点数据: <https://assets.zmdmap.com/data/entity/1.2.4/settlement_trade.json>

```shell
# 在仓库根目录运行
pnpm generate:SellProduct

# 等价于在当前目录运行
npx @joebao/maa-pipeline-generate --config pipeline-config.json
npx @joebao/maa-pipeline-generate --config task-config.json
# 需要生成安卓端（ADB）专用流水线时使用
npx @joebao/maa-pipeline-generate --config pipeline-adb-config.json
```

## 致谢

- 感谢 `zmdmap` 提供的数据
