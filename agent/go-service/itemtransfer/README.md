# ItemTransfer Fallback (NND 兜底策略)

当 `ItemTransferFindItemInRepo` 的 NeuralNetworkDetect 识别失败时，由本模块接管，通过 **悬停 + OCR + 二分法** 在当前可见页面中定位目标物品并完成转移。

## 触发时机

Fallback 仅在 Pipeline 的上下滚动循环全部完成、NND 仍未找到物品时触发，位于 `ItemTransferScrollUpwardRepo` 的 `next` 链中、`ItemTransferItemNotFound` 之前。

```
Pipeline 滚动循环（现有逻辑）
  NND 尝试 → 滚动下翻 → NND 尝试 → 触底 → 滚动上翻 → NND 尝试
  └── 全部失败 → Go 兜底（当前页面）→ 格子耗尽 → ItemNotFound
```

## 工作流程

```
1. 以低阈值（0.3）运行 NND，不过滤 class，获取当前页面所有物品的 box
2. 按网格位置排序：先按 Y 聚类分行（行间距 > 20px），再行内按 X 排序
   得到从左到右、从上到下的一维格子序列

3. Case 2.1 —— 目标 class 被检测到但得分低于阈值：
   悬停在该物品中心 → 等待 1s → OCR tooltip → 名称匹配 → Ctrl+Click

4. Case 2.2 —— 目标 class 未检测到：
   4a. 若 category_order 数据可用 → 二分法搜索当前页面可见格子
   4b. 若 category_order 数据为空 → 线性扫描每个格子

5. 格子二分区间归零（lo > hi，没有相邻格子可查）→ 返回 false，任务失败
```

## 二分法搜索

依赖 `item_order.json` 中 `category_order` 提供的物品排序（按游戏内升序排列）。

1. 将当前页面所有格子排成一维序列（从左到右、从上到下）
2. 取中间格子，悬停并 OCR 物品名
3. 在 `category_order` 中查找 OCR 结果的索引 `ocrIdx` 和目标物品的索引 `targetIdx`
4. `ocrIdx < targetIdx` → 搜索右半区（物品在后面）
5. `ocrIdx > targetIdx` → 搜索左半区（物品在前面）
6. `lo > hi` → 没有格子可查，返回失败

若物品选项中配置了 `"descending": true`（降序排列），Go 代码会在运行时反转 `category_order`。

## 文件结构

```
agent/go-service/itemtransfer/
├── action.go      # ItemTransferFallbackAction 主逻辑
├── types.go       # 类型定义、常量、数据加载
├── register.go    # 注册 Custom Action
└── README.md

assets/data/ItemTransfer/
└── item_order.json  # 物品 class → 名称/类别映射 + 各类别排序
```

## Pipeline 节点

| 节点 | 用途 |
|------|------|
| `ItemTransferDetectAllItems` | NND 低阈值检测仓库区域所有物品 |
| `ItemTransferDetectAllItemsBag` | NND 低阈值检测背包区域所有物品 |
| `ItemTransferTooltipOCR` | OCR 辅助节点，ROI 由 Go 代码运行时覆盖 |
| `ItemTransferFindItemFallback` | 仓库侧兜底入口 |
| `ItemTransferFindItemFallbackBag` | 背包侧兜底入口 |
| `ItemTransferFindItemFallbackBagReturn` | 背包返还侧兜底入口 |

Fallback 节点仅出现在各 `ScrollUpward` 节点的 `next` 链末尾（`ItemNotFound` / 链尾之前），不参与正常的滚动循环。

## `custom_action_param` 参数

通过 `pipeline_override` 在 `tasks/ItemTransfer.json` 的每个物品选项中传入：

```json
{
    "target_class": 141,
    "descending": false,
    "side": "repo"
}
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `target_class` | int | - | NND 模型的 class ID，与 `ItemTransferFindItemInRepo.expected` 相同 |
| `descending` | bool | `false` | 当前排序是否为降序，为 `true` 时反转 `category_order` |
| `side` | string | `"repo"` | 操作区域：`"repo"` 使用仓库 ROI，`"bag"` 使用背包 ROI |

## `item_order.json` 数据格式

```json
{
    "items": {
        "141": { "name": "蓝铁矿", "category": "矿物" }
    },
    "category_order": {
        "矿物": ["蓝铁矿", "紫晶矿", "源矿"],
        "植物": ["原木", "芽针", "..."],
        "产物": ["..."],
        "可用道具": ["..."]
    }
}
```

- `items`：NND class ID（字符串）→ 物品名称 + 所属类别
- `category_order`：每个类别下所有物品的**游戏内升序排列名称**（中文），用于二分法定位。需手动填写。

## 关键常量

定义在 `types.go` 中，可根据实际游戏 UI 调整：

| 常量 | 值 | 说明 |
|------|----|------|
| `tooltipOffsetX` | 15 | tooltip 相对悬停点的 X 偏移（右侧） |
| `tooltipOffsetY` | 0 | tooltip 相对悬停点的 Y 偏移 |
| `tooltipWidth` | 155 | tooltip OCR 区域宽度 |
| `tooltipHeight` | 70 | tooltip OCR 区域高度 |

## 环境变量

| 变量 | 说明 |
|------|------|
| `MAAEND_ITEMTRANSFER_DATA_DIR` | 手动指定 `item_order.json` 所在目录；未设置时自动从 cwd / exe 向上搜索 `assets/data/ItemTransfer/`。找不到时错误日志会列出所有尝试过的候选路径。 |
