---
name: autostockstaple-log-analysis
description: 仅分析 `AutoStockStapleMain` 的 MaaEnd 日志。用于还原该任务实际购买了什么、购买所对应的证据、逐步剩余账单（券/账单）数值时间线，以及在 pipeline 或 go-service 中应当加日志/埋点的位置。适用于用户询问 `AutoStockStapleMain`、`AutoStockStaple`、武陵/四号谷地的稳定需求物资购买，或该任务内账单数值变化原因等场景。
---

# AutoStockStapleMain 日志分析

该 skill 仅用于 `AutoStockStapleMain`。

不要复用该流程到其他任务，例如 `AutoStockpileMain`、信用购物（credit shopping）、出售（selling），或通用 issue 故障排查。

## 适用范围

当用户提出下面这类问题时使用本 skill：

- "`AutoStockStapleMain` 都买了什么"
- "每次剩余的调度券是多少"
- "武陵/四号谷地稳定需求物资买了哪些"
- "这两个 `AutoStockStaple` 节点适不适合记录日志"
- "为什么 `AutoStockStapleMain` 没买/停止买了"

本 skill 重点包括：

- 购买重建：`AutoStockStapleMain` 实际买了什么
- 剩余账单时间线：逐步还原可见的剩余账单（券/账单）变化
- 跨场景账单解读：`ValleyIV` 与 `Wuling` 之间切换时如何解释账单数值
- 把日志证据映射回 `AutoStockStaple` 的 pipeline 节点
- 指出最佳加日志/埋点位置（instrumentation points）

## 主要证据

优先从目标日志目录读取：

1. `go-service.log`
2. `maafw.log`
3. 匹配的 `maafw.bak.*.log`
4. `mxu-tauri.log`
5. 当需要运行时 override 时读取 `mxu-web-YYYY-MM-DD.log`

代码上下文（用于理解任务语义与节点含义）：

- `assets/resource/pipeline/AutoStockStaple/ValleyIV.json`
- `assets/resource/pipeline/AutoStockStaple/Wuling.json`
- `assets/resource/pipeline/AutoStockStaple/General/Item.json`
- `docs/en_us/developers/custom.md`

## 工作流

### 1. 锁定任务实例

先定位精确的 `AutoStockStapleMain` 任务实例。

搜索：

- `AutoStockStapleMain`
- `task_id`
- `Tasker.Task.Starting`
- `Tasker.Task.Succeeded`
- `task end: [cb_detail={"entry":"AutoStockStapleMain"...`

注意：

- `maafw.log` 可能发生了轮转（rotated）。
- 若目标时间点在 `maafw.log` 中缺失，改去检查 `maafw.bak.*.log`。
- 不要把其他任务的 `task_id` 混入本次分析。

### 2. 还原实际购买

购买真相不能只看商品 OCR 候选，也不能只看进入买入任务。

`AutoStockBuyItemValleyIVTask` / `AutoStockBuyItemWulingTask` 只表示“识别到候选商品并进入是否需要购买的判定流程”，不直接等价于“实际已购买”。

先做强制检查：

- 先按目标 `task_id` 缩小范围，只跟踪本次 `AutoStockStapleMain` 的事件。
- 优先在已经命中的那一个 `maafw*.log` 文件内继续向后追踪，不要只看命中的第一小段上下文。
- 如果搜索结果发生截断、分页，或只返回部分命中，不能直接下“没有购买”的结论，必须继续分页或缩小到命中的 `maafw*.log` 文件重搜。

在 `maafw*.log` 中搜索：

- `Node.Action.Succeeded.*AutoStockBuyItemValleyIVTask`
- `Node.Action.Succeeded.*AutoStockBuyItemWulingTask`
- `AutoStockStapleQuantityControl`
- `AutoStockStapleQuantityControl.*Buy`
- `AutoStockStapleQuantityControl.*Exclude`
- `AutoStockStapleQuantityControlConfirmBuy`
- `AutoStockStapleQuantityControlAction`

每次成功点击里都会包含 `box=[x,y,w,h]`。

随后在附近查找 `AutoStockInStapleItemName` 的 OCR 结果，并把“点击框（box）”与“OCR 的商品框（box）”做一一对应匹配。

这样才能得到“候选商品名称”。

不要把所有 OCR 候选都当成“已购买”。

- `AutoStockBuyItem(ValleyIV|Wuling)Task` 动作成功，只能说明“进入候选商品的数量控制流程”。
- 必须继续在同一 `task_id` 下向后追踪命中的具体物品子节点，例如 `AutoStockStapleQuantityControlEchoingRemedy`。
- 仅当对应的 `AutoStockStapleQuantityControl<Item>Buy` 分支识别成功，并继续走到其后的 `AutoStockStapleQuantityControlAction` / `AutoStockStapleQuantityControlConfirmBuy` 成功路径时，才把该商品标记为“实际已购买”。
- 如果 `AutoStockStapleQuantityControl<Item>Buy` 未命中，而 `AutoStockStapleQuantityControl<Item>Exclude` 成功，则应标记为“识别到该商品，但判定为当前不需要购买”，不能记入购买列表。
- 如果只看到 `AutoStockStapleQuantityControl<Item>` 命中，但后续 `Buy` / `Exclude` 分支未查全，结论必须保持为“证据不足，需继续向后追踪”。

#### 负结论保护规则

只有满足以下全部条件，才能得出“本次没有购买”的结论：

- 对目标 `task_id` 所在的全部相关 `maafw*.log` 完成检索。
- 未发现任何实际走通 `AutoStockStapleQuantityControl<Item>Buy` 并完成后续购买确认的证据。

如果已经发现 `AutoStockBuyItem...Task` 的 `Node.Recognition.Starting`，
则必须继续检查同文件后续日志，直到确认以下至少一种结果：

- `Node.Action.Succeeded`
- `Node.Action.Failed`
- 任务结束
- 明确切换出该节点并进入其他分支

如果已经发现 `AutoStockStapleQuantityControl<Item>` 或其 `Buy` / `Exclude` 分支的 `Node.Recognition.Starting`，
则同样必须继续向后检查，直到确认以下至少一种结果：

- `AutoStockStapleQuantityControl<Item>Buy` 成功并继续进入购买确认路径
- `AutoStockStapleQuantityControl<Item>Exclude` 成功
- `Node.Action.Failed`
- 任务结束
- 明确切换出该节点并进入其他分支

禁止仅依据以下局部证据直接下结论“没有购买”：

- `Node.Recognition.Starting`
- 某一次 `Node.Recognition.Failed`
- 单个被截断的搜索片段

### 3. 还原剩余账单并建立时间线

逐步的剩余账单使用：

- `AutoStockCurrentStockBill`
- `CurrentStockBillText`

典型信号：

- `OCRer.cpp ... CurrentStockBillText ... "text":"4153万"`

解读规则：

- 这是该次识别时屏幕上可见的剩余账单数值。
- 如果某次购买点击发生在该次识别之后，把该值视作“购买前的剩余账单”。
- 如果购买之后出现了下一次 `CurrentStockBillText` OCR，把下一次数值视作“上一次购买后的最新可见剩余账单”。
- 必须构建完整时间线，而不只是购买列表。
- 时间线里需要包含：任务开始、场景切换、商品点击、账单 OCR 点位、以及任务结束。

#### 跨场景账单解读规则

`AutoStockStapleMain` 可能在 `ValleyIV` 和 `Wuling` 之间切换。

当任务发生场景切换时，不要假设“账单数值口径与上一场景完全可直接比较、可当作同一账本连续相减”。

默认解释：

- `ValleyIV` 与 `Wuling` 可能使用不同的账单类型/券种口径。
- 若大额账单变化恰好发生在场景切换附近，优先解释为“场景切换导致账单类型口径变化”，而不是“某一个商品消耗了异常巨大数量”。
- 除非日志明确证明“购买的数量与单价”等信息，否则不要把多百万的差值直接归因到某一单购买。

当出现这类情况时，需要在结论中明确写出：

- 大额跳变来自“从某一场景账单口径切换到另一场景账单口径”
- 因此跨场景的数值不应被当作同一种券的连续流水账本来做简单加减推导

优先使用时间线表：

| 时间 | 场景 | 已购买商品 | 购买前可见剩余账单 | 下一次可见剩余账单（购买后） |
| ---- | ---- | ---------- | ------------------ | ---------------------------- |

如果最后一次购买之后没有更晚的 OCR 识别结果，需明确说明“最终购买后账单不可用/无法从当前证据得到”。

同时提供事件时间线：

| 时间 | 事件                                              | 证据 |
| ---- | ------------------------------------------------- | ---- |
| ...  | 进入四号谷地 / 买了某商品 / 切换到武陵 / 任务结束 | ...  |

### 4. 区分 AutoStockStaple 与 AutoStockpile

`AutoStockpileMain` 是另一套任务。

不要把 `AutoStockpileMain` 的内容合并进 `AutoStockStapleMain` 的最终购买结果。

快速区分：

- `AutoStockpileMain`：`go-service` 中 autostockpile 的货物礼包/套装选择流程
- `AutoStockStapleMain`：由 pipeline 节点（如 `AutoStockBuyItemValleyIVTask`、`AutoStockBuyItemWulingTask`）驱动的稳定需求物资购买流程

如果用户询问的是 `AutoStockStapleMain` 前后某段时间范围，你可以提到相邻出现的 `AutoStockpileMain` 活动，但必须把它与 staple 购买列表分开。

### 5. 把证据映射回 pipeline

用这些节点来解释行为：

#### 入口与分支选择

- `AutoStockInStapleValleyIV`
- `AutoStockInStapleWuling`

它们决定本地搜索循环走向：

- 无法继续买分支
- 买入商品分支
- 售罄分支
- 滑动分支

#### 买入分支

- `AutoStockBuyItemValleyIVTask`
- `AutoStockBuyItemWulingTask`

它们是“识别到候选商品并进入后续购买判定”时的最佳 pipeline 节点。

原因：

- 它们同时满足：
    - `AutoStockInStapleItem`
    - `AutoStockInStapleItemDiscounts`
    - `AutoStockInStapleItemName`
    - `AutoStockTargetCanBuy`
- `box_index` 指向商品名 OCR 的结果
- 识别成功之后立刻发生点击
- 它们的 `next` 会进入 `AutoStockStapleQuantityControl`，因此这里只能确定“候选商品是谁”，不能单独作为“已购买”的最终证据

#### 数量控制分支

- `AutoStockStapleQuantityControl`
- `AutoStockStapleQuantityControl<Item>`
- `AutoStockStapleQuantityControl<Item>Buy`
- `AutoStockStapleQuantityControl<Item>Exclude`
- `AutoStockStapleQuantityControlConfirmBuy`

它们决定“是否真的需要买”以及“后续走买入还是排除”。

解读规则：

- `AutoStockStapleQuantityControl<Item>` 命中：说明当前候选商品已被映射到具体物品规则。
- `AutoStockStapleQuantityControl<Item>Buy` 命中：说明该商品通过数量阈值判定，允许继续购买。
- `AutoStockStapleQuantityControl<Item>Exclude` 命中：说明该商品被判定为无需购买，应从候选集中排除。
- 只有 `Buy` 分支继续走到其后的购买动作/确认路径，才算实际购买。

#### 停止买入分支

- `AutoStockTargetCanNotBuyValleyIV`
- `AutoStockTargetCanNotBuyWuling`

它们是“账单低于阈值因此无法继续购买”的最佳节点。

#### 剩余账单识别节点

- `AutoStockCurrentStockBill`
- `CurrentStockBillText`

它们是“实际剩余账单数值”的最佳证据来源。

### 6. 加日志/埋点建议

当用户询问“该在哪里加日志/埋点”时，建议：

#### 记录“识别到的候选商品”

最佳位置：

- 买入任务识别成功路径
- 重点在 `AutoStockBuyItemValleyIVTask` / `AutoStockBuyItemWulingTask` 附近

为什么：

- 商品名已经被解析出来
- 点击目标是固定对应的所选商品
- 但这一步之后还会进入“是否需要购买”的数量控制判定

建议 payload：

- 区域（region）
- 商品名
- 商品框（item box）
- 当前可见剩余账单
- 可选：`task_id`

#### 记录“为什么停止购买”

最佳位置：

- `AutoStockTargetCanNotBuyValleyIV`
- `AutoStockTargetCanNotBuyWuling`

建议 payload：

- 区域（region）
- 当前可见剩余账单
- 阈值或对比表达式
- 停止原因（stop reason）

#### 记录“为什么这件商品没买”

最佳位置：

- `AutoStockStapleQuantityControl<Item>Buy`
- `AutoStockStapleQuantityControl<Item>Exclude`

建议 payload：

- 区域（region）
- 商品名
- 当前识别到的持有数量
- 阈值
- 分支结果：`buy` / `exclude`

如果要记录“实际完成购买”的最终证据，优先放在 `AutoStockStapleQuantityControl<Item>Buy` 后续的确认节点上。

#### 记录“每次购买后的剩余账单”

最佳位置：

- 确认买入结果节点之后，再做一次新的 `AutoStockCurrentStockBill` 识别

如果只能选用其中一个节点：

- 用买入任务节点记录“即将购买”的日志
- 用停止买入节点记录“停止原因”的日志

不要声称“仅通过停止买入节点就能直接得出实际购买了哪个商品”。

### 7. go-service 线索

`go-service.log` 仍然能提供上下文：

- `AttachToExpectedRegexAction` 展示当时把哪些商品白名单挂载到了 `AutoStockInStapleItemName`
- 它也能确认任务入口与运行时 override 的设置

但“购买真相”仍应以 `maafw*.log` 的点击结果 + OCR box 对应匹配为准。

## 常见模式

### 模式：OCR 候选多于实际购买

症状：

- `AutoStockInStapleItemName` 展示了多个允许的商品
- 但后续只有一次 `AutoStockBuyItem...Task` 的成功点击

结论：

- 只有“点击框（box）与 OCR 中商品框一致”的那一个商品才算真正被购买

### 模式：命中 `AutoStockBuyItem...Task` 但最终没有购买

症状：

- 已经看到 `AutoStockBuyItemValleyIVTask` 或 `AutoStockBuyItemWulingTask` 的点击成功
- 随后进入 `AutoStockStapleQuantityControl<Item>`
- 但 `AutoStockStapleQuantityControl<Item>Buy` 未命中
- 同时 `AutoStockStapleQuantityControl<Item>Exclude` 成功

结论：

- 这表示“识别到了该商品，并进入了是否需要购买的判定流程”
- 但数量控制认为当前不需要购买
- 该商品应写入“识别到但未购买”或“被排除”的说明，不能写入“实际购买顺序”

### 模式：账单在一次购买前出现两次且数值相同

症状：

- 在一次购买发生之前，出现两次相同文本的 `CurrentStockBillText`

结论：

- 把它们视作同一个“购买前可见账单”，不要当作两段不同账单状态

### 模式：场景切换附近出现大额账单跳变

症状：

- 大额账单变化出现在“最后一次可见 `ValleyIV` 账单”和“首次可见 `Wuling` 账单”之间，或反过来

结论：

- 先检查任务是否从 `AutoStockInStapleValleyIV` 切换到 `AutoStockInStapleWuling`（或反向）
- 若确有切换，说明账单类型口径很可能随场景改变
- 不要把它描述为“某一单购买消耗了完整差额”

### 模式：最后一次购买之后没有后续账单 OCR

结论：

- 报告最后一次“购买前可见账单”
- 明确标注“最终购买后账单”由于缺少后续 OCR 证据无法获得

### 模式：任务跨轮转日志文件

症状：

- task 的开始出现在某个 `maafw.bak.*.log`
- 但 `maafw.log` 或后续某个 backup 只包含解析/初始化内容

结论：

- 继续跟踪“包含匹配 task_id 的那个日志文件”
- 不要仅根据文件名里的时间戳去切换文件

### 模式：只看到 `Recognition.Starting` 就误判未购买

症状：

- 已经看到 `AutoStockBuyItemValleyIVTask` 或 `AutoStockBuyItemWulingTask` 的 `Node.Recognition.Starting`
- 但没有继续检查同一文件后续是否出现 `Node.Action.Succeeded` / `Node.Action.Failed`

结论：

- `Node.Recognition.Starting` 只说明进入了买入判定，不代表最终没有购买
- 必须以后续的 `Node.Action.Succeeded` / `Node.Action.Failed` 为准
- 若搜索输出被截断，先缩小到命中的 `maafw*.log` 文件，再继续向后核对

## 输出模板

使用下面结构进行回答：

```markdown
## AutoStockStapleMain 概要

- task_id: `...`
- 起止时间: `...`
- 结果: 成功 / 失败

## 实际购买顺序

1. `时间` - `区域` - `商品名`
2. `时间` - `区域` - `商品名`

## 识别到但未购买（如有）

- `时间` - `区域` - `商品名` - `原因：数量控制走了 Exclude / Buy 分支未通过`

## 事件时间线

| 时间 | 事件                                              | 说明 |
| ---- | ------------------------------------------------- | ---- |
| ...  | 进入四号谷地 / 购买某商品 / 切换到武陵 / 任务结束 | ...  |

## 调度券时间线

| 时间 | 商品 | 购买前剩余调度券 | 购买后下一次可见调度券 |
| ---- | ---- | ---------------- | ---------------------- |
| ...  | ...  | ...              | ...                    |

说明:

- 同场景内可近似按连续账本理解
- 跨 `ValleyIV` / `Wuling` 场景时，若出现大幅变化，优先解释为券种切换而不是单次异常大额消耗

## 关键依据

- `maafw*.log`: `AutoStockBuyItem...Task` 点击成功 + `AutoStockInStapleItemName` OCR box 对应（用于锁定候选商品）
- `maafw*.log`: `AutoStockStapleQuantityControl<Item>Buy` / `Exclude` / `ConfirmBuy`（用于判断是否真的购买）
- `maafw*.log`: `CurrentStockBillText` OCR
- `go-service.log`: 运行时 override / 任务上下文

## 适合加日志的节点

- 记录购买项: `AutoStockBuyItemValleyIVTask` / `AutoStockBuyItemWulingTask`
- 记录停止原因: `AutoStockTargetCanNotBuyValleyIV` / `AutoStockTargetCanNotBuyWuling`
- 记录剩余调度券: `AutoStockCurrentStockBill`
```

## 约束（Guardrails）

- 仅分析 `AutoStockStapleMain`。
- 不要把 `AutoStockpileMain` 合并进最终购买列表。
- 不要把 `AutoStockBuyItem...Task` 的成功点击直接当作“购买结果”；它只代表进入了是否需要购买的后续判定。
- 只有在匹配到成功的买入点击，且后续购买判定链路也支持“确实买了”，才把该 OCR 候选当作“购买结果”。
- 在下结论“没有购买”之前，必须先确认目标 `task_id` 范围内不存在任何真正走通的购买确认路径；不能只看 `AutoStockBuyItem...Task` 有没有命中。
- 如果已经看到 `AutoStockBuyItem...Task` 的 `Node.Recognition.Starting`，必须继续向后核对到 `Node.Action.Succeeded`、`Node.Action.Failed`、任务结束或明确切分支，不能中途停止。
- 如果已经看到 `AutoStockStapleQuantityControl<Item>` 或其 `Buy` / `Exclude` 分支开始，也必须继续向后核对到购买确认、排除成功、失败、任务结束或明确切分支，不能中途停止。
- 如果搜索结果被截断、分页，或日志发生轮转，不能直接给出负结论，必须继续分页或缩小到命中的相关 `maafw*.log` 文件重查。
- 不要在缺少后续 OCR 证据的情况下推断“最终购买后账单”。
- 除非日志明确证明同一券种/同一账单口径，否则不要把跨场景账单变化当作同一种货币的连续加减关系来解释。
- 当引用 pipeline 节点时，必须使用仓库中真实的节点名（原样一致）。
