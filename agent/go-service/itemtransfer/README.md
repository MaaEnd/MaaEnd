# ItemTransfer 物品搬运

本模块包含两个 Custom Action：

- **ItemTransferFallbackAction** — NND 兜底，用于有 NND class ID 的物品在 NND 识别失败时的 fallback
- **ItemTransferOCRAction** — OCR 直查，用于没有 NND class ID 的物品，直接通过 OCR 定位

两者共享相同的 OCR 二分法搜索逻辑和 `item_order.json` 数据。

## Fallback（NND 兜底）

当 `ItemTransferFindItemInRepo` 的 NeuralNetworkDetect 识别失败时触发。仅挂载在各 `ScrollUpward` 节点的 `next` 链末尾，只在 NND 上下翻页全部失败后才触发一次。

```
Pipeline 滚动循环
  NND 尝试 → 滚动下翻 → NND 尝试 → 触底 → 滚动上翻 → NND 尝试
  └── 全部失败 → Go 兜底（仅当前页面）→ 格子耗尽 → ItemNotFound
```

工作流程：

1. 截图，以低阈值（0.3）运行 NND（不过滤 class），获取当前页面所有物品的 box
2. 按网格位置排序（Y 聚类分行，行内 X 排序）
3. Case 2.1 — 目标 class 被检测到但得分低于阈值：悬停 OCR 验证 → Ctrl+Click
4. Case 2.2 — 目标 class 未检测到：二分法搜索或线性扫描

## OCR 直查（无 NND class 物品）

用于 `item_order.json` 的 `category_order` 中有记录但没有 NND class ID 的物品（如壤晶、中容武陵电池等）。物品在 `tasks/ItemTransfer.json` 的 `WhatToTransfer` 列表中与 NND 物品并列，通过 `pipeline_override` 将 `ItemTransferFindItemInRepo` 替换为 `ItemTransferFindItemWithOCR`。

```
Pipeline 流程（OCR 物品）
  排序 → 点击分类标签 → ItemTransferFindItemWithOCR（Go OCR 直查）
  └── 无滚动，直接在当前页面 OCR 定位 → Ctrl+Click → 切换仓库
```

### 网格重建

NND 检测仅用于获取坐标框架。由于目标物品没有 NND class，NND 可能漏检该物品的网格位置。`buildFullGrid` 从 NND 返回的所有坐标重建完整网格：

1. 按 Y 坐标聚类确定行数（相邻 Y 差距 ≤ 34px 视为同行），取每行平均 Y
2. 取全局最小 X 作为第 1 列
3. 按固定列数（仓库 8 列、背包 5 列）和固定间距（69px）生成所有列坐标
4. 输出 `行数 × 列数` 的完整网格，确保每个位置都可被二分法访问

## 二分法搜索

依赖 `item_order.json` 中 `category_order` 提供的物品排序（按游戏内升序排列）。Fallback 和 OCR 直查共用此逻辑。

1. **固定从第一个格子（左上角）开始**：悬停 1s 后 OCR tooltip 物品名
2. 若第一个格子已经超过目标（`ocrIdx > targetIdx`）→ 目标不在当前页面，立即返回失败
3. 否则在 `[1, len-1]` 范围内进行标准二分搜索，固定向后收敛
4. 每次取 `mid = (lo + hi) / 2`，OCR 后比较 `ocrIdx` 与 `targetIdx`
5. `ocrIdx < targetIdx` → `lo = mid + 1`（向后推进）
6. `ocrIdx > targetIdx` → `hi = mid - 1`（从右侧收窄）
7. `lo > hi` → 范围耗尽，返回失败

### OCR 失败时的方向决策

当 OCR 结果为空、包含 "已盛装"、或物品名不在 `category_order` 中时，固定向后推进（`lo = mid + 1`），不做反向回退。

### 降序处理

若物品选项中配置了 `"descending": true`，Go 代码在运行时反转 `category_order`，使逻辑统一为"索引小 = 格子上方"。

### 名称匹配

`matchesTarget` 使用精确匹配（非子串匹配），仅在清除 OCR 噪声字符（空格、`·`、`.`、`,`、`、`）后再比较一次，避免 "芽针" 误匹配 "芽针种子" 等情况。

### Side 推断

`side` 参数决定使用仓库侧还是背包侧的 NND 检测节点和 ROI。当 `custom_action_param` 中未显式指定 `side` 时（常见于 `pipeline_override` 整体替换丢失默认值），`inferSide` 根据当前 pipeline 节点名自动推断：节点名含 `Bag` → bag 侧，否则 → repo 侧。

## 文件结构

```
agent/go-service/itemtransfer/
├── action.go      # ItemTransferFallbackAction（NND 兜底）
├── ocr_action.go  # ItemTransferOCRAction（OCR 直查）+ buildFullGrid
├── types.go       # 类型定义、常量、数据加载、inferSide
├── register.go    # 注册 Custom Action
└── README.md

assets/data/ItemTransfer/
└── item_order.json  # 物品 class → 名称/类别映射 + 各类别排序
```

## Pipeline 节点

| 节点                                    | 用途                                   |
| --------------------------------------- | -------------------------------------- |
| `ItemTransferDetectAllItems`            | NND 低阈值检测仓库区域所有物品         |
| `ItemTransferDetectAllItemsBag`         | NND 低阈值检测背包区域所有物品         |
| `ItemTransferTooltipOCR`                | OCR 辅助节点，ROI 由 Go 代码运行时覆盖 |
| `ItemTransferFindItemFallback`          | 仓库侧兜底入口                         |
| `ItemTransferFindItemFallbackBag`       | 背包侧兜底入口                         |
| `ItemTransferFindItemFallbackBagReturn` | 背包返还侧兜底入口                     |
| `ItemTransferFindItemWithOCR`           | OCR 直查仓库侧入口                     |
| `ItemTransferFindItemWithOCRBag`        | OCR 直查背包侧入口                     |
| `ItemTransferFindItemWithOCRBagReturn`  | OCR 直查背包返还侧入口                 |

## `custom_action_param` 参数

### Fallback（ItemTransferFallbackAction）

```json
{
    "target_class": 141,
    "descending": false,
    "side": "repo"
}
```

| 字段           | 类型   | 默认值   | 说明                                      |
| -------------- | ------ | -------- | ----------------------------------------- |
| `target_class` | int    | -        | NND 模型的 class ID                       |
| `descending`   | bool   | `false`  | 当前排序是否为降序                        |
| `side`         | string | 自动推断 | `"repo"` 或 `"bag"`，未设置时从节点名推断 |

### OCR 直查（ItemTransferOCRAction）

```json
{
    "item_name": "壤晶",
    "descending": false,
    "side": "repo"
}
```

| 字段         | 类型   | 默认值   | 说明                                       |
| ------------ | ------ | -------- | ------------------------------------------ |
| `item_name`  | string | -        | 物品中文名称，需与 `category_order` 中一致 |
| `descending` | bool   | `false`  | 当前排序是否为降序                         |
| `side`       | string | 自动推断 | `"repo"` 或 `"bag"`，未设置时从节点名推断  |

## `item_order.json` 数据格式

```json
{
    "items": {
        "141": {"name": "蓝铁矿", "category": "Ore"}
    },
    "category_order": {
        "Ore": [
            "赤铜矿",
            "蓝铁矿",
            "紫晶矿",
            "源矿"
        ],
        "Plant": [
            "原木",
            "芽针",
            "..."
        ],
        "Product": [
            "壤晶",
            "息壤",
            "..."
        ],
        "Usable": ["..."]
    }
}
```

- `items`：NND class ID（字符串）→ 物品名称 + 所属类别。仅包含 NND 模型支持的物品。
- `category_order`：每个类别下所有物品的**游戏内升序排列名称**（中文），用于二分法定位。可以包含不在 `items` 中的物品（如非 NND 识别的物品），只要排序正确即可。

## 关键常量

| 常量              | 值  | 定义位置        | 说明                        |
| ----------------- | --- | --------------- | --------------------------- |
| `gridCellSpacing` | 69  | `ocr_action.go` | 网格格子中心间距（px）      |
| `repoCols`        | 8   | `ocr_action.go` | 仓库侧每行列数              |
| `bagCols`         | 5   | `ocr_action.go` | 背包侧每行列数              |
| `tooltipOffsetX`  | 15  | `types.go`      | tooltip 相对悬停点的 X 偏移 |
| `tooltipOffsetY`  | 0   | `types.go`      | tooltip 相对悬停点的 Y 偏移 |
| `tooltipWidth`    | 155 | `types.go`      | tooltip OCR 区域宽度        |
| `tooltipHeight`   | 70  | `types.go`      | tooltip OCR 区域高度        |

## 环境变量

| 变量                           | 说明                                                                                                 |
| ------------------------------ | ---------------------------------------------------------------------------------------------------- |
| `MAAEND_ITEMTRANSFER_DATA_DIR` | 手动指定 `item_order.json` 所在目录；未设置时自动从 cwd / exe 向上搜索 `assets/data/ItemTransfer/`。 |
