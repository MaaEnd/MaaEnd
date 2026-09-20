# BetterSliding 参数改造 —— String|Object 参数化（实现计划）

> 本计划定义 BetterSliding 参数体系的目标形态与落地步骤，作为本次实现的唯一依据。
> 参数契约见「参数契约」章节，改动范围见「改动清单」，符号定位见「符号索引」。

## 0. 落笔前的仓库核对

本计划落笔前已对当前工作树核对，结论：**所引用的 BetterSliding 相关文件在最近一次 fast-forward 中无任何变动**。

- 当前分支 `feat/bs` @ `791534a7`，工作树 clean（`git status --porcelain` 为空）。
- 最近一次 `pull: Fast-forward` 的落点为 `b2d795f3`。
- `git diff --stat b2d795f3 791534a7` 覆盖 926 个文件，其中与本次改造相关的路径改动为 **0**：
    - `agent/go-service/bettersliding/` —— 无改动
    - `assets/resource/pipeline/BetterSliding/` —— 无改动
    - `tools/parser/index.ts` —— 无改动
    - `docs/{zh_cn,en_us}/developers/components/better-sliding.md` —— 无改动
- 该区间内 `custom.action.schema.json` 仅新增枚举项 `DeliveryJobsResolveOngoingDepotAction`；BetterSliding 参数在 schema 中无定义（`grep IncreaseButton|SliderQuantity|SwipeButton tools/schema/` 结果为空）。
- 8 个 Go 文件与 3 个 Pipeline JSON 的行号已逐条复核（21 项符号抽查），本文行号可直接使用。

> 实施时若再发生 fast-forward，请先按「符号索引」重新核对行号。

## 1. 目标与成功标准

把 BetterSliding 的模板 / ROI / 点击坐标类参数，统一为「String 即节点引用、Object 即识别参数补丁」模型，并使 Helper 节点成为参数覆写的唯一落点。

成功标准：

1. 六个参数支持 `String | Object`（按钮额外支持 `int[2|4]`）；String 一律解释为**节点引用**，不做后缀判断。
2. 覆写只作用于 `recognition.param`，**禁止替换 recognition**（不写 `type`）。
3. 参数覆写到 Helper 节点，再由 `And all_of` 引用（按钮）或既有静态 `all_of`（SwipeButton / Quantity）生效。
4. 新增 `SliderQuantityFilter` / `AvailableQuantityFilter` 两个平级参数，默认不配置。
5. `OnlyRec` 与 `Filter` 子字段不再作为参数入口；非按钮参数不支持数组。
6. 中英文档同步；不新增 `*_test.go`，除非用户另行要求。
7. `go build ./...`、`go vet ./bettersliding/`、`pnpm format:go`、`pnpm format:md` 通过。**不主动跑 `pnpm check` / `pnpm test`**，交给 PR 的 CI（依据 AGENTS.md「验证按需，默认交给 CI」）。

## 2. 参数契约（冻结）

```text
SwipeButton             : String | Object          默认不配置
SliderQuantity          : String | Object          默认不配置
SliderQuantityFilter    : String | Object          默认不配置
AvailableQuantity       : String | Object          默认不配置
AvailableQuantityFilter : String | Object          默认不配置
IncreaseButton          : int[2] | int[4] | String | Object
DecreaseButton          : int[2] | int[4] | String | Object

String   → 节点引用。只取该节点的 recognition.param 作为识别参数补丁，
           绝不取其 type / recognition（禁止替换 recognition）
Object   → 识别参数补丁，内容即 recognition.param 的键值
int[2|4] → 坐标，仅按钮可用，直接写 action.param.target
数组     → 非按钮参数一律报错返回 false
```

约束：

- `Object` 内出现 `recognition` / `type` / `action` 键 → Error 日志 + 返回 `false`，不静默忽略。
- 唯一写入形态为 `{"recognition": {"param": P}}`；不写 `type`，由框架按「同类型继承」保留原节点类型与未提及字段。

## 3. 参数行为约定

| 编号 | 项 | 约定 |
| --- | --- | --- |
| D1 | String 判定 | **不做后缀判断**，一律视为节点引用 |
| D2 | 模板传递 | 通过 `{"template": "xxx.png"}` 传入 |
| D3 | 点击坐标传递 | `int[2\|4]` 直接改 `action.param.target`；不提供 `{"target": [...]}` 对象形态 |
| D4 | recognition 替换 | **禁止**。覆写仅限 `recognition.param` |
| D5 | Helper 落点 | 参数覆写到 Helper 对应 node，再由 `And all_of` 引用 |
| D6 | `SwipeButton` green_mask | 默认 `true`，可被补丁覆盖 |
| D7 | `IncreaseButton` / `DecreaseButton` green_mask | 同上，默认 `true`，可覆盖 |
| D8 | `SliderQuantity` | 允许经补丁更换识别参数；本层不做校验 |
| D9 | `OnlyRec` | 不作为参数入口，改由补丁直接写 `only_rec` |
| D10 | `Filter` 子字段 | 不作为参数入口 |
| D11 | Filter 参数 | `SliderQuantityFilter` / `AvailableQuantityFilter`，与 Quantity 同级，String\|Object，默认不配置 |
| D12 | color_filter 链接 | 由 Filter 参数设置；**优先级低于 Quantity 补丁自身声明的 `color_filter`** |
| D13 | Filter 未配置 | **不写** `color_filter` |
| D14 | Filter String 形态 | 与其它参数行为一致：读引用节点 param，**覆写内建 Filter 节点**（不把引用名直接填 `color_filter`） |
| D15 | `AvailableQuantityFilter` 无 `AvailableQuantity` | 仍写内建节点，Quantity 节点保持 `enabled:false`，**不告警** |
| D16 | 定量模式判据 | 任意一个 Filter 参数存在即视为定量模式（退出 swipe-only） |
| D17 | 数组语义 | 非按钮参数不支持数组 |

## 4. 参数 → 落点映射

沿用「注入 Helper 节点 + `And all_of` 引用」的既有范式。

| 参数 | 目标 Helper 节点 | 写入 | 引用来源 |
| --- | --- | --- | --- |
| `SwipeButton` | `BetterSlidingSwipeButton` | `recognition.param ← 补丁` | `Main.json` 既有 4 处 `all_of` |
| `SliderQuantity` | `BetterSlidingGetSliderQuantity` | 同上 | `Main.json` 既有 2 处 `all_of` |
| `SliderQuantityFilter` | `BetterSlidingSliderQuantityFilter` | 同上 | 经 Quantity 的 `color_filter` 字符串链接 |
| `AvailableQuantity` | `BetterSlidingGetAvailableQuantity` | 同上 + `enabled: true` | `Main.json` 既有流程 |
| `AvailableQuantityFilter` | `BetterSlidingAvailableQuantityFilter` | 同上 | 经 Quantity 的 `color_filter` 链接 |
| `IncreaseButton` | `BetterSlidingIncreaseButton` | 同上 | Go 动态给 `BetterSlidingIncreaseQuantity` 写 `And all_of` |
| `DecreaseButton` | `BetterSlidingDecreaseButton` | 同上 | 同上 |

## 5. 解析算法

```go
// resolveRecognitionParam 把 String|Object 归一为 recognition.param 补丁。
// String: 读引用节点的 recognition.param（不取 type）
// Object: 直用，禁止含 recognition / type / action
func resolveRecognitionParam(ctx *maa.Context, raw any) (map[string]any, error)

// extractRecognitionParam 从 GetNodeJSON 的规范化 JSON 中取 param，
// 兼容扁平（recognition:"OCR" + 顶层）与 v2（recognition:{type,param}）两形态。
func extractRecognitionParam(raw string) (map[string]any, error)

// resolveFilterPatch 返回内建节点名（供 color_filter 引用）+ 要写入的 override。
// 未配置时两者皆为空。
func resolveFilterPatch(ctx *maa.Context, raw any, builtin string) (string, map[string]any, error)

// applyColorFilter 给 OCR 补丁挂 color_filter；补丁自身已声明则不覆盖。
func applyColorFilter(patch map[string]any, filterNode string)
```

主流程：

```go
sqFilterNode, sqFilterOv, _ := resolveFilterPatch(ctx, params.SliderQuantityFilter,        nodeBetterSlidingSliderQuantityFilter)
aqFilterNode, aqFilterOv, _ := resolveFilterPatch(ctx, params.AvailableQuantityFilter,     nodeBetterSlidingAvailableQuantityFilter)

sqPatch, _ := resolveRecognitionParam(ctx, params.SliderQuantity)
applyColorFilter(sqPatch, sqFilterNode)

if params.presence.AvailableQuantity {
    aqPatch, _ := resolveRecognitionParam(ctx, params.AvailableQuantity)
    applyColorFilter(aqPatch, aqFilterNode)
    override[nodeBetterSlidingGetAvailableQuantity] = map[string]any{
        "enabled": true, "recognition": map[string]any{"param": aqPatch},
    }
} else {
    override[nodeBetterSlidingGetAvailableQuantity] = map[string]any{"enabled": false}
}

for k, v := range merge(sqFilterOv, aqFilterOv) { override[k] = v }
```

按钮分流（`resolveRecognitionParam` 只处理 String|Object，数组在更外层先分流）：

```go
switch v := raw.(type) {
case []any, []int:                                    // int[2] | int[4]
    coords := normalizeButton(v)                      // len 2 → [x,y,1,1]
    override[nextNode] = map[string]any{
        "action": map[string]any{"param": map[string]any{"target": coords}},
        "repeat": repeat,
    }

default:                                              // String | Object
    patch, _ := resolveRecognitionParam(ctx, v)
    if _, ok := patch["green_mask"]; !ok { patch["green_mask"] = true }
    helper := resolveButtonHelperNode(nextNode)

    override[helper] = map[string]any{
        "recognition": map[string]any{"param": patch},
    }
    override[nextNode] = map[string]any{
        "recognition": map[string]any{
            "type":  "And",
            "param": map[string]any{"all_of": []string{helper}, "box_index": 0},
        },
        "action": map[string]any{
            "type":  "Click",
            "param": map[string]any{"target": true, "target_offset": []int{5, 5, -10, -10}},
        },
        "repeat": repeat,
    }
}
```

**为何按钮模板形态必须经 And 包装**：`BetterSlidingIncreaseQuantity`（`Main.json:197-215`）没有 `recognition`，即默认 `DirectHit`。若把 `{"template": ...}` 写进它的 `recognition.param`，`parse_direct_hit_param` 只读 `roi_target`，`template` 被静默忽略。因此需用 `And all_of` 换成 TemplateMatch，并以 `box_index:0` 把 Helper 的框透传给 Quantity 节点（`Recognizer::and_` 末段 `result.box = sub_results[box_index].box`），再以 `target: true` 点击该框。

## 6. 优先级规则

| 关系 | 规则 | 实现位置 |
| --- | --- | --- |
| Quantity 补丁 `.color_filter` vs 对应 Filter 参数 | **Quantity 补丁优先** | `applyColorFilter` 的 `if has { return }` |
| 补丁 vs Helper 默认（`order_by` / `threshold` / 模板） | **补丁优先，Helper 兜底** | 只写 `param` 不写 `type`，框架以原节点 param 为 default |
| `green_mask` 补丁 vs 默认 `true` | 补丁优先 | `if !ok { patch["green_mask"] = true }` |
| 按钮 `repeat` vs 补丁 | `repeat` 最后由数量差写入，不可被参数覆盖 | 覆盖之后再写 `repeat` |

## 7. 改动清单

### 7.1 结构体（`types.go`）

| 符号 | 行 | 动作 |
| --- | --- | --- |
| `betterSlidingParam` | 10 | `SwipeButton` / `SliderQuantity` / `AvailableQuantity` 改为 `any`；新增 `SliderQuantityFilter` / `AvailableQuantityFilter any` |
| `betterSlidingParamPresence` | 30 | 新增两个 Filter 字段 |
| `quantityParam` | 49 | **删除** |
| `quantityFilterParam` | 56 | **删除** |
| `BetterSlidingAction` | 96 | `SliderQuantityBox` / `AvailableQuantityBox` / `SliderQuantityFilter` / `AvailableQuantityFilter` / `*OnlyRec` 替换为各参数补丁载体；`SwipeButton` 由 `string` 改 `any` |
| `buttonTarget` + `logValue` | 138 / 143 | 改为「坐标 / 补丁」二选一载体，`logValue` 同步 |

### 7.2 参数解析（`params.go`）

| 符号 | 行 | 动作 |
| --- | --- | --- |
| `detectBetterSlidingParamPresence` | 36 | 新增两个 `hasNonNullRawKey` |
| `parseBetterSlidingParam` | 86 | 不变 |
| `loadActionParams` | 101 | 不变 |
| `normalizeActionParams` | 121 | 删 `normalizeButtonParam` / `normalizeQuantityFilter` / `normalizeQuantityParam` 调用；改为四次 `resolveRecognitionParam` + 两次 `resolveFilterPatch` |
| `applyActionParams` | 283 | 字段同步 |
| `logParsedActionParams` | 311 | 删 Box / OnlyRec / Filter 明细（`314-315`、`320-323`），改为「补丁生效键 / 字节数」摘要 |
| `mergeAttachParams` | 369 | 不变 |

### 7.3 归一化（`normalize.go`）

| 符号 | 行 | 动作 |
| --- | --- | --- |
| `normalizeButton` | 22 | 保留（仅坐标路径使用） |
| `normalizeButtonParam` | 38 | **删除** |
| `normalizeCenterPointOffset` | 56 | 不变 |
| `normalizeQuantityFilter` | 73 | **删除** |
| `normalizeQuantityParam` | 102 | **删除** |
| `quantityFilterChannelCount` | 111 | **删除** |
| `isSwipeOnlyMode` | 327 | 追加 `!presence.SliderQuantityFilter && !presence.AvailableQuantityFilter`（D16） |
| 新增 | — | `resolveRecognitionParam` / `extractRecognitionParam` / `resolveFilterPatch` / `applyColorFilter` |

### 7.4 Override 构造（`overrides.go`）

| 符号 | 行 | 动作 |
| --- | --- | --- |
| `buildMainInitializationOverride` | 78 | 改签名（收四个补丁 + 两个 filter 节点名），重写 `102-167` |
| `buildCheckQuantityBranchOverride` | 172 | 判据由 `target.template != ""` 改为「值是否为 `int[]`」 |
| `overrideCheckQuantityBranch` | 200 | `buttonTarget` 参数改为新载体 |
| `resolveButtonHelperNode` | 213 | 不变 |
| `buildTemplateMatchButtonHelperOverride` | 232 | 泛化为 `{recognition:{param: patch}}`（`green_mask` 由调用方注入） |
| `buildTemplateMatchButtonOverride` | 243 | 保留（And 包装 + `box_index:0` + `target:true`） |

### 7.5 调用方（`handlers.go`）

| 位置 | 行 | 动作 |
| --- | --- | --- |
| `handleMain` Box 长度校验 | 75-86 | **删除** |
| `buildMainInitializationOverride` 调用 | 98 | 同步新签名 |
| `handleMain` Filter/Box 日志 | 159-187 | 删 `162-168`、`173-185` |
| `overrideCheckQuantityBranch` 调用 | 284, 333, 556, 578, 606, 669 | 传新载体（空载体表示无需识别补丁） |

### 7.6 不改动

- `ocr.go`：两处 `findRecognitionDetailByName` 按名反查依赖节点名；本次禁止替换 recognition，节点名恒定，反查路径不受影响。
- `nodes.go`、`register.go`。
- `assets/resource/pipeline/BetterSliding/Main.json`、`Helper.json` 结构。

## 8. 调用点迁移

| 源文件 | 站点 | 迁移 |
| --- | --- | --- |
| `BetterSliding/Test.json` | 8 | `Inc/Dec` 为 `int[4]` → 不改；`SQ.Box:[1090,535,100,30]` → `{"roi":[...]}` |
| `AutoStockStaple/General/Item.json` | 2 | `Inc/Dec` String 需按节点引用语义改写 → `{"template":"AutoStockpile/IncreaseButton.png"}`；`SQ:{Box:[375,508,36,24],OnlyRec:true}` → `{"roi":[...], "only_rec":true}` |
| `resource_adb/.../AutoStockStaple/General/Item.json` | 2 | 同上，roi `[304,540,54,39]` |
| `AutoStockpile/Purchase.json` | 2 | `Inc/Dec` → `{"template":...}`；`SQ:{Box:[360,484,100,38],Filter:{method:4,lower:[75,75,75],upper:[255,255,255]}}` → `{"roi":[...]}` + 顶层 `SliderQuantityFilter` |
| `resource_adb/.../AutoStockpile/Purchase.json` | 2 | 同上，roi `[303,516,111,47]`，HSV `method:40, lower:[20,150,150], upper:[35,255,255]` |
| `OutpostTrading/pipeline-template.jsonc` | 1（生成 6 处） | `Inc/Dec` → `{"template":...}`；`SQ/AQ` → `{"roi":"${...}", "only_rec":true}` |
| `OutpostTrading/pipeline-adb-template.jsonc` | 1 | 同上 |
| `BetterSliding/Main.json` | 7 | 内部节点无参数 → 无需改 |

迁移样例：

```jsonc
"custom_action_param": {
    "TargetQuantity": 999999,
    "ClampTargetToSliderMax": true,
    "FineTuneQuantity": 100,
    "FineTuneFallback": "less",
    "Direction": "right",
    "SwipeButton":             { "template": "BetterSliding/SwipeButton.png" },
    "SliderQuantity":          { "roi": [1107, 535, 74, 29], "only_rec": true },
    "SliderQuantityFilter":    { "method": 4, "lower": [75, 75, 75], "upper": [255, 255, 255] },
    "AvailableQuantity":       { "roi": [1073, 327, 119, 25], "only_rec": true },
    "AvailableQuantityFilter": { "method": 40, "lower": [20, 150, 150], "upper": [35, 255, 255] },
    "DecreaseButton":          { "template": "OutpostTrading/DecreaseButton.png" },
    "IncreaseButton":          { "template": "OutpostTrading/IncreaseButton.png" },
    "OutOfRangeOverrideEnable": "OutpostTradingReserveAlreadySatisfied",
    "TargetReachableOverrideEnable": "OutpostTradingReserveQuantityReached"
}
```

## 9. Filter 细则

### 9.1 内建节点形态

用户写 `{"method":4,"lower":[75,75,75],"upper":[255,255,255]}` 时，写入：

```jsonc
"BetterSlidingSliderQuantityFilter": {
    "recognition": { "param": { "method": 4, "lower": [75,75,75], "upper": [255,255,255] } }
}
```

`lower` / `upper` 写 1D 或 2D 均可（`get_and_check_array_or_2darray` 对 1D 自动包成一行）。未声明的键（`roi` / `count` / `order_by`）从 `Helper.json` 继承。

### 9.2 color_filter 的解析时机

`color_filter` 不在 `PipelineParser` 解析，而在 `Recognizer`：按名字 `get_pipeline_data` 找节点，要求该节点 `reco_type == ColorMatch`；找不到或类型不符都会 `LogError` 并让本次识别返回空。

### 9.3 已知边界（D14 的副作用）

Filter 的 String 指向非 ColorMatch 节点时不会报错：`resolveRecognitionParam` 只取 `param`，其 `expected` / `only_rec` 等键被 `parse_color_matcher_param` 忽略，最终落到 ColorMatch 默认区间。这与「参数层面不做校验」一致，**在双语文档明确写「Filter 的 String 引用应指向 ColorMatch 节点」**。

## 10. 工具链与文档同步

| 文件 | 改动 |
| --- | --- |
| `tools/parser/index.ts:136-137` | `BetterSliding` 模板登记：String **恒为 `taskRef`**；Object 内的 `template` 仍需按 `template` 登记（否则 `optimize_templates` 漏图）；新增两个 Filter 字段 |
| `tools/parser/index.ts:157-158` | `OutOfRangeOverrideEnable` / `TargetReachableOverrideEnable` 保持 `taskRef` |
| `tools/schema/custom.action.schema.json` | BetterSliding 目前**没有**参数级 schema（只有 enum 成员）；若新增参数 Schema，一并清理不再使用的 `OnlyRec` / `Filter` 规则 |
| `docs/zh_cn/developers/components/better-sliding.md` | 参数表改用新契约：String=节点引用、Object=识别参数补丁、两个新 Filter 入口、`green_mask` 可覆盖 |
| `docs/en_us/developers/components/better-sliding.md` | 同步 |
| `tools/optimize_templates/optimize_templates.json` | 模板路径改写后重跑登记 |

> `pnpm check` 对「String 指向不存在的节点」不报错（实测仅给 `warning: 检测到动态图片路径`），防线在 TS parser 的 `taskRef` 类型上，parser 必须同步改。

## 11. 验证计划

1. `cd agent/go-service && go build ./... && go vet ./bettersliding/`
2. `pnpm format:go`、`pnpm format:md`
3. 四个反例（手工或临时脚本，验证后删除）：①String 指向不存在节点；②Object 含 `recognition` / `type` / `action`；③非按钮参数传数组；④`color_filter` 指向非 ColorMatch
4. `assets/resource/pipeline/BetterSliding/Test.json` 增补引用 / Object / Filter 三组 case（该任务是 `enabled: true` 的串跑回归任务）
5. 人工跑 `BetterSlidingTest` 任务
6. `pnpm check` / `pnpm test` **不主动跑**，交给 PR 的 CI

## 12. 实施顺序

1. `types.go` —— 结构体与新载体
2. `normalize.go` —— 新增四个解析函数，删除三个旧函数
3. `params.go` —— presence / 归一化调用 / 日志
4. `overrides.go` —— 两处 builder 重写
5. `handlers.go` —— 校验与日志清理、调用同步
6. 每步后 `go build ./...`
7. Pipeline JSON 迁移（§8）
8. `tools/parser/index.ts` + 文档（§10）
9. `pnpm format:go` / `pnpm format:md`

## 13. 符号索引（实施时核对用）

| 文件 | 符号 | 行 |
| --- | --- | --- |
| types.go | `betterSlidingParam` | 10 |
| types.go | `betterSlidingParamPresence` | 30 |
| types.go | `quantityParam` | 49 |
| types.go | `quantityFilterParam` | 56 |
| types.go | `BetterSlidingAction` | 96 |
| types.go | `buttonTarget` / `logValue` | 138 / 143 |
| params.go | `detectBetterSlidingParamPresence` | 36 |
| params.go | `normalizeActionParams` | 121 |
| params.go | `applyActionParams` | 283 |
| params.go | `logParsedActionParams` | 311 |
| params.go | `mergeAttachParams` | 369 |
| normalize.go | `normalizeButton` | 22 |
| normalize.go | `normalizeButtonParam` | 38 |
| normalize.go | `normalizeQuantityFilter` | 73 |
| normalize.go | `normalizeQuantityParam` | 102 |
| normalize.go | `quantityFilterChannelCount` | 111 |
| normalize.go | `isSwipeOnlyMode` | 327 |
| overrides.go | `buildMainInitializationOverride` | 78 |
| overrides.go | `buildCheckQuantityBranchOverride` | 172 |
| overrides.go | `overrideCheckQuantityBranch` | 200 |
| overrides.go | `resolveButtonHelperNode` | 213 |
| overrides.go | `buildTemplateMatchButtonHelperOverride` | 232 |
| overrides.go | `buildTemplateMatchButtonOverride` | 243 |
| handlers.go | `handleMain` | 56 |
| handlers.go | `buildMainInitializationOverride` 调用 | 98 |
| handlers.go | `handleCheckQuantity` | 533 |
