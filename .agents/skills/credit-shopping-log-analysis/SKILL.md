---
name: credit-shopping-log-analysis
description: 分析 MaaEnd `CreditShoppingMain` 的日志。用于还原信用购物任务中实际购买了什么商品、每件商品的折扣力度、是否触发过刷新（或刷新次数已用尽）、稳健刷新是否触发、以及信用点的消耗状况。货架证据来自 IconRecognition 的 `CreditShoppingPriority{1,2,3}Item` / `CreditShoppingShelfAnyItem`（detail 含 `cell_box` / `row` / `column` / `item_id`）；折扣 OCR 为 `CreditShoppingPriority*Discount`（买不起链为 `*DiscountCannotAfford`）；买得起判定为 `*CanAfford` / `*CanNotAfford`。适用于用户询问信用点交易、信用购物买了什么、折扣情况、刷新配置、`CreditShoppingMain` 任务行为等场景。
---

# CreditShoppingMain 日志分析

该 Skill 仅用于 `CreditShoppingMain`。

不要将本流程复用到 `AutoStockStapleMain`、`AutoStockpileMain` 或通用 issue 故障排查。

## 节点速查（当前 Pipeline）

| 用途 | 日志中搜索的节点名 |
| ---- | -------------------------- |
| 优先购买 N 的物品识别（IconRecognition） | `CreditShoppingPriority1Item` / `CreditShoppingPriority2Item` / `CreditShoppingPriority3Item` |
| 任意购买物品识别（全 catalog） | `CreditShoppingShelfAnyItem` |
| 未售罄（ColorMatch） | `CreditShoppingPriority{1,2,3}NotSoldOut`、`CreditShoppingShelfAnyNotSoldOut` |
| 买得起 / 买不起（ColorMatch） | `CreditShoppingPriority{1,2,3}CanAfford` / `...CanNotAfford`、`CreditShoppingShelfAnyCanAfford` |
| 折扣 OCR（买得起链） | `CreditShoppingPriority1Discount` / `2Discount` / `3Discount` |
| 折扣 OCR（买不起链，补信用等） | `CreditShoppingPriority{1,2,3}DiscountCannotAfford` |
| 购买决策（And 链） | `CreditShoppingBuyPriority1/2/3`、`CreditShoppingBuyBlacklist`、`CreditShoppingPrudentRefresh`、`CreditShoppingRefreshCountReached` |
| 购买确认 / 奖励领取 | `CreditShoppingBuyConfirm`、`CreditShoppingBuyFailed`、`CreditShoppingClaimConfirm` |
| 保留阈值（表达式识别） | `CreditShoppingReserveCredit`、`CreditShoppingReserveCreditSatisfied` |
| 刷新状态 | `RefreshButton`、`RefreshCost`、`RefreshUsedUp`、`RefreshItem` |

> **已删除、勿再 grep**：`CreditIcon`、`CreditShoppingRecordShelfSlot`、`CreditShoppingRecordItem*`、`RecordItemDiscount`、`CreditShoppingScanItemAction`、`BuyFirstOCR`、`Priority2OCR`、`IsDiscountPriority*`。货架记录模块与商品名 OCR 都已移除，分析**新日志**时必须用上表名称。

## 适用范围

- "信用购物买了什么"
- "折扣力度如何"
- "有没有刷新商品"
- "稳健刷新触发了吗"
- "信用点消耗了多少"
- "为什么没买/没刷新"
- "货架上有什么 / 缺哪一格 / 某时刻货架什么样"

## 主要证据来源

按优先级读取：

1. `maafw.log`（最新会话）
2. `maafw.bak.*.log`（若任务发生在之前的会话）
3. **`CreditShoppingShelfAnyItem` / `CreditShoppingPriority{N}Item`**：IconRecognition 的 Custom 结果。每个 `all_results_` 元素带 `cell_box`（格子框）、`row` / `column`（行列）、`item_id`（catalog id）、`score`，是还原货架的唯一权威来源。
4. **`*Discount`**：折扣角标 OCR，`all_results_` 里是 `{"box":[x,y,w,h],"score":..,"text":"-50%"}`。
5. **`*CanAfford` / `*CanNotAfford` / `*NotSoldOut`**：ColorMatch，`count` 与 `box` 可用来解释「为什么这一格没被买」。
6. `go-service.log`：`apply priority items: OverridePipeline applied`（本次勾选）、`ExpressionRecognition`（信用点数值 / 保留阈值）。
7. `mxu-web-YYYY-MM-DD.log`：前端下发的 `pipelineOverride`（勾选与开关）。

代码上下文（了解节点语义）：

- `assets/tasks/CreditShopping.json`
- `assets/resource/pipeline/CreditShopping/Flow/ApplyPriorityItems.json`
- `assets/resource/pipeline/CreditShopping/Item/ShelfChecks.json`

## 工作流

### 1. 锁定任务实例

在 `maafw*.log` 中搜索：

```text
Tasker.Task.Starting.*CreditShoppingMain
```

记录命中的 `task_id`，后续所有分析限定在该 `task_id` 范围内。

### 2. 读取本次勾选与开关

先在 `go-service.log` 搜：

```text
apply priority items: OverridePipeline applied
```

日志里的 `priority1/2/3` 数组就是本次三档勾选（case 名），据此知道哪些 `CreditShoppingPriority{N}Item` 会真正参与识别。

再在 `mxu-web-YYYY-MM-DD.log` 里找同一个任务的 `pipelineOverride`，关注：

```json
"CreditShoppingPrudentRefresh": {"enabled": false/true},
"RefreshItem":                  {"enabled": false/true},
"CreditShoppingBuyPriority1":   {"enabled": false/true},
"CreditShoppingBuyPriority2":   {"enabled": false/true},
"CreditShoppingBuyPriority3":   {"enabled": false/true}
```

- `CreditShoppingPrudentRefresh: enabled: false` → 稳健刷新被**主动禁用**，不是条件不满足
- `RefreshItem: enabled: false` → 刷新商品功能关闭，不会消耗信用点刷新

### 3. 还原货架（以 IconRecognition 的格子为准）

同一 `task_id` 下，每次进入识别会有一条 Custom 结果：

```json
{"box":[56,144,160,160],
 "detail":{"cell_box":[56,144,160,160],"row":0,"column":0,
           "item_id":"item_gold","item_box":[49,150,175,175],"score":0.95}}
```

还原步骤：

1. 取**同一帧**（同一时间戳附近）的 `CreditShoppingShelfAnyItem` 或三个 `CreditShoppingPriority{N}Item` 的 `all_results_`，按 `row` / `column` 归位。
2. 只出现部分格子时（例如某一档只认自己勾选的商品），**不得**把不同帧的结果拼成一份货架；换 `CreditShoppingShelfAnyItem` 或同一帧的其他档补全。
3. `CreditShoppingShelfAnyItem` 只在强制购买 / 稳健刷新 / 次数用尽的兜底路径里跑；**普通一轮扫描没有全 catalog 结果**，此时只能按各档自己的 `Priority{N}Item` 描述，缺的格子写「未识别」，不要编造。
4. 槽位布局：ADB 为 **6+4**（上排 6、下排 4），Win32 为 **7+3**，合计 10 格；以日志里实际出现的 `row` / `column` 为准。

### 4. 还原折扣

折扣来自 `CreditShoppingPriority{N}Discount`（买不起链为 `...DiscountCannotAfford`），`expected` 由该档「最低折扣」选项改写。

```log
"name":"CreditShoppingPriority1Discount" ... {"box":[169,140,53,25],"score":0.99,"text":"-50%"}
```

**逐次对比法**：比较相邻两次扫描的 `all_results_`，消失的条目对应刚被购买的商品。

折扣 ROI 是相对**物品框**的偏移（`ShelfChecks.json` 的 `roi_offset`），所以某个格子有物品识别、但该格折扣 OCR 为空时，通常意味着那一格没有角标，而不是识别失败。

「任意」档（`CreditShoppingPriority{N}DiscountValue` = `Any`）会把该档折扣节点整体换成 `DirectHit`：日志里没有折扣 OCR 文本、也不会出现 `*Discount` 的 `all_results_`，这是配置结果，不是识别失败，更不能据此推断「该格无折扣」。

### 5. 还原实际购买

购买事实以框架点击结果为准，不能仅看识别候选。弹窗内的商品名 OCR 已移除，播报由 IMS A3 负责。

1. `maafw*.log` 中搜本轮 `CreditShoppingBuyConfirm`（`Action.Succeeded`）
2. 确认后续 `CreditShoppingClaimConfirm`（奖励界面关闭）成功
3. 商品名来源：同帧 `CreditShoppingBuyPriority{N}` 的 `all_results_` 里 `CreditShoppingPriority{N}Item` 的 `item_id`、`CreditShoppingShelfAnyItem` 的 `item_id`、或 IMS A3 的「获得 xxx ×n」；**不得编造**

只有同时满足以下条件才算已购买：

- `CreditShoppingBuyConfirm` Action.Succeeded
- `CreditShoppingClaimConfirm`（或购买成功文案）识别并成功关闭奖励界面

常见 `item_id` → 中文对照（与 `assets/tasks/CreditShopping.json` 的 case 名一致）：

| Catalog ID | 商品名 |
| ---------------------------- | ------------ |
| `ArmsInspector` | 武器检查单元 |
| `ArmsINSPKit` | 武器检查装置 |
| `ArsenalTicket` | 武库配额 |
| `Oroberyl` | 嵌晶玉 |
| `TCreds` | 折金票 |
| `Protoprism` | 协议棱柱 |
| `Protohedron` | 协议棱柱组 |
| `Protodisk` | 协议圆盘 |
| `Protoset` | 协议圆盘组 |
| `ElementaryCombatRecord` | 初级作战记录 |
| `IntermediateCombatRecord` | 中级作战记录 |
| `ElementaryCognitiveCarrier` | 初级认知载体 |
| `CastDie` | 强固模具 |
| `HeavyCastDie` | 重型强固模具 |

### 6. 判断刷新状态

**区分两种「刷新」**：

#### 6a. 今日刷新次数已用尽（`CreditShoppingRefreshCountReached`）

```text
CreditShoppingRefreshCountReached.*Succeeded
今日刷新次数已用尽
```

命中说明游戏内每日免费刷新配额已耗尽（非 MAA 刷新），OCR 同时会读到倒计时文字。

该节点 Succeeded 后仍会点击购买剩余商品，**不等于成功刷新了一次货架**。

#### 6b. 稳健刷新（`CreditShoppingPrudentRefresh`）

```text
Node.Recognition.Starting.*CreditShoppingPrudentRefresh
```

**只有**找到该 `Recognition.Starting` 才算真正进入识别；仅出现在 `parse_node` / `NextList` 中不算触发。

### 7. 信用点数值追踪

在 `go-service.log` 中搜索：

```text
ExpressionRecognition.*CreditShoppingReserveCreditOCRInternal
```

每条包含：

```json
{
    "expression": "{CreditShoppingReserveCreditOCRInternal}>=300",
    "resolved_expression": "850>=300",
    "values": {"CreditShoppingReserveCreditOCRInternal": 850},
    "matched": true
}
```

与 `maafw.log` 的购买事件按时间对齐，即可还原信用点时间线。

> 数值有时因 OCR 时机（购买确认动画中）出现跳变，需结合上下文解读，不要孤立解释单个数值。

## 输出模板

````markdown
## CreditShoppingMain 概要

- task_id: `...`
- 起止时间: `...`
- 结束原因: 自然完成 / 被停止

## 本次勾选

| 档位 | 勾选商品 | 开关（无条件购买 / 自动补信用 / 稳健刷新） |
| ---- | -------- | ------------------------------------------ |
| P1   | ...      | ...                                        |
| P2   | ...      | ...                                        |
| P3   | ...      | ...                                        |

## 实际购买

| #   | 时间 | 商品         | 折扣       | 购买路径                     |
| --- | ---- | ------------ | ---------- | ---------------------------- |
| 1   | ...  | 武器检查单元 | 由日志填写 | Priority 2 扫描命中          |

## 货架快照

### 首次进入（时间：`...`）

```text
上排（row=0）: [col0 折金票 -50%] [col1 协议圆盘 -50%] ...
下排（row=1）: [col0 协议圆盘 -25%] ...（其余未识别）
```

### 第 N 次刷新后（`RefreshItem` Action.Succeeded 之后）

（同上格式；数值来自同一帧的 IconRecognition 结果）

## 刷新状态

- 每日刷新配额：已用尽（OCR: 「今日刷新次数已用尽」+ 倒计时）
- 实际刷新次数：0 次
- 稳健刷新：未触发（原因: `CreditShoppingPrudentRefresh` enabled: false）

## 信用点时间线

| 时间     | 信用点读数 | 事件                              |
| -------- | ---------- | --------------------------------- |
| 01:22:57 | 850        | 任务开始，储备门控 ≥300 通过      |
| 01:23:06 | 528        | 购买①后                           |
````

## 约束（Guardrails）

- 仅分析 `CreditShoppingMain`，不混入其他任务的购买列表。
- 稳健刷新未触发时，必须区分「被禁用（enabled: false）」与「条件不满足」。
- `CreditShoppingRefreshCountReached` Succeeded **不等于**执行了一次商品刷新。
- 只有 `Recognition.Starting` 出现在 `CreditShoppingPrudentRefresh` 节点时，才能确认稳健刷新真正进入识别。
- **货架只能来自同一帧的 IconRecognition 结果**（`cell_box` / `row` / `column` / `item_id`）；不得跨帧拼接，也不得用已删除的记录节点或快照 JSON。
- 普通扫描没有全 catalog 结果时，只描述实际出现的 `CreditShoppingPriority{N}Item` 命中，缺格写「未识别」，禁止猜测。
- **折扣**须来自 `CreditShoppingPriority{N}Discount`（或 `*DiscountCannotAfford`）；不得猜测。
- 判断"没有购买"之前，必须确认目标 `task_id` 内无 `CreditShoppingBuyConfirm` Action.Succeeded + 奖励界面关闭的组合。
- 信用点数值异常跳变标注 ⚠️。

### 防幻觉（禁止编造）

- **商品名**只能来自 `item_id` 对照表、`CreditShoppingPriority{N}Item` / `CreditShoppingShelfAnyItem` 的 detail、A3 Focus 或 logs 中明确字段。
- **槽位**只能来自同一帧的 IconRecognition 结果；不得跨帧拼成 10 格。
- 区分：**槽位（row/column）**、**商品（item_id）**、**折扣 OCR** 分开写，勿混为一谈。

## 历史日志（重构前）

若日志日期早于当前 Pipeline，可能仍出现 `CreditIcon`、`CreditShoppingRecordShelfSlot`、`BuyFirstOCR`、`IsDiscountPriority2` 等节点名；仅在对**旧包**做考古时使用旧 grep，并在结论中注明「旧 Pipeline」。**新日志一律用上文节点表。**
