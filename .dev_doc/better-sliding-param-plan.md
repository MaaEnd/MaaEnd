# BetterSliding 微调开关（FineTuneQuantity / FineTuneFallback）实现计划

> ⚠️ 本文档为 v1 初版，已由 [`better-sliding-param-plan-v2.md`](./better-sliding-param-plan-v2.md)（迭代版）取代；请以 v2 为准。

> 本文档是 [`better-sliding-param.md`](./better-sliding-param.md) 的执行版，记录已确定的实现细节；原文不改动。

## 目标与成功标准

按 `.dev_doc/better-sliding-param.md` 的意图，把「精确点击后是否继续用 Increase/Decrease 微调」与「不微调时做什么」做成两个显式参数，并删除 `FinishAfterPreciseClick`。

成功标准：

1. 新增 Param A（`Bool | Int`）与 Param B（`String: none/more/less`），两者同时支持 `custom_action_param` 与调用节点 `attach`（`attach` 优先）。
2. 删除 `FinishAfterPreciseClick`；调用方仍传该字段时被忽略并打印告警日志。
3. 不微调时可由 Param B 触发「PreciseClick 坐标单轴 1px 累加偏移 + 复查」的有限循环，循环上界复用 `BetterSlidingCheckQuantity` 的 `max_hit: 4`。
4. 中英文档同步；不新增任何 `*_test.go`，`agent/go-service/bettersliding/` 保持 0 测试文件。
5. `go build ./...`、`pnpm format`、`pnpm check` 通过。

## 已确认的设计决定

| 项 | 决定 |
| --- | --- |
| Param A 为 Int 时 | 按差值阈值：`abs(当前数量 − 目标数量) <= N` 才微调；否则走 Param B |
| Param B 触发条件 | `more`：当前 < 目标 → 朝 End 方向偏移 1px；`less`：当前 > 目标 → 朝 Start 方向偏移 1px；条件不满足 → 直接按成功收尾（路由 `BetterSlidingDone`） |
| 偏移正方向定义 | 以 **Start → End** 向量为正方向：`more` 朝 End 靠近，`less` 朝 Start 靠近（与 `Direction`、屏幕坐标正负无关） |
| 偏移轴选择 | `abs(Δx) > abs(Δy)` 取 x 轴，否则 y 轴（`Δ = End − Start`，用经 `CenterPointOffset` 处理的中心点计算） |
| 1px 偏移收敛方式 | 偏移量**累加**：第 k 次偏移 = 原始精确点击坐标 + (±1 × k) px（仅作用于选定轴）；每次偏移点击后回 `BetterSlidingCheckQuantity` 复查，循环用尽（`max_hit: 4`）后走既有 `BetterSlidingJumpBackNode → BetterSlidingFail` 路径 |
| 参数读取位置 | `custom_action_param` 与 `attach` 都可读，`attach` 优先；默认 `A=true`、`B="none"` |
| 旧参数移除方式 | 硬移除；仍传入时忽略但打印告警 |
| 偏移分支是否附带 `[JumpBack]BetterSlidingMoveMouse` | 不附带，next 只放 `BetterSlidingPreciseClick` |
| Param B = none 的收尾 | 仍经过 `BetterSlidingCheckQuantity` 复检后路由 `BetterSlidingDone`（不做旧 `FinishAfterPreciseClick` 那种在 FindEnd 阶段清空 next 的快速路径） |
| 测试策略 | 不新增 `*_test.go`；仅构建/格式检查 + 手工跑 `BetterSlidingTest` 任务验证 |

## 参数命名与语义

命名风格对齐既有 `ClampTargetToSliderMax` / `ReverseTarget`：

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `FineTuneQuantity` | `bool` 或 `int` | `true` | `true`：CheckQuantity 后照旧用 Increase/Decrease 微调；`false`：不微调；`int N`（N ≥ 0）：仅当 `abs(当前−目标) <= N` 时微调 |
| `FineTuneFallback` | `string` | `"none"` | 仅在本次判定为「不微调」时生效：`none` 直接成功收尾；`more` / `less` 按下方规则做单轴 1px 累加偏移并复查 |

判定伪代码（`handleCheckQuantity` 入口）：

```text
if !shouldFineTuneQuantity(a.FineTuneQuantity, current, target) {
    // 不微调
    switch a.FineTuneFallback {
    case "none":  -> next = Done
    case "more":  if current < target { nudge(towardEnd) } else { -> next = Done }
    case "less":  if current > target { nudge(towardStart) } else { -> next = Done }
    }
    return
}
// 原逻辑不变：current == target -> Done；< -> Increase；> -> Decrease
```

偏移规则：

1. 轴选择：`dx = endX - startX`、`dy = endY - startY`，其中首尾坐标即 `handleFindEnd` 中经 `centerPoint(..., CenterPointOffset)` 得到的中心点（`CenterPointOffset` 对首尾同加，不影响差值）。`|dx| > |dy|` → x 轴，否则 y 轴；两者皆为 0 时取 y 轴并打一条 `Warn`。
2. 正方向：选定轴上的符号 `endSign = sign(dx)`（x 轴）或 `sign(dy)`（y 轴），即 **Start → End** 的方向。
3. `more` 的步进方向 = `+endSign`（朝 End 靠近）；`less` 的步进方向 = `−endSign`（朝 Start 靠近）。
4. 第 k 次偏移坐标 = 原始精确点击坐标在选定轴分量上 `+= endSign × stepSign × k`，其中 `stepSign` 为 `more:+1` / `less:−1`；`k` 从 1 递增。
5. 与方向语义自洽：`click = start + (end−start)×numerator/denominator`，朝 End 靠近即增大数量，因此 `more` 用于修正「当前 < 目标」。
6. 循环上界：`BetterSlidingCheckQuantity` 的 `max_hit: 4`，用尽后其被 next 跳过，`BetterSlidingJumpBackNode` 的 next 落到既有 `BetterSlidingFail`（既有行为，本次不改）。

## 实现改动

### 1. `agent/go-service/bettersliding/types.go`

- `betterSlidingParam`：删 `FinishAfterPreciseClick bool`；新增 `FineTuneQuantity any`（`json:"FineTuneQuantity"`）、`FineTuneFallback string`（`json:"FineTuneFallback"`）。
- `betterSlidingParamPresence`：新增 `FineTuneQuantity bool`（区分「未提供 → 默认 true」与「显式 false」）；保留 `FinishAfterPreciseClick bool` 仅作废弃检测位。
- 新增归一化载体与常量：

```go
type fineTuneQuantity struct {
    thresholdMode bool // true 表示按 int 阈值语义
    enabled       bool // 布尔语义下的取值
    threshold     int  // 阈值语义下的取值，>= 0
}

var defaultFineTuneQuantity = fineTuneQuantity{enabled: true}

const (
    FineTuneFallbackNone = "none"
    FineTuneFallbackMore = "more"
    FineTuneFallbackLess = "less"
)

// nudgeAxis 表示精确点击坐标单轴偏移所作用的轴。
const (
    nudgeAxisX = 0
    nudgeAxisY = 1
)
```

- `BetterSlidingAction`：删 `FinishAfterPreciseClick bool`；新增 `FineTuneQuantity fineTuneQuantity`、`FineTuneFallback string`、`preciseClickBase [2]int`、`preciseClickAxis int`、`preciseClickEndSign int`、`preciseClickNudges int`、`warnedFinishAfterPreciseClick bool`。
- 同步更新 `BetterSlidingAction` 上方字段说明注释。

### 2. `agent/go-service/bettersliding/normalize.go`

```go
// normalizeFineTuneQuantity 归一化 Param A：
// 未提供 -> 默认 enabled；bool -> 布尔语义；
// 整数（含 JSON 的 float64 整数形式）-> 阈值语义，必须 >= 0；
// 其他类型或负数返回错误。
func normalizeFineTuneQuantity(raw any, present bool) (fineTuneQuantity, error)

// normalizeFineTuneFallback 归一化 Param B：空串 -> none；
// 大小写不敏感接受 none/more/less，返回小写规范值；其他返回错误。
func normalizeFineTuneFallback(raw string) (string, error)
```

`isSwipeOnlyMode` **不加**两个新参数的 presence 判断（与 `FinishAfterPreciseClick`/`ResetBeforeFindStart` 同属「仅指定数量模式」参数）。

### 3. `agent/go-service/bettersliding/params.go`

- `parsedBetterSlidingParams`：删 `finishAfterPreciseClick`；加 `fineTuneQuantity fineTuneQuantity`、`fineTuneFallback string`。
- `detectBetterSlidingParamPresence`：加 `FineTuneQuantity: hasNonNullRawKey(rawKeys, "FineTuneQuantity")`；`FinishAfterPreciseClick` 的 presence 保留，仅用于告警。
- `normalizeActionParams`：调用两个 normalize 函数并处理错误；`params.presence.FinishAfterPreciseClick` 且未告警过时打印一次 `Warn` 并置位标志（避免外层 + 7 个内部节点重复刷屏）；swipe-only 提前返回的分支补齐这两个字段默认值。
- `applyActionParams`：写入 `a.FineTuneQuantity` / `a.FineTuneFallback`。
- `logParsedActionParams`：删 `finish_after_precise_click`，加 `fine_tune_quantity_threshold_mode` / `fine_tune_quantity_enabled` / `fine_tune_quantity_threshold` / `fine_tune_fallback`。
- `mergeAttachParams`：把 `attach.FinishAfterPreciseClick` 合并块替换为通用透传式 `attach.FineTuneQuantity`（`json.Unmarshal` 到 `any`）与 `attach.FineTuneFallback`（字符串）；同步更新函数头注释。

### 4. `agent/go-service/bettersliding/handlers.go`

- `handleFindEnd`：记录 `preciseClickBase` / `preciseClickAxis` / `preciseClickEndSign` 并重置 `preciseClickNudges`；删除 `FinishAfterPreciseClick` 分支，改为无条件把 `BetterSlidingPreciseClick.next` 恢复为 `BetterSlidingJumpBackNode`。
- `handleCheckQuantity`：先做微调闸门，不微调时走 `handleNoFineTune`。
- 新增纯函数 `shouldFineTuneQuantity` / `resolveNudgeAxis` / `nudgedClickTarget`，与 `handleNoFineTune` / `nudgePreciseClick` 两个方法。
- `resetState`：重置新状态字段。

### 5. Pipeline

- `assets/resource/pipeline/BetterSliding/Main.json`：无需改动（`BetterSlidingCheckQuantity.next` 已含 `BetterSlidingPreciseClick` 作兜底，Go 每个分支都会 `OverrideNext`）。
- `assets/resource/pipeline/BetterSliding/Test.json`：`__BS-5` 迁移到新参数；新增 `more` / `less` / 整数值阈值用例并挂进 `__BS-T-1.next`。
- 无 schema 改动：`tools/schema/custom.action.schema.json` 对 `BetterSliding` 只登记 enum、无参数 `$ref`。
- 无 `assets/tasks` / `assets/locales` / `assets/interface.json` 改动。

### 6. 文档

- `docs/zh_cn/developers/components/better-sliding.md` 与 `docs/en_us/developers/components/better-sliding.md`：attach 参数由 5 改为 6，删除 `FinishAfterPreciseClick` 行，新增两个新参数行；新增「不微调语义」小节说明触发条件、Start→End 正方向、轴选择、累加与循环上界。

## 边界与失败模式

- `FineTuneQuantity` 传负数或非 bool/int 类型、`FineTuneFallback` 非法值：`normalizeActionParams` 失败 → Custom 返回 `false`。
- 同时传 `FinishAfterPreciseClick` 与新参数：新参数生效，旧字段忽略并告警一次。
- 不微调分支在 `current == target` 时（`more` / `less`）一律路由 `Done`。
- `dx == dy == 0` 兜底取 y 轴并 `Warn`；`endSign == 0` 兜底取 `+1` 并 `Warn`。
- 偏移累计最多受 `max_hit: 4` 限制，不会无限循环。
- 既有行为不变：`ClampTargetToSliderMax`、`ReverseTarget`、`TargetQuantityType`、`AvailableQuantity`、`ResetBeforeFindStart` 的 80% 复位、结果节点契约。

## 假设

1. 不微调提前收尾时 `applyOutcomeOverrides` 仍把 `TargetReachableOverrideEnable` 置为 `true`（与旧 `FinishAfterPreciseClick` 一致）。
2. `BetterSlidingFail` 分支「数量微调超次数」的既有语义不在本次范围内改动。
3. `CenterPointOffset` 对首尾同加，不影响 `Δ = End − Start`。

## 验证与验收

```bash
cd agent/go-service && go build ./... && go vet ./bettersliding/
pnpm format
pnpm check
```

手工验证：运行 `BetterSlidingTest` 任务（据点管理处，可交易数量 1k~3k），依次确认 `none` 直接收尾、`more` 朝 End 累加偏移、`less` 朝 Start 累加偏移、整数值阈值用例，并观察日志中的 `fine_tune_fallback`、`axis`、`end_sign`、`step_sign`、`nudged_target`。

验收：两个新参数在 `custom_action_param` 与 `attach` 两处均可生效且 attach 优先；`FinishAfterPreciseClick` 除告警检测与文档说明外不再被读取；`go build` / `pnpm check` 通过；中英文档参数表与示例与新行为一致。
