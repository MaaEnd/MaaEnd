---
name: credit-shopping-log-analysis
description: 分析 MaaEnd `CreditShoppingMain` 的日志。用于还原信用购物任务中实际购买了什么商品、每件商品的折扣力度、是否触发过刷新（或刷新次数已用尽）、稳健刷新是否触发、以及信用点的消耗状况。货架槽位以 `CreditShoppingRecordShelfSlot` 为锚；商品模板为 `CreditShoppingRecordItem*` / 购买链上的 `CreditShoppingPriority*Item*`；折扣 OCR 为 `CreditShoppingPriority*Discount`（及买不起链上的 `*DiscountCannotAfford`）。适用于用户询问信用点交易、信用购物买了什么、折扣情况、刷新配置、`CreditShoppingMain` 任务行为等场景。
---

# CreditShoppingMain 日志分析

该 Skill 仅用于 `CreditShoppingMain`。

不要将本流程复用到 `AutoStockStapleMain`、`AutoStockpileMain` 或通用 issue 故障排查。

## 节点速查（当前 Pipeline）

| 用途 | 日志中搜索的节点名（示例） |
| ---- | -------------------------- |
| 格位锚点（槽位骨架） | `CreditShoppingRecordShelfSlot` |
| 货架记录 / 快照用物品模板 | `CreditShoppingRecordItemArmsInspector` 等 `CreditShoppingRecordItem*` |
| 优先购买 N 物品模板 | `CreditShoppingPriority1Item*` / `Priority2Item*` / `Priority3Item*`（仅用户勾选并经 override 启用的子节点会命中） |
| 任意购买物品模板 | `CreditShoppingShelfAnyItem`（Or → 同上 Record 模板） |
| 未售罄 | `CreditShoppingPriority1NotSoldOut` 等、`CreditShoppingShelfAnyNotSoldOut` |
| 买得起 | `CreditShoppingPriority1CanAfford` 等、`CreditShoppingShelfAnyCanAfford` |
| 折扣 OCR（买得起链） | `CreditShoppingPriority1Discount`、`CreditShoppingPriority2Discount`、`CreditShoppingPriority3Discount` |
| 折扣 OCR（买不起链，补信用等） | `CreditShoppingPriority1DiscountCannotAfford` 等 |
| 快照折扣（go-service 写 JSON） | `RecordItemDiscount` |
| 货架快照 Custom | `CreditShoppingScanItemAction` → `go-service.log` 中 `record:` |

> **已删除、勿再 grep**：`CreditIcon`、`BuyFirstOCR`、`Priority2OCR`、`IsDiscountPriority*`、`BuyFirstOCRLabelColor` 等旧节点。分析**新日志**时必须用上表名称。

## 适用范围

当用户提出下列问题时使用本 Skill：

- "信用购物买了什么"
- "折扣力度如何"
- "有没有刷新商品"
- "稳健刷新触发了吗"
- "信用点消耗了多少"
- "为什么没买/没刷新"
- "货架上有什么 / 缺哪一格 / 某时刻货架什么样"（槽位以 `CreditShoppingRecordShelfSlot` 为准）

## 主要证据来源

按优先级读取：

1. `maafw.log`（最新会话）
2. `maafw.bak.*.log`（若任务发生在之前的会话）
3. **`CreditShoppingRecordShelfSlot`（`TemplateMatcher`）**：货架**槽位与排位**的锚；每次出现即对应一份槽位级货架（ADB / Win32 通常同一心跳内为一整份 10 格，排版可为上五下五或上七下三）。
4. **`CreditShoppingRecordItem*`**：快照扫描时 go-service 对全 catalog 跑模板（见 `record.json`）；日志里可出现多个 `TemplateMatcher.*CreditShoppingRecordItem*`。
5. **购买链**：命中 `CreditShoppingBuyPriorityN` / `BuyBlacklist` / `PrudentRefresh` 等时，同帧会有对应 `CreditShoppingPriorityNDiscount` 或 `CreditShoppingShelfAny*` 下游 OCR / ColorMatch。
6. `go-service.log`（信用点 OCR、表达式求值、`record: shelf captured`、快照读写）
7. 可选：`debug/record/CreditShoppingShelfSnapshots.json`（若用户打包了 output）
8. `mxu-web-YYYY-MM-DD.log`（前端下发的 pipelineOverride，含开关配置）

代码上下文（了解节点语义）：

- `assets/tasks/CreditShopping.json`
- `assets/resource/pipeline/CreditShopping/Item/ShelfChecks.json`
- `assets/resource/pipeline/CreditShopping/record.json`

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
"CreditShoppingBuyPriority2":   {"enabled": false/true},
"CreditShoppingBuyPriority3":   {"enabled": false/true}
```

这一步决定哪些功能在本次运行中被关闭，从而解释后续日志中"节点从未进入识别"的原因。

**常见结论**：

- `CreditShoppingPrudentRefresh: enabled: false` → 稳健刷新被**主动禁用**，不是条件不满足
- `RefreshItem: enabled: false` → 信用点刷新商品功能关闭，不会消耗信用点刷新

并确认用户启用了哪些 **Priority N 物品**（`CreditShoppingPriorityNItems` 的 case → 仅这些 `CreditShoppingPriorityNItem*` 会从占位变为 TemplateMatch）。

### 3. 还原折扣信息

折扣由 **`CreditShoppingPriorityNDiscount`**（及买不起侧的 `CreditShoppingPriorityNDiscountCannotAfford`）OCR 读取，`expected` 含 `75|95|99` 等（以 `ShelfChecks.json` 为准）。

在 `maafw*.log` 中按**实际启用的购买档**搜索，例如：

```log
OCRer.*CreditShoppingPriority1Discount
OCRer.*CreditShoppingPriority2Discount
OCRer.*CreditShoppingPriority3Discount
```

任意购买 / 稳健刷新 / 刷新用尽路径若走 Priority 链未命中，则同帧可能只有 **`CreditShoppingShelfAny*`** 链，折扣仍来自**当时参与 And 识别的那一档 `*Discount`**（看 `CreditShoppingBuyPriorityN` 或 `BuyBlacklist` 的 `all_of` 列表）。

每次扫描的 `all_results_` 包含可见折扣标签，格式如：

```log
{"box":[x,y,w,h],"score":...,"text":"-75%"}
```

**逐次对比法**：比较相邻两次扫描的 `all_results_`，消失的条目对应刚被购买的商品。

配合对应 **`CreditShoppingBuyPriorityN`**（或任意购买 Click 节点）命中时的 box（x 坐标）定位该列折扣。

> 该档 `*Discount` 的 `filtered_results_` 非空表示 OCR 命中且满足配置的 `expected` 阈值，并参与该档购买 And 识别。

快照任务在 go-service 内对每格跑 **`RecordItemDiscount`**，证据在 `go-service.log` 的识别明细或 `CreditShoppingShelfSnapshots.json` 的 `discount` 字段，与购物链上的 `CreditShoppingPriority*Discount` 相互印证即可。

### 3b. 还原每次货架（以 `CreditShoppingRecordShelfSlot` 为锚）

当用户询问“第一次刷新出了什么”“买完某件后货架怎么变了”“有没有缺格/漏识别”时，必须补做本节。

#### 货架的判定来源（权威顺序）

1. **槽位与排位（必须）**：同一 `task_id` 下，出现 **`CreditShoppingRecordShelfSlot`** 的 `TemplateMatch`（搜索 `TemplateMatcher.*CreditShoppingRecordShelfSlot` 或 `"name":"CreditShoppingRecordShelfSlot"`），即视为该次扫描有一份**货架骨架**。同一帧/同一心跳内通常为**一整份**槽位（`all_results_` 中每个 box 为一格）；ADB 与 Win32 仅 y 带分布不同（上五下五 vs 上七下三），**不要**跨帧合并两屏。
2. **商品身份（模板，非名字 OCR）**：同帧若存在 **`CreditShoppingRecordItem*`** 的 `TemplateMatch`，按 box 与槽位列对齐，可知该格是哪类商品（节点名后缀即 catalog ID，如 `CreditShoppingRecordItemOroberyl`）。未命中模板但槽位存在 → 记 **unknown / 未识别**，勿猜名。
3. **优先购买链上的物品框**：若分析“哪一档想买谁”，另搜已启用的 **`CreditShoppingPriorityNItem*`** TemplateMatch，仅覆盖用户勾选项。
4. **禁止臆造名字**：当前 Pipeline **无**货架商品名 OCR；不得用旧版 `BuyFirstOCR`/`Priority2OCR` 的 grep 方式。商品中文名来自模板节点 ↔ 对照表、IMS A3、或快照 JSON 的 `name`/`id` 字段。

#### 槽位还原步骤（有 `CreditShoppingRecordShelfSlot` 就必须输出）

在 `maafw*.log` 中搜索：

```log
TemplateMatcher.*CreditShoppingRecordShelfSlot
```

取该次 **`CreditShoppingRecordShelfSlot`** 的 `all_results_`（若实际以 `filtered_results_` 为准，在分析中说明）。

1. 按 `box` 的 **y** 分成两排：`y≈240` 一带为上排，`y≈440～486` 一带为下排（以相对聚集为准，勿机械抠死一个像素）。
2. 每排内按 **x 从小到大** 排序，得到从左到右的列（第 1 列…）。
3. **输出最低要求**：至少写出 **上排 m 格、下排 n 格**（或每格 x / 列序号），使排位可追溯。
4. 叠加 **`CreditShoppingRecordItem*`**（及需要的 `CreditShoppingPriorityNItem*`）到各列；无模板命中则该列写「未识别」。
5. 用 **`CreditShoppingPriority1NotSoldOut`** / `ShelfAnyNotSoldOut` 等（若日志中有）辅助判断未卖空；格数少于 10 时如实记录；买空后 9 格且缺列与刚购列一致 → 「买空后正常缺格」。

#### go-service 快照（若存在）

搜索 `go-service.log`：

```log
record: shelf captured
record: snapshot already exists, skip
record: no shelf slots captured, skip persist
```

并读取 `CreditShoppingShelfSnapshots.json`（`uid` + `game_date` + `refresh_index`）中的 `slots[]`（含 `unknown` 与 `discount`）。

#### 异常情况：刷新后只识别到部分货架

- `CreditShoppingRecordShelfSlot` 的 `filtered_results_` 只剩 1~9 个
- `*NotSoldOut` 只剩部分槽位
- 无任何 `CreditShoppingRecordItem*` 命中

**仍须先根据 `CreditShoppingRecordShelfSlot` 写出槽位表**；「无模板命中」不等于「无货架」。

若槽位数明显异常或动画中间帧导致锚点不稳定，标为**异常中间态**，在同一刷新窗口内找下一帧 `CreditShoppingRecordShelfSlot` 佐证。

#### 区分两类表述

1. **槽位货架（必有，只要跑了锚点）**：用 `CreditShoppingRecordShelfSlot` 格数与排位；**不要求**名字 OCR。
2. **命名货架（模板 / JSON / A3）**：在槽位基础上用 `CreditShoppingRecordItem*` 或快照 JSON / A3 填商品；缺失则「若干列未识别」，**不得编造**。
3. **完整刷新后的槽位快照**：默认 **10 个**锚点 box（命名另计）。
4. **购买后中间快照**：允许 **9** 个锚点 box。

处理规则：

1. **不得**因缺少旧名字 OCR 而跳过货架小节；有 `CreditShoppingRecordShelfSlot` 就必须给出槽位表。
2. 购买后 9 槽位须说明缺列与购买列关系（若可对齐）。
3. 部分槽位且非买空 → **异常帧**，同轮刷新窗口内向后找补充帧。
4. 查找「刷新后完整 10 槽位」时，**不得跨过**下一次 `RefreshItem` 的 `Node.Action.Succeeded`。
5. 无法得到稳定 10 槽位时如实说明；勿用猜测商品名补全。

### 4. 还原实际购买

购买事实以框架点击结果为准，不能仅看 OCR / 模板候选。弹窗内商品名 OCR（`BuyItemOCR_*`）已移除，播报改由 IMS A3 负责。

步骤：

1. 在 `maafw*.log` 中搜索本轮点击货架后的 `CreditShoppingBuyConfirm`（Action.Succeeded）
2. 确认后续 `CreditShoppingClaimConfirm` / `CreditShoppingBuySuccess`（含 `"购买成功"`）识别成功并关闭
3. 商品名优先从：同轮 **`CreditShoppingRecordItem*` / 启用的 `CreditShoppingPriorityNItem*`** 模板节点名、快照 JSON、`go-service` / Focus 中 A3「获得 xxx ×n」；**不得编造**

只有同时满足以下条件才算已购买：

- `CreditShoppingBuyConfirm` Action.Succeeded
- `CreditShoppingClaimConfirm`（或购买成功文案）识别并成功关闭奖励界面

常见货架商品名对照（模板节点后缀 / JSON `id` → 中文）：

| 内部名 / 节点后缀            | 商品名       |
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
| Priority 3 购买 | 启用 / 关闭                 |
| 稳健刷新        | 启用 / **关闭（主动禁用）** |
| RefreshItem     | 启用 / 关闭                 |

## 实际购买

| #   | 时间 | 商品         | 折扣       | 购买路径                     |
| --- | ---- | ------------ | ---------- | ---------------------------- |
| 1   | ...  | 武器检查单元 | 由日志填写 | Priority 2 扫描命中          |
| 2   | ...  | 协议棱柱组   | 由日志填写 | RefreshCountReached 后续购买 |

## 货架快照

每一小节**至少**包含：`CreditShoppingRecordShelfSlot` 槽位数与排位；商品列来自 `CreditShoppingRecordItem*` / 快照 JSON / A3，无则写「未识别」。

### 首次进入

时间：`...`（附 `CreditShoppingRecordShelfSlot` 日志时间戳）

```text
槽位: 上排 m 格 | 下排 n 格（列序：…）
商品（模板/JSON）: 上排: [...] / 下排: [...]；若无则写「未识别」
```

### 第 1 次刷新后

刷新点击：`...`
刷新后扫描：`...`（**必须有该帧 `CreditShoppingRecordShelfSlot`**）

```text
（同上格式）
```

### 第 2 次刷新后

刷新点击：`...`
刷新后扫描：`...`

```text
（同上格式）
```

> 必须按每一次 `RefreshItem` 的 `Node.Action.Succeeded` 逐次追加货架小节。
> 若中途发生购买，可补「购买后、下次刷新前」货架（以锚点格数变化为准）。
> 若锚点不稳定，标「异常帧」并在同一刷新窗口内找下一帧佐证；禁止用猜测商品名凑满格子。

## 折扣全览（首次扫描时商店）

| 槽位 x | 折扣       | 是否购买         |
| ------ | ---------- | ---------------- |
| x=...  | 由日志填写 | ✅/❌ 由日志填写 |

（折扣来自对应档 `CreditShoppingPriorityNDiscount` 的 `all_results_` 或快照 JSON。）

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
- **货架排位必须以 `CreditShoppingRecordShelfSlot` 为准**；商品身份来自 **`CreditShoppingRecordItem*`**（及启用的 **`CreditShoppingPriorityNItem*`**），**不是**已删除的名字 OCR 节点。
- 当用户询问“某次刷新后有什么”“玉有没有出现”“哪一格缺了”时：**先列锚点槽位**；再叠模板 / JSON / A3；无证据则「未识别」，**禁止**猜测。
- 每一次 `RefreshItem` 点击成功都应在“货架快照”中单独列出一节（含锚点时间戳），即使无模板命中、无购买。
- 刷新后锚点不稳定 → 同窗口找下一帧；不得用下一轮刷新冒充本轮。
- “完整刷新后的槽位快照”默认 **10 个**锚点 box。
- “购买后中间快照”允许 **9** 个锚点 box。
- 查找某次刷新后的完整槽位时，不得跨过下一次 `RefreshItem` 的 `Node.Action.Succeeded`。
- ADB 与 Win32 均按**单次** `CreditShoppingRecordShelfSlot` 描述；不足 10 格时在同窗口向后找下一帧，**不得**跨帧拼接。
- 判断缺格/漏识别：以 **锚点格数 + 列序** 为主，辅以 `*NotSoldOut`、模板命中。
- **折扣**须来自 **`CreditShoppingPriorityNDiscount`**（或 `*DiscountCannotAfford` / `RecordItemDiscount` / 快照 JSON），不得猜测。
- 信用点数值异常跳变标注 ⚠️。
- 判断"没有购买"之前，必须确认目标 `task_id` 内无 `CreditShoppingBuyConfirm` Action.Succeeded + 购买成功关闭组合。

### 防幻觉（禁止编造）

- **商品名**只能来自模板节点后缀、快照 JSON、`RecordItem*` 对齐结果、A3 Focus、等日志明确字段。
- **槽位**只能来自**单次** `CreditShoppingRecordShelfSlot` 的 `all_results_` / `filtered_results_`；不得跨帧合并臆造 10 格。
- 区分：**槽位货架**、**商品模板/JSON**、**折扣 OCR** 分开写，勿混为一谈。

### 防止注意力丢失（自检清单）

1. 已锁定 `task_id`，后续 grep 限定该 id。
2. 还原货架时 **先搜 `CreditShoppingRecordShelfSlot`**，再搜 **`CreditShoppingRecordItem*`**（及需要的 `CreditShoppingPriorityNItem*`）；**不要**再搜 `BuyFirstOCR` / `CreditIcon`。
3. 折扣 grep **`CreditShoppingPriorityNDiscount`**（按启用档），勿搜 `IsDiscountPriority2`。
4. 每一次 `RefreshItem` **Succeeded** 是否都有货架小节（含锚点时间戳）。
5. 用户点名时刻：检查附近是否有 **`CreditShoppingRecordShelfSlot`**；无模板则标「未识别」，**不因无旧 OCR 而留空**。
6. 输出前复读：是否写了日志里**未出现**的具体商品名？若有 → 删除或改为「未识别」。

### 历史日志（重构前）

若日志日期早于 Pipeline 重构，可能仍出现 `CreditIcon`、`BuyFirstOCR`、`IsDiscountPriority2` 等节点名；仅在对**旧包**做考古时使用旧 grep，并在结论中注明「旧 Pipeline」。**新日志一律用上文节点表。**
