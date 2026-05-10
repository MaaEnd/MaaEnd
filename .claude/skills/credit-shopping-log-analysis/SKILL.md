---
name: credit-shopping-log-analysis
description: 分析 MaaEnd `CreditShoppingMain` 的日志。用于还原信用购物任务中实际购买了什么商品、每件商品的折扣力度、是否触发过刷新（或刷新次数已用尽）、稳健刷新是否触发、以及信用点的消耗状况。适用于用户询问信用点交易、信用购物买了什么、折扣情况、刷新配置、`CreditShoppingMain` 任务行为等场景。
---

# CreditShoppingMain 日志分析

该 Skill 仅用于 `CreditShoppingMain`。

不要将本流程复用到 `AutoStockStapleMain`、`AutoStockpileMain` 或通用 issue 故障排查。

## 适用范围

当用户提出下列问题时使用本 Skill：

- "信用购物买了什么"
- "折扣力度如何"
- "有没有刷新商品"
- "稳健刷新触发了吗"
- "信用点消耗了多少"
- "为什么没买/没刷新"

## 主要证据来源

按优先级读取：

1. `maafw.log`（最新会话）
2. `maafw.bak.*.log`（若任务发生在之前的会话）
3. `go-service.log`（信用点 OCR 数值、表达式求值）
4. `mxu-web-YYYY-MM-DD.log`（前端下发的 pipelineOverride，含开关配置）

代码上下文（了解节点语义）：

- `assets/tasks/CreditShopping.json`

## 工作流

### 1. 锁定任务实例

在 `maafw*.log` 中搜索：

```text
Tasker.Task.Starting.*CreditShoppingMain
task start:.*CreditShoppingMain
```

记录命中的 `task_id`，后续所有分析必须限定在该 `task_id` 范围内。

> 若 `maafw.log` 未命中，改查 `maafw.bak.*.log`，以文件时间戳最近的为优先。

### 2. 读取前端配置（关键前置步骤）

在对应日期的 `mxu-web-YYYY-MM-DD.log` 中找 `CreditShoppingMain` 的 `pipelineOverride`，重点关注末尾：

```json
"CreditShoppingPrudentRefresh": {"enabled": false/true},
"RefreshItem":                  {"enabled": false/true},
"CreditShoppingBuyPriority1":   {"enabled": false/true},
"CreditShoppingBuyPriority2":   {"enabled": false/true}
```

这一步决定哪些功能在本次运行中被关闭，从而解释后续日志中"节点从未进入识别"的原因。

**常见结论**：

- `CreditShoppingPrudentRefresh: enabled: false` → 稳健刷新被**主动禁用**，不是条件不满足
- `RefreshItem: enabled: false` → 信用点刷新商品功能关闭，不会消耗信用点刷新

### 3. 还原折扣信息

`IsDiscountPriority2` OCR 会在每次扫描时读取整行商品的折扣标签（`expected: "75|95|99"`），可用**多次扫描对比**定位每件商品的折扣。

在 `maafw*.log` 中搜索：

```log
OCRer.*IsDiscountPriority2
```

每次扫描的 `all_results_` 包含所有可见折扣，格式如：

```log
{"box":[x,y,w,h],"score":...,"text":"-75%"}
```

**逐次对比法**：比较相邻两次扫描的 `all_results_`，消失的条目对应刚被购买的商品。

配合 `CreditShoppingBuyPriority2` 的命中 box（x 坐标）即可定位该商品的折扣标签（同一列 x 坐标）。

> `filtered_results_` 中出现表示该条目满足 75/95/99 阈值，触发了优先级购买。

### 3b. 还原每次货架（名字 ROI 可视化）

当用户询问“第一次刷新出了什么”“买完某件后货架怎么变了”“有没有缺格/漏识别”时，必须补做这一步。

优先使用以下 OCR 节点的 `all_results_`：

```log
OCRer.*BuyFirstOCR
OCRer.*Priority2OCR
```

必要时配合这些节点判断“有几个槽位”和“是否卖空”：

```log
TemplateMatcher.*CreditIcon
ColorMatcher.*NotSoldOut
ColorMatcher.*BuyFirstOCRLabelColor
ColorMatcher.*BuyFirstOCRTextColor
```

还原方法：

1. 取某一轮扫描的 `BuyFirstOCR` 或 `Priority2OCR` 的 `all_results_`
2. 按名字 ROI 的 `y` 值分成两排
   - `y≈280` 视为上排
   - `y≈486` 视为下排
3. 每排内按 `x` 从小到大排序
4. 用 `CreditIcon` 的 10 个槽位模板 + `NotSoldOut` 的过滤结果核对是否有空位
5. 若上一轮是 10 格、下一轮变 9 格，且缺失位置与刚购买商品所在列一致，应明确说明这是“买空后的正常缺格”，不是 OCR 漏识别

#### 异常情况：刷新后只识别到部分货架

有时日志里会出现类似以下情况：

- `CreditIcon` 候选存在，但 `filtered_results_` 只剩 1~9 个
- `NotSoldOut` 只剩 1~9 个槽位
- 没有跑出完整的 `BuyFirstOCR` / `Priority2OCR` 名字列表
- 或者只识别出上排、下排中的一部分

这类帧必须视为**异常中间态 / 不稳定货架**，**不能**当作一次“成功刷新后的货架”直接下结论。

#### 区分两类货架

1. **完整刷新货架**
   - 指“首次进入”或“某次成功刷新后”用于描述本轮商店内容的货架
   - 默认要求 **10 个物品、10 个名字**
2. **购买后中间快照**
   - 指“买完某件后、下一次刷新前”的临时货架
   - 允许因买空出现 **9 格**
   - 只要能证明缺失位置与刚购买商品所在列一致，就应视为**正常买空快照**，不是异常帧

处理规则：

1. **完整刷新货架**必须满足：10 个物品、10 个名字
2. **购买后中间快照**允许是 9 格，但必须说明“缺的是刚买空的位置”
3. 如果只识别到部分槽位、部分名字，且又不满足“买空后的正常 9 格”条件，必须明确标注为“异常帧/不完整货架”
4. 查找补充帧时，**只能在同一轮刷新内继续向后找**，不得跨过下一次 `RefreshItem` 的 `Node.Action.Succeeded`
5. 只有在**下一次刷新点击之前**找到了完整 10 名字货架，才可作为“本次刷新后的货架”
6. 若直到下一次刷新点击或任务结束，都无法拿到完整 10 名字货架，必须如实说明“该次刷新后的完整货架未能从日志中稳定还原”

#### ADB 特例：分两次识别上下半货架

部分 ADB 场景下，日志/截图可能无法在同一帧内拿到完整两排商品：

- 第一次识别只覆盖上半部分
- 滑动后才出现下半部分

这类情况允许按**两次采集后合并**来还原单次货架，但仍需满足：

1. 合并后总数必须是 **10 个物品**
2. 10 个物品都必须有名字
3. 必须说明“该货架由上下半两次识别合并得到”
4. 合并范围必须限制在**同一轮刷新、且下一次 `RefreshItem` 点击之前**
5. 若上下半合并后仍不足 10 个，仍应判定为**不完整货架**

### 4. 还原实际购买

购买事实以框架点击结果为准，不能仅看 OCR 候选。

步骤：

1. 在 `maafw*.log` 中搜索 `CreditShoppingBuyItemOCR_.*Succeeded`，找到命中的商品名节点（例如 `CreditShoppingBuyItemOCR_ArmsInspector`）
2. 在该节点的 OCR `all_results_` 中确认商品名文本（例如 `"text":"武器检查单元"`）
3. 确认后续 `CreditShoppingClaimConfirm` 识别成功（含 `"text":"购买成功"`）

只有同时满足以下两个条件才算已购买：

- `CreditShoppingBuyItemOCR_X` Recognition.Succeeded
- `CreditShoppingClaimConfirm` 中 OCR 读到 `"购买成功"` 并点击成功

常见商品节点与中文名对照：

| 节点后缀                     | 商品名       |
| ---------------------------- | ------------ |
| `ArmsInspector`              | 武器检查单元 |
| `ArmsINSPKit`                | 武器检查装置 |
| `ArsenalTicket`              | 武库配额     |
| `Oroberyl`                   | 嵌晶玉       |
| `TCreds`                     | 折金票       |
| `Protoprism`                 | 协议棱柱     |
| `Protohedron`                | 协议棱柱组   |
| `Protodisk`                  | 协议圆盘     |
| `Protoset`                   | 协议圆盘组   |
| `ElementaryCombatRecord`     | 初级作战记录 |
| `IntermediateCombatRecord`   | 中级作战记录 |
| `ElementaryCognitiveCarrier` | 初级认知载体 |
| `CastDie`                    | 强固模具     |
| `HeavyCastDie`               | 重型强固模具 |

### 5. 判断刷新状态

**区分两种不同的「刷新」概念**：

#### 5a. 今日刷新次数已用尽（`CreditShoppingRefreshCountReached`）

在 `maafw*.log` 中搜索：

```text
CreditShoppingRefreshCountReached.*Succeeded
今日刷新次数已用尽
```

若命中，说明**游戏内每日免费刷新配额已耗尽**（非 MAA 刷新），OCR 会同时读到倒计时文字（如 `2小时36分钟`）。

该节点 Succeeded 后会点击（Click）——这是在「次数已满」状态下继续扫描购买剩余商品，**不等于成功刷新了一次商品列表**。

#### 5b. 稳健刷新（`CreditShoppingPrudentRefresh`）

在 `maafw*.log` 中搜索：

```text
Node.Recognition.Starting.*CreditShoppingPrudentRefresh
```

**只有**找到该 `Recognition.Starting` 记录，才说明稳健刷新节点被真正进入识别。仅出现在 `parse_node`/`NextList` 中不算触发。

### 6. 信用点数值追踪

在 `go-service.log` 中搜索：

```text
ExpressionRecognition.*CreditShoppingReserveCreditOCRInternal
```

每条记录包含：

```json
{
    "expression": "{CreditShoppingReserveCreditOCRInternal}>=300",
    "resolved_expression": "850>=300",
    "values": {"CreditShoppingReserveCreditOCRInternal": 850},
    "matched": true
}
```

将这些时间戳与 `maafw.log` 的购买事件对齐，即可还原信用点时间线。

> **注意**：数值有时因 OCR 时机（购买确认动画中）出现非预期跳变，需结合上下文解读，不要孤立解释单个数值。

## 输出模板

````markdown
## CreditShoppingMain 概要

- task_id: `...`
- 起止时间: `...`
- 结束原因: 自然完成 / 被停止

## 前端配置

| 功能            | 状态                        |
| --------------- | --------------------------- |
| Priority 1 购买 | 启用 / 关闭                 |
| Priority 2 购买 | 启用 / 关闭                 |
| 稳健刷新        | 启用 / **关闭（主动禁用）** |
| RefreshItem     | 启用 / 关闭                 |

## 实际购买

| #   | 时间 | 商品         | 折扣 | 购买路径                     |
| --- | ---- | ------------ | ---- | ---------------------------- |
| 1   | ...  | 武器检查单元 | 由日志填写 | Priority 2 扫描命中          |
| 2   | ...  | 协议棱柱组   | 由日志填写 | RefreshCountReached 后续购买 |

## 货架快照

### 首次进入

时间：`...`

```text
上排: [...]
下排: [...]
```

### 第 1 次刷新后

刷新点击：`...`
刷新后扫描：`...`

```text
上排: [...]
下排: [...]
```

### 第 2 次刷新后

刷新点击：`...`
刷新后扫描：`...`

```text
上排: [...]
下排: [...]
```

> 若有更多刷新，继续按相同格式追加。
> 若中途发生购买且用户关心“缺格/漏识别”，补一节“购买后、下次刷新前”的货架，并标出空位位置。
> 若某次刷新后只得到部分槽位/部分名字，不要把它直接写成完整货架；应单列为“异常帧”，并继续查找下一帧完整 10 物品货架。

## 折扣全览（首次扫描时商店）

| 槽位 x | 折扣       | 是否购买         |
| ------ | ---------- | ---------------- |
| x=...  | 由日志填写 | ✅/❌ 由日志填写 |
| x=...  | 由日志填写 | ✅/❌ 由日志填写 |
| x=...  | 由日志填写 | ✅/❌ 由日志填写 |
| x=...  | 由日志填写 | ✅/❌ 由日志填写 |

## 刷新状态

- 每日刷新配额：**已用尽**（OCR: 「今日刷新次数已用尽」+ 倒计时）
- 实际刷新次数：**0 次**
- 稳健刷新：**未触发**（原因: `CreditShoppingPrudentRefresh` enabled: false）

## 信用点时间线

| 时间     | 信用点读数 | 事件                              |
| -------- | ---------- | --------------------------------- |
| 01:22:57 | 850        | 任务开始，储备门控 ≥300 通过      |
| 01:23:06 | 528        | 购买①后                           |
| 01:23:16 | 758 ⚠️     | OCR 疑似误读（购买②后数值应偏低） |

````

## 约束（Guardrails）

- 仅分析 `CreditShoppingMain`，不混入其他任务的购买列表。
- 稳健刷新未触发时，必须区分「被禁用（enabled: false）」与「条件不满足」两种原因。
- `CreditShoppingRefreshCountReached` Succeeded **不等于**执行了一次商品刷新。
- 只有 `Recognition.Starting` 出现在 `CreditShoppingPrudentRefresh` 节点时，才能确认稳健刷新真正进入识别。
- 当用户询问“某次刷新后有什么”“玉有没有出现”“哪一格缺了”时，必须按名字 ROI 还原二维货架，至少覆盖首次进入和每次成功刷新后的货架。
- 货架可视化优先使用 `BuyFirstOCR` / `Priority2OCR` 的 `all_results_`；不能只给商品集合，必须保留排位信息（第几排第几个）。
- “完整刷新货架”和“购买后中间快照”必须分开判断：前者默认 10 个带名字物品，后者允许因买空出现正常 9 格。
- 少于 10 个或存在无名槽位时，默认视为异常中间态；但若能证明缺口正好是刚买空的商品位置，则应判定为正常购买后快照，而不是异常。
- 查找某次刷新后的完整货架时，不得跨过下一次 `RefreshItem` 的 `Node.Action.Succeeded`；否则容易把下一轮货架错记到本轮。
- ADB 场景允许按“上半一次 + 下半一次”合并货架，但合并范围必须限制在同一轮刷新内；合并后仍必须达到 **10 个有名字的物品**，否则仍记为不完整货架。
- 判断“缺少的物品”时，必须结合 `CreditIcon`、`NotSoldOut`、名字 OCR 三类证据，区分“买空后的正常空位”和“识别漏了一件”。
- 折扣结论必须来自 `IsDiscountPriority2` OCR 的 `all_results_` 对比，而非猜测。
- 信用点数值若出现非预期跳变（如购买后反升），标注 ⚠️ 并说明可能原因，不要强行解释为"获得了信用"。
- 判断"没有购买"之前，必须确认目标 `task_id` 范围内不存在任何 `CreditShoppingBuyItemOCR_.*Succeeded` + `购买成功` 组合。
