# BetterSliding 参数重构 v2 —— FineTuneQuantity / FineTuneFallback（实现计划·迭代版）

> 本计划是 `.dev_doc/better-sliding-param-plan.md` 的**迭代版（v2）**，取代原计划作为本次实现的唯一依据。
>
> ⚠️ **后续变更**：本计划落笔时把 P1-1 / P2-4 / P3-5 列为「已知问题、暂不修复」。这三项**已在本计划之后修复**：预算从 `BetterSlidingCheckQuantity` 的心跳数改挂 Increase/Decrease/Reset2 动作节点，`BetterSlidingFail` 节点已删除。正文中描述旧机制的段落（含设计决定表、边界与假设）保留为当时的记录，现状请以 `.dev_doc/better-sliding-nudge-loop-bound.md` 第 13 节与 `.dev_doc/better-sliding-fail-node-semantics.md` 第 9 节为准。
> 需求原文见 `.dev_doc/better-sliding-param.md`（不改动）。
> 已知问题记录见 `.dev_doc/better-sliding-nudge-loop-bound.md`（P1-1 / P2-4）与 `.dev_doc/better-sliding-fail-node-semantics.md`（P3-5）。
> 本计划已逐条吸收审核报告与用户裁决（P1 / P2 / P3）。落笔前已核对实现：`agent/go-service/bettersliding/` 下 `types.go` / `normalize.go` / `params.go` / `handlers.go` / `overrides.go`，以及 `assets/resource/pipeline/BetterSliding/Main.json`、`Test.json`、两份 `docs/**/better-sliding.md`、`tools/schema/custom.action.schema.json`。基线 `go build ./...` 与 `go vet ./bettersliding/` 已实测通过。

## 目标与成功标准

把「精确点击后是否继续用 Increase/Decrease 微调」与「不微调时做什么」做成两个显式参数，并**彻底移除** `FinishAfterPreciseClick`（不保留兼容代码、不对多余入参做检查）。

成功标准：

1. 新增 `FineTuneQuantity`（`bool` 或 `int`）与 `FineTuneFallback`（`none` / `more` / `less`），`custom_action_param` 与调用节点 `attach` 均可传入，`attach` 优先。
2. `FinishAfterPreciseClick` 从类型、presence、归一化、attach 合并、日志与分支中全部删除；外部仍传该字段时由 JSON 解析静默忽略，不告警、不校验。
3. 不微调时可由 `FineTuneFallback` 触发「PreciseClick 坐标单轴 1px 累加偏移 + 复查」的有限循环；循环上界沿用 `BetterSlidingCheckQuantity` 的 `max_hit: 4`，其 off-by-one 问题本次不修，见 `.dev_doc/better-sliding-nudge-loop-bound.md`。
4. 中英文档同步；不新增任何 `*_test.go`。
5. 本地 `go build ./...`、`go vet ./bettersliding/`、`pnpm format`、`pnpm check` 通过（注意 `pnpm check` 不覆盖 `agent/go-service`）。

## 已确认的设计决定（含对审核意见的裁决）

| 编号 | 项 | 决定 |
| --- | --- | --- |
| P1-1 | nudge 循环最后一次偏移不被复查（off-by-one） | **后续已修复**：预算改挂动作节点、`CheckQuantity` 去掉 `max_hit`，见 `.dev_doc/better-sliding-nudge-loop-bound.md` 第 13 节 |
| P1-2 | `handleNoFineTune` 必须重写 `BetterSlidingPreciseClick` 的 target | **批准**，写入实现规格 |
| P1-3 | 旧参数告警 | **改为**硬移除、不保留检测位、**不加任何告警**；多余入参由 `encoding/json` 静默忽略 |
| P1-4 | 过冲与 `TargetReachable` 语义 | **保持源代码判断**：`TargetReachableOverrideEnable` 只表示解析后的目标可达，与最终调整结果无关 |
| P1-5 | 偏移分支是否挂 `[JumpBack]BetterSlidingMoveMouse` | **不挂**。防遮挡仅服务 `IncreaseButton` / `DecreaseButton`，`PreciseClick` 不受影响 |
| P1-6 | 旧参数影响面 | 已核对：真实业务调用点 0 处，仅 `assets/resource/pipeline/BetterSliding/Test.json:283`，影响面小 |
| P2-1 | int 语义 | 阈值必须 `>= 1`；允许「偏移进入阈值后切回 Increase/Decrease」的混合行为，文档补充说明 |
| P2-2 | `null` 与类型校验 | `null` 视为**已提供**并按正整数校验（因此报错）；非整数与非法类型报错；**超大值允许**，不做上界钳制 |
| P2-3 | 告警标志位生命周期 | **随 P1-3 一并移除**，不再存在 `warnedFinishAfterPreciseClick` |
| P2-4 | 循环上界与复查时机的详细分析 | **已并入** `.dev_doc/better-sliding-nudge-loop-bound.md`，**后续已修复**（第 13 节） |
| P2-5 | 轴选择平局取 y | **批准**，规则与文档均显式写明 |
| P2-6 | 「只设 B 不生效」提醒 | **批准**，写入两份文档 |
| P2-7 | `isSwipeOnlyMode` 判定 | **传入这两个参数即视为非 swipe-only** |
| P3-1 | 文档正文「5 个字段」漏项 | **批准**，两份文档正文同步改为 6 |
| P3-2 | 与源需求的快速路径偏差 | **批准**：不做快速路径，`none` 仍经 `BetterSlidingCheckQuantity` 复检后路由 `Done` |
| P3-3 | 验收命令与 CI 不匹配 | **批准**，在验收章节显式标注 |
| P3-4 | `Test.json` 迁移范围 | 仅把 `__BS-5` 的 `FinishAfterPreciseClick: true` 改写为 `FineTuneQuantity: false` + `FineTuneFallback: none` |
| P3-5 | `BetterSlidingFail` 空叶节点判负性 | **后续已修复**：实测确认空叶节点不判负，节点已删除，见 `.dev_doc/better-sliding-fail-node-semantics.md` 第 9 节 |

## 参数命名与语义

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `FineTuneQuantity` | `bool` 或 `int` | `true` | `true`：CheckQuantity 后照旧用 Increase/Decrease 微调；`false`：一律不微调；`int N`（`N >= 1`）：仅当 `abs(当前 − 目标) <= N` 时微调 |
| `FineTuneFallback` | `string` | `none` | 仅在「本次判定为不微调」时生效：`none` 复检后收尾；`more` / `less` 按下方规则做单轴 1px 累加偏移并复查 |

判定伪代码（`handleCheckQuantity` 入口）：

```text
current := readQuantityValue(arg.RecognitionDetail)
if !shouldFineTuneQuantity(a.FineTuneQuantity, current, a.TargetQuantity) {
    return a.handleNoFineTune(ctx, arg, current)   // 含 Done 与 nudge 两条出口
}
// 原有 switch 保持不变：current == target -> Done；< -> Increase；> -> Decrease
```

`shouldFineTuneQuantity(q, current, target)`：`q.thresholdMode ? abs(current-target) <= q.threshold : q.enabled`。

`handleNoFineTune`：

```text
axis, endSign := resolveNudgeAxis(a.startBox, a.endBox, a.CenterPointOffset)
stepSign := 0
switch a.FineTuneFallback {
case none: stepSign = 0
case more: if current < target { stepSign = +1 }
case less: if current > target { stepSign = -1 }
}
if stepSign == 0 { overrideCheckQuantityBranch(..., Done, ...); log; return true }
return a.nudgePreciseClick(ctx, arg, stepSign)
```

偏移规则：

1. 轴选择：`dx = endX - startX`、`dy = endY - startY`，首尾中心点由 `centerPoint(box, CenterPointOffset)` 得到（`CenterPointOffset` 对首尾同加，不影响差值）。`abs(dx) > abs(dy)` 取 x 轴，否则取 y 轴（**平局取 y**）；`dx == dy == 0` 取 y 轴并 `Warn`。
2. 正方向：`endSign = sign(dx)`（x 轴）或 `sign(dy)`（y 轴），即 **Start → End** 方向；`endSign == 0` 时取 `+1` 并 `Warn`。
3. `more` 步进方向为 `+endSign`（朝 End 靠近）；`less` 步进方向为 `-endSign`（朝 Start 靠近）。
4. 第 k 次偏移坐标 = `preciseClickBase` 在选定轴分量上 `+= endSign * stepSign * k`（`stepSign`：`more` 为 `+1`，`less` 为 `-1`），`k` 从 1 递增；另一轴分量保持不变。
5. 与既有方向语义自洽：`click = start + (end - start) * numerator / denominator`，朝 End 靠近即增大数量，故 `more` 用于修正「当前 < 目标」。
6. 循环上界由**动作节点**的 `max_hit: 4` 在框架层强制（`CheckQuantity` 本身不限次）；用尽后 next 候选全部不可用，框架以 `Node.NextList.Failed` 结束并让内部流水线判负；Go 侧**不自建计数上限**，仅递增日志用索引（原因与影响见 `.dev_doc/better-sliding-nudge-loop-bound.md` 第 13 节）。

## 实现改动

### 1. `agent/go-service/bettersliding/types.go`

- `betterSlidingParam`：删 `FinishAfterPreciseClick bool`；新增 `FineTuneQuantity any`（json tag `FineTuneQuantity`）与 `FineTuneFallback string`（json tag `FineTuneFallback`）。
- `betterSlidingParamPresence`：删 `FinishAfterPreciseClick`；新增 `FineTuneQuantity bool`、`FineTuneFallback bool`（供 `isSwipeOnlyMode` 使用）。
- 新增归一化载体、常量与轴类型：

```go
type fineTuneQuantity struct {
    thresholdMode bool // true 表示按 int 阈值语义
    enabled       bool // 布尔语义下的取值
    threshold     int  // 阈值语义下的取值，>= 1
}

var defaultFineTuneQuantity = fineTuneQuantity{enabled: true}

const (
    FineTuneFallbackNone = "none"
    FineTuneFallbackMore = "more"
    FineTuneFallbackLess = "less"
)

type nudgeAxis uint8

const (
    nudgeAxisX nudgeAxis = iota
    nudgeAxisY
)
```

- `BetterSlidingAction`：删 `FinishAfterPreciseClick bool`；新增 `FineTuneQuantity fineTuneQuantity`、`FineTuneFallback string`、`preciseClickBase [2]int`、`preciseClickNudges int`。
    - 轴与方向不落字段：由 `resolveNudgeAxis` 在偏移路径按 `a.startBox` / `a.endBox` 现场计算（纯函数、确定性、成本可忽略），避免无谓的跨节点缓存与误告警。
- 更新 `BetterSlidingAction` 上方字段说明注释：删除 `FinishAfterPreciseClick` 条目，新增两个新参数说明（含指向文档「不微调语义」的提示）。

### 2. `agent/go-service/bettersliding/normalize.go`

```go
// normalizeFineTuneQuantity 归一化 Param A：
// 未提供（present=false）-> 默认 enabled；
// bool -> 布尔语义；
// float64 整数值 -> 阈值语义，必须 >= 1（不允许 0、负数、非整数）；
// null / 其他类型 / 非整数 / <1 -> 返回错误（显式 null 视为已提供，无静默默认）。
func normalizeFineTuneQuantity(raw any, present bool) (fineTuneQuantity, error)

// normalizeFineTuneFallback 归一化 Param B：空串或 null -> none；
// 大小写不敏感接受 none/more/less，返回小写规范值；其他返回错误。
func normalizeFineTuneFallback(raw string) (string, error)
```

- 新增 `hasRawKey(rawKeys map[string]json.RawMessage, key string) bool`：**仅判断键是否存在**（`null` 也算存在），与既有 `hasNonNullRawKey` 并存并补注释说明差异；`hasRawKey` 只用于两个新参数。
- `isSwipeOnlyMode`：新增 `&& !params.presence.FineTuneQuantity && !params.presence.FineTuneFallback`（P2-7），只传这两个参数不会再被误判为 swipe-only。
- 整数值判定使用 `v == math.Trunc(v)`；`math` 已在文件中导入。

### 3. `agent/go-service/bettersliding/params.go`

- `parsedBetterSlidingParams`：删 `finishAfterPreciseClick`；新增 `fineTuneQuantity fineTuneQuantity`、`fineTuneFallback string`。
- `detectBetterSlidingParamPresence`：删 `FinishAfterPreciseClick` 行；新增 `FineTuneQuantity: hasRawKey(rawKeys, "FineTuneQuantity")`、`FineTuneFallback: hasRawKey(rawKeys, "FineTuneFallback")`。其余字段继续用 `hasNonNullRawKey`，行为不变。
- `normalizeActionParams`：调用两个 normalize 函数，错误时 `a.logger.Error()` 并返回 `false`；swipe-only 提前返回分支补默认值（`defaultFineTuneQuantity` 与 `FineTuneFallbackNone`）；正常分支写入归一化值。
- `applyActionParams`：写入 `a.FineTuneQuantity` 与 `a.FineTuneFallback`。
- `logParsedActionParams`：删 `finish_after_precise_click`；新增 `fine_tune_quantity_mode`（`bool` / `threshold`）、`fine_tune_quantity_enabled`、`fine_tune_quantity_threshold`、`fine_tune_fallback`。
- `mergeAttachParams`：删除 `attach.FinishAfterPreciseClick` 整块；新增 `attach.FineTuneQuantity` 透传（`json.Unmarshal` 到 `any` 后写回 `paramMap`，解析失败仅 `Warn` 且不写）与 `attach.FineTuneFallback`（字符串，同上）；同步更新函数头注释中的字段清单。

### 4. `agent/go-service/bettersliding/handlers.go`

- `handleFindEnd`：
    - 计算 `clickX` / `clickY` 后记录 `a.preciseClickBase = [2]int{clickX, clickY}` 并置 `a.preciseClickNudges = 0`；`OverridePipeline` 写入的初值即 base。
    - 删除 `if a.FinishAfterPreciseClick { ... } else { ... }`，改为**无条件** `OverrideNext(nodeBetterSlidingPreciseClick, []maa.NextItem{{Name: nodeBetterSlidingJumpBackNode}})`。
    - 80% 复位（`shouldResetBeforePreciseClick`）逻辑不变。
- `handleCheckQuantity`：`readQuantityValue` 成功后先过 `shouldFineTuneQuantity` 闸门，不微调则 `return a.handleNoFineTune(ctx, arg, currentQuantity)`；其余 switch 分支与日志不变。
- 新增纯函数：`shouldFineTuneQuantity(q fineTuneQuantity, current, target int) bool`、`resolveNudgeAxis(startBox, endBox []int, offset [2]int) (nudgeAxis, int)`（含两处 `Warn` 兜底）、`nudgedClickTarget(base [2]int, axis nudgeAxis, endSign, stepSign, k int) [2]int`。
- 新增方法：
    - `handleNoFineTune(ctx, arg, current) bool`：按上文伪代码路由到 `Done` 或 nudge；日志含 `fine_tune_fallback` / `axis` / `end_sign` / `current_quantity` / `target_quantity`。
    - `nudgePreciseClick(ctx, arg, stepSign int) bool`：`a.preciseClickNudges++` → `OverridePipeline` 把 `BetterSlidingPreciseClick.action.param.target` 设为 `nudgedClickTarget(...)` → `OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: nodeBetterSlidingPreciseClick}})`（**不含** `[JumpBack]BetterSlidingMoveMouse`，P1-5）→ Info 日志（`step_sign` / `nudge_index` / `nudged_target`）→ 返回 `true`。**必须重写 target**，否则点击坐标仍是 `handleFindEnd` 的原值，nudge 变成无效空转（P1-2）。
- `resetState`：新增 `a.preciseClickBase = [2]int{}` 与 `a.preciseClickNudges = 0`；不新增任何告警标志位（P1-3 / P2-3）。

### 5. Pipeline

- `assets/resource/pipeline/BetterSliding/Main.json`：**不改**。`BetterSlidingCheckQuantity.next` 已含 `BetterSlidingPreciseClick` 兜底（第 222-228 行），且 Go 每个分支都 `OverrideNext`。
- `assets/resource/pipeline/BetterSliding/Test.json`（P3-4，最小改动）：
    - `__BS-5`：删除 `FinishAfterPreciseClick: true`，改为 `FineTuneQuantity: false` 与 `FineTuneFallback: none`；`focus` 文案 `325（非精确模式）` 同步改为语义等价的新描述（如 `325（不微调，直接收尾）`）。
    - 新增手工用例 `__BS-7`（`FineTuneQuantity: false` + `FineTuneFallback: more`）、`__BS-8`（`less`）、`__BS-9`（`FineTuneQuantity: 3` 阈值，验证混合行为），并追加到 `__BS-T-1.next` 末尾。
- 无 schema 改动：`tools/schema/custom.action.schema.json` 对 `BetterSliding` 只登记 enum、无参数 `$ref`（已核对）。
- 无 `assets/tasks` / `assets/locales` / `assets/interface.json` 改动：`BetterSlidingTest` 不在 task 清单中（已核对）。

### 6. 文档与 `.dev_doc`

- `docs/zh_cn/developers/components/better-sliding.md` 与 `docs/en_us/developers/components/better-sliding.md`：
  1. attach 参数表 5 改为 6：删 `FinishAfterPreciseClick` 行；加 `FineTuneQuantity`（`bool` / `int`，默认 `true`，int 须 `>= 1`）与 `FineTuneFallback`（`none` / `more` / `less`，默认 `none`）。
  2. 正文「除上述 5 个字段外…」与对应英文句同步改为 6（P3-1）。
  3. 新增「不微调语义」小节：触发条件、Start → End 正方向、轴选择（`abs(dx) > abs(dy)` 取 x，否则 y，平局取 y）、`more` / `less` 含义、以原精确点击坐标为基准的 ±k px 累加、复查循环上界 `BetterSlidingCheckQuantity.max_hit`（当前 4）、用尽后走 `BetterSlidingFail`。
  4. 补充「只设置 `FineTuneFallback` 而不关闭微调时该参数不生效」的提醒（P2-6）。
  5. 补充 int 阈值下的混合行为：偏移进入阈值后，后续回到 Increase/Decrease 微调（P2-1）。
  6. 结果节点契约处补一句：`TargetReachableOverrideEnable` 只表示解析后的目标可达，与最终调整结果无关（P1-4）。
- 新建 `.dev_doc/better-sliding-nudge-loop-bound.md`（P1-1 与 P2-4 合并）：记录 nudge 循环把 `max_hit` 当作偏移次数使用导致的 off-by-one、逐拍推演、影响场景、与 P1-4 / P3-5 的交互、可观测性影响、修复方向与待裁决清单；明确**本次暂不修复**。
- 新建 `.dev_doc/better-sliding-fail-node-semantics.md`（P3-5）：记录 `BetterSlidingFail` 为空叶节点、判负性未实测确认（可能使 `runInternalPipeline` 把失败当成功并触发 `applyOutcomeOverrides`）；明确**本次不修复**。
- 在 `.dev_doc/better-sliding-param-plan.md` 顶部加一行指向本 v2 计划，避免两份计划并存时误用旧版。

## 已知问题（当时不修复，仅记录；后续均已修复）

> 下表为 v2 落笔时的状态。P1-1 / P2-4 / P3-5 已在本计划之后修复：预算改挂动作节点、`BetterSlidingFail` 已删除。

| 编号 | 问题 | 记录位置 | 后续状态 |
| --- | --- | --- | --- |
| P1-1 / P2-4 | nudge 循环最后一次偏移不被复查（`max_hit` 是心跳数不是重试次数），有效可验证偏移仅 3 次；第 4 次点击结果不可见且必然走 `BetterSlidingFail` | `.dev_doc/better-sliding-nudge-loop-bound.md` | **已修复**，见该文档第 13 节 |
| P3-5 | `BetterSlidingFail` 为空叶节点，判负性未实测确认，可能把 nudge 失败上报为成功 | `.dev_doc/better-sliding-fail-node-semantics.md` | **已修复**（删除该节点），见该文档第 9 节 |

本计划在实现 `nudgePreciseClick` 时**未自建计数上限**（仅递增日志索引）；后续修复也沿用了这一约定——预算仍由框架层的 `max_hit` 承担，只是从识别节点移到了动作节点。

## 边界与失败模式

- `FineTuneQuantity` 传 `null`、非整数浮点、负数、`0` 或非 bool/int 类型 → `normalizeFineTuneQuantity` 报错 → `normalizeActionParams` 失败 → Custom 返回 `false`（不静默降级）。
- `FineTuneFallback` 传非法字符串 → 报错 → 返回 `false`；传 `null` 或 `""` → 归一化为 `none`（键存在仍计入 presence，因此仍视为非 swipe-only）；大小写不敏感（`MORE` 与 `more` 等价，与 `Direction` / `TargetQuantityType` 的既有风格一致）。
- `FineTuneQuantity` 为超大 int：允许，不钳制；`abs(current - target) <= N` 恒真，等价于「总是微调」。
- 只传 `FineTuneQuantity` / `FineTuneFallback` 之一或全部 → 按 P2-7 不进入 swipe-only 模式，会继续要求 `TargetQuantity` / `SliderQuantity` / `Direction` 等必需参数，缺失时按既有校验失败。
- 不微调且 `current == target`（`none` / `more` / `less` 任一）→ 一律路由 `BetterSlidingDone`。
- `dx == dy == 0` → 取 y 轴并 `Warn`；`endSign == 0` → 取 `+1` 并 `Warn`。
- 偏移循环用尽 `max_hit` 后框架以 `Node.NextList.Failed` 结束并判负（`BetterSlidingFail` 已删除；详见问题记录文档第 13 节）。
- 既有行为不变：`ClampTargetToSliderMax` / `ReverseTarget` / `TargetQuantityType` / `AvailableQuantity` / `ResetBeforeFindStart` 的 80% 复位 / 结果节点契约。
- 旧参数硬移除的语义变化：原 `FinishAfterPreciseClick: true` 现在会被忽略并回落为默认 `FineTuneQuantity: true`（**继续微调**）。已确认真实业务代码无调用，仅 `Test.json` 一处，因此判定影响面可接受；两份文档与 `.dev_doc` 记录该破坏性变更。

## 验证与验收

```bash
cd agent/go-service && go build ./... && go vet ./bettersliding/
pnpm format          # prettier + maafw-sort，会重排 JSON
pnpm check           # maa-tools check
```

- **CI 覆盖说明（P3-3）**：`.github/workflows/` 中没有任何 Go build / vet / test 步骤；`pnpm check` 只覆盖 `assets/**` 与 `tools/schema/**`（`check.yml` 的 `paths` 不含 `agent/**`）。因此 Go 命令只是**本地门槛**，不能视为 CI 保障。
- 手工验证：运行 `BetterSlidingTest`（据点管理处，可交易数量 1k 到 3k），依次确认 `none` 复检后收尾、`more` 朝 End 累加偏移、`less` 朝 Start 累加偏移、`FineTuneQuantity: 3` 的阈值混合行为；日志观察 `fine_tune_fallback` / `axis` / `end_sign` / `step_sign` / `nudged_target` / `nudge_index`。该测试任务不在 `assets/tasks` 清单中，需开发者自行触发。

验收清单：

- [ ] 两个新参数在 `custom_action_param` 与 `attach` 两处均可生效，`attach` 优先。
- [ ] 仓库内不存在任何 `FinishAfterPreciseClick` 引用（代码、pipeline、文档、schema）。
- [ ] `go build ./...` / `go vet ./bettersliding/` / `pnpm check` 通过。
- [ ] 两份文档的参数表、正文「6 个字段」、示例与新行为一致。
- [ ] 两份问题记录文档内容准确，且明确标注「本次不修复」。
- [ ] 无新增 `*_test.go`。

## 假设

1. `TargetReachableOverrideEnable` 的语义与实现本次均不改动（P1-4）。
2. nudge 循环 off-by-one 与 `BetterSlidingFail` 判负性属已知问题，本次只文档化（P1-1 / P2-4 / P3-5）。
3. 现有真实调用点不使用 `FinishAfterPreciseClick`（已 grep 确认仅 `Test.json`）。
4. `null` 视为已提供的新判定（`hasRawKey`）仅作用于 `FineTuneQuantity` 与 `FineTuneFallback`，其余字段继续沿用 `hasNonNullRawKey`。
5. `BetterSlidingAction` 为注册期单例（`register.go`），`startBox` / `endBox` / `preciseClickBase` 等跨内部节点状态依赖该既有机制。
6. `CenterPointOffset` 对首尾中心点同加，不影响 `End - Start`。
