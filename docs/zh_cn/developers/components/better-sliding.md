# 开发手册 - BetterSliding 参考文档

该CustomAction支持对滑块进行滑动，支持滑动到指定数值

![BetterSliding示例](https://github.com/user-attachments/assets/27365f2c-b1a5-43cb-8ff6-d75d506716e2)

如上图所示，可通过`SwipeButton`实现滑动，并通过`DecreaseButton`与`IncreaseButton`进行精确操作

> [!note]
> 部分滑条在可滑动数量为1时会隐藏，请注意处理该种情况。

## 仅滑动模式

适合滑动到最大/最小的情景，参数如下。如需精确控制数量，请跳转下文[指定数量模式](#指定数量模式)。

### 参数说明

| 字段          | 类型     | 必填 | 说明                                                                                                                                      |
| ------------- | -------- | ---- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction`   | `string` | 是   | 滑动方向。支持 `left` / `right` / `up` / `down`。                                                                                         |
| `SwipeButton` | `string` | 否   | 自定义滑块模板路径。提供时覆盖 `BetterSlidingSwipeButton` 节点的默认模板。默认 `""`（使用共享默认模板 `BetterSliding/SwipeButton.png`）。 |

> [!note]
> Custom内部在对`SwipeButton`进行匹配时，`GreenMask`设置为`true`，涂绿方式可参考默认模板

### 示例

```json
"SomeTaskSwipeToMax": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "BetterSliding",
            "custom_action_param": {
                "Direction": "right",
                "SwipeButton": "BetterSliding/SwipeButton.png"
            }
        }
    }
}
```

## 指定数量模式

> [!important]
> 在CustomAction执行前，请确保滑块位于初始值，且初始值为1。否则将无法计算滑块在最小与最大的位置偏差，导致数量调整失效。

### 参数说明

#### 可在 `attach` 中传入的参数

以下 4 个字段推荐通过调用节点的 `attach` 传入，`attach` 优先级高于 `custom_action_param` 中的同名字段。

| 字段                      | 类型            | 必填 | 说明                                                                                                                                                                          |
| ------------------------- | --------------- | ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Target`                  | `int`（正整数） | 是   | 目标数量。最终希望滑到的档位值，必须大于 0。                                                                                                                                  |
| `TargetType`              | `string`        | 否   | 如何解释 `Target`。`"Value"`（默认）：绝对离散计数；`"Percentage"`：`maxQuantity` 的百分比（1–100），四舍五入后钳制到 `[1, maxQuantity]`。                                    |
| `TargetReverse`           | `bool`          | 否   | 为 `true` 时从范围远端计算目标：Value 模式为 `maxQuantity - Target`；Percentage 模式为 `round(maxQuantity * (100 - Target) / 100)`，钳制到 `[1, maxQuantity]`。默认 `false`。 |
| `FinishAfterPreciseClick` | `bool`          | 否   | 为 `true` 时精确点击后直接返回成功，不再进入数量校验与微调流程。默认 `false`。                                                                                                |

> [!note]
> `TargetType` 与 `TargetReverse` 的组合计算逻辑：
>
> | TargetType     | TargetReverse | 有效目标                                                               |
> | -------------- | ------------- | ---------------------------------------------------------------------- |
> | `"Value"`      | `false`       | `Target`（原值）                                                       |
> | `"Value"`      | `true`        | `maxQuantity - Target`（不钳制，可能 < 1）                             |
> | `"Percentage"` | `false`       | `round(maxQuantity × Target / 100)`，钳制到 `[1, maxQuantity]`         |
> | `"Percentage"` | `true`        | `round(maxQuantity × (100 - Target) / 100)`，钳制到 `[1, maxQuantity]` |

#### 仅能通过 `custom_action_param` 传入的参数

除上述 4 个字段外，其余参数都只能从 `custom_action_param` 读取：

| 字段                      | 类型                    | 必填 | 说明                                                                                                                          |
| ------------------------- | ----------------------- | ---- | ----------------------------------------------------------------------------------------------------------------------------- |
| `Direction`               | `string`                | 是   | 滑动方向。指定"最大值所在方向"，支持 `left` / `right` / `up` / `down`。                                                       |
| `Quantity.Box`            | `int[4]`                | 是   | 当前数量 OCR 区域，格式 `[x, y, w, h]`。                                                                                      |
| `IncreaseButton`          | `string` 或 `int[2\|4]` | 是   | "增加数量"按钮。推荐传模板路径（阈值固定 `0.8`），也可传坐标 `[x, y]` 或 `[x, y, w, h]`。                                     |
| `DecreaseButton`          | `string` 或 `int[2\|4]` | 是   | "减少数量"按钮。格式同 `IncreaseButton`。                                                                                     |
| `MaxTarget.Box`           | `int[4]`                | 否   | OCR 区域，用于读取物品的最大可用数量（如可购买/可出售数量），格式 `[x, y, w, h]`。缺失时使用滑块终点值作为回退。              |
| `Quantity.Filter`         | `object`                | 否   | 当前数量 OCR 的颜色过滤参数，适合数字颜色稳定但背景干扰较多的场景。                                                           |
| `MaxTarget.Filter`        | `object`                | 否   | 最大目标数量 OCR 的颜色过滤参数。仅在显式提供 `MaxTarget` 时使用。                                                            |
| `Quantity.OnlyRec`        | `bool`                  | 否   | 是否为数量 OCR 节点启用 `only_rec`。默认 `false`。                                                                            |
| `MaxTarget.OnlyRec`       | `bool`                  | 否   | 是否为 `BetterSlidingGetMaxTarget` 的 OCR 节点启用 `only_rec`。仅在显式提供 `MaxTarget` 时使用。                              |
| `GreenMask`               | `bool`                  | 否   | 使用模板路径定位按钮时，是否对模板匹配启用绿色掩膜过滤。默认 `false`。对`IncreaseButton`与`DecreaseButton`生效                |
| `CenterPointOffset`       | `int[2]`                | 否   | 相对滑块识别框中心点的点击偏移 `[x, y]`，负数向左/上，正数向右/下。默认 `[-10, 0]`。                                          |
| `ClampTargetToMax`        | `bool`                  | 否   | 为 `true` 时，若目标超过 `maxQuantity` 则自动钳制为 `maxQuantity` 继续执行，而非直接失败。默认 `false`。                      |
| `SwipeButton`             | `string`                | 否   | 自定义滑块模板路径，覆盖 `BetterSlidingSwipeButton` 节点的默认模板。默认 `""`（使用共享默认模板）。                           |
| `ExceedingOverrideEnable` | `string`                | 否   | 当解析后的目标超出可滑动范围时，将指定 Pipeline 节点的 `enabled` 设为 `true`，然后返回成功。默认 `""`（禁用，动作直接失败）。 |

### 示例

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
                    "Box": [340, 430, 200, 140]
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
