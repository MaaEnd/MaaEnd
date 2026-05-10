---
name: credit-shopping-log-analysis
description: 分析 MaaEnd `CreditShoppingMain` 的日志。用于还原信用购物任务中实际购买了什么商品、每件商品的折扣力度、是否触发过刷新（或刷新次数已用尽）、稳健刷新是否触发、以及信用点的消耗状况。货架槽位必须以日志中的 `CreditIcon`（TemplateMatcher）为锚，有无 `BuyFirstOCR`/`Priority2OCR` 不影响槽位级货架的还原与呈现。适用于用户询问信用点交易、信用购物买了什么、折扣情况、刷新配置、`CreditShoppingMain` 任务行为等场景。
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
- "货架上有什么 / 缺哪一格 / 某时刻货架什么样"（槽位以 `CreditIcon` 为准）

## 主要证据来源

按优先级读取：

1. `maafw.log`（最新会话）
2. `maafw.bak.*.log`（若任务发生在之前的会话）
3. **`CreditIcon`（`TemplateMatcher`）**：货架**槽位与排位**的锚；每次出现即对应一份槽位级货架（ADB 可能为半份后再合并）。
4. `go-service.log`（信用点 OCR 数值、表达式求值）
5. `mxu-web-YYYY-MM-DD.log`（前端下发的 pipelineOverride，含开关配置）

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

### 3b. 还原每次货架（以 `CreditIcon` 为锚；名字为可选叠加）

当用户询问“第一次刷新出了什么”“买完某件后货架怎么变了”“有没有缺格/漏识别”时，必须补做本节。**货架槽位不以 `BuyFirstOCR` / `Priority2OCR` 是否运行为前提。**

#### 货架的判定来源（权威顺序）

1. **槽位与排位（必须）**：同一 `task_id` 下，只要在日志中出现 **`CreditIcon`** 的 `TemplateMatch`（搜索 `TemplateMatcher.*CreditIcon` 或带 `"name":"CreditIcon"` 的识别结果），即视为该次扫描有一份**货架骨架**。
   - **非 ADB**：同一帧/同一心跳内通常为**一整份**槽位（`all_results_` 中每个 box 为一格）。
   - **ADB**：可能仅为**半份**（例如先上半再下半）；须标注「半份」，并按下文 ADB 规则与相邻采集合并后再写「满架」结论。
2. **商品名（可选叠加）**：若**同一扫描时刻**存在 `BuyFirstOCR` / `Priority2OCR` 的 `all_results_`，可将其中 `"text"` 按列对齐叠到槽位上。
3. **禁止臆造名字**：若某帧**没有**名字 OCR 的 `all_results_`（或 `all_results_` 为空），**不得**用上一轮/下一轮的名称、也不得凭推测填写商品名；必须明确写 **「本帧无商品名 OCR」** 或 **「仅槽位」**。

#### 槽位还原步骤（有 `CreditIcon` 就必须输出）

在 `maafw*.log` 中搜索：

```log
TemplateMatcher.*CreditIcon
```

取该次 **`CreditIcon`** 的 `all_results_`（若流水线实际使用 `filtered_results_`，以日志为准并在分析中说明二者差异）。

1. 按 `box` 的 **y** 分成两排：`y≈240` 一带为上排，`y≈440～486` 一带为下排（以相对聚集为准，勿机械抠死一个像素）。
2. 每排内按 **x 从小到大** 排序，得到从左到右的列（第 1 列…）。
3. **输出最低要求**：至少写出 **上排 m 格、下排 n 格**（或每格 x / 列序号），使排位可追溯；**即使本轮没有任何商品名 OCR** 也要写。
4. 用 `NotSoldOut` 等（若日志中有）辅助判断未卖空槽位；格数少于 10 时如实记录；若为买空后 9 格且缺列与刚购商品列一致，注明「买空后正常缺格」。

#### 商品名叠加（若存在）

当同一次扫描中存在名字 OCR 时，搜索：

```log
OCRer.*BuyFirstOCR
OCRer.*Priority2OCR
```

将 `all_results_` 中各条的 `box` 与 `CreditIcon` 列 **按 x/y 对齐** 后填入名称列。**不得**在日志无 `text` 证据时填充具体商品名。

其它辅助节点（可按需引用）：

```log
ColorMatcher.*NotSoldOut
ColorMatcher.*BuyFirstOCRLabelColor
ColorMatcher.*BuyFirstOCRTextColor
```

#### 异常情况：刷新后只识别到部分货架

有时日志里会出现类似以下情况：

- `CreditIcon` 候选存在，但 `filtered_results_` 只剩 1~9 个
- `NotSoldOut` 只剩 1~9 个槽位
- 没有跑出 `BuyFirstOCR` / `Priority2OCR` 名字列表，或列表为空
- 或者只识别出上排、下排中的一部分

**仍须先根据 `CreditIcon`（及可用的半份/合并规则）写出槽位级货架**；其中「无名字」不等于「无货架」。

若槽位数明显异常（例如远少于 9 且无法用买空解释）、或动画中间帧导致 `CreditIcon` 不稳定，该帧可标为**异常中间态 / 不稳定货架**，**不能**单独当作“本次刷新后的最终完整商店”，应继续在同一轮刷新窗口内找下一帧 `CreditIcon` 佐证。

#### 区分两类表述（避免把「无名」当成「无货架」）

1. **槽位货架（必有，只要跑了 `CreditIcon`）**
   - 用 `CreditIcon` 的格数与排位描述；**不要求**名字 OCR。
2. **命名货架（可选）**
   - 在槽位货架基础上，若存在 `BuyFirstOCR` / `Priority2OCR` 的 `text`，再填商品名。
   - **完整命名货架**（用于回答“架上分别是什么商品”）：宜满足 **10 个槽位 + 10 个可对齐的名字**；若名字缺失，应写「若干列无名」，**不得编造**。
3. **完整刷新后的槽位快照**
   - 指“首次进入”或“某次 `RefreshItem` 成功后”用于描述本轮内容的帧：默认 **10 个 `CreditIcon` 槽位**（命名与否另计）。
4. **购买后中间快照**
   - 允许 **9 个** `CreditIcon` 槽位；若能对齐刚购列，注明「买空后正常缺格」。

处理规则：

1. **不得**因本轮未运行 `BuyFirstOCR`/`Priority2OCR` 而跳过货架小节；有 `CreditIcon` 就必须给出槽位表。
2. **购买后中间快照**允许 9 槽位，须说明缺列与购买列的关系（若日志可对齐）。
3. 若仅有 **部分槽位**（例如远少于 9）且不满足买空解释 → 标为 **异常帧**，并在同一轮刷新内向后找补充 `CreditIcon` 帧。
4. 查找「刷新后完整 10 槽位」时，**不得跨过**下一次 `RefreshItem` 的 `Node.Action.Succeeded`。
5. 若直到下一次刷新点击仍无法得到稳定 10 槽位 `CreditIcon`，如实说明；若仅有槽位而无名字，同样如实说明，**不要用猜测的商品名补全**。

#### ADB 特例：分两次识别上下半货架

部分 ADB 场景下，日志/截图可能无法在同一帧内拿到完整两排：

- 第一次识别只覆盖上半部分
- 滑动后才出现下半部分

这类情况允许按**两次采集后合并**来还原单次货架：

1. **槽位**：合并后宜达到 **10 个 `CreditIcon` 槽位**（半份须标注，合并后再计数）。
2. **名字**：若两次采集均无名字 OCR，合并结果仍为**有效槽位货架**，名称列填「未识别」；**不得编造**。若仅部分采集有名字，只填写有日志证据的列。
3. 必须说明「该货架由上下半（或两次心跳）合并得到」。
4. 合并范围必须限制在**同一轮刷新、且下一次 `RefreshItem` 点击之前**。
5. 合并后仍不足 10 槽位 → **不完整货架**，须标注并继续在同一刷新窗口内找下一帧佐证。

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

每一小节**至少**包含：`CreditIcon` 对应的槽位数与排位（上/下排、列序）；商品名仅在日志有 `BuyFirstOCR`/`Priority2OCR` 证据时列出，否则写「本帧无商品名 OCR」。

### 首次进入

时间：`...`（附 `CreditIcon` 日志时间戳）

```text
槽位（CreditIcon）: 上排 m 格 | 下排 n 格（列序：…）
商品名（若有 OCR）: 上排: [...] / 下排: [...]；若无则写「仅槽位」
```

### 第 1 次刷新后

刷新点击：`...`
刷新后扫描：`...`（**必须有该帧 `CreditIcon`**）

```text
（同上格式）
```

### 第 2 次刷新后

刷新点击：`...`
刷新后扫描：`...`

```text
（同上格式）
```

> 必须按每一次 `RefreshItem` 的 `Node.Action.Succeeded` 逐次追加货架小节；**不得**因本轮未运行名字 OCR 而省略「槽位货架」。
> 若中途发生购买且用户关心“缺格/漏识别”，补一节“购买后、下次刷新前”的货架（以 `CreditIcon` 格数变化为准），并标出空列。
> 若某次刷新后只有部分槽位或不稳定的 `CreditIcon`，单列为「异常帧」，并在同一刷新窗口内继续找下一帧佐证；**禁止**用猜测的商品名凑满格子。
> 若刷新后某一心跳仅有 `CreditIcon`、无 `BuyFirstOCR`/`Priority2OCR`，仍须给出该心跳的槽位表；不得写成「无法还原货架」除非连 `CreditIcon` 也不存在或日志截断。

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
- **货架排位必须以 `CreditIcon` 为准**（槽位级）；`BuyFirstOCR` / `Priority2OCR` 仅用于**叠加商品名**，不可替代槽位来源。
- 当用户询问“某次刷新后有什么”“玉有没有出现”“哪一格缺了”时：**先用 `CreditIcon` 列出槽位与列序**；若日志中有名字 OCR，再回答“有什么商品”；若无名字 OCR，明确写「仅有槽位、无名证据」，**禁止**用猜测补商品名。
- 每一次 `RefreshItem` 点击成功都必须在“货架快照”中单独列出一节；小节内须包含该刷新窗口内用于描述的 **`CreditIcon` 帧**（时间戳），即使本轮没有名字 OCR、没有命中购买。
- 刷新后若 **`CreditIcon` 不稳定或缺失**（动画中间帧等），可将该轮标为“不稳定帧”，并在同一刷新窗口内继续向后找下一帧 `CreditIcon`；不得用下一轮刷新的槽位冒充本轮。
- **不得**因本轮未运行 `BuyFirstOCR`/`Priority2OCR` 而省略货架小节——只要有 `CreditIcon`，就必须给出槽位表。
- “完整刷新后的槽位快照”默认 **10 个 `CreditIcon` 槽位**；“完整命名货架”另行要求 10 个可对齐的名字（缺失则如实写无名列）。
- “购买后中间快照”允许 **9 槽位** `CreditIcon`；若能对齐刚购列，判定为正常买空而非异常。
- 查找某次刷新后的完整槽位时，不得跨过下一次 `RefreshItem` 的 `Node.Action.Succeeded`。
- ADB 允许按“上半 + 下半”合并；合并后宜达到 **10 槽位 `CreditIcon`**；名字可为空或部分，**不得编造**。
- 判断“缺的是哪一格/是否漏识别”时，以 **`CreditIcon` 格数变化 + 列序** 为主，辅以 `NotSoldOut`、名字 OCR（若有）。
- 折扣结论必须来自 `IsDiscountPriority2` OCR 的 `all_results_` 对比，而非猜测。
- 信用点数值若出现非预期跳变（如购买后反升），标注 ⚠️ 并说明可能原因，不要强行解释为"获得了信用"。
- 判断"没有购买"之前，必须确认目标 `task_id` 范围内不存在任何 `CreditShoppingBuyItemOCR_.*Succeeded` + `购买成功` 组合。

### 防幻觉（禁止编造）

- **商品名**只能来自日志里出现的 OCR `text`（或购买详情 `CreditShoppingBuyItemOCR_*` 等明确字段），不得用常识、上一轮货架或用户口述代替。
- **槽位**只能来自日志里的 `CreditIcon`（及 ADB 合并规则）；不得仅凭“应该有 10 格”臆造格数。
- 区分三件事并分开写：**槽位货架**（必有若跑了 `CreditIcon`）、**命名叠加**（可有可无）、**折扣数字**（须单独引用 `IsDiscountPriority2`，勿与名称混为一谈）。

### 防止注意力丢失（执行本 Skill 时的自检清单）

1. 已锁定 `task_id`，后续 grep **全部带该 id**（或确认上下文无串任务）。
2. 还原货架时 **先搜 `CreditIcon`**，再搜 `BuyFirstOCR`/`Priority2OCR`；避免只 grep 名字节点导致“以为没有货架”。
3. 每一次 `RefreshItem` **Succeeded** 是否都对应一小节快照（含 `CreditIcon` 时间戳）。
4. 若用户点名某一时刻（如 `03:31:38`），检查该时刻附近是否有 **`CreditIcon`**：有则输出槽位表，无名字则标明「无名」；**不因无 BuyFirst 而留空**。
5. 输出前复读：是否写了任何日志里**未出现**的具体商品名？若有 → 删除或改为「未识别」。
