---
name: autostockstaple-log-analysis
description: Analyze MaaEnd logs for AutoStockStapleMain only. Reconstruct what AutoStockStapleMain bought, the purchase order, per-step remaining stock bill values, and where to instrument logging in pipeline or go-service. Use when the user asks about AutoStockStapleMain, AutoStockStaple, stable-demand goods purchases, Wuling or ValleyIV staple logs, or stock bill changes during this task.
---

# AutoStockStapleMain Log Analysis

This skill is only for `AutoStockStapleMain`.

Do not reuse this workflow for other tasks such as `AutoStockpileMain`, credit shopping, selling, or generic issue triage.

## Scope

Use this skill when the user asks questions like:

- "`AutoStockStapleMain` 都买了什么"
- "每次剩余的调度券是多少"
- "武陵/四号谷地稳定需求物资买了哪些"
- "这两个 `AutoStockStaple` 节点适不适合记录日志"
- "为什么 `AutoStockStapleMain` 没买/停止买了"

This skill focuses on:

- purchase reconstruction for `AutoStockStapleMain`
- remaining stock bill timeline
- cross-scene stock bill interpretation
- matching log evidence back to `AutoStockStaple` pipeline nodes
- identifying the best instrumentation points

## Primary Evidence

Read these files first from the target log directory:

1. `go-service.log`
2. `maafw.log`
3. matching `maafw.bak.*.log`
4. `mxu-tauri.log`
5. `mxu-web-YYYY-MM-DD.log` when you need runtime overrides

For code context, inspect:

- `assets/resource/pipeline/AutoStockStaple/ValleyIV.json`
- `assets/resource/pipeline/AutoStockStaple/Wuling.json`
- `assets/resource/pipeline/AutoStockStaple/General/Item.json`
- `docs/en_us/developers/custom.md`

## Workflow

### 1. Lock the task instance

Find the exact `AutoStockStapleMain` task instance first.

Search:

- `AutoStockStapleMain`
- `task_id`
- `Tasker.Task.Starting`
- `Tasker.Task.Succeeded`
- `task end: [cb_detail={"entry":"AutoStockStapleMain"...`

Important:

- `maafw.log` may have rotated.
- If the target time is missing from `maafw.log`, check `maafw.bak.*.log`.
- Do not mix another task's `task_id` into the analysis.

## 2. Reconstruct actual purchases

The most reliable purchase proof is the framework click result, not only OCR candidates.

Search in `maafw*.log` for:

- `Node.Action.Succeeded.*AutoStockBuyItemValleyIVTask`
- `Node.Action.Succeeded.*AutoStockBuyItemWulingTask`

Each successful click contains a `box=[x,y,w,h]`.

Then search nearby `AutoStockInStapleItemName` OCR results and match the clicked `box` to the OCR item's `box`.

This gives the actual purchased item name.

Do not report every OCR candidate as purchased.

Only report an item as purchased when both are true:

- `AutoStockBuyItem(ValleyIV|Wuling)Task` action succeeded
- the click box matches an OCR item box from `AutoStockInStapleItemName`

## 3. Reconstruct remaining stock bill and timeline

For per-step remaining stock bill, use:

- `AutoStockCurrentStockBill`
- `CurrentStockBillText`

Typical signal:

- `OCRer.cpp ... CurrentStockBillText ... "text":"4153万"`

Interpretation rules:

- This is the current visible stock bill at the time of that recognition.
- If a purchase click happens after that recognition, treat this as the "before purchase" remaining bill.
- If the next stock bill OCR appears after a purchase, that next value can be treated as the latest visible "after previous purchase" value.
- Always build a full timeline, not just a purchase list.
- Include task start, scene switches, item clicks, stock bill OCR points, and task end.

### Cross-scene bill rule

`AutoStockStapleMain` may switch between `ValleyIV` and `Wuling`.

When the task switches scene, do not assume the stock bill value is directly comparable to the previous scene's bill.

Default interpretation:

- `ValleyIV` and `Wuling` may use different stock bill types.
- If a large bill change happens exactly around a scene switch, prefer the explanation "scene changed to a different bill type" over "one item consumed an abnormally large amount".
- Do not attribute a multi-million drop to a single purchased item unless the log explicitly proves quantity and unit cost.

When reporting this case, state it explicitly:

- the large apparent jump is caused by switching from one scene's stock bill to another scene's stock bill
- therefore the cross-scene values should not be treated as a continuous single-currency ledger

Prefer a timeline table:

| Time | Region | Purchased item | Remaining bill before purchase | Next visible bill after purchase |
| ---- | ------ | -------------- | ------------------------------ | -------------------------------- |

If the log has no later OCR after the last purchase, explicitly say the final "after purchase" bill is unavailable.

Also provide an event timeline:

| Time | Event | Evidence |
| ---- | ----- | -------- |
| ... | task started / entered ValleyIV / bought item / switched to Wuling / task ended | ... |

## 4. Separate AutoStockStaple from AutoStockpile

`AutoStockpileMain` is a different task.

Do not merge it into `AutoStockStapleMain` results.

Quick distinction:

- `AutoStockpileMain`: goods bundle selection in `go-service` autostockpile logs
- `AutoStockStapleMain`: stable-demand goods purchase flow driven by pipeline nodes like `AutoStockBuyItemValleyIVTask` and `AutoStockBuyItemWulingTask`

If the user asks about a time range around `AutoStockStapleMain`, you may mention nearby `AutoStockpileMain` activity separately, but keep it outside the staple purchase list.

## 5. Map evidence back to pipeline

Use these nodes to explain behavior:

### Entry and branch selection

- `AutoStockInStapleValleyIV`
- `AutoStockInStapleWuling`

These decide the local search loop:

- cannot buy branch
- buy item branch
- sold out branch
- swipe branch

### Buy branch

- `AutoStockBuyItemValleyIVTask`
- `AutoStockBuyItemWulingTask`

These are the best pipeline nodes for "about to buy item X".

Reason:

- they require all of:
    - `AutoStockInStapleItem`
    - `AutoStockInStapleItemDiscounts`
    - `AutoStockInStapleItemName`
    - `AutoStockTargetCanBuy`
- the `box_index` points to the item-name OCR result
- the click happens immediately after recognition succeeds

### Stop-buy branch

- `AutoStockTargetCanNotBuyValleyIV`
- `AutoStockTargetCanNotBuyWuling`

These are the best nodes for "cannot continue buying because the bill is below threshold".

### Stock bill recognizers

- `AutoStockCurrentStockBill`
- `CurrentStockBillText`

These are the best nodes for the actual remaining bill value.

## 6. Instrumentation guidance

When the user asks where to add logging, recommend:

### To record "what item is about to be bought"

Best location:

- buy-task recognition success path
- specifically around `AutoStockBuyItemValleyIVTask` / `AutoStockBuyItemWulingTask`

Why:

- item name is already resolved
- the branch already guarantees "can buy"
- the click target is fixed to the chosen item

Recommended payload:

- region
- item name
- item box
- current stock bill
- optionally task_id

### To record "why buying stopped"

Best location:

- `AutoStockTargetCanNotBuyValleyIV`
- `AutoStockTargetCanNotBuyWuling`

Recommended payload:

- region
- current stock bill
- threshold or compare expression
- stop reason

### To record "remaining bill after every purchase"

Best location:

- after confirm-buy result node, plus a fresh `AutoStockCurrentStockBill` recognition

If only one of the two pipeline nodes can be used, prefer:

- buy-task node for "about to buy" logs
- cannot-buy node for "stop reason" logs

Do not claim that the cannot-buy node alone can tell you what item was purchased.

## 7. go-service clues

`go-service.log` is still useful for context:

- `AttachToExpectedRegexAction` shows which item whitelist was attached to `AutoStockInStapleItemName`
- it confirms the task entry and runtime override setup

But purchase truth should still come from `maafw*.log` click results plus OCR box matching.

## Common Patterns

### Pattern: OCR candidates exceed actual purchases

Symptom:

- `AutoStockInStapleItemName` shows several allowed items
- but only one `AutoStockBuyItem...Task` action click follows

Conclusion:

- only the item whose box matches the clicked box was purchased

### Pattern: stock bill appears twice with same value

Symptom:

- repeated `CurrentStockBillText` with identical text before one purchase

Conclusion:

- treat them as the same pre-purchase visible bill, not two separate bill states

### Pattern: large bill jump at scene switch

Symptom:

- a large stock bill change appears between the last visible `ValleyIV` bill and the first visible `Wuling` bill, or the reverse

Conclusion:

- first check whether the task changed from `AutoStockInStapleValleyIV` to `AutoStockInStapleWuling`, or the reverse
- if yes, explain that the bill type likely changed with the scene
- do not describe this as one item consuming the whole difference

### Pattern: no later stock bill after last purchase

Conclusion:

- report the last known "before purchase" bill
- explicitly mark final post-purchase bill as unavailable from current evidence

### Pattern: task spans rotated logs

Symptom:

- start is in one `maafw.bak.*.log`
- `maafw.log` or a later backup only contains parser/init lines

Conclusion:

- keep following the log file that contains the matching `task_id`
- do not switch files only by timestamp name

## Output Template

Use this structure when answering:

```markdown
## AutoStockStapleMain 概要

- task_id: `...`
- 起止时间: `...`
- 结果: 成功 / 失败

## 实际购买顺序

1. `时间` - `区域` - `商品名`
2. `时间` - `区域` - `商品名`

## 事件时间线

| 时间 | 事件 | 说明 |
| ---- | ---- | ---- |
| ...  | 进入四号谷地 / 购买某商品 / 切换到武陵 / 任务结束 | ... |

## 调度券时间线

| 时间 | 商品 | 购买前剩余调度券 | 购买后下一次可见调度券 |
| ---- | ---- | ---------------- | ---------------------- |
| ...  | ...  | ...              | ...                    |

说明:

- 同场景内可近似按连续账本理解
- 跨 `ValleyIV` / `Wuling` 场景时，若出现大幅变化，优先解释为券种切换而不是单次异常大额消耗

## 关键依据

- `maafw*.log`: `AutoStockBuyItem...Task` 点击成功 + `AutoStockInStapleItemName` OCR box 对应
- `maafw*.log`: `CurrentStockBillText` OCR
- `go-service.log`: 运行时 override / 任务上下文

## 适合加日志的节点

- 记录购买项: `AutoStockBuyItemValleyIVTask` / `AutoStockBuyItemWulingTask`
- 记录停止原因: `AutoStockTargetCanNotBuyValleyIV` / `AutoStockTargetCanNotBuyWuling`
- 记录剩余调度券: `AutoStockCurrentStockBill`
```

## Guardrails

- Only analyze `AutoStockStapleMain`.
- Do not fold `AutoStockpileMain` into the final purchase list.
- Do not treat OCR candidates as purchases unless a matching buy-task click succeeded.
- Do not infer missing final stock bill values without a later OCR record.
- Do not treat cross-scene stock bill changes as direct same-currency arithmetic unless the log explicitly proves they are the same bill type.
- When referencing pipeline nodes, use the exact node names from the repository.
