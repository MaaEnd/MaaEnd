# BetterSliding nudge 循环上界与复查时机问题（P1-1 / P2-4）

> 状态：**已知问题，暂不修复**（BetterSliding 参数重构 v2 只文档化，不改行为）。
> 来源：审核报告 P1-1（off-by-one）与 P2-4（循环上界与复查时机详情）；两者合并记录于本文档。
> 关联：`FineTuneFallback` 的 `more` / `less` 单轴 1px 累加偏移机制；P1-4（过冲语义，保持源代码判断）；P3-5（`BetterSlidingFail` 判负性，见 `.dev_doc/better-sliding-fail-node-semantics.md`）。
> 实现计划见 `.dev_doc/better-sliding-param-plan-v2.md`。

## 1. 问题摘要

`FineTuneFallback` 的偏移循环把 `BetterSlidingCheckQuantity` 的 `max_hit: 4` 当作「偏移次数」使用，但该值实际限制的是「识别成功次数（心跳数）」。由于循环中第 1 个心跳被「精确点击后的首次确认」占用，因此：

- 偏移最多可执行 4 次（`base±1` 到 `base±4`）；
- 但只有前 3 次偏移能被复查；
- **第 4 次偏移无论在画面上是否命中目标，都必然进入 `BetterSlidingFail`**。

即：**「4」既不是「4 次可验证偏移」，也不是「4 次点击」，而是「4 次心跳」**——预算按心跳配置却被当作重试次数使用，属典型 off-by-one。

## 2. 机制基础：`max_hit` 的真实语义

来自 Pipeline 协议 schema（`tools/schema/pipeline.schema.json:4212-4217`）：

> 该节点最多可被识别成功多少次。可选，默认 UINT_MAX，即无限制。
> 若超过该次数，其他 node 的 next 列表中的该 node 会被跳过，既不会被识别也不会被执行。

三个关键点：

1. 计数对象是**识别成功次数**（`BetterSlidingCheckQuantity` 被识别成功一次即 +1），不是动作执行次数，也不是「出现在 next 里」的次数。
2. 超过预算后，该节点在别人的 `next` 列表里被**跳过**——跳过不等于报错，而是「当作不存在，继续看下一个候选」。
3. 计数跨节点全局存续，只在显式 `ClearHitCount` 时归零。

在 BetterSliding 中，归零发生在每次运行开头的 `BetterSlidingClearMaxHit`（`assets/resource/pipeline/BetterSliding/Main.json:81-102`），它清的是 `BetterSlidingCheckQuantity` 与 `BetterSlidingMoveMouse` 两个节点（节点名常量见 `agent/go-service/bettersliding/nodes.go:23-24`）。

## 3. 节点链与计数归属

```text
BetterSlidingGetSliderMaxQuantity --next--> BetterSlidingFindEnd
BetterSlidingFindEnd                --next--> BetterSlidingPreciseClick          (Main.json:59-61)
BetterSlidingPreciseClick           --next--> BetterSlidingJumpBackNode          (Main.json:77-79，由 Go 恢复)
BetterSlidingJumpBackNode           --next--> [CheckQuantity, BetterSlidingFail]  (Main.json:132-135)
BetterSlidingCheckQuantity          --next--> (每个分支由 Go OverrideNext 决定)     (Main.json:222-228)
BetterSlidingCheckQuantity(nudge)   --next--> BetterSlidingReset2  --next--> BetterSlidingPreciseClick  (由 Go OverrideNext + Reset2 静态 next)
```

- `max_hit: 4` **只挂在 `CheckQuantity` 上**（`Main.json:202-204`）。
- `JumpBackNode`、`PreciseClick`、`Fail` 都**没有 `max_hit`**，因此不会被拦截，可以无限次经过。
- 所以「4」= 本次运行中 `CheckQuantity` 最多被识别成功 4 次 = 最多 4 次「数量读数心跳」。
- nudge 分支额外插入的 `BetterSlidingReset2` 同样**没有 `max_hit`**，只做一次复位滑动，**不消耗**这 4 次心跳预算，因此不影响下文的 off-by-one 结论。

注意一个易误读点：`CheckQuantity` 的识别是 `And(BetterSlidingGetSliderQuantity)`（`Main.json:205-212`）。计数加在 `CheckQuantity` 本身，子节点 `BetterSlidingGetSliderQuantity` 不计数、不限次。

## 4. 逐拍推演（精确到 hit 编号）

设 `FineTuneQuantity: false`、`FineTuneFallback: "more"`、`current < target`，基准精确点击坐标记为 `base`。

| 拍 | 触发链路 | CheckQuantity 计数 | 判定与动作 |
| --- | --- | --- | --- |
| — | FindEnd 算出 `base`，并 OverridePipeline 写入 `PreciseClick.target = base` | 0 | — |
| 1 | PreciseClick(base) → JumpBackNode → CheckQuantity | 记第 1 次 | 不微调 → 第 1 次 nudge：`target = base+1`，Reset2 复位 → PreciseClick |
| 2 | PreciseClick(base+1) → JumpBackNode → CheckQuantity | 记第 2 次 | 复查不匹配 → 第 2 次 nudge：`base+2`，Reset2 复位 → PreciseClick |
| 3 | PreciseClick(base+2) → JumpBackNode → CheckQuantity | 记第 3 次 | 复查不匹配 → 第 3 次 nudge：`base+3`，Reset2 复位 → PreciseClick |
| 4 | PreciseClick(base+3) → JumpBackNode → CheckQuantity | 记第 4 次 | 复查不匹配 → 第 4 次 nudge：`base+4`（复位与点击照常执行） |
| 5 | JumpBackNode → next 第一项 CheckQuantity | 已达 4 | 该节点被跳过（不识别、不执行） |
| 5b | 于是落到 next 第二项 BetterSlidingFail | — | 空叶节点，链路结束 |

结论：**偏移最多执行 4 次，但只有前 3 次能被复查。**

## 5. 数值推演：影响面

滑动条上 1px 与 1 个数量单位之间没有固定比例。设 `U` 为 1px 对应的数量单位数（`U = 滑条最大数量 / 滑条像素长度`，可能大于 1、约等于 1 或小于 1）。

### 场景 A：U 约等于 1，初始差值 d = target - current

| d | 结果 |
| --- | --- |
| 1 | 第 1 次 nudge 命中，第 2 拍复查通过 → Done（成功） |
| 2 | 第 2 次 nudge 命中，第 3 拍复查通过 → Done |
| 3 | 第 3 次 nudge 命中，第 4 拍复查通过 → Done |
| >=4 | 每拍只推进 1px，4 拍后仍差 >=1 → 第 5 拍被跳过 → 必然 Fail |

即 `more` / `less` 在当前设计下只能修正偏差 <= 3 的情况；超出后 100% 失败，且失败前还会多执行一次看不见结果的点击。

### 场景 B：U 约等于 3（宽滑条、大 maxQty）

初始 `d = 10`：

| 拍 | 当前值 | 判定 | 累计 px | 累计单位 |
| --- | --- | --- | --- | --- |
| 1 | target-10 | < target | +1 | +3 |
| 2 | target-7 | < target | +2 | +6 |
| 3 | target-4 | < target | +3 | +9 |
| 4 | target-1 | < target | +4 | +12 → target+2（过冲） |
| 5 | — | 被跳过 → Fail | — | — |

两层问题叠加：

- **过冲发生了但没人看到**：第 4 拍的复查被 `max_hit` 吃掉，`target+2` 这一错误状态不会被识别到。
- **仅把 `max_hit` 调大也不够**：若预算为 5，第 5 拍会看到 `current > target`；此时 `more` 的条件（`current < target`）不成立，按设计会路由 `Done`，于是把「数量错了 2 个」上报为成功（P1-4 裁决保留的过冲语义在 nudge 场景下被显著放大）。

### 场景 C：U 小于 1（滑条很长、maxQty 很小）

1px 常常完全没效果（整数点击坐标落在同一滑条档位）。此时 4 拍全部空转后 Fail。nudge 机制对这种滑条天然无效，与预算无关。

### 场景 D：int 阈值 + 混合行为

`FineTuneQuantity: 3`、`FineTuneFallback: "more"` 时，两条分支共用同一个 4 次心跳预算：

```text
hit#1  diff=10 → 不微调 → nudge#1
hit#2  diff=7  → 不微调 → nudge#2
hit#3  diff=4  → 不微调 → nudge#3
hit#4  diff=1  → 进入阈值 → Increase/Decrease 微调（repeat=1）
hit#5  被跳过 → Fail
```

第 4 拍触发的 Increase 点击已经执行，但其结果不会被验证。也就是说：**混合模式下「进入阈值后的第一次（也可能是唯一一次）微调」正好落在被吃掉的那一拍上**。这是允许混合行为的具体代价。

## 6. 与既有 Increase/Decrease 循环的关系

本问题属于**继承**而非新引入：

- 既有微调路径同样是 `CheckQuantity → Increase/Decrease → JumpBackNode → CheckQuantity`，同样吃 `max_hit: 4`，最后一拍同样不被验证（`Main.json:137-142` 的 `BetterSlidingFail` 注释即写着「如果路由至此，则说明微调失败（超过次数）」）。
- 区别在**粒度**：既有微调用 `repeat = clampClickRepeat(diff)`（`handlers.go` 的 Increase/Decrease 分支、`overrides.go` 的 `buildCheckQuantityBranchOverride`，单次最多 30 连点），一次心跳就能修掉大差值；nudge 是 1px/心跳，同样预算可覆盖的范围小一个数量级。
- 因此 `max_hit: 4` 大概率是为**粗粒度微调**标定的（配合 `BetterSlidingMoveMouse` 同为 4 次，见 `Main.json:254`），直接平移到 1px 步进语义并不合适。

## 7. 与 P1-4（过冲）、P3-5（Fail 判负）的交互

| 交互点 | 表现 |
| --- | --- |
| P1-4 过冲（可复查部分） | nudge#1 到 #3 的过冲会被下一拍复查到：`current > target` → 条件不满足 → Done + `TargetReachableOverrideEnable = true`。调用方可能以错误数量继续交易（计划按 P1-4 裁决保留此语义）。 |
| P1-4 过冲（隐形部分） | nudge#4 的过冲不会被复查到，直接 Fail。同一份错误状态，因落在第 4 拍而被归为失败而非过冲成功，行为不一致。 |
| P3-5 Fail 判负 | 若 `BetterSlidingFail` 作为空叶节点被框架判为链路正常结束，则 `runInternalPipeline` 的 `detail.Status.Success()` 为真，`applyOutcomeOverrides` 仍可能把 `TargetReachableOverrideEnable` 置 true——nudge 失败可能既没有错误日志，也没有对外的失败信号。 |

## 8. 可观测性影响

nudge 路径会打 `nudge_index` / `nudged_target` / `reset_side` / `reset_end`（后两者为 `BetterSlidingReset2` 的复位方向与终点），但**不会有第 4 次 nudge 之后的复查日志**。日志表现为：

```text
... nudge_index=4 nudged_target=[x,y] reset_side=start reset_end=[a,b,c,d]   <- 最后一次复位与点击，之后直接断流
（没有对应的 quantity matched / quantity below target 日志）
BetterSlidingFail 被命中（无任何子日志）
```

排查时容易误判为「OCR 读不到」「点击无效」「任务被外部中断」，而不是「预算恰好用光」。

## 9. 可选修复方向（暂不实施，供后续裁决）

| 方案 | 做法 | 优点 | 代价 |
| --- | --- | --- | --- |
| A. 保留现状 + 文档化（当前采用） | `max_hit` 不动，仅本文档记录 | 零改动、不碰既有行为、风险最小 | 有效偏移仅 3 次；第 4 次点击结果不可见；失败语义不清晰 |
| B. 抬高预算 | 把 `BetterSlidingCheckQuantity.max_hit` 改为 5 或更大，让每次 nudge 都有复查 | 改一行，语义立刻自洽 | 更频繁触发「过冲即 Done」（P1-4）；同时放大既有微调循环的尝试次数，属对既有行为的可见变更 |
| C. Go 侧显式收口（倾向） | 用 `a.preciseClickNudges` 维护预算：预算用尽时不再发起无法复查的点击，直接按策略路由 Done 或 Fail；预算写成 Go 常量并与 `max_hit` 交叉校验 | 行为确定、日志完整、不依赖框架跳过机制的隐式效果；可为 int 混合模式预留「最后一拍用来微调」的余量 | 需定义预算用尽时收 Done 还是 Fail，且需把常量与 pipeline `max_hit` 的耦合写进注释/文档 |

若选 C，还需确定：预算常量位置（与 `nudgeAxis` 并列比较自然）、是否允许配置、预算用尽时的收尾策略。

## 10. 待裁决清单（下次定稿用）

1. **预算语义**：把「4」定义为「4 个心跳」还是「4 次可验证偏移」？前者即现状，后者需要改 `max_hit` 或 Go 侧收口。
2. **是否容忍「最后一拍点击不可见」**：若容忍，是否至少在日志中显式打一行 `nudge_budget_exhausted` 以便排查？
3. **修不修 off-by-one**：方案 A（只文档化）/ B（调 `max_hit`）/ C（Go 侧显式预算）选哪条？
4. **若选 C**：预算用尽时收 `Done` 还是 `Fail`？是否给 int 混合模式预留额外一拍（nudge 预算与微调预算分开计）？
5. **过冲边界**：结合 P1-4，是否需要在「复查发现 `current > target` 且 `FineTuneFallback: more`」时至少记一条 Warn（不改路由，只提高可观测性）？

## 11. 相关文件与行号

| 文件 | 位置 | 说明 |
| --- | --- | --- |
| `tools/schema/pipeline.schema.json` | 4212-4217 | `max_hit` 协议语义 |
| `assets/resource/pipeline/BetterSliding/Main.json` | 202-204 | `BetterSlidingCheckQuantity` 的 `max_hit: 4` |
| `assets/resource/pipeline/BetterSliding/Main.json` | 205-212 | CheckQuantity 的 And 识别（子节点不计数） |
| `assets/resource/pipeline/BetterSliding/Main.json` | 222-228 | CheckQuantity 的 next 兜底列表 |
| `assets/resource/pipeline/BetterSliding/Main.json` | 127-142 | `BetterSlidingJumpBackNode` 与 `BetterSlidingFail` |
| `assets/resource/pipeline/BetterSliding/Main.json` | 81-102 | `BetterSlidingClearMaxHit`（每次运行清零） |
| `assets/resource/pipeline/BetterSliding/Main.json` | 254 | `BetterSlidingMoveMouse` 的 `max_hit: 4`（印证原设计意图） |
| `agent/go-service/bettersliding/handlers.go` | `handleCheckQuantity` / `handleFindEnd` | 分支路由与精确点击坐标写入 |
| `agent/go-service/bettersliding/overrides.go` | `buildCheckQuantityBranchOverride` | 既有 Increase/Decrease 的 repeat 机制 |
| `agent/go-service/bettersliding/nodes.go` | 23-24 | `ClearMaxHit` / `JumpBackNode` 常量 |

## 12. 当前结论

- 本问题**本次不修复**，仅在本文档记录；实现计划中保持「Go 侧不自建计数上限、仅递增日志索引」的约定。
- 实现 `FineTuneFallback` 时不要顺手把 `max_hit` 调大，也不要隐式改变既有 Increase/Decrease 循环的预算。
- 若后续选择方案 C，只需在 `handleNoFineTune` 增加一次上界判断，不影响 nudge 的其余设计。
