# 开发手册 - BetterSliding 参考文档

`BetterSliding` 是一个通过 `Custom` 动作类型调用的 go-service 自定义动作，用于处理"拖动滑条选择数量，但目标值是离散档位"的界面。

适合下面这类场景：

- 先拖到大致位置，再通过 `+` / `-` 按钮微调数量；
- 拖条本身没有稳定的固定坐标，但滑块模板可以识别；
- 当前数量可以通过 OCR 读出，且界面最大值会随库存或条件变化。

当前实现位于：

- Go 动作包：`agent/go-service/bettersliding/`
- 包内注册：`agent/go-service/bettersliding/register.go`
- go-service 总注册入口：`agent/go-service/register.go`
- 公共 Pipeline：`assets/resource/pipeline/BetterSliding/Main.json` 与 `Helper.json`
- 测试 Pipeline：`assets/resource/pipeline/BetterSliding/Test.json`
- 现有接入示例：`assets/resource/pipeline/AutoStockpile/Purchase.json` 中的 `AutoStockpileSwipeSpecificQuantity`

其中 `agent/go-service/bettersliding/` 已按职责拆分为多个文件：

| 文件           | 作用                                       |
| -------------- | ------------------------------------------ |
| `types.go`     | 参数结构、动作类型、运行期状态与包级类型   |
| `params.go`    | 参数解析与归一化                           |
| `nodes.go`     | 公共动作名、内部节点名与 override key 常量 |
| `handlers.go`  | `Run()` 分发、各阶段处理函数、状态重置     |
| `overrides.go` | Pipeline override 构造逻辑                 |
| `ocr.go`       | typed-first 的识别框/数量读取辅助逻辑      |
| `normalize.go` | 按钮参数归一化与基础计算辅助               |
| `register.go`  | 向 go-service 注册 `BetterSliding` 动作    |

## 测试 Pipeline：`Test.json`

`assets/resource/pipeline/BetterSliding/Test.json` 是 `BetterSliding` 的手动回归测试任务集合。修改 `agent/go-service/bettersliding/`、`BetterSliding/Main.json`、`Helper.json` 或相关参数解析逻辑后，至少应手动跑一次该测试任务。

当前入口节点为 `BetterSlidingTest`。文件中的说明要求在**据点管理**执行，并保证当前界面的可交易数量大致在 **1k ~ 3k**，这样既能覆盖普通目标值，也能覆盖按百分比和越界分支。

它的大致结构如下：

- `BetterSlidingTest`：测试入口；
- `__BS-T-2`：先将滑条向左归位，尽量把每个用例的起始状态统一；
- `__BS-T-1`：从统一起点分发到各个测试用例；
- `__BS-T-3` / `__BS-T-4` / `__BS-T-5`：用于标记退出、越界路由成功、越界路由失败等结果。

当前内置的测试场景包括：

| 节点     | 目的                                                                             |
| -------- | -------------------------------------------------------------------------------- |
| `__BS-1` | 普通 Value 模式，目标值 `325`                                                    |
| `__BS-2` | `TargetReverse: true`，验证“保留 325”这类反向目标                                |
| `__BS-3` | `TargetType: "Percentage"`，验证按 `10%` 计算目标                                |
| `__BS-4` | `Percentage + TargetReverse`，验证“保留 10%”                                     |
| `__BS-5` | `FinishAfterPreciseClick: true`，验证精确点击后直接结束                          |
| `__BS-6` | 超大目标值 `10000` + `ExceedingOverrideEnable`，验证越界时是否正确路由到兜底节点 |

这份 `Test.json` 主要用于验证以下几类能力是否仍然正常：

- 滑条归位、拖到最大值、再次识别终点这一整套基础流程；
- Value / Percentage / Reverse 三类目标解释逻辑；
- 精确点击后继续微调，或直接结束这两种收尾路径；
- 目标超出上限时，`ExceedingOverrideEnable` 的分支路由是否符合预期。

如果你给 `BetterSliding` 增加了新参数语义或新的分支行为，建议同步在 `Test.json` 中补一个对应场景，并在此文档中补充说明，避免测试覆盖与实现演进脱节。

## 执行模式

`BetterSliding` 当前有两种执行模式：

1. **对外调用模式**：当业务任务以 `custom_action: "BetterSliding"` 调用它时，Go 侧会自动构造内部 Pipeline override，并从 `BetterSlidingMain` 开始执行整条内部节点链。
2. **内部节点模式**：在当前节点本身就是 `BetterSlidingMain`、`BetterSlidingFindStart`、`BetterSlidingGetMaxQuantity`、`BetterSlidingFindEnd`、`BetterSlidingCheckQuantity`、`BetterSlidingDone` 之一时，Go 侧会直接处理该阶段逻辑。

业务接入方通常只需要传一次 `custom_action_param`，**不需要**手动串起内部节点。

## 它是怎么工作的

`BetterSliding` 不是"按固定百分比滑到某个位置"，而是一个**先探测、再计算、再微调**的流程。

整体步骤如下：

1. 识别滑块当前位置，记录滑动起点。
2. 将滑块拖到最大值。
3. OCR 识别当前最大可选数量。
4. 再次识别滑块位置，记录滑动终点。
5. 根据 `Target` 与 `maxQuantity` 计算精确点击位置。
6. 点击该位置。
7. OCR 再次识别当前数量；若仍不等于目标值，则通过加减按钮微调。
8. 数量与目标一致后结束。

其中第 5 步的精确点击位置按线性插值计算：

```text
numerator = Target - 1
denominator = maxQuantity - 1
clickX = startX + (endX - startX) * numerator / denominator
clickY = startY + (endY - startY) * numerator / denominator
```

计算出的 `[clickX, clickY]` 会被动态写入公共节点 `BetterSlidingPreciseClick` 的 `action.param.target`。

## 调用方式

在业务 Pipeline 中，像普通 `Custom` 动作一样调用即可。示例采用 MaaFramework Pipeline 协议 v2 写法。

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "GreenMask": false,
                "Target": 1,
                "Quantity": {
                    "Box": [360, 490, 110, 70],
                    "OnlyRec": false
                },
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "CenterPointOffset": [-10, 0]
            }
        }
    }
}
```

## 参数说明

`BetterSliding` 的参数可以分成两类：

1. **可通过调用节点 `attach` 传入的参数**：适合在复用同一套 `custom_action_param` 时按节点动态覆盖；
2. **仅能通过 `custom_action_param` 传入的参数**：属于动作本体配置，不会从 `attach` 读取。

### 可在 `attach` 中传入的参数

下表中的 4 个字段，既可以写在 `custom_action_param` 中，也可以由调用节点的 `attach` 覆盖；在对外调用模式下，`attach` 优先级更高。

| 字段                      | 类型            | 必填 | 说明                                                                                                                                                                                                                  |
| ------------------------- | --------------- | ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Target`                  | `int`（正整数） | 是\* | 目标数量。最终希望调到的档位值，正常模式下必须大于 0。仅滑动模式下忽略。若目标值需要按节点动态变化，推荐通过 `attach.Target` 传入。                                                                                   |
| `TargetType`              | `string`        | 否   | 如何解释 `Target`。`"Value"`（默认）：绝对离散计数。`"Percentage"`：`maxQuantity` 的百分比（1–100），四舍五入后钳制到 `[1, maxQuantity]`。若同一套节点需按调用点切换目标解释方式，推荐通过 `attach.TargetType` 传入。 |
| `TargetReverse`           | `bool`          | 否   | 为 `true` 时反向计算目标：Value 模式为 `maxQuantity - target`；Percentage 模式为 `round(maxQuantity * (100 - target) / 100)`。默认 `false`。若是否反向取值取决于调用场景，推荐通过 `attach.TargetReverse` 传入。      |
| `FinishAfterPreciseClick` | `bool`          | 否   | 为 `true` 时，精确点击后直接返回成功，不再进入数量校验与微调流程。默认 `false`。若是否跳过微调取决于调用场景，推荐通过 `attach.FinishAfterPreciseClick` 传入。                                                        |

### 仅能通过 `custom_action_param` 传入的参数

除上表 4 个字段外，其余参数当前都只能从 `custom_action_param` 读取：

| 字段                      | 类型                    | 必填 | 说明                                                                                                                                                          |
| ------------------------- | ----------------------- | ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GreenMask`               | `bool`                  | 否   | 使用模板路径定位按钮时，是否对模板匹配启用绿色掩膜过滤。默认 `false`。                                                                                        |
| `Quantity.Box`            | `int[4]`                | 是\* | 当前数量 OCR 区域，格式固定为 `[x, y, w, h]`。仅滑动模式下忽略。                                                                                              |
| `QuantityFilter`          | `object`                | 否   | 数量 OCR 的可选颜色过滤参数，适合数字颜色稳定但背景干扰较多的场景。                                                                                           |
| `Quantity.OnlyRec`        | `bool`                  | 否   | 是否为数量 OCR 节点启用 `only_rec`。当前默认值为 `false`；若显式传入，则按传入值覆盖。Go 侧仍只从 `Results.Best.AsOCR().Text` 读取数量文本。                  |
| `Direction`               | `string`                | 是   | 拖动方向，支持 `left` / `right` / `up` / `down`。Go 侧会先去掉首尾空白并转成小写后再校验。                                                                    |
| `IncreaseButton`          | `string` 或 `int[2\|4]` | 是\* | "增加数量"按钮。可传模板路径，也可传坐标。仅滑动模式下忽略。                                                                                                  |
| `DecreaseButton`          | `string` 或 `int[2\|4]` | 是\* | "减少数量"按钮。可传模板路径，也可传坐标。仅滑动模式下忽略。                                                                                                  |
| `CenterPointOffset`       | `int[2]`                | 否   | 相对滑块识别框中心点的点击偏移，默认 `[-10, 0]`。                                                                                                             |
| `ClampTargetToMax`        | `bool`                  | 否   | 为 `true` 时，若目标超过识别到的 `maxQuantity`，自动将目标值钳制为 `maxQuantity` 并继续，而非直接失败。默认 `false`（超过上限时直接失败）。                   |
| `SwipeButton`             | `string`                | 否   | 自定义滑块模板路径。提供时覆盖 `BetterSlidingSwipeButton` 节点的默认模板。路径相对于 `resource/image/` 目录。默认 `""`（使用共享默认模板）。                  |
| `ExceedingOverrideEnable` | `string`                | 否   | 当解析后的目标超出可滑动范围时，将指定 Pipeline 节点的 `enabled` 设为 `true`，然后返回成功。用于目标无法到达时触发降级分支。默认 `""`（禁用，动作直接失败）。 |

\* 正常模式下必填；仅滑动模式下忽略。

`CenterPointOffset` 用于微调 `BetterSlidingPreciseClick` 的落点。格式固定为 `[x, y]`：

- `x` 为水平方向偏移，负数表示向左，正数表示向右；
- `y` 为垂直方向偏移，负数表示向上，正数表示向下；
- 不传时默认使用 `[-10, 0]`，即相对滑块中心向左偏移 10 像素。

### `QuantityFilter`

`QuantityFilter` 是一个**可选增强项**。不传时，仅表示不启用 `BetterSlidingGetQuantity` 的颜色过滤预处理；传入后，会先对该 OCR 结果做颜色过滤，再识别数字。

最小示例：

```json
"QuantityFilter": {
    "method": 4,
    "lower": [0, 0, 0],
    "upper": [255, 255, 255]
}
```

约束与限制：

- `lower` / `upper` 必须同时存在，且长度必须一致；
- 通道数量必须与 `method` 匹配：`4`（RGB）和 `40`（HSV）需要 3 个通道，`6`（GRAY）需要 1 个通道；
- 当前仅支持**单组**颜色阈值，不支持 `[[...], [...]]` 这种多段范围；
- 可以把它理解为对数量区域先做一次按颜色的"近似二值化"，尽量只留下目标数字再交给 OCR；
- 如果干扰数字和目标数字颜色完全一致，`QuantityFilter` 也无法从根本上区分，这时仍应优先收紧 `Quantity.Box`；
- `QuantityFilter` 只是增强 OCR 预处理，不是 `Quantity.Box` 选区不准时的替代品。

### `IncreaseButton` / `DecreaseButton` 的写法

这两个字段支持两种形式：

#### 1. 传模板路径（推荐）

```json
"IncreaseButton": "AutoStockpile/IncreaseButton.png"
```

此时 go-service 会动态把对应分支节点改成 `TemplateMatch + Click`：

- 模板阈值固定为 `0.8`
- 顶层参数 `GreenMask` 默认值为 `false`，进入 TemplateMatch 协议层后映射为 `green_mask`
- 点击时使用 `target: true`，并附带 `target_offset: [5, 5, -10, -10]`

这种方式通常比硬编码坐标更稳，推荐优先使用。

#### 2. 传坐标

支持：

- `[x, y]`
- `[x, y, w, h]`

如果传入 `[x, y]`，内部会自动补成 `[x, y, 1, 1]`。

另外，实际从 JSON 反序列化进入 Go 后，这类数组可能表现为 `[]float64` 或 `[]any`，当前实现会自动归一化为整数数组；但如果长度既不是 `2` 也不是 `4`，动作会直接报错返回失败。

## 仅滑动模式

如果你只需要将滑块拖到最大位置，而不需要读取数量或进行微调，可以使用**仅滑动模式**。

仅滑动模式在 `custom_action_param` **不传任何参数**，或**仅传入 `Direction` 和/或 `SwipeButton`** 时自动激活。

在此模式下，`BetterSliding` 执行 `SwipeToMax` 拖动后立即返回成功，跳过 OCR、比例点击和微调。`SwipeButton` 仍然有效——你可以在仅滑动模式下提供自定义滑块模板路径；`Direction` 则用于指定"最大值所在方向"。

最小示例：

```json
"SomeTaskSwipeToMax": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right"
            }
        }
    }
}
```

使用自定义滑块模板：

```json
"SomeTaskSwipeToMax": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "SwipeButton": "MyFeature/MySlider.png"
            }
        }
    }
}
```

## 附加参数

`BetterSliding` 当前会从调用节点的 `attach` 块读取这 4 个字段：`Target`、`TargetType`、`TargetReverse` 和 `FinishAfterPreciseClick`。

对于这 4 个字段，**推荐优先通过 `attach` 传入**，而不是直接硬编码在 `custom_action_param` 中。这样做的好处是：

- 同一套 `BetterSliding` 参数可以被多个节点复用，只在 `attach` 中切换目标；
- 调整目标值、目标解释方式或是否反向时，不需要复制整段 `custom_action_param`；
- 更符合当前实现的运行时覆盖方式，维护时也更容易看出"这一调用点真正想调到什么目标"。

当前实现的优先级如下：

1. `runInternalPipeline` 会先读取调用节点的 `attach`；
2. 如果 `attach` 中存在 `Target`、`TargetType`、`TargetReverse`、`FinishAfterPreciseClick`，就把它们覆盖进 `custom_action_param`；
3. 然后再按覆盖后的结果解析整套 `BetterSliding` 参数。

也就是说，在**对外调用模式**（即业务节点通过 `custom_action: "BetterSliding"` 调用）下，`attach` 中这 4 个字段的优先级**高于** `custom_action_param` 中的同名字段。

注意：

- 只有这 4 个键会被读取，其他 `attach` 字段会被忽略；
- 这是对外调用模式的行为；如果当前节点本身已经是 `BetterSlidingMain` 等内部节点，Go 侧不会再做 `attach` 合并；
- 如果 `attach` 缺失、节点 JSON 读取失败，或其中某个字段类型不合法，对应字段会回退到原始 `custom_action_param` 的值。

### `TargetType`

| 取值           | 行为                                                                                           |
| -------------- | ---------------------------------------------------------------------------------------------- |
| `"Value"`      | （默认）`Target` 是绝对离散计数。                                                              |
| `"Percentage"` | `Target` 是百分比（1–100）。Go 侧计算 `round(max * t/100)` 并将结果钳制到 `[1, maxQuantity]`。 |

### `TargetReverse`

为 `true` 时，从范围的**远端**计算目标：

- `Value` 模式：有效目标 = `maxQuantity - Target`
- `Percentage` 模式：有效目标 = `round(maxQuantity * (100 - Target) / 100)`，钳制到 `[1, maxQuantity]`

对于 `Value + TargetReverse`，计算结果**不会**被钳制——可能小于 `1`。此时动作会失败，除非设置了 `ExceedingOverrideEnable`（见下文）。

### `FinishAfterPreciseClick`

为 `true` 时，`BetterSliding` 在执行完精确点击后**直接返回成功**，不再进入 `BetterSlidingCheckQuantity` 校验数量，也不再触发 `BetterSlidingIncreaseQuantity` / `BetterSlidingDecreaseQuantity` 微调。

**与 `SwipeOnlyMode` 的区别**：

- `SwipeOnlyMode`：跳过比例点击和微调，仅拖到最大值后返回。适用于只需要"拖到尽头"的场景。
- `FinishAfterPreciseClick`：仍执行比例点击，但跳过后续的 OCR 校验和微调。适用于"位置偏差可接受，只需点击到大致位置"的场景。

**注意**：

- 启用 `FinishAfterPreciseClick` 后，最终实际数量**不保证**与 `Target` 完全一致；
- `ExceedingOverrideEnable` 和 `ClampTargetToMax` 仍在精确点击之前生效，不受此参数影响。

### 附加参数示例

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "Quantity": {
                    "Box": [340, 430, 200, 140],
                    "OnlyRec": true
                },
                "QuantityFilter": {
                    "lower": [20, 150, 150],
                    "upper": [35, 255, 255],
                    "method": 40
                }
            }
        }
    },
    "attach": {
        "Target": 50,
        "TargetType": "Percentage",
        "TargetReverse": false
    }
}
```

在上面的例子中，`Target` 从 `attach` 读取并注入到顶层 `Target` 字段，因此滑块目标是当前最大值的 50%。

下面是一个 `FinishAfterPreciseClick` 通过 `attach` 传入的例子：

```json
"SomeTaskClickAndLeave": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "Quantity": {
                    "Box": [340, 430, 200, 140],
                    "OnlyRec": true
                }
            }
        }
    },
    "attach": {
        "Target": 799,
        "FinishAfterPreciseClick": true
    }
}
```

在上面的例子中，`FinishAfterPreciseClick` 从 `attach` 读取并注入，因此点击使得目标数量接近799后直接返回成功，不再执行数量校验和微调。

如果某个业务节点的目标值、目标类型和正反向逻辑都是固定不变的，直接写在 `custom_action_param` 里也可以；但只要这些值存在“同一套节点配置要按调用点切换”的需求，就应优先改为通过 `attach` 传入。

## 超出范围覆盖启用

当解析后的目标超出可滑动范围时，此参数决定失败时的行为。

### 超出范围条件

- `解析后的目标 > maxQuantity` —— 总是超出范围。
- `TargetType = "Value"` 且 `TargetReverse = true` 且 `maxQuantity - target < 1` —— 计算值为负或零，视为超出范围。

### 优先于 `ClampTargetToMax`

`ExceedingOverrideEnable` 在 `ClampTargetToMax` **之前**评估。当设置了 `ExceedingOverrideEnable` 时：

1. 如果超出范围：将指定 Pipeline 节点的 `enabled` 设为 `true`，流程直接分支到 `BetterSlidingDone`，`BetterSliding` 返回**成功**。
2. 如果在范围内：将指定 Pipeline 节点的 `enabled` 设为 `false`，正常流程继续（包括任何 `ClampTargetToMax` 逻辑）。

当**未**设置 `ExceedingOverrideEnable` 且目标超出范围（包括 `Value + TargetReverse` 产生 `< 1` 的情况），动作立即返回 **false**，`ClampTargetToMax` 不会被应用。

### 示例

```json
"SomeTaskAdjustWithFallback": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "ExceedingOverrideEnable": "SomeFallbackNode",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "Target": 1,
                "Quantity": {
                    "Box": [340, 430, 200, 140],
                    "OnlyRec": true
                },
                "QuantityFilter": {
                    "lower": [20, 150, 150],
                    "upper": [35, 255, 255],
                    "method": 40
                }
            }
        }
    },
    "post_delay": 0,
    "rate_limit": 0,
    "next": ["AutoStockpileRelayNodeSwipe"],
    "focus": {
        "Node.Action.Failed": "定量滑动失败，取消购买"
    }
}
```

文件位置：`assets/resource/pipeline/AutoStockpile/Purchase.json`（节点：`AutoStockpileSwipeSpecificQuantity`）

## 成功与失败条件

### 成功条件

- 能识别到滑块起点；
- 能成功拖到最大值；
- 能 OCR 出最大值与当前值；
- 目标值 `Target` 不大于最大值，或 `ClampTargetToMax` 为 `true`（此时目标值会被钳制为 `maxQuantity`）；
- 若识别到的 `maxQuantity` 为 `1`，且目标值最终也是 `1`（包括被 `ClampTargetToMax` 钳制后的情况），流程会直接分支到成功，不会再走比例点击；
- 若 `FinishAfterPreciseClick` 为 `true`，经过比例点击后直接返回成功，不再校验实际数量是否等于目标；
- 若 `FinishAfterPreciseClick` 为 `false`（默认），经过精确点击与微调后，当前值最终等于 `Target`。

### 常见失败条件

- `Quantity.Box` 不是 `[x, y, w, h]` 四元组；
- `Direction` 不是 `left/right/up/down` 之一；
- OCR 没有读到数字；
- 最大值 `maxQuantity` 小于 `Target`，且 `ClampTargetToMax` 为 `false`（默认值）；当上限只有 `1` 且目标值仍大于 `1` 时，也属于这一类失败；
- 加减按钮无法识别或无法点击；
- 微调次数过多仍未收敛。

当前实现会把单次微调点击次数限制在 `0 ~ 30` 之间，`BetterSlidingCheckQuantity` 的 `max_hit` 为 `4`。如果走满后仍未到目标值，就会失败并进入 `BetterSlidingFail`。

## 为什么还需要微调按钮

表面上看，既然已经根据起点、终点和最大值算出了精确点击位置，似乎不需要再点 `+` / `-`。

但实际界面里往往会有这些误差来源：

- 滑块模板识别框不是严格的几何中心；
- 触控区域和视觉位置不完全重合；
- 某些档位的实际映射并非完全均匀；
- OCR 或动画过渡导致点击后落点有轻微偏差。

所以当前实现采用的是：

> 先用比例点击快速靠近目标，再用加减按钮收尾。

这比"全靠加减按钮硬点"快得多，也比"只点一次比例位置"稳得多。

## 常见坑

- **把它当成普通滑动节点**：它本质上是一个完整子流程，不只是一次 `Swipe`。
- **`Direction` 填反**：会导致"滑到最大值"这一步本身就不成立。
- **OCR 框进了多个数字组**：例如 `12/99` 会被拼成 `1299`，不是自动取第一个数字。
- **`Quantity.Box` 截得太紧**：数字跳动或描边变化时 OCR 容易失败。
- **只给按钮坐标，不做识别兜底**：界面轻微偏移后就可能点歪。
- **滑块模板不通用**：不同界面滑块样式不一致时，公共模板可能失效。
- **目标值超过上限**：`Target > maxQuantity` 默认会直接失败。设置 `ClampTargetToMax: true` 可自动将目标值钳制为最大值继续执行，但需注意最终实际数量为 `maxQuantity`，而非原始 `Target`。
- **没有考虑冻结等待**：避免在内部流程之上叠加过多硬延迟；应依赖内置的 rate_limit 和 max_hit 机制来控制节奏。

## 自检清单

接入后，至少检查下面这些点：

1. 滑块模板 `BetterSliding/SwipeButton.png` 是否能稳定命中。
2. `Quantity.Box` 是否基于 **1280×720**，且 OCR 能稳定读出数字。
3. `Direction` 是否与"最大值所在方向"一致。
4. `IncreaseButton` / `DecreaseButton` 是否优先使用模板路径。
5. `Target` 是否有可能大于当前场景允许的最大值。
6. 若启用了 `ClampTargetToMax`，调用方是否能处理"实际数量可能小于原始 `Target`"的情况。
7. 失败分支是否有明确处理，例如提示、跳过或取消当前任务。

## 代码定位

如果需要继续追实现，建议按下面顺序看：

1. `agent/go-service/bettersliding/register.go`：确认动作注册名。
2. `agent/go-service/bettersliding/handlers.go`：看 `Run()` 如何区分"对外调用模式"和"内部节点模式"。
3. `agent/go-service/bettersliding/nodes.go`：看公共动作名、内部节点名与 override key 常量。
4. `agent/go-service/bettersliding/params.go`：看参数解析与归一化。
5. `agent/go-service/bettersliding/overrides.go`：看内部 Pipeline override、方向终点和按钮分支是怎么生成的。
6. `agent/go-service/bettersliding/ocr.go`：看 typed-first 的数量与识别框提取逻辑。
7. `agent/go-service/bettersliding/normalize.go`：看按钮参数归一化、点击次数限制和中心点计算。
8. `assets/resource/pipeline/BetterSliding/Main.json`：看公共节点默认配置，例如 `max_hit`、`pre_delay`、`post_delay`、`rate_limit`、默认 `next` 关系。
9. `assets/resource/pipeline/BetterSliding/Helper.json`：看基础识别节点配置。

## 相关文档

- [Custom 动作与识别参考文档](../custom.md)：了解 `Custom` 动作与识别的通用调用方式。
- [编码规范](../coding-standards.md)：了解 Pipeline / Go Service 的整体开发规范。
- [开发者文档索引](../README.md)：查看阅读路线，以及工具、测试、任务文档入口。
