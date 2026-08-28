# 帝江号仓库

帝江号仓库公共 Pipeline 提供仓库主界面识别、界面进入、地区切换和物品分类切换能力。

## 公开节点

| 节点 | 说明 | 前置状态 |
| --------------------------------------- | ---------------------------------- | ---------------------- |
| `InDijiangDepot` | 判断当前是否位于帝江号仓库主界面 | 无 |
| `SceneEnterMenuBackpackWithDepot` | 从任意界面进入帝江号仓库主界面 | 无 |
| `DijiangDepotSwitchToValleyIV` | 将仓库切换到四号谷地 | 仓库主界面或仓库切换页 |
| `DijiangDepotSwitchToWuling` | 将仓库切换到武陵 | 仓库主界面或仓库切换页 |

分类节点要求当前位于仓库主界面；已经选中目标分类时会直接完成。

| 分类 | 节点 |
| -------- | ------------------------------------------------- |
| 全部 | `DijiangDepotSelectItemCategoryAll` |
| 矿物 | `DijiangDepotSelectItemCategoryOre` |
| 植物 | `DijiangDepotSelectItemCategoryPlant` |
| 产物 | `DijiangDepotSelectItemCategoryProduct` |
| 采集材料 | `DijiangDepotSelectItemCategoryDoodad` |
| 培养素材 | `DijiangDepotSelectItemCategoryNurturance` |
| 可用道具 | `DijiangDepotSelectItemCategoryUsable` |
| 生产工具 | `DijiangDepotSelectItemCategoryProducer` |
| 随身装置 | `DijiangDepotSelectItemCategoryPortableDevice` |

## 调用顺序

从任意界面切换仓库和分类时，依次调用：

1. `SceneEnterMenuBackpackWithDepot`
2. `DijiangDepotSwitchToValleyIV` 或 `DijiangDepotSwitchToWuling`
3. 对应的 `DijiangDepotSelectItemCategory...` 节点
