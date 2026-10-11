# ItemTransfer 库存转移

当前 ItemTransfer Pipeline 使用 C++ `IconRecognition` 查找物品并执行可选的单格分类反查。页面滚动、仓库切换和转移动作仍由 Pipeline 控制。

## 组件分工

- `ItemTransferSameItemRecognition`：比较去程和返程配置的物品 ID，避免双向选择相同物品；
- `ExpressionRecognition`：公共表达式识别器，执行 Pipeline 数量 OCR 并复用 `ocrnum` 解析单位数量、比较保留量；
- `InventoryTransferAll`：公共 Pipeline，执行桌面端 Ctrl+Click 或 ADB 长按菜单全部转移，入口和输入契约见 [Inventory 公共 Pipeline](../../../assets/resource/pipeline/Common/Private/Inventory/README.md)。

去程、返程仓库保留数量独立配置，均为非负整数，默认 100。数量小于或等于保留量时仅关闭当前方向，另一方向仍可继续。识别或解析失败也关闭当前方向，并给出用户可见报错；`maafw.log` 保留数量 OCR 的原文与 ROI，`go-service.log` 记录公共表达式求值或解析失败原因。

保留量是每次取货前的停止门槛，仍执行全部转移动作，不保证搬运后精确留下该数量。带单位的游戏显示可能经过取整，比较依据是显示数量解析后的数值。

`ItemTransferFallbackAction` 和 `ItemTransferOCRAction` 的兼容实现仍保留在包内，但当前 ItemTransfer Pipeline 不再引用这些旧 NND/OCR 流程。

## 候选反查流程

ItemTransfer 的 IconRecognition 节点接收 Task 覆盖注入的唯一 `item_id` 和 `item_recheck_filters`：

```json
{
    "grid_type": "transfer",
    "item_ids": [
        "item_copper_ore"
    ],
    "item_recheck_filters": [
        "Normal:Ore"
    ],
    "deduplicate": true
}
```

一次当前页识别按以下顺序执行：

1. 使用 `grid_type=transfer` 和目标 `item_id` 调用 `IconRecognition`，取得当前页全部候选格；
2. 对每个候选格使用其 `cell_box` 作为 ROI；
3. 使用 `grid_type=single_roi` 和 `item_recheck_filters` 重新识别该格，不传 `item_ids`；
4. 反查得到的 `item_id` 与目标一致时返回该格；仓库取货先检查数量，再由 Pipeline 调公共转移动作；
5. 反查不一致时忽略该格并继续验证下一个候选；
6. 当前页没有候选通过时返回未命中，由现有 Pipeline 继续翻页或进入未找到分支。

`deduplicate=true` 时，同一 `item_id` 首次反查成功后跳过该 ID 的后续候选，以节约截图识别开销。反查只处理当前截图中的候选，不保存跨页屏蔽状态。Pipeline 滚动后会使用新截图重新识别。

## Task 参数生成

库存转移物品选项由 `tools/icon_recognition/item_transfer/generate.py` 从 IconRecognition catalog 生成。生成器负责把以下数据写入正向和返程 Task 覆盖：

- `item_ids`：目标物品 ID；
- `item_recheck_filters`：由 catalog 的 `storageKind` 和 `categoryType` 组成，用于 C++ 单格反查；
- `deduplicate`：固定为 `true`，避免重复反查同一物品；
- 分类入口：由 `categoryType` 选择 `SceneEnterMenuBackpackWithDepot*` 公共接口。

## Pipeline 节点

| 节点 | 区域 | 目标 |
| --- | --- | --- |
| `ItemTransferFindForwardItemInRepo` | 仓库 | 去程物品 |
| `ItemTransferFindReturnItemInRepo` | 仓库 | 返程物品 |
| `ItemTransferFindForwardItemInBag` | 背包 | 去程物品 |
| `ItemTransferFindReturnItemInBag` | 背包 | 返程物品 |

四个节点统一使用 `IconRecognition` 并由参数启用单格反查，ADB 资源覆盖网格 ROI、物品格点击范围与滚动条区域。数量 OCR 的 `roi` 引用刚找到的物品格，桌面端 `roi_offset=[0,46,0,-46]` 取底部 18px，ADB 覆盖为 `[0,58,0,-58]` 取底部 22px。识别未命中时继续完整扫描，滚动调用公共 `Inventory*Scroll*` 节点。

进入仓库、地区与分类切换复用公共帝江号仓库接口。满仓判断使用 OCR；目标仓库放不下或无法确认存入时，将物品退回原仓。

## 文件结构

```text
agent/go-service/itemtransfer/
├── same_item_recognition.go       # 双向相同物品判断
├── register.go                    # Go 组件注册
├── action.go                      # 旧 NND fallback 兼容实现
├── ocr_action.go                  # 旧 OCR 兼容实现
└── README.md

tools/icon_recognition/item_transfer/
├── generate.py                    # ItemTransfer Task 生成器
├── test_generate.py               # 生成器测试
└── README.md
```
