# AutoStockStaple General 模板

使用 `MAA-pipeline-generate` 为 `assets/resource/pipeline/AutoStockStaple/General` 下的重复节点生成模板结果。

## 生成目标

- `Goods.json`
- `GoodsCountValidate.json`
- `QuantityControl.json`

## 运行方式

在仓库根目录执行：

```bash
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/goods-config.json
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/goods-count-validate-config.json
npx @joebao/maa-pipeline-generate --config tools/pipeline-generate/AutoStockStaple/General/quantity-control-config.json
```

## 说明

- `data.mjs` 维护稳定需求物资的基础数据。
- 3 份模板都使用整值占位符，将 JS 中构造出的完整对象直接写入目标文件。
- 物品顺序与当前 `AutoStockStaple` 购买流程保持一致，避免生成后节点遍历顺序变化。
