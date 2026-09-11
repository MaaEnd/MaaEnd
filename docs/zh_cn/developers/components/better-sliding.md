# 开发手册 - BetterSliding 参考文档

该CustomAction支持对滑块进行滑动，支持滑动到指定数值

![BetterSliding示例](https://github.com/user-attachments/assets/27365f2c-b1a5-43cb-8ff6-d75d506716e2)

如上图所示，可通过`SwipeButton`实现滑动，并通过`DecreaseButton`与`IncreaseButton`进行精确操作

> [!note]
> 部分滑条在可滑动数量为1时会隐藏，请注意处理该种情况。

## 仅滑动模式

适合滑动到最大/最小的情景，参数如下。如需精确控制数量，请跳转下文[指定数量模式](#指定数量模式)。

### 参数说明

| 字段 | 类型 | 必填 | 说明 |
| ---------------------- | -------- | ---- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `Direction` | `string` | 是 | 滑动方向。支持 `left` / `right` / `up` / `down`。 |
| `SwipeButton` | `string` | 否 | 自定义滑块模板路径。提供时覆盖 `BetterSlidingSwipeButton` 节点的默认模板。默认 `""`（使用共享默认模板 `BetterSliding/SwipeButton.png`）。 |
| `ResetBeforeFindStart` | `bool` | 否 | 为 `true` 时，先向最小方向滑动复位，再匹配滑块起始位置并执行滑动。默认 `false`。 |

> [!note]
> Custom 内部匹配 `SwipeButton` 时固定开启绿色掩码（`green_mask: true`），涂绿方式可参考默认模板。该行为为默认行为，无需也不能通过参数关闭。

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
> 在CustomAction执行前，请确保滑块位于初始值，且初始值为1。否则将无法计算滑块在最小与最大的位置偏差，导致数量调整失效。若调用方无法保证滑块位于初始值，可设置 `ResetBeforeFindStart: true`，BetterSliding 会在匹配起始位置前先向最小方向滑动复位。

> [!note]
> 当解析后的目标数量严格大于滑条最大数量的 80% 时，BetterSliding 会在记录终点位置后、执行精确点击前，先向最小方向滑动复位一次，再从最小值起按比例精确点击，保证靠近最大端的档位也能稳定命中。目标数量等于最大数量时仍直接完成，不执行复位。该行为默认开启，无需额外参数。

### 参数说明

#### 可在 `attach` 中传入的参数

以下 6 个字段推荐通过调用节点的 `attach` 传入，`attach` 优先级高于 `custom_action_param` 中的同名字段。

| 字段 | 类型 | 必填 | 说明 |
| ------------------------- | --------------- | ---- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TargetQuantity` | `int`（正整数） | 是 | 目标数量。最终希望滑到的档位值，必须大于 0。 |
| `TargetQuantityType` | `string` | 否 | 如何解释 `TargetQuantity`。`"Value"`（默认）：绝对离散计数；`"Percentage"`：`availableQuantity` 的百分比（1–100），四舍五入后钳制到 `[1, availableQuantity]`。 |
| `ReverseTarget` | `bool` | 否 | 为 `true` 时从可用总量反向计算目标：Value 模式为 `availableQuantity - TargetQuantity`；Percentage 模式按剩余百分比计算。默认 `false`。 |
| `FineTuneQuantity` | `bool` 或 `int` | 否 | 精确点击后是否继续用 Increase/Decrease 微调。`true`（默认）：始终微调；`false`：一律不微调；整数 `N`（须 `>= 1`）：仅当 `abs(当前数量 − 目标数量) <= N` 时微调。 |
| `FineTuneFallback` | `string` | 否 | 仅在本次判定为「不微调」时生效，控制此时的行为。`"none"`（默认）：复检后直接收尾；`"more"` / `"less"`：按单轴 1px 累加偏移后复查，详见[不微调语义](#不微调语义)。 |
| `ResetBeforeFindStart` | `bool` | 否 | 为 `true` 时，在匹配滑条起始位置前先向最小方向滑动复位，保证后续记录到的起始位置为最小值。默认 `false`。 |

> [!warning]
> 旧参数 `FinishAfterPreciseClick` 已被移除，请改用 `FineTuneQuantity: false` + `FineTuneFallback: "none"` 表达「精确点击后不再微调」。仍传入 `FinishAfterPreciseClick` 时会被静默忽略（不告警、不校验），行为回落为默认的 `FineTuneQuantity: true`，即**继续微调**。

> [!note]
> `TargetQuantityType` 与 `ReverseTarget` 的组合计算逻辑：
>
> | TargetQuantityType | ReverseTarget | 有效目标 |
> | ------------------ | ------------- | ------------------------------------------------------------------------------------------ |
> | `"Value"` | `false` | `TargetQuantity`（原值） |
> | `"Value"` | `true` | `availableQuantity - TargetQuantity`（不钳制，可能 < 1） |
> | `"Percentage"` | `false` | `round(availableQuantity × TargetQuantity / 100)`，钳制到 `[1, availableQuantity]` |
> | `"Percentage"` | `true` | `round(availableQuantity × (100 - TargetQuantity) / 100)`，钳制到 `[1, availableQuantity]` |

#### 仅能通过 `custom_action_param` 传入的参数

除上述 6 个字段外，其余参数都只能从 `custom_action_param` 读取：

| 字段 | 类型 | 必填 | 说明 |
| ------------------------------- | ----------------------- | ---- | ------------------------------------------------------------------------------------------------------------------- |
| `Direction` | `string` | 是 | 滑动方向。指定"最大值所在方向"，支持 `left` / `right` / `up` / `down`。 |
| `SliderQuantity.Box` | `int[4]` | 是 | 当前滑条数量 OCR 区域，格式 `[x, y, w, h]`。 |
| `IncreaseButton` | `string` 或 `int[2\|4]` | 是 | "增加数量"按钮。推荐传模板路径（阈值固定 `0.8`），也可传坐标 `[x, y]` 或 `[x, y, w, h]`。 |
| `DecreaseButton` | `string` 或 `int[2\|4]` | 是 | "减少数量"按钮。格式同 `IncreaseButton`。 |
| `AvailableQuantity.Box` | `int[4]` | 否 | OCR 区域，用于读取物品可购买/可出售的总量。缺失时使用滑条终点值作为计算基准。 |
| `SliderQuantity.Filter` | `object` | 否 | 当前滑条数量 OCR 的颜色过滤参数。 |
| `AvailableQuantity.Filter` | `object` | 否 | 可用总量 OCR 的颜色过滤参数。仅在显式提供 `AvailableQuantity` 时使用。 |
| `SliderQuantity.OnlyRec` | `bool` | 否 | 是否为滑条数量 OCR 节点启用 `only_rec`。默认 `false`。 |
| `AvailableQuantity.OnlyRec` | `bool` | 否 | 是否为 `BetterSlidingGetAvailableQuantity` 启用 `only_rec`。 |
| `CenterPointOffset` | `int[2]` | 否 | 相对滑块识别框中心点的点击偏移 `[x, y]`，负数向左/上，正数向右/下。默认 `[-10, 0]`。 |
| `ClampTargetToSliderMax` | `bool` | 否 | 为 `true` 时，若目标超过 `sliderMaxQuantity`，则钳制为滑条最大可选数量继续执行。默认 `false`。 |
| `SwipeButton` | `string` | 否 | 自定义滑块模板路径，覆盖 `BetterSlidingSwipeButton` 节点的默认模板。默认 `""`（使用共享默认模板）。 |
| `OutOfRangeOverrideEnable` | `string` | 否 | 当解析后的目标超出可滑动范围时，将指定 Pipeline 节点的 `enabled` 设为 `true`，然后返回成功。默认 `""`。 |
| `TargetReachableOverrideEnable` | `string` | 否 | 当解析后的目标无需钳制且位于 `[1, sliderMaxQuantity]` 时，将指定 Pipeline 节点的 `enabled` 设为 `true`。默认 `""`。 |

> [!note]
> `SwipeButton`、`IncreaseButton`、`DecreaseButton` 使用模板路径匹配时，Custom 内部固定开启绿色掩码（`green_mask: true`），无需也无法通过参数关闭。请按默认模板的涂绿方式处理模板图片（不参与匹配的部分涂绿 RGB: (0, 255, 0)）。

### 不微调语义

`FineTuneFallback` 只在**本次判定为不微调**时生效。判定发生在 `BetterSlidingCheckQuantity` 读到当前数量之后：`FineTuneQuantity: false` 时始终判定为不微调；为整数 `N` 时，仅当 `abs(当前数量 − 目标数量) <= N` 才微调，否则判定为不微调。

判定为不微调后：

1. 读当前数量与目标数量比较，据此决定偏移方向。`current == target` 时一律直接收尾，不做任何偏移。
2. `FineTuneFallback: "none"`：不做偏移，经 `BetterSlidingCheckQuantity` 复检后路由 `BetterSlidingDone`。
3. `FineTuneFallback: "more"`：当 `current < target` 时，把精确点击坐标朝 **Start → End** 方向移动 1px；`current > target` 时不偏移，直接收尾。
4. `FineTuneFallback: "less"`：当 `current > target` 时，把精确点击坐标朝 **End → Start** 方向移动 1px；`current < target` 时不偏移，直接收尾。
5. 偏移后先经 `BetterSlidingReset2` 把滑块向另一侧滑动复位（避免上一次精确点击落在滑块本体上影响本次点击），再执行精确点击并复查；未命中则继续按 1px 累加（第 k 次为基准坐标 `±k` px，最多 4 次）。

方向与轴选择规则：

- 轴由起点与终点的中心点差值决定：`dx = endX - startX`、`dy = endY - startY`，`abs(dx) > abs(dy)` 取 x 轴，否则取 y 轴（**平局取 y**）。`dx == dy == 0` 时取 y 轴并打印告警。
- 正方向即 **Start → End**（`sign(dx)` 或 `sign(dy)`）；方向分量为 0 时取 `+1` 并打印告警。
- 与点击比例语义自洽：`click = start + (end - start) * numerator / denominator`，朝 End 靠近即增大数量，因此 `more` 用于修正「当前 < 目标」。
- 偏移只作用于选定轴的单个分量，另一轴分量保持精确点击基准值不变。

`BetterSlidingReset2` 复位规则：

- 复位方向按**精确点击基准坐标**在 Start → End 轴上的投影位置决定：投影比例 `< 0.5`（靠近 Start）时向 **End**（最大值侧）滑动，否则向 **Start**（最小值侧）滑动；恰好落在中线时取向 Start。
- 滑动终点坐标由 `Direction` 推导并直接覆盖 `BetterSlidingReset2` 的 `end`（`right` / `up` 的最大值侧为 `[1260, 10, 10, 10]`、最小值侧为 `[10, 700, 10, 10]`，`left` / `down` 相反），不使用 pipeline 中的占位 `end`。
- 轴跨度为 0（Start 与 End 中心重合）时打印告警并回退为向 Start 滑动。
- `BetterSlidingReset2` 自身有 `max_hit: 4`，即**每次运行最多复位 4 次**（与 Increase/Decrease 的微调预算一致）；复位后由该节点的静态 `next` 路由回 `BetterSlidingPreciseClick`。

> [!note]
> 偏移与微调的循环上界由**动作节点各自的 `max_hit: 4`** 在框架层限制（`BetterSlidingIncreaseQuantity`、`BetterSlidingDecreaseQuantity`、`BetterSlidingReset2`，以及用于防遮挡的 `BetterSlidingMoveMouse`）。`BetterSlidingCheckQuantity` 自身**不设 `max_hit`**，因此每一次偏移或点击的结果都会被下一次数量复查看到，不存在「最后一次动作的结果不被复查」的问题。
>
> 当上述动作节点的预算全部用尽时，`BetterSlidingCheckQuantity` 的 `next` 候选中已无可用节点，框架以 `Node.NextList.Failed` 结束该内部流水线并**判定为失败**：Go 侧会记录 `internal BetterSliding pipeline failed` 并返回 `false`，同时**不会**写入结果节点（`TargetReachableOverrideEnable` 不会被置为 `true`）。调用方应按失败分支处理。
>
> 注意该判负需要等待框架的 `timeout`（默认 20 秒）耗尽，因此从预算用尽到报错之间会有明显停顿。
>
> 各动作节点的命中计数**每次运行开头由 `BetterSlidingClearMaxHit` 清零**；若后续新增带 `max_hit` 的动作节点，必须同步加入该节点的 `nodes` 列表，否则同一次外层任务内的后续调用会直接跳过该节点。

> [!note]
> 只设置 `FineTuneFallback` 而不关闭微调（`FineTuneQuantity` 保持默认 `true`，或整数阈值判定为需要微调）时，`FineTuneFallback` **不生效**，流程仍走 Increase/Decrease 微调。

> [!note]
> `FineTuneQuantity` 为整数阈值时允许混合行为：偏移使差值进入阈值后，后续轮次会切回 Increase/Decrease 微调。例如 `FineTuneQuantity: 3` + `FineTuneFallback: "more"`，差值从 10 逐步被 1px 偏移压到 3 以内后，剩余差值由 Increase/Decrease 一次点击完成。注意偏移与微调**共享**动作节点的 4 次预算。

### 结果节点契约

`OutOfRangeOverrideEnable` 与 `TargetReachableOverrideEnable` 用于把本次 BetterSliding 的判定传回调用方。两个参数必须引用不同节点，且结果节点建议默认设置 `enabled: false`。

| 解析后的目标 | `OutOfRangeOverrideEnable` | `TargetReachableOverrideEnable` | BetterSliding 行为 |
| ------------------------------------------------------------ | -------------------------- | ------------------------------- | ---------------------------------------------- |
| 小于 1、`sliderMaxQuantity` 为 0，或未钳制时大于滑条最大数量 | `true` | `false` | 不调整数量，返回成功，由调用方处理越界结果 |
| 位于 `[1, sliderMaxQuantity]` | `false` | `true` | 调整到目标数量 |
| 大于 `sliderMaxQuantity` 且启用钳制 | `false` | `false` | 调整到 `sliderMaxQuantity`，尚不能达到原始目标 |

`sliderMaxQuantity == 0` 只表示当前没有可选的正数目标，BetterSliding 不推断余额不足、库存不足或控件不可用等业务原因。调用方如需区分具体状态，应在 Pipeline 中识别对应界面。

> [!important]
> `TargetReachableOverrideEnable` 只表示**解析后的目标可达**，与最终调整结果无关：该判定在读取目标数量与滑条上限时即已确定，之后无论微调、偏移复查是否命中目标，都不会改变它。它只表示调用方的下一步操作可以达到目标，不表示该操作已经成功。例如售卖、购买等流程仍须在外层 Pipeline 确认交易成功后，才能记录业务目标已完成。

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
                "SliderQuantity": {
                    "Box": [340, 430, 200, 140]
                }
            }
        }
    },
    "attach": {
        "TargetQuantity": 50,
        "TargetQuantityType": "Percentage",
        "ReverseTarget": false
    }
}
```
