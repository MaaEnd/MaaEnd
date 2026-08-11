# ItemTransfer 配置生成

该工具从 IconRecognition 发布资源生成库存转移任务的物品选项，只更新
`assets/tasks/ItemTransfer.json` 中的 `option.WhatToTransfer.cases`。

数据规则：

- 仅选择 `storageKind` 为 `Normal`，且 `categoryType` 为 `Ore`、`Plant`、
  `Product`、`Nurturance` 或 `Usable` 的物品；
- `Ore` 仅保留赤铜矿、蓝铁矿、紫晶矿和源矿对应的四个固定 ID；
- 先按 `Ore`、`Plant`、`Product`、`Doodad`、`Nurturance`、`Usable`、
  `Producer`、`PortableDevice` 的分类顺序排列，再在分类内按 `sortId1`、
  `sortId2`、`id` 依次降序排列；当前未进入物品列表的分类也保留排序定义；
- case 的 `name` 取简体中文 IconRecognition 文案，`label` 使用对应 i18n key；
- 按 `categoryType` 选择库存界面分类模板，并为三个物品查找节点覆盖 `item_ids`。

生成配置：

```powershell
conda activate cuda124
python tools/icon_recognition/item_transfer/generate.py
```

运行测试：

```powershell
conda activate cuda124
python -m unittest discover -s tools/icon_recognition -p "test_generate.py" -v
```
