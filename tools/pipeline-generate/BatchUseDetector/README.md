# 批量探测器

使用 `MAA-pipeline-generate` 工具批量生成对应的 Pipeline 文件。

## 运行方式

在仓库根目录执行，会依次跑完本目录下所有 `*config.json`：

```bash
pnpm generate:BatchUseDetector
```

## 新增/更新地区探测器和罗盘

修改文档同目录下的 `data.json`中的物品名称

## 新增地区独有道具

探物器和罗盘每个地区都有，写在 `data.json`；只有部分地区才有的道具（如武陵塔晶探针）写在 `extra-item-data.json`，一种道具一行：

- `LocationId`：所属地区，与 `data.json` 一致。
- `ItemId`：节点名后缀，生成 `BatchUseDetectorFind<ItemId>` 和 `BatchUseDetectorConfirm<ItemId>Name`；模板图为 `image/BatchUseDetector/<ItemId>.png`，`resource` 和 `resource_adb` 各放一张。
- `ItemName`：详情页里的物品名，OCR 确认用。

`region-data.mjs` 会把这些道具接进所属地区入口节点的 `next`。生成后还要在 `assets/tasks/BatchUseDetector.json` 的探测器类别里加对应选项，并在 `assets/locales/interface/` 补上各语言文案。
