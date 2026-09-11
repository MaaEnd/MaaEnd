# BetterSlidingFail 判负性未确认问题（P3-5）

> 状态：**已修复（方案：删除 `BetterSlidingFail` 节点）**。
> 来源：审核报告 P3-5。
> 关联：`FineTuneFallback` 不微调循环用尽后必然经由 `BetterSlidingJumpBackNode` 落到本节点；P1-1 / P2-4（nudge 循环上界，见 `.dev_doc/better-sliding-nudge-loop-bound.md`）。
> 实现计划见 `.dev_doc/better-sliding-param-plan-v2.md`。
>
> **修复结论**：第 2 节的推断被实测证实——空叶节点**不会**判负，会把「重试耗尽」静默上报为成功。最终不采用第 6 节的任何方案，而是**直接删除该节点**：删掉之后 next 候选耗尽会走框架原生失败路径。第 1～8 节保留为修复前的分析记录，修复结果见第 9 节。

> [!warning]
> 本文档第 1～8 节描述的是**修复前**的旧结构，其中的行号与「节点定义」均已随节点删除而失效，请勿据此排查现状；现状以第 9 节为准。

## 1. 问题摘要

`BetterSlidingFail` 是**空叶节点**（`assets/resource/pipeline/BetterSliding/Main.json:137-142`，只有 `pre_delay` / `post_delay` / `rate_limit`，没有 `recognition`、没有 `action`、没有 `next`）：

```json
"BetterSlidingFail": {
    "desc": "如果路由至此，则说明微调失败（超过次数）",
    "pre_delay": 0,
    "post_delay": 0,
    "rate_limit": 0
}
```

疑点：**它是否真的会把内部流水线判为失败？** 如果不会，那么 `runInternalPipeline` 会把它当作成功结束，从而把「微调失败 / nudge 用尽」上报为成功，并可能继续触发结果节点覆盖。

## 2. 机制分析：为什么它很可能**不会**判负

### 2.1 `on_error` 才是「失败」的触发点

Pipeline 协议（`tools/schema/pipeline.schema.json:4188-4192`）：

> `on_error`：当识别超时，或动作执行失败后，接下来会执行该列表中的节点。

也就是说，单个节点被判为「失败」的情形只有两类：**识别超时**或**动作执行失败**。`BetterSlidingFail`：

- 没有识别配置 → 不存在识别超时；
- 没有动作配置 → 不存在动作失败；
- 是经由 `next` 列表被**命中并执行**的（`Main.json:132-135` 的 `BetterSlidingJumpBackNode.next` 第二项），属于正常流转；
- 自身 `next` 为空 → 链路正常结束。

因此按协议语义推断：**`BetterSlidingFail` 很可能被判定为「正常结束」而非「失败」。** `Main.json:138` 的 desc 文案（“微调失败”）更像是作者意图，而不是框架行为。

### 2.2 该结论会传导到 Go 侧

`agent/go-service/bettersliding/handlers.go` 的 `runInternalPipeline`：

```go
if !detail.Status.Success() {
    // 记 Error 日志并 return false
}
// 继续执行 applyOutcomeOverrides(...) 并 return true
```

若 `BetterSlidingFail` 被当作正常结束，则 `detail.Status.Success()` 为真，于是：

1. `runInternalPipeline` 返回 `true`，Custom Action 对外**成功**；
2. `applyOutcomeOverrides` 继续执行，按本次判定把 `outOfRange` / `targetReachable` 写入调用方结果节点的 `enabled`；
3. 调用方（如 `OutpostTrading*BetterSliding`、`AutoStockStapleBetterSliding`）的 `next` 会继续推进，按其业务逻辑认为数量已经设置好。

### 2.3 与结果节点契约的关系

`applyOutcomeOverrides`（`handlers.go:889-925`）只看**解析期**的两个布尔量：

- `a.outOfRange`：目标 < 1 / 滑条上限为 0 / 未钳制时超上限；
- `a.targetReachable`：目标位于 `[1, sliderMaxQuantity]`。

两者都在 `handleGetSliderMaxQuantity` / `handleGetAvailableQuantity` 阶段确定，**与后续微调或 nudge 是否成功无关**（这正是 P1-4 裁决保留的语义）。因此当 nudge 循环失败走到 `BetterSlidingFail` 时，`targetReachable` 仍为 `true`，`TargetReachableOverrideEnable` 会被置为 `true`——**失败被包装成「目标可达」**。

### 2.4 对照：其他子任务是怎么处理失败的

`agent/go-service/common/subtask/action.go:108-132` 的 `SubTask` 显式检查 `detail.Status.Success()`，失败时可通过 `failActionOnSubFailure` 让自身返回 `false`；`autosell`、`intelarchive`、`camerascan` 等同样检查状态。BetterSliding 的检查代码是有的——**问题不在检查缺失，而在于被检查的「失败」节点可能根本不产生失败状态**。

## 3. 影响面

| 场景 | 表现 |
| --- | --- |
| 既有 Increase/Decrease 微调超次数 | 同样走 `BetterSlidingFail`，同样可能被上报为成功（既有问题，非本次引入） |
| 新增 nudge 循环用尽 `max_hit` | 更频繁地走到该路径，因此暴露概率显著上升（P1-1 / P2-4） |
| 调用方业务 | 售卖 / 购买等外层流程可能按「已达目标数量」继续执行，实际数量不符 |
| 日志排查 | `BetterSlidingFail` 无任何子日志，链路静默结束，缺少「为什么失败」的线索 |

严重度取决于实际验证结果：若 `Fail` 确实会判负，则本项只是「desc 文案与实现一致的确认问题」；若不会，则属**静默成功**类缺陷，优先级较高。

## 4. 为什么本次不修复

1. 修复方案会改动**既有 Increase/Decrease 微调路径**的对外行为（从「可能静默成功」变为「明确失败」），影响面超出本次参数重构范围。
2. 需要先确认框架对「空叶节点 + next 耗尽」的真实判定，属**待实测验证**项，不宜凭推断改代码。
3. 本次 v2 计划遵循「只做参数重构、不夹带行为修复」的原则；已知问题统一以文档形式沉淀。

## 5. 待验证项（建议优先做）

1. 构造一次必然触发 `BetterSlidingFail` 的运行（例如 `FineTuneQuantity: false` + `FineTuneFallback: "more"`，且目标数量与初始值差距大于 nudge 预算），观察：
   - `maafw.log` 中 `BetterSlidingFail` 命中后的子任务状态（`subtask_status` 字段）；
   - `go-service.log` 中是否出现 `internal BetterSliding pipeline failed`；
   - 调用方结果节点（`TargetReachableOverrideEnable` 指向的节点）最终 `enabled` 值。
2. 用等价的空叶节点 + `next` 耗尽结构做一个最小复现，确认框架是否统一按「正常结束」处理。
3. 确认「识别超时 / 动作失败」是本框架里唯一会触发 `on_error` 与失败状态的路径。

验证方式示例（记录到日志后比对）：

```bash
# 触发一次 BetterSlidingTest 中的偏移用尽用例后，检查：
# 1) go-service.log 是否出现 "internal BetterSliding pipeline failed"
# 2) 若未出现，说明子任务被判为成功 -> 本问题成立
```

## 6. 可选修复方向（暂不实施）

| 方案 | 做法 | 优点 | 代价 |
| --- | --- | --- | --- |
| A. 文档化 + 实测确认（当前采用） | 不改代码，仅记录并安排验证 | 零风险，不动既有行为 | 静默成功风险持续存在 |
| B. 给 `BetterSlidingFail` 加显式失败动作 | 例如挂 `StopTask`（仓库内 `assets/resource/pipeline` 多处使用该动作），让失败显式终止 | 语义明确，日志可见 | 会改变既有微调失败路径的对外行为；需确认 `StopTask` 对子任务状态的影响 |
| C. Go 侧不依赖 Fail 节点判负 | nudge 预算用尽时由 Go 直接返回 `false`（或记 Error 并拒绝置位结果节点） | 不依赖框架对空叶节点的判定，行为可控 | 与 P1-1 / P2-4 的方案 C 绑定，需一并设计 |
| D. 引入显式失败识别节点 | 在 Fail 前放一个必然失败的识别（如超时）以触发 `on_error` 链路 | 走框架原生失败路径 | 引入额外 `timeout`，与仓库「少用 timeout」的规范相悖 |

## 7. 相关文件与行号

| 文件 | 位置 | 说明 |
| --- | --- | --- |
| `assets/resource/pipeline/BetterSliding/Main.json` | 137-142 | `BetterSlidingFail` 节点定义（空叶节点） |
| `assets/resource/pipeline/BetterSliding/Main.json` | 127-135 | `BetterSlidingJumpBackNode` 的 next（第二项即 Fail） |
| `agent/go-service/bettersliding/handlers.go` | `runInternalPipeline` | `detail.Status.Success()` 检查与成功返回 |
| `agent/go-service/bettersliding/handlers.go` | `applyOutcomeOverrides` | 结果节点开关同步（只看解析期判定） |
| `tools/schema/pipeline.schema.json` | 4188-4192 | `on_error` 触发条件（识别超时 / 动作失败） |
| `agent/go-service/common/subtask/action.go` | 108-132 | 其他组件对子任务状态的对照处理 |

## 8. 当时的结论（修复前）

- 本问题当时**不修复**，仅记录。
- 实现 v2 计划时不要顺手给 `BetterSlidingFail` 加动作或改判负逻辑；保持既有结构。
- 建议优先完成第 5 节的实测确认，再决定是否按方案 B / C 修复。

## 9. 修复结果（当前生效机制）

### 9.1 实测证实：空叶节点不判负

第 2 节的推断成立。决定性证据来自 issue 日志包中的 `maafw.bak.2026.05.08-09.21.44.644.log`（`BetterSlidingCheckQuantity` 的 next 候选被 `max_hit` 全部耗尽的场景）：

```text
09:18:02.979  msg=Node.NextList.Failed
              name=BetterSlidingCheckQuantity
              list=[{name:BetterSlidingDecreaseQuantity},{jump_back:true,name:BetterSlidingMoveMouse}]
...（框架按 timeout 持续重试）...
09:18:22.862  msg=Node.NextList.Failed   ← 最后一轮
```

随后 go-service 侧：

```json
{"level":"error","component":"BetterSliding","task":"BetterSlidingCheckQuantity",
 "subtask_status":"failure","message":"internal BetterSliding pipeline failed"}
```

关键区别在于：**候选耗尽**（next 列表里没有任何节点可命中）会被框架判为节点失败；而**命中一个空叶节点**则被判为链路正常结束。`BetterSlidingFail` 属后者，因此它一旦被路由到，就把失败「吃掉」了。

### 9.2 采用的修复：删除该节点

最终删除 `BetterSlidingFail`（节点定义 + `BetterSlidingJumpBackNode.next` / `BetterSlidingCheckQuantity.next` 中的全部引用 + `agent/go-service/bettersliding/nodes.go` 的 `nodeBetterSlidingFail` 常量），让耗尽路径回到框架原生判负。

为什么不采用第 6 节的方案：

| 方案 | 未采用的原因 |
| --- | --- |
| B. 给 `BetterSlidingFail` 加显式失败动作（如 `StopTask` / `FalseAction`） | 需要为一个「本可以不存在」的节点维护判负动作；`StopTask` 还会中断整条任务链，语义比单纯判负更重 |
| C. Go 侧不依赖 Fail 节点判负 | 实测证明框架已能正确判负，Go 侧额外收口属重复实现；本次 `overrides.go` / `handlers.go` 的路由逻辑**零改动** |
| D. 引入必然失败的识别节点 | 引入额外 `timeout`，与仓库「少用 timeout」的规范相悖 |

### 9.3 影响面

| 场景 | 修复后表现 |
| --- | --- |
| 新增 nudge 循环用尽 `max_hit` | next 候选耗尽 → 框架判负 → `runInternalPipeline` 记录 `internal BetterSliding pipeline failed` 并返回 `false` |
| 既有 Increase/Decrease 微调超次数 | 同上，失败不再被静默上报为成功 |
| 调用方业务 | `applyOutcomeOverrides` **不再执行**，`TargetReachableOverrideEnable` 不会被误置为 true；调用方（如 `AutoStockpileSwipeSpecificQuantity`）按失败分支处理，其 `focus` 文案（如「定量滑动失败，取消购买」）会正常触发 |
| 日志排查 | `maafw.log` 可见 `Node.NextList.Failed`，go-service 可见失败原因与 `subtask_status: failure` |

### 9.4 相关文件

| 文件 | 说明 |
| --- | --- |
| `assets/resource/pipeline/BetterSliding/Main.json` | `BetterSlidingFail` 节点定义已删除；`BetterSlidingJumpBackNode.next` 与 `BetterSlidingCheckQuantity.next` 中的引用已移除 |
| `assets/resource/pipeline/BetterSliding/Main.json` | `BetterSlidingClearMaxHit` 的 `nodes` 已补入三个新预算节点 |
| `agent/go-service/bettersliding/nodes.go` | 已删除 `nodeBetterSlidingFail` 常量 |
| `tools/schema/pipeline.schema.json` | 4188-4192：`on_error` 触发条件（识别超时 / 动作失败）——本次未改动，仅作为判负机制的依据 |
| `agent/go-service/bettersliding/handlers.go` | `runInternalPipeline` 的 `detail.Status.Success()` 检查（未改动，修复后才能真正生效） |

剩余代价：耗尽后需等框架 `timeout`（默认 20s）才判负（上例约 20 秒）。如需更快失败，可另行在 `BetterSlidingCheckQuantity` 上显式设 `timeout`。
